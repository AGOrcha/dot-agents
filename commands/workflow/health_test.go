package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// TestComputeWorkflowHealth_CompletedPlansPendingArchive verifies the new field is
// populated from LocalDrift and that status thresholds are unaffected.
func TestComputeWorkflowHealth_CompletedPlansPendingArchive(t *testing.T) {
	t.Run("positive: count reflects LocalDrift.CompletedPlanIDs", func(t *testing.T) {
		state := &workflowOrientState{
			Git: workflowGitSummary{Branch: "main"},
			LocalDrift: &RepoDriftReport{
				CompletedPlanIDs: []string{"plan-a", "plan-b"},
			},
		}
		h := computeWorkflowHealth(state)
		if h.Workflow.CompletedPlansPendingArchive != 2 {
			t.Errorf("CompletedPlansPendingArchive = %d, want 2", h.Workflow.CompletedPlansPendingArchive)
		}
		// Status must not change — informational only
		if h.Status == "partial" || h.Status == "degraded" {
			t.Errorf("status changed to %q due to pending archive count; should not affect thresholds", h.Status)
		}
	})

	t.Run("negative: zero when LocalDrift is nil", func(t *testing.T) {
		state := &workflowOrientState{
			Git:        workflowGitSummary{Branch: "main"},
			LocalDrift: nil,
		}
		h := computeWorkflowHealth(state)
		if h.Workflow.CompletedPlansPendingArchive != 0 {
			t.Errorf("CompletedPlansPendingArchive = %d, want 0 when no drift", h.Workflow.CompletedPlansPendingArchive)
		}
	})

	t.Run("json: field present in marshaled output", func(t *testing.T) {
		state := &workflowOrientState{
			Git: workflowGitSummary{Branch: "main"},
			LocalDrift: &RepoDriftReport{
				CompletedPlanIDs: []string{"plan-x"},
			},
		}
		h := computeWorkflowHealth(state)
		data, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"completed_plans_pending_archive":1`) {
			t.Errorf("JSON output missing completed_plans_pending_archive:1, got: %s", string(data))
		}
	})
}

// TestComputeWorkflowHealth_HealthyState exercises the all-green branch:
// zero dirty files, no proposals, and a checkpoint present.
func TestComputeWorkflowHealth_HealthyState(t *testing.T) {
	state := &workflowOrientState{
		Git:        workflowGitSummary{Branch: "main", DirtyFileCount: 0},
		Checkpoint: &workflowCheckpoint{},
		Proposals:  workflowProposalSummary{PendingCount: 0},
	}
	h := computeWorkflowHealth(state)
	if h.Status != "healthy" {
		t.Fatalf("status = %q, want healthy", h.Status)
	}
	if len(h.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", h.Warnings)
	}
	if !h.Git.InsideRepo {
		t.Fatal("expected InsideRepo true when branch != 'unknown'")
	}
	if h.Workflow.HasCheckpoint != true {
		t.Fatal("expected HasCheckpoint true")
	}
}

// TestComputeWorkflowHealth_WarnsOnExcessDirtyFiles covers the >20 dirty files
// warning branch.
func TestComputeWorkflowHealth_WarnsOnExcessDirtyFiles(t *testing.T) {
	state := &workflowOrientState{
		Git:        workflowGitSummary{Branch: "main", DirtyFileCount: 25},
		Checkpoint: &workflowCheckpoint{},
	}
	h := computeWorkflowHealth(state)
	if h.Status != "warn" {
		t.Fatalf("status = %q, want warn", h.Status)
	}
	found := false
	for _, w := range h.Warnings {
		if strings.Contains(w, "25 dirty files") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dirty-files warning in %v", h.Warnings)
	}
}

// TestComputeWorkflowHealth_WarnsOnPendingProposalsAndMissingCheckpoint
// covers two independent warn-triggers in the same snapshot.
func TestComputeWorkflowHealth_WarnsOnPendingProposalsAndMissingCheckpoint(t *testing.T) {
	state := &workflowOrientState{
		Git:       workflowGitSummary{Branch: "main"},
		Proposals: workflowProposalSummary{PendingCount: 3},
	}
	h := computeWorkflowHealth(state)
	if h.Status != "warn" {
		t.Fatalf("status = %q, want warn", h.Status)
	}
	joined := strings.Join(h.Warnings, "|")
	if !strings.Contains(joined, "3 pending proposal") {
		t.Errorf("expected pending-proposals warning, got %v", h.Warnings)
	}
	if !strings.Contains(joined, "no checkpoint recorded") {
		t.Errorf("expected no-checkpoint warning, got %v", h.Warnings)
	}
}

// TestComputeWorkflowHealth_UnknownBranchMarksOutsideRepo verifies the
// inside_repo flag flips when git did not resolve a branch.
func TestComputeWorkflowHealth_UnknownBranchMarksOutsideRepo(t *testing.T) {
	state := &workflowOrientState{
		Git:        workflowGitSummary{Branch: "unknown"},
		Checkpoint: &workflowCheckpoint{},
	}
	h := computeWorkflowHealth(state)
	if h.Git.InsideRepo {
		t.Fatal("expected InsideRepo false when branch is 'unknown'")
	}
}

// TestHealthSnapshot_WriteThenRead_RoundTrip verifies the persisted health.json
// schema round-trips faithfully.
func TestHealthSnapshot_WriteThenRead_RoundTrip(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	snap := WorkflowHealthSnapshot{
		SchemaVersion: 1,
		Timestamp:     "2026-04-10T10:00:00Z",
		Status:        "warn",
		Warnings:      []string{"x", "y"},
	}
	snap.Git.Branch = "main"
	snap.Git.DirtyFileCount = 4
	snap.Workflow.HasCheckpoint = true

	if err := writeHealthSnapshot("rt-proj", snap); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config.ProjectContextDir("rt-proj"), "health.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected health.json on disk: %v", err)
	}

	loaded, err := readHealthSnapshot("rt-proj")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if loaded.Status != "warn" || loaded.Git.Branch != "main" || loaded.Git.DirtyFileCount != 4 {
		t.Fatalf("round-trip mismatch: %+v", loaded)
	}
	if len(loaded.Warnings) != 2 {
		t.Fatalf("warnings len = %d, want 2", len(loaded.Warnings))
	}
}

// TestReadHealthSnapshot_MissingReturnsNil ensures absence is not an error.
func TestReadHealthSnapshot_MissingReturnsNil(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	snap, err := readHealthSnapshot("never-existed")
	if err != nil {
		t.Fatal(err)
	}
	if snap != nil {
		t.Fatalf("expected nil snapshot, got %+v", snap)
	}
}

// TestRunWorkflowHealth_PersistsSnapshotAndRenders covers the full command path:
// it must compute, persist, and render the snapshot.
func TestRunWorkflowHealth_PersistsSnapshotAndRenders(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowHealth() },
		"Workflow Health",
		"status:",
		"Git",
		"branch:",
	)

	path := filepath.Join(config.ProjectContextDir("workflow-proj"), "health.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected health snapshot persisted at %s: %v", path, err)
	}
	var snap WorkflowHealthSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("persisted health.json should be valid JSON: %v", err)
	}
	if snap.SchemaVersion != 1 {
		t.Fatalf("snapshot SchemaVersion = %d, want 1", snap.SchemaVersion)
	}
}

func TestWriteHealthSnapshot_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	t.Setenv("HOME", tmp)
	h := WorkflowHealthSnapshot{
		SchemaVersion: 1,
		Status:        "healthy",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Warnings:      []string{},
	}
	if err := writeHealthSnapshot("proj-x", h); err != nil {
		t.Fatalf("writeHealthSnapshot: %v", err)
	}
	got, err := readHealthSnapshot("proj-x")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != "healthy" {
		t.Errorf("readHealthSnapshot mismatch: %+v", got)
	}
}

func TestRunWorkflowHealth_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowHealth() })
	if err != nil {
		t.Fatalf("runWorkflowHealth: %v", err)
	}
	if !strings.Contains(out, "\"status\":") {
		t.Errorf("expected JSON status field, got: %s", out)
	}
}

func TestReadHealthSnapshot_Missing(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	h, err := readHealthSnapshot("p")
	if err != nil {
		t.Fatal(err)
	}
	if h != nil {
		t.Fatalf("expected nil snapshot, got %+v", h)
	}
}

func TestReadHealthSnapshot_MalformedJSON(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctx, "health.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := readHealthSnapshot("p")
	if err == nil {
		t.Fatal("expected json error")
	}
}

func TestReadHealthSnapshot_ReadError(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ctx, "health.json")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, path)
	_, err := readHealthSnapshot("p")
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestRunWorkflowHealth_JSON_FromRepo(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowHealth() },
		`"status"`)
}

// TestRunWorkflowHealth_GetwdErrorPropagates drives the `if err != nil` branch
// in runWorkflowHealth (health.go:87-89) by faulting the osGetwd seam so that
// collectWorkflowState fails. The error must propagate verbatim with no
// snapshot persisted.
func TestRunWorkflowHealth_GetwdErrorPropagates(t *testing.T) {
	sentinel := errSentinelHealthGetwd
	saved := osGetwd
	t.Cleanup(func() { osGetwd = saved })
	osGetwd = func() (string, error) { return "", sentinel }

	err := runWorkflowHealth()
	if err == nil {
		t.Fatal("expected getwd failure to propagate from collectWorkflowState")
	}
}

// TestRunWorkflowHealth_RendersCompletedPlansPendingArchive verifies the
// optional "completed plans pending archive: N" line in renderer
// (health.go:119-121) appears when LocalDrift reports completed-but-not-
// archived plans. We seed a repo with one PLAN.yaml whose status is
// "completed" and execute the human-mode handler.
func TestRunWorkflowHealth_RendersCompletedPlansPendingArchive(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", agentsHome)
	// Seed a completed plan that triggers driftPlanScanPhase to populate
	// LocalDrift.CompletedPlanIDs (and thus
	// health.Workflow.CompletedPlansPendingArchive > 0).
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "stale-plan")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), []byte("status: completed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowHealth() },
		"completed plans pending archive:")
}

// errSentinelHealthGetwd lets the getwd-error test assert error chain identity.
var errSentinelHealthGetwd = errorString("synthetic getwd failure for health test")
