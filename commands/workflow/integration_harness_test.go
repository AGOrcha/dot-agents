package workflow

// Cobra-integration tests for the workflow command tree. These exercise the
// JSON-output branches, error returns, and rare success cases of the
// `run*` handlers in delegation.go / plan_task.go / prefs.go / state.go that
// are otherwise reachable only via the cobra surface. Tests rely on the
// existing harness helpers (setupTestProject, executeWorkflowCommand,
// workflowTestJSON, captureStdoutWhileRunning, withYamlMarshalStub, etc.).

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── runWorkflowTasks / runWorkflowSlices JSON paths ─────────────────────────

func TestRunWorkflowTasks_JSON(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowTasks("plan-001")
	}, `"plan_id"`, `"task-001"`)
}

func TestRunWorkflowTasks_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowTasks("ghost-plan")
	if err == nil || !strings.Contains(err.Error(), "ghost-plan") {
		t.Fatalf("expected plan-not-found error, got %v", err)
	}
}

func TestRunWorkflowTasks_TasksMissingErrors(t *testing.T) {
	repo := setupTestProject(t)
	// remove TASKS.yaml but keep PLAN.yaml
	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	err := runWorkflowTasks("plan-001")
	if err == nil || !strings.Contains(err.Error(), "plan-001") {
		t.Fatalf("expected tasks-not-found error, got %v", err)
	}
}

func TestRunWorkflowSlices_JSON(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowSlices("p1")
	}, `"plan_id"`, `"s1"`)
}

func TestRunWorkflowSlices_PlanNotFound(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	chdirRepo(t, repo)
	err := runWorkflowSlices("ghost-plan")
	if err == nil || !strings.Contains(err.Error(), "ghost-plan") {
		t.Fatalf("expected plan-not-found error, got %v", err)
	}
}

func TestRunWorkflowSlices_SlicesMissingErrors(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowSlices("plan-001")
	if err == nil || !strings.Contains(err.Error(), "p") {
		t.Fatalf("expected slices-not-found error, got %v", err)
	}
}

// ── runWorkflowPlanShow JSON / error ────────────────────────────────────────

func TestRunWorkflowPlanShow_JSON_WithSlices(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanShow("p1")
	}, `"plan"`, `"slices"`)
}

func TestRunWorkflowPlanShow_NoTasksFile(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	// Render path: no TASKS.yaml branch with "(no TASKS.yaml found)" message.
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanShow("plan-001")
	}, "no ")
}

// ── runWorkflowPlanList JSON path ──────────────────────────────────────────

func TestRunWorkflowPlanList_JSON_HasPlans(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanList()
	}, `"p1"`)
}

// ── runWorkflowPlanSchedule JSON path ──────────────────────────────────────

func TestRunWorkflowPlanSchedule_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanSchedule("wave-2")
	}, `"waves"`, `"t1"`)
}

// ── runWorkflowPlanDeriveScope JSON + warnings paths ───────────────────────

func TestRunWorkflowPlanDeriveScope_JSON(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanDeriveScope("plan-001", "task-001", []string{"Sym"}, []string{"path/"})
	}, `"plan_id"`, `"task_id"`)
}

func TestRunWorkflowPlanDeriveScope_PersistError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })
	err := runWorkflowPlanDeriveScope("plan-001", "task-001", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestRunWorkflowPlanDeriveScope_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowPlanDeriveScope("plan-001", "ghost-task", nil, nil)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

// ── runWorkflowFanout error paths ──────────────────────────────────────────

func TestFanout_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "no-such-plan", "--task", "task-001", "--owner", "w")
	if err == nil || !strings.Contains(err.Error(), "plan no-such-plan not found") {
		t.Fatalf("expected plan-not-found, got %v", err)
	}
}

func TestFanout_ExistingDelegationRejected(t *testing.T) {
	repo := setupTestProject(t)
	// pre-create a contract for task-001
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-existing")

	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil || !strings.Contains(err.Error(), "already has an active delegation") {
		t.Fatalf("expected already-delegated error, got %v", err)
	}
}

func TestFanout_TaskNotFoundInPlan(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "ghost-task", "--owner", "w")
	if err == nil {
		t.Fatal("expected task-not-found error")
	}
}

// ── runWorkflowMergeBack error paths ───────────────────────────────────────

func TestMergeBack_NoDelegationContractFails(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "merge-back", "--task", "task-001", "--summary", "x", "--verification-status", "pass")
	if err == nil || !strings.Contains(err.Error(), "delegation contract for task task-001") {
		t.Fatalf("expected missing-contract error, got %v", err)
	}
}

func TestMergeBack_RefusesCompletedContract(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	// Move contract to completed status directly.
	c, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	c.Status = "completed"
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}
	err = executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "x", "--verification-status", "pass")
	if err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("expected already-completed error, got %v", err)
	}
}

// ── runWorkflowDelegationCloseout ──────────────────────────────────────────

func TestDelegationCloseout_InvalidDecision(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "delegation", "closeout",
		"--plan", "p1", "--task", "t1", "--decision", "maybe")
	if err == nil || !strings.Contains(err.Error(), "accept") {
		t.Fatalf("expected accept/reject error, got %v", err)
	}
}

func TestDelegationCloseout_NoMergeBack(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	// No merge-back written.
	err := executeWorkflowCommand(t, repo, "delegation", "closeout",
		"--plan", "p1", "--task", "t1", "--decision", "accept")
	if err == nil || !strings.Contains(err.Error(), "merge-back") {
		t.Fatalf("expected merge-back-required error, got %v", err)
	}
}

func TestDelegationCloseout_JSON(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "done", "--verification-status", "pass"); err != nil {
		t.Fatal(err)
	}

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		oldwd, _ := os.Getwd()
		defer os.Chdir(oldwd)
		if err := os.Chdir(repo); err != nil {
			return err
		}
		cmd := NewCmdForTest()
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs([]string{"delegation", "closeout", "--plan", "p1", "--task", "t1", "--decision", "accept"})
		return cmd.Execute()
	}, `"decision"`, `"accept"`)
}

// ── runWorkflowFoldBackUpsert error paths ──────────────────────────────────

func TestFoldBackCreate_PlanNotFound(t *testing.T) {
	repo := setupFoldBackProject(t)
	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "no-such", "--observation", "x")
	if err == nil || !strings.Contains(err.Error(), "no-such") {
		t.Fatalf("expected plan-not-found, got %v", err)
	}
}

func TestFoldBackCreate_JSON(t *testing.T) {
	repo := setupFoldBackProject(t)
	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	out := executeWorkflowCommandOutput(t, repo, "fold-back",
		"create", "--plan", "p1", "--task", "t1", "--observation", "test obs")
	if !strings.Contains(out, `"plan_id"`) || !strings.Contains(out, `"observation"`) {
		t.Fatalf("missing JSON fields: %s", out)
	}
}

// ── runWorkflowFoldBackList ────────────────────────────────────────────────

func TestFoldBackList_RendersHuman(t *testing.T) {
	repo := setupFoldBackProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "x"); err != nil {
		t.Fatal(err)
	}
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, "p1") {
		t.Fatalf("expected p1 in human render: %s", out)
	}
}

func TestFoldBackList_NoArtifactsHuman(t *testing.T) {
	repo := setupTestProject(t)
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, "No fold-back observations") {
		t.Fatalf("expected empty list message, got %s", out)
	}
}

// ── runWorkflowPrefs JSON path ─────────────────────────────────────────────

func TestRunWorkflowPrefs_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPrefs() },
		`"verification"`)
}

// ── runWorkflowAdvance error path ──────────────────────────────────────────

func TestRunWorkflowAdvance_InvalidStatus(t *testing.T) {
	err := runWorkflowAdvance("plan-001", "task-001", "weird")
	if err == nil || !strings.Contains(err.Error(), "invalid task status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}

func TestRunWorkflowAdvance_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowAdvance("ghost", "task-001", "in_progress")
	if err == nil {
		t.Fatal("expected error for ghost plan")
	}
}

func TestRunWorkflowAdvance_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowAdvance("plan-001", "ghost-task", "in_progress")
	if err == nil || !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

// ── runWorkflowEligible JSON ───────────────────────────────────────────────

func TestRunWorkflowEligible_JSON_WithFilter(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowEligible("wave-2", 0)
	}, `"eligible_tasks"`)
}

// ── runWorkflowComplete: empty plan param error ───────────────────────────

func TestRunWorkflowComplete_EmptyPlanRejected(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowComplete("")
	if err == nil || !strings.Contains(err.Error(), "--plan must not be empty") {
		t.Fatalf("expected --plan must not be empty, got %v", err)
	}
}

// ── runWorkflowCheckpoint error paths ─────────────────────────────────────

func TestRunWorkflowCheckpoint_InvalidVerificationStatus(t *testing.T) {
	err := runWorkflowCheckpoint("msg", "garbage", "summary")
	if err == nil || !strings.Contains(err.Error(), "invalid verification status") {
		t.Fatalf("expected invalid verification status, got %v", err)
	}
}

// (TestRunWorkflowCheckpoint_MkdirError / WriteError already covered by
// seams_test.go; we only add marshal + append-log seam variants below.)

// ── runWorkflowTaskAdd / runWorkflowTaskUpdate error paths ─────────────────

func TestRunWorkflowTaskAdd_DuplicateID(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001",
		TaskID: "task-001",
		Title:  "dup",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestRunWorkflowTaskAdd_PlanMissingTasksFile(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	err := runWorkflowTaskAdd(taskAddInputs{PlanID: "plan-001", TaskID: "tnew", Title: "x"})
	if err == nil {
		t.Fatal("expected tasks-file-missing error")
	}
}

func TestRunWorkflowTaskUpdate_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowTaskUpdate("plan-001", "ghost-task", "T", "N", "")
	if err == nil || !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

func TestRunWorkflowTaskUpdate_PlanMissing(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowTaskUpdate("ghost-plan", "task-001", "T", "N", "")
	if err == nil {
		t.Fatal("expected plan-not-found error")
	}
}

// ── prefs JSON output and propagation error paths ─────────────────────────

func TestRunWorkflowPrefsSetShared_PropagatesProposalSaveError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)
	// Force AGENTS_HOME to a path where SaveProposal must mkdir but the
	// parent is a file (so it fails).
	tmp := t.TempDir()
	blockerFile := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blockerFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", blockerFile)

	err := runWorkflowPrefsSetShared("verification.test_command", "go test ./...")
	if err == nil {
		t.Fatal("expected save proposal error")
	}
}

// ── runWorkflowPlanCheckScope error paths ──────────────────────────────────

func TestRunWorkflowPlanCheckScope_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	withOsExitStub(t)
	err := runWorkflowPlanCheckScope("ghost-plan", "task-001", []string{"x.go"}, false)
	if err == nil {
		t.Fatal("expected error for missing plan")
	}
}

func TestRunWorkflowPlanCheckScope_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	withOsExitStub(t)
	err := runWorkflowPlanCheckScope("plan-001", "ghost-task", []string{"x.go"}, false)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

// ── runWorkflowPlanCreate error path ───────────────────────────────────────

func TestRunWorkflowPlanCreate_DuplicatePlanFails(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowPlanCreate("plan-001", "T", "S", "O", "SC", "VS")
	if err == nil {
		t.Fatal("expected duplicate plan error")
	}
}

// ── runWorkflowEligible: malformed prefs propagates ────────────────────────

func TestRunWorkflowEligible_PrefsParseError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	// write malformed preferences.yaml so resolvePreferences fails.
	prefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefsDir, "preferences.yaml"),
		[]byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	err := runWorkflowEligible("", 0)
	if err == nil {
		t.Fatal("expected prefs parse error to propagate")
	}
}

// ── resolvePreferencesWithSources / resolvePreferences error propagation ──

func TestResolvePreferences_RepoParseError(t *testing.T) {
	repo := t.TempDir()
	prefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefsDir, "preferences.yaml"),
		[]byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePreferences(repo, "any-proj"); err == nil {
		t.Fatal("expected repo parse error")
	}
	if _, err := resolvePreferencesWithSources(repo, "any-proj"); err == nil {
		t.Fatal("expected repo parse error from sources")
	}
}

func TestResolvePreferences_LocalParseError(t *testing.T) {
	repo := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	dir := filepath.Join(agentsHome, "context", "broken-proj")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preferences.local.yaml"),
		[]byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePreferences(repo, "broken-proj"); err == nil {
		t.Fatal("expected local parse error")
	}
	if _, err := resolvePreferencesWithSources(repo, "broken-proj"); err == nil {
		t.Fatal("expected local parse error from sources")
	}
}

// ── setLocalPreference error paths ─────────────────────────────────────────

func TestSetLocalPreference_ParseExistingError(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	dir := filepath.Join(agentsHome, "context", "broken-proj")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preferences.local.yaml"),
		[]byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := setLocalPreference("broken-proj", "verification.test_command", "go test"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSetLocalPreference_WriteFails(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	sentinel := errors.New("marshal boom")
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, sentinel }
	t.Cleanup(func() { yamlMarshal = prev })

	if err := setLocalPreference("p", "verification.test_command", "x"); !errors.Is(err, sentinel) {
		t.Fatalf("expected marshal sentinel, got %v", err)
	}
}

// ── verification: invalid verifier-type/status surface via verify record ───

func TestVerifyRecord_InvalidStatusRejected(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "test",
		"--status", "bogus-status",
		"--summary", "x",
	)
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}

// ── runWorkflowMergeBack JSON-free body still writes verification artifact ─

func TestMergeBack_WritesContractCompletion(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1",
		"--summary", "shipped", "--verification-status", "pass",
		"--integration-notes", "no conflicts"); err != nil {
		t.Fatal(err)
	}
	c, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "completed" {
		t.Fatalf("expected delegation completed, got %q", c.Status)
	}
}

// ── runWorkflowFanout: persist artifacts error via seam ────────────────────

func TestFanout_PersistArtifactsWriteError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	// Inject write error: triggers contract or bundle save failure inside
	// persistFanoutArtifacts.
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected write-fault error")
	}
	_ = sentinel
}

// ── runWorkflowFanout: scope conflict path ────────────────────────────────

func TestFanout_WriteScopeConflictRejected(t *testing.T) {
	repo := setupFanoutTwoTaskProject(t)
	// First fanout uses commands/.
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--task", "t1", "--owner", "a", "--write-scope", "commands/"); err != nil {
		t.Fatal(err)
	}
	// Second fanout claims the same scope — should fail conflict check.
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--task", "t2", "--owner", "b", "--write-scope", "commands/")
	if err == nil {
		t.Fatal("expected write-scope conflict error")
	}
}

// ── runWorkflowFoldBackUpsert: invalid prior validation ───────────────────

func TestFoldBackCreate_RewritesFromBlocks(t *testing.T) {
	repo := setupFoldBackProject(t)
	// Use --slug + --plan/--task to exercise the priorExists/no-prior validate path.
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--slug", "first-slug", "--observation", "v1"); err != nil {
		t.Fatal(err)
	}
	// Same slug, plan-only (no --task) — should be rejected (task-scoped requires --task).
	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--slug", "first-slug", "--observation", "v2")
	if err == nil {
		t.Fatal("expected prior-validation error for task-scoped slug without --task")
	}
}

// ── runWorkflowFanout: bundle escape path ─────────────────────────────────

func TestFanout_PromptBundleAcceptsRelativePath(t *testing.T) {
	repo := setupTestProject(t)
	promptPath := filepath.Join(repo, ".agents", "prompts")
	if err := os.MkdirAll(promptPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptPath, "p.md"), []byte("# p\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--prompt-file", ".agents/prompts/p.md",
	); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

// ── runWorkflowComplete: collectWorkflowCompletionState error ─────────────

func TestRunWorkflowComplete_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowComplete("ghost-plan")
	if err == nil {
		t.Fatal("expected error for ghost plan")
	}
}

// ── runWorkflowCheckpoint: appendWorkflowSessionLog seam error ────────────

func TestRunWorkflowCheckpoint_AppendLogOpenError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)

	sentinel := errors.New("open boom")
	prev := osOpenFile
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, sentinel }
	t.Cleanup(func() { osOpenFile = prev })

	err := runWorkflowCheckpoint("msg", "pass", "summary")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected open sentinel, got %v", err)
	}
}

// ── runWorkflowCheckpoint: marshal error propagates ───────────────────────

func TestRunWorkflowCheckpoint_MarshalError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)

	sentinel := errors.New("marshal boom")
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, sentinel }
	t.Cleanup(func() { yamlMarshal = prev })

	err := runWorkflowCheckpoint("msg", "pass", "summary")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected marshal sentinel, got %v", err)
	}
}

// ── runWorkflowMergeBack save-error via stub ──────────────────────────────

func TestMergeBack_SaveError(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := executeWorkflowCommand(t, repo, "merge-back",
		"--task", "t1", "--summary", "done", "--verification-status", "pass")
	if err == nil {
		t.Fatal("expected save error")
	}
}

// ── runWorkflowDelegationCloseout: applyCloseoutDecisionToTasks error ─────

func TestDelegationCloseout_RejectWithoutNote(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "tried", "--verification-status", "fail"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "delegation", "closeout",
		"--plan", "p1", "--task", "t1", "--decision", "reject"); err != nil {
		t.Fatalf("expected successful reject closeout, got %v", err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if tf.Tasks[0].Status != "blocked" {
		t.Fatalf("expected task blocked, got %q", tf.Tasks[0].Status)
	}
}

// ── runWorkflowPlanList: no plans renders empty hint ──────────────────────

func TestRunWorkflowPlanList_EmptyHint(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanList() },
		"No canonical plans found")
}

// ── runWorkflowPlanShow: plan missing error ──────────────────────────────

func TestRunWorkflowPlanShow_PlanMissing(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowPlanShow("ghost-plan")
	if err == nil || !strings.Contains(err.Error(), "ghost-plan") {
		t.Fatalf("expected plan-not-found, got %v", err)
	}
}

// ── runWorkflowFoldBackUpsert: writeFoldBackArtifact error ────────────────

func TestFoldBackCreate_WriteError(t *testing.T) {
	repo := setupFoldBackProject(t)
	chdirRepo(t, repo)
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "x")
	if err == nil {
		t.Fatal("expected write fault")
	}
}

// ── runWorkflowFanout: ensureTaskVerificationDir failure via mkdir stub ───

func TestFanout_MkdirVerifyDirError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	// Allow the contract/bundle dirs to be created, fail only on
	// the verification dir mkdir. We approximate by failing every mkdir.
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected mkdir fault")
	}
}

// ── time-sensitive sanity: ensures harness contract IDs are stable ────────

func TestSaveTestDelegationContract_StableID(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "tx", "px", "del-tx-fixed")
	c, err := loadDelegationContract(repo, "tx")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "del-tx-fixed" {
		t.Fatalf("contract id = %q, want del-tx-fixed", c.ID)
	}
	if time.Since(mustParseRFC3339(t, c.UpdatedAt)) > time.Minute {
		t.Fatalf("UpdatedAt should be recent, got %q", c.UpdatedAt)
	}
}

func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// ── small JSON serialization sanity (encode->decode round-trip) ───────────

func TestEligibleOutput_JSONRoundTrip(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{},
		MaxBatch:      []string{},
		ConflictGraph: map[string][]string{},
		DraftPlans:    []string{},
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded eligibleOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.EligibleTasks) != 0 {
		t.Fatalf("decoded mismatch: %+v", decoded)
	}
}
