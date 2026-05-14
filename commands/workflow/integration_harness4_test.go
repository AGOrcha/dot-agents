package workflow

// Fourth and final batch: targets specific uncovered statement ranges
// identified by per-line analysis of the coverage profile. Each test is
// chosen to hit one or more error-return branches that previous batches
// missed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── parseFoldBackUpsertInputs error branches ───────────────────────────────

func TestFoldBackCreate_MissingObservation(t *testing.T) {
	repo := setupFoldBackProject(t)
	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "   ")
	if err == nil || !strings.Contains(err.Error(), "observation") {
		t.Fatalf("expected observation-required, got %v", err)
	}
}

func TestFoldBackUpdate_MissingSlug(t *testing.T) {
	repo := setupFoldBackProject(t)
	err := executeWorkflowCommand(t, repo, "fold-back", "update",
		"--plan", "p1", "--observation", "x")
	if err == nil || !strings.Contains(err.Error(), "slug") {
		t.Fatalf("expected slug-required-for-update, got %v", err)
	}
}

// ── validateFoldBackPriorAgreement plan-mismatch path ──────────────────────

func TestFoldBackUpdate_RejectsCrossPlan(t *testing.T) {
	repo := setupFoldBackTwoPlanProject(t)
	slug := "shared-slug"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "v1"); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "fold-back", "update",
		"--plan", "p2", "--task", "t1", "--slug", slug, "--observation", "v2")
	if err == nil {
		t.Fatal("expected cross-plan rejection")
	}
}

func TestFoldBackUpdate_ProposeInvalid(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "small-slug"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "v1"); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "fold-back", "update",
		"--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "v2", "--propose")
	if err == nil {
		t.Fatal("expected --propose-not-valid-for-update")
	}
}

// ── runWorkflowNext: render verification optional path ─────────────────────

func TestRunWorkflowNext_VerificationOptional(t *testing.T) {
	repo := setupTestProject(t)
	// Mark task-001 as verification_required: false to hit the "optional" branch.
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	for i := range tf.Tasks {
		tf.Tasks[i].VerificationRequired = false
	}
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowNext("plan-001")
	}, "verification: optional")
}

// ── runWorkflowNext: task with depends_on rendered ─────────────────────────

func TestRunWorkflowNext_RendersDependsOn(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	chdirRepo(t, repo)
	// "planner" depends on "prep" (completed) so is eligible. Notice
	// renderer outputs "depends on:" when DependsOn is non-empty.
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowNext("wave-next")
	}, "depends on:")
}

// ── deriveScopeRunScopeLane returns files when adapter present ────────────

// (No additional test needed — already covered via runWorkflowPlanDeriveScope.)

// ── runWorkflowEligible: limit truncation hits 1424 ───────────────────────

func TestRunWorkflowEligible_LimitTruncationCount(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	chdirRepo(t, repo)
	// wave-next has 2 pending tasks (planner, tests) — limit=1 truncates.
	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	out := captureStdoutToString(t, func() {
		_ = runWorkflowEligible("wave-next", 1)
	})
	// JSON should still emit eligible_tasks (truncated).
	if !strings.Contains(out, `"eligible_tasks"`) {
		t.Fatalf("expected eligible_tasks key: %s", out)
	}
}

// ── runWorkflowPlanList: JSON path with plans present ─────────────────────

func TestRunWorkflowPlanList_JSON_WithMultiplePlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	addCanonicalPendingPlanFixture(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanList() },
		`"wave-2"`, `"wave-next"`)
}

// ── runWorkflowTaskAdd: parse CSV blocks/depends_on ────────────────────────

func TestRunWorkflowTaskAdd_WithFullCSVFields(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID:               "plan-001",
		TaskID:               "tcsv",
		Title:                "with deps",
		DependsOn:            "task-001, task-002",
		Blocks:               "x, y",
		WriteScope:           "a/,b/",
		Owner:                "me",
		AppType:              "api",
		VerificationRequired: true,
	}); err != nil {
		t.Fatal(err)
	}
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	for _, t2 := range tf.Tasks {
		if t2.ID == "tcsv" {
			if len(t2.DependsOn) != 2 || len(t2.Blocks) != 2 || len(t2.WriteScope) != 2 {
				t.Fatalf("CSV not parsed: %+v", t2)
			}
			return
		}
	}
	t.Fatal("tcsv not found")
}

// ── runWorkflowCheckpoint: writes a session-log entry the first time ──────

func TestRunWorkflowCheckpoint_PersistsSessionLog(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowCheckpoint("first message", "pass", "all good"); err != nil {
		t.Fatal(err)
	}
	// expect session-log.md exists
	matches, _ := filepath.Glob(filepath.Join(agentsHome, "context", "*", "session-log.md"))
	if len(matches) == 0 {
		t.Fatalf("expected session-log.md, found: %v", matches)
	}
}

// ── runWorkflowFanout: --selection-reason path through bundle ─────────────

func TestFanout_SelectionReasonPropagatedToBundle(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--selection-reason", "for tests",
	); err != nil {
		t.Fatal(err)
	}
	bundle := loadFanoutBundle(t, repo, "task-001")
	if bundle.Selection == nil || bundle.Selection.Reason != "for tests" {
		t.Fatalf("selection reason missing: %+v", bundle.Selection)
	}
}

// ── runWorkflowFoldBackList: JSON path through readFoldBackArtifacts ──────

func TestRunWorkflowFoldBackList_JSON_WithArtifacts(t *testing.T) {
	repo := setupFoldBackProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "v"); err != nil {
		t.Fatal(err)
	}

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, `"plan_id"`) {
		t.Fatalf("expected JSON plan_id: %s", out)
	}
}

// ── runWorkflowDelegationCloseout: JSON output via cobra ──────────────────

func TestDelegationCloseoutCobra_JSON_RejectsInvalidDecision(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "delegation", "closeout",
		"--plan", "p1", "--task", "t1", "--decision", "")
	if err == nil {
		t.Fatal("expected empty-decision error")
	}
}

// (TestFanoutEvidenceWarning_NoSidecarGraphDegraded already in delegation_fanout_test.go)

// ── runWorkflowVerifyRecord happy: no task means no typed artifact ────────

func TestVerifyRecord_HappyPath_NoTask(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "test", "--status", "pass",
		"--summary", "all tests passed",
	); err != nil {
		t.Fatalf("verify record: %v", err)
	}
}

// ── runWorkflowCheckpointLogToIter: via cobra ──────────────────────────────

func TestRunWorkflowCheckpoint_LogToIterFlag(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	// log-to-iter requires an existing plan and iter-log dir.
	addCanonicalPlanFixture(t, repo)
	iterDir := filepath.Join(repo, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(iterDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed iter-1.yaml so log-to-iter can read it.
	if err := os.WriteFile(filepath.Join(iterDir, "iter-1.yaml"),
		[]byte("schema_version: 1\niteration: 1\nstarted_at: 2026-01-01T00:00:00Z\nrole: worker\n"),
		0644); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "checkpoint",
		"--message", "iter checkpoint",
		"--verification-status", "pass",
		"--log-to-iter", "1",
	); err != nil {
		t.Fatalf("checkpoint --log-to-iter: %v", err)
	}
}

// ── runWorkflowFanout: handles verifier-retry-max flag ─────────────────────

func TestFanout_VerifierRetryMax(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--verifier-retry-max", "5",
	); err != nil {
		t.Fatal(err)
	}
}

// ── runWorkflowPlanArchive bulk: empty list ────────────────────────────────

func TestRunWorkflowPlanArchive_EmptyList(t *testing.T) {
	repo := setupTestProject(t)
	// passing no IDs should not fail
	if err := runWorkflowPlanArchive(repo, nil, false, false); err != nil {
		t.Fatalf("empty plan archive should be a no-op, got %v", err)
	}
}

// ── runWorkflowLog: read existing log ──────────────────────────────────────

func TestRunWorkflowLog_ReadsExistingFile(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowCheckpoint("for-log", "pass", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := runWorkflowLog(false); err != nil {
		t.Fatalf("runWorkflowLog: %v", err)
	}
	if err := runWorkflowLog(true); err != nil {
		t.Fatalf("runWorkflowLog all: %v", err)
	}
}

// ── runWorkflowPrefs: render rendering for non-default values ─────────────

func TestRunWorkflowPrefs_OverridesShown(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowPrefsSetLocal("verification.test_command", "make test"); err != nil {
		t.Fatal(err)
	}
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPrefs() },
		"verification", "make test")
}

// (verify record artifact marshal error path: not easily seamed because
// jsonMarshal is the relevant seam, but the writeVerifyResultArtifact
// path uses jsonMarshal after yamlMarshal-only paths complete)

// ── runWorkflowAdvance via cobra ──────────────────────────────────────────

func TestRunWorkflowAdvance_CobraJSONNotUsed(t *testing.T) {
	repo := setupTestProject(t)
	// Use cobra surface to ensure newWorkflowAdvanceCmd happy path is hit.
	if err := executeWorkflowCommand(t, repo, "advance", "plan-001",
		"--task", "task-001", "--status", "in_progress"); err != nil {
		t.Fatal(err)
	}
}

// ── runWorkflowComplete via cobra ─────────────────────────────────────────

func TestRunWorkflowComplete_CobraHappyPath(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	if err := executeWorkflowCommand(t, repo, "complete", "--plan", "wave-2"); err != nil {
		t.Fatal(err)
	}
}

// ── runWorkflowTasks via cobra ────────────────────────────────────────────

// (cobra surface tasks JSON already covered via runWorkflowTasks JSON test)

// (runWorkflowPlanSchedule covered by direct + JSON tests above)

// ── runWorkflowEligible via cobra ─────────────────────────────────────────

// (cobra surface for eligible JSON already covered via runWorkflowEligible JSON test)

// ── runWorkflowVerifyRecordReview happy path: phase decision ──────────────

func TestVerifyRecordReview_PhaseDecisionRecorded(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "review",
		"--task", "t1",
		"--phase1-decision", "accept",
		"--phase2-decision", "accept",
		"--summary", "LGTM",
	); err != nil {
		t.Fatalf("verify record review: %v", err)
	}
}

// ── runWorkflowFanout: with selection-reason flag (covers selection bundle path)

func TestFanout_SelectionReasonFromFlag(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--selection-reason", "smoke",
	); err != nil {
		t.Fatal(err)
	}
	b := loadFanoutBundle(t, repo, "task-001")
	if b.Selection == nil || b.Selection.Reason != "smoke" {
		t.Fatalf("expected selection reason 'smoke': %+v", b.Selection)
	}
}

// ── mergeBack via cobra round trip + post-conditions ─────────────────────

func TestMergeBack_CobraRoundTrip(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back",
		"--task", "t1", "--summary", "implemented",
		"--verification-status", "pass",
		"--integration-notes", "no blockers",
	); err != nil {
		t.Fatal(err)
	}
	mb, err := loadMergeBack(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if mb.VerificationResult.Status != "pass" {
		t.Fatalf("status mismatch: %+v", mb)
	}
}

// ── delegation_test: existing-contract status update on re-save ───────────

func TestSaveDelegationContract_UpdatesTimestamp(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-x", "plan-x", "del-x")
	c, err := loadDelegationContract(repo, "task-x")
	if err != nil {
		t.Fatal(err)
	}
	firstUpdate := c.UpdatedAt
	time.Sleep(1100 * time.Millisecond)
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}
	c2, err := loadDelegationContract(repo, "task-x")
	if err != nil {
		t.Fatal(err)
	}
	if c2.UpdatedAt == firstUpdate {
		t.Fatalf("expected UpdatedAt to advance: %q == %q", firstUpdate, c2.UpdatedAt)
	}
}
