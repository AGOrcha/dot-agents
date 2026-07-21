package workflow

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/AGOrcha/dot-agents/commands/observability"
	"github.com/AGOrcha/dot-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

const (
	observabilityOutboxVersion = 1
	observabilityDAVersion     = "dev"
	observabilityPublishLimit  = 2 * time.Second
)

type observabilityClient struct {
	DAVersion    string `json:"da_version"`
	HostOS       string `json:"host_os"`
	AgentRuntime string `json:"agent_runtime"`
}

type observabilityEvent struct {
	Kind         string          `json:"kind"`
	OccurredAt   string          `json:"occurred_at"`
	PlanID       string          `json:"plan_id"`
	TaskID       string          `json:"task_id"`
	Iteration    int             `json:"iteration"`
	SchemaHash   string          `json:"schema_hash"`
	Payload      json.RawMessage `json:"payload"`
	ScoreSidecar json.RawMessage `json:"score_sidecar"`
}

type observabilityOutboxRecord struct {
	OutboxVersion int                 `json:"outbox_version"`
	ID            string              `json:"id"`
	QueuedAt      string              `json:"queued_at"`
	Attempts      int                 `json:"attempts"`
	NextAttemptAt string              `json:"next_attempt_at"`
	LastError     *string             `json:"last_error"`
	ProjectID     string              `json:"project_id"`
	Client        observabilityClient `json:"client"`
	Event         observabilityEvent  `json:"event"`
}

// publishCheckpointBestEffort queues the accepted canonical iteration record,
// then gives the observability client a bounded chance to drain it. Every error
// is intentionally swallowed: the local workflow artifact is authoritative.
func publishCheckpointBestEffort(projectPath string, iteration int) {
	publishIterationBestEffort(projectPath, iteration, "iteration.checkpointed", false)
}

// publishScoreBestEffort queues a first-score event only after its sidecar has
// landed. Recompute writers must use a separate hook once they can prove the
// persisted score content changed; verify-record never invents that event.
func publishScoreBestEffort(projectPath string, iteration int) {
	publishIterationBestEffort(projectPath, iteration, "iteration.scored", true)
}

// publishLatestIterationBestEffort is the verify-record hook. Verification
// records do not create score versions: they re-publish the current canonical
// state so the server can idempotently dedupe a prior successful delivery.
func publishLatestIterationBestEffort(projectPath string) {
	iteration, active, err := resolveActiveIterationN(stdHookOutcomeDeps{}, projectPath)
	if err != nil || !active {
		return
	}
	if _, err := os.Stat(scoreSidecarPath(projectPath, iteration)); err == nil {
		publishScoreBestEffort(projectPath, iteration)
		return
	}
	publishCheckpointBestEffort(projectPath, iteration)
}

func publishIterationBestEffort(projectPath string, iteration int, kind string, withScore bool) {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil || rc == nil || rc.Observability == nil || !rc.Observability.Enabled {
		return
	}
	now := time.Now().UTC()
	record, err := buildObservabilityRecord(projectPath, iteration, kind, withScore, rc, now)
	if err != nil {
		return
	}
	if err := enqueueObservabilityRecord(projectPath, record); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), observabilityPublishLimit)
	defer cancel()
	observability.PublishBestEffort(ctx, projectPath, observabilityDAVersion, nil)
}

func buildObservabilityRecord(projectPath string, iteration int, kind string, withScore bool, rc *config.AgentsRC, now time.Time) (observabilityOutboxRecord, error) {
	var record observabilityOutboxRecord
	payloadRaw, err := os.ReadFile(iterRecordPath(projectPath, iteration))
	if err != nil {
		return record, err
	}
	var payload map[string]any
	if err := yaml.Unmarshal(payloadRaw, &payload); err != nil {
		return record, err
	}
	planID, _ := payload["wave"].(string)
	taskID, _ := payload["task_id"].(string)
	if strings.TrimSpace(planID) == "" || strings.TrimSpace(taskID) == "" {
		return record, errors.New("iteration wave and task_id are required")
	}
	payloadIteration, ok := observabilityInteger(payload["iteration"])
	if !ok || payloadIteration != iteration {
		return record, errors.New("iteration payload does not match event iteration")
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return record, err
	}

	scoreJSON := json.RawMessage("null")
	if withScore {
		scoreRaw, readErr := os.ReadFile(scoreSidecarPath(projectPath, iteration))
		if readErr != nil {
			return record, readErr
		}
		var score map[string]any
		if err := yaml.Unmarshal(scoreRaw, &score); err != nil {
			return record, err
		}
		scoreIteration, ok := observabilityInteger(score["iteration"])
		if !ok || scoreIteration != iteration {
			return record, errors.New("score sidecar does not match event iteration")
		}
		scoreJSON, err = json.Marshal(score)
		if err != nil {
			return record, err
		}
	}

	checkpointAt, _ := payload["checkpoint_at"].(string)
	parsed, parseErr := time.Parse(time.RFC3339, checkpointAt)
	if parseErr != nil {
		return record, errors.New("iteration checkpoint_at is not RFC3339")
	}
	// The iteration's canonical timestamp keeps verify-record re-publication
	// byte-equivalent to the first checkpoint/score event for server dedupe.
	occurredAt := parsed.UTC().Format(time.RFC3339)
	event := observabilityEvent{
		Kind:         kind,
		OccurredAt:   occurredAt,
		PlanID:       planID,
		TaskID:       taskID,
		Iteration:    iteration,
		Payload:      payloadJSON,
		ScoreSidecar: scoreJSON,
	}
	event.SchemaHash, err = hashObservabilityEvent(event)
	if err != nil {
		return record, err
	}

	projectID := strings.TrimSpace(rc.RepoID)
	if projectID == "" {
		projectID = config.DeriveRepoIDFromGit(projectPath)
	}
	if projectID == "" {
		return record, errors.New("observability project_id is empty")
	}
	agentRuntime := "unknown"
	if agent, ok := payload["agent"].(map[string]any); ok {
		if harness, ok := agent["harness"].(string); ok && strings.TrimSpace(harness) != "" {
			agentRuntime = strings.TrimSpace(harness)
		}
	}
	id, err := newObservabilityUUIDv7(now)
	if err != nil {
		return record, err
	}
	queuedAt := now.Format(time.RFC3339)
	nextAttempt := now.Add(time.Duration(rc.Observability.PushThrottleSeconds) * time.Second).Format(time.RFC3339)
	return observabilityOutboxRecord{
		OutboxVersion: observabilityOutboxVersion,
		ID:            id,
		QueuedAt:      queuedAt,
		Attempts:      0,
		NextAttemptAt: nextAttempt,
		LastError:     nil,
		ProjectID:     projectID,
		Client: observabilityClient{
			DAVersion:    observabilityDAVersion,
			HostOS:       runtime.GOOS,
			AgentRuntime: agentRuntime,
		},
		Event: event,
	}, nil
}

func observabilityInteger(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case uint64:
		return int(value), true
	case float64:
		return int(value), value == float64(int(value))
	case string:
		parsed, err := strconv.Atoi(value)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func enqueueObservabilityRecord(projectPath string, record observabilityOutboxRecord) error {
	dir := filepath.Join(projectPath, ".agents", "active", "obs-outbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmpPath := filepath.Join(dir, "."+record.ID+".tmp")
	readyPath := filepath.Join(dir, record.ID+".obs-v1.json")
	file, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	written, err := file.Write(body)
	if err != nil || written != len(body) {
		_ = file.Close()
		if err != nil {
			return err
		}
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, readyPath); err != nil {
		return err
	}
	removeTemp = false
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}

func newObservabilityUUIDv7(now time.Time) (string, error) {
	var id [16]byte
	millis := uint64(now.UnixMilli())
	id[0] = byte(millis >> 40)
	id[1] = byte(millis >> 32)
	id[2] = byte(millis >> 24)
	id[3] = byte(millis >> 16)
	id[4] = byte(millis >> 8)
	id[5] = byte(millis)
	if _, err := io.ReadFull(rand.Reader, id[6:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x70
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		id[0:4], id[4:6], id[6:8], id[8:10], id[10:16]), nil
}

func hashObservabilityEvent(event observabilityEvent) (string, error) {
	material := struct {
		Kind         string          `json:"kind"`
		OccurredAt   string          `json:"occurred_at"`
		PlanID       string          `json:"plan_id"`
		TaskID       string          `json:"task_id"`
		Iteration    int             `json:"iteration"`
		Payload      json.RawMessage `json:"payload"`
		ScoreSidecar json.RawMessage `json:"score_sidecar"`
	}{
		Kind:         event.Kind,
		OccurredAt:   event.OccurredAt,
		PlanID:       event.PlanID,
		TaskID:       event.TaskID,
		Iteration:    event.Iteration,
		Payload:      event.Payload,
		ScoreSidecar: event.ScoreSidecar,
	}
	raw, err := json.Marshal(material)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalizeObservabilityJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalizeObservabilityJSON(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("JSON is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("JSON contains trailing data")
	}
	var out bytes.Buffer
	if err := appendCanonicalObservabilityJSON(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func appendCanonicalObservabilityJSON(out *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if value {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		appendCanonicalObservabilityString(out, value)
	case json.Number:
		number, err := canonicalObservabilityNumber(value)
		if err != nil {
			return err
		}
		out.WriteString(number)
	case []any:
		out.WriteByte('[')
		for i, item := range value {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := appendCanonicalObservabilityJSON(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return observabilityUTF16Less(keys[i], keys[j]) })
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			appendCanonicalObservabilityString(out, key)
			out.WriteByte(':')
			if err := appendCanonicalObservabilityJSON(out, value[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

func appendCanonicalObservabilityString(out *bytes.Buffer, value string) {
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

func canonicalObservabilityNumber(number json.Number) (string, error) {
	value, err := strconv.ParseFloat(string(number), 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return "", fmt.Errorf("number %q is not a finite IEEE-754 value", number)
	}
	if value == 0 {
		return "0", nil
	}
	absolute := math.Abs(value)
	if absolute >= 1e-6 && absolute < 1e21 {
		return strconv.FormatFloat(value, 'f', -1, 64), nil
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	marker := strings.LastIndexByte(scientific, 'e')
	mantissa, exponent := scientific[:marker], scientific[marker+1:]
	sign := ""
	if exponent[0] == '+' || exponent[0] == '-' {
		sign, exponent = exponent[:1], exponent[1:]
	}
	for len(exponent) > 1 && exponent[0] == '0' {
		exponent = exponent[1:]
	}
	if sign == "" {
		sign = "+"
	}
	return mantissa + "e" + sign + exponent, nil
}

func observabilityUTF16Less(left, right string) bool {
	a, b := utf16.Encode([]rune(left)), utf16.Encode([]rune(right))
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
