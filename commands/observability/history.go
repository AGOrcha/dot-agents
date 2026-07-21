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

var (
	historyIterationPattern = regexp.MustCompile(`^iter-([1-9]\d*)\.yaml$`)
	historyVerifierPattern  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

const (
	iterationCheckpointedKind = "iteration.checkpointed"
	iterationScoredKind       = "iteration.scored"
	scoreRecomputedKind       = "score.recomputed"
)

type historyEvent struct {
	projectID string
	client    clientInfo
	event     ingestEvent
	source    string
}

type historyRecord struct {
	projectID  string
	client     clientInfo
	occurredAt string
	planID     string
	taskID     string
	iteration  int
	payload    json.RawMessage
	score      json.RawMessage
	hasScore   bool
	source     string
	active     bool
}

type historyLogicalKey struct {
	planID    string
	taskID    string
	iteration int
}

type historyReplayRequest struct {
	ctx        context.Context
	client     httpDoer
	endpoint   string
	headers    http.Header
	projectDir string
	rc         *cfg.AgentsRC
	version    string
	report     *SyncReport
}

func replayHistory(request historyReplayRequest) {
	events, err := collectHistoryEvents(request.projectDir, request.rc, request.version)
	if err != nil {
		request.report.Errors = append(request.report.Errors, sanitizeError(err.Error()))
		return
	}
	for start := 0; start < len(events); {
		end := historyBatchEnd(events, start)
		if !replayHistoryBatch(request, events[start:end], len(events)-start) {
			return
		}
		start = end
	}
}

func replayHistoryBatch(request historyReplayRequest, batch []historyEvent, remaining int) bool {
	envelope := ingestEnvelope{
		SchemaVersion: ingestSchema,
		ProjectID:     batch[0].projectID,
		Client:        batch[0].client,
		Events:        make([]ingestEvent, len(batch)),
	}
	for i := range batch {
		envelope.Events[i] = batch[i].event
	}
	response, _, status, postErr := postEnvelope(request.ctx, request.client, request.endpoint, request.headers, envelope)
	if postErr != nil {
		request.report.Retained += len(batch)
		request.report.Errors = append(request.report.Errors, sanitizeError(postErr.Error()))
		return false
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		request.report.Retained += remaining
		request.report.Errors = append(request.report.Errors, fmt.Sprintf("authentication failed during history replay: HTTP %d", status))
		return false
	}
	if status != http.StatusOK {
		request.report.Retained += len(batch)
		request.report.Errors = append(request.report.Errors, fmt.Sprintf("history ingest returned HTTP %d", status))
		return false
	}
	if validationErr := validateIngestResponse(response, len(batch)); validationErr != nil {
		request.report.Retained += len(batch)
		request.report.Errors = append(request.report.Errors, sanitizeError(validationErr.Error()))
		return false
	}
	request.report.Accepted += response.Accepted
	request.report.Deduped += response.Deduped
	for _, rejected := range response.Rejected {
		request.report.Retained++
		request.report.Errors = append(request.report.Errors, sanitizeError(fmt.Sprintf("history event %s rejected (%s): %s", batch[rejected.Index].source, rejected.Code, rejected.Message)))
	}
	return true
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

	activeRoot := filepath.Join(projectDir, ".agents", "active", "iteration-log")
	paths, err := discoverHistoryPaths(activeRoot, filepath.Join(projectDir, ".agents", "history"))
	if err != nil {
		return nil, err
	}
	records, err := loadHistoryRecords(paths, activeRoot, projectID, version)
	if err != nil {
		return nil, err
	}
	sortHistoryRecords(records)
	return buildHistoryEvents(records)
}

type historyPathSet struct {
	dedicated []string
	aggregate []string
}

func discoverHistoryPaths(activeRoot, historyRoot string) (historyPathSet, error) {
	var paths historyPathSet
	for _, root := range []string{activeRoot, historyRoot} {
		if err := discoverHistoryRoot(root, activeRoot, &paths); err != nil {
			return historyPathSet{}, err
		}
	}
	sort.Strings(paths.dedicated)
	sort.Strings(paths.aggregate)
	return paths, nil
}

func discoverHistoryRoot(root, activeRoot string, paths *historyPathSet) error {
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		if root != activeRoot && filepath.Base(filepath.Dir(path)) != "iteration-log" {
			return nil
		}
		switch {
		case historyIterationPattern.MatchString(entry.Name()):
			paths.dedicated = append(paths.dedicated, path)
		case entry.Name() == "historical.yaml":
			paths.aggregate = append(paths.aggregate, path)
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		return fmt.Errorf("walk history at %s: %w", root, walkErr)
	}
	return nil
}

func loadHistoryRecords(paths historyPathSet, activeRoot, projectID, version string) ([]historyRecord, error) {
	records := make([]historyRecord, 0, len(paths.dedicated))
	dedicatedIterations := make(map[int]struct{}, len(paths.dedicated))
	for _, path := range paths.dedicated {
		record, err := historyRecordFromFile(path, projectID, version, isActiveHistoryPath(path, activeRoot))
		if err != nil {
			return nil, fmt.Errorf("build history event from %s: %w", path, err)
		}
		records = append(records, record)
		dedicatedIterations[record.iteration] = struct{}{}
	}
	for _, path := range paths.aggregate {
		aggregate, err := historyRecordsFromAggregate(path, projectID, version, isActiveHistoryPath(path, activeRoot))
		if err != nil {
			return nil, fmt.Errorf("build aggregate history events from %s: %w", path, err)
		}
		for _, record := range aggregate {
			if _, superseded := dedicatedIterations[record.iteration]; !superseded {
				records = append(records, record)
			}
		}
	}
	return records, nil
}

func isActiveHistoryPath(path, activeRoot string) bool {
	return strings.HasPrefix(path, activeRoot+string(os.PathSeparator))
}

func sortHistoryRecords(records []historyRecord) {
	sort.Slice(records, func(i, j int) bool {
		left, right := records[i], records[j]
		switch {
		case left.occurredAt != right.occurredAt:
			return left.occurredAt < right.occurredAt
		case left.planID != right.planID:
			return left.planID < right.planID
		case left.taskID != right.taskID:
			return left.taskID < right.taskID
		case left.iteration != right.iteration:
			return left.iteration < right.iteration
		case left.active != right.active:
			return !left.active
		default:
			return left.source < right.source
		}
	})
}

type historyEventBuilder struct {
	events            []historyEvent
	seenCheckpoints   map[string]struct{}
	seenScoreVersions map[historyLogicalKey]map[string]struct{}
	scoredIterations  map[historyLogicalKey]bool
}

func buildHistoryEvents(records []historyRecord) ([]historyEvent, error) {
	builder := historyEventBuilder{
		events:            make([]historyEvent, 0, len(records)*2),
		seenCheckpoints:   make(map[string]struct{}, len(records)),
		seenScoreVersions: make(map[historyLogicalKey]map[string]struct{}),
		scoredIterations:  make(map[historyLogicalKey]bool),
	}
	for _, record := range records {
		if err := builder.appendCheckpoint(record); err != nil {
			return nil, err
		}
		if record.hasScore {
			if err := builder.appendScore(record); err != nil {
				return nil, err
			}
		}
	}
	return builder.events, nil
}

func (builder *historyEventBuilder) appendCheckpoint(record historyRecord) error {
	checkpoint, err := record.event(iterationCheckpointedKind, json.RawMessage("null"))
	if err != nil {
		return fmt.Errorf("build checkpoint event from %s: %w", record.source, err)
	}
	if _, duplicate := builder.seenCheckpoints[checkpoint.event.SchemaHash]; duplicate {
		return nil
	}
	builder.seenCheckpoints[checkpoint.event.SchemaHash] = struct{}{}
	builder.events = append(builder.events, checkpoint)
	return nil
}

func (builder *historyEventBuilder) appendScore(record historyRecord) error {
	key := historyLogicalKey{planID: record.planID, taskID: record.taskID, iteration: record.iteration}
	scoreEvent, err := record.event(iterationScoredKind, record.score)
	if err != nil {
		return fmt.Errorf("build score event from %s: %w", record.source, err)
	}
	versions := builder.seenScoreVersions[key]
	if versions == nil {
		versions = make(map[string]struct{})
		builder.seenScoreVersions[key] = versions
	}
	if _, duplicate := versions[scoreEvent.event.SchemaHash]; duplicate {
		return nil
	}
	versions[scoreEvent.event.SchemaHash] = struct{}{}

	if builder.scoredIterations[key] {
		scoreEvent, err = record.event(scoreRecomputedKind, record.score)
		if err != nil {
			return fmt.Errorf("build recomputed score event from %s: %w", record.source, err)
		}
	}
	builder.scoredIterations[key] = true
	builder.events = append(builder.events, scoreEvent)
	return nil
}

func historyRecordFromFile(path, projectID, version string, active bool) (historyRecord, error) {
	var result historyRecord
	raw, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	var payload map[string]any
	if err := yaml.Unmarshal(raw, &payload); err != nil {
		return result, fmt.Errorf("parse iteration YAML: %w", err)
	}

	var scoreJSON json.RawMessage
	hasScore := false
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
		hasScore = true
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return result, fmt.Errorf("read score sidecar: %w", readErr)
	}
	return historyRecordFromPayload(payload, projectID, version, path, active, scoreJSON, hasScore)
}

func historyRecordsFromAggregate(path, projectID, version string, active bool) ([]historyRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var aggregate struct {
		Iterations []map[string]any `yaml:"iterations"`
	}
	if err := yaml.Unmarshal(raw, &aggregate); err != nil {
		return nil, fmt.Errorf("parse historical YAML: %w", err)
	}
	records := make([]historyRecord, 0, len(aggregate.Iterations))
	for index, payload := range aggregate.Iterations {
		source := fmt.Sprintf("%s#iterations[%d]", path, index)
		record, err := historyRecordFromPayload(payload, projectID, version, source, active, nil, false)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func historyRecordFromPayload(payload map[string]any, projectID, version, source string, active bool, score json.RawMessage, hasScore bool) (historyRecord, error) {
	var result historyRecord
	normalized, err := normalizeHistoryPayload(payload)
	if err != nil {
		return result, err
	}
	iteration, err := integerField(normalized, "iteration")
	if err != nil || iteration <= 0 {
		return result, errors.New("iteration must be a positive integer")
	}
	planID, _ := normalized["wave"].(string)
	taskID, _ := normalized["task_id"].(string)
	if strings.TrimSpace(planID) == "" || strings.TrimSpace(taskID) == "" {
		return result, errors.New("wave and task_id must be non-empty strings")
	}
	occurredAt, err := historyOccurredAt(normalized)
	if err != nil {
		return result, err
	}
	payloadJSON, err := json.Marshal(normalized)
	if err != nil {
		return result, fmt.Errorf("encode iteration payload: %w", err)
	}

	agentRuntime := "unknown"
	if agent, ok := normalized["agent"].(map[string]any); ok {
		if harness, ok := agent["harness"].(string); ok && strings.TrimSpace(harness) != "" {
			agentRuntime = harness
		}
	}
	return historyRecord{
		projectID:  projectID,
		client:     clientInfo{DAVersion: version, HostOS: runtime.GOOS, AgentRuntime: agentRuntime},
		occurredAt: occurredAt,
		planID:     planID,
		taskID:     taskID,
		iteration:  iteration,
		payload:    payloadJSON,
		score:      score,
		hasScore:   hasScore,
		source:     source,
		active:     active,
	}, nil
}

func normalizeHistoryPayload(payload map[string]any) (map[string]any, error) {
	schemaVersion, err := integerField(payload, "schema_version")
	if err != nil {
		return nil, err
	}
	switch schemaVersion {
	case 1:
		payload = migrateLegacyHistoryPayload(payload)
	case 2:
		if _, hasImpl := payload["impl"]; !hasImpl {
			payload = migrateLegacyHistoryPayload(payload)
		} else {
			payload = normalizeRoleOwnedHistoryPayload(payload)
		}
	default:
		return nil, fmt.Errorf("unsupported iteration schema_version %d", schemaVersion)
	}
	if value, _ := payload["date"].(string); strings.TrimSpace(value) == "" {
		payload["date"] = "1970-01-01"
	}
	if value, _ := payload["wave"].(string); strings.TrimSpace(value) == "" {
		if legacyPlan, _ := payload["plan_id"].(string); strings.TrimSpace(legacyPlan) != "" {
			payload["wave"] = legacyPlan
		} else {
			payload["wave"] = "legacy-unassigned"
		}
	}
	if value, _ := payload["task_id"].(string); strings.TrimSpace(value) == "" {
		payload["task_id"] = "legacy-unassigned"
	}
	return payload, nil
}

func migrateLegacyHistoryPayload(payload map[string]any) map[string]any {
	scopeNote, _ := payload["scope_note"].(string)
	switch scopeNote {
	case "", "on-target", "scope-breach", "partial":
	default:
		scopeNote = "partial"
	}
	selfAssessment, _ := payload["self_assessment"].(map[string]any)
	implSelfAssessment := map[string]any{
		"read_loop_state":                 boolField(selfAssessment, "read_loop_state"),
		"one_item_only":                   boolField(selfAssessment, "one_item_only"),
		"committed_after_tests":           boolField(selfAssessment, "committed_after_tests"),
		"aligned_with_canonical_tasks":    boolField(selfAssessment, "aligned_with_canonical_tasks"),
		"persisted_via_workflow_commands": stringField(selfAssessment, "persisted_via_workflow_commands"),
		"stayed_under_10_files":           boolField(selfAssessment, "stayed_under_10_files"),
		"no_destructive_commands":         boolField(selfAssessment, "no_destructive_commands"),
	}
	focusedTestsPass, ok := payload["tests_total_pass"].(bool)
	if !ok {
		focusedTestsPass = false
	}
	date := stringField(payload, "date")
	if strings.TrimSpace(date) == "" {
		date = "1970-01-01"
	}
	wave := stringField(payload, "wave")
	if strings.TrimSpace(wave) == "" {
		wave = stringField(payload, "plan_id")
	}
	return map[string]any{
		"schema_version": 2,
		"iteration":      payload["iteration"],
		"date":           date,
		"wave":           wave,
		"task_id":        stringField(payload, "task_id"),
		"commit":         stringField(payload, "commit"),
		"files_changed":  nonNegativeIntegerField(payload, "files_changed"),
		"lines_added":    nonNegativeIntegerField(payload, "lines_added"),
		"lines_removed":  nonNegativeIntegerField(payload, "lines_removed"),
		"first_commit":   boolField(payload, "first_commit"),
		"impl": map[string]any{
			"item":                stringField(payload, "item"),
			"summary":             stringField(payload, "summary"),
			"scope_note":          scopeNote,
			"feedback_goal":       stringField(payload, "feedback_goal"),
			"retries":             nonNegativeIntegerField(payload, "retries"),
			"focused_tests_added": nonNegativeIntegerField(payload, "tests_added"),
			"focused_tests_pass":  focusedTestsPass,
			"self_assessment":     implSelfAssessment,
		},
		"verifiers": []any{},
		"review":    map[string]any{},
	}
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func boolField(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func nonNegativeIntegerField(payload map[string]any, key string) int {
	value, err := integerField(payload, key)
	if err != nil || value < 0 {
		return 0
	}
	return value
}
func normalizeRoleOwnedHistoryPayload(payload map[string]any) map[string]any {
	normalized := copyHistoryFields(payload,
		"schema_version", "iteration", "date", "wave", "task_id", "commit",
		"files_changed", "lines_added", "lines_removed", "first_commit",
		"checkpoint_at",
	)
	normalized["schema_version"] = 2
	if agent, ok := payload["agent"].(map[string]any); ok {
		normalized["agent"] = copyHistoryFields(agent, "session_id", "harness", "harness_version", "model", "entrypoint")
	}
	if tokens, ok := payload["session_tokens"].(map[string]any); ok {
		normalized["session_tokens"] = copyHistoryFields(tokens,
			"input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens",
			"reasoning_tokens", "cache_hit_rate", "message_count",
		)
	}

	impl, _ := payload["impl"].(map[string]any)
	normalizedImpl := copyHistoryFields(impl, "item", "summary", "scope_note", "feedback_goal", "retries", "focused_tests_added")
	scopeNote, _ := normalizedImpl["scope_note"].(string)
	switch scopeNote {
	case "", "on-target", "scope-breach", "partial":
	default:
		normalizedImpl["scope_note"] = "partial"
	}
	if focusedPass, ok := optionalBoolField(impl, "focused_tests_pass"); ok {
		normalizedImpl["focused_tests_pass"] = focusedPass
	} else if legacyPass, ok := optionalBoolField(impl, "tests_total_pass"); ok {
		normalizedImpl["focused_tests_pass"] = legacyPass
	}
	if _, exists := normalizedImpl["focused_tests_added"]; !exists {
		normalizedImpl["focused_tests_added"] = nonNegativeIntegerField(impl, "tests_added")
	}
	if selfAssessment, ok := impl["self_assessment"].(map[string]any); ok {
		normalizedImpl["self_assessment"] = copyHistoryFields(selfAssessment,
			"read_loop_state", "one_item_only", "committed_after_tests",
			"aligned_with_canonical_tasks", "persisted_via_workflow_commands",
			"stayed_under_10_files", "no_destructive_commands",
			"scoped_tests_to_write_scope", "tdd_refresh_performed",
		)
	}
	normalized["impl"] = normalizedImpl

	normalized["verifiers"] = normalizeHistoryVerifiers(payload["verifiers"])
	review, _ := payload["review"].(map[string]any)
	normalizedReview := copyHistoryFields(review,
		"phase_1_decision", "phase_2_decision", "overall_decision", "failed_gates",
		"escalation_reason", "reviewer_notes", "decision_artifact", "verify_record_appended",
	)
	for _, key := range []string{"phase_1_decision", "phase_2_decision", "overall_decision"} {
		switch normalizedReview[key] {
		case nil, "", "accept", "reject", "escalate":
		default:
			normalizedReview[key] = ""
		}
	}
	normalized["review"] = normalizedReview
	return normalized
}

func normalizeHistoryVerifiers(value any) []any {
	rawVerifiers, _ := value.([]any)
	verifiers := make([]any, 0, len(rawVerifiers))
	for _, raw := range rawVerifiers {
		verifier, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		verifierType, _ := verifier["type"].(string)
		if !historyVerifierPattern.MatchString(verifierType) {
			continue
		}
		normalized := copyHistoryFields(verifier,
			"type", "status", "gate_passed", "tests_added", "tests_added_by_kind",
			"linked_traces", "scenario_tags", "retries", "result_artifact",
		)
		switch normalized["status"] {
		case nil, "pass", "fail", "partial", "unknown":
		default:
			normalized["status"] = "unknown"
		}
		if testsPass, ok := optionalBoolField(verifier, "tests_total_pass"); ok {
			normalized["tests_total_pass"] = testsPass
		}
		if selfAssessment, ok := verifier["self_assessment"].(map[string]any); ok {
			normalized["self_assessment"] = copyHistoryFields(selfAssessment,
				"tests_positive_and_negative", "tests_used_sandbox", "exercised_new_scenario",
				"ran_cli_command", "cli_produced_actionable_feedback", "linked_traces_to_outcomes",
			)
		}
		verifiers = append(verifiers, normalized)
	}
	return verifiers
}

func copyHistoryFields(source map[string]any, keys ...string) map[string]any {
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, exists := source[key]; exists {
			result[key] = value
		}
	}
	return result
}

func optionalBoolField(payload map[string]any, key string) (any, bool) {
	switch value := payload[key].(type) {
	case bool:
		return value, true
	case int:
		if value == 0 || value == 1 {
			return value == 1, true
		}
	case int64:
		if value == 0 || value == 1 {
			return value == 1, true
		}
	case uint64:
		if value == 0 || value == 1 {
			return value == 1, true
		}
	case float64:
		if value == 0 || value == 1 {
			return value == 1, true
		}
	case nil:
		if _, exists := payload[key]; exists {
			return nil, true
		}
	}
	return nil, false
}

func (record historyRecord) event(kind string, score json.RawMessage) (historyEvent, error) {
	event := ingestEvent{
		Kind:         kind,
		OccurredAt:   record.occurredAt,
		PlanID:       record.planID,
		TaskID:       record.taskID,
		Iteration:    record.iteration,
		Payload:      record.payload,
		ScoreSidecar: score,
	}
	hash, err := computeEventHash(event)
	if err != nil {
		return historyEvent{}, fmt.Errorf("compute event hash: %w", err)
	}
	event.SchemaHash = hash
	return historyEvent{
		projectID: record.projectID,
		client:    record.client,
		event:     event,
		source:    record.source,
	}, nil
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
