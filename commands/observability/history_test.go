package observability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

func TestHistoryEventBuildsCheckpointAndScoredEnvelopes(t *testing.T) {
	project := t.TempDir()
	dir := filepath.Join(project, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeHistoryFile(t, filepath.Join(dir, "iter-1.yaml"), `
schema_version: 2
iteration: 1
date: "2026-07-20"
wave: plan-a
task_id: task-a
checkpoint_at: "2026-07-20T10:00:00Z"
agent:
  harness: codex
impl: {}
verifiers: []
review: {}
`)
	writeHistoryFile(t, filepath.Join(dir, "iter-2.yaml"), `
schema_version: 2
iteration: 2
date: "2026-07-20"
wave: plan-a
task_id: task-b
checkpoint_at: "2026-07-20T11:00:00Z"
impl: {}
verifiers: []
review: {}
`)
	writeHistoryFile(t, filepath.Join(dir, "iter-2.score.yaml"), `
iteration: 2
rubric_version: r1
scored: true
value: 0.9
band: excellent
breakdown: []
`)
	rc := &cfg.AgentsRC{RepoID: "github.com/AGOrcha/dot-agents"}
	events, err := collectHistoryEvents(project, rc, "v1.2.3")
	if err != nil {
		t.Fatalf("collectHistoryEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d", len(events))
	}
	assertHistoryEvent(t, events[0], iterationCheckpointedKind, 1)
	if string(events[0].event.ScoreSidecar) != "null" {
		t.Fatalf("first checkpoint sidecar = %s", events[0].event.ScoreSidecar)
	}
	if events[0].client.AgentRuntime != "codex" || events[0].client.DAVersion != "v1.2.3" {
		t.Fatalf("client = %#v", events[0].client)
	}
	assertHistoryEvent(t, events[1], iterationCheckpointedKind, 2)
	assertHistoryEvent(t, events[2], iterationScoredKind, 2)
	var score map[string]any
	if err := json.Unmarshal(events[2].event.ScoreSidecar, &score); err != nil || score["rubric_version"] != "r1" {
		t.Fatalf("score sidecar = %s err=%v", events[2].event.ScoreSidecar, err)
	}
	assertHistoryHashes(t, events)
}

func TestFullReplayAlsoWalksAgentsHistory(t *testing.T) {
	project := t.TempDir()
	historyDir := filepath.Join(project, ".agents", "history", "archived-plan", "iteration-log")
	if err := os.MkdirAll(historyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeHistoryFile(t, filepath.Join(historyDir, "iter-7.yaml"), `
schema_version: 1
iteration: 7
date: "2026-07-19"
wave: archived-plan
task_id: archived-task
`)
	events, err := collectHistoryEvents(project, &cfg.AgentsRC{RepoID: "github.com/AGOrcha/dot-agents"}, "test")
	if err != nil {
		t.Fatalf("collectHistoryEvents: %v", err)
	}
	if len(events) != 1 || events[0].event.PlanID != "archived-plan" || events[0].event.Iteration != 7 {
		t.Fatalf("history events = %#v", events)
	}
	if events[0].event.OccurredAt != "2026-07-19T00:00:00Z" {
		t.Fatalf("date fallback = %q", events[0].event.OccurredAt)
	}
}

func assertHistoryEvent(t *testing.T, event historyEvent, kind string, iteration int) {
	t.Helper()
	if event.event.Kind != kind {
		t.Fatalf("event kind = %q, want %q", event.event.Kind, kind)
	}
	if event.event.Iteration != iteration {
		t.Fatalf("event iteration = %d, want %d", event.event.Iteration, iteration)
	}
}

func assertHistoryHashes(t *testing.T, events []historyEvent) {
	t.Helper()
	for _, event := range events {
		computed, err := computeEventHash(event.event)
		if err != nil || computed != event.event.SchemaHash {
			t.Fatalf("history event hash mismatch: computed=%s stored=%s err=%v", computed, event.event.SchemaHash, err)
		}
	}
}

func writeHistoryFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
