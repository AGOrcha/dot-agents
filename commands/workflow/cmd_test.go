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
)

func TestWorkflowCmd_NewCmd_BuildsRoot(t *testing.T) {

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

	err := executeWorkflowCommand(t, dir, "plan", "archive")
	if err == nil {
		t.Error("expected error when --plan flag missing")
	}
}

func TestNewWorkflowPlanArchiveCmd_EmptyAfterTrim(t *testing.T) {
	dir := initWorkflowTestRepo(t)

	err := executeWorkflowCommand(t, dir, "plan", "archive", "--plan", ", ,")
	if err == nil || !strings.Contains(err.Error(), "at least one plan ID") {
		t.Errorf("expected 'at least one plan ID' error, got: %v", err)
	}
}

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

func TestNewCmd_LogSubcommand(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := executeWorkflowCommand(t, repo, "log"); err != nil {
		t.Fatalf("workflow log: %v", err)
	}

	if err := executeWorkflowCommand(t, repo, "log", "--all"); err != nil {
		t.Fatalf("workflow log --all: %v", err)
	}
}

func TestNewCmd_LogRejectsPositional(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := executeWorkflowCommand(t, repo, "log", "stray"); err == nil {
		t.Fatal("expected positional rejection")
	}
}

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

func TestCmd_TaskAdd_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "task", "add", "plan-001",
		"--id", "task-new", "--title", "New task"); err != nil {
		t.Fatalf("task add: %v", err)
	}
}

func TestCmd_TaskUpdate_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "task", "update", "plan-001",
		"--task", "task-001", "--notes", "updated"); err != nil {
		t.Fatalf("task update: %v", err)
	}
}

func TestCmd_PlanArchive_EmptyPlanIDs(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "plan", "archive", "--plan", ",,,")
	if err == nil || !strings.Contains(err.Error(), "at least one plan ID") {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestCmd_PlanCreate_Cobra(t *testing.T) {
	repo := t.TempDir()
	if err := executeWorkflowCommand(t, repo, "plan", "create", "p-new",
		"--title", "New"); err != nil {
		t.Fatalf("plan create: %v", err)
	}
}

func TestCmd_PlanUpdate_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "plan", "update", "plan-001",
		"--status", "paused"); err != nil {
		t.Fatalf("plan update: %v", err)
	}
}

func TestCmd_PlanDeriveScope_Cobra(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "plan", "derive-scope", "plan-001", "task-001"); err != nil {
		t.Fatalf("derive-scope: %v", err)
	}
}

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

func TestCobra_CompleteRequiresPlan(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "complete", "--plan", "")
	if err == nil {
		t.Fatal("expected missing-plan error")
	}
}

func TestCobra_GraphQueryMissingIntent(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "graph", "query")
	if err == nil || !strings.Contains(err.Error(), "intent") {
		t.Fatalf("expected intent-required error, got %v", err)
	}
}

func TestCobra_GraphQueryUnknownIntent(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo)
	err := executeWorkflowCommand(t, repo, "graph", "query",
		"--intent", "totally-bogus", "x")
	if err == nil {
		t.Fatal("expected unknown-intent error")
	}
}

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

func TestCobra_MergeBackMissingTaskFlag(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "merge-back", "--summary", "x")
	if err == nil {
		t.Fatal("expected missing-task error for merge-back")
	}
}

func TestCobra_DelegationCloseoutMissingFlags(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "delegation", "closeout")
	if err == nil {
		t.Fatal("expected required-flag error for delegation closeout")
	}
}

func TestCobra_FoldBackCreateMissingPlan(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--observation", "x")
	if err == nil {
		t.Fatal("expected missing-plan for fold-back create")
	}
}

func TestCobra_PlanCheckScopeMissingArgs(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "plan", "check-scope")
	if err == nil {
		t.Fatal("expected required-arg error")
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
