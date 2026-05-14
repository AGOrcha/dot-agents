// Package workflow — third batch of coverage tests targeting low-coverage
// run-* commands and render helpers (runWorkflowComplete, runWorkflowVerifyLog,
// renderEligibleOutput/Task, renderPlanShowSlices, scanActiveDelegationContract,
// canonicalPlansHaveActive, runWorkflowOrient sessions section, and confirmSweepAction).
package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// ─────────────────────────────────────────────────────────────────────────────
// canonicalPlansHaveActive
// ─────────────────────────────────────────────────────────────────────────────

func TestCanonicalPlansHaveActive(t *testing.T) {
	if canonicalPlansHaveActive(nil) {
		t.Error("nil slice should not have active")
	}
	summaries := []workflowCanonicalPlanSummary{
		{ID: "a", Status: "draft"},
		{ID: "b", Status: "completed"},
	}
	if canonicalPlansHaveActive(summaries) {
		t.Error("no active plan should return false")
	}
	summaries = append(summaries, workflowCanonicalPlanSummary{ID: "c", Status: "active"})
	if !canonicalPlansHaveActive(summaries) {
		t.Error("should return true with an active plan")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// scanActiveDelegationContract
// ─────────────────────────────────────────────────────────────────────────────

func TestScanActiveDelegationContract_NoContracts(t *testing.T) {
	dir := t.TempDir()
	wave, taskID := scanActiveDelegationContract(dir)
	if wave != "" || taskID != "" {
		t.Errorf("expected empty pair for no contracts, got (%q,%q)", wave, taskID)
	}
}

func TestScanActiveDelegationContract_ActiveContractReturned(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-a", ParentPlanID: "plan-x", ParentTaskID: "task-y",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, c); err != nil {
		t.Fatal(err)
	}
	wave, taskID := scanActiveDelegationContract(dir)
	if wave != "plan-x" || taskID != "task-y" {
		t.Errorf("scanActiveDelegationContract = (%q,%q), want (plan-x,task-y)", wave, taskID)
	}
}

func TestScanActiveDelegationContract_ClosedContractsSkipped(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	closed := &DelegationContract{
		SchemaVersion: 1, ID: "del-closed", ParentPlanID: "plan-closed", ParentTaskID: "task-closed",
		Title: "x", WriteScope: []string{"commands/"}, Status: "completed",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, closed); err != nil {
		t.Fatal(err)
	}
	wave, taskID := scanActiveDelegationContract(dir)
	if wave != "" || taskID != "" {
		t.Errorf("expected empty for only-closed contracts, got (%q,%q)", wave, taskID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// renderEligibleTask / renderEligibleOutput
// ─────────────────────────────────────────────────────────────────────────────

func TestRenderEligibleTask_Conflicts(t *testing.T) {
	at := AnnotatedTask{
		workflowNextTaskSuggestion: workflowNextTaskSuggestion{
			PlanID: "p1", TaskID: "t1", TaskTitle: "Build feature", Status: "pending",
			WriteScope: []string{"commands/"},
		},
		WriteScopeDeclared: true,
		HasEvidence:        true,
		EvidenceConfidence: "high",
		ConflictsWith:      []string{"t2"},
	}
	out, _ := captureCovStdout(t, func() error {
		renderEligibleTask(at)
		return nil
	})
	for _, want := range []string{"[p1/t1]", "Build feature", "evidence: true", "high", "conflicts: t2"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestRenderEligibleTask_NoWriteScope(t *testing.T) {
	at := AnnotatedTask{
		workflowNextTaskSuggestion: workflowNextTaskSuggestion{
			PlanID: "p1", TaskID: "t1", TaskTitle: "T", Status: "pending",
		},
		WriteScopeDeclared: false,
		EvidenceConfidence: "none",
	}
	out, _ := captureCovStdout(t, func() error {
		renderEligibleTask(at)
		return nil
	})
	if !strings.Contains(out, "(none) [no write_scope declared]") {
		t.Errorf("expected no-write-scope label in output, got: %s", out)
	}
}

func TestRenderEligibleOutput_WithMaxBatch(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{{
			workflowNextTaskSuggestion: workflowNextTaskSuggestion{
				PlanID: "p1", TaskID: "t1", TaskTitle: "T", Status: "pending",
			},
			WriteScopeDeclared: true,
			EvidenceConfidence: "none",
		}},
		MaxBatch:      []string{"p1/t1"},
		TotalEligible: 1,
		MaxParallel:   1,
	}
	stdout, _ := captureCovStdout(t, func() error {
		renderEligibleOutput(out, 1, 0)
		return nil
	})
	if !strings.Contains(stdout, "Eligible Tasks") || !strings.Contains(stdout, "max batch: p1/t1") {
		t.Errorf("expected eligible tasks header + max batch, got: %s", stdout)
	}
	if !strings.Contains(stdout, "max_parallel_workers=1") {
		t.Errorf("expected max_parallel label, got: %s", stdout)
	}
}

func TestRenderEligibleOutput_LimitOverride(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{},
		TotalEligible: 0,
		DraftPlans:    []string{"draft-x"},
	}
	stdout, _ := captureCovStdout(t, func() error {
		renderEligibleOutput(out, 1, 3)
		return nil
	})
	if !strings.Contains(stdout, "--limit=3") {
		t.Errorf("expected --limit=3 label, got: %s", stdout)
	}
	if !strings.Contains(stdout, "draft-x") {
		t.Errorf("expected draft plans hint when zero eligible, got: %s", stdout)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowComplete — full render including paused / locked plans
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowComplete_DrainedRendered(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("nope") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "state: drained") {
		t.Errorf("expected drained state in output, got: %s", out)
	}
}

func TestRunWorkflowComplete_ActionableJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("wave-next") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "\"state\": \"actionable\"") {
		t.Errorf("expected state=actionable in JSON, got: %s", out)
	}
	if !strings.Contains(out, "wave-next") {
		t.Errorf("expected plan id in JSON, got: %s", out)
	}
}

func TestRunWorkflowComplete_PausedRendered(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	savePausedPlanFixture(t, repo)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("paused-plan") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "Scoped Plan Completion") {
		t.Errorf("expected header, got: %s", out)
	}
	if !strings.Contains(out, "paused-plan") {
		t.Errorf("expected paused plan id in output, got: %s", out)
	}
	if !strings.Contains(out, "paused plans:") {
		t.Errorf("expected paused plans list, got: %s", out)
	}
}

func TestRunWorkflowComplete_LockedRendered(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	// Add an active delegation contract that locks every pending task in wave-next.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, taskID := range []string{"planner", "tests"} {
		if err := saveDelegationContract(repo, &DelegationContract{
			SchemaVersion: 1,
			ID:            "del-" + taskID,
			ParentPlanID:  "wave-next",
			ParentTaskID:  taskID,
			Title:         "lock",
			WriteScope:    []string{"commands/"},
			Status:        "active",
			CreatedAt:     now,
			UpdatedAt:     now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("wave-next") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "locked plans:") {
		t.Errorf("expected locked plans line in output, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowVerifyLog
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowVerifyLog_Empty(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(false) })
	if err != nil {
		t.Fatalf("runWorkflowVerifyLog: %v", err)
	}
	if !strings.Contains(out, "No verification records") {
		t.Errorf("expected empty marker, got: %s", out)
	}
}

func TestRunWorkflowVerifyLog_WithRecords(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	chdirForCov(t, repo)
	for _, status := range []string{"pass", "fail", "partial", "unknown"} {
		rec := VerificationRecord{
			SchemaVersion: 1, Timestamp: time.Now().UTC().Format(time.RFC3339),
			Kind: "test", Status: status, Scope: "repo",
			Summary: "summary-" + status, Command: "go test",
		}
		if err := appendVerificationLog("workflow-proj", rec); err != nil {
			t.Fatal(err)
		}
	}
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(true) })
	if err != nil {
		t.Fatalf("runWorkflowVerifyLog: %v", err)
	}
	for _, want := range []string{"Verification Log", "summary-pass", "summary-fail", "summary-partial", "summary-unknown", "cmd: go test"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestRunWorkflowVerifyLog_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	chdirForCov(t, repo)
	rec := VerificationRecord{
		SchemaVersion: 1, Timestamp: time.Now().UTC().Format(time.RFC3339),
		Kind: "test", Status: "pass", Scope: "repo", Summary: "ok",
	}
	if err := appendVerificationLog("workflow-proj", rec); err != nil {
		t.Fatal(err)
	}
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(false) })
	if err != nil {
		t.Fatalf("runWorkflowVerifyLog: %v", err)
	}
	if !strings.Contains(out, "\"kind\": \"test\"") {
		t.Errorf("expected JSON output, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// renderPlanShowSlices — covers the nil-error branch with rendered slices.
// ─────────────────────────────────────────────────────────────────────────────

func TestRenderPlanShowSlices_NilError(t *testing.T) {
	sf := &CanonicalSliceFile{
		SchemaVersion: 1, PlanID: "p1",
		Slices: []CanonicalSlice{
			{ID: "slice-a", ParentTaskID: "t1", Title: "Slice A", Status: "pending"},
			{ID: "slice-b", ParentTaskID: "t2", Title: "Slice B", Status: "completed"},
		},
	}
	out, _ := captureCovStdout(t, func() error {
		renderPlanShowSlices(sf, nil)
		return nil
	})
	if !strings.Contains(out, "Slices") ||
		!strings.Contains(out, "slice-a") ||
		!strings.Contains(out, "slice-b") {
		t.Errorf("expected slices listed, got: %s", out)
	}
}

func TestRenderPlanShowSlices_ErrorPath(t *testing.T) {
	sf := &CanonicalSliceFile{}
	out, _ := captureCovStdout(t, func() error {
		// Pass a non-nil error: function should skip the section but still
		// print a trailing newline.
		renderPlanShowSlices(sf, os.ErrNotExist)
		return nil
	})
	if strings.Contains(out, "Slices") {
		t.Errorf("expected slices header suppressed on err, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// renderOrientRecentSessionsSection
// ─────────────────────────────────────────────────────────────────────────────

func TestRenderOrientRecentSessionsSection_RendersSessions(t *testing.T) {
	state := &workflowOrientState{
		RecentSessions: []branchSessionInfo{
			{Platform: "claude", SessionID: "abcdefgh1234567890", Timestamp: "2026-04-30T10:11:12Z", MessageCount: 42},
			{Platform: "codex", SessionID: "short", Timestamp: "2026-04-30T10", MessageCount: 0},
		},
	}
	out, _ := captureCovStdout(t, func() error {
		renderOrientRecentSessionsSection(state, os.Stdout)
		return nil
	})
	for _, want := range []string{"Recent sessions on this branch", "abcdefgh", "claude", "~42 messages", "codex"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestRenderOrientRecentSessionsSection_EmptySkipped(t *testing.T) {
	state := &workflowOrientState{}
	out, _ := captureCovStdout(t, func() error {
		renderOrientRecentSessionsSection(state, os.Stdout)
		return nil
	})
	if out != "" {
		t.Errorf("expected empty output when no sessions, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// mergeImplIterLog
// ─────────────────────────────────────────────────────────────────────────────

func TestMergeImplIterLog_NoBundleFallsBackEmpty(t *testing.T) {
	dir := t.TempDir()
	entry := &iterLogEntry{}
	// Nil contract → no bundle path → empty feedback goal.
	mergeImplIterLog(entry, nil, dir)
	if entry.Impl.FeedbackGoal != "" {
		t.Errorf("expected empty feedback goal with nil contract, got %q", entry.Impl.FeedbackGoal)
	}
}

func TestMergeImplIterLog_WithBundle(t *testing.T) {
	dir := t.TempDir()
	bundlesDir := delegationBundlesDir(dir)
	if err := os.MkdirAll(bundlesDir, 0755); err != nil {
		t.Fatal(err)
	}
	bundle := delegationBundleYAML{}
	bundle.Verification.FeedbackGoal = "tests must cover new behavior"
	data, err := yaml.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundlesDir, "del-id.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	c := &DelegationContract{ID: "del-id"}
	entry := &iterLogEntry{}
	mergeImplIterLog(entry, c, dir)
	if entry.Impl.FeedbackGoal != "tests must cover new behavior" {
		t.Errorf("expected feedback goal merged from bundle, got %q", entry.Impl.FeedbackGoal)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowOrient — exercises the JSON render path
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowOrient_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowOrient() })
	if err != nil {
		t.Fatalf("runWorkflowOrient: %v", err)
	}
	if !strings.Contains(out, "\"project\":") {
		t.Errorf("expected project field in JSON output, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowEligible / runWorkflowNext JSON branches
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowEligible_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowEligible("", 0) })
	if err != nil {
		t.Fatalf("runWorkflowEligible: %v", err)
	}
	if !strings.Contains(out, "\"eligible_tasks\":") {
		t.Errorf("expected eligible_tasks key, got: %s", out)
	}
}

func TestRunWorkflowEligible_LimitTruncates(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	// limit=1 forces the truncation branch (selectAllEligibleTasks > effective).
	out, err := captureCovStdout(t, func() error { return runWorkflowEligible("", 1) })
	if err != nil {
		t.Fatalf("runWorkflowEligible: %v", err)
	}
	if !strings.Contains(out, "Eligible Tasks") {
		t.Errorf("expected eligible header, got: %s", out)
	}
}

func TestRunWorkflowNext_JSONNoSuggestion(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowNext("") })
	if err != nil {
		t.Fatalf("runWorkflowNext: %v", err)
	}
	if !strings.Contains(out, "\"suggestion\": null") {
		t.Errorf("expected null suggestion in JSON, got: %s", out)
	}
}

func TestRunWorkflowNext_JSONWithSuggestion(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowNext("") })
	if err != nil {
		t.Fatalf("runWorkflowNext: %v", err)
	}
	if !strings.Contains(out, "\"plan_id\":") {
		t.Errorf("expected plan_id in JSON, got: %s", out)
	}
}

func TestRunWorkflowNext_HumanRendersFullSuggestion(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowNext("") })
	if err != nil {
		t.Fatalf("runWorkflowNext: %v", err)
	}
	for _, want := range []string{"Next Canonical Task", "plan:", "task:", "status:", "reason:", "verification:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowGraphQuery — error paths
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowGraphQuery_UnknownIntent(t *testing.T) {
	dir := t.TempDir()
	chdirForCov(t, dir)
	// Seed an enabled bridge config so resolveGraphBridgeConfig returns OK,
	// then a totally unknown intent triggers the validateGraphBridgeIntent error.
	cfgDir := filepath.Join(dir, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), []byte("schema_version: 1\nenabled: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := newGraphQueryTestCommand("totally_bogus_intent", "")
	err := runWorkflowGraphQuery(cmd, nil)
	if err == nil {
		t.Error("expected unknown-intent error")
	}
}

func newGraphQueryTestCommand(intent, scope string) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("intent", intent, "")
	c.Flags().String("scope", scope, "")
	return c
}

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowFoldBackList — no fold-back dir
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowFoldBackList_NoArtifacts(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, "No fold-back observations") {
		t.Errorf("expected no-artifacts message, got: %s", out)
	}
}

func TestRunWorkflowFoldBackList_NoArtifactsJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, "[]") {
		t.Errorf("expected empty JSON array, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// mergeReviewIterLog
// ─────────────────────────────────────────────────────────────────────────────

func TestMergeReviewIterLog_NoFile(t *testing.T) {
	dir := t.TempDir()
	entry := &iterLogEntry{}
	if err := mergeReviewIterLog(entry, dir, "t1"); err != nil {
		t.Fatal(err)
	}
	// No review file → DecisionArtifact set but no other fields.
	if entry.Review.Phase1Decision != "" {
		t.Errorf("expected no phase1 decision when file absent, got %q", entry.Review.Phase1Decision)
	}
	if entry.Review.DecisionArtifact == "" {
		t.Errorf("expected decision artifact path populated")
	}
}

func TestMergeReviewIterLog_FromFile(t *testing.T) {
	dir := t.TempDir()
	taskID := "task-review"
	rel := iterLogReviewDecisionPath(taskID)
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	doc := []byte(`phase_1_decision: accept
phase_2_decision: reject
overall_decision: reject
failed_gates:
  - unit
  - integration
escalation_reason: needs more work
reviewer_notes: detailed
`)
	if err := os.WriteFile(full, doc, 0644); err != nil {
		t.Fatal(err)
	}
	entry := &iterLogEntry{}
	if err := mergeReviewIterLog(entry, dir, taskID); err != nil {
		t.Fatal(err)
	}
	if entry.Review.Phase1Decision != "accept" || entry.Review.Phase2Decision != "reject" {
		t.Errorf("phase decisions = %q/%q, want accept/reject",
			entry.Review.Phase1Decision, entry.Review.Phase2Decision)
	}
	if entry.Review.OverallDecision != "reject" {
		t.Errorf("overall = %q want reject", entry.Review.OverallDecision)
	}
	if len(entry.Review.FailedGates) != 2 {
		t.Errorf("failed gates = %v, want 2 entries", entry.Review.FailedGates)
	}
	if !entry.Review.VerifyRecordAppended {
		t.Error("expected VerifyRecordAppended = true after parsing file")
	}
}

func TestMergeReviewIterLog_EmptyTaskID(t *testing.T) {
	dir := t.TempDir()
	entry := &iterLogEntry{}
	if err := mergeReviewIterLog(entry, dir, ""); err != nil {
		t.Errorf("empty task id should not error, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// confirmSweepAction with --yes set
// ─────────────────────────────────────────────────────────────────────────────

func TestConfirmSweepAction_YesFlagBypasses(t *testing.T) {
	old := deps.Flags.Yes
	deps.Flags.Yes = func() bool { return true }
	t.Cleanup(func() { deps.Flags.Yes = old })

	action := SweepActionItem{
		Project: ManagedProject{Name: "p"}, Action: SweepActionCreateCheckpointReminder,
		RequiresConfirmation: true, Description: "test reminder",
	}
	if !confirmSweepAction(action, nil) {
		t.Error("expected confirm with --yes to return true even when confirmation required")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// cmd.go entry points exercised through NewCmdForTest
// ─────────────────────────────────────────────────────────────────────────────

func TestWorkflowCmd_NewCmd_BuildsRoot(t *testing.T) {
	// Snapshot package-level deps so test-init values are restored after the
	// real production NewCmd is exercised. NewCmd overwrites `deps`.
	saved := deps
	t.Cleanup(func() { deps = saved })

	newDeps := Deps{
		ErrNoProject: errors.New("no project"),
		Flags: GlobalFlags{
			JSON:   func() bool { return false },
			Yes:    func() bool { return false },
			DryRun: func() bool { return false },
		},
		ErrorWithHints: func(msg string, hints ...string) error { return errors.New(msg) },
		UsageError:     func(msg string, hints ...string) error { return errors.New(msg) },
		NoArgsWithHints: func(hints ...string) cobra.PositionalArgs {
			return func(cmd *cobra.Command, args []string) error {
				if len(args) > 0 {
					return fmt.Errorf("expected no args, got %d", len(args))
				}
				return nil
			}
		},
		ExactArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return func(cmd *cobra.Command, args []string) error {
				if len(args) != n {
					return fmt.Errorf("expected %d args", n)
				}
				return nil
			}
		},
		MaximumNArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return func(cmd *cobra.Command, args []string) error {
				if len(args) > n {
					return fmt.Errorf("too many args")
				}
				return nil
			}
		},
		ExampleBlock: func(lines ...string) string { return strings.Join(lines, "\n") },
	}
	cmd := NewCmd(newDeps)
	if cmd == nil {
		t.Fatal("NewCmd returned nil")
	}
	if !strings.HasPrefix(cmd.Use, "workflow") {
		t.Errorf("expected workflow root, got use=%q", cmd.Use)
	}
}

func TestNewWorkflowPlanCmd_ListsWhenNoPlans(t *testing.T) {
	dir := t.TempDir()
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "plan")
	})
	if !strings.Contains(out, "No canonical plans") {
		t.Errorf("expected empty list output, got: %s", out)
	}
}

func TestNewWorkflowTasksCmd_ListsTasks(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "tasks", "plan-001")
	})
	if !strings.Contains(out, "task-001") {
		t.Errorf("expected task-001 in output, got: %s", out)
	}
}

func TestNewWorkflowSlicesCmd_NoSlices(t *testing.T) {
	dir := setupTestProject(t)
	// Add an empty SLICES.yaml so the runner doesn't error out.
	slicesPath := filepath.Join(dir, ".agents", "workflow", "plans", "plan-001", "SLICES.yaml")
	if err := os.WriteFile(slicesPath, []byte("schema_version: 1\nplan_id: plan-001\nslices: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "slices", "plan-001")
	})
	if !strings.Contains(out, "Slices") {
		t.Errorf("expected slices header in output, got: %s", out)
	}
}

func TestNewWorkflowHealthCmd_RendersSnapshot(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "health")
	})
	// Health output should mention status.
	if !strings.Contains(strings.ToLower(out), "status") {
		t.Errorf("expected status in health output, got: %s", out)
	}
}

func TestNewWorkflowStatusCmd_Renders(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "status")
	})
	if !strings.Contains(out, "Workflow Status") {
		t.Errorf("expected Workflow Status header, got: %s", out)
	}
}

func TestNewWorkflowOrientCmd_Renders(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "orient")
	})
	if !strings.Contains(out, "# Project") {
		t.Errorf("expected orient render, got: %s", out)
	}
}

func TestNewWorkflowPlanArchiveCmd_RequiresPlanFlag(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	// Invocation without --plan must fail (flag marked required).
	err := executeWorkflowCommand(t, dir, "plan", "archive")
	if err == nil {
		t.Error("expected error when --plan flag missing")
	}
}

func TestNewWorkflowPlanArchiveCmd_EmptyAfterTrim(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	// Comma-only plan ids → cleaned slice is empty → error.
	err := executeWorkflowCommand(t, dir, "plan", "archive", "--plan", ", ,")
	if err == nil || !strings.Contains(err.Error(), "at least one plan ID") {
		t.Errorf("expected 'at least one plan ID' error, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// More cmd.go RunE coverage — schedule, plan show, plan graph (single arg)
// ─────────────────────────────────────────────────────────────────────────────

func TestNewWorkflowPlanShowCmd_Invokes(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "plan", "show", "plan-001")
	})
	if !strings.Contains(out, "plan-001") {
		t.Errorf("expected plan-001 in output, got: %s", out)
	}
}

func TestNewWorkflowPlanGraphCmd_NoArg(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "plan", "graph")
	})
	// All-plans graph render; we just need the cmd to execute.
	if out == "" {
		t.Errorf("expected plan graph output, got empty")
	}
}

func TestNewWorkflowPlanGraphCmd_WithArg(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "plan", "graph", "plan-001")
	})
	if !strings.Contains(out, "plan-001") {
		t.Errorf("expected plan-001 in graph output, got: %s", out)
	}
}

func TestNewWorkflowPlanScheduleCmd_Invokes(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "plan", "schedule", "plan-001")
	})
	// Schedule should print at least one task in a wave.
	if out == "" {
		t.Errorf("expected schedule output, got empty")
	}
}

func TestNewWorkflowCompleteCmd_RequiresPlan(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	err := executeWorkflowCommand(t, dir, "complete")
	if err == nil {
		t.Error("expected error when --plan flag missing (marked required)")
	}
}

func TestNewWorkflowCompleteCmd_EmptyPlanRejected(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	chdirForCov(t, dir)
	err := executeWorkflowCommand(t, dir, "complete", "--plan", "   ")
	if err == nil || !strings.Contains(err.Error(), "--plan must not be empty") {
		t.Errorf("expected empty-plan rejection, got: %v", err)
	}
}

func TestNewWorkflowVerifyLogCmd_Invokes(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", agentsHome)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "verify", "log")
	})
	if !strings.Contains(out, "No verification records") {
		t.Errorf("expected empty-log message, got: %s", out)
	}
}

func TestNewWorkflowVerifyLogCmd_AllFlag(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", agentsHome)
	chdirForCov(t, dir)
	// Add one record
	rec := VerificationRecord{
		SchemaVersion: 1, Timestamp: time.Now().UTC().Format(time.RFC3339),
		Kind: "test", Status: "pass", Scope: "repo", Summary: "ok",
	}
	if err := appendVerificationLog("workflow-proj", rec); err != nil {
		t.Fatal(err)
	}
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "verify", "log", "--all")
	})
	if !strings.Contains(out, "Verification Log") {
		t.Errorf("expected verification log header with --all, got: %s", out)
	}
}

func TestNewWorkflowPrefsCmd_Invokes(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", agentsHome)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "prefs")
	})
	if !strings.Contains(out, "Workflow Preferences") {
		t.Errorf("expected Preferences header, got: %s", out)
	}
}

func TestNewWorkflowPrefsShowCmd_Invokes(t *testing.T) {
	dir := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", agentsHome)
	chdirForCov(t, dir)
	out, _ := captureCovStdout(t, func() error {
		return executeWorkflowCommand(t, dir, "prefs", "show")
	})
	if !strings.Contains(out, "Workflow Preferences") {
		t.Errorf("expected Preferences header, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// writeHealthSnapshot / runWorkflowHealth JSON
// ─────────────────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────────────────
// appendFallbackItem caps at 3 entries.
// ─────────────────────────────────────────────────────────────────────────────

func TestAppendFallbackItem_CapsAt3(t *testing.T) {
	got := []string{}
	got = appendFallbackItem(got, "one")
	got = appendFallbackItem(got, "two")
	got = appendFallbackItem(got, "three")
	got = appendFallbackItem(got, "four") // should be ignored
	if len(got) != 3 {
		t.Errorf("expected 3 fallback items, got %d (%v)", len(got), got)
	}
	if got[2] != "three" {
		t.Errorf("expected third entry to remain 'three', got %q", got[2])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// appendWorkflowSessionLog — exercise additional fields populated.
// ─────────────────────────────────────────────────────────────────────────────

func TestAppendWorkflowSessionLog_WritesFullEntry(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session-log.md")
	cp := workflowCheckpoint{
		Timestamp:  "2026-04-30T10:11:12Z",
		Git:        workflowGitSummary{Branch: "br", SHA: "abc"},
		Message:    "test message",
		NextAction: "do thing",
	}
	cp.Files.Modified = []string{"a.go", "b.go"}
	cp.Verification.Status = "pass"
	cp.Verification.Summary = "all green"
	if err := appendWorkflowSessionLog(logPath, cp); err != nil {
		t.Fatalf("appendWorkflowSessionLog: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"2026-04-30T10:11:12Z", "branch: br", "sha: abc", "files: 2", "test message"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in session log, got:\n%s", want, body)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// completedPlanStatus + pendingPlanItem branch coverage helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestCompletedPlanStatus(t *testing.T) {
	if isC, ok := completedPlanStatus("Status: Completed"); !ok || !isC {
		t.Errorf("Completed should parse to (true,true), got (%v,%v)", isC, ok)
	}
	if isC, ok := completedPlanStatus("Status: in_progress"); !ok || isC {
		t.Errorf("in_progress should parse to (false,true)")
	}
	if _, ok := completedPlanStatus("- bullet"); ok {
		t.Error("non-status line should return ok=false")
	}
}

func TestPendingPlanItem(t *testing.T) {
	if got, ok := pendingPlanItem("- [ ] my task"); !ok || got != "my task" {
		t.Errorf("got (%q,%v), want (my task,true)", got, ok)
	}
	if _, ok := pendingPlanItem("- [x] done"); ok {
		t.Error("checked item should return ok=false")
	}
	if _, ok := pendingPlanItem("not a bullet"); ok {
		t.Error("non-bullet should return ok=false")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// renderEligibleOutput — empty branch with no draft plans
// ─────────────────────────────────────────────────────────────────────────────

func TestRenderEligibleOutput_NoMaxBatchNoDrafts(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{},
		TotalEligible: 0,
	}
	stdout, _ := captureCovStdout(t, func() error {
		renderEligibleOutput(out, 2, 0)
		return nil
	})
	if strings.Contains(stdout, "max batch") {
		t.Errorf("expected no max batch line, got: %s", stdout)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// firstReadableDelegationContract — covers the closed contract skip path
// ─────────────────────────────────────────────────────────────────────────────

func TestFirstReadableDelegationContract_PrefersActive(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	closed := &DelegationContract{
		SchemaVersion: 1, ID: "del-closed", ParentPlanID: "pc", ParentTaskID: "tc",
		Title: "x", WriteScope: []string{"commands/"}, Status: "completed",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, closed); err != nil {
		t.Fatal(err)
	}
	active := &DelegationContract{
		SchemaVersion: 1, ID: "del-active", ParentPlanID: "pa", ParentTaskID: "ta",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, active); err != nil {
		t.Fatal(err)
	}
	c := firstReadableDelegationContract(dir)
	if c == nil || c.Status != "active" {
		t.Fatalf("expected active contract, got %+v", c)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// renderOrientRecentSessionsSection — long-timestamp truncation branch
// ─────────────────────────────────────────────────────────────────────────────

func TestRenderOrientRecentSessionsSection_TimestampTruncated(t *testing.T) {
	state := &workflowOrientState{
		RecentSessions: []branchSessionInfo{
			{Platform: "claude", SessionID: "x", Timestamp: "2026-04-30T10:11:12Z-extra-stuff", MessageCount: 1},
		},
	}
	out, _ := captureCovStdout(t, func() error {
		renderOrientRecentSessionsSection(state, os.Stdout)
		return nil
	})
	// Timestamp should be truncated to 16 chars at most.
	if strings.Contains(out, "extra-stuff") {
		t.Errorf("expected timestamp truncated, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowVerifyRecordDispatch — error paths
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowVerifyRecordDispatch_KindRequired(t *testing.T) {
	err := runWorkflowVerifyRecordDispatch(verifyRecordDispatchInputs{Summary: "x"})
	if err == nil || !strings.Contains(err.Error(), "--kind is required") {
		t.Errorf("expected --kind required error, got: %v", err)
	}
}

func TestRunWorkflowVerifyRecordDispatch_SummaryRequired(t *testing.T) {
	err := runWorkflowVerifyRecordDispatch(verifyRecordDispatchInputs{Kind: "test"})
	if err == nil || !strings.Contains(err.Error(), "--summary is required") {
		t.Errorf("expected --summary required error, got: %v", err)
	}
}

func TestRunWorkflowVerifyRecordDispatch_StatusRequiredForNonReview(t *testing.T) {
	err := runWorkflowVerifyRecordDispatch(verifyRecordDispatchInputs{Kind: "test", Summary: "x"})
	if err == nil || !strings.Contains(err.Error(), "--status is required") {
		t.Errorf("expected --status required error for non-review kind, got: %v", err)
	}
}

// dispatchVerifyRecordReview enforces flag combos for kind=review.
func TestDispatchVerifyRecordReview_StatusForbidden(t *testing.T) {
	err := dispatchVerifyRecordReview(verifyRecordDispatchInputs{
		Kind: "review", Summary: "x", Status: "pass",
	})
	if err == nil || !strings.Contains(err.Error(), "--status must not be set") {
		t.Errorf("expected --status forbidden error, got: %v", err)
	}
}

func TestDispatchVerifyRecordReview_RequiresPhaseDecisions(t *testing.T) {
	err := dispatchVerifyRecordReview(verifyRecordDispatchInputs{
		Kind: "review", Summary: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "--phase1-decision and --phase2-decision are required") {
		t.Errorf("expected phase-decision-required error, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// writeFoldBackArtifact + loadFoldBackArtifactByID round-trip
// ─────────────────────────────────────────────────────────────────────────────

func TestWriteAndLoadFoldBackArtifact(t *testing.T) {
	dir := t.TempDir()
	artifact := foldBackArtifact{
		ID:             "fold-test-1",
		PlanID:         "p1",
		TaskID:         "t1",
		Classification: "small",
		Observation:    "tests should cover this",
		RoutedTo:       "task-note",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeFoldBackArtifact(dir, artifact); err != nil {
		t.Fatalf("writeFoldBackArtifact: %v", err)
	}
	got, err := loadFoldBackArtifactByID(dir, artifact.ID)
	if err != nil {
		t.Fatalf("loadFoldBackArtifactByID: %v", err)
	}
	if got.ID != artifact.ID || got.PlanID != "p1" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestLoadFoldBackArtifactByID_MissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadFoldBackArtifactByID(dir, "does-not-exist"); err == nil {
		t.Error("expected error for missing artifact")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// resolvePreferences error paths
// ─────────────────────────────────────────────────────────────────────────────

func TestResolvePreferences_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	t.Setenv("HOME", tmp)
	prefs, err := resolvePreferences(tmp, "workflow-test-empty-proj")
	if err != nil {
		t.Fatalf("resolvePreferences: %v", err)
	}
	// Should return defaults.
	if prefs.Verification.TestCommand == nil {
		t.Error("expected default test_command")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// graphSearchSubdir — directory missing
// ─────────────────────────────────────────────────────────────────────────────

func TestGraphSearchSubdir_MissingDir(t *testing.T) {
	seen := map[string]bool{}
	var results []GraphBridgeResult
	// Non-existent directory — function must return cleanly.
	graphSearchSubdir(t.TempDir(), "nope", "x", seen, &results, 10)
	if len(results) != 0 {
		t.Errorf("expected zero results from missing dir, got %d", len(results))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// readWorkflowPlanTitle — covers fallback to filename.
// ─────────────────────────────────────────────────────────────────────────────

func TestReadWorkflowPlanTitle_NoHeader(t *testing.T) {
	got := readWorkflowPlanTitle([]string{"- not a header"}, "/path/to/myplan.md")
	if got != "myplan.md" {
		t.Errorf("expected fallback to filename, got %q", got)
	}
}

func TestReadWorkflowPlanTitle_EmptyLines(t *testing.T) {
	got := readWorkflowPlanTitle([]string{}, "/path/to/named.md")
	if got != "named.md" {
		t.Errorf("expected fallback to filename for empty lines, got %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowComplete JSON with no plans (drained)
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowGraphQuery — success path with seeded local adapter (no kg cli).
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowGraphQuery_SuccessLocalAdapter(t *testing.T) {
	project := t.TempDir()
	kgHome := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("KG_HOME", kgHome)
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", t.TempDir())

	// Self-initialize the adapter requirements: config.yaml + a decisions note.
	if err := os.MkdirAll(filepath.Join(kgHome, "self"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kgHome, "self", "config.yaml"), []byte("version: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	notesDir := filepath.Join(kgHome, "notes", "decisions")
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		t.Fatal(err)
	}
	note := "---\nid: dec-1\ntitle: Decision\nsummary: chosen\n---\n\nbody about loops\n"
	if err := os.WriteFile(filepath.Join(notesDir, "dec-1.md"), []byte(note), 0644); err != nil {
		t.Fatal(err)
	}

	cfgDir := filepath.Join(project, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := []byte("schema_version: 1\nenabled: true\ngraph_home: \"" + kgHome + "\"\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), cfg, 0644); err != nil {
		t.Fatal(err)
	}

	chdirForCov(t, project)
	cmd := newGraphQueryTestCommand("decision_lookup", "")
	out, _ := captureCovStdout(t, func() error {
		return runWorkflowGraphQuery(cmd, []string{"loops"})
	})
	if !strings.Contains(out, "Graph Query") {
		t.Errorf("expected graph query header, got: %s", out)
	}
}

func TestRunWorkflowComplete_DrainedJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("anything") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "\"state\": \"drained\"") {
		t.Errorf("expected drained state in JSON, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// checkPreVerifierTDDGate — happy + error paths
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckPreVerifierTDDGate_SkipFlag(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"commands/x.go"}, true, true); err != nil {
		t.Errorf("skip=true should bypass gate, got: %v", err)
	}
}

func TestCheckPreVerifierTDDGate_VerificationNotRequired(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"commands/x.go"}, false, false); err != nil {
		t.Errorf("verification_required=false should bypass gate, got: %v", err)
	}
}

func TestCheckPreVerifierTDDGate_NonGoScope(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"docs/x.md"}, true, false); err != nil {
		t.Errorf("non-Go write_scope should bypass gate, got: %v", err)
	}
}

func TestCheckPreVerifierTDDGate_HasAdjacentTests(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "x_test.go"), []byte("package commands\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPreVerifierTDDGate(dir, []string{"commands/x.go"}, true, false); err != nil {
		t.Errorf("adjacent test should bypass gate, got: %v", err)
	}
}

func TestCheckPreVerifierTDDGate_NoTestsFails(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := checkPreVerifierTDDGate(dir, []string{"commands/x.go"}, true, false); err == nil {
		t.Error("expected TDD gate failure when verification required, Go scope, no tests")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// writeScopeHasAdjacentGoTests — directory scope
// ─────────────────────────────────────────────────────────────────────────────

func TestWriteScopeHasAdjacentGoTests_DirectoryWithTest(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "internal", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo_test.go"), []byte("package foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !writeScopeHasAdjacentGoTests(dir, []string{"internal/foo"}) {
		t.Error("expected directory-scope to detect adjacent test file")
	}
}

func TestWriteScopeHasAdjacentGoTests_NoTests(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "internal", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if writeScopeHasAdjacentGoTests(dir, []string{"internal/foo/main.go"}) {
		t.Error("expected no adjacent tests when none on disk")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// writeScopeImpliesNonTestGo
// ─────────────────────────────────────────────────────────────────────────────

func TestWriteScopeImpliesNonTestGo(t *testing.T) {
	if !writeScopeImpliesNonTestGo([]string{"x.go"}) {
		t.Error("plain .go file should imply non-test Go scope")
	}
	if writeScopeImpliesNonTestGo([]string{"x_test.go"}) {
		t.Error("_test.go should not imply non-test Go scope")
	}
	if writeScopeImpliesNonTestGo([]string{"docs/x.md"}) {
		t.Error("non-Go file should not imply non-test Go scope")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// validateGraphBridgeIntent — already-valid intent and unknown.
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateGraphBridgeIntent_NoAllowlist(t *testing.T) {
	if err := validateGraphBridgeIntent("decision_lookup", nil); err != nil {
		t.Errorf("nil allowlist should be passthrough, got: %v", err)
	}
}

func TestValidateGraphBridgeIntent_NotInAllowlist(t *testing.T) {
	err := validateGraphBridgeIntent("decision_lookup", []string{"plan_context"})
	if err == nil || !strings.Contains(err.Error(), "not in allowed_intents") {
		t.Errorf("expected 'not in allowed_intents' error, got: %v", err)
	}
}

func TestValidateGraphBridgeIntent_Unknown(t *testing.T) {
	err := validateGraphBridgeIntent("totally_invalid", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown intent") {
		t.Errorf("expected unknown intent error, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// iter_log helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestLoadIterLogDocument_InvalidYAML(t *testing.T) {
	if _, err := loadIterLogDocument([]byte("not: : valid")); err == nil {
		t.Error("expected parse error for invalid YAML")
	}
}

func TestLoadIterLogDocument_V1Migration(t *testing.T) {
	v1 := []byte(`schema_version: 1
iteration: 5
date: "2026-04-30"
wave: w
task_id: t1
commit: abc
files_changed: 3
lines_added: 10
lines_removed: 2
first_commit: true
item: thing
summary: did it
scope_note: ok
feedback_goal: cover
retries: 0
tests_added: 1
tests_total_pass: 1
self_assessment:
  read_loop_state: true
  one_item_only: true
  committed_after_tests: true
  aligned_with_canonical_tasks: true
  persisted_via_workflow_commands: true
  stayed_under_10_files: true
  no_destructive_commands: true
`)
	got, err := loadIterLogDocument(v1)
	if err != nil {
		t.Fatalf("loadIterLogDocument(v1): %v", err)
	}
	if got.SchemaVersion != 2 {
		t.Errorf("expected migration to schema_version=2, got %d", got.SchemaVersion)
	}
	if got.Iteration != 5 {
		t.Errorf("expected iteration=5, got %d", got.Iteration)
	}
}

func TestLoadIterLogDocument_V2(t *testing.T) {
	v2 := []byte(`schema_version: 2
iteration: 7
date: "2026-04-30"
wave: w
task_id: t1
commit: abc
impl:
  feedback_goal: cover
`)
	got, err := loadIterLogDocument(v2)
	if err != nil {
		t.Fatalf("loadIterLogDocument(v2): %v", err)
	}
	if got.SchemaVersion != 2 || got.Iteration != 7 {
		t.Errorf("unexpected entry: %+v", got)
	}
	if got.Verifiers == nil {
		t.Error("expected non-nil verifiers slice")
	}
}

func TestMigrateIterLogV1Legacy_BasicFields(t *testing.T) {
	v1 := &iterLogV1Legacy{
		SchemaVersion: 1,
		Iteration:     3,
		Date:          "2026-04-30",
		Wave:          "w",
		TaskID:        "t1",
		Item:          "item",
		Summary:       "did it",
	}
	got := migrateIterLogV1Legacy(v1)
	if got.SchemaVersion != 2 {
		t.Errorf("schema=%d, want 2", got.SchemaVersion)
	}
	if got.Impl.Item != "item" || got.Impl.Summary != "did it" {
		t.Errorf("expected fields migrated to Impl block, got %+v", got.Impl)
	}
	if got.Verifiers == nil {
		t.Error("verifiers should be initialized")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// loadCheckScopeSidecar — happy path with valid sidecar
// ─────────────────────────────────────────────────────────────────────────────

func TestLoadCheckScopeSidecar_ValidSidecar(t *testing.T) {
	dir := t.TempDir()
	ev := NewScopeEvidence("plan-x", "task-y")
	ev.FinalWriteScope = []string{"commands/x.go"}
	if _, err := persistScopeEvidenceSidecar(dir, "plan-x", "task-y", ev); err != nil {
		t.Fatalf("persist sidecar: %v", err)
	}
	path, got, err := loadCheckScopeSidecar(dir, "plan-x", "task-y")
	if err != nil {
		t.Fatalf("loadCheckScopeSidecar: %v", err)
	}
	if path == "" || got == nil {
		t.Errorf("expected path+evidence, got path=%q ev=%v", path, got)
	}
	if got.PlanID != "plan-x" {
		t.Errorf("expected plan-x, got %q", got.PlanID)
	}
}

func TestLoadCheckScopeSidecar_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	// Write an invalid YAML sidecar.
	path := deriveScopeEvidencePath(dir, "plan-x", "task-y")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not: : valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCheckScopeSidecar(dir, "plan-x", "task-y"); err == nil {
		t.Error("expected parse error on invalid yaml")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// persistScopeEvidenceSidecar — round-trip including yaml marshal error path
// ─────────────────────────────────────────────────────────────────────────────

func TestPersistScopeEvidenceSidecar_HappyPath(t *testing.T) {
	dir := t.TempDir()
	ev := NewScopeEvidence("plan-a", "task-b")
	ev.Confidence = "high"
	got, err := persistScopeEvidenceSidecar(dir, "plan-a", "task-b", ev)
	if err != nil {
		t.Fatalf("persistScopeEvidenceSidecar: %v", err)
	}
	if got == "" {
		t.Error("expected path returned")
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("file should exist at %s: %v", got, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// renderEligibleTask — empty conflicts
// ─────────────────────────────────────────────────────────────────────────────

func TestRenderEligibleTask_NoConflicts(t *testing.T) {
	at := AnnotatedTask{
		workflowNextTaskSuggestion: workflowNextTaskSuggestion{
			PlanID: "p1", TaskID: "t1", TaskTitle: "T", Status: "pending",
			WriteScope: []string{"commands/"},
		},
		WriteScopeDeclared: true,
		HasEvidence:        false,
		EvidenceConfidence: "none",
	}
	out, _ := captureCovStdout(t, func() error {
		renderEligibleTask(at)
		return nil
	})
	if strings.Contains(out, "conflicts:") {
		t.Errorf("expected no conflicts line when empty, got: %s", out)
	}
}
