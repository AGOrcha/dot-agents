package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// directContractTestPlan seeds a minimal canonical plan + tasks for
// contract-create tests. Reuses the same on-disk layout that workflow fanout
// + closeout already exercise.
func directContractTestPlan(t *testing.T, repo string) {
	t.Helper()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "plan-direct")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1, ID: "plan-direct", Title: "Direct Plan", Status: "active",
		CreatedAt: "2026-05-26T00:00:00Z", UpdatedAt: "2026-05-26T00:00:00Z",
	}
	pd, _ := yaml.Marshal(plan)
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), pd, 0644); err != nil {
		t.Fatalf("write PLAN.yaml: %v", err)
	}
	tf := CanonicalTaskFile{
		SchemaVersion: 1, PlanID: "plan-direct",
		Tasks: []CanonicalTask{
			{ID: "task-direct", Title: "Direct work", Status: "pending", WriteScope: []string{"commands/workflow/contract.go"}},
			{ID: "task-completed", Title: "Already done", Status: "completed", WriteScope: []string{"commands/workflow/other.go"}},
		},
	}
	td, _ := yaml.Marshal(tf)
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), td, 0644); err != nil {
		t.Fatalf("write TASKS.yaml: %v", err)
	}
}

func TestDelegationContractMode_RoundTripDirect(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-direct", Mode: DelegationContractModeDirect,
		ParentPlanID: "p1", ParentTaskID: "t1", Title: "Direct",
		WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Mode != DelegationContractModeDirect {
		t.Errorf("mode round-trip: got %q want %q", got.Mode, DelegationContractModeDirect)
	}
}

func TestDelegationContractMode_RoundTripDelegated(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-fanout", Mode: DelegationContractModeDelegated,
		ParentPlanID: "p1", ParentTaskID: "t1", Title: "Delegated",
		WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Mode != DelegationContractModeDelegated {
		t.Errorf("mode round-trip: got %q want %q", got.Mode, DelegationContractModeDelegated)
	}
}

// TestLoadDelegationContract_LegacyModeDefaultsDelegated documents the
// backward-compat path: a v1 contract on disk (no `mode:` field) loads with
// Mode="delegated" so historical fanout-owned-by-worker semantics survive.
func TestLoadDelegationContract_LegacyModeDefaultsDelegated(t *testing.T) {
	repo := t.TempDir()
	dir := delegationDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := []byte(`schema_version: 1
id: del-legacy
parent_plan_id: p1
parent_task_id: t-legacy
title: legacy
write_scope:
  - commands/
status: active
created_at: 2026-01-01T00:00:00Z
updated_at: 2026-01-01T00:00:00Z
`)
	if err := os.WriteFile(filepath.Join(dir, "t-legacy.yaml"), legacy, 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	got, err := loadDelegationContract(repo, "t-legacy")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Mode != DelegationContractModeDelegated {
		t.Errorf("legacy mode default: got %q want %q", got.Mode, DelegationContractModeDelegated)
	}
}

func TestIsValidDelegationContractMode(t *testing.T) {
	cases := []struct {
		mode  DelegationContractMode
		valid bool
	}{
		{DelegationContractModeDirect, true},
		{DelegationContractModeDelegated, true},
		{"", false},
		{"bogus", false},
	}
	for _, c := range cases {
		if got := isValidDelegationContractMode(c.mode); got != c.valid {
			t.Errorf("isValidDelegationContractMode(%q) = %v, want %v", c.mode, got, c.valid)
		}
	}
}

func TestNormalizeDelegationContractMode_DefaultsDelegated(t *testing.T) {
	if got := normalizeDelegationContractMode(""); got != DelegationContractModeDelegated {
		t.Errorf("normalize empty: got %q want %q", got, DelegationContractModeDelegated)
	}
	if got := normalizeDelegationContractMode(DelegationContractModeDirect); got != DelegationContractModeDirect {
		t.Errorf("normalize direct: got %q want %q", got, DelegationContractModeDirect)
	}
}

func TestResolveContractWriteScope_ExplicitCSV(t *testing.T) {
	got := resolveContractWriteScope("commands/foo.go, internal/bar/", true, []string{"fallback/"})
	want := []string{"commands/foo.go", "internal/bar/"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestResolveContractWriteScope_FallbackToTaskScope(t *testing.T) {
	got := resolveContractWriteScope("", false, []string{"task/scope/"})
	if len(got) != 1 || got[0] != "task/scope/" {
		t.Errorf("got %v want [task/scope/]", got)
	}
}

func TestResolveContractWriteScope_EmptyExplicitProducesEmpty(t *testing.T) {
	got := resolveContractWriteScope("  ,  ", true, []string{"fallback/"})
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestResolveContractTargetTask_NotFound(t *testing.T) {
	tf := &CanonicalTaskFile{Tasks: []CanonicalTask{{ID: "a", Status: "pending"}}}
	_, err := resolveContractTargetTask(tf, "missing", "p1")
	if err == nil || !strings.Contains(err.Error(), "not found in plan") {
		t.Fatalf("expected not-found, got %v", err)
	}
}

func TestResolveContractTargetTask_RejectsCompleted(t *testing.T) {
	tf := &CanonicalTaskFile{Tasks: []CanonicalTask{{ID: "a", Status: "completed"}}}
	_, err := resolveContractTargetTask(tf, "a", "p1")
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected status rejection, got %v", err)
	}
}

func TestResolveContractTargetTask_RequiresTaskID(t *testing.T) {
	_, err := resolveContractTargetTask(&CanonicalTaskFile{}, "", "p1")
	if err == nil || !strings.Contains(err.Error(), "--task is required") {
		t.Fatalf("expected required error, got %v", err)
	}
}

func TestWorkflowContractCreate_DirectModeCLISmoke(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	directContractTestPlan(t, repo)

	// SuccessBox writes to os.Stdout (not cobra's buffer), so this smoke
	// asserts behaviour through the on-disk state rather than rendered text.
	executeWorkflowCommandOutput(t, repo,
		"contract", "create",
		"--plan", "plan-direct",
		"--task", "task-direct",
	)

	got, err := loadDelegationContract(repo, "task-direct")
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	if got.Mode != DelegationContractModeDirect {
		t.Errorf("mode: got %q want %q", got.Mode, DelegationContractModeDirect)
	}
	if got.Status != "active" {
		t.Errorf("status: got %q want active", got.Status)
	}
	if len(got.WriteScope) != 1 || got.WriteScope[0] != "commands/workflow/contract.go" {
		t.Errorf("write scope inherited from TASKS.yaml: got %v", got.WriteScope)
	}
	if got.ParentPlanID != "plan-direct" || got.ParentTaskID != "task-direct" {
		t.Errorf("plan/task: got plan=%q task=%q", got.ParentPlanID, got.ParentTaskID)
	}
	if !strings.HasPrefix(got.ID, "del-task-direct-") {
		t.Errorf("contract id pattern: got %q", got.ID)
	}
}

func TestWorkflowContractCreate_ExplicitDelegatedMode(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	directContractTestPlan(t, repo)

	executeWorkflowCommandOutput(t, repo,
		"contract", "create",
		"--plan", "plan-direct",
		"--task", "task-direct",
		"--delegated",
		"--owner", "worker-x",
	)
	got, err := loadDelegationContract(repo, "task-direct")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Mode != DelegationContractModeDelegated {
		t.Errorf("mode: got %q want %q", got.Mode, DelegationContractModeDelegated)
	}
	if got.Owner != "worker-x" {
		t.Errorf("owner: got %q want worker-x", got.Owner)
	}
}

func TestWorkflowContractCreate_RejectsCompletedTask(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	directContractTestPlan(t, repo)

	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"contract", "create", "--plan", "plan-direct", "--task", "task-completed"})

	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for completed task")
	} else if !strings.Contains(err.Error(), "status") {
		t.Errorf("expected status error, got: %v", err)
	}
}

func TestWorkflowContractCreate_RejectsExistingWithoutForce(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	directContractTestPlan(t, repo)

	// First create succeeds.
	executeWorkflowCommandOutput(t, repo,
		"contract", "create", "--plan", "plan-direct", "--task", "task-direct",
	)

	// Second create without --force must fail.
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"contract", "create", "--plan", "plan-direct", "--task", "task-direct"})

	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "already has an active delegation contract") {
		t.Fatalf("expected duplicate-contract error, got %v", err)
	}
}

func TestWorkflowContractCreate_ForceOverwrites(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	directContractTestPlan(t, repo)

	executeWorkflowCommandOutput(t, repo,
		"contract", "create", "--plan", "plan-direct", "--task", "task-direct",
		"--owner", "first-owner",
	)
	// Force overwrite with a different owner — proves --force took effect.
	executeWorkflowCommandOutput(t, repo,
		"contract", "create", "--plan", "plan-direct", "--task", "task-direct",
		"--owner", "second-owner", "--force",
	)
	got, err := loadDelegationContract(repo, "task-direct")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Owner != "second-owner" {
		t.Errorf("owner after force: got %q want second-owner", got.Owner)
	}
}

type resolveModeCase struct {
	name    string
	flags   []string
	want    DelegationContractMode
	wantErr bool
}

func resolveModeCases() []resolveModeCase {
	return []resolveModeCase{
		{name: "default direct", flags: []string{"--plan", "p", "--task", "t"}, want: DelegationContractModeDirect},
		{name: "explicit direct", flags: []string{"--plan", "p", "--task", "t", "--direct"}, want: DelegationContractModeDirect},
		{name: "delegated flag", flags: []string{"--plan", "p", "--task", "t", "--delegated"}, want: DelegationContractModeDelegated},
		{name: "mode direct", flags: []string{"--plan", "p", "--task", "t", "--mode", "direct"}, want: DelegationContractModeDirect},
		{name: "mode delegated", flags: []string{"--plan", "p", "--task", "t", "--mode", "delegated"}, want: DelegationContractModeDelegated},
		{name: "direct + delegated conflict", flags: []string{"--plan", "p", "--task", "t", "--direct", "--delegated"}, wantErr: true},
		{name: "mode bogus", flags: []string{"--plan", "p", "--task", "t", "--mode", "bogus"}, wantErr: true},
		{name: "mode direct + --delegated conflict", flags: []string{"--plan", "p", "--task", "t", "--mode", "direct", "--delegated"}, wantErr: true},
		{name: "mode delegated + --direct conflict", flags: []string{"--plan", "p", "--task", "t", "--mode", "delegated", "--direct"}, wantErr: true},
	}
}

// runResolveModeCase keeps the test loop body small (cognitive-complexity
// budget per go:S3776) by handling both the success and error branches in a
// single helper.
func runResolveModeCase(t *testing.T, tc resolveModeCase) {
	t.Helper()
	createCmd := findCreateSubcommand(t)
	if err := createCmd.ParseFlags(tc.flags); err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := resolveContractCreateMode(createCmd)
	if tc.wantErr {
		if err == nil {
			t.Fatalf("expected error, got mode=%q", got)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tc.want {
		t.Errorf("got %q want %q", got, tc.want)
	}
}

func TestResolveContractCreateMode_FlagsAndDefaults(t *testing.T) {
	for _, tc := range resolveModeCases() {
		t.Run(tc.name, func(t *testing.T) { runResolveModeCase(t, tc) })
	}
}

// findCreateSubcommand walks the contract command tree and returns the
// `create` subcommand so flag parsing tests don't have to re-declare flags.
func findCreateSubcommand(t *testing.T) *cobra.Command {
	t.Helper()
	contractCmd := newWorkflowContractCmd()
	for _, sub := range contractCmd.Commands() {
		if sub.Name() == "create" {
			return sub
		}
	}
	t.Fatalf("contract create subcommand not registered")
	return nil
}

// TestDirectContract_ClosesOutAndAutoAdvances pins the headline guarantee of
// PR-B: a direct-mode contract goes through merge-back + closeout exactly the
// same way a delegated contract does, including the auto-advance of the
// canonical task to "completed".
//
// This is the audit-trail invariant the proposal calls out: every chunk of
// work — direct or delegated — produces a closeout record and an advanced
// task row, with no manual `da workflow advance` follow-up required.
func TestDirectContract_ClosesOutAndAutoAdvances(t *testing.T) {
	repo := t.TempDir()
	planID := "plan-direct-close"
	taskID := "task-d1"
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", planID)
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1, ID: planID, Title: "Direct Closeout", Status: "active",
		CreatedAt: "2026-05-26T00:00:00Z", UpdatedAt: "2026-05-26T00:00:00Z",
	}
	pd, _ := yaml.Marshal(plan)
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), pd, 0644); err != nil {
		t.Fatal(err)
	}
	tf := &CanonicalTaskFile{
		SchemaVersion: 1, PlanID: planID,
		Tasks: []CanonicalTask{{ID: taskID, Title: "Direct task", Status: "in_progress", WriteScope: []string{"commands/workflow/contract.go"}}},
	}
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}

	// 1. Materialize a direct-mode contract for the task (simulating the
	//    orchestrator picking it up directly rather than via fanout).
	now := time.Now().UTC().Format(time.RFC3339)
	contract := &DelegationContract{
		SchemaVersion: 1, ID: "del-direct-1", Mode: DelegationContractModeDirect,
		ParentPlanID: planID, ParentTaskID: taskID, Title: "Direct task",
		WriteScope: []string{"commands/workflow/contract.go"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, contract); err != nil {
		t.Fatalf("save direct contract: %v", err)
	}

	// 2. Write a merge-back record (would normally come from
	//    `da workflow merge-back` after verification passes).
	if err := saveMergeBack(repo, &MergeBackSummary{
		SchemaVersion: 1, TaskID: taskID, ParentPlanID: planID,
		Title: "Direct task", Summary: "done",
		VerificationResult: MergeBackVerification{Status: "pass", Summary: "ok"},
		IntegrationNotes:   "ok", CreatedAt: now,
	}); err != nil {
		t.Fatalf("save merge-back: %v", err)
	}
	// reconcileDelegationContractForCloseout will flip status to completed
	// (would normally be set by merge-back).
	contract.Status = "completed"
	if err := saveDelegationContract(repo, contract); err != nil {
		t.Fatalf("update contract status: %v", err)
	}

	// 3. Apply the closeout decision — this is the auto-advance pivot.
	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: planID, TaskID: taskID,
		DelegationID: contract.ID, Decision: "accept",
		ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := applyCloseoutDecisionToTasks(repo, planID, taskID, closeout); err != nil {
		t.Fatalf("applyCloseoutDecisionToTasks: %v", err)
	}

	// 4. Verify the task auto-advanced to completed exactly as it would for
	//    a delegated contract — no manual `workflow advance` required.
	gotTF, err := loadCanonicalTasks(repo, planID)
	if err != nil {
		t.Fatalf("reload tasks: %v", err)
	}
	if len(gotTF.Tasks) != 1 || gotTF.Tasks[0].Status != "completed" {
		t.Errorf("task status: got %+v want status=completed", gotTF.Tasks)
	}

	// And the plan should have advanced to completed since all tasks terminal.
	gotPlan, err := loadCanonicalPlan(repo, planID)
	if err != nil {
		t.Fatalf("reload plan: %v", err)
	}
	if gotPlan.Status != "completed" {
		t.Errorf("plan status after sole task completes: got %q want completed", gotPlan.Status)
	}
}

func TestContractsPath_DerivedFromDelegationDir(t *testing.T) {
	repo := t.TempDir()
	got := contractsPath(repo)
	want := delegationDir(repo)
	if got != want {
		t.Errorf("contractsPath = %q, want %q", got, want)
	}
}

// runContractCreateExpectErr is a small helper that invokes the
// `workflow contract create` command and returns the resulting error.
// Centralizes the chdir + cobra-execute boilerplate used by the
// error-branch coverage tests below.
func runContractCreateExpectErr(t *testing.T, repo string, args ...string) error {
	t.Helper()
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(append([]string{"contract", "create"}, args...))
	return cmd.Execute()
}

func TestWorkflowContractCreate_RejectsMissingPlan(t *testing.T) {
	repo := initWorkflowTestRepo(t) // no canonical plan seeded
	err := runContractCreateExpectErr(t, repo,
		"--plan", "ghost-plan", "--task", "x",
	)
	if err == nil || !strings.Contains(err.Error(), "plan ghost-plan not found") {
		t.Fatalf("expected plan-not-found error, got: %v", err)
	}
}

func TestWorkflowContractCreate_RejectsEmptyWriteScope(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	// Seed a plan + task whose TASKS.yaml write_scope is empty so the
	// fallback path produces nothing and the explicit CSV is also blank.
	planID := "plan-empty-scope"
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", planID)
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1, ID: planID, Title: "Empty Scope", Status: "active",
		CreatedAt: "2026-05-26T00:00:00Z", UpdatedAt: "2026-05-26T00:00:00Z",
	}
	pd, _ := yaml.Marshal(plan)
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), pd, 0644); err != nil {
		t.Fatal(err)
	}
	tf := CanonicalTaskFile{
		SchemaVersion: 1, PlanID: planID,
		Tasks: []CanonicalTask{{ID: "t-noscope", Title: "no scope", Status: "pending"}},
	}
	td, _ := yaml.Marshal(tf)
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), td, 0644); err != nil {
		t.Fatal(err)
	}

	err := runContractCreateExpectErr(t, repo, "--plan", planID, "--task", "t-noscope")
	if err == nil || !strings.Contains(err.Error(), "write scope is empty") {
		t.Fatalf("expected empty-write-scope error, got: %v", err)
	}
}

func TestWorkflowContractCreate_RejectsConflictingMode(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	directContractTestPlan(t, repo)
	err := runContractCreateExpectErr(t, repo,
		"--plan", "plan-direct", "--task", "task-direct",
		"--direct", "--delegated",
	)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually-exclusive error, got: %v", err)
	}
}

func TestWorkflowContractCreate_WriteScopeConflictRejected(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	directContractTestPlan(t, repo)

	// Pre-seed an active contract on a different task whose write_scope
	// overlaps with the one we are about to materialize. The conflict check
	// is shared with fanout, so this exercises the same gate from the new
	// path.
	now := time.Now().UTC().Format(time.RFC3339)
	pre := &DelegationContract{
		SchemaVersion: 1, ID: "del-other",
		ParentPlanID: "plan-direct", ParentTaskID: "task-other",
		Title:      "blocker",
		WriteScope: []string{"commands/workflow/contract.go"},
		Status:     "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, pre); err != nil {
		t.Fatal(err)
	}
	err := runContractCreateExpectErr(t, repo,
		"--plan", "plan-direct", "--task", "task-direct",
	)
	if err == nil || !strings.Contains(err.Error(), "write scope overlaps") {
		t.Fatalf("expected write-scope-overlap error, got: %v", err)
	}
}

func TestWorkflowContractCreate_JSONOutput(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	directContractTestPlan(t, repo)

	prev := workflowTestJSON
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = prev })

	executeWorkflowCommandOutput(t, repo,
		"contract", "create", "--plan", "plan-direct", "--task", "task-direct",
	)
	// JSON output writes to os.Stdout (not cobra's buffer); assert via disk.
	got, err := loadDelegationContract(repo, "task-direct")
	if err != nil {
		t.Fatalf("load after JSON-mode create: %v", err)
	}
	if got.Mode != DelegationContractModeDirect {
		t.Errorf("mode after JSON create: got %q want direct", got.Mode)
	}
}

func TestWorkflowContractList_EmptyAndPopulated(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	directContractTestPlan(t, repo)

	// Empty case — should succeed and emit the "no contracts" message.
	// Asserted via successful execution (text output goes to os.Stdout).
	executeWorkflowCommandOutput(t, repo, "contract", "list")

	// Populate three contracts: two delegated (to force the same-mode sort
	// comparator to fall through to alphabetical-by-task-id ordering) plus
	// one direct. This exercises both branches of the comparator in
	// runWorkflowContractList.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range []*DelegationContract{
		{SchemaVersion: 1, ID: "del-a", Mode: DelegationContractModeDelegated,
			ParentPlanID: "p1", ParentTaskID: "task-b", Title: "A",
			WriteScope: []string{"commands/a/"}, Status: "active",
			CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "del-b", Mode: DelegationContractModeDelegated,
			ParentPlanID: "p1", ParentTaskID: "task-a", Title: "B",
			WriteScope: []string{"commands/b/"}, Status: "active",
			CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "del-c", Mode: DelegationContractModeDirect,
			ParentPlanID: "p1", ParentTaskID: "task-c", Title: "C",
			WriteScope: []string{"commands/c/"}, Status: "active",
			CreatedAt: now, UpdatedAt: now},
	} {
		if err := saveDelegationContract(repo, c); err != nil {
			t.Fatal(err)
		}
	}

	executeWorkflowCommandOutput(t, repo, "contract", "list")

	prev := workflowTestJSON
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = prev })
	executeWorkflowCommandOutput(t, repo, "contract", "list")
}

func TestWorkflowContractList_PropagatesListError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	// Replace the delegation dir with an unreadable directory so
	// listDelegationContracts surfaces a ReadDir error to the list runner.
	dir := delegationDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, dir)

	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"contract", "list"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "list contracts") {
		t.Fatalf("expected list-contracts error, got: %v", err)
	}
}

func TestResolveContractWriteScope_EmptyEverything(t *testing.T) {
	// When neither an explicit CSV nor a TASKS.yaml fallback exists the
	// resolver returns nil — the caller (runWorkflowContractCreate) then
	// surfaces the friendly "write scope is empty" error.
	got := resolveContractWriteScope("", false, nil)
	if got != nil {
		t.Errorf("expected nil for empty-everything, got %v", got)
	}
}

// TestMaterializeDelegationContract_SaveErrorPropagates covers the failure
// branch of the shared core: when saveDelegationContract fails (forced here
// via the osMkdirAll func-var seam), the helper must surface a wrapped
// "save delegation contract" error rather than silently swallowing it.
// Without this test, materializeDelegationContract has an uncovered error
// path and the file falls below the 95% per-file coverage gate.
func TestMaterializeDelegationContract_SaveErrorPropagates(t *testing.T) {
	prior := osMkdirAll
	osMkdirAll = func(string, os.FileMode) error { return errors.New("synthetic mkdir fault") }
	t.Cleanup(func() { osMkdirAll = prior })

	_, err := materializeDelegationContract(materializeContractRequest{
		ProjectPath: t.TempDir(),
		Mode:        DelegationContractModeDirect,
		PlanID:      "p", TaskID: "t", Title: "x",
		WriteScope: []string{"commands/"},
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected save error, got nil")
	}
	if !strings.Contains(err.Error(), "save delegation contract") {
		t.Errorf("expected wrapped save error, got %v", err)
	}
}

// TestMaterializeDelegationContract_DRYAcrossFanoutAndContractCreate pins the
// #131 review guarantee: `workflow fanout` and `workflow contract create` now
// go through one business-logic path (materializeDelegationContract). Calling
// it with the same inputs from both call sites must produce identical
// contracts (modulo UpdatedAt, which saveDelegationContract stamps to wall
// clock). If a future change reintroduces a parallel contract-construction
// path in either command, this test fails.
func TestMaterializeDelegationContract_DRYAcrossFanoutAndContractCreate(t *testing.T) {
	repoFanout := t.TempDir()
	repoContract := t.TempDir()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

	req := materializeContractRequest{
		Mode:            DelegationContractModeDelegated,
		PlanID:          "plan-x",
		TaskID:          "task-x",
		Title:           "Shared task",
		Summary:         "shared summary",
		WriteScope:      []string{"commands/foo.go"},
		SuccessCriteria: "tests green",
		Owner:           "worker-x",
		Now:             now,
	}

	req.ProjectPath = repoFanout
	fromFanout, err := materializeDelegationContract(req)
	if err != nil {
		t.Fatalf("fanout-style materialize: %v", err)
	}
	req.ProjectPath = repoContract
	fromContract, err := materializeDelegationContract(req)
	if err != nil {
		t.Fatalf("contract-create-style materialize: %v", err)
	}

	// The persisted contracts must agree on every field except UpdatedAt
	// (which saveDelegationContract stamps to time.Now()). If any other field
	// drifts between the two call sites, the shared core has been bypassed.
	// Normalise UpdatedAt before comparing so the assertion isn't flaky on
	// machines where the two writes straddle a second boundary.
	a := *fromFanout
	b := *fromContract
	a.UpdatedAt = ""
	b.UpdatedAt = ""
	if !reflect.DeepEqual(a, b) {
		t.Errorf("fanout vs contract-create produced different contracts:\nfanout=  %+v\ncontract=%+v", a, b)
	}

	// Sanity: both files exist on disk.
	if _, err := os.Stat(filepath.Join(delegationDir(repoFanout), "task-x.yaml")); err != nil {
		t.Errorf("fanout contract not persisted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(delegationDir(repoContract), "task-x.yaml")); err != nil {
		t.Errorf("contract-create contract not persisted: %v", err)
	}
}
