package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

const (
	testAgentsDir       = ".agents"
	testIterationLogDir = "iteration-log"
	testPlanA           = "plan-a"
	testTaskA           = "task-a"
	testSessionA        = "session-a"
)

func TestFullReplayPopulatesDedupesAndRebuildsEquivalentRunSet(t *testing.T) {
	project := t.TempDir()
	archived := filepath.Join(project, testAgentsDir, "history", "archived-plan", testIterationLogDir)
	active := filepath.Join(project, testAgentsDir, "active", testIterationLogDir)
	if err := os.MkdirAll(archived, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(active, 0o700); err != nil {
		t.Fatal(err)
	}
	writeHistoryFile(t, filepath.Join(archived, "iter-1.yaml"), rebuildIterationYAML(1, "archived-plan", testTaskA, testSessionA, "2026-07-18T10:00:00Z"))
	writeHistoryFile(t, filepath.Join(archived, "iter-1.score.yaml"), rebuildScoreYAML(1, 0.8, "good"))
	writeHistoryFile(t, filepath.Join(active, "iter-2.yaml"), rebuildIterationYAML(2, "active-plan", "task-b", "session-b", "2026-07-19T10:00:00Z"))
	writeHistoryFile(t, filepath.Join(active, "iter-2.score.yaml"), rebuildScoreYAML(2, 0.95, "excellent"))
	writeHistoryFile(t, filepath.Join(active, "iter-3.yaml"), rebuildIterationYAML(3, "active-plan", "task-c", testSessionA, "2026-07-20T10:00:00Z"))

	remote := newRebuildRemote()
	server := httptest.NewServer(remote)
	defer server.Close()
	deps := testDeps(t, server)

	first, err := syncProject(context.Background(), project, deps, syncOptions{Explicit: true, Full: true})
	if err != nil {
		t.Fatalf("first full sync: %v (%#v)", err, first)
	}
	if first.Accepted != 5 || first.Deduped != 0 {
		t.Fatalf("first full sync report = %#v, want accepted=5 deduped=0", first)
	}
	beforeBody, beforeETag := fetchRebuildRuns(t, server)
	var runs []rebuildRun
	if err := json.Unmarshal(beforeBody, &runs); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	wantRuns := []rebuildRun{
		{SessionID: testSessionA, IterationCount: 2, Scored: true},
		{SessionID: "session-b", IterationCount: 1, Scored: true},
	}
	if fmt.Sprint(runs) != fmt.Sprint(wantRuns) {
		t.Fatalf("run set = %#v, want %#v", runs, wantRuns)
	}

	second, err := syncProject(context.Background(), project, deps, syncOptions{Explicit: true, Full: true})
	if err != nil {
		t.Fatalf("second full sync: %v (%#v)", err, second)
	}
	if second.Accepted != 0 || second.Deduped != 5 {
		t.Fatalf("second full sync report = %#v, want accepted=0 deduped=5", second)
	}

	remote.wipe()
	third, err := syncProject(context.Background(), project, deps, syncOptions{Explicit: true, Full: true})
	if err != nil {
		t.Fatalf("full sync after wipe: %v (%#v)", err, third)
	}
	if third.Accepted != 5 || third.Deduped != 0 {
		t.Fatalf("post-wipe full sync report = %#v, want accepted=5 deduped=0", third)
	}
	afterBody, afterETag := fetchRebuildRuns(t, server)
	if string(afterBody) != string(beforeBody) || afterETag != beforeETag {
		t.Fatalf("rebuilt route changed:\n before body=%s etag=%s\n  after body=%s etag=%s", beforeBody, beforeETag, afterBody, afterETag)
	}
}

func TestFullReplayMigratesLegacyAggregateAndEmitsScoreRecomputeInOrder(t *testing.T) {
	project := t.TempDir()
	active := filepath.Join(project, testAgentsDir, "active", testIterationLogDir)
	archived := filepath.Join(project, testAgentsDir, "history", testPlanA, testIterationLogDir)
	if err := os.MkdirAll(active, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(archived, 0o700); err != nil {
		t.Fatal(err)
	}
	writeHistoryFile(t, filepath.Join(active, "historical.yaml"), `
iterations:
  - schema_version: 1
    iteration: 4
    date: "2026-07-17"
    wave: legacy-plan
    task_id: ""
    tests_total_pass: true
    scope_note: "legacy path list"
`)
	writeHistoryFile(t, filepath.Join(archived, "iter-5.yaml"), rebuildIterationYAML(5, testPlanA, testTaskA, testSessionA, "2026-07-18T10:00:00Z"))
	writeHistoryFile(t, filepath.Join(archived, "iter-5.score.yaml"), rebuildScoreYAML(5, 0.7, "good"))
	writeHistoryFile(t, filepath.Join(active, "iter-5.yaml"), rebuildIterationYAML(5, testPlanA, testTaskA, testSessionA, "2026-07-19T10:00:00Z"))
	writeHistoryFile(t, filepath.Join(active, "iter-5.score.yaml"), rebuildScoreYAML(5, 0.9, "excellent"))

	events, err := collectHistoryEvents(project, configuredRC("http://localhost", false), "test")
	if err != nil {
		t.Fatalf("collectHistoryEvents: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("events = %d, want legacy checkpoint plus two checkpoint/score versions", len(events))
	}
	wantKinds := []string{iterationCheckpointedKind, iterationCheckpointedKind, iterationScoredKind, iterationCheckpointedKind, scoreRecomputedKind}
	for index, event := range events {
		if event.event.Kind != wantKinds[index] {
			t.Fatalf("event %d kind = %q, want %q", index, event.event.Kind, wantKinds[index])
		}
		computed, hashErr := computeEventHash(event.event)
		if hashErr != nil || computed != event.event.SchemaHash {
			t.Fatalf("event %d hash = %q, computed=%q err=%v", index, event.event.SchemaHash, computed, hashErr)
		}
	}
	var legacy map[string]any
	if err := json.Unmarshal(events[0].event.Payload, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy["schema_version"] != float64(2) || legacy["task_id"] != "legacy-unassigned" {
		t.Fatalf("normalized legacy payload = %#v", legacy)
	}
	impl := legacy["impl"].(map[string]any)
	if impl["scope_note"] != "partial" {
		t.Fatalf("legacy scope_note = %v, want partial", impl["scope_note"])
	}
}

func rebuildIterationYAML(iteration int, planID, taskID, sessionID, checkpointAt string) string {
	return fmt.Sprintf(`
schema_version: 2
iteration: %d
date: %q
wave: %s
task_id: %s
checkpoint_at: %q
agent:
  session_id: %s
  harness: codex
  model: gpt-test
impl: {}
verifiers: []
review: {}
`, iteration, checkpointAt[:10], planID, taskID, checkpointAt, sessionID)
}

func rebuildScoreYAML(iteration int, value float64, band string) string {
	return fmt.Sprintf(`
iteration: %d
rubric_version: 1.0.0
scored: true
value: %.2f
band: %s
breakdown: []
`, iteration, value, band)
}

type rebuildStoredEvent struct {
	event ingestEvent
	order int
}

type rebuildRemote struct {
	mu     sync.Mutex
	events map[string]rebuildStoredEvent
	next   int
}

func newRebuildRemote() *rebuildRemote {
	return &rebuildRemote{events: make(map[string]rebuildStoredEvent)}
}

func (remote *rebuildRemote) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodPost && request.URL.Path == ingestPath:
		remote.ingest(w, request)
	case request.Method == http.MethodGet && request.URL.Path == "/api/v1/observability/runs":
		remote.listRuns(w)
	default:
		http.NotFound(w, request)
	}
}

func (remote *rebuildRemote) ingest(w http.ResponseWriter, request *http.Request) {
	var envelope ingestEnvelope
	if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	response := ingestResponse{Rejected: []rejection{}}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	for index, event := range envelope.Events {
		hash, err := computeEventHash(event)
		if err != nil || hash != event.SchemaHash {
			response.Rejected = append(response.Rejected, rejection{Index: index, Code: "invalid_event", Message: "bad schema hash"})
			continue
		}
		if _, exists := remote.events[event.SchemaHash]; exists {
			response.Deduped++
			continue
		}
		remote.next++
		remote.events[event.SchemaHash] = rebuildStoredEvent{event: event, order: remote.next}
		response.Accepted++
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

type rebuildRun struct {
	SessionID      string `json:"session_id"`
	IterationCount int    `json:"iteration_count"`
	Scored         bool   `json:"scored"`
}

func (remote *rebuildRemote) listRuns(w http.ResponseWriter) {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	type logicalKey struct {
		planID    string
		taskID    string
		iteration int
	}
	winning := make(map[logicalKey]rebuildStoredEvent)
	for _, candidate := range remote.events {
		key := logicalKey{planID: candidate.event.PlanID, taskID: candidate.event.TaskID, iteration: candidate.event.Iteration}
		current, exists := winning[key]
		if !exists || candidate.event.OccurredAt > current.event.OccurredAt ||
			(candidate.event.OccurredAt == current.event.OccurredAt && candidate.order > current.order) {
			winning[key] = candidate
		}
	}
	runsBySession := make(map[string]rebuildRun)
	for _, stored := range winning {
		var payload struct {
			Agent struct {
				SessionID string `json:"session_id"`
			} `json:"agent"`
		}
		_ = json.Unmarshal(stored.event.Payload, &payload)
		sessionID := payload.Agent.SessionID
		if sessionID == "" {
			sessionID = stored.event.PlanID + ":" + stored.event.TaskID
		}
		run := runsBySession[sessionID]
		run.SessionID = sessionID
		run.IterationCount++
		run.Scored = run.Scored || stored.event.Kind != iterationCheckpointedKind
		runsBySession[sessionID] = run
	}
	runs := make([]rebuildRun, 0, len(runsBySession))
	for _, run := range runsBySession {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].SessionID < runs[j].SessionID })
	body, _ := json.Marshal(runs)
	sum := sha256.Sum256(body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	_, _ = w.Write(body)
}

func (remote *rebuildRemote) wipe() {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	remote.events = make(map[string]rebuildStoredEvent)
	remote.next = 0
}

func fetchRebuildRuns(t *testing.T, server *httptest.Server) ([]byte, string) {
	t.Helper()
	response, err := server.Client().Get(server.URL + "/api/v1/observability/runs")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body, response.Header.Get("ETag")
}
