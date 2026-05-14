package workflow

// Fifth batch: cobra Args validators, additional render-path tests, and
// reaches into low-coverage helpers via specific input fixtures.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Cobra Args validators reject wrong positional counts ───────────────────

func TestCobra_TasksRequiresOneArg(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "tasks")
	if err == nil {
		t.Fatal("expected missing-arg error for tasks")
	}
}

func TestCobra_TasksTooManyArgs(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "tasks", "a", "b")
	if err == nil {
		t.Fatal("expected too-many-args error for tasks")
	}
}

func TestCobra_SlicesRequiresOneArg(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "slices")
	if err == nil {
		t.Fatal("expected missing-arg error for slices")
	}
}

func TestCobra_AdvanceRequiresPlanArg(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "advance", "--task", "x", "--status", "in_progress")
	if err == nil {
		t.Fatal("expected missing-plan-arg error for advance")
	}
}

func TestCobra_PlanScheduleRequiresPlanArg(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "plan", "schedule")
	if err == nil {
		t.Fatal("expected missing-arg error for plan schedule")
	}
}

func TestCobra_PlanDeriveScopeRequiresTwoArgs(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "plan", "derive-scope", "plan-only")
	if err == nil {
		t.Fatal("expected missing-arg error for derive-scope")
	}
}

func TestCobra_PrefsSetLocalRequiresKVargs(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "prefs", "set-local", "key-only")
	if err == nil {
		t.Fatal("expected missing-arg error for set-local")
	}
}

func TestCobra_PrefsSetSharedRequiresKVargs(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "prefs", "set-shared")
	if err == nil {
		t.Fatal("expected missing-arg error for set-shared")
	}
}

// ── runWorkflowPlanArchive: error when invalid plan-id input ──────────────

func TestCobra_PlanArchive_RequiresAtLeastOneID(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "plan", "archive", "--plan", ",, ,")
	if err == nil || !strings.Contains(err.Error(), "at least one plan ID") {
		t.Fatalf("expected at-least-one-id error, got %v", err)
	}
}

func TestCobra_PlanArchive_MissingPlanFlag(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "plan", "archive")
	if err == nil {
		t.Fatal("expected missing --plan flag error")
	}
}

// ── runWorkflowComplete cobra: empty --plan rejected ─────────────────────

func TestCobra_CompleteRequiresPlan(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "complete", "--plan", "")
	if err == nil {
		t.Fatal("expected missing-plan error")
	}
}

// ── runWorkflowGraphQuery cobra: missing --intent ─────────────────────────

func TestCobra_GraphQueryMissingIntent(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "graph", "query")
	if err == nil || !strings.Contains(err.Error(), "intent") {
		t.Fatalf("expected intent-required error, got %v", err)
	}
}

// ── runWorkflowGraphQuery cobra: unknown intent ───────────────────────────

func TestCobra_GraphQueryUnknownIntent(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo)
	err := executeWorkflowCommand(t, repo, "graph", "query",
		"--intent", "totally-bogus", "x")
	if err == nil {
		t.Fatal("expected unknown-intent error")
	}
}

// ── ExtraFields: runWorkflowPlanGraph JSON path (mostly redundant) ────────

func TestRunWorkflowPlanGraph_NoPlanID_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanGraph("") },
		`"nodes"`)
}

// ── runWorkflowGraphQuery cobra: project resolution + bridge-config error ─

func TestRunWorkflowGraphQuery_BadBridgeConfig(t *testing.T) {
	repo := setupTestProject(t)
	bridgeDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(bridgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bridgeDir, "graph-bridge.yaml"),
		[]byte("not: valid: yaml:"), 0644); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "graph", "query",
		"--intent", "plan_context", "loop")
	if err == nil {
		t.Fatal("expected bridge config error")
	}
}

// (graph query via KG bridge: spawns subprocess — too slow for unit tests)

// ── plan show: corrupt SLICES.yaml triggers slicesErr branch ───────────────

func TestRunWorkflowPlanShow_CorruptSlices(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "p1", "SLICES.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	// Human render path should still succeed and tolerate the slice-error.
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanShow("p1")
	}, "p1")
}

// ── runWorkflowTaskAdd: plan save warn path ────────────────────────────────

func TestRunWorkflowTaskAdd_PlanReloadSkippedSoftError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	// Make PLAN.yaml unreadable AFTER tasks save by replacing the file
	// post-add. The plan reload inside runWorkflowTaskAdd uses _ = save so
	// soft errors are tolerated. Simulating exact race is brittle; this
	// test just confirms the happy path completes.
	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001", TaskID: "tt", Title: "T",
	}); err != nil {
		t.Fatal(err)
	}
}

// (drift/sweep happy paths covered by drift_sweep_test.go)

// ── runWorkflowBundle: cobra surface ──────────────────────────────────────

func TestCobra_BundleCmdSurfaceExists(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	c, _ := loadDelegationContract(repo, "t1")
	bundlePath := filepath.Join(repo, ".agents", "active", "delegation-bundles", c.ID+".yaml")
	// `bundle stages <path>` was already used in lifecycle_e2e_test; here
	// we just verify the cmd handles a missing path gracefully.
	err := executeWorkflowCommand(t, repo, "bundle", "stages", bundlePath+".does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing bundle")
	}
}

// ── runWorkflowCheckpoint: gitModifiedFiles fallback to empty when non-git

func TestRunWorkflowCheckpoint_NonGitDirectory(t *testing.T) {
	// Use a TempDir without git init so gitModifiedFiles returns an error
	// and the checkpoint falls through with empty modified list.
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"),
		[]byte(`{"project":"no-git","version":1,"sources":[{"type":"local"}]}`),
		0644); err != nil {
		t.Fatal(err)
	}
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowCheckpoint("msg", "pass", "sum"); err != nil {
		t.Fatalf("checkpoint should tolerate non-git: %v", err)
	}
}

// ── runWorkflowEligible: filter to a non-existent plan returns error ──────

func TestRunWorkflowEligible_FilterMatchesNoPlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)
	err := runWorkflowEligible("no-such-plan", 0)
	if err == nil {
		t.Fatal("expected plan-not-found error from filter")
	}
}

// ── runWorkflowGraphHealth: rendering green status ────────────────────────

func TestRunWorkflowGraphHealth_GreenStatus(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo)
	chdirRepo(t, repo)
	// Run health → produce a snapshot. Test passes if no panic.
	if err := runWorkflowGraphHealth(nil, nil); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

// (TestSaveCanonicalPlan_WriteError / Tasks_WriteError already in seams_test.go)

// ── runWorkflowFanout cobra: missing --plan ────────────────────────────────

func TestCobra_FanoutMissingPlanFlag(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "fanout", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected missing-plan error for fanout")
	}
}

func TestCobra_FanoutMissingTaskFlag(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected missing-task error for fanout")
	}
}

// ── runWorkflowMergeBack cobra: missing --task ────────────────────────────

func TestCobra_MergeBackMissingTaskFlag(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "merge-back", "--summary", "x")
	if err == nil {
		t.Fatal("expected missing-task error for merge-back")
	}
}

// ── runWorkflowDelegationCloseout cobra: missing --plan/--task/--decision

func TestCobra_DelegationCloseoutMissingFlags(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "delegation", "closeout")
	if err == nil {
		t.Fatal("expected required-flag error for delegation closeout")
	}
}

// ── runWorkflowFoldBackUpsert cobra: required flags ───────────────────────

func TestCobra_FoldBackCreateMissingPlan(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--observation", "x")
	if err == nil {
		t.Fatal("expected missing-plan for fold-back create")
	}
}

// ── runWorkflowPlanCheckScope cobra: required flags ───────────────────────

func TestCobra_PlanCheckScopeMissingArgs(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "plan", "check-scope")
	if err == nil {
		t.Fatal("expected required-arg error")
	}
}
