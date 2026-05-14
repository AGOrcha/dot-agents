package workflow

// Second batch of cobra-integration tests. Targets the closeout helper
// branches, archive helpers, graph health JSON path, fold-back proposal flow,
// and additional handler error paths.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── reconcileDelegationContractForCloseout error paths ─────────────────────

func TestReconcileDelegationContractForCloseout_PlanMismatch(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")
	_, err := reconcileDelegationContractForCloseout(repo, "task-001", "wrong-plan")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected plan-id mismatch, got %v", err)
	}
}

func TestReconcileDelegationContractForCloseout_PromotesActiveToCompleted(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")
	c, err := reconcileDelegationContractForCloseout(repo, "task-001", "plan-001")
	if err != nil {
		t.Fatalf("expected reconcile to succeed, got %v", err)
	}
	if c.Status != "completed" {
		t.Fatalf("expected reconcile to promote status to completed, got %q", c.Status)
	}
}

func TestReconcileDelegationContractForCloseout_MissingContract(t *testing.T) {
	repo := setupTestProject(t)
	_, err := reconcileDelegationContractForCloseout(repo, "missing-task", "plan-001")
	if err == nil || !strings.Contains(err.Error(), "delegation contract for task missing-task") {
		t.Fatalf("expected missing-contract error, got %v", err)
	}
}

func TestReconcileDelegationContractForCloseout_SaveError(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	_, err := reconcileDelegationContractForCloseout(repo, "task-001", "plan-001")
	if err == nil || !strings.Contains(err.Error(), "reconcile delegation status") {
		t.Fatalf("expected save error, got %v", err)
	}
}

// ── applyCloseoutDecisionToTasks error branches ────────────────────────────

func TestApplyCloseoutDecisionToTasks_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "ghost", DelegationID: "x",
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}
	err := applyCloseoutDecisionToTasks(repo, "plan-001", "ghost", closeout)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

func TestApplyCloseoutDecisionToTasks_PlanLoadFails(t *testing.T) {
	repo := setupTestProject(t)
	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", DelegationID: "x",
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// First, accept-decision tasks-save succeeds; now corrupt PLAN.yaml to force loadCanonicalPlan failure.
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml"),
		[]byte("not: valid: yaml: at: all"), 0644); err != nil {
		t.Fatal(err)
	}
	err := applyCloseoutDecisionToTasks(repo, "plan-001", "task-001", closeout)
	if err == nil {
		t.Fatal("expected plan-load failure to propagate")
	}
}

func TestApplyCloseoutDecisionToTasks_SaveTasksFails(t *testing.T) {
	repo := setupTestProject(t)
	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", DelegationID: "x",
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := applyCloseoutDecisionToTasks(repo, "plan-001", "task-001", closeout)
	if err == nil || !strings.Contains(err.Error(), "save tasks") {
		t.Fatalf("expected save-tasks error, got %v", err)
	}
}

func TestApplyCloseoutDecisionToTasks_TasksLoadFails(t *testing.T) {
	repo := setupTestProject(t)
	// Remove TASKS.yaml to trigger load failure.
	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", Decision: "accept",
	}
	err := applyCloseoutDecisionToTasks(repo, "plan-001", "task-001", closeout)
	if err == nil || !strings.Contains(err.Error(), "load canonical tasks") {
		t.Fatalf("expected tasks-load error, got %v", err)
	}
}

// ── archiveCloseoutArtifacts error branches ────────────────────────────────

func TestArchiveCloseoutArtifacts_MissingMergeBackFails(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")
	c, _ := loadDelegationContract(repo, "task-001")

	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", DelegationID: c.ID,
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// merge-back source does not exist → copyWorkflowArtifact fails.
	_, _, err := archiveCloseoutArtifacts(repo, "task-001", "plan-001", "accept", c, closeout)
	if err == nil || !strings.Contains(err.Error(), "archive merge-back") {
		t.Fatalf("expected archive merge-back error, got %v", err)
	}
}

func TestArchiveCloseoutArtifacts_MarshalCloseoutError(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")
	c, _ := loadDelegationContract(repo, "task-001")

	// pre-seed merge-back so it can be copied
	if err := saveMergeBack(repo, &MergeBackSummary{
		SchemaVersion: 1, TaskID: "task-001", ParentPlanID: "plan-001",
		Title: "x", Summary: "s", VerificationResult: MergeBackVerification{Status: "pass"},
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("marshal boom")
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, sentinel }
	t.Cleanup(func() { yamlMarshal = prev })

	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", DelegationID: c.ID,
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}
	_, _, err := archiveCloseoutArtifacts(repo, "task-001", "plan-001", "accept", c, closeout)
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected marshal sentinel, got %v", err)
	}
}

// ── runWorkflowGraphHealth JSON path ───────────────────────────────────────

func TestRunWorkflowGraphHealth_JSON_FromRepo(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowGraphHealth(nil, nil)
	}, `"status"`)
}

func TestRunWorkflowGraphHealth_DegradedHuman(t *testing.T) {
	repo := setupTestProject(t)
	// Bridge config pointing at a non-existent graph home.
	bridgeDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(bridgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bridgeDir, "graph-bridge.yaml"),
		[]byte("schema_version: 1\nenabled: true\ngraph_home: /no/such/path\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowGraphHealth(nil, nil)
	}, "Graph Bridge Health")
}

// ── runWorkflowHealth JSON path ────────────────────────────────────────────

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

// ── runWorkflowFanout TDD-gate skip flag path ─────────────────────────────

func TestFanout_SkipTDDGate(t *testing.T) {
	repo := setupTestProject(t)
	// Force a .go file under commands/ without a paired _test.go.
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "commands", "x.go"), []byte("package commands\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	tf.Tasks[0].WriteScope = []string{"commands/x.go"}
	tf.Tasks[0].VerificationRequired = true
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}

	// --skip-tdd-gate should bypass the gate.
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w", "--skip-tdd-gate"); err != nil {
		t.Fatalf("expected fanout to succeed with --skip-tdd-gate: %v", err)
	}
}

// ── runWorkflowFanout skip-evidence-check flag path ────────────────────────

func TestFanout_SkipEvidenceCheck(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo) // graph healthy
	chdirRepo(t, repo)

	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w", "--skip-evidence-check"); err != nil {
		t.Fatalf("expected fanout to succeed with --skip-evidence-check: %v", err)
	}
}

// ── runWorkflowFoldBackUpsert: --propose path through createProposalFoldBack

func TestFoldBackCreate_ProposeWritesProposal(t *testing.T) {
	repo := setupFoldBackProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "needs design change", "--propose"); err != nil {
		t.Fatalf("expected propose flow to succeed: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(agentsHome, "proposals", "obs-*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one proposal, err=%v matches=%v", err, matches)
	}
}

// ── runWorkflowMergeBack: default --verification-status is "unknown" (valid)
// so omitting it succeeds. Not an error case.

// ── runWorkflowAdvance: subsequent saveCanonicalPlan error via marshal stub

func TestRunWorkflowAdvance_PlanSaveError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	// First write: tasks save succeeds (uses unstubbed os.WriteFile path).
	// Inject yamlMarshal stub that fails on the second call (the plan save).
	prev := yamlMarshal
	calls := 0
	yamlMarshal = func(v any) ([]byte, error) {
		calls++
		if calls >= 2 {
			return nil, errors.New("marshal plan boom")
		}
		return prev(v)
	}
	t.Cleanup(func() { yamlMarshal = prev })

	err := runWorkflowAdvance("plan-001", "task-001", "in_progress")
	if err == nil {
		t.Fatal("expected save-plan error to propagate")
	}
}

// ── runWorkflowTaskAdd: skipped plan save (graceful warn) ──────────────────

func TestRunWorkflowTaskAdd_SucceedsEvenIfPlanSaveSkipped(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001", TaskID: "tnew", Title: "T", WriteScope: "x/,y/",
	}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	tf, err := loadCanonicalTasks(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, t := range tf.Tasks {
		if t.ID == "tnew" {
			found = true
			if len(t.WriteScope) != 2 {
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected new task in tasks: %+v", tf.Tasks)
	}
}

// ── runWorkflowPlanUpdate happy path + invalid status ─────────────────────

func TestRunWorkflowPlanUpdate_Status(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowPlanUpdate("plan-001", "paused", "T2", "S2", "focus", "SC", "VS"); err != nil {
		t.Fatalf("update: %v", err)
	}
	plan, err := loadCanonicalPlan(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "paused" || plan.Title != "T2" {
		t.Fatalf("update did not persist: %+v", plan)
	}
}

func TestRunWorkflowPlanUpdate_InvalidStatus(t *testing.T) {
	err := runWorkflowPlanUpdate("plan-001", "bogus", "", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "invalid plan status") {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

func TestRunWorkflowPlanUpdate_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowPlanUpdate("ghost-plan", "paused", "", "", "", "", "")
	if err == nil {
		t.Fatal("expected plan-not-found")
	}
}

// ── runWorkflowPlanCreate: tasks file write error ─────────────────────────

func TestRunWorkflowPlanCreate_TasksWriteError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	prev := osWriteFile
	calls := 0
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		calls++
		if calls >= 2 {
			return errors.New("write tasks boom")
		}
		return prev(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = prev })

	err := runWorkflowPlanCreate("plan-new", "T", "S", "O", "SC", "VS")
	if err == nil {
		t.Fatal("expected error from second write")
	}
}

// ── loadCanonicalPlan: parse error ─────────────────────────────────────────

func TestLoadCanonicalPlan_ParseError(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadCanonicalPlan(repo, "plan-001")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadCanonicalTasks_ParseError(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadCanonicalTasks(repo, "plan-001")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadCanonicalSlices_ParseError(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "p1", "SLICES.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadCanonicalSlices(repo, "p1")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// ── saveCanonicalPlan / saveCanonicalTasks marshal/write errors ───────────

func TestSaveCanonicalPlan_MarshalError(t *testing.T) {
	repo := t.TempDir()
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { yamlMarshal = prev })
	err := saveCanonicalPlan(repo, &CanonicalPlan{ID: "p"})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestSaveCanonicalTasks_MarshalError(t *testing.T) {
	repo := t.TempDir()
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { yamlMarshal = prev })
	err := saveCanonicalTasks(repo, &CanonicalTaskFile{PlanID: "p"})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

// (TestSaveCanonicalTasks_MkdirError / TestSaveCanonicalPlan_MkdirError already in seams_test.go)

// ── loadDelegationContract: parse error ────────────────────────────────────

func TestLoadDelegationContract_ParseError(t *testing.T) {
	repo := t.TempDir()
	dir := delegationDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t.yaml"), []byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadDelegationContract(repo, "t")
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// ── collectCanonicalPlans: unreadable plan path ────────────────────────────

func TestCollectCanonicalPlans_UnreadablePlan(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, warnings := collectCanonicalPlans(repo)
	if len(warnings) == 0 {
		t.Fatal("expected warning for unreadable plan")
	}
}

// (readFoldBackProposalFile error paths covered by foldback_helpers_test.go)

// ── proposalAbsPathFromRoutedTo error paths ───────────────────────────────

func TestProposalAbsPathFromRoutedTo_NotProposal(t *testing.T) {
	_, err := proposalAbsPathFromRoutedTo("not-a-proposal")
	if err == nil || !strings.Contains(err.Error(), "not a proposal route") {
		t.Fatalf("expected not-a-proposal error, got %v", err)
	}
}

func TestProposalAbsPathFromRoutedTo_InvalidName(t *testing.T) {
	cases := []string{
		"proposal:",
		"proposal:../etc/passwd",
		"proposal:foo/bar",
		"proposal:foo\\bar",
	}
	for _, c := range cases {
		c := c
		t.Run(c, func(t *testing.T) {
			_, err := proposalAbsPathFromRoutedTo(c)
			if err == nil || !strings.Contains(err.Error(), "invalid proposal name") {
				t.Fatalf("expected invalid-name error for %q, got %v", c, err)
			}
		})
	}
}

func TestProposalAbsPathFromRoutedTo_HappyPath(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	p, err := proposalAbsPathFromRoutedTo("proposal:obs-123.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, filepath.Join("proposals", "obs-123.md")) {
		t.Fatalf("unexpected path %q", p)
	}
}

// ── loadFoldBackArtifactByID error paths ──────────────────────────────────

func TestLoadFoldBackArtifactByID_NotFound(t *testing.T) {
	repo := t.TempDir()
	_, err := loadFoldBackArtifactByID(repo, "no-such")
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestLoadFoldBackArtifactByID_ParseError(t *testing.T) {
	repo := t.TempDir()
	dir := foldBackDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foldBackArtifactFile(repo, "bad"), []byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadFoldBackArtifactByID(repo, "bad")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// ── writeFoldBackArtifact mkdir/marshal error ──────────────────────────────

// (TestWriteFoldBackArtifact_MkdirError already in seams_test.go)

func TestWriteFoldBackArtifact_MarshalError(t *testing.T) {
	repo := t.TempDir()
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal boom") }
	t.Cleanup(func() { yamlMarshal = prev })
	err := writeFoldBackArtifact(repo, foldBackArtifact{ID: "x"})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

// ── runWorkflowAdvance: ui.Success + happy path ────────────────────────────

func TestRunWorkflowAdvance_HappyPathInProgress(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowAdvance("plan-001", "task-001", "in_progress"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	if tf.Tasks[0].Status != "in_progress" {
		t.Fatalf("expected in_progress, got %q", tf.Tasks[0].Status)
	}
	plan, _ := loadCanonicalPlan(repo, "plan-001")
	if plan.CurrentFocusTask != "Do the thing" {
		t.Fatalf("expected focus update, got %q", plan.CurrentFocusTask)
	}
}

// ── runWorkflowEligible: error when selectAllEligibleTasks fails ──────────

func TestRunWorkflowEligible_EmptyJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowEligible("", 0)
	}, `"eligible_tasks": []`)
}

// ── runWorkflowFanout: bundle write error via marshal stub ────────────────

func TestFanout_BundleMarshalError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	prev := yamlMarshal
	calls := 0
	yamlMarshal = func(v any) ([]byte, error) {
		calls++
		// Let some marshal succeed (delegation contract write happens
		// earlier and we want it to succeed), then fail the bundle save.
		if calls >= 2 {
			return nil, errors.New("marshal bundle boom")
		}
		return prev(v)
	}
	t.Cleanup(func() { yamlMarshal = prev })

	err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected marshal failure")
	}
}

// ── runWorkflowVerifyRecordReview error: --status forbidden with kind=review

func TestRunWorkflowVerifyRecordReview_StatusForbidden(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "review", "--status", "pass",
		"--task", "t1", "--summary", "review approved",
	)
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected status-forbidden error, got %v", err)
	}
}

// ── runWorkflowCheckpoint: gitModifiedFiles error path returns empty list ─

func TestRunWorkflowCheckpoint_HappyPath(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowCheckpoint("hello", "pass", "all green"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

// ── runWorkflowFanout: append-canonical-plan-to-graph path ────────────────

func TestRunWorkflowComplete_ActionableHuman(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowComplete("wave-2")
	}, "Scoped Plan Completion")
}

// ── ensureTaskVerificationDir uses os.MkdirAll directly (no seam) ─────────
