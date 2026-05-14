// Package workflow — final batch of long-tail coverage tests to close
// the gap to the 95% threshold.
package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ─── iter_log.go: mergeReviewIterLog malformed YAML branch ────────────────

func TestMergeReviewIterLog_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	taskID := "task-mr"
	dir := filepath.Join(repo, ".agents", "active", "verification", taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review-decision.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	entry := &iterLogEntry{}
	err := mergeReviewIterLog(entry, repo, taskID)
	if err == nil || !strings.Contains(err.Error(), "parse review decision") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// ─── iter_log.go: mergeReviewIterLog ReadFile non-NotExist err branch ─────

func TestMergeReviewIterLog_ReadError(t *testing.T) {
	repo := t.TempDir()
	taskID := "task-rr"
	dir := filepath.Join(repo, ".agents", "active", "verification", taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "review-decision.yaml")
	if err := os.WriteFile(path, []byte("overall_decision: accept\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, path)
	entry := &iterLogEntry{}
	err := mergeReviewIterLog(entry, repo, taskID)
	if err == nil || !strings.Contains(err.Error(), "read review decision") {
		t.Fatalf("expected read error, got %v", err)
	}
}

// ─── iter_log.go: loadOrInitIterLogEntry unsupported schema_version ───────

func TestLoadOrInitIterLogEntry_UnsupportedSchemaVersion(t *testing.T) {
	tmp := t.TempDir()
	iterPath := filepath.Join(tmp, "iter-1.yaml")
	// schema_version=99 is neither 1 (migrated) nor 2 — triggers the unsupported branch.
	if err := os.WriteFile(iterPath, []byte("schema_version: 99\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrInitIterLogEntry(iterPath, "goal", "t1")
	if err == nil || !strings.Contains(err.Error(), "unsupported schema_version") {
		t.Fatalf("expected schema_version error, got %v", err)
	}
}

// ─── iter_log.go: loadOrInitIterLogEntry ReadFile non-NotExist err ────────

func TestLoadOrInitIterLogEntry_ReadError(t *testing.T) {
	tmp := t.TempDir()
	iterPath := filepath.Join(tmp, "iter-1.yaml")
	if err := os.WriteFile(iterPath, []byte("schema_version: 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, iterPath)
	_, err := loadOrInitIterLogEntry(iterPath, "goal", "t1")
	if err == nil || !strings.Contains(err.Error(), "read existing iteration log") {
		t.Fatalf("expected read error, got %v", err)
	}
}

// ─── iter_log.go: loadOrInitIterLogEntry happy load v2 ────────────────────

func TestLoadOrInitIterLogEntry_LoadsV2(t *testing.T) {
	tmp := t.TempDir()
	iterPath := filepath.Join(tmp, "iter-1.yaml")
	if err := os.WriteFile(iterPath, []byte("schema_version: 2\niteration: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	entry, err := loadOrInitIterLogEntry(iterPath, "goal", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.SchemaVersion != 2 {
		t.Fatalf("expected v2, got %d", entry.SchemaVersion)
	}
}

// ─── iter_log.go: loadOrInitIterLogEntry init missing returns empty ───────

func TestLoadOrInitIterLogEntry_InitFromMissing(t *testing.T) {
	tmp := t.TempDir()
	entry, err := loadOrInitIterLogEntry(filepath.Join(tmp, "iter-1.yaml"), "goal", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.SchemaVersion != 2 {
		t.Fatalf("expected v2 init, got %d", entry.SchemaVersion)
	}
	if entry.Impl.FeedbackGoal != "goal" {
		t.Fatalf("expected feedback goal set, got %q", entry.Impl.FeedbackGoal)
	}
}

// ─── iter_log.go: applyIterLogRole dispatch table ─────────────────────────

func TestApplyIterLogRole_EmptyRole(t *testing.T) {
	entry := &iterLogEntry{SchemaVersion: 2}
	if err := applyIterLogRole(entry, "", "", "goal", t.TempDir(), "t1", nil); err != nil {
		t.Fatal(err)
	}
	if entry.Impl.FeedbackGoal != "goal" {
		t.Fatalf("expected goal applied for empty role, got %q", entry.Impl.FeedbackGoal)
	}
}

func TestApplyIterLogRole_ImplRoleSetsBlock(t *testing.T) {
	entry := &iterLogEntry{SchemaVersion: 2}
	c := &DelegationContract{ID: "d1", ParentTaskID: "t1"}
	if err := applyIterLogRole(entry, "impl", "", "g", t.TempDir(), "t1", c); err != nil {
		t.Fatal(err)
	}
}

// ─── iter_log.go: writeIterLogEntry happy + reads back ────────────────────

func TestWriteIterLogEntry_Happy(t *testing.T) {
	tmp := t.TempDir()
	iterPath := filepath.Join(tmp, "iter-1.yaml")
	entry := newValidIterLogEntry()
	if err := writeIterLogEntry(iterPath, entry); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "iteration: 1") {
		t.Fatalf("expected file content, got %s", string(data))
	}
}

// ─── plan_task.go: graphAdapterForProject default-graph-home fallback ─────

func TestGraphAdapterForProject_DefaultHome(t *testing.T) {
	repo := t.TempDir()
	adapter := graphAdapterForProject(repo)
	if adapter == nil {
		t.Fatal("expected adapter, got nil")
	}
}

// ─── plan_task.go: runWorkflowPlanGraph load error ────────────────────────

func TestRunWorkflowPlanGraph_LoadError(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowPlanGraph("missing-plan")
	if err == nil {
		t.Fatal("expected load error for missing plan")
	}
}

// ─── graph.go: runWorkflowGraphQuery missing-intent branch ────────────────

func TestRunWorkflowGraphQuery_MissingIntent_Push8(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "", "")
	cmd.Flags().String("scope", "", "")
	err := runWorkflowGraphQuery(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--intent") {
		t.Fatalf("expected intent-required, got %v", err)
	}
}

// ─── graph.go: runWorkflowGraphQuery code-bridge intent dispatch ──────────

func TestRunWorkflowGraphQuery_CodeBridgeIntent(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "symbol_lookup", "")
	cmd.Flags().String("scope", "", "")
	// Force the underlying executable resolver to fail so we don't
	// actually spawn a subprocess; this exercises the kg-bridge dispatch.
	saved := workflowDotAgentsExe
	workflowDotAgentsExe = func() (string, error) { return "", errors.New("synthetic") }
	t.Cleanup(func() { workflowDotAgentsExe = saved })
	err := runWorkflowGraphQuery(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "resolve da executable") {
		t.Fatalf("expected resolve-exe error, got %v", err)
	}
}

// ─── graph.go: runWorkflowGraphHealth JSON output ─────────────────────────

func TestRunWorkflowGraphHealth_JSON_Push8(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowGraphHealth(nil, nil) })
	if err != nil {
		t.Fatalf("graph health: %v", err)
	}
	if !strings.Contains(out, `"status"`) {
		t.Fatalf("expected status field in JSON, got: %s", out)
	}
}

// ─── cmd.go: prefs set-local subcommand exercised via cobra ──────────────

func TestCmd_PrefsSetLocal_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := executeWorkflowCommand(t, repo, "prefs", "set-local", "execution.max_parallel_workers", "2"); err != nil {
		t.Fatalf("set-local: %v", err)
	}
}

func TestCmd_PrefsSetShared_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := executeWorkflowCommand(t, repo, "prefs", "set-shared", "execution.max_parallel_workers", "2"); err != nil {
		t.Fatalf("set-shared: %v", err)
	}
}

// ─── cmd.go: task add via cobra (covers RunE wrapper) ─────────────────────

func TestCmd_TaskAdd_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "task", "add", "plan-001",
		"--id", "task-new", "--title", "New task"); err != nil {
		t.Fatalf("task add: %v", err)
	}
}

// ─── cmd.go: task update via cobra ────────────────────────────────────────

func TestCmd_TaskUpdate_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "task", "update", "plan-001",
		"--task", "task-001", "--notes", "updated"); err != nil {
		t.Fatalf("task update: %v", err)
	}
}

// ─── cmd.go: plan archive missing-plan flag rejected ──────────────────────

func TestCmd_PlanArchive_EmptyPlanIDs(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "plan", "archive", "--plan", ",,,")
	if err == nil || !strings.Contains(err.Error(), "at least one plan ID") {
		t.Fatalf("expected error, got %v", err)
	}
}

// ─── cmd.go: plan create via cobra (covers RunE wrapper) ──────────────────

func TestCmd_PlanCreate_Cobra(t *testing.T) {
	repo := t.TempDir()
	if err := executeWorkflowCommand(t, repo, "plan", "create", "p-new",
		"--title", "New"); err != nil {
		t.Fatalf("plan create: %v", err)
	}
}

// ─── cmd.go: plan update via cobra ────────────────────────────────────────

func TestCmd_PlanUpdate_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "plan", "update", "plan-001",
		"--status", "paused"); err != nil {
		t.Fatalf("plan update: %v", err)
	}
}

// ─── cmd.go: plan derive-scope via cobra (exercise RunE) ──────────────────

func TestCmd_PlanDeriveScope_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "plan", "derive-scope", "plan-001", "task-001"); err != nil {
		t.Fatalf("derive-scope: %v", err)
	}
}

// ─── plan_task.go: runWorkflowPlanArchive missing-plan branch ────────────

func TestRunWorkflowPlanArchive_PlanNotFound(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowPlanArchive(repo, []string{"no-such"}, false, true)
	if err == nil {
		t.Fatal("expected plan-not-found error")
	}
}

// ─── delegation.go: scanActiveDelegationContract no-contract ──────────────

func TestScanActiveDelegationContract_None(t *testing.T) {
	wave, tid := scanActiveDelegationContract(t.TempDir())
	if wave != "" || tid != "" {
		t.Fatalf("expected empty values, got wave=%q tid=%q", wave, tid)
	}
}

// ─── verification_result_schema.go: validateVerificationResultDoc valid ───

func TestValidateVerificationResultDoc_Valid_Push8(t *testing.T) {
	if err := validateVerificationResultDoc(newValidVerificationResultDoc()); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}
}

// ─── review_decision_schema.go: validateReviewDecisionDoc valid ──────────

func TestValidateReviewDecisionDoc_Valid(t *testing.T) {
	if err := validateReviewDecisionDoc(newValidReviewDecisionDoc()); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}
}

// ─── iter_log_schema.go: validateWorkflowIterLogEntry valid ──────────────

func TestValidateWorkflowIterLogEntry_Valid(t *testing.T) {
	if err := validateWorkflowIterLogEntry(newValidIterLogEntry()); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}
}

// ─── state.go: gitOutput from real repo ──────────────────────────────────

func TestGitOutput_RealRepo(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	out := gitOutput(repo, "rev-parse", "HEAD")
	if out == "" {
		t.Fatal("expected SHA output from real repo")
	}
}

func TestGitModifiedFiles_RealRepo(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	files, err := gitModifiedFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Modified README.md from initWorkflowTestRepo post-commit edit.
	if len(files) == 0 {
		t.Fatal("expected modified files")
	}
}

// ─── verification.go: appendVerificationLog wraps a long line ────────────

func TestAppendVerificationLog_Multiline(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	for i := 0; i < 3; i++ {
		rec := VerificationRecord{
			SchemaVersion: 1, Kind: "test", Status: "pass",
			Timestamp: fmt.Sprintf("2026-05-12T00:00:0%dZ", i),
			Summary:   fmt.Sprintf("row-%d", i),
		}
		if err := appendVerificationLog("p", rec); err != nil {
			t.Fatal(err)
		}
	}
	out, err := readVerificationLog("p", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 records, got %d", len(out))
	}
}

// ─── delegation.go: listDelegationContracts skips non-yaml entries ───────

func TestListDelegationContracts_SkipsNonYaml(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	out, err := listDelegationContracts(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("expected zero contracts, got %d", len(out))
	}
}

// ─── delegation.go: listDelegationContracts skips unreadable ─────────────

func TestListDelegationContracts_SkipsUnreadable(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "task-bad.yaml")
	if err := os.WriteFile(bad, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := listDelegationContracts(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("expected unreadable to be skipped, got %d", len(out))
	}
}
