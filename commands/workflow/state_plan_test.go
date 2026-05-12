package workflow

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func seedWorkflowStateContext(t *testing.T, repo, agentsHome string) {
	t.Helper()
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
	pendingProposal := `schema_version: 1
id: one
status: pending
type: rule
action: add
target: rules/global/one.mdc
rationale: pending proposal
content: |
  rule
created_at: "2026-04-10T00:00:00Z"
created_by: test
`
	if err := os.WriteFile(filepath.Join(proposalsDir, "one.yaml"), []byte(pendingProposal), 0644); err != nil {
		t.Fatal(err)
	}
	draftProposal := `schema_version: 1
id: draft-one
status: draft
type: skill
action: add
target: skills/global/draft-one/SKILL.md
rationale: draft proposal
content: |
  draft
created_at: "2026-04-10T00:00:00Z"
created_by: test
`
	if err := os.WriteFile(filepath.Join(proposalsDir, "draft-one.yaml"), []byte(draftProposal), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertWorkflowStateOverview(t *testing.T, state *workflowOrientState) {
	t.Helper()
	if state.Project.Name != "workflow-proj" {
		t.Fatalf("project name = %q", state.Project.Name)
	}
	if len(state.ActivePlans) != 1 || state.ActivePlans[0].Title != "Sample Plan" {
		t.Fatalf("unexpected plans: %+v", state.ActivePlans)
	}
	if len(state.ActivePlans[0].PendingItems) == 0 || state.ActivePlans[0].PendingItems[0] != "First pending task" {
		t.Fatalf("unexpected pending items: %+v", state.ActivePlans[0].PendingItems)
	}
}

func assertWorkflowStateAncillary(t *testing.T, state *workflowOrientState) {
	t.Helper()
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

func assertCompletionStateActionable(t *testing.T, state *workflowCompletionScopeState) {
	t.Helper()
	if state.State != "actionable" {
		t.Fatalf("state = %q, want actionable", state.State)
	}
	if state.Next == nil || state.Next.TaskID != "planner" {
		t.Fatalf("next = %+v, want planner", state.Next)
	}
	if len(state.PausedPlans) != 0 || len(state.LockedPlans) != 0 {
		t.Fatalf("unexpected paused/locked plans: %+v", state)
	}
}

func assertCompletionStateLocked(t *testing.T, state *workflowCompletionScopeState) {
	t.Helper()
	if state.State != "locked" {
		t.Fatalf("state = %q, want locked", state.State)
	}
	if state.Next != nil {
		t.Fatalf("next = %+v, want nil", state.Next)
	}
	if len(state.LockedPlans) != 1 || state.LockedPlans[0] != "wave-next" {
		t.Fatalf("locked plans = %+v, want [wave-next]", state.LockedPlans)
	}
}

func assertCompletionStatePaused(t *testing.T, state *workflowCompletionScopeState) {
	t.Helper()
	if state.State != "paused" {
		t.Fatalf("state = %q, want paused", state.State)
	}
	if state.Next != nil {
		t.Fatalf("next = %+v, want nil", state.Next)
	}
	if len(state.PausedPlans) != 1 || state.PausedPlans[0] != "paused-plan" {
		t.Fatalf("paused plans = %+v, want [paused-plan]", state.PausedPlans)
	}
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
	seedWorkflowStateContext(t, repo, agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("project-and-plans", func(t *testing.T) {
		assertWorkflowStateOverview(t, state)
	})

	t.Run("checkpoint-and-actions", func(t *testing.T) {
		if state.Checkpoint == nil || state.Checkpoint.NextAction != "Resume implementation" {
			t.Fatalf("unexpected checkpoint: %+v", state.Checkpoint)
		}
		if state.NextAction != "First pending task" {
			t.Fatalf("next action = %q, want First pending task", state.NextAction)
		}
		if state.NextActionSource != "active_plan" {
			t.Fatalf("next action source = %q, want active_plan", state.NextActionSource)
		}
	})

	t.Run("ancillary-artifacts", func(t *testing.T) {
		assertWorkflowStateAncillary(t, state)
	})
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

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanGraph("wave-2") },
		"Canonical Plan Graph: wave-2",
		"[wave-2] Wave 2 Test Plan",
		"-> [t1] implement structs",
		"=> [slice-read-surface] Read surface",
		"depends_on: implement structs",
	)
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

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowSlices("wave-2") },
		"Slices: wave-2",
		"[slice-read-surface] Read surface",
		"task: t1",
		"write scope: commands/workflow.go, commands/workflow_test.go",
	)
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
	writeActiveDelegationContract(t, repo, "del-t1", "wave-2", "t1")

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
	savePausedPlanFixture(t, repo)

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
	writeActiveDelegationContract(t, repo, "del-t1", "wave-2", "t1")

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
	writeActiveDelegationContract(t, repo, "del-planner", "wave-next", "planner")

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
		assertCompletionStateActionable(t, state)
	})

	t.Run("locked", func(t *testing.T) {
		repo := initWorkflowTestRepo(t)
		addCanonicalPendingPlanFixture(t, repo)
		now := time.Now().UTC().Format(time.RFC3339)
		if err := saveDelegationContract(repo, &DelegationContract{
			SchemaVersion: 1, ID: "del-planner", ParentPlanID: "wave-next", ParentTaskID: "planner",
			Title: "x", WriteScope: []string{"commands/"}, Status: "active",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		state, err := collectWorkflowCompletionState(repo, "wave-next")
		if err != nil {
			t.Fatal(err)
		}
		assertCompletionStateLocked(t, state)
	})

	t.Run("paused", func(t *testing.T) {
		repo := initWorkflowTestRepo(t)
		savePausedPlanFixture(t, repo)
		state, err := collectWorkflowCompletionState(repo, "paused-plan")
		if err != nil {
			t.Fatal(err)
		}
		assertCompletionStatePaused(t, state)
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

// TestRunWorkflowCheckpoint_WritesState verifies that runWorkflowCheckpoint
// writes both checkpoint.yaml and session-log.md in the project context dir.
func TestRunWorkflowCheckpoint_WritesState(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowCheckpoint("resume transport slice", "pass", "go test ./... passed"); err != nil {
		t.Fatalf("runWorkflowCheckpoint: %v", err)
	}

	contextDir := filepath.Join(agentsHome, "context", "workflow-proj")
	cpPath := filepath.Join(contextDir, "checkpoint.yaml")
	if _, err := os.Stat(cpPath); err != nil {
		t.Fatalf("checkpoint.yaml should exist: %v", err)
	}
	cpData, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(cpData)
	if !strings.Contains(s, "resume transport slice") {
		t.Error("checkpoint should contain the message")
	}
	if !strings.Contains(s, "status: \"pass\"") && !strings.Contains(s, "status: pass") {
		t.Error("checkpoint should contain verification status pass")
	}
}

// TestRunWorkflowLog_WritesEntry verifies that running checkpoint creates a
// session-log.md entry and that runWorkflowLog can read it without error.
func TestRunWorkflowLog_WritesEntry(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowCheckpoint("first checkpoint", "pass", "tests passed"); err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}

	// Verify session-log.md was created
	contextDir := filepath.Join(agentsHome, "context", "workflow-proj")
	logPath := filepath.Join(contextDir, "session-log.md")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("session-log.md should exist: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "first checkpoint") {
		t.Error("session log should contain the checkpoint message")
	}

	// Write a second checkpoint
	if err := runWorkflowCheckpoint("second checkpoint", "unknown", ""); err != nil {
		t.Fatalf("second checkpoint: %v", err)
	}
	logData, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := splitWorkflowLogEntries(string(logData))
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if !strings.Contains(entries[1], "second checkpoint") {
		t.Errorf("second entry should contain 'second checkpoint': %s", entries[1])
	}
}

// TestRunWorkflowCheckpoint_RejectsInvalidVerificationStatus verifies that an
// invalid verification status string is rejected.
func TestRunWorkflowCheckpoint_RejectsInvalidVerificationStatus(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	err := runWorkflowCheckpoint("msg", "invalid-status", "")
	if err == nil {
		t.Fatal("expected error for invalid verification status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid verification status") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunWorkflowCheckpoint_StateRoundTrip writes a checkpoint with verification data
// and verifies collectWorkflowState observes both the checkpoint and the next-action source.
func TestRunWorkflowCheckpoint_StateRoundTrip(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowCheckpoint("resume coverage", "pass", "go test ./... passed"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Checkpoint == nil {
		t.Fatal("expected checkpoint loaded into state after checkpoint write")
	}
	if state.Checkpoint.Verification.Status != "pass" {
		t.Fatalf("verification = %q, want pass", state.Checkpoint.Verification.Status)
	}
	if state.Checkpoint.Message != "resume coverage" {
		t.Fatalf("message = %q, want 'resume coverage'", state.Checkpoint.Message)
	}
	if state.Health == nil {
		t.Fatal("expected health snapshot computed during enrichment")
	}
}

// TestRunWorkflowStatus_Renders covers the human-readable status renderer.
func TestRunWorkflowStatus_Renders(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	seedWorkflowStateContext(t, repo, agentsHome)

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowStatus() },
		"Workflow Status",
		"workflow-proj",
		"branch:",
		"Last Checkpoint",
		"Next Action",
	)
}

// TestRunWorkflowOrient_Renders covers the markdown orient renderer end-to-end.
func TestRunWorkflowOrient_Renders(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowOrient() },
		"# Project",
		"# Active Plans",
		"# Next Action",
	)
}

// TestRunWorkflowLog_NoLog reports a friendly message when no session log exists.
func TestRunWorkflowLog_NoLog(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowLog(false) },
		"No session log found",
	)
}

// TestRunWorkflowLog_AllShowsEverything ensures --all includes more than the
// most recent 10 entries when many checkpoints have been written.
func TestRunWorkflowLog_AllShowsEverything(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	// Write 12 checkpoints so the default (last-10) truncates and --all does not.
	for i := 0; i < 12; i++ {
		msg := "iter-" + strconv.Itoa(i)
		if err := runWorkflowCheckpoint(msg, "pass", ""); err != nil {
			t.Fatalf("checkpoint %d: %v", i, err)
		}
	}

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowLog(true) },
		"iter-0",
		"iter-11",
	)
}

// TestCollectDelegationSummary_ActiveAndMergeBacks covers the delegation summary
// and pending-merge-back counter that feeds workflowOrientState.
func TestCollectDelegationSummary_ActiveAndMergeBacks(t *testing.T) {
	repo := initWorkflowTestRepo(t)

	// Active contract.
	writeActiveDelegationContract(t, repo, "del-active", "plan-x", "task-active")

	// Completed contract — should not be counted.
	delegDir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(delegDir, 0755); err != nil {
		t.Fatal(err)
	}
	completed := `schema_version: 1
id: del-done
parent_plan_id: plan-x
parent_task_id: task-done
title: t
write_scope: []
status: completed
created_at: "2026-04-10T00:00:00Z"
updated_at: "2026-04-10T00:00:00Z"
`
	if err := os.WriteFile(filepath.Join(delegDir, "task-done.yaml"), []byte(completed), 0644); err != nil {
		t.Fatal(err)
	}

	// Two pending merge-backs.
	mbDir := filepath.Join(repo, ".agents", "active", "merge-back")
	if err := os.MkdirAll(mbDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(mbDir, name), []byte("# merge"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Non-markdown file should be ignored.
	if err := os.WriteFile(filepath.Join(mbDir, "ignore.txt"), []byte("nope"), 0644); err != nil {
		t.Fatal(err)
	}

	summary, pendingMergebacks := collectDelegationSummary(repo)
	if summary.ActiveCount != 1 {
		t.Fatalf("ActiveCount = %d, want 1", summary.ActiveCount)
	}
	if pendingMergebacks != 2 {
		t.Fatalf("pendingMergebacks = %d, want 2", pendingMergebacks)
	}
}

// TestCollectWorkflowState_IncludesDelegationAndMergeBack ensures the orient
// state assembly carries delegations and merge-back counts through to the
// rendered surface.
func TestCollectWorkflowState_IncludesDelegationAndMergeBack(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	writeActiveDelegationContract(t, repo, "del-a", "plan-x", "task-a")
	mbDir := filepath.Join(repo, ".agents", "active", "merge-back")
	if err := os.MkdirAll(mbDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mbDir, "a.md"), []byte("# merge"), 0644); err != nil {
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
	if state.ActiveDelegations.ActiveCount != 1 {
		t.Fatalf("ActiveDelegations.ActiveCount = %d, want 1", state.ActiveDelegations.ActiveCount)
	}
	if state.PendingMergeBacks != 1 {
		t.Fatalf("PendingMergeBacks = %d, want 1", state.PendingMergeBacks)
	}

	var buf bytes.Buffer
	renderWorkflowOrientMarkdown(state, &buf)
	rendered := buf.String()
	if !strings.Contains(rendered, "# Delegations") {
		t.Fatalf("orient missing # Delegations section:\n%s", rendered)
	}
	if !strings.Contains(rendered, "active delegations: 1") {
		t.Fatalf("orient missing active delegations count:\n%s", rendered)
	}
}

// TestGitModifiedFiles_NonGit returns an empty slice for non-repo paths.
func TestGitModifiedFiles_NonGit(t *testing.T) {
	tmp := t.TempDir()
	files, err := gitModifiedFiles(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected empty modified files for non-repo, got %v", files)
	}
}

// TestGitModifiedFiles_TracksDirty surfaces dirty files in an initialized repo.
func TestGitModifiedFiles_TracksDirty(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	dirtyPath := filepath.Join(repo, "dirty.txt")
	if err := os.WriteFile(dirtyPath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	files, err := gitModifiedFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f == "dirty.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dirty.txt in modified files, got %v", files)
	}
}

// TestCollectWorkflowGitSummary_NonRepoReturnsWarning ensures the git summary
// gracefully degrades when the project path is not a git checkout.
func TestCollectWorkflowGitSummary_NonRepoReturnsWarning(t *testing.T) {
	tmp := t.TempDir()
	summary, warnings := collectWorkflowGitSummary(tmp)
	if summary.Branch != "unknown" || summary.SHA != "unknown" {
		t.Fatalf("expected unknown/unknown for non-repo, got branch=%q sha=%q", summary.Branch, summary.SHA)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning when path is not a git repo")
	}
}

// TestDeriveWorkflowNextAction_PrefersCanonicalThenActive verifies the
// fallback chain when no checkpoint is current.
func TestDeriveWorkflowNextAction_PrefersCanonicalThenActive(t *testing.T) {
	git := workflowGitSummary{Branch: "main", SHA: "abc1234"}
	canonical := []workflowCanonicalPlanSummary{{ID: "p1", Status: "active", CurrentFocusTask: "do canonical"}}
	active := []workflowPlanSummary{{Title: "active", PendingItems: []string{"do active"}}}

	action, source := deriveWorkflowNextAction(git, nil, canonical, active)
	if action != "do canonical" || source != "canonical_plan" {
		t.Fatalf("got (%q, %q), want (do canonical, canonical_plan)", action, source)
	}

	// Without canonical, fall back to active plan.
	action, source = deriveWorkflowNextAction(git, nil, nil, active)
	if action != "do active" || source != "active_plan" {
		t.Fatalf("got (%q, %q), want (do active, active_plan)", action, source)
	}

	// Stale checkpoint when nothing else is available.
	cp := &workflowCheckpoint{NextAction: "resume"}
	cp.Git.Branch = "old"
	cp.Git.SHA = "old"
	action, source = deriveWorkflowNextAction(git, cp, nil, nil)
	if action != "resume" || source != "checkpoint_stale" {
		t.Fatalf("got (%q, %q), want (resume, checkpoint_stale)", action, source)
	}

	// Default when nothing is available.
	action, source = deriveWorkflowNextAction(git, nil, nil, nil)
	if source != "default" {
		t.Fatalf("source = %q, want default", source)
	}
	if action == "" {
		t.Fatal("default action should not be empty")
	}
}

// TestIsCheckpointCurrent covers the freshness predicate.
func TestIsCheckpointCurrent(t *testing.T) {
	git := workflowGitSummary{Branch: "main", SHA: "abc1234"}

	if isCheckpointCurrent(git, nil) {
		t.Fatal("nil checkpoint is not current")
	}

	cp := &workflowCheckpoint{}
	cp.Git.Branch = "main"
	cp.Git.SHA = "abc1234"
	if !isCheckpointCurrent(git, cp) {
		t.Fatal("matching branch+sha checkpoint should be current")
	}

	cp.Git.SHA = "deadbeef"
	if isCheckpointCurrent(git, cp) {
		t.Fatal("mismatched SHA should not be current")
	}

	empty := &workflowCheckpoint{}
	if isCheckpointCurrent(git, empty) {
		t.Fatal("empty checkpoint git block is not current")
	}
}

// TestCollectWorkflowLessons_ReadsIndex covers loading and trimming lessons.
func TestCollectWorkflowLessons_ReadsIndex(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	lessonsDir := filepath.Join(repo, ".agents", "lessons")
	if err := os.MkdirAll(lessonsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write 15 lines so we exercise the trim-to-last-10 path.
	var b strings.Builder
	for i := 0; i < 15; i++ {
		b.WriteString("lesson ")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(lessonsDir, "index.md"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}

	lessons, warnings := collectWorkflowLessons(repo)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(lessons) != 10 {
		t.Fatalf("len(lessons) = %d, want 10 (trimmed)", len(lessons))
	}
	if lessons[len(lessons)-1] != "lesson 14" {
		t.Fatalf("last lesson = %q, want 'lesson 14'", lessons[len(lessons)-1])
	}
}

// TestCollectWorkflowLessons_MissingReturnsWarning ensures a missing index
// surfaces a warning rather than an error.
func TestCollectWorkflowLessons_MissingReturnsWarning(t *testing.T) {
	tmp := t.TempDir()
	lessons, warnings := collectWorkflowLessons(tmp)
	if len(lessons) != 0 {
		t.Fatalf("expected no lessons, got %v", lessons)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning when lessons index is missing")
	}
}

// TestLoadWorkflowCheckpoint_MissingReturnsNil ensures the loader produces
// no warnings when there is simply no checkpoint yet.
func TestLoadWorkflowCheckpoint_MissingReturnsNil(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	cp, warnings := loadWorkflowCheckpoint("no-such-project")
	if cp != nil {
		t.Fatalf("expected nil checkpoint, got %+v", cp)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for missing checkpoint, got %v", warnings)
	}
}

// TestLoadWorkflowCheckpoint_Unreadable ensures malformed YAML produces a warning.
func TestLoadWorkflowCheckpoint_Unreadable(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	contextDir := filepath.Join(agentsHome, "context", "broken-proj")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextDir, "checkpoint.yaml"), []byte("foo: bar\n\t- not: yaml\n\t\tbroken"), 0644); err != nil {
		t.Fatal(err)
	}
	cp, warnings := loadWorkflowCheckpoint("broken-proj")
	if cp != nil {
		t.Fatalf("expected nil checkpoint on parse failure, got %+v", cp)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "checkpoint unreadable") {
		t.Fatalf("expected 'checkpoint unreadable' warning, got %v", warnings)
	}
}
