// Package workflow — fifth batch of coverage tests targeting long-tail
// branches across state.go, prefs.go, verification.go, plan_task.go, and
// graph.go. Each test isolates a single err-return or branch that the
// previous coverage pushes missed.
package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// chmodUnreadable sets a path to 0o000 and restores 0644 on cleanup. Skips
// the test on platforms where chmod cannot reliably make a file unreadable
// for the current user (e.g. running as root in some CI environments).
func chmodUnreadable(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod unreadable not supported on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("chmod unreadable unreliable as root")
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod 0: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
}

// chmodUnreadableDir locks a directory at 0o000 and restores 0755 on cleanup.
func chmodUnreadableDir(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod unreadable not supported on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("chmod unreadable unreliable as root")
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod 0: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

// ─── state.go: loadWorkflowCheckpoint ReadFile non-NotExist err branch ──────

func TestLoadWorkflowCheckpoint_ReadFileError(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	contextDir := filepath.Join(agentsHome, "context", "proj-unreadable")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatal(err)
	}
	cpPath := filepath.Join(contextDir, "checkpoint.yaml")
	if err := os.WriteFile(cpPath, []byte("schema_version: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, cpPath)

	cp, warnings := loadWorkflowCheckpoint("proj-unreadable")
	if cp != nil {
		t.Fatalf("expected nil checkpoint on read error, got %+v", cp)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "checkpoint unreadable") {
		t.Fatalf("expected unreadable warning, got %v", warnings)
	}
}

// ─── state.go: runWorkflowLog ReadFile non-NotExist err branch ──────────────

func TestRunWorkflowLog_ReadError(t *testing.T) {
	repo := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	contextDir := filepath.Join(agentsHome, "context", filepath.Base(repo))
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(contextDir, "session-log.md")
	if err := os.WriteFile(logPath, []byte("# log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, logPath)

	chdirForCov(t, repo)
	err := runWorkflowLog(false)
	if err == nil {
		t.Fatal("expected ReadFile error to propagate")
	}
}

// ─── state.go: firstMarkdownTitle ReadFile error ────────────────────────────

func TestFirstMarkdownTitle_ReadError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "note.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, path)

	_, err := firstMarkdownTitle(path)
	if err == nil {
		t.Fatal("expected ReadFile error")
	}
}

// ─── state.go: collectWorkflowHandoffs propagates firstMarkdownTitle err ─────

func TestCollectWorkflowHandoffs_PropagatesReadErr(t *testing.T) {
	repo := t.TempDir()
	handoffDir := filepath.Join(repo, ".agents", "active", "handoffs")
	if err := os.MkdirAll(handoffDir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(handoffDir, "bad.md")
	if err := os.WriteFile(bad, []byte("# h\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, bad)

	_, err := collectWorkflowHandoffs(repo)
	if err == nil {
		t.Fatal("expected handoff read error to propagate")
	}
}

// ─── state.go: collectWorkflowPlans propagates readWorkflowPlan err ─────────

func TestCollectWorkflowPlans_PropagatesReadErr(t *testing.T) {
	repo := t.TempDir()
	planDir := filepath.Join(repo, ".agents", "active")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(planDir, "x.plan.md")
	if err := os.WriteFile(bad, []byte("# plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, bad)

	_, err := collectWorkflowPlans(repo)
	if err == nil {
		t.Fatal("expected plan read error to propagate")
	}
}

// ─── state.go: collectWorkflowPlanItems pending break at 3 ──────────────────

func TestCollectWorkflowPlanItems_PendingCapped(t *testing.T) {
	lines := []string{
		"# Plan",
		"- [ ] a",
		"- [ ] b",
		"- [ ] c",
		"- [ ] should-not-appear",
	}
	pending, _, _ := collectWorkflowPlanItems(lines)
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending (cap), got %d: %v", len(pending), pending)
	}
}

func TestAppendFallbackItem_CappedAtThree(t *testing.T) {
	out := appendFallbackItem([]string{"a", "b", "c"}, "d")
	if len(out) != 3 {
		t.Fatalf("expected 3 (no append), got %d: %v", len(out), out)
	}
}

// ─── plan_task.go: runWorkflowPlanList prints "unreadable" for bad plan ─────

func TestRunWorkflowPlanList_UnreadablePlanRendered(t *testing.T) {
	repo := setupTestProject(t)
	// Corrupt PLAN.yaml so loadCanonicalPlan returns an error.
	planPath := filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml")
	if err := os.WriteFile(planPath, []byte("not: [valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, runWorkflowPlanList)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "unreadable") {
		t.Fatalf("expected unreadable in output, got: %s", out)
	}
}

// ─── plan_task.go: appendPlanGraphSliceNodes warns on unknown parent task ───

func TestAppendPlanGraphSliceNodes_UnknownParentWarn(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	slices := []CanonicalSlice{{ID: "s1", ParentTaskID: "missing", Title: "x"}}
	taskIDs := map[string]string{} // empty -> parent lookup fails
	out := appendPlanGraphSliceNodes(graph, plan, slices, taskIDs)
	if len(out) != 0 {
		t.Fatalf("expected no slice IDs registered, got %d", len(out))
	}
	if len(graph.Warnings) == 0 || !strings.Contains(graph.Warnings[0], "unknown parent task") {
		t.Fatalf("expected unknown-parent warning, got %v", graph.Warnings)
	}
}

// ─── plan_task.go: buildTaskConflictGraph nil ConflictsWith branch ──────────

func TestBuildTaskConflictGraph_NilConflicts(t *testing.T) {
	tasks := []workflowNextTaskSuggestion{
		{TaskID: "t1", ConflictsWith: nil},
		{TaskID: "t2", ConflictsWith: []string{"t1"}},
	}
	g := buildTaskConflictGraph(tasks)
	if got, ok := g["t1"]; !ok || got == nil || len(got) != 0 {
		t.Fatalf("expected empty []string for nil ConflictsWith, got %v ok=%v", got, ok)
	}
	if g["t2"][0] != "t1" {
		t.Fatalf("expected t1 in t2 conflicts, got %v", g["t2"])
	}
}

// ─── plan_task.go: rankNextTaskCandidate in_progress without focus ──────────

func TestRankNextTaskCandidate_InProgressNoFocusMatch(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1, ID: "p1", Title: "P1", Status: "active",
		CreatedAt:        "2026-04-10T00:00:00Z",
		UpdatedAt:        "2026-04-10T00:00:00Z",
		CurrentFocusTask: "some other title",
	}
	if err := saveCanonicalPlan(repo, &plan); err != nil {
		t.Fatal(err)
	}
	sug := workflowNextTaskSuggestion{
		PlanID:    "p1",
		TaskID:    "t1",
		TaskTitle: "in-progress-not-focus",
		Status:    "in_progress",
	}
	out, priority := rankNextTaskCandidate(repo, sug)
	if priority != 1 {
		t.Fatalf("expected priority 1 for in_progress no focus match, got %d (reason=%q)", priority, out.Reason)
	}
	if !strings.Contains(out.Reason, "in progress") {
		t.Fatalf("expected 'in progress' reason, got %q", out.Reason)
	}
}

// ─── plan_task.go: runWorkflowPlanGraph JSON path ───────────────────────────

func TestRunWorkflowPlanGraph_JSON_AllPlans(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanGraph("") })
	if err != nil {
		t.Fatalf("runWorkflowPlanGraph json: %v", err)
	}
	if !strings.Contains(out, `"nodes"`) {
		t.Fatalf("expected JSON output with nodes field, got: %s", out)
	}
}

// ─── prefs.go: loadRepoPreferences ReadFile (non-NotExist) error ────────────

func TestLoadRepoPreferences_ReadError(t *testing.T) {
	repo := t.TempDir()
	prefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	prefsPath := filepath.Join(prefsDir, "preferences.yaml")
	if err := os.WriteFile(prefsPath, []byte("planning:\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, prefsPath)

	_, err := loadRepoPreferences(repo)
	if err == nil {
		t.Fatal("expected ReadFile error to propagate")
	}
}

// ─── prefs.go: resolvePreferences propagates load errors ────────────────────

func TestResolvePreferences_RepoLoadError(t *testing.T) {
	repo := t.TempDir()
	prefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(prefsDir, "preferences.yaml")
	if err := os.WriteFile(bad, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := resolvePreferences(repo, "p")
	if err == nil {
		t.Fatal("expected malformed YAML to surface")
	}
}

func TestResolvePreferences_LocalLoadError(t *testing.T) {
	repo := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	// Valid repo prefs (or absent), corrupt local prefs.
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctx, "preferences.local.yaml"), []byte(":\n  - bad"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := resolvePreferences(repo, "p")
	if err == nil {
		t.Fatal("expected local malformed YAML to surface")
	}
}

// ─── verification.go: runWorkflowVerifyLog JSON path ────────────────────────

func TestRunWorkflowVerifyLog_JSON_Push5(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	rec := VerificationRecord{
		SchemaVersion: 1, Kind: "test", Status: "pass",
		Timestamp: "2026-05-12T00:00:00Z", Summary: "ok",
	}
	if err := appendVerificationLog(filepath.Base(repo), rec); err != nil {
		t.Fatal(err)
	}
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(false) })
	if err != nil {
		t.Fatalf("verify log json: %v", err)
	}
	if !strings.Contains(out, `"kind"`) {
		t.Fatalf("expected JSON output, got: %s", out)
	}
}

func TestRunWorkflowVerifyLog_Empty_Push5(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(true) })
	if err != nil {
		t.Fatalf("verify log: %v", err)
	}
	if !strings.Contains(out, "No verification records") {
		t.Fatalf("expected empty-log message, got: %s", out)
	}
}

func TestRunWorkflowVerifyLog_RenderAllIcons(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	for _, s := range []string{"pass", "fail", "partial", "unknown"} {
		rec := VerificationRecord{
			SchemaVersion: 1, Kind: "test", Status: s,
			Timestamp: "2026-05-12T00:00:00Z", Summary: "row-" + s,
			Command: "go test",
		}
		if err := appendVerificationLog(filepath.Base(repo), rec); err != nil {
			t.Fatal(err)
		}
	}
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(true) })
	if err != nil {
		t.Fatalf("verify log: %v", err)
	}
	for _, marker := range []string{"row-fail", "row-partial", "row-unknown", "go test"} {
		if !strings.Contains(out, marker) {
			t.Fatalf("missing %q in output: %s", marker, out)
		}
	}
}

// ─── verification.go: runWorkflowVerifyLog readVerificationLog error ────────

func TestRunWorkflowVerifyLog_ReadError(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	// Pre-create the log file then make it unreadable.
	ctx := filepath.Join(agentsHome, "context", filepath.Base(repo))
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(ctx, "verification-log.jsonl")
	if err := os.WriteFile(logPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, logPath)
	chdirForCov(t, repo)
	if err := runWorkflowVerifyLog(false); err == nil {
		t.Fatal("expected read error to propagate")
	}
}

// ─── verification.go: validateVerifyRecordInputs review-kind internal err ───

func TestValidateVerifyRecordInputs_ReviewKindRejected(t *testing.T) {
	err := validateVerifyRecordInputs("review", "pass", "package")
	if err == nil || !strings.Contains(err.Error(), "use runWorkflowVerifyRecordReview") {
		t.Fatalf("expected internal-use-review-fn error, got %v", err)
	}
}

// ─── verification.go: resolveReviewOverallDecision branches ─────────────────

func TestResolveReviewOverallDecision_OverallDisagreesWithDerived(t *testing.T) {
	_, err := resolveReviewOverallDecision("accept", "accept", "reject", "")
	if err == nil || !strings.Contains(err.Error(), "disagrees with phases") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestResolveReviewOverallDecision_EscalateWithoutReason(t *testing.T) {
	_, err := resolveReviewOverallDecision("escalate", "accept", "escalate", "")
	if err == nil || !strings.Contains(err.Error(), "escalation-reason is empty") {
		t.Fatalf("expected escalation-empty error, got %v", err)
	}
}

func TestResolveReviewOverallDecision_DerivedFromEmpty(t *testing.T) {
	out, err := resolveReviewOverallDecision("accept", "accept", "", "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != "accept" {
		t.Fatalf("expected accept derived, got %q", out)
	}
}

// ─── verification.go: resolveReviewDelegationContract no-contracts ──────────

func TestResolveReviewDelegationContract_NoContractsHint(t *testing.T) {
	repo := t.TempDir()
	_, _, err := resolveReviewDelegationContract(repo, "")
	if err == nil || !strings.Contains(err.Error(), "needs a delegation task id") {
		t.Fatalf("expected hint error, got %v", err)
	}
}

// ─── verification.go: writeVerifyResultArtifact invalid stem ────────────────

func TestWriteVerifyResultArtifact_InvalidStem(t *testing.T) {
	_, err := writeVerifyResultArtifact(verifyResultArtifactInputs{
		ProjectPath: t.TempDir(), TaskID: "t1", Kind: "test",
		VerifierType: "BAD-STEM!", Now: "2026-05-12T00:00:00Z",
	})
	if err == nil || !strings.Contains(err.Error(), "not a valid artifact stem") {
		t.Fatalf("expected invalid-stem error, got %v", err)
	}
}

// ─── verification.go: writeVerifyResultArtifact loadDelegationContract err ──

func TestWriteVerifyResultArtifact_ContractMissing(t *testing.T) {
	repo := t.TempDir()
	_, err := writeVerifyResultArtifact(verifyResultArtifactInputs{
		ProjectPath: repo, TaskID: "no-such-task", Kind: "test",
		VerifierType: "unit", Now: "2026-05-12T00:00:00Z",
	})
	if err == nil || !strings.Contains(err.Error(), "load delegation contract") {
		t.Fatalf("expected load-contract error, got %v", err)
	}
}

// ─── verification.go: writeVerifyResultArtifact writeVerificationResultYAML err

func TestWriteVerifyResultArtifact_WriteYAMLErr(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-x", "plan-x", "deleg-x")

	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	_, err := writeVerifyResultArtifact(verifyResultArtifactInputs{
		ProjectPath: repo, TaskID: "task-x", Kind: "test", Status: "pass",
		Summary: "ok", VerifierType: "unit", Now: "2026-05-12T00:00:00Z",
	})
	if err == nil {
		t.Fatal("expected error from yaml stub")
	}
	// The yamlMarshal stub fires inside writeVerificationResultYAML; the
	// wrapper prepends "write verification result artifact:" so we cannot
	// use errors.Is — match on substring instead.
	if !strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("expected sentinel %q in error, got %v", sentinel, err)
	}
}

// ─── verification.go: runWorkflowVerifyRecord propagates writeArtifact err ──

func TestRunWorkflowVerifyRecord_WriteArtifactErrPropagates(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind: "test", Status: "pass", Scope: "package",
		Summary: "x", TaskID: "task-001", // no contract -> writeVerifyResultArtifact fails
	})
	if err == nil || !strings.Contains(err.Error(), "load delegation contract") {
		t.Fatalf("expected propagated load-contract error, got %v", err)
	}
}

// ─── verification.go: runWorkflowVerifyRecord appendVerificationLog err ─────

func TestRunWorkflowVerifyRecord_AppendLogErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })
	err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind: "test", Status: "pass", Scope: "package", Summary: "x",
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel from append log, got %v", err)
	}
}

// ─── verification.go: runWorkflowVerifyRecordReview writeReviewDecisionYAML err

func TestRunWorkflowVerifyRecordReview_WriteYAMLErr(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "deleg-r")
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowVerifyRecordReview(reviewRecordInputs{
		Command: "review", Scope: "repo", Summary: "ok",
		Phase1In: "accept", Phase2In: "accept", OverallIn: "accept",
		TaskFlag: "task-001",
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

// ─── verification.go: runWorkflowVerifyRecordReview invalid scope branch ────

func TestRunWorkflowVerifyRecordReview_InvalidScope(t *testing.T) {
	err := runWorkflowVerifyRecordReview(reviewRecordInputs{Scope: "BAD"})
	if err == nil || !strings.Contains(err.Error(), "invalid scope") {
		t.Fatalf("expected invalid-scope error, got %v", err)
	}
}

// ─── graph.go: loadGraphBridgeConfig non-NotExist ReadFile error ────────────

func TestLoadGraphBridgeConfig_ReadError(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "graph-bridge.yaml")
	if err := os.WriteFile(cfgPath, []byte("enabled: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, cfgPath)

	_, err := loadGraphBridgeConfig(repo)
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestLoadGraphBridgeConfig_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph-bridge.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadGraphBridgeConfig(repo)
	if err == nil || !strings.Contains(err.Error(), "parse graph-bridge.yaml") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// ─── state.go: runWorkflowStatus + runWorkflowOrient JSON paths ─────────────

func TestRunWorkflowStatus_JSON(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, runWorkflowStatus)
	if err != nil {
		t.Fatalf("status json: %v", err)
	}
	if !strings.Contains(out, `"project"`) {
		t.Fatalf("expected JSON output, got: %s", out)
	}
}

// ─── plan_task.go: collectCanonicalPlans listCanonicalPlanIDs error ─────────

func TestCollectCanonicalPlans_ListIDsErr(t *testing.T) {
	repo := t.TempDir()
	base := filepath.Join(repo, ".agents", "workflow", "plans")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, base)

	_, warnings := collectCanonicalPlans(repo)
	if len(warnings) == 0 || !strings.Contains(warnings[0], "canonical plans unreadable") {
		t.Fatalf("expected plans-unreadable warning, got %v", warnings)
	}
}

// ─── plan_task.go: collectDraftPlanIDs listCanonicalPlanIDs error ───────────

func TestCollectDraftPlanIDs_ListIDsErr(t *testing.T) {
	repo := t.TempDir()
	base := filepath.Join(repo, ".agents", "workflow", "plans")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, base)

	out := collectDraftPlanIDs(repo)
	if out == nil || len(out) != 0 {
		t.Fatalf("expected non-nil empty slice on error, got %v", out)
	}
}

// ─── plan_task.go: collectDraftPlanIDs skip plans with load error ───────────

func TestCollectDraftPlanIDs_SkipsLoadErr(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "bad")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	// PLAN.yaml exists but unparseable -> listed but load fails -> skipped.
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), []byte(":\n  bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	out := collectDraftPlanIDs(repo)
	if len(out) != 0 {
		t.Fatalf("expected unreadable plan to be skipped, got %v", out)
	}
}

// ─── plan_task.go: listCanonicalPlanIDs propagates Stat err ─────────────────

func TestListCanonicalPlanIDs_ReadDirError(t *testing.T) {
	repo := t.TempDir()
	base := filepath.Join(repo, ".agents", "workflow", "plans")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, base)

	_, err := listCanonicalPlanIDs(repo)
	if err == nil {
		t.Fatal("expected ReadDir error")
	}
}

// ─── plan_task.go: deriveScopeConfidence branches ───────────────────────────

func TestDeriveScopeConfidence_NonCodeContextReady(t *testing.T) {
	got := deriveScopeConfidence("doc", false, true, false, 0)
	if got != "medium" {
		t.Fatalf("expected medium for doc + contextReady, got %q", got)
	}
}

func TestDeriveScopeConfidence_NonCodeContextNotReady(t *testing.T) {
	got := deriveScopeConfidence("doc", false, false, false, 0)
	if got != "low" {
		t.Fatalf("expected low, got %q", got)
	}
}

func TestDeriveScopeConfidence_CodeBothReady(t *testing.T) {
	got := deriveScopeConfidence("code", true, true, false, 0)
	if got != "low" {
		t.Fatalf("expected low (no scope inputs), got %q", got)
	}
}

func TestDeriveScopeConfidence_CodeReadyWithInputsNoQueries(t *testing.T) {
	got := deriveScopeConfidence("code", true, false, true, 0)
	if got != "medium" {
		t.Fatalf("expected medium, got %q", got)
	}
}

// ─── plan_task.go: deriveScopeMode research-marker branches ─────────────────

func TestDeriveScopeMode_ResearchMarker(t *testing.T) {
	got := deriveScopeMode(&CanonicalTask{Notes: "this is a research task"})
	if got != "research" {
		t.Fatalf("expected research, got %q", got)
	}
}

func TestDeriveScopeMode_DocOnlyWriteScope(t *testing.T) {
	got := deriveScopeMode(&CanonicalTask{WriteScope: []string{"docs/foo.md", "README.md"}})
	if got != "doc" {
		t.Fatalf("expected doc, got %q", got)
	}
}

// ─── verification.go: isValidVerificationScope default branch ──────────────

func TestIsValidVerificationScope_Invalid(t *testing.T) {
	if isValidVerificationScope("garbage") {
		t.Fatal("expected false for unknown scope")
	}
}

// ─── verification.go: isValidVerificationKind default branch ───────────────

func TestIsValidVerificationKind_Invalid(t *testing.T) {
	if isValidVerificationKind("garbage") {
		t.Fatal("expected false for unknown kind")
	}
}

// ─── state.go: gitOutput failure path ──────────────────────────────────────

func TestGitOutput_NotARepo(t *testing.T) {
	out := gitOutput(t.TempDir(), "rev-parse", "HEAD")
	if out != "" {
		t.Fatalf("expected empty output from non-repo, got %q", out)
	}
}

func TestGitModifiedFiles_NotARepo(t *testing.T) {
	files, err := gitModifiedFiles(t.TempDir())
	if err != nil {
		t.Fatalf("non-repo should not error, got %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected empty slice, got %v", files)
	}
}

// ─── cmd.go: top-level `log` RunE through cobra ────────────────────────────

func TestNewCmd_LogSubcommand(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := executeWorkflowCommand(t, repo, "log"); err != nil {
		t.Fatalf("workflow log: %v", err)
	}
	// Also exercise --all flag (covers logAll branch).
	if err := executeWorkflowCommand(t, repo, "log", "--all"); err != nil {
		t.Fatalf("workflow log --all: %v", err)
	}
}

// ─── cmd.go: positional-arg validators on log (NoArgsWithHints branch) ─────

func TestNewCmd_LogRejectsPositional(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := executeWorkflowCommand(t, repo, "log", "stray"); err == nil {
		t.Fatal("expected positional rejection")
	}
}

// ─── prefs.go: setLocalPreference key validation branch ─────────────────────

func TestSetLocalPreference_UnknownKey(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	err := runWorkflowPrefsSetLocal("unknown.key", "value")
	if err == nil || !strings.Contains(err.Error(), "unknown preference key") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}

func TestSetSharedPreference_UnknownKey(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowPrefsSetShared("unknown.key", "value")
	if err == nil || !strings.Contains(err.Error(), "unknown preference key") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}

// ─── plan_task.go: runWorkflowPlanCheckScope already-covered hints path ─────

// (placeholder kept short — full check-scope coverage lives in plan_check_scope_test.go)

// ─── delegation.go small target: mustGetStringSlice fallback branch ─────────

func TestMustGetStringSlice_NoFlagReturnsNil(t *testing.T) {
	c := &cobra.Command{}
	got := mustGetStringSlice(c, "absent-name")
	if got != nil {
		t.Fatalf("expected nil for missing flag, got %v", got)
	}
}

func TestMustGetStringSlice_WrongTypeReturnsNil(t *testing.T) {
	c := &cobra.Command{}
	// Register as plain String, then ask for StringSlice -> GetStringSlice errors.
	c.Flags().String("wrong-type", "", "")
	got := mustGetStringSlice(c, "wrong-type")
	if got != nil {
		t.Fatalf("expected nil for wrong-type flag, got %v", got)
	}
}

// ─── state.go: appendBranchSessions: empty branch short-circuits ────────────

func TestEnrichWorkflowState_EmptyBranchNoSessions(t *testing.T) {
	state := &workflowOrientState{
		Project: workflowProjectRef{Name: "p", Path: t.TempDir()},
		Git:     workflowGitSummary{Branch: ""}, // empty branch => no session lookup
	}
	enrichWorkflowState(state) // must not panic
	if len(state.RecentSessions) != 0 {
		t.Fatalf("expected no sessions for empty branch, got %d", len(state.RecentSessions))
	}
}

// ─── verification.go: resolveReviewDelegationContract with stale task flag ─

func TestResolveReviewDelegationContract_BadTaskID(t *testing.T) {
	repo := t.TempDir()
	_, _, err := resolveReviewDelegationContract(repo, "no-such")
	if err == nil || !strings.Contains(err.Error(), "load delegation contract") {
		t.Fatalf("expected load-contract error, got %v", err)
	}
}

// ─── verification.go: readVerificationLog with malformed JSON line ──────────

func TestReadVerificationLog_SkipsMalformedLine(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	rec := `{"schema_version":1,"kind":"test","status":"pass","summary":"a","timestamp":"2026-05-12T00:00:00Z"}` + "\n" +
		`not-json` + "\n"
	if err := os.WriteFile(filepath.Join(ctx, "verification-log.jsonl"), []byte(rec), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := readVerificationLog("p", 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 record (malformed skipped), got %d", len(out))
	}
}

// ─── verification.go: readVerificationLog limit truncates ───────────────────

func TestReadVerificationLog_LimitTrim(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	for i := 0; i < 5; i++ {
		buf.WriteString(fmt.Sprintf(`{"schema_version":1,"kind":"test","status":"pass","summary":"r%d","timestamp":"2026-05-12T00:00:0%dZ"}`+"\n", i, i))
	}
	if err := os.WriteFile(filepath.Join(ctx, "verification-log.jsonl"), []byte(buf.String()), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := readVerificationLog("p", 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 records after limit, got %d", len(out))
	}
}

// ─── verification.go: readVerificationLog ReadFile non-NotExist error ──────

func TestReadVerificationLog_ReadError(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(ctx, "verification-log.jsonl")
	if err := os.WriteFile(logPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, logPath)
	if _, err := readVerificationLog("p", 0); err == nil {
		t.Fatal("expected read error to propagate")
	}
}
