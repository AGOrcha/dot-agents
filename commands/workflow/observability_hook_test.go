package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func writeObservabilityHookConfig(t *testing.T, repo string, enabled bool, endpoint string, throttle int) {
	t.Helper()
	body := fmt.Sprintf(`{
  "version": 1,
  "project": "workflow-proj",
  "repo_id": "github.com/AGOrcha/dot-agents",
  "observability": {
    "enabled": %t,
    "endpoint": %q,
    "push_throttle_seconds": %d
  }
}
`, enabled, endpoint, throttle)
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readyObservabilityRecords(t *testing.T, repo string) []observabilityOutboxRecord {
	t.Helper()
	dir := filepath.Join(repo, ".agents", "active", "obs-outbox")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var records []observabilityOutboxRecord
	for _, entry := range entries {
		if rec, ok := readyObservabilityRecord(t, dir, entry); ok {
			records = append(records, rec)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records
}

// readyObservabilityRecord parses one outbox dir entry, returning (record, true)
// for a valid `.obs-v1.json` ready file and (zero, false) for a non-match.
// Extracted from readyObservabilityRecords to keep both under the S3776 budget.
func readyObservabilityRecord(t *testing.T, dir string, entry os.DirEntry) (observabilityOutboxRecord, bool) {
	t.Helper()
	var zero observabilityOutboxRecord
	if strings.HasPrefix(entry.Name(), ".") {
		t.Fatalf("outbox contains temporary file %q", entry.Name())
	}
	if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".obs-v1.json") {
		return zero, false
	}
	info, err := entry.Info()
	if err != nil {
		t.Fatal(err)
	}
	// Windows chmod only sets the read-only bit (0600 renders as 0666), so the
	// owner-only perm assertion is Unix-only (leverage-cross-platform-fs-helpers).
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("ready file %s mode = %o, want 600", entry.Name(), info.Mode().Perm())
	}
	raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
	if err != nil {
		t.Fatal(err)
	}
	var record observabilityOutboxRecord
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		t.Fatalf("parse %s: %v", entry.Name(), err)
	}
	if record.ID+".obs-v1.json" != entry.Name() {
		t.Fatalf("record id %q does not match filename %q", record.ID, entry.Name())
	}
	wantHash, err := hashObservabilityEvent(record.Event)
	if err != nil {
		t.Fatal(err)
	}
	if record.Event.SchemaHash != wantHash {
		t.Fatalf("schema_hash = %q, want %q", record.Event.SchemaHash, wantHash)
	}
	return record, true
}

func setupObservabilityCheckpoint(t *testing.T, enabled bool, endpoint string, throttle int) string {
	t.Helper()
	repo := initWorkflowTestRepoWithCommit(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	saveTestDelegationContract(t, repo, "task-obs", "plan-obs", "del-obs")
	writeObservabilityHookConfig(t, repo, enabled, endpoint, throttle)
	if err := runWorkflowCheckpointLogToIter(1, "impl", ""); err != nil {
		t.Fatalf("checkpoint --log-to-iter: %v", err)
	}
	return repo
}

func TestCheckpointQueuesOneCrashSafeEvent(t *testing.T) {
	repo := setupObservabilityCheckpoint(t, true, "http://127.0.0.1:8787", 3600)
	records := readyObservabilityRecords(t, repo)
	if len(records) != 1 {
		t.Fatalf("ready records = %d, want 1", len(records))
	}
	record := records[0]
	if record.OutboxVersion != 1 || record.Attempts != 0 || record.LastError != nil {
		t.Fatalf("retry envelope = %+v", record)
	}
	if record.Event.Kind != "iteration.checkpointed" || record.Event.PlanID != "plan-obs" || record.Event.TaskID != "task-obs" || record.Event.Iteration != 1 {
		t.Fatalf("checkpoint event = %+v", record.Event)
	}
	if string(record.Event.ScoreSidecar) != "null" {
		t.Fatalf("checkpoint score_sidecar = %s, want null", record.Event.ScoreSidecar)
	}
}

func TestVerifyRecordRepublishesCanonicalCheckpointIdempotently(t *testing.T) {
	repo := setupObservabilityCheckpoint(t, true, "http://127.0.0.1:8787", 3600)
	if err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind: "test", Status: "pass", Scope: "repo", Summary: "focused tests pass",
	}); err != nil {
		t.Fatalf("verify record: %v", err)
	}
	records := readyObservabilityRecords(t, repo)
	if len(records) != 2 {
		t.Fatalf("ready records = %d, want 2", len(records))
	}
	if records[0].Event.Kind != "iteration.checkpointed" || records[1].Event.Kind != "iteration.checkpointed" {
		t.Fatalf("verify republish kinds = %q, %q", records[0].Event.Kind, records[1].Event.Kind)
	}
	if records[0].Event.SchemaHash != records[1].Event.SchemaHash {
		t.Fatalf("verify republish changed canonical hash: %s != %s", records[0].Event.SchemaHash, records[1].Event.SchemaHash)
	}
}

func TestVerifyRecordRepublishesExistingScoreWithoutInventingRecompute(t *testing.T) {
	repo := setupObservabilityCheckpoint(t, true, "http://127.0.0.1:8787", 3600)
	score := `iteration: 1
rubric_version: 3.0.0
scored: true
value: 0.9
band: excellent
breakdown: []
`
	if err := os.WriteFile(scoreSidecarPath(repo, 1), []byte(score), 0o644); err != nil {
		t.Fatal(err)
	}
	publishScoreBestEffort(repo, 1)
	if err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind: "test", Status: "pass", Scope: "repo", Summary: "score remains current",
	}); err != nil {
		t.Fatalf("verify record: %v", err)
	}
	records := readyObservabilityRecords(t, repo)
	if len(records) != 3 {
		t.Fatalf("ready records = %d, want 3", len(records))
	}
	for _, record := range records[1:] {
		if record.Event.Kind != "iteration.scored" {
			t.Fatalf("verify score republish kind = %q, want iteration.scored", record.Event.Kind)
		}
	}
	if records[1].Event.SchemaHash != records[2].Event.SchemaHash {
		t.Fatalf("verify score republish changed canonical hash: %s != %s", records[1].Event.SchemaHash, records[2].Event.SchemaHash)
	}
}

func TestPublishFailureNeverChangesCheckpointOrVerifyResult(t *testing.T) {
	t.Setenv("DA_OBS_TEST_JWT", "fixture.jwt")
	repo := setupObservabilityCheckpoint(t, true, "http://127.0.0.1:1", 0)
	if err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind: "test", Status: "pass", Scope: "repo", Summary: "local result wins",
	}); err != nil {
		t.Fatalf("verify record changed result after publish failure: %v", err)
	}
	records := readyObservabilityRecords(t, repo)
	if len(records) != 2 {
		t.Fatalf("retained ready records = %d, want 2", len(records))
	}
	for _, record := range records {
		if record.Attempts < 1 || record.LastError == nil {
			t.Fatalf("unreachable endpoint did not retain retry metadata: %+v", record)
		}
	}
}

func TestDisabledObservabilityLeavesNoOutbox(t *testing.T) {
	repo := setupObservabilityCheckpoint(t, false, "http://127.0.0.1:8787", 0)
	if records := readyObservabilityRecords(t, repo); len(records) != 0 {
		t.Fatalf("disabled observability queued %d records", len(records))
	}
}

func TestCloseTaskQueuesCheckpointAndFirstScore(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	saveTestDelegationContract(t, repo, taskID, planID, "del-close-task")
	writeObservabilityHookConfig(t, repo, true, "http://127.0.0.1:8787", 3600)
	if err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current", noCommit: true, repoDir: repo,
	}); err != nil {
		t.Fatalf("close-task: %v", err)
	}
	records := readyObservabilityRecords(t, repo)
	if len(records) != 2 {
		t.Fatalf("close-task ready records = %d, want checkpoint + score", len(records))
	}
	kinds := []string{records[0].Event.Kind, records[1].Event.Kind}
	sort.Strings(kinds)
	if strings.Join(kinds, ",") != "iteration.checkpointed,iteration.scored" {
		t.Fatalf("close-task kinds = %v", kinds)
	}
	if _, err := time.Parse(time.RFC3339, records[0].Event.OccurredAt); err != nil {
		t.Fatalf("occurred_at is not RFC3339: %q", records[0].Event.OccurredAt)
	}
}
