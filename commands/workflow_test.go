package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// dotAgentsRepoRoot returns the module root (directory containing go.mod) by walking
// up from this test file. It does not depend on process working directory, which can
// be stale or under a deleted t.TempDir() after other tests use os.Chdir.
func dotAgentsRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from %s", file)
		}
		dir = parent
	}
}

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

func addCanonicalPendingPlanFixture(t *testing.T, repo string) {
	t.Helper()
	write := func(rel, content string) {
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write(".agents/workflow/plans/wave-next/PLAN.yaml", `schema_version: 1
id: "wave-next"
title: "Pending-first fixture"
status: "active"
summary: "Canonical plan fixture for workflow next tests"
created_at: "2026-04-10T10:00:00Z"
updated_at: "2026-04-10T10:00:00Z"
owner: "test"
success_criteria: "all tasks complete"
verification_strategy: "go test"
current_focus_task: "finish planner"
`)
	write(".agents/workflow/plans/wave-next/TASKS.yaml", `schema_version: 1
plan_id: "wave-next"
tasks:
  - id: "prep"
    title: "prep docs"
    status: "completed"
    depends_on: []
    blocks: ["planner"]
    owner: "test"
    write_scope: ["docs/"]
    verification_required: false
    notes: ""
  - id: "planner"
    title: "finish planner"
    status: "pending"
    depends_on: ["prep"]
    blocks: ["tests"]
    owner: "test"
    write_scope: ["commands/"]
    verification_required: true
    notes: ""
  - id: "tests"
    title: "add tests"
    status: "pending"
    depends_on: ["planner"]
    blocks: []
    owner: "test"
    write_scope: ["commands/workflow_test.go"]
    verification_required: true
    notes: ""
`)
}

func addCanonicalSliceFixture(t *testing.T, repo, planID string) {
	t.Helper()
	write := func(rel, content string) {
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write(".agents/workflow/plans/"+planID+"/SLICES.yaml", `schema_version: 1
plan_id: "`+planID+`"
slices:
  - id: "slice-read-surface"
    parent_task_id: "t1"
    title: "Read surface"
    summary: "Add a read-only CLI surface for slices."
    status: "completed"
    depends_on: []
    write_scope: ["commands/workflow.go", "commands/workflow_test.go"]
    verification_focus: "workflow slices and plan graph"
    owner: "dot-agents"
  - id: "slice-artifacts"
    parent_task_id: "t2"
    title: "Artifact docs"
    summary: "Add canonical slice artifacts and docs."
    status: "pending"
    depends_on: ["slice-read-surface"]
    write_scope: [".agents/workflow/plans/", "docs/"]
    verification_focus: "fixture and CLI readback"
    owner: "dot-agents"
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

// ── Usage-flow scenario tests ─────────────────────────────────────────────────

// writeCheckpointFixture writes a checkpoint.yaml into the AGENTS_HOME context dir for the given project.
func writeCheckpointFixture(t *testing.T, agentsHome, projectName, repo string, nextAction, verStatus, timestamp string) {
	t.Helper()
	contextDir := filepath.Join(agentsHome, "context", projectName)
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimSpace(gitOutput(repo, "rev-parse", "--abbrev-ref", "HEAD"))
	sha := strings.TrimSpace(gitOutput(repo, "rev-parse", "--short", "HEAD"))
	checkpoint := `schema_version: 1
timestamp: "` + timestamp + `"
project:
  name: "` + projectName + `"
  path: "` + repo + `"
git:
  branch: "` + branch + `"
  sha: "` + sha + `"
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

func writeCheckpointFixtureWithGitOverride(t *testing.T, agentsHome, projectName, repo string, nextAction, verStatus, timestamp, branch, sha string) {
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
  branch: "` + branch + `"
  sha: "` + sha + `"
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

// ── Wave 5: Graph bridge types ────────────────────────────────────────────────

func TestLoadGraphBridgeConfig_Absent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := loadGraphBridgeConfig(dir)
	if err != nil {
		t.Fatalf("loadGraphBridgeConfig absent: %v", err)
	}
	if cfg.Enabled {
		t.Error("expected bridge disabled when config absent")
	}
}

func TestLoadGraphBridgeConfig_Present(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `schema_version: 1
enabled: true
graph_home: /tmp/my-graph
allowed_intents:
  - plan_context
  - decision_lookup
`
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := loadGraphBridgeConfig(dir)
	if err != nil {
		t.Fatalf("loadGraphBridgeConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Error("expected bridge enabled")
	}
	if cfg.GraphHome != "/tmp/my-graph" {
		t.Errorf("graph_home: got %s", cfg.GraphHome)
	}
	if len(cfg.AllowedIntents) != 2 {
		t.Errorf("allowed_intents: expected 2, got %d", len(cfg.AllowedIntents))
	}
}

func TestIsValidWorkflowBridgeIntent(t *testing.T) {
	valid := []string{"plan_context", "decision_lookup", "entity_context", "workflow_memory", "contradictions"}
	for _, intent := range valid {
		if !isValidWorkflowBridgeIntent(intent) {
			t.Errorf("expected %s to be valid", intent)
		}
	}
	if isValidWorkflowBridgeIntent("unknown") {
		t.Error("'unknown' should not be valid")
	}
}

func TestRunWorkflowGraphQueryAllowsWorkflowBridgeIntent(t *testing.T) {
	project := t.TempDir()
	kgHome := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("KG_HOME", kgHome)
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := runKGSetup(); err != nil {
		t.Fatalf("kg setup: %v", err)
	}

	cfgDir := filepath.Join(project, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := []byte("schema_version: 1\nenabled: true\ngraph_home: \"" + kgHome + "\"\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), cfg, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "decision_lookup", "")
	cmd.Flags().String("scope", "", "")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowGraphQuery(cmd, nil); err != nil {
		t.Fatalf("runWorkflowGraphQuery: %v", err)
	}
}

func TestWorkflowGraphQueryCodeStructureRoutesToKGBridge(t *testing.T) {
	oldExe := workflowDotAgentsExe
	t.Cleanup(func() { workflowDotAgentsExe = oldExe })

	repoRoot := dotAgentsRepoRoot(t)
	bin := filepath.Join(t.TempDir(), "dot-agents")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/dot-agents")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build dot-agents: %v\n%s", err, out)
	}
	workflowDotAgentsExe = func() (string, error) { return bin, nil }

	project := t.TempDir()
	t.Setenv("KG_HOME", t.TempDir())

	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "symbol_lookup", "")
	cmd.Flags().String("scope", "", "")

	oldwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	err := runWorkflowGraphQuery(cmd, []string{"SomeQuery"})
	if err == nil {
		t.Fatal("expected error from kg bridge when graph is not initialized")
	}
	if strings.Contains(err.Error(), "workflow graph query does not handle") {
		t.Fatalf("expected route to kg bridge, got old guard: %v", err)
	}
	if strings.Contains(err.Error(), "Use `dot-agents kg bridge query") {
		t.Fatalf("expected route to kg bridge, got manual-use hint: %v", err)
	}
}

func TestWorkflowGraphQueryKGBridgeIntentsNotRouted(t *testing.T) {
	kgIntents := []string{"plan_context", "decision_lookup", "entity_context", "workflow_memory", "contradictions"}
	for _, intent := range kgIntents {
		if isWorkflowGraphCodeBridgeIntent(intent) {
			t.Errorf("intent %q must not be classified as workflow code-bridge intent (should use local graph bridge path)", intent)
		}
	}
}

// ── Wave 5: GraphBridgeHealth write/read ─────────────────────────────────────

func TestWriteReadGraphBridgeHealth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := GraphBridgeHealth{
		SchemaVersion:    1,
		Timestamp:        "2026-01-01T00:00:00Z",
		AdapterAvailable: true,
		NoteCount:        5,
		Status:           "healthy",
	}
	if err := writeGraphBridgeHealth("test-project", h); err != nil {
		t.Fatalf("writeGraphBridgeHealth: %v", err)
	}
	got, err := readGraphBridgeHealth("test-project")
	if err != nil {
		t.Fatalf("readGraphBridgeHealth: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil health")
	}
	if got.NoteCount != 5 {
		t.Errorf("NoteCount: got %d, want 5", got.NoteCount)
	}
}

// ── Wave 5: LocalGraphAdapter ─────────────────────────────────────────────────

func newTempKGForWorkflow(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KG_HOME", dir)
	return dir
}

func TestLocalGraphAdapter_Health_NotInitialized(t *testing.T) {
	dir := t.TempDir()
	adapter := NewLocalGraphAdapter(dir)
	h, err := adapter.Health()
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.AdapterAvailable {
		t.Error("expected unavailable before setup")
	}
	if h.Status == "healthy" {
		t.Error("expected non-healthy status")
	}
}

func TestLocalGraphAdapter_Query_ReturnsResults(t *testing.T) {
	home := newTempKGForWorkflow(t)
	// Set up KG with notes using the kg package functions
	if err := runKGSetup(); err != nil {
		t.Fatalf("kg setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	note := &GraphNote{
		SchemaVersion: 1, ID: "dec-workflow-test", Type: "decision",
		Title: "Use cobra for CLI", Summary: "We chose cobra.", Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := createGraphNote(home, note, "body content about cobra CLI framework"); err != nil {
		t.Fatalf("createGraphNote: %v", err)
	}

	adapter := NewLocalGraphAdapter(home)
	resp, err := adapter.Query(GraphBridgeQuery{
		Intent: "decision_lookup",
		Query:  "cobra",
	})
	if err != nil {
		t.Fatalf("adapter.Query: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Error("expected at least one result for 'cobra'")
	}
	if resp.Results[0].Type != "decision" {
		t.Errorf("expected type=decision, got %s", resp.Results[0].Type)
	}
}

func TestLocalGraphAdapter_Query_UnknownIntent(t *testing.T) {
	dir := t.TempDir()
	adapter := NewLocalGraphAdapter(dir)
	_, err := adapter.Query(GraphBridgeQuery{Intent: "bad_intent", Query: "x"})
	if err == nil {
		t.Error("expected error for unknown intent")
	}
}

// ── Wave 6: Delegation & Merge-back ─────────────────────────────���────────────

func setupTestProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Create minimal plan + tasks
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans", "plan-001")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	plan := CanonicalPlan{SchemaVersion: 1, ID: "plan-001", Title: "Test Plan", Status: "active",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z"}
	planData, _ := yaml.Marshal(plan)
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planData, 0644); err != nil {
		t.Fatalf("write PLAN.yaml: %v", err)
	}
	tasks := CanonicalTaskFile{SchemaVersion: 1, PlanID: "plan-001", Tasks: []CanonicalTask{
		{ID: "task-001", Title: "Do the thing", Status: "pending", WriteScope: []string{"commands/"}},
		{ID: "task-002", Title: "Other task", Status: "pending", WriteScope: []string{"internal/"}},
	}}
	tasksData, _ := yaml.Marshal(tasks)
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), tasksData, 0644); err != nil {
		t.Fatalf("write TASKS.yaml: %v", err)
	}
	return dir
}

func setupFanoutSliceProject(t *testing.T, sliceStatus string) string {
	t.Helper()
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}

	plan := CanonicalPlan{
		SchemaVersion: 1,
		ID:            "p1",
		Title:         "Fanout Test Plan",
		Status:        "active",
		CreatedAt:     "2026-04-10T00:00:00Z",
		UpdatedAt:     "2026-04-10T00:00:00Z",
	}
	planData, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planData, 0644); err != nil {
		t.Fatalf("write PLAN.yaml: %v", err)
	}

	tasks := CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        "p1",
		Tasks: []CanonicalTask{
			{ID: "t1", Title: "Fanout Task", Status: "pending", WriteScope: []string{"commands/"}},
		},
	}
	tasksData, err := yaml.Marshal(tasks)
	if err != nil {
		t.Fatalf("marshal tasks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), tasksData, 0644); err != nil {
		t.Fatalf("write TASKS.yaml: %v", err)
	}

	slices := CanonicalSliceFile{
		SchemaVersion: 1,
		PlanID:        "p1",
		Slices: []CanonicalSlice{
			{
				ID:           "s1",
				ParentTaskID: "t1",
				Title:        "Fanout Slice",
				Summary:      "Resolve fanout from slice metadata.",
				Status:       sliceStatus,
				WriteScope:   []string{"commands/"},
			},
		},
	}
	slicesData, err := yaml.Marshal(slices)
	if err != nil {
		t.Fatalf("marshal slices: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "SLICES.yaml"), slicesData, 0644); err != nil {
		t.Fatalf("write SLICES.yaml: %v", err)
	}

	return dir
}

func executeWorkflowCommand(t *testing.T, repo string, args ...string) error {
	t.Helper()
	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	cmd := NewWorkflowCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestLoadSaveDelegationContract_RoundTrip(t *testing.T) {
	dir := setupTestProject(t)
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-task-001", ParentPlanID: "plan-001", ParentTaskID: "task-001",
		Title: "Do the thing", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := loadDelegationContract(dir, "task-001")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ID != c.ID || loaded.Status != "active" {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestListDelegationContracts(t *testing.T) {
	dir := setupTestProject(t)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"task-001", "task-002"} {
		c := &DelegationContract{
			SchemaVersion: 1, ID: "del-" + id, ParentPlanID: "plan-001", ParentTaskID: id,
			Title: id, WriteScope: []string{id + "/"}, Status: "active",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := saveDelegationContract(dir, c); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	contracts, err := listDelegationContracts(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(contracts) != 2 {
		t.Errorf("expected 2 contracts, got %d", len(contracts))
	}
}

func TestWriteScopeOverlaps_NoConflict(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DelegationContract{
		{ParentTaskID: "task-001", WriteScope: []string{"commands/"}, Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	conflicts := writeScopeOverlaps(existing, []string{"internal/"}, "task-002")
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got: %v", conflicts)
	}
}

func TestWriteScopeOverlaps_DetectsConflict(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DelegationContract{
		{ParentTaskID: "task-001", WriteScope: []string{"commands/"}, Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	// commands/workflow.go is contained within commands/ — should conflict
	conflicts := writeScopeOverlaps(existing, []string{"commands/workflow.go"}, "task-002")
	if len(conflicts) == 0 {
		t.Error("expected conflict for commands/workflow.go vs commands/")
	}
}

func TestWriteScopeOverlaps_IdenticalScope(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DelegationContract{
		{ParentTaskID: "task-001", WriteScope: []string{"commands/"}, Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	conflicts := writeScopeOverlaps(existing, []string{"commands/"}, "task-002")
	if len(conflicts) == 0 {
		t.Error("expected conflict for identical scope")
	}
}

func TestWriteScopeOverlaps_SkipsCompletedDelegation(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DelegationContract{
		{ParentTaskID: "task-001", WriteScope: []string{"commands/"}, Status: "completed", CreatedAt: now, UpdatedAt: now},
	}
	// Completed delegation should not block new delegation with same scope
	conflicts := writeScopeOverlaps(existing, []string{"commands/"}, "task-002")
	if len(conflicts) != 0 {
		t.Errorf("completed delegation should not block, got: %v", conflicts)
	}
}

func TestFanoutFromSlice(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "test"); err != nil {
		t.Fatal(err)
	}

	contract, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if contract.ParentTaskID != "t1" {
		t.Fatalf("parent_task_id = %q, want t1", contract.ParentTaskID)
	}
	if contract.Owner != "test" {
		t.Fatalf("owner = %q, want test", contract.Owner)
	}
	if !strings.HasPrefix(contract.ID, "del-t1-") {
		t.Fatalf("contract id = %q, want prefix del-t1-", contract.ID)
	}
	if len(contract.WriteScope) != 1 || contract.WriteScope[0] != "commands/" {
		t.Fatalf("write_scope = %+v, want [commands/]", contract.WriteScope)
	}

	bundlePath := filepath.Join(repo, ".agents", "active", "delegation-bundles", contract.ID+".yaml")
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("delegation bundle: %v", err)
	}
	var bundle delegationBundleYAML
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.DelegationID != contract.ID || bundle.PlanID != "p1" || bundle.TaskID != "t1" {
		t.Fatalf("bundle ids: %+v", bundle)
	}
	if bundle.SliceID != "s1" {
		t.Fatalf("slice_id = %q, want s1", bundle.SliceID)
	}
	if bundle.Worker.Profile != defaultDelegateProfile {
		t.Fatalf("profile = %q", bundle.Worker.Profile)
	}
	if bundle.Verification.FeedbackGoal == "" {
		t.Fatal("expected default feedback_goal")
	}
	if len(bundle.Closeout.WorkerMust) == 0 || len(bundle.Closeout.ParentMust) == 0 {
		t.Fatal("expected closeout defaults")
	}
}

func setupFanoutTwoTaskProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1,
		ID:            "p1",
		Title:         "Two-task fanout",
		Status:        "active",
		CreatedAt:     "2026-04-10T00:00:00Z",
		UpdatedAt:     "2026-04-10T00:00:00Z",
	}
	planData, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planData, 0644); err != nil {
		t.Fatalf("write PLAN.yaml: %v", err)
	}
	tasks := CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        "p1",
		Tasks: []CanonicalTask{
			{ID: "t1", Title: "First", Status: "pending", WriteScope: []string{"commands/"}},
			{ID: "t2", Title: "Second", Status: "pending", WriteScope: []string{"internal/"}},
		},
	}
	tasksData, err := yaml.Marshal(tasks)
	if err != nil {
		t.Fatalf("marshal tasks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), tasksData, 0644); err != nil {
		t.Fatalf("write TASKS.yaml: %v", err)
	}
	slices := CanonicalSliceFile{
		SchemaVersion: 1,
		PlanID:        "p1",
		Slices: []CanonicalSlice{
			{
				ID:           "s1",
				ParentTaskID: "t1",
				Title:        "Slice",
				Status:       "in_progress",
				WriteScope:   []string{"commands/"},
			},
		},
	}
	slicesData, err := yaml.Marshal(slices)
	if err != nil {
		t.Fatalf("marshal slices: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "SLICES.yaml"), slicesData, 0644); err != nil {
		t.Fatalf("write SLICES.yaml: %v", err)
	}
	return dir
}

func TestFanoutTwoTasksDistinctBundles(t *testing.T) {
	repo := setupFanoutTwoTaskProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "a"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--task", "t2", "--owner", "b", "--write-scope", "internal/"); err != nil {
		t.Fatal(err)
	}
	c1, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := loadDelegationContract(repo, "t2")
	if err != nil {
		t.Fatal(err)
	}
	if c1.ID == c2.ID {
		t.Fatalf("expected distinct delegation ids: %s", c1.ID)
	}
	b1, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", c1.ID+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", c2.ID+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var x1, x2 delegationBundleYAML
	if err := yaml.Unmarshal(b1, &x1); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(b2, &x2); err != nil {
		t.Fatal(err)
	}
	if x1.TaskID != "t1" || x2.TaskID != "t2" {
		t.Fatalf("task mismatch: %s / %s", x1.TaskID, x2.TaskID)
	}
	if x1.Owner != "a" || x2.Owner != "b" {
		t.Fatalf("owner leak: %s / %s", x1.Owner, x2.Owner)
	}
	if x2.SliceID != "" {
		t.Fatalf("t2 bundle should not set slice_id, got %q", x2.SliceID)
	}
}

func TestFanoutDelegationBundlePromptAndFiles(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	promptPath := filepath.Join(repo, ".agents", "ctx", "prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("# hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w",
		"--delegate-profile", "custom-worker",
		"--prompt", "line one", "--prompt", "line two",
		"--prompt-file", ".agents/ctx/prompt.md",
		"--context-file", ".agents/ctx/prompt.md",
		"--feedback-goal", "Prove fanout bundles persist.",
		"--scenario-tag", "a", "--scenario-tag", "b",
		"--regression-artifact", ".agents/workflow/plans/p1/TASKS.yaml",
		"--selection-reason", "integration test",
		"--require-negative-coverage", "--sandbox-mutations",
	)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", contract.ID+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle delegationBundleYAML
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Worker.Profile != "custom-worker" {
		t.Fatalf("profile %q", bundle.Worker.Profile)
	}
	if len(bundle.Prompt.Inline) != 2 || bundle.Prompt.Inline[0] != "line one" {
		t.Fatalf("inline prompt: %+v", bundle.Prompt.Inline)
	}
	if len(bundle.Prompt.PromptFiles) != 1 || bundle.Prompt.PromptFiles[0] != ".agents/ctx/prompt.md" {
		t.Fatalf("prompt_files: %+v", bundle.Prompt.PromptFiles)
	}
	if len(bundle.Context.RequiredFiles) != 1 {
		t.Fatalf("context: %+v", bundle.Context.RequiredFiles)
	}
	if bundle.Verification.FeedbackGoal != "Prove fanout bundles persist." {
		t.Fatalf("feedback_goal %q", bundle.Verification.FeedbackGoal)
	}
	if len(bundle.Verification.ScenarioTags) != 2 {
		t.Fatalf("scenario_tags: %+v", bundle.Verification.ScenarioTags)
	}
	if len(bundle.Verification.RegressionArtifacts) != 1 || !strings.HasSuffix(bundle.Verification.RegressionArtifacts[0], "TASKS.yaml") {
		t.Fatalf("regression: %+v", bundle.Verification.RegressionArtifacts)
	}
	if bundle.Selection == nil || bundle.Selection.Reason != "integration test" {
		t.Fatalf("selection: %+v", bundle.Selection)
	}
	if bundle.Verification.EvidencePolicy == nil || bundle.Verification.EvidencePolicy.RequireNegativeCoverage == nil || !*bundle.Verification.EvidencePolicy.RequireNegativeCoverage {
		t.Fatal("expected require_negative_coverage")
	}
}

func TestFanoutDelegationBundleRejectsEscapePath(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w",
		"--prompt-file", "../../../etc/passwd",
	)
	if err == nil {
		t.Fatal("expected error for path escape")
	}
	if !strings.Contains(err.Error(), "prompt-file") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestFanoutSliceAndTaskMutuallyExclusive(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--task", "t1", "--owner", "test")
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags, got nil")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFanout_CreatesVerificationDir(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(repo, ".agents", "active", "verification", "task-001"))
	if err != nil || !st.IsDir() {
		t.Fatalf("verification dir: %v", err)
	}
}

func TestFanout_TDDGateRejectsGoScopeWithoutTests(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "commands", "x.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	tf.Tasks[0].WriteScope = []string{"commands/x.go"}
	tf.Tasks[0].VerificationRequired = true
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}

	err = executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected TDD gate error")
	}
	if !strings.Contains(err.Error(), "pre-verifier TDD gate") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestFanout_VerifierRetryMaxInBundle(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w", "--verifier-retry-max", "4"); err != nil {
		t.Fatal(err)
	}
	c, err := loadDelegationContract(repo, "task-001")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", c.ID+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle delegationBundleYAML
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Verification.EvidencePolicy == nil || bundle.Verification.EvidencePolicy.PrimaryChainMax == nil || *bundle.Verification.EvidencePolicy.PrimaryChainMax != 4 {
		t.Fatalf("expected primary_chain_max 4, got %+v", bundle.Verification.EvidencePolicy)
	}
}

func TestFanoutTaskWriteScopeFallback(t *testing.T) {
	// fanout --plan X --task Y without --write-scope should pull write_scope from task definition
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	contract, err := loadDelegationContract(repo, "task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(contract.WriteScope) != 1 || contract.WriteScope[0] != "commands/" {
		t.Fatalf("write_scope = %+v, want [commands/]", contract.WriteScope)
	}
	bundleData, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", contract.ID+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle delegationBundleYAML
	if err := yaml.Unmarshal(bundleData, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Scope.WriteScope) != 1 || bundle.Scope.WriteScope[0] != "commands/" {
		t.Fatalf("bundle write_scope = %+v, want [commands/]", bundle.Scope.WriteScope)
	}
}

func TestFanoutSliceNotFound(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "missing", "--owner", "test")
	if err == nil {
		t.Fatal("expected error for missing slice, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFanoutSliceAlreadyCompleted(t *testing.T) {
	repo := setupFanoutSliceProject(t, "completed")
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "test")
	if err == nil {
		t.Fatal("expected error for completed slice, got nil")
	}
	if !strings.Contains(err.Error(), "already completed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveLoadMergeBack_RoundTrip(t *testing.T) {
	dir := setupTestProject(t)
	s := &MergeBackSummary{
		SchemaVersion: 1, TaskID: "task-001", ParentPlanID: "plan-001",
		Title: "Do the thing", Summary: "Implemented the feature.",
		FilesChanged:       []string{"commands/workflow.go"},
		VerificationResult: MergeBackVerification{Status: "pass", Summary: "tests green"},
		IntegrationNotes:   "No conflicts expected.",
		CreatedAt:          time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveMergeBack(dir, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := loadMergeBack(dir, "task-001")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.TaskID != "task-001" || loaded.VerificationResult.Status != "pass" {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestValidateVerificationResultDoc_Table(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	base := VerificationResultDoc{
		SchemaVersion: 1,
		TaskID:        "t1",
		ParentPlanID:  "p1",
		VerifierType:  "unit",
		Status:        "pass",
		Summary:       "go test ./...",
		RecordedAt:    now,
	}
	t.Run("valid minimal", func(t *testing.T) {
		d := base
		if err := validateVerificationResultDoc(&d); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("invalid status", func(t *testing.T) {
		d := base
		d.Status = "green"
		if err := validateVerificationResultDoc(&d); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid verifier_type pattern", func(t *testing.T) {
		d := base
		d.VerifierType = "Unit"
		if err := validateVerificationResultDoc(&d); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("missing required field", func(t *testing.T) {
		d := base
		d.ParentPlanID = ""
		if err := validateVerificationResultDoc(&d); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestMergeBack_WritesVerificationResultYAML(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "done", "--verification-status", "pass", "--integration-notes", "go test ./commands"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, ".agents", "active", "verification", "t1", VerifierTypeMergeBack+".result.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read verification result: %v", err)
	}
	var got VerificationResultDoc
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if got.TaskID != "t1" || got.ParentPlanID != "p1" || got.VerifierType != VerifierTypeMergeBack {
		t.Fatalf("unexpected doc: %+v", got)
	}
	if got.Status != "pass" || !strings.Contains(got.Summary, "go test") {
		t.Fatalf("status/summary: %+v", got)
	}
	if err := validateVerificationResultDoc(&got); err != nil {
		t.Fatalf("re-validate: %v", err)
	}
}

func TestMergeBack_InvalidVerificationStatus(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "done", "--verification-status", "bogus")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid verification status") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── Wave 6 Step 7: orient/status delegation summary ───────────────────────────

func TestCollectDelegationSummary_Empty(t *testing.T) {
	dir := setupTestProject(t)
	summary, mergebacks := collectDelegationSummary(dir)
	if summary.ActiveCount != 0 || summary.PendingIntents != 0 {
		t.Errorf("expected empty summary on fresh project: %+v", summary)
	}
	if mergebacks != 0 {
		t.Errorf("expected 0 merge-backs, got %d", mergebacks)
	}
}

func TestCollectDelegationSummary_WithActiveContracts(t *testing.T) {
	dir := setupTestProject(t)
	now := time.Now().UTC().Format(time.RFC3339)
	// One active with pending intent, one active without
	for _, tc := range []struct {
		taskID string
		intent CoordinationIntent
	}{
		{"task-001", CoordinationIntentStatusRequest},
		{"task-002", CoordinationIntentNone},
	} {
		c := &DelegationContract{
			SchemaVersion: 1, ID: "del-" + tc.taskID, ParentPlanID: "plan-001",
			ParentTaskID: tc.taskID, Title: tc.taskID, WriteScope: []string{tc.taskID + "/"},
			Status: "active", PendingIntent: tc.intent, CreatedAt: now, UpdatedAt: now,
		}
		if err := saveDelegationContract(dir, c); err != nil {
			t.Fatalf("save %s: %v", tc.taskID, err)
		}
	}
	summary, _ := collectDelegationSummary(dir)
	if summary.ActiveCount != 2 {
		t.Errorf("expected 2 active, got %d", summary.ActiveCount)
	}
	if summary.PendingIntents != 1 {
		t.Errorf("expected 1 pending intent, got %d", summary.PendingIntents)
	}
}

func TestCollectDelegationSummary_CompletedNotCounted(t *testing.T) {
	dir := setupTestProject(t)
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-task-001", ParentPlanID: "plan-001",
		ParentTaskID: "task-001", Title: "done", WriteScope: []string{"commands/"},
		Status: "completed", CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	summary, _ := collectDelegationSummary(dir)
	if summary.ActiveCount != 0 {
		t.Errorf("completed delegation should not count as active, got %d", summary.ActiveCount)
	}
}

// ── Wave 7: Drift & Sweep ─────────────────────────────────────────────────────

func TestDetectRepoDrift_Unreachable(t *testing.T) {
	project := ManagedProject{Name: "gone", Path: "/nonexistent/path/does/not/exist"}
	report := detectRepoDrift(project, 7, 30)
	if report.Reachable {
		t.Error("expected unreachable")
	}
	if report.Status != "unreachable" {
		t.Errorf("expected status=unreachable, got %s", report.Status)
	}
}

func TestDetectRepoDrift_FreshProject(t *testing.T) {
	dir := t.TempDir()
	// A brand-new project: no checkpoint, no workflow dir
	project := ManagedProject{Name: "fresh", Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if !report.Reachable {
		t.Error("expected reachable")
	}
	if !report.MissingCheckpoint {
		t.Error("expected missing_checkpoint")
	}
	if !report.MissingWorkflowDir {
		t.Error("expected missing_workflow_dir")
	}
	if report.Status != "warn" {
		t.Errorf("expected warn, got %s", report.Status)
	}
}

func TestDetectRepoDrift_HealthyProject(t *testing.T) {
	dir := t.TempDir()
	// Create a workflow dir, plans dir, and a fresh checkpoint
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "workflow", "plans"), 0755); err != nil {
		t.Fatal(err)
	}
	// Write a recent checkpoint (today)
	projectName := "healthy-proj"
	checkpointDir := filepath.Join(config.AgentsContextDir(), projectName)
	if err := os.MkdirAll(checkpointDir, 0755); err != nil {
		t.Fatal(err)
	}
	checkpointData := []byte("schema_version: 1\ntimestamp: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	checkpointPath := filepath.Join(checkpointDir, "checkpoint.yaml")
	if err := os.WriteFile(checkpointPath, checkpointData, 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(checkpointDir) })

	project := ManagedProject{Name: projectName, Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if report.MissingCheckpoint {
		t.Error("should not flag missing checkpoint")
	}
	if report.StaleCheckpoint {
		t.Error("should not flag stale checkpoint for fresh checkpoint")
	}
	if report.Status != "healthy" {
		t.Errorf("expected healthy, got %s — warnings: %v", report.Status, report.Warnings)
	}
}

func TestDetectRepoDrift_StaleCheckpoint(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "workflow", "plans"), 0755); err != nil {
		t.Fatal(err)
	}
	projectName := "stale-cp-proj"
	checkpointDir := filepath.Join(config.AgentsContextDir(), projectName)
	if err := os.MkdirAll(checkpointDir, 0755); err != nil {
		t.Fatal(err)
	}
	// 30-day-old checkpoint
	oldTime := time.Now().AddDate(0, 0, -30).UTC().Format(time.RFC3339)
	checkpointData := []byte("schema_version: 1\ntimestamp: " + oldTime + "\n")
	if err := os.WriteFile(filepath.Join(checkpointDir, "checkpoint.yaml"), checkpointData, 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(checkpointDir) })

	project := ManagedProject{Name: projectName, Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if !report.StaleCheckpoint {
		t.Error("expected stale_checkpoint")
	}
	if report.CheckpointAgeDays < 28 {
		t.Errorf("expected checkpoint age >= 28 days, got %d", report.CheckpointAgeDays)
	}
}

func TestAggregateDrift_Summary(t *testing.T) {
	reports := []RepoDriftReport{
		{Project: ManagedProject{Name: "a"}, Status: "healthy"},
		{Project: ManagedProject{Name: "b"}, Status: "warn", Warnings: []string{"stale checkpoint"}},
		{Project: ManagedProject{Name: "c"}, Status: "unreachable", Warnings: []string{"path missing"}},
	}
	agg := aggregateDrift(reports)
	if agg.HealthyCount != 1 {
		t.Errorf("healthy: want 1, got %d", agg.HealthyCount)
	}
	if agg.WarnCount != 1 {
		t.Errorf("warn: want 1, got %d", agg.WarnCount)
	}
	if agg.UnreachableCount != 1 {
		t.Errorf("unreachable: want 1, got %d", agg.UnreachableCount)
	}
	if len(agg.TopWarnings) != 2 {
		t.Errorf("top_warnings: want 2, got %d", len(agg.TopWarnings))
	}
}

func TestPlanSweep_GeneratesActions(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:            ManagedProject{Name: "needs-workflow", Path: "/tmp/x"},
			Reachable:          true,
			MissingWorkflowDir: true,
			MissingCheckpoint:  true,
			Status:             "warn",
		},
	}
	plan := planSweep(reports)
	if len(plan.Actions) == 0 {
		t.Fatal("expected sweep actions")
	}
	// Scaffold workflow dir should be present
	found := false
	for _, a := range plan.Actions {
		if a.Action == SweepActionScaffoldWorkflowDir {
			found = true
			if !a.RequiresConfirmation {
				t.Error("scaffold_workflow_dir should require confirmation")
			}
		}
	}
	if !found {
		t.Error("expected scaffold_workflow_dir action")
	}
}

func TestPlanSweep_UnreachableSkipped(t *testing.T) {
	reports := []RepoDriftReport{
		{Project: ManagedProject{Name: "gone"}, Reachable: false, Status: "unreachable"},
	}
	plan := planSweep(reports)
	if len(plan.Actions) != 0 {
		t.Errorf("expected no actions for unreachable project, got %d", len(plan.Actions))
	}
}

func TestPlanSweep_AllMutatingActionsRequireConfirmation(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:              ManagedProject{Name: "x"},
			Reachable:            true,
			MissingWorkflowDir:   true,
			MissingPlanStructure: true,
			Status:               "warn",
		},
	}
	plan := planSweep(reports)
	for _, a := range plan.Actions {
		if a.Action == SweepActionScaffoldWorkflowDir || a.Action == SweepActionCreatePlanStructure {
			if !a.RequiresConfirmation {
				t.Errorf("action %s should require confirmation", a.Action)
			}
		}
	}
}

// ── Phase 6: fold-back ───────────────────────────────────────────────────────

func setupFoldBackProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1,
		ID:            "p1",
		Title:         "P1",
		Status:        "active",
		Summary:       "start",
		CreatedAt:     "2026-04-10T00:00:00Z",
		UpdatedAt:     "2026-04-10T00:00:00Z",
	}
	planData, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planData, 0644); err != nil {
		t.Fatalf("write PLAN.yaml: %v", err)
	}
	tasks := CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        "p1",
		Tasks: []CanonicalTask{
			{ID: "t1", Title: "T1", Status: "pending", Notes: "existing"},
		},
	}
	tasksData, err := yaml.Marshal(tasks)
	if err != nil {
		t.Fatalf("marshal tasks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), tasksData, 0644); err != nil {
		t.Fatalf("write TASKS.yaml: %v", err)
	}
	return dir
}

func setupFoldBackTwoPlanProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, pid := range []string{"p1", "p2"} {
		plansDir := filepath.Join(dir, ".agents", "workflow", "plans", pid)
		if err := os.MkdirAll(plansDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		plan := CanonicalPlan{
			SchemaVersion: 1,
			ID:            pid,
			Title:         pid,
			Status:        "active",
			Summary:       "s",
			CreatedAt:     "2026-04-10T00:00:00Z",
			UpdatedAt:     "2026-04-10T00:00:00Z",
		}
		planData, err := yaml.Marshal(plan)
		if err != nil {
			t.Fatalf("marshal plan: %v", err)
		}
		if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planData, 0644); err != nil {
			t.Fatalf("write PLAN: %v", err)
		}
		tasks := CanonicalTaskFile{
			SchemaVersion: 1,
			PlanID:        pid,
			Tasks: []CanonicalTask{
				{ID: "t1", Title: "T", Status: "pending"},
			},
		}
		tasksData, err := yaml.Marshal(tasks)
		if err != nil {
			t.Fatalf("marshal tasks: %v", err)
		}
		if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), tasksData, 0644); err != nil {
			t.Fatalf("write TASKS: %v", err)
		}
	}
	return dir
}

func executeWorkflowCommandOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	cmd := NewWorkflowCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workflow %v: %v\n%s", args, err, buf.String())
	}
	return buf.String()
}

func TestFoldBackCreateSmall(t *testing.T) {
	repo := setupFoldBackProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--observation", "new obs"); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tf.Tasks[0].Notes, "existing") || !strings.Contains(tf.Tasks[0].Notes, "new obs") {
		t.Fatalf("task notes = %q", tf.Tasks[0].Notes)
	}
	matches, err := filepath.Glob(filepath.Join(repo, ".agents", "active", "fold-back", "fold-*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("glob fold-back artifacts: got %d files", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var a foldBackArtifact
	if err := yaml.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	if a.Classification != "small" || a.RoutedTo != "task_note:p1/t1" {
		t.Fatalf("artifact: %+v", a)
	}
}

func TestFoldBackCreateNoTask(t *testing.T) {
	repo := setupFoldBackProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--observation", "plan-level obs"); err != nil {
		t.Fatal(err)
	}
	plan, err := loadCanonicalPlan(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Summary, "start") || !strings.Contains(plan.Summary, "plan-level obs") {
		t.Fatalf("plan summary = %q", plan.Summary)
	}
	matches, err := filepath.Glob(filepath.Join(repo, ".agents", "active", "fold-back", "fold-*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one fold-back file, got %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var a foldBackArtifact
	if err := yaml.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	if a.Classification != "small" || a.RoutedTo != "plan_summary:p1" || a.TaskID != "" {
		t.Fatalf("artifact: %+v", a)
	}
}

func TestFoldBackCreatePropose(t *testing.T) {
	repo := setupFoldBackProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	tfBefore, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	beforeNotes := tfBefore.Tasks[0].Notes

	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--observation", "big change", "--propose"); err != nil {
		t.Fatal(err)
	}

	tfAfter, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if tfAfter.Tasks[0].Notes != beforeNotes {
		t.Fatalf("TASKS.yaml notes changed under --propose: %q -> %q", beforeNotes, tfAfter.Tasks[0].Notes)
	}

	propMatches, err := filepath.Glob(filepath.Join(agentsHome, "proposals", "obs-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(propMatches) != 1 {
		t.Fatalf("expected one proposal, got %d", len(propMatches))
	}

	matches, err := filepath.Glob(filepath.Join(repo, ".agents", "active", "fold-back", "fold-*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one fold-back artifact, got %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var a foldBackArtifact
	if err := yaml.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	if a.Classification != "proposal" || !strings.HasPrefix(a.RoutedTo, "proposal:obs-") {
		t.Fatalf("artifact: %+v", a)
	}
}

func TestDelegationCloseoutAccept(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "done", "--verification-status", "pass"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "delegation", "closeout", "--plan", "p1", "--task", "t1", "--decision", "accept"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "active", "delegation", "t1.yaml")); !os.IsNotExist(err) {
		t.Fatal("expected active delegation removed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "active", "merge-back", "t1.md")); !os.IsNotExist(err) {
		t.Fatal("expected active merge-back removed")
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if tf.Tasks[0].Status != "completed" {
		t.Fatalf("task status = %q, want completed", tf.Tasks[0].Status)
	}
	plan, err := loadCanonicalPlan(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "completed" {
		t.Fatalf("plan status = %q, want completed", plan.Status)
	}
	matches, _ := filepath.Glob(filepath.Join(repo, ".agents", "history", "p1", "delegate-merge-back-archive", "*", "t1", "closeout.yaml"))
	if len(matches) != 1 {
		t.Fatalf("expected one closeout record, got %v", matches)
	}
}

func TestDelegationCloseoutReject(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "try", "--verification-status", "fail"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "delegation", "closeout", "--plan", "p1", "--task", "t1", "--decision", "reject", "--note", "fix tests"); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if tf.Tasks[0].Status != "blocked" {
		t.Fatalf("task status = %q, want blocked", tf.Tasks[0].Status)
	}
	if !strings.Contains(tf.Tasks[0].Notes, "delegation closeout reject: fix tests") {
		t.Fatalf("expected reject note in task notes: %q", tf.Tasks[0].Notes)
	}
	plan, err := loadCanonicalPlan(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "active" {
		t.Fatalf("plan status = %q, want active", plan.Status)
	}
}

func TestFoldBackList(t *testing.T) {
	repo := setupFoldBackTwoPlanProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--observation", "a1"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p2", "--task", "t1", "--observation", "a2"); err != nil {
		t.Fatal(err)
	}

	prev := Flags.JSON
	Flags.JSON = true
	defer func() { Flags.JSON = prev }()

	outAll := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(outAll, `"plan_id": "p1"`) || !strings.Contains(outAll, `"plan_id": "p2"`) {
		t.Fatalf("list all should include both plans: %s", outAll)
	}

	outP1 := executeWorkflowCommandOutput(t, repo, "fold-back", "list", "--plan", "p1")
	if !strings.Contains(outP1, `"plan_id": "p1"`) || strings.Contains(outP1, `"plan_id": "p2"`) {
		t.Fatalf("filtered list: %s", outP1)
	}
}

// initWorkflowTestRepoWithCommit creates a repo with a second commit so HEAD~1 exists.
func initWorkflowTestRepoWithCommit(t *testing.T) string {
	t.Helper()
	repo := initWorkflowTestRepo(t)
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
	// Write a second file and commit so HEAD~1 exists
	second := filepath.Join(repo, "second.txt")
	if err := os.WriteFile(second, []byte("second\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "second.txt")
	run("commit", "-m", "second commit")
	return repo
}

func TestCheckpointLogToIter(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	const iterN = 38
	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "38"); err != nil {
		t.Fatalf("checkpoint --log-to-iter 38: %v", err)
	}

	iterPath := filepath.Join(repo, ".agents", "active", "iteration-log", "iter-38.yaml")
	raw, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatalf("iter-38.yaml not created: %v", err)
	}
	content := string(raw)

	// Header comment must be present
	if !strings.HasPrefix(content, "# yaml-language-server:") {
		t.Errorf("missing yaml-language-server header; got: %q", content[:min(len(content), 80)])
	}
	if !strings.Contains(content, "workflow-iter-log.schema.json") {
		t.Errorf("header does not reference schema: %s", content[:min(len(content), 120)])
	}

	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal iter-38.yaml: %v", err)
	}

	// CLI-deterministic fields
	if entry.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", entry.SchemaVersion)
	}
	if entry.Iteration != iterN {
		t.Errorf("iteration = %d, want %d", entry.Iteration, iterN)
	}
	today := time.Now().UTC().Format("2006-01-02")
	if entry.Date != today {
		t.Errorf("date = %q, want %q", entry.Date, today)
	}
	// commit sha should be non-empty (repo has commits)
	if entry.Commit == "" {
		t.Errorf("commit sha is empty; expected a git SHA")
	}
	// files_changed, lines_added, lines_removed are >= 0 (parsed from diff --stat)
	if entry.FilesChanged < 0 {
		t.Errorf("files_changed = %d, want >= 0", entry.FilesChanged)
	}

	// impl block: empty stubs (schema v2)
	impl := entry.Impl
	if impl.Item != "" {
		t.Errorf("impl.item = %q, want empty string", impl.Item)
	}
	if impl.Summary != "" {
		t.Errorf("impl.summary = %q, want empty", impl.Summary)
	}
	if impl.ScopeNote != "" {
		t.Errorf("impl.scope_note = %q, want empty", impl.ScopeNote)
	}
	if impl.FeedbackGoal != "" {
		t.Errorf("impl.feedback_goal = %q, want empty", impl.FeedbackGoal)
	}
	if impl.Retries != 0 {
		t.Errorf("impl.retries = %d, want 0", impl.Retries)
	}
	if impl.FocusedTestsAdded != 0 {
		t.Errorf("impl.focused_tests_added = %d, want 0", impl.FocusedTestsAdded)
	}
	if impl.FocusedTestsPass != nil {
		t.Errorf("impl.focused_tests_pass = %v, want nil", impl.FocusedTestsPass)
	}
	isa := impl.SelfAssessment
	if isa.ReadLoopState {
		t.Error("impl.self_assessment.read_loop_state should be false")
	}
	if isa.OneItemOnly {
		t.Error("impl.self_assessment.one_item_only should be false")
	}
	if isa.CommittedAfterTests {
		t.Error("impl.self_assessment.committed_after_tests should be false")
	}
	if isa.AlignedWithCanonicalTasks {
		t.Error("impl.self_assessment.aligned_with_canonical_tasks should be false")
	}
	if isa.PersistedViaWorkflowCommands != "" {
		t.Errorf("impl.self_assessment.persisted_via_workflow_commands = %q, want empty", isa.PersistedViaWorkflowCommands)
	}
	if isa.StayedUnder10Files {
		t.Error("impl.self_assessment.stayed_under_10_files should be false")
	}
	if isa.NoDestructiveCommands {
		t.Error("impl.self_assessment.no_destructive_commands should be false")
	}
	if isa.ScopedTestsToWriteScope {
		t.Error("impl.self_assessment.scoped_tests_to_write_scope should be false")
	}
	if isa.TddRefreshPerformed {
		t.Error("impl.self_assessment.tdd_refresh_performed should be false")
	}

	if len(entry.Verifiers) != 0 {
		t.Errorf("verifiers = %v, want empty", entry.Verifiers)
	}
	// review block: empty defaults
	if entry.Review.Phase1Decision != "" || entry.Review.Phase2Decision != "" || entry.Review.OverallDecision != "" {
		t.Errorf("review decisions should be empty stubs, got %#v", entry.Review)
	}

	if err := validateWorkflowIterLogEntry(&entry); err != nil {
		t.Fatalf("schema validation failed for valid stub: %v", err)
	}
}

func TestCheckpointLogToIterRequiresPositiveIteration(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	for _, n := range []string{"0", "-1"} {
		t.Run("n="+n, func(t *testing.T) {
			err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", n)
			if err == nil {
				t.Fatalf("expected error for --log-to-iter %s, got nil", n)
			}
		})
	}
}

func TestWorkflowIterLogEmbeddedSchemaMatchesCanonical(t *testing.T) {
	root := dotAgentsRepoRoot(t)
	want, err := os.ReadFile(filepath.Join(root, "schemas", "workflow-iter-log.schema.json"))
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}
	if string(want) != string(workflowIterLogSchemaJSON) {
		t.Fatal("commands/static/workflow-iter-log.schema.json is out of sync with schemas/workflow-iter-log.schema.json — copy the canonical file after editing")
	}
}

func TestCheckpointLogToIterFirstCommit(t *testing.T) {
	// Repo with only one commit: HEAD~1 does not exist → first_commit: true, counts 0
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "1"); err != nil {
		t.Fatalf("checkpoint --log-to-iter 1: %v", err)
	}

	iterPath := filepath.Join(repo, ".agents", "active", "iteration-log", "iter-1.yaml")
	raw, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatalf("iter-1.yaml not created: %v", err)
	}

	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !entry.FirstCommit {
		t.Errorf("first_commit = false, want true when HEAD~1 absent")
	}
	if entry.FilesChanged != 0 || entry.LinesAdded != 0 || entry.LinesRemoved != 0 {
		t.Errorf("expected zero diff counts for first commit, got files=%d added=%d removed=%d",
			entry.FilesChanged, entry.LinesAdded, entry.LinesRemoved)
	}
}

func TestCheckpointLogToIterNoDelegation(t *testing.T) {
	// No delegation contracts → wave and task_id are empty strings
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "5"); err != nil {
		t.Fatalf("checkpoint --log-to-iter 5: %v", err)
	}

	iterPath := filepath.Join(repo, ".agents", "active", "iteration-log", "iter-5.yaml")
	raw, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatalf("iter-5.yaml not created: %v", err)
	}
	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry.Wave != "" {
		t.Errorf("wave = %q, want empty when no delegation contract", entry.Wave)
	}
	if entry.TaskID != "" {
		t.Errorf("task_id = %q, want empty when no delegation contract", entry.TaskID)
	}
}

func TestCheckpointLogToIterVerifierRequiresVerifierType(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "2", "--role", "verifier")
	if err == nil {
		t.Fatal("expected error for --role verifier without --verifier-type")
	}
}

func TestCheckpointLogToIterVerifierTypeWithoutLogToIterRejected(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	err := executeWorkflowCommand(t, repo, "checkpoint", "--verifier-type", "unit", "--message", "x")
	if err == nil {
		t.Fatal("expected error when --verifier-type is set without --log-to-iter")
	}
}

func TestCheckpointLogToIterVerifierMergePreservesImpl(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	delegDir := filepath.Join(repo, ".agents", "active", "delegation")
	bundleDir := filepath.Join(repo, ".agents", "active", "delegation-bundles")
	if err := os.MkdirAll(delegDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	const taskID = "slice-task"
	const bundleID = "del-slice-task-999001"
	contract := fmt.Sprintf(`schema_version: 1
id: %s
parent_plan_id: plan-x
parent_task_id: %s
title: t
write_scope: []
status: active
created_at: "2026-04-18T00:00:00Z"
updated_at: "2026-04-18T00:00:00Z"
`, bundleID, taskID)
	if err := os.WriteFile(filepath.Join(delegDir, taskID+".yaml"), []byte(contract), 0644); err != nil {
		t.Fatal(err)
	}
	bundle := fmt.Sprintf(`schema_version: 1
delegation_id: %s
plan_id: plan-x
task_id: %s
owner: test
worker:
  profile: loop-worker
scope:
  write_scope: []
prompt: {}
context: {}
verification:
  feedback_goal: bundle-fg
closeout: {}
`, bundleID, taskID)
	if err := os.WriteFile(filepath.Join(bundleDir, bundleID+".yaml"), []byte(bundle), 0644); err != nil {
		t.Fatal(err)
	}

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "77"); err != nil {
		t.Fatalf("stub: %v", err)
	}
	iterPath := filepath.Join(repo, ".agents", "active", "iteration-log", "iter-77.yaml")
	raw, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatal(err)
	}
	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	entry.Impl.Item = "keep-me"
	body, err := yaml.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	const header = "# yaml-language-server: $schema=../../../../schemas/workflow-iter-log.schema.json\n"
	if err := os.WriteFile(iterPath, append([]byte(header), body...), 0644); err != nil {
		t.Fatal(err)
	}

	verDir := filepath.Join(repo, ".agents", "active", "verification", taskID)
	if err := os.MkdirAll(verDir, 0755); err != nil {
		t.Fatal(err)
	}
	result := fmt.Sprintf(`schema_version: 1
task_id: %s
parent_plan_id: plan-x
verifier_type: unit
status: pass
summary: ok
recorded_at: "2026-04-18T12:00:00Z"
`, taskID)
	if err := os.WriteFile(filepath.Join(verDir, "unit.result.yaml"), []byte(result), 0644); err != nil {
		t.Fatal(err)
	}

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "77", "--role", "verifier", "--verifier-type", "unit"); err != nil {
		t.Fatalf("verifier merge: %v", err)
	}
	raw2, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatal(err)
	}
	var out iterLogEntry
	if err := yaml.Unmarshal(raw2, &out); err != nil {
		t.Fatal(err)
	}
	if out.Impl.Item != "keep-me" {
		t.Errorf("impl.item = %q, want keep-me (verifier merge must not wipe impl)", out.Impl.Item)
	}
	if len(out.Verifiers) != 1 {
		t.Fatalf("verifiers len = %d, want 1", len(out.Verifiers))
	}
	if out.Verifiers[0].Type != "unit" || out.Verifiers[0].Status != "pass" || !out.Verifiers[0].GatePassed {
		t.Fatalf("unexpected verifier row: %#v", out.Verifiers[0])
	}
}

func TestCheckpointLogToIterBundleFeedbackGoalOnStub(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	delegDir := filepath.Join(repo, ".agents", "active", "delegation")
	bundleDir := filepath.Join(repo, ".agents", "active", "delegation-bundles")
	if err := os.MkdirAll(delegDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	const taskID = "fg-task"
	const bundleID = "del-fg-task-999002"
	contract := fmt.Sprintf(`schema_version: 1
id: %s
parent_plan_id: plan-fg
parent_task_id: %s
title: t
write_scope: []
status: active
created_at: "2026-04-18T00:00:00Z"
updated_at: "2026-04-18T00:00:00Z"
`, bundleID, taskID)
	if err := os.WriteFile(filepath.Join(delegDir, taskID+".yaml"), []byte(contract), 0644); err != nil {
		t.Fatal(err)
	}
	bundle := fmt.Sprintf(`schema_version: 1
delegation_id: %s
plan_id: plan-fg
task_id: %s
owner: test
worker:
  profile: loop-worker
scope:
  write_scope: []
prompt: {}
context: {}
verification:
  feedback_goal: "read bundle goal"
closeout: {}
`, bundleID, taskID)
	if err := os.WriteFile(filepath.Join(bundleDir, bundleID+".yaml"), []byte(bundle), 0644); err != nil {
		t.Fatal(err)
	}

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "12"); err != nil {
		t.Fatalf("stub: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "iteration-log", "iter-12.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Impl.FeedbackGoal != "read bundle goal" {
		t.Errorf("impl.feedback_goal = %q, want from delegation bundle", entry.Impl.FeedbackGoal)
	}
}

func TestCheckpointLogToIterMigratesV1Document(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	iterDir := filepath.Join(repo, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(iterDir, 0755); err != nil {
		t.Fatal(err)
	}
	v1 := `# yaml-language-server: $schema=../../../../schemas/workflow-iter-log.schema.json
schema_version: 1
iteration: 20
date: 2020-01-01
wave: old-wave
task_id: old-task
commit: deadbeef
files_changed: 1
lines_added: 2
lines_removed: 3
first_commit: false
item: legacy-item
scenario_tags: []
feedback_goal: old-fg
tests_added: 0
tests_total_pass: null
retries: 0
scope_note: ""
summary: legacy-summary
self_assessment:
  read_loop_state: true
  one_item_only: false
  committed_after_tests: false
  tests_positive_and_negative: false
  tests_used_sandbox: false
  aligned_with_canonical_tasks: false
  persisted_via_workflow_commands: ""
  ran_cli_command: false
  exercised_new_scenario: false
  cli_produced_actionable_feedback: ""
  linked_traces_to_outcomes: false
  stayed_under_10_files: false
  no_destructive_commands: false
`
	iterPath := filepath.Join(iterDir, "iter-20.yaml")
	if err := os.WriteFile(iterPath, []byte(v1), 0644); err != nil {
		t.Fatal(err)
	}

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "20"); err != nil {
		t.Fatalf("migrate pass: %v", err)
	}
	raw, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatal(err)
	}
	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d after migrate", entry.SchemaVersion)
	}
	if entry.Impl.Item != "legacy-item" {
		t.Errorf("impl.item = %q, want legacy-item", entry.Impl.Item)
	}
	if entry.Impl.Summary != "legacy-summary" {
		t.Errorf("impl.summary = %q", entry.Impl.Summary)
	}
	if !entry.Impl.SelfAssessment.ReadLoopState {
		t.Error("expected migrated read_loop_state true")
	}
}

func TestParseGitDiffStatSummary(t *testing.T) {
	cases := []struct {
		summary     string
		wantFiles   int
		wantAdded   int
		wantRemoved int
	}{
		{"3 files changed, 42 insertions(+), 5 deletions(-)", 3, 42, 5},
		{"1 file changed, 10 insertions(+)", 1, 10, 0},
		{"1 file changed, 3 deletions(-)", 1, 0, 3},
		{"2 files changed, 1 insertion(+), 1 deletion(-)", 2, 1, 1},
		{"", 0, 0, 0},
	}
	for _, tc := range cases {
		r := parseGitDiffStatSummary(tc.summary)
		if r.FilesChanged != tc.wantFiles || r.LinesAdded != tc.wantAdded || r.LinesRemoved != tc.wantRemoved {
			t.Errorf("parseGitDiffStatSummary(%q) = {files:%d added:%d removed:%d}, want {files:%d added:%d removed:%d}",
				tc.summary, r.FilesChanged, r.LinesAdded, r.LinesRemoved,
				tc.wantFiles, tc.wantAdded, tc.wantRemoved)
		}
	}
}
