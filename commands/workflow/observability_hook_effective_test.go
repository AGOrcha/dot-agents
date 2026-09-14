package workflow

// Layer-awareness coverage for the workflow observability publication hook.
//
// publishIterationBestEffort admitted on the FLAT repo-local manifest, so a
// checkpoint in a repo whose `observability` block comes from an org/team
// layer queued nothing at all — the local iteration record landed, the event
// never existed, and nothing reported the gap because the hook is
// best-effort by design.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// setupLayeredObservabilityCheckpoint stands up a workflow repo whose
// observability configuration lives ONLY in an imported layer, resolves the
// lock so the read-only effective loader can replay it, and runs one
// checkpoint. Returns the repo path.
func setupLayeredObservabilityCheckpoint(t *testing.T) string {
	t.Helper()
	repo := initWorkflowTestRepoWithCommit(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	saveTestDelegationContract(t, repo, "task-obs", "plan-obs", "del-obs")

	layerRoot := t.TempDir()
	layerFile := filepath.Join(layerRoot, "org", "base.json")
	if err := os.MkdirAll(filepath.Dir(layerFile), 0o755); err != nil {
		t.Fatal(err)
	}
	layerBody := `{"observability":{"enabled":true,"endpoint":"http://127.0.0.1:8787","push_throttle_seconds":3600}}`
	if err := os.WriteFile(layerFile, []byte(layerBody), 0o644); err != nil {
		t.Fatal(err)
	}

	rootJSON, _ := json.Marshal(layerRoot)
	manifest := `{
  "version": 2,
  "project": "workflow-proj",
  "repo_id": "github.com/AGOrcha/dot-agents",
  "sources": [{"id": "org", "type": "local", "path": ` + string(rootJSON) + `}],
  "extends": ["org:org/base.json"]
}
`
	if err := os.WriteFile(filepath.Join(repo, config.AgentsRCFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.EnsureResolved(repo, config.EnsureOpts{}); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	if err := runWorkflowCheckpointLogToIter(1, "impl", ""); err != nil {
		t.Fatalf("checkpoint --log-to-iter: %v", err)
	}
	return repo
}

// TestCheckpointQueuesEventFromLayerSuppliedObservability is the publication
// half of the layered-consumer fix: nothing in the repo manifest mentions
// observability, yet the checkpoint must queue exactly one crash-safe event
// because the effective config enables it.
func TestCheckpointQueuesEventFromLayerSuppliedObservability(t *testing.T) {
	repo := setupLayeredObservabilityCheckpoint(t)

	records := readyObservabilityRecords(t, repo)
	if len(records) != 1 {
		t.Fatalf("ready records = %d, want 1 queued from the layer-supplied configuration", len(records))
	}
	if got := records[0].Event.Kind; got != "iteration.checkpointed" {
		t.Errorf("event kind = %q, want iteration.checkpointed", got)
	}
	if got := records[0].ProjectID; got != "github.com/AGOrcha/dot-agents" {
		t.Errorf("project id = %q, want the repo-local repo_id", got)
	}
}

// TestCheckpointQueuesNothingWhenNoLayerEnablesObservability pins the other
// direction: routing through the layer stack must not invent a configuration
// for a project that has none.
func TestCheckpointQueuesNothingWhenNoLayerEnablesObservability(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	saveTestDelegationContract(t, repo, "task-obs", "plan-obs", "del-obs")
	if err := os.WriteFile(filepath.Join(repo, config.AgentsRCFile),
		[]byte(`{"version":2,"project":"workflow-proj","repo_id":"github.com/AGOrcha/dot-agents"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runWorkflowCheckpointLogToIter(1, "impl", ""); err != nil {
		t.Fatalf("checkpoint --log-to-iter: %v", err)
	}
	if records := readyObservabilityRecords(t, repo); len(records) != 0 {
		t.Fatalf("ready records = %d, want none", len(records))
	}
}
