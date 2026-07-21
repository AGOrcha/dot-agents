package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

var historyIterationPattern = regexp.MustCompile(`^iter-([1-9]\d*)\.yaml$`)

type historyEvent struct {
	projectID string
	client    clientInfo
	event     ingestEvent
	source    string
}

func replayHistory(ctx context.Context, client httpDoer, endpoint interface{ String() string }, headers http.Header, projectDir string, rc *cfg.AgentsRC, version string, report *SyncReport) {
	events, err := collectHistoryEvents(projectDir, rc, version)
	if err != nil {
		report.Errors = append(report.Errors, sanitizeError(err.Error()))
		return
	}
	for start := 0; start < len(events); {
		end := historyBatchEnd(events, start)
		batch := events[start:end]
		envelope := ingestEnvelope{
			SchemaVersion: ingestSchema,
			ProjectID:     batch[0].projectID,
			Client:        batch[0].client,
			Events:        make([]ingestEvent, len(batch)),
		}
		for i := range batch {
			envelope.Events[i] = batch[i].event
		}
		response, _, status, postErr := postEnvelope(ctx, client, endpoint.String(), headers, envelope)
		if postErr != nil {
			report.Retained += len(batch)
			report.Errors = append(report.Errors, sanitizeError(postErr.Error()))
			return
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			report.Retained += len(events) - start
			report.Errors = append(report.Errors, fmt.Sprintf("authentication failed during history replay: HTTP %d", status))
			return
		}
		if status != http.StatusOK {
			report.Retained += len(batch)
			report.Errors = append(report.Errors, fmt.Sprintf("history ingest returned HTTP %d", status))
			return
		}
		if validationErr := validateIngestResponse(response, len(batch)); validationErr != nil {
			report.Retained += len(batch)
			report.Errors = append(report.Errors, sanitizeError(validationErr.Error()))
			return
		}
		report.Accepted += response.Accepted
		report.Deduped += response.Deduped
		if len(response.Rejected) > 0 {
			report.Retained += len(response.Rejected)
			for _, rejected := range response.Rejected {
				report.Errors = append(report.Errors, sanitizeError(fmt.Sprintf("history event %s rejected (%s): %s", batch[rejected.Index].source, rejected.Code, rejected.Message)))
			}
		}
		start = end
	}
}

func historyBatchEnd(events []historyEvent, start int) int {
	end := start + 1
	for end < len(events) && end-start < maxBatchSize &&
		events[end].projectID == events[start].projectID &&
		events[end].client == events[start].client {
		end++
	}
	return end
}

func collectHistoryEvents(projectDir string, rc *cfg.AgentsRC, version string) ([]historyEvent, error) {
	projectID := strings.TrimSpace(rc.RepoID)
	if projectID == "" {
		projectID = cfg.DeriveRepoIDFromGit(projectDir)
	}
	if projectID == "" {
		return nil, errors.New("--full requires .agentsrc.json repo_id or a canonical git origin")
	}
	roots := []string{
		filepath.Join(projectDir, ".agents", "active", "iteration-log"),
		filepath.Join(projectDir, ".agents", "history"),
	}
	var paths []string
	for _, root := range roots {
		walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !entry.Type().IsRegular() || !historyIterationPattern.MatchString(entry.Name()) {
				return nil
			}
			paths = append(paths, path)
			return nil
		})
		if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
			return nil, fmt.Errorf("walk history at %s: %w", root, walkErr)
		}
	}
	sort.Strings(paths)
	events := make([]historyEvent, 0, len(paths))
	for _, path := range paths {
		event, err := historyEventFromFile(path, projectID, version)
		if err != nil {
			return nil, fmt.Errorf("build history event from %s: %w", path, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func historyEventFromFile(path, projectID, version string) (historyEvent, error) {
	var result historyEvent
	raw, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	var payload map[string]any
	if err := yaml.Unmarshal(raw, &payload); err != nil {
		return result, fmt.Errorf("parse iteration YAML: %w", err)
	}
	iteration, err := integerField(payload, "iteration")
	if err != nil || iteration <= 0 {
		return result, errors.New("iteration must be a positive integer")
	}
	planID, _ := payload["wave"].(string)
	taskID, _ := payload["task_id"].(string)
	if strings.TrimSpace(planID) == "" || strings.TrimSpace(taskID) == "" {
		return result, errors.New("wave and task_id must be non-empty strings")
	}
	occurredAt, err := historyOccurredAt(payload)
	if err != nil {
		return result, err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return result, fmt.Errorf("encode iteration payload: %w", err)
	}
	scoreJSON := json.RawMessage("null")
	kind := "iteration.checkpointed"
	scorePath := strings.TrimSuffix(path, ".yaml") + ".score.yaml"
	if scoreRaw, readErr := os.ReadFile(scorePath); readErr == nil {
		var score any
		if err := yaml.Unmarshal(scoreRaw, &score); err != nil {
			return result, fmt.Errorf("parse score sidecar: %w", err)
		}
		encoded, err := json.Marshal(score)
		if err != nil {
			return result, fmt.Errorf("encode score sidecar: %w", err)
		}
		scoreJSON = encoded
		kind = "iteration.scored"
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return result, fmt.Errorf("read score sidecar: %w", readErr)
	}
	agentRuntime := "unknown"
	if agent, ok := payload["agent"].(map[string]any); ok {
		if harness, ok := agent["harness"].(string); ok && strings.TrimSpace(harness) != "" {
			agentRuntime = harness
		}
	}
	event := ingestEvent{
		Kind:         kind,
		OccurredAt:   occurredAt,
		PlanID:       planID,
		TaskID:       taskID,
		Iteration:    iteration,
		Payload:      payloadJSON,
		ScoreSidecar: scoreJSON,
	}
	event.SchemaHash, err = computeEventHash(event)
	if err != nil {
		return result, fmt.Errorf("compute event hash: %w", err)
	}
	result = historyEvent{
		projectID: projectID,
		client: clientInfo{
			DAVersion:    version,
			HostOS:       runtime.GOOS,
			AgentRuntime: agentRuntime,
		},
		event:  event,
		source: path,
	}
	return result, nil
}

func integerField(payload map[string]any, key string) (int, error) {
	switch value := payload[key].(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case uint64:
		return int(value), nil
	case float64:
		if value == float64(int(value)) {
			return int(value), nil
		}
	case string:
		return strconv.Atoi(value)
	}
	return 0, fmt.Errorf("%s is not an integer", key)
}

func historyOccurredAt(payload map[string]any) (string, error) {
	if checkpoint, ok := payload["checkpoint_at"].(string); ok && checkpoint != "" {
		parsed, err := time.Parse(time.RFC3339, checkpoint)
		if err != nil {
			return "", errors.New("checkpoint_at is not RFC3339")
		}
		return parsed.UTC().Format(time.RFC3339), nil
	}
	date, ok := payload["date"].(string)
	if !ok || date == "" {
		return "", errors.New("iteration has neither checkpoint_at nor date")
	}
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", errors.New("date is not YYYY-MM-DD")
	}
	return parsed.UTC().Format(time.RFC3339), nil
}
