package commands

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func initWorkflowTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
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

	write := func(rel, content string) {
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write(".agentsrc.json", `{"project":"workflow-proj","version":1,"sources":[{"type":"local"}]}`)
	write(".agents/active/sample.plan.md", "# Sample Plan\n\n- [ ] First pending task\n- [ ] Second pending task\n")
	write(".agents/active/handoffs/next.md", "# Next Handoff\n")
	write(".agents/lessons.md", "- lesson one\n- lesson two\n")
	write("README.md", "hello\n")
	run("add", ".")
	run("commit", "-m", "initial")
	write("README.md", "hello world\n")
	return repo
}

func TestCurrentWorkflowProjectUsesManifestProjectName(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	project, err := currentWorkflowProject()
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "workflow-proj" {
		t.Fatalf("project.Name = %q, want workflow-proj", project.Name)
	}
	gotPath, err := filepath.EvalSymlinks(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	wantPath, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("project.Path = %q, want %q", gotPath, wantPath)
	}
}

func TestCollectWorkflowStateReadsPlansCheckpointSourcesAndProposals(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	contextDir := filepath.Join(config.AgentsContextDir(), "workflow-proj")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatal(err)
	}
	checkpoint := `schema_version: 1
timestamp: "2026-04-10T10:00:00Z"
project:
  name: "workflow-proj"
  path: "` + repo + `"
git:
  branch: "main"
  sha: "abc1234"
  dirty_file_count: 1
files:
  modified:
    - "README.md"
message: ""
verification:
  status: "pass"
  summary: "go test ./... passed"
next_action: "Resume implementation"
blockers: []
`
	if err := os.WriteFile(filepath.Join(contextDir, "checkpoint.yaml"), []byte(checkpoint), 0644); err != nil {
		t.Fatal(err)
	}
	proposalsDir := filepath.Join(agentsHome, "proposals")
	if err := os.MkdirAll(proposalsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proposalsDir, "one.yaml"), []byte("id: one\n"), 0644); err != nil {
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
	if state.Project.Name != "workflow-proj" {
		t.Fatalf("project name = %q", state.Project.Name)
	}
	if len(state.ActivePlans) != 1 || state.ActivePlans[0].Title != "Sample Plan" {
		t.Fatalf("unexpected plans: %+v", state.ActivePlans)
	}
	if len(state.ActivePlans[0].PendingItems) == 0 || state.ActivePlans[0].PendingItems[0] != "First pending task" {
		t.Fatalf("unexpected pending items: %+v", state.ActivePlans[0].PendingItems)
	}
	if state.Checkpoint == nil || state.Checkpoint.NextAction != "Resume implementation" {
		t.Fatalf("unexpected checkpoint: %+v", state.Checkpoint)
	}
	if state.NextAction != "Resume implementation" {
		t.Fatalf("next action = %q, want Resume implementation", state.NextAction)
	}
	if len(state.Handoffs) != 1 || state.Handoffs[0].Title != "Next Handoff" {
		t.Fatalf("unexpected handoffs: %+v", state.Handoffs)
	}
	if len(state.Lessons) != 2 {
		t.Fatalf("unexpected lessons: %+v", state.Lessons)
	}
	if state.Proposals.PendingCount != 1 {
		t.Fatalf("pending proposals = %d, want 1", state.Proposals.PendingCount)
	}
}

func TestAppendWorkflowSessionLogAndSplitEntries(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "session-log.md")

	first := workflowCheckpoint{Timestamp: "2026-04-10T10:00:00Z", NextAction: "one"}
	first.Git.Branch = "main"
	first.Git.SHA = "abc1234"
	first.Verification.Status = "pass"
	first.Files.Modified = []string{"a.go"}
	if err := appendWorkflowSessionLog(logPath, first); err != nil {
		t.Fatal(err)
	}

	second := workflowCheckpoint{Timestamp: "2026-04-10T11:00:00Z", NextAction: "two"}
	second.Git.Branch = "main"
	second.Git.SHA = "def5678"
	second.Verification.Status = "unknown"
	if err := appendWorkflowSessionLog(logPath, second); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := splitWorkflowLogEntries(string(content))
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2\n%s", len(entries), string(content))
	}
	if !strings.Contains(entries[1], "next_action: two") {
		t.Fatalf("unexpected second entry: %s", entries[1])
	}
}

func TestRenderWorkflowOrientMarkdownIncludesRequiredSections(t *testing.T) {
	state := &workflowOrientState{
		Project:        workflowProjectRef{Name: "workflow-proj", Path: "/tmp/workflow-proj"},
		Git:            workflowGitSummary{Branch: "main", SHA: "abc1234", DirtyFileCount: 2, RecentCommits: []string{"abc1234 init"}},
		ActivePlans:    []workflowPlanSummary{{Title: "Plan", Path: "/tmp/workflow-proj/.agents/active/plan.plan.md", PendingItems: []string{"first"}}},
		CanonicalPlans: []workflowCanonicalPlanSummary{{ID: "cp1", Title: "Canonical Plan", Status: "active", CurrentFocusTask: "do thing"}},
		Checkpoint:     &workflowCheckpoint{Timestamp: "2026-04-10T10:00:00Z", NextAction: "do work"},
		Handoffs:       []workflowHandoffSummary{{Title: "handoff", Path: "/tmp/handoff.md"}},
		Lessons:        []string{"lesson"},
		Proposals:      workflowProposalSummary{PendingCount: 2},
		NextAction:     "do work",
	}

	var buf bytes.Buffer
	renderWorkflowOrientMarkdown(state, &buf)
	rendered := buf.String()
	for _, heading := range []string{
		"# Project",
		"# Canonical Plans",
		"# Active Plans",
		"# Last Checkpoint",
		"# Pending Handoffs",
		"# Recent Lessons",
		"# Pending Proposals",
		"# Next Action",
	} {
		if !strings.Contains(rendered, heading) {
			t.Fatalf("rendered orient output missing %q:\n%s", heading, rendered)
		}
	}
	if !strings.Contains(rendered, "Canonical Plan") {
		t.Fatalf("rendered orient output missing canonical plan title:\n%s", rendered)
	}
}

func TestIsValidVerificationStatus(t *testing.T) {
	for _, status := range []string{"pass", "fail", "partial", "unknown"} {
		if !isValidVerificationStatus(status) {
			t.Fatalf("expected %q to be valid", status)
		}
	}
	if isValidVerificationStatus("broken") {
		t.Fatal("expected broken to be invalid")
	}
}

// ── Canonical plan helpers ────────────────────────────────────────────────────

func addCanonicalPlanFixture(t *testing.T, repo string) {
	t.Helper()
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
	write(".agents/workflow/plans/wave-2/PLAN.yaml", `schema_version: 1
id: "wave-2"
title: "Wave 2 Test Plan"
status: "active"
summary: "Canonical plan fixture for tests"
created_at: "2026-04-10T10:00:00Z"
updated_at: "2026-04-10T10:00:00Z"
owner: "test"
success_criteria: "all tasks complete"
verification_strategy: "go test"
current_focus_task: "implement structs"
`)
	write(".agents/workflow/plans/wave-2/TASKS.yaml", `schema_version: 1
plan_id: "wave-2"
tasks:
  - id: "t1"
    title: "implement structs"
    status: "in_progress"
    depends_on: []
    blocks: ["t2"]
    owner: "test"
    write_scope: ["commands/workflow.go"]
    verification_required: true
    notes: ""
  - id: "t2"
    title: "add subcommands"
    status: "pending"
    depends_on: ["t1"]
    blocks: ["t3"]
    owner: "test"
    write_scope: ["commands/workflow.go"]
    verification_required: true
    notes: ""
  - id: "t3"
    title: "add tests"
    status: "completed"
    depends_on: ["t2"]
    blocks: []
    owner: "test"
    write_scope: ["commands/workflow_test.go"]
    verification_required: false
    notes: "done"
`)
}

// ── Canonical plan tests ──────────────────────────────────────────────────────

func TestIsValidPlanStatus(t *testing.T) {
	for _, s := range []string{"draft", "active", "paused", "completed", "archived"} {
		if !isValidPlanStatus(s) {
			t.Fatalf("expected %q to be valid plan status", s)
		}
	}
	if isValidPlanStatus("unknown") {
		t.Fatal("expected 'unknown' to be invalid plan status")
	}
}

func TestIsValidTaskStatus(t *testing.T) {
	for _, s := range []string{"pending", "in_progress", "blocked", "completed", "cancelled"} {
		if !isValidTaskStatus(s) {
			t.Fatalf("expected %q to be valid task status", s)
		}
	}
	if isValidTaskStatus("active") {
		t.Fatal("expected 'active' to be invalid task status")
	}
}

func TestListCanonicalPlanIDsEmptyWhenDirAbsent(t *testing.T) {
	tmp := t.TempDir()
	ids, err := listCanonicalPlanIDs(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty ids, got %v", ids)
	}
}

func TestListCanonicalPlanIDs(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	ids, err := listCanonicalPlanIDs(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "wave-2" {
		t.Fatalf("expected [wave-2], got %v", ids)
	}
}

func TestLoadCanonicalPlanRoundTrip(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	plan, err := loadCanonicalPlan(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID != "wave-2" {
		t.Fatalf("id = %q", plan.ID)
	}
	if plan.Status != "active" {
		t.Fatalf("status = %q", plan.Status)
	}
	if plan.CurrentFocusTask != "implement structs" {
		t.Fatalf("current_focus_task = %q", plan.CurrentFocusTask)
	}

	// Round-trip: save and reload
	plan.Title = "Updated Title"
	if err := saveCanonicalPlan(repo, plan); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadCanonicalPlan(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Title != "Updated Title" {
		t.Fatalf("reloaded title = %q, want Updated Title", reloaded.Title)
	}
}

func TestLoadCanonicalTasksRoundTrip(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	tf, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if tf.PlanID != "wave-2" {
		t.Fatalf("plan_id = %q", tf.PlanID)
	}
	if len(tf.Tasks) != 3 {
		t.Fatalf("task count = %d, want 3", len(tf.Tasks))
	}
	if tf.Tasks[0].ID != "t1" || tf.Tasks[0].Status != "in_progress" {
		t.Fatalf("unexpected first task: %+v", tf.Tasks[0])
	}
	if tf.Tasks[2].Status != "completed" {
		t.Fatalf("expected t3 to be completed, got %q", tf.Tasks[2].Status)
	}

	// Round-trip: save and reload
	tf.Tasks[1].Status = "in_progress"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Tasks[1].Status != "in_progress" {
		t.Fatalf("reloaded t2 status = %q, want in_progress", reloaded.Tasks[1].Status)
	}
}

func TestCollectCanonicalPlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	summaries, warnings := collectCanonicalPlans(repo)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	s := summaries[0]
	if s.ID != "wave-2" {
		t.Fatalf("id = %q", s.ID)
	}
	if s.Status != "active" {
		t.Fatalf("status = %q", s.Status)
	}
	if s.CurrentFocusTask != "implement structs" {
		t.Fatalf("focus = %q", s.CurrentFocusTask)
	}
	// t1=in_progress -> pending, t2=pending, t3=completed
	if s.PendingCount != 2 {
		t.Fatalf("pending count = %d, want 2", s.PendingCount)
	}
	if s.CompletedCount != 1 {
		t.Fatalf("completed count = %d, want 1", s.CompletedCount)
	}
}

func TestRunWorkflowAdvanceUpdatesTaskAndPlan(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowAdvance("wave-2", "t2", "in_progress"); err != nil {
		t.Fatal(err)
	}

	// Tasks updated
	tf, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tf.Tasks {
		if task.ID == "t2" && task.Status != "in_progress" {
			t.Fatalf("t2 status = %q, want in_progress", task.Status)
		}
	}

	// Plan focus task updated
	plan, err := loadCanonicalPlan(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentFocusTask != "add subcommands" {
		t.Fatalf("current_focus_task = %q, want add subcommands", plan.CurrentFocusTask)
	}
}

func TestRunWorkflowAdvanceInvalidStatus(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	err := runWorkflowAdvance("wave-2", "t1", "active")
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid task status") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWorkflowAdvanceMissingTask(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	err := runWorkflowAdvance("wave-2", "t999", "completed")
	if err == nil {
		t.Fatal("expected error for missing task, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── Usage-flow scenario tests ─────────────────────────────────────────────────

// writeCheckpointFixture writes a checkpoint.yaml into the AGENTS_HOME context dir for the given project.
func writeCheckpointFixture(t *testing.T, agentsHome, projectName, repo string, nextAction, verStatus, timestamp string) {
	t.Helper()
	contextDir := filepath.Join(agentsHome, "context", projectName)
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatal(err)
	}
	checkpoint := `schema_version: 1
timestamp: "` + timestamp + `"
project:
  name: "` + projectName + `"
  path: "` + repo + `"
git:
  branch: "main"
  sha: "abc1234"
  dirty_file_count: 0
files:
  modified: []
message: ""
verification:
  status: "` + verStatus + `"
  summary: "tests passed"
next_action: "` + nextAction + `"
blockers: []
`
	if err := os.WriteFile(filepath.Join(contextDir, "checkpoint.yaml"), []byte(checkpoint), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestWorkflow_CheckpointThenOrient writes a checkpoint with verification data and then
// verifies that collectWorkflowState reflects the checkpoint's next_action and verification status.
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
	// Next action still comes from checkpoint
	if state.NextAction != "old task" {
		t.Fatalf("next action = %q, want 'old task'", state.NextAction)
	}
	// Orient renders cleanly with the old timestamp
	var buf bytes.Buffer
	renderWorkflowOrientMarkdown(state, &buf)
	rendered := buf.String()
	if !strings.Contains(rendered, "2026-04-01T00:00:00Z") {
		t.Fatalf("orient output missing stale timestamp:\n%s", rendered)
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
}

// ── Wave 4: Preference tests ──────────────────────────────────────────────────

func TestPreferences_DefaultsAreNonEmpty(t *testing.T) {
	d := defaultWorkflowPreferences()
	if d.Verification.TestCommand == nil || *d.Verification.TestCommand == "" {
		t.Fatal("default test_command must be set")
	}
	if d.Planning.PlanDirectory == nil || *d.Planning.PlanDirectory == "" {
		t.Fatal("default plan_directory must be set")
	}
	if d.Execution.Formatter == nil || *d.Execution.Formatter == "" {
		t.Fatal("default formatter must be set")
	}
}

func TestPreferences_MergeNoOverrides(t *testing.T) {
	d := defaultWorkflowPreferences()
	out := mergePreferences(d, WorkflowPreferences{}, WorkflowPreferences{})
	if strPtrVal(out.Verification.TestCommand) != strPtrVal(d.Verification.TestCommand) {
		t.Fatalf("test_command changed without override")
	}
}

func TestPreferences_RepoOverridesDefault(t *testing.T) {
	d := defaultWorkflowPreferences()
	cmd := "make test"
	repo := WorkflowPreferences{Verification: WorkflowVerificationPrefs{TestCommand: &cmd}}
	out := mergePreferences(d, repo, WorkflowPreferences{})
	if strPtrVal(out.Verification.TestCommand) != "make test" {
		t.Fatalf("repo override not applied: got %q", strPtrVal(out.Verification.TestCommand))
	}
	// Other defaults must be preserved
	if strPtrVal(out.Execution.Formatter) != strPtrVal(d.Execution.Formatter) {
		t.Fatalf("other defaults lost after repo override")
	}
}

func TestPreferences_LocalTrumpsRepo(t *testing.T) {
	d := defaultWorkflowPreferences()
	repo := "make test"
	local := "npm test"
	repoPrefs := WorkflowPreferences{Verification: WorkflowVerificationPrefs{TestCommand: &repo}}
	localPrefs := WorkflowPreferences{Verification: WorkflowVerificationPrefs{TestCommand: &local}}
	out := mergePreferences(d, repoPrefs, localPrefs)
	if strPtrVal(out.Verification.TestCommand) != "npm test" {
		t.Fatalf("local pref did not trump repo: got %q", strPtrVal(out.Verification.TestCommand))
	}
}

func TestPreferences_SetLocalPersists(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := setLocalPreference("my-proj", "verification.test_command", "pytest"); err != nil {
		t.Fatal(err)
	}

	f, err := loadLocalPreferences("my-proj")
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatal("loadLocalPreferences returned nil after set")
	}
	if strPtrVal(f.Verification.TestCommand) != "pytest" {
		t.Fatalf("persisted test_command = %q, want pytest", strPtrVal(f.Verification.TestCommand))
	}
}

func TestPreferences_SetLocalUpdateExisting(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	_ = setLocalPreference("my-proj", "verification.test_command", "pytest")
	_ = setLocalPreference("my-proj", "execution.formatter", "black")
	// Now update test_command
	_ = setLocalPreference("my-proj", "verification.test_command", "pytest -x")

	f, _ := loadLocalPreferences("my-proj")
	if strPtrVal(f.Verification.TestCommand) != "pytest -x" {
		t.Fatalf("updated test_command = %q, want 'pytest -x'", strPtrVal(f.Verification.TestCommand))
	}
	// Other key must not be clobbered
	if strPtrVal(f.Execution.Formatter) != "black" {
		t.Fatalf("formatter clobbered: got %q", strPtrVal(f.Execution.Formatter))
	}
}

func TestPreferences_InvalidKeyRejected(t *testing.T) {
	if err := applyPreferenceKey(&WorkflowPreferences{}, "nonexistent.key", "val"); err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
}

func TestPreferences_BoolField(t *testing.T) {
	p := WorkflowPreferences{}
	if err := applyPreferenceKey(&p, "verification.require_regression_before_handoff", "false"); err != nil {
		t.Fatal(err)
	}
	if p.Verification.RequireRegressionBeforeHandoff == nil {
		t.Fatal("bool field nil after apply")
	}
	if *p.Verification.RequireRegressionBeforeHandoff != false {
		t.Fatal("expected false")
	}
}

func TestPreferences_ResolveWithSources(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	// Write a repo preference
	repoPrefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(repoPrefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPrefsDir, "preferences.yaml"), []byte("schema_version: 1\nverification:\n  test_command: make test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a local override
	if err := setLocalPreference("workflow-proj", "execution.formatter", "prettier"); err != nil {
		t.Fatal(err)
	}

	sources, err := resolvePreferencesWithSources(repo, "workflow-proj")
	if err != nil {
		t.Fatal(err)
	}

	srcMap := make(map[string]preferenceSource)
	for _, s := range sources {
		srcMap[s.Key] = s
	}

	if srcMap["verification.test_command"].Source != "repo" {
		t.Fatalf("test_command source = %q, want repo", srcMap["verification.test_command"].Source)
	}
	if srcMap["verification.test_command"].Value != "make test" {
		t.Fatalf("test_command value = %q, want 'make test'", srcMap["verification.test_command"].Value)
	}
	if srcMap["execution.formatter"].Source != "local" {
		t.Fatalf("formatter source = %q, want local", srcMap["execution.formatter"].Source)
	}
	if srcMap["verification.lint_command"].Source != "default" {
		t.Fatalf("lint_command source = %q, want default", srcMap["verification.lint_command"].Source)
	}
}

func TestPreferences_OrientIncludesPreferences(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Preferences == nil {
		t.Fatal("workflowOrientState.Preferences must be populated by collectWorkflowState")
	}

	var buf bytes.Buffer
	renderWorkflowOrientMarkdown(state, &buf)
	rendered := buf.String()
	if !strings.Contains(rendered, "# Preferences") {
		t.Fatalf("orient output missing Preferences section:\n%s", rendered)
	}
	if !strings.Contains(rendered, "test_command") {
		t.Fatalf("orient output missing test_command:\n%s", rendered)
	}
}
