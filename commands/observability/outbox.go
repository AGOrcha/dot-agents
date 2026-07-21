package observability

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

const (
	outboxVersion      = 1
	ingestSchema       = 1
	maxBatchSize       = 100
	orphanTempLifetime = 24 * time.Hour
	quarantineLifetime = 30 * 24 * time.Hour
	maxRetryAfter      = 24 * time.Hour
)

var (
	readyFilePattern = regexp.MustCompile(`^([0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\.obs-v1\.json$`)
	hashPattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type clientInfo struct {
	DAVersion    string `json:"da_version"`
	HostOS       string `json:"host_os"`
	AgentRuntime string `json:"agent_runtime"`
}

type ingestEvent struct {
	Kind         string          `json:"kind"`
	OccurredAt   string          `json:"occurred_at"`
	PlanID       string          `json:"plan_id"`
	TaskID       string          `json:"task_id"`
	Iteration    int             `json:"iteration"`
	SchemaHash   string          `json:"schema_hash"`
	Payload      json.RawMessage `json:"payload"`
	ScoreSidecar json.RawMessage `json:"score_sidecar"`
}

type outboxRecord struct {
	OutboxVersion int         `json:"outbox_version"`
	ID            string      `json:"id"`
	QueuedAt      string      `json:"queued_at"`
	Attempts      int         `json:"attempts"`
	NextAttemptAt string      `json:"next_attempt_at"`
	LastError     *string     `json:"last_error"`
	ProjectID     string      `json:"project_id"`
	Client        clientInfo  `json:"client"`
	Event         ingestEvent `json:"event"`
}

type queuedFile struct {
	name   string
	path   string
	record outboxRecord
}

type ingestEnvelope struct {
	SchemaVersion int           `json:"schema_version"`
	ProjectID     string        `json:"project_id"`
	Client        clientInfo    `json:"client"`
	Events        []ingestEvent `json:"events"`
}

type eventKey struct {
	ProjectID  string `json:"project_id"`
	PlanID     string `json:"plan_id"`
	TaskID     string `json:"task_id"`
	Iteration  int    `json:"iteration"`
	SchemaHash string `json:"schema_hash"`
}

type rejection struct {
	Index     int      `json:"index"`
	Key       eventKey `json:"key"`
	Code      string   `json:"code"`
	Message   string   `json:"message"`
	Retryable bool     `json:"retryable"`
}

type ingestResponse struct {
	Accepted int         `json:"accepted"`
	Deduped  int         `json:"deduped"`
	Rejected []rejection `json:"rejected"`
}

// SyncReport is the stable result returned to explicit and best-effort callers.
type SyncReport struct {
	Accepted    int      `json:"accepted"`
	Deduped     int      `json:"deduped"`
	Retained    int      `json:"retained"`
	Quarantined int      `json:"quarantined"`
	Pruned      int      `json:"pruned,omitempty"`
	Skipped     int      `json:"skipped,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}

type syncOptions struct {
	Explicit bool
	Full     bool
}

func syncProject(ctx context.Context, projectDir string, deps Deps, options syncOptions) (SyncReport, error) {
	var report SyncReport
	rc, err := deps.loadConfig(projectDir)
	if err != nil {
		return report, fmt.Errorf("load .agentsrc.json: %w", err)
	}
	obs, err := requireObservability(rc)
	if err != nil {
		return report, err
	}
	headers, endpoint, err := authorization(obs, deps.newResolver(), os.Getenv("DA_OBS_TEST_JWT"))
	if err != nil {
		return report, err
	}
	now := deps.now().UTC()
	if err := pruneOutbox(outboxDir(projectDir), now, &report); err != nil {
		report.Errors = append(report.Errors, sanitizeError(err.Error()))
	}
	files, err := loadReadyFiles(outboxDir(projectDir), now, options.Explicit, &report)
	if err != nil {
		report.Errors = append(report.Errors, sanitizeError(err.Error()))
	}
	stopped := false
	if len(files) > 0 {
		stopped = drainReady(ctx, deps.httpClient, endpoint, headers, files, now, &report)
	}
	if options.Full && !stopped {
		replayHistory(ctx, deps.httpClient, endpoint, headers, projectDir, rc, deps.Version, &report)
	}
	if options.Explicit && (report.Retained > 0 || report.Quarantined > 0 || len(report.Errors) > 0) {
		return report, fmt.Errorf("observability sync incomplete: %d retained, %d quarantined%s",
			report.Retained, report.Quarantined, reportErrorSuffix(report.Errors))
	}
	return report, nil
}

func reportErrorSuffix(messages []string) string {
	if len(messages) == 0 {
		return ""
	}
	return ": " + messages[0]
}

func pruneOutbox(dir string, now time.Time, report *SyncReport) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read observability outbox: %w", err)
	}
	var failures []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			failures = append(failures, infoErr.Error())
			continue
		}
		if now.Sub(info.ModTime()) <= orphanTempLifetime {
			continue
		}
		if removeErr := fsops.Remove(filepath.Join(dir, entry.Name())); removeErr != nil {
			failures = append(failures, removeErr.Error())
		} else {
			report.Pruned++
		}
	}
	quarantine := filepath.Join(dir, "quarantine")
	entries, err = os.ReadDir(quarantine)
	if errors.Is(err, os.ErrNotExist) {
		return joinFailures("prune observability outbox", failures)
	}
	if err != nil {
		failures = append(failures, err.Error())
		return joinFailures("prune observability outbox", failures)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			failures = append(failures, infoErr.Error())
			continue
		}
		if now.Sub(info.ModTime()) <= quarantineLifetime {
			continue
		}
		if removeErr := fsops.Remove(filepath.Join(quarantine, entry.Name())); removeErr != nil {
			failures = append(failures, removeErr.Error())
		} else {
			report.Pruned++
		}
	}
	return joinFailures("prune observability outbox", failures)
}

func joinFailures(prefix string, failures []string) error {
	if len(failures) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", prefix, strings.Join(failures, "; "))
}

func loadReadyFiles(dir string, now time.Time, explicit bool, report *SyncReport) ([]queuedFile, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read observability outbox: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	files := make([]queuedFile, 0, len(entries))
	var failures []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".obs-v1.json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		match := readyFilePattern.FindStringSubmatch(entry.Name())
		if len(match) == 0 {
			if quarantineErr := quarantineFile(path, entry.Name(), "corrupt", "corrupt", "ready filename is not a canonical UUIDv7 outbox name", now); quarantineErr != nil {
				failures = append(failures, quarantineErr.Error())
				report.Retained++
			} else {
				report.Quarantined++
			}
			continue
		}
		record, parseErr := parseOutboxFile(path, match[1])
		if parseErr != nil {
			if quarantineErr := quarantineFile(path, entry.Name(), "corrupt", "corrupt", parseErr.Error(), now); quarantineErr != nil {
				failures = append(failures, quarantineErr.Error())
				report.Retained++
			} else {
				report.Quarantined++
			}
			continue
		}
		if !explicit {
			next, nextErr := time.Parse(time.RFC3339, record.NextAttemptAt)
			if nextErr != nil {
				if quarantineErr := quarantineFile(path, entry.Name(), "corrupt", "corrupt", "invalid next_attempt_at", now); quarantineErr != nil {
					failures = append(failures, quarantineErr.Error())
					report.Retained++
				} else {
					report.Quarantined++
				}
				continue
			}
			if next.After(now) {
				report.Skipped++
				continue
			}
		}
		files = append(files, queuedFile{name: entry.Name(), path: path, record: record})
	}
	return files, joinFailures("load observability outbox", failures)
}

func parseOutboxFile(path, filenameID string) (outboxRecord, error) {
	var record outboxRecord
	raw, err := os.ReadFile(path)
	if err != nil {
		return record, fmt.Errorf("read outbox file: %w", err)
	}
	if err := decodeStrict(raw, &record); err != nil {
		return record, fmt.Errorf("invalid outbox JSON: %w", err)
	}
	if record.OutboxVersion != outboxVersion {
		return record, fmt.Errorf("unsupported outbox_version %d", record.OutboxVersion)
	}
	if record.ID != filenameID {
		return record, errors.New("filename UUID does not match outbox id")
	}
	if record.Attempts < 0 || strings.TrimSpace(record.ProjectID) == "" {
		return record, errors.New("outbox retry metadata or project_id is invalid")
	}
	if _, err := time.Parse(time.RFC3339, record.QueuedAt); err != nil {
		return record, errors.New("queued_at is not RFC3339")
	}
	if _, err := time.Parse(time.RFC3339, record.NextAttemptAt); err != nil {
		return record, errors.New("next_attempt_at is not RFC3339")
	}
	if record.Client.DAVersion == "" || record.Client.HostOS == "" || record.Client.AgentRuntime == "" {
		return record, errors.New("outbox client fields must be non-empty")
	}
	if !hashPattern.MatchString(record.Event.SchemaHash) {
		return record, errors.New("schema_hash must be 64 lowercase hexadecimal characters")
	}
	computed, err := computeEventHash(record.Event)
	if err != nil {
		return record, fmt.Errorf("compute schema_hash: %w", err)
	}
	if computed != record.Event.SchemaHash {
		return record, errors.New("schema_hash mismatch")
	}
	return record, nil
}

func decodeStrict(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func drainReady(ctx context.Context, client httpDoer, endpoint interface{ String() string }, headers http.Header, files []queuedFile, now time.Time, report *SyncReport) bool {
	for start := 0; start < len(files); {
		end := batchEnd(files, start)
		batch := files[start:end]
		response, retryAfter, status, err := postBatch(ctx, client, endpoint.String(), headers, batch)
		if err != nil {
			message := sanitizeError(err.Error())
			for i := range batch {
				if rewriteErr := retainWithBackoff(&batch[i], message, retryAfter, now); rewriteErr != nil {
					report.Errors = append(report.Errors, sanitizeError(rewriteErr.Error()))
				}
				report.Retained++
			}
			report.Errors = append(report.Errors, message)
			return true
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			report.Retained += len(files) - start
			report.Errors = append(report.Errors, fmt.Sprintf("authentication failed: HTTP %d", status))
			return true
		}
		if status != http.StatusOK {
			message := fmt.Sprintf("ingest returned HTTP %d", status)
			if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500 {
				for i := range batch {
					if rewriteErr := retainWithBackoff(&batch[i], message, retryAfter, now); rewriteErr != nil {
						report.Errors = append(report.Errors, sanitizeError(rewriteErr.Error()))
					}
					report.Retained++
				}
			} else {
				report.Retained += len(batch)
			}
			report.Errors = append(report.Errors, message)
			return true
		}
		if validationErr := validateIngestResponse(response, len(batch)); validationErr != nil {
			message := sanitizeError(validationErr.Error())
			for i := range batch {
				if rewriteErr := retainWithBackoff(&batch[i], message, retryAfter, now); rewriteErr != nil {
					report.Errors = append(report.Errors, sanitizeError(rewriteErr.Error()))
				}
				report.Retained++
			}
			report.Errors = append(report.Errors, message)
			return true
		}
		rejections := make(map[int]rejection, len(response.Rejected))
		for _, item := range response.Rejected {
			rejections[item.Index] = item
		}
		for index := range batch {
			item, rejected := rejections[index]
			if !rejected {
				if removeErr := fsops.Remove(batch[index].path); removeErr != nil {
					report.Retained++
					report.Errors = append(report.Errors, sanitizeError(fmt.Sprintf("delete %s: %v", batch[index].name, removeErr)))
				}
				continue
			}
			if item.Retryable || item.Code == "storage_unavailable" {
				if rewriteErr := retainWithBackoff(&batch[index], item.Message, retryAfter, now); rewriteErr != nil {
					report.Errors = append(report.Errors, sanitizeError(rewriteErr.Error()))
				}
				report.Retained++
				continue
			}
			if quarantineErr := quarantineFile(batch[index].path, batch[index].name, "rejected", item.Code, item.Message, now); quarantineErr != nil {
				report.Retained++
				report.Errors = append(report.Errors, sanitizeError(quarantineErr.Error()))
			} else {
				report.Quarantined++
			}
		}
		report.Accepted += response.Accepted
		report.Deduped += response.Deduped
		start = end
	}
	return false
}

func batchEnd(files []queuedFile, start int) int {
	end := start + 1
	for end < len(files) && end-start < maxBatchSize &&
		files[end].record.ProjectID == files[start].record.ProjectID &&
		files[end].record.Client == files[start].record.Client {
		end++
	}
	return end
}

func postBatch(ctx context.Context, client httpDoer, endpoint string, headers http.Header, batch []queuedFile) (ingestResponse, retryDelay, int, error) {
	envelope := ingestEnvelope{
		SchemaVersion: ingestSchema,
		ProjectID:     batch[0].record.ProjectID,
		Client:        batch[0].record.Client,
		Events:        make([]ingestEvent, len(batch)),
	}
	for i := range batch {
		envelope.Events[i] = batch[i].record.Event
	}
	return postEnvelope(ctx, client, endpoint, headers, envelope)
}

type retryDelay struct {
	duration time.Duration
	valid    bool
}

func postEnvelope(ctx context.Context, client httpDoer, endpoint string, headers http.Header, envelope ingestEnvelope) (ingestResponse, retryDelay, int, error) {
	var decoded ingestResponse
	body, err := json.Marshal(envelope)
	if err != nil {
		return decoded, retryDelay{}, 0, fmt.Errorf("encode ingest request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURLString(endpoint, ingestPath), bytes.NewReader(body))
	if err != nil {
		return decoded, retryDelay{}, 0, fmt.Errorf("build ingest request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	applyHeaders(req, headers)
	res, err := client.Do(req)
	if err != nil {
		return decoded, retryDelay{}, 0, fmt.Errorf("post observability ingest: %w", err)
	}
	defer res.Body.Close()
	retryAfter := parseRetryAfter(res.Header.Get("Retry-After"), time.Now().UTC())
	body, err = io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return decoded, retryAfter, res.StatusCode, fmt.Errorf("read ingest response: %w", err)
	}
	if res.StatusCode == http.StatusOK {
		decoded, err = parseIngestResponse(body)
		if err != nil {
			return decoded, retryAfter, res.StatusCode, fmt.Errorf("parse ingest response: %w", err)
		}
	}
	return decoded, retryAfter, res.StatusCode, nil
}

func parseIngestResponse(raw []byte) (ingestResponse, error) {
	var wire struct {
		Accepted *int         `json:"accepted"`
		Deduped  *int         `json:"deduped"`
		Rejected *[]rejection `json:"rejected"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return ingestResponse{}, err
	}
	if wire.Accepted == nil || wire.Deduped == nil || wire.Rejected == nil {
		return ingestResponse{}, errors.New("ingest response requires accepted, deduped, and rejected")
	}
	return ingestResponse{Accepted: *wire.Accepted, Deduped: *wire.Deduped, Rejected: *wire.Rejected}, nil
}

func apiURLString(endpoint, route string) string {
	return strings.TrimRight(endpoint, "/") + route
}

func validateIngestResponse(response ingestResponse, count int) error {
	if response.Accepted < 0 || response.Deduped < 0 || response.Accepted+response.Deduped+len(response.Rejected) != count {
		return errors.New("ingest response item counts do not match request")
	}
	seen := make(map[int]struct{}, len(response.Rejected))
	for _, item := range response.Rejected {
		if item.Index < 0 || item.Index >= count {
			return errors.New("ingest response contains an out-of-range rejection index")
		}
		if _, duplicate := seen[item.Index]; duplicate {
			return errors.New("ingest response contains a duplicate rejection index")
		}
		seen[item.Index] = struct{}{}
		if strings.TrimSpace(item.Code) == "" {
			return errors.New("ingest response rejection code is empty")
		}
	}
	return nil
}

func retainWithBackoff(file *queuedFile, message string, retryAfter retryDelay, now time.Time) error {
	file.record.Attempts++
	clean := sanitizeError(message)
	file.record.LastError = &clean
	delay := retryAfter.duration
	if !retryAfter.valid {
		delay = fullJitter(file.record.Attempts)
	}
	if delay > maxRetryAfter {
		delay = maxRetryAfter
	}
	file.record.NextAttemptAt = now.Add(delay).UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(file.record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode retry metadata for %s: %w", file.name, err)
	}
	data = append(data, '\n')
	if err := fsops.WriteFileAtomic(file.path, data); err != nil {
		return fmt.Errorf("rewrite retry metadata for %s: %w", file.name, err)
	}
	return nil
}

func fullJitter(attempts int) time.Duration {
	exponent := attempts
	if exponent > 12 {
		exponent = 12
	}
	capSeconds := math.Pow(2, float64(exponent))
	if capSeconds > 3600 {
		capSeconds = 3600
	}
	var random [8]byte
	if _, err := cryptorand.Read(random[:]); err != nil {
		return 0
	}
	fraction := float64(binary.LittleEndian.Uint64(random[:])) / float64(math.MaxUint64)
	return time.Duration(fraction * capSeconds * float64(time.Second))
}

func parseRetryAfter(value string, now time.Time) retryDelay {
	value = strings.TrimSpace(value)
	if value == "" {
		return retryDelay{}
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		delay := time.Duration(seconds) * time.Second
		if delay > maxRetryAfter {
			delay = maxRetryAfter
		}
		return retryDelay{duration: delay, valid: true}
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return retryDelay{}
	}
	delay := when.Sub(now)
	if delay > maxRetryAfter {
		delay = maxRetryAfter
	}
	return retryDelay{duration: delay, valid: true}
}

type quarantineReason struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

func quarantineFile(path, originalName, suffix, code, message string, now time.Time) error {
	dir := filepath.Join(filepath.Dir(path), "quarantine")
	if err := fsops.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create quarantine directory: %w", err)
	}
	target := filepath.Join(dir, originalName+"."+suffix)
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("quarantine target already exists: %s", filepath.Base(target))
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check quarantine target: %w", err)
	}
	if err := fsops.Rename(path, target); err != nil {
		return fmt.Errorf("quarantine %s: %w", originalName, err)
	}
	reason := quarantineReason{Code: sanitizeError(code), Message: sanitizeError(message), Timestamp: now.UTC().Format(time.RFC3339)}
	data, err := json.Marshal(reason)
	if err != nil {
		return fmt.Errorf("encode quarantine reason: %w", err)
	}
	if err := fsops.WriteFileAtomic(target+".reason.json", append(data, '\n')); err != nil {
		return fmt.Errorf("write quarantine reason for %s: %w", originalName, err)
	}
	return nil
}
