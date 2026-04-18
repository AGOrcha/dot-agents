package workflow

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestWorkflow_CheckpointThenOrient(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	writeCheckpointFixture(t, agentsHome, "workflow-proj", repo, "Continue wave-3 implementation", "pass", "2026-04-10T10:00:00Z")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Checkpoint == nil {
		t.Fatal("expected checkpoint, got nil")
	}
	if state.Checkpoint.Verification.Status != "pass" {
		t.Fatalf("checkpoint verification status = %q, want pass", state.Checkpoint.Verification.Status)
	}
	if state.NextAction != "Continue wave-3 implementation" {
		t.Fatalf("next action = %q, want 'Continue wave-3 implementation'", state.NextAction)
	}
	if state.NextActionSource != "checkpoint" {
		t.Fatalf("next action source = %q, want checkpoint", state.NextActionSource)
	}

	// Orient output must include the checkpoint's next action
	var buf bytes.Buffer
	renderWorkflowOrientMarkdown(state, &buf)
	rendered := buf.String()
	if !strings.Contains(rendered, "Continue wave-3 implementation") {
		t.Fatalf("orient output missing checkpoint next action:\n%s", rendered)
	}
	if !strings.Contains(rendered, "pass") {
		t.Fatalf("orient output missing verification status:\n%s", rendered)
	}
}

// TestWorkflow_PlanLifecycle creates PLAN.yaml + TASKS.yaml, lists the plan, shows it,
// advances a task, and verifies the status and focus task are updated.
func TestWorkflow_PlanLifecycle(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	addCanonicalPlanFixture(t, repo)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	// List: wave-2 should appear
	ids, err := listCanonicalPlanIDs(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "wave-2" {
		t.Fatalf("expected [wave-2], got %v", ids)
	}

	// Show: plan loads cleanly with expected fields
	plan, err := loadCanonicalPlan(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "active" {
		t.Fatalf("plan status = %q, want active", plan.Status)
	}

	// Advance t1 to completed
	if err := runWorkflowAdvance("wave-2", "t1", "completed"); err != nil {
		t.Fatal(err)
	}

	// Verify task status
	tf, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	var t1Status string
	for _, task := range tf.Tasks {
		if task.ID == "t1" {
			t1Status = task.Status
		}
	}
	if t1Status != "completed" {
		t.Fatalf("t1 status = %q, want completed", t1Status)
	}

	// Advance t2 to in_progress — this should update plan focus task
	if err := runWorkflowAdvance("wave-2", "t2", "in_progress"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadCanonicalPlan(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CurrentFocusTask != "add subcommands" {
		t.Fatalf("current_focus_task = %q, want 'add subcommands'", reloaded.CurrentFocusTask)
	}

	// collectWorkflowState should report updated task counts
	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.CanonicalPlans) != 1 {
		t.Fatalf("expected 1 canonical plan, got %d", len(state.CanonicalPlans))
	}
	// t1=completed(1), t2=in_progress(pending), t3=completed(1) → completed=2, pending=1
	if state.CanonicalPlans[0].CompletedCount != 2 {
		t.Fatalf("completed count = %d, want 2", state.CanonicalPlans[0].CompletedCount)
	}
}

// TestWorkflow_VerifyThenHealth records a passing verification run, then calls computeWorkflowHealth
// and verifies that having a checkpoint results in the "healthy" status (no "no checkpoint" warning).
func TestWorkflow_VerifyThenHealth(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	// Seed a checkpoint so health does not warn about missing checkpoint
	writeCheckpointFixture(t, agentsHome, "workflow-proj", repo, "done", "pass", "2026-04-10T10:00:00Z")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	// Record a passing verification
	if err := runWorkflowVerifyRecord("test", "pass", "go test ./...", "repo", "all tests green"); err != nil {
		t.Fatal(err)
	}

	// Read it back
	records, err := readVerificationLog("workflow-proj", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("verification log count = %d, want 1", len(records))
	}
	if records[0].Status != "pass" {
		t.Fatalf("verification status = %q, want pass", records[0].Status)
	}

	// Health should be "healthy" — checkpoint present, no excess dirty files or proposals
	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	health := computeWorkflowHealth(state)
	if health.Status != "healthy" {
		t.Fatalf("health status = %q, want healthy; warnings: %v", health.Status, health.Warnings)
	}
	if !health.Workflow.HasCheckpoint {
		t.Fatal("health.Workflow.HasCheckpoint should be true")
	}
}

func TestReviewDecisionSchema_Validate(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	ok := &ReviewDecisionDoc{
		SchemaVersion: 1, TaskID: "t1", ParentPlanID: "p1",
		Phase1Decision: "accept", Phase2Decision: "accept", OverallDecision: "accept",
		FailedGates: []string{}, RecordedAt: now,
	}
	if err := validateReviewDecisionDoc(ok); err != nil {
		t.Fatalf("valid doc: %v", err)
	}
	escNoReason := &ReviewDecisionDoc{
		SchemaVersion: 1, TaskID: "t1", ParentPlanID: "p1",
		Phase1Decision: "escalate", Phase2Decision: "accept", OverallDecision: "escalate",
		FailedGates: []string{}, RecordedAt: now,
	}
	if err := validateReviewDecisionDoc(escNoReason); err == nil {
		t.Fatal("expected schema error for escalate without escalation_reason")
	}
}

func TestWorkflow_VerifyRecordReview_WritesArtifactAndLog(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	writeCheckpointFixture(t, agentsHome, "workflow-proj", repo, "review gate", "pass", "2026-04-10T10:00:00Z")
	saveTestDelegationContract(t, repo, "slice-99", "plan-loop", "del-slice-99-1")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowVerifyRecordReview("", "repo", "LGTM scoped surface",
		"accept", "accept", "", "", "", "slice-99", []string{"unit", "api"}); err != nil {
		t.Fatal(err)
	}

	decPath := filepath.Join(repo, ".agents", "active", "verification", "slice-99", "review-decision.yaml")
	data, err := os.ReadFile(decPath)
	if err != nil {
		t.Fatalf("read review decision: %v", err)
	}
	var got ReviewDecisionDoc
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.OverallDecision != "accept" || got.Phase1Decision != "accept" || len(got.FailedGates) != 2 {
		t.Fatalf("unexpected decision doc: %+v", got)
	}
	if err := validateReviewDecisionDoc(&got); err != nil {
		t.Fatalf("on disk doc invalid: %v", err)
	}

	records, err := readVerificationLog("workflow-proj", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "review" || records[0].Status != "pass" {
		t.Fatalf("log: %+v", records)
	}
	if len(records[0].Artifacts) != 1 || !strings.HasSuffix(records[0].Artifacts[0], "review-decision.yaml") {
		t.Fatalf("artifacts: %+v", records[0].Artifacts)
	}
}

func TestWorkflow_VerifyRecordReview_Errors(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	writeCheckpointFixture(t, agentsHome, "workflow-proj", repo, "review gate", "pass", "2026-04-10T10:00:00Z")
	saveTestDelegationContract(t, repo, "slice-err", "plan-loop", "del-slice-err")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowVerifyRecordReview("", "repo", "x",
		"escalate", "accept", "", "", "", "slice-err", nil); err == nil {
		t.Fatal("expected error for escalate without escalation reason")
	}
	if err := runWorkflowVerifyRecordReview("", "repo", "x",
		"accept", "accept", "reject", "", "", "slice-err", nil); err == nil {
		t.Fatal("expected error when overall disagrees with phases")
	}
	if err := runWorkflowVerifyRecordReview("", "repo", "x",
		"maybe", "accept", "", "", "", "slice-err", nil); err == nil {
		t.Fatal("expected error for invalid phase decision")
	}
}

func TestWorkflow_VerifyRecordReview_Cobra(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	writeCheckpointFixture(t, agentsHome, "workflow-proj", repo, "review gate", "pass", "2026-04-10T10:00:00Z")
	saveTestDelegationContract(t, repo, "t-cobra", "p-cobra", "del-t-cobra")

	if err := executeWorkflowCommand(t, repo,
		"verify", "record", "--kind", "review",
		"--task", "t-cobra",
		"--phase1-decision", "reject", "--phase2-decision", "accept",
		"--failed-gate", "unit", "--summary", "blocked on unit"); err != nil {
		t.Fatal(err)
	}
	records, err := readVerificationLog("workflow-proj", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != "fail" {
		t.Fatalf("want fail log, got %+v", records)
	}
}

// TestWorkflow_StaleCheckpointState writes a checkpoint with an old timestamp and verifies
// that collectWorkflowState succeeds, returns the checkpoint without error, and that the
// orient output renders the old timestamp (no crash or data loss).
func TestWorkflow_StaleCheckpointState(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	// Checkpoint >7 days old relative to "now" (2026-04-10); use 2026-04-01
	writeCheckpointFixture(t, agentsHome, "workflow-proj", repo, "old task", "unknown", "2026-04-01T00:00:00Z")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Checkpoint == nil {
		t.Fatal("expected stale checkpoint to be loaded, got nil")
	}
	if state.Checkpoint.Timestamp != "2026-04-01T00:00:00Z" {
		t.Fatalf("checkpoint timestamp = %q, want 2026-04-01T00:00:00Z", state.Checkpoint.Timestamp)
	}
	// Next action still comes from checkpoint because the git state still matches.
	if state.NextAction != "old task" {
		t.Fatalf("next action = %q, want 'old task'", state.NextAction)
	}
	if state.NextActionSource != "checkpoint" {
		t.Fatalf("next action source = %q, want checkpoint", state.NextActionSource)
	}
	// Orient renders cleanly with the old timestamp
	var buf bytes.Buffer
	renderWorkflowOrientMarkdown(state, &buf)
	rendered := buf.String()
	if !strings.Contains(rendered, "2026-04-01T00:00:00Z") {
		t.Fatalf("orient output missing stale timestamp:\n%s", rendered)
	}
}

func TestWorkflow_PrefersCanonicalWhenCheckpointGitStateIsStale(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	addCanonicalPlanFixture(t, repo)

	writeCheckpointFixtureWithGitOverride(t, agentsHome, "workflow-proj", repo, "stale checkpoint task", "pass", "2026-04-10T10:00:00Z", "other-branch", "deadbee")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if state.NextAction != "implement structs" {
		t.Fatalf("next action = %q, want canonical focus task", state.NextAction)
	}
	if state.NextActionSource != "canonical_plan" {
		t.Fatalf("next action source = %q, want canonical_plan", state.NextActionSource)
	}
	found := false
	for _, warning := range state.Warnings {
		if strings.Contains(warning, "checkpoint next action") && strings.Contains(warning, "stale") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected stale-checkpoint warning, got %v", state.Warnings)
	}
}

// TestWorkflow_DerivedFocusIgnoresStaleCurrentFocusTask verifies that orient NextAction
// and canonical plan summaries derive focus from TASKS.yaml when PLAN.yaml current_focus_task
// still names a completed task (common after workflow advance ... completed).
func TestWorkflow_DerivedFocusIgnoresStaleCurrentFocusTask(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write(".agents/workflow/plans/z-stale-focus/PLAN.yaml", `schema_version: 1
id: "z-stale-focus"
title: "Stale focus fixture"
status: "active"
summary: "x"
created_at: "2026-04-10T10:00:00Z"
updated_at: "2026-04-10T10:00:00Z"
owner: "test"
success_criteria: ""
verification_strategy: ""
current_focus_task: "Phase A — done work"
`)
	write(".agents/workflow/plans/z-stale-focus/TASKS.yaml", `schema_version: 1
plan_id: "z-stale-focus"
tasks:
  - id: "a1"
    title: "Phase A — done work"
    status: "completed"
    depends_on: []
    blocks: ["a2"]
    owner: "test"
    write_scope: []
    verification_required: false
    notes: ""
  - id: "a2"
    title: "Phase B — next work"
    status: "pending"
    depends_on: ["a1"]
    blocks: []
    owner: "test"
    write_scope: []
    verification_required: false
    notes: ""
`)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if state.NextAction != "Phase B — next work" {
		t.Fatalf("next action = %q, want %q", state.NextAction, "Phase B — next work")
	}
	if state.NextActionSource != "canonical_plan" {
		t.Fatalf("next action source = %q, want canonical_plan", state.NextActionSource)
	}
	var found bool
	for _, cp := range state.CanonicalPlans {
		if cp.ID == "z-stale-focus" {
			found = true
			if cp.CurrentFocusTask != "Phase B — next work" {
				t.Fatalf("canonical plan summary focus = %q, want derived Phase B title", cp.CurrentFocusTask)
			}
		}
	}
	if !found {
		t.Fatal("expected z-stale-focus in canonical plans")
	}
}

func TestReadWorkflowPlanCompletedStatusDoesNotCreatePendingFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "completed.plan.md")
	content := `# Completed Plan

Status: Completed (2026-04-11)
Depends on: something else
- [x] finished item
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := readWorkflowPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.PendingItems) != 0 {
		t.Fatalf("expected no pending items, got %+v", plan.PendingItems)
	}
}

// TestWorkflow_MultiPlanPriority creates two canonical plans (one active, one paused) and verifies
// that orient's NextAction is driven by the active plan's focus task, not the paused one.
func TestWorkflow_MultiPlanPriority(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Active plan
	write(".agents/workflow/plans/alpha/PLAN.yaml", `schema_version: 1
id: "alpha"
title: "Alpha Plan"
status: "active"
summary: "first"
created_at: "2026-04-10T10:00:00Z"
updated_at: "2026-04-10T10:00:00Z"
owner: "test"
success_criteria: ""
verification_strategy: ""
current_focus_task: "alpha-focus-task"
`)
	write(".agents/workflow/plans/alpha/TASKS.yaml", `schema_version: 1
plan_id: "alpha"
tasks:
  - id: "a1"
    title: "alpha-focus-task"
    status: "in_progress"
    depends_on: []
    blocks: []
    owner: "test"
    write_scope: []
    verification_required: false
    notes: ""
`)
	// Paused plan
	write(".agents/workflow/plans/beta/PLAN.yaml", `schema_version: 1
id: "beta"
title: "Beta Plan"
status: "paused"
summary: "second"
created_at: "2026-04-10T10:00:00Z"
updated_at: "2026-04-10T10:00:00Z"
owner: "test"
success_criteria: ""
verification_strategy: ""
current_focus_task: "beta-focus-task"
`)
	write(".agents/workflow/plans/beta/TASKS.yaml", `schema_version: 1
plan_id: "beta"
tasks:
  - id: "b1"
    title: "beta-focus-task"
    status: "pending"
    depends_on: []
    blocks: []
    owner: "test"
    write_scope: []
    verification_required: false
    notes: ""
`)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.CanonicalPlans) != 2 {
		t.Fatalf("expected 2 canonical plans, got %d", len(state.CanonicalPlans))
	}
	// NextAction must come from the active plan (alpha), not the paused one (beta)
	if state.NextAction != "alpha-focus-task" {
		t.Fatalf("next action = %q, want 'alpha-focus-task'", state.NextAction)
	}
	// Orient output must include both plans
	var buf bytes.Buffer
	renderWorkflowOrientMarkdown(state, &buf)
	rendered := buf.String()
	if !strings.Contains(rendered, "Alpha Plan") {
		t.Fatalf("orient missing Alpha Plan:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Beta Plan") {
		t.Fatalf("orient missing Beta Plan:\n%s", rendered)
	}
}

// TestWorkflow_DirtyGitWarning modifies a file without committing and verifies that
// collectWorkflowState reports dirty_file_count > 0 in the git summary.
func TestWorkflow_DirtyGitWarning(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	// initWorkflowTestRepo already writes README.md without committing — README.md is dirty
	// Confirm by writing another dirty file
	dirtyPath := filepath.Join(repo, "dirty.txt")
	if err := os.WriteFile(dirtyPath, []byte("uncommitted change\n"), 0644); err != nil {
		t.Fatal(err)
	}

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Git.DirtyFileCount == 0 {
		t.Fatal("expected dirty_file_count > 0 for repo with uncommitted changes")
	}
}

// TestWorkflow_EmptyStateGraceful runs orient and health in a fresh repo with no plans,
// no checkpoint, and no proposals — verifying valid state is returned without errors.
func TestWorkflow_EmptyStateGraceful(t *testing.T) {
	repo := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	// Minimal git repo with .agentsrc.json — no plans, no checkpoint
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}
	run("init")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")

	rcPath := filepath.Join(repo, ".agentsrc.json")
	if err := os.WriteFile(rcPath, []byte(`{"project":"empty-proj","version":1,"sources":[{"type":"local"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	readmePath := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readmePath, []byte("empty\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatalf("collectWorkflowState on empty repo failed: %v", err)
	}
	if state.Project.Name != "empty-proj" {
		t.Fatalf("project name = %q, want empty-proj", state.Project.Name)
	}
	if len(state.ActivePlans) != 0 {
		t.Fatalf("expected 0 active plans, got %d", len(state.ActivePlans))
	}
	if len(state.CanonicalPlans) != 0 {
		t.Fatalf("expected 0 canonical plans, got %d", len(state.CanonicalPlans))
	}
	if state.Checkpoint != nil {
		t.Fatal("expected nil checkpoint in empty repo")
	}
	if state.NextAction == "" {
		t.Fatal("NextAction should not be empty even with no plans")
	}

	// Health should return valid (possibly warn, but not error, and not panic)
	health := computeWorkflowHealth(state)
	if health.Status == "" {
		t.Fatal("health status should not be empty")
	}
	if health.Status == "error" {
		t.Fatalf("health status = 'error' for empty-but-valid repo; warnings: %v", health.Warnings)
	}

	// Orient renders without panic
	var buf bytes.Buffer
	renderWorkflowOrientMarkdown(state, &buf)
	rendered := buf.String()
	if !strings.Contains(rendered, "# Next Action") {
		t.Fatalf("orient output missing Next Action section:\n%s", rendered)
	}
}

func TestCollectWorkflowStateIncludesCanonicalPlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	addCanonicalPlanFixture(t, repo)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.CanonicalPlans) != 1 {
		t.Fatalf("canonical plans count = %d, want 1", len(state.CanonicalPlans))
	}
	if state.CanonicalPlans[0].ID != "wave-2" {
		t.Fatalf("canonical plan id = %q", state.CanonicalPlans[0].ID)
	}
	// With no checkpoint, canonical plan's focus task should drive NextAction
	if state.NextAction != "implement structs" {
		t.Fatalf("next action = %q, want 'implement structs'", state.NextAction)
	}
	if state.NextActionSource != "canonical_plan" {
		t.Fatalf("next action source = %q, want canonical_plan", state.NextActionSource)
	}
}

// ── Wave 4: Preference tests ──────────────────────────────────────────────────
