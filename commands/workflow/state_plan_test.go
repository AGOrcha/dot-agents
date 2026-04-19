package workflow

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

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
	if state.NextAction != "First pending task" {
		t.Fatalf("next action = %q, want First pending task", state.NextAction)
	}
	if state.NextActionSource != "active_plan" {
		t.Fatalf("next action source = %q, want active_plan", state.NextActionSource)
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

func TestListCanonicalPlanIDsSkipsDirsWithoutPlanYAML(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	ghostDir := filepath.Join(repo, ".agents", "workflow", "plans", "ghost-plan")
	if err := os.MkdirAll(ghostDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ghostDir, "TASKS.yaml"), []byte("schema_version: 1\nplan_id: ghost-plan\ntasks: []\n"), 0644); err != nil {
		t.Fatal(err)
	}

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

func TestCollectCanonicalPlansIgnoresTombstonedPlanDirs(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	ghostDir := filepath.Join(repo, ".agents", "workflow", "plans", "ghost-plan")
	if err := os.MkdirAll(ghostDir, 0755); err != nil {
		t.Fatal(err)
	}

	summaries, warnings := collectCanonicalPlans(repo)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(summaries) != 1 || summaries[0].ID != "wave-2" {
		t.Fatalf("unexpected summaries: %+v", summaries)
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

// TestObs1776217867311807000_WorkflowAdvancePersistsTaskRowOnDisk covers fold-back proposal
// obs-1776217867311807000: `workflow advance` must update the on-disk TASKS.yaml task status
// (not only PLAN metadata) for a long task id similar to loop-runtime-refactor/phase-5d-iter-log-schema.
func TestObs1776217867311807000_WorkflowAdvancePersistsTaskRowOnDisk(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addObs1776217867311807000PlanFixture(t, repo)

	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	const planID = "wf-advance-regress"
	const taskID = "phase-5d-iter-log-schema"

	if err := runWorkflowAdvance(planID, taskID, "in_progress"); err != nil {
		t.Fatal(err)
	}

	tasksPath := filepath.Join(repo, ".agents", "workflow", "plans", planID, "TASKS.yaml")
	raw, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, taskID) {
		t.Fatalf("TASKS.yaml on disk missing task id %q", taskID)
	}
	if !strings.Contains(s, "status: \"in_progress\"") && !strings.Contains(s, "status: in_progress") {
		t.Fatalf("TASKS.yaml on disk missing in_progress status after advance:\n%s", s)
	}

	tf, err := loadCanonicalTasks(repo, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tf.Tasks) != 1 || tf.Tasks[0].ID != taskID || tf.Tasks[0].Status != "in_progress" {
		t.Fatalf("reload tasks = %+v, want one row %s in_progress", tf.Tasks, taskID)
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

func TestBuildWorkflowPlanGraph(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	addCanonicalSliceFixture(t, repo, "wave-2")

	graph, err := buildWorkflowPlanGraph(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 6 {
		t.Fatalf("node count = %d, want 6", len(graph.Nodes))
	}
	if len(graph.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", graph.Warnings)
	}

	contains, dependsOn, blocks := 0, 0, 0
	for _, edge := range graph.Edges {
		switch edge.Type {
		case "contains":
			contains++
		case "depends_on":
			dependsOn++
		case "blocks":
			blocks++
		}
	}
	if contains != 5 {
		t.Fatalf("contains edges = %d, want 5", contains)
	}
	if dependsOn != 3 {
		t.Fatalf("depends_on edges = %d, want 3", dependsOn)
	}
	if blocks != 2 {
		t.Fatalf("blocks edges = %d, want 2", blocks)
	}
}

func TestBuildWorkflowPlanGraphMissingPlan(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	_, err := buildWorkflowPlanGraph(repo, "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `plan "missing" not found`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWorkflowPlanGraphRendersPlanAndTasks(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	addCanonicalSliceFixture(t, repo, "wave-2")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	if err := runWorkflowPlanGraph("wave-2"); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old

	rendered := string(out)
	for _, want := range []string{
		"Canonical Plan Graph: wave-2",
		"[wave-2] Wave 2 Test Plan",
		"-> [t1] implement structs",
		"=> [slice-read-surface] Read surface",
		"depends_on: implement structs",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered graph missing %q:\n%s", want, rendered)
		}
	}
}

func TestLoadCanonicalSlicesRoundTrip(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	addCanonicalSliceFixture(t, repo, "wave-2")

	sf, err := loadCanonicalSlices(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if sf.PlanID != "wave-2" {
		t.Fatalf("plan_id = %q", sf.PlanID)
	}
	if len(sf.Slices) != 2 {
		t.Fatalf("slice count = %d, want 2", len(sf.Slices))
	}
	if sf.Slices[1].DependsOn[0] != "slice-read-surface" {
		t.Fatalf("unexpected slice dependency: %+v", sf.Slices[1].DependsOn)
	}
}

func TestRunWorkflowSlicesRendersSlices(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	addCanonicalSliceFixture(t, repo, "wave-2")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	if err := runWorkflowSlices("wave-2"); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old

	rendered := string(out)
	for _, want := range []string{
		"Slices: wave-2",
		"[slice-read-surface] Read surface",
		"task: t1",
		"write scope: commands/workflow.go, commands/workflow_test.go",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered slices missing %q:\n%s", want, rendered)
		}
	}
}

func TestSelectNextCanonicalTaskPrefersInProgressFocusTask(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	suggestion, err := selectNextCanonicalTask(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if suggestion == nil {
		t.Fatal("expected suggestion, got nil")
	}
	if suggestion.PlanID != "wave-2" || suggestion.TaskID != "t1" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if suggestion.Reason != "current focus task is already in progress" {
		t.Fatalf("unexpected reason: %q", suggestion.Reason)
	}
}

func TestSelectNextCanonicalTask_ScopedToPlansWithActiveDelegation(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	addCanonicalPendingPlanFixture(t, repo)

	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-t1", ParentPlanID: "wave-2", ParentTaskID: "t1",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}

	suggestion, err := selectNextCanonicalTask(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if suggestion != nil {
		t.Fatalf("expected nil while wave-2 has an active delegation and remaining tasks there are blocked/skipped, got %+v", suggestion)
	}
}

func TestSelectNextCanonicalTask_ExplicitUnknownPlan(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	_, err := selectNextCanonicalTask(repo, "missing-plan")
	if err == nil {
		t.Fatal("expected error for unknown plan id")
	}
}

func TestSelectNextCanonicalTask_ExplicitPausedPlanReturnsNil(t *testing.T) {
	repo := initWorkflowTestRepo(t)

	plan := &CanonicalPlan{
		SchemaVersion:    1,
		ID:               "paused-plan",
		Title:            "Paused Plan",
		Status:           "paused",
		Summary:          "paused for planning review",
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
		Owner:            "test",
		CurrentFocusTask: "needs review",
	}
	if err := saveCanonicalPlan(repo, plan); err != nil {
		t.Fatal(err)
	}
	tf := &CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        "paused-plan",
		Tasks: []CanonicalTask{
			{
				ID:                   "needs-review",
				Title:                "needs review",
				Status:               "pending",
				DependsOn:            nil,
				Blocks:               nil,
				Owner:                "test",
				WriteScope:           []string{"commands/workflow.go"},
				VerificationRequired: true,
			},
		},
	}
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}

	suggestion, err := selectNextCanonicalTask(repo, "paused-plan")
	if err != nil {
		t.Fatal(err)
	}
	if suggestion != nil {
		t.Fatalf("expected nil for paused plan scope, got %+v", suggestion)
	}
}

func TestSelectNextCanonicalTask_ExplicitCommaSeparatedPlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	addCanonicalPendingPlanFixture(t, repo)

	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-t1", ParentPlanID: "wave-2", ParentTaskID: "t1",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}

	suggestion, err := selectNextCanonicalTask(repo, "wave-2, wave-next")
	if err != nil {
		t.Fatal(err)
	}
	if suggestion == nil {
		t.Fatal("expected suggestion, got nil")
	}
	if suggestion.PlanID != "wave-next" || suggestion.TaskID != "planner" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
}

func TestSelectNextCanonicalTask_ExplicitPlanSkipsLockedPlan(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)

	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-planner", ParentPlanID: "wave-next", ParentTaskID: "planner",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}

	suggestion, err := selectNextCanonicalTask(repo, "wave-next")
	if err != nil {
		t.Fatal(err)
	}
	if suggestion != nil {
		t.Fatalf("expected nil for locked explicit plan, got %+v", suggestion)
	}
}

func TestSelectNextCanonicalTaskChoosesUnblockedPendingTask(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)

	suggestion, err := selectNextCanonicalTask(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if suggestion == nil {
		t.Fatal("expected suggestion, got nil")
	}
	if suggestion.PlanID != "wave-next" || suggestion.TaskID != "planner" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if suggestion.Reason != "current focus task is pending and all dependencies are complete" {
		t.Fatalf("unexpected reason: %q", suggestion.Reason)
	}
}

func TestRunWorkflowNextPrintsHelpfulMessageWhenNoActionableTaskExists(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	contract := &DelegationContract{
		SchemaVersion: 1,
		ID:            "del-t1",
		ParentPlanID:  "wave-2",
		ParentTaskID:  "t1",
		Title:         "implement structs",
		Status:        "active",
		CreatedAt:     "2026-04-10T10:00:00Z",
		UpdatedAt:     "2026-04-10T10:00:00Z",
	}
	if err := saveDelegationContract(repo, contract); err != nil {
		t.Fatal(err)
	}

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	if err := runWorkflowNext(""); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old

	rendered := string(out)
	if !strings.Contains(rendered, "No actionable canonical task found.") {
		t.Fatalf("unexpected workflow next output:\n%s", rendered)
	}
}

func TestCollectWorkflowCompletionStateDistinguishesActionableLockedAndPaused(t *testing.T) {
	t.Run("actionable", func(t *testing.T) {
		repo := initWorkflowTestRepo(t)
		addCanonicalPendingPlanFixture(t, repo)

		state, err := collectWorkflowCompletionState(repo, "wave-next")
		if err != nil {
			t.Fatal(err)
		}
		if state.State != "actionable" {
			t.Fatalf("state = %q, want actionable", state.State)
		}
		if state.Next == nil || state.Next.TaskID != "planner" {
			t.Fatalf("next = %+v, want planner", state.Next)
		}
		if len(state.PausedPlans) != 0 || len(state.LockedPlans) != 0 {
			t.Fatalf("unexpected paused/locked plans: %+v", state)
		}
	})

	t.Run("locked", func(t *testing.T) {
		repo := initWorkflowTestRepo(t)
		addCanonicalPendingPlanFixture(t, repo)

		now := time.Now().UTC().Format(time.RFC3339)
		c := &DelegationContract{
			SchemaVersion: 1, ID: "del-planner", ParentPlanID: "wave-next", ParentTaskID: "planner",
			Title: "x", WriteScope: []string{"commands/"}, Status: "active",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := saveDelegationContract(repo, c); err != nil {
			t.Fatal(err)
		}

		state, err := collectWorkflowCompletionState(repo, "wave-next")
		if err != nil {
			t.Fatal(err)
		}
		if state.State != "locked" {
			t.Fatalf("state = %q, want locked", state.State)
		}
		if state.Next != nil {
			t.Fatalf("next = %+v, want nil", state.Next)
		}
		if len(state.LockedPlans) != 1 || state.LockedPlans[0] != "wave-next" {
			t.Fatalf("locked plans = %+v, want [wave-next]", state.LockedPlans)
		}
	})

	t.Run("paused", func(t *testing.T) {
		repo := initWorkflowTestRepo(t)
		plan := &CanonicalPlan{
			SchemaVersion:    1,
			ID:               "paused-plan",
			Title:            "Paused Plan",
			Status:           "paused",
			Summary:          "paused for planning review",
			CreatedAt:        time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
			Owner:            "test",
			CurrentFocusTask: "needs review",
		}
		if err := saveCanonicalPlan(repo, plan); err != nil {
			t.Fatal(err)
		}
		tf := &CanonicalTaskFile{
			SchemaVersion: 1,
			PlanID:        "paused-plan",
			Tasks: []CanonicalTask{
				{
					ID:                   "needs-review",
					Title:                "needs review",
					Status:               "pending",
					DependsOn:            nil,
					Blocks:               nil,
					Owner:                "test",
					WriteScope:           []string{"commands/workflow.go"},
					VerificationRequired: true,
				},
			},
		}
		if err := saveCanonicalTasks(repo, tf); err != nil {
			t.Fatal(err)
		}

		state, err := collectWorkflowCompletionState(repo, "paused-plan")
		if err != nil {
			t.Fatal(err)
		}
		if state.State != "paused" {
			t.Fatalf("state = %q, want paused", state.State)
		}
		if state.Next != nil {
			t.Fatalf("next = %+v, want nil", state.Next)
		}
		if len(state.PausedPlans) != 1 || state.PausedPlans[0] != "paused-plan" {
			t.Fatalf("paused plans = %+v, want [paused-plan]", state.PausedPlans)
		}
	})
}

func TestRunWorkflowCompleteRejectsBlankPlanFilter(t *testing.T) {
	repo := initWorkflowTestRepo(t)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowComplete("   "); err == nil || !strings.Contains(err.Error(), "--plan must not be empty") {
		t.Fatalf("expected blank-plan error, got %v", err)
	}
}

// ── Usage-flow scenario tests ─────────────────────────────────────────────────

// TestWorkflow_CheckpointThenOrient writes a checkpoint with verification data and then
