package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

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

func TestFoldBackSlugTaskDedupesNotes(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "schema-drift-p1-t1"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "first"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "second"); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	notes := tf.Tasks[0].Notes
	if strings.Count(notes, "(fb:"+slug+")") != 1 {
		t.Fatalf("expected exactly one tagged line for slug, got notes:\n%s", notes)
	}
	if !strings.Contains(notes, "second") || strings.Contains(notes, "first") {
		t.Fatalf("expected latest observation only, got:\n%s", notes)
	}
}

func TestFoldBackUpdateMissingSlug(t *testing.T) {
	repo := setupFoldBackProject(t)
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"fold-back", "update", "--plan", "p1", "--slug", "missing-slug", "--observation", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing slug")
	}
}

func TestFoldBackUpdatePlanScoped(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "fold-back-triage-p1"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--slug", slug, "--observation", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fold-back", "update", "--plan", "p1", "--slug", slug, "--observation", "v2"); err != nil {
		t.Fatal(err)
	}
	plan, err := loadCanonicalPlan(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(plan.Summary, "(fb:"+slug+")") != 1 || !strings.Contains(plan.Summary, "v2") {
		t.Fatalf("plan summary = %q", plan.Summary)
	}
}

func TestFoldBackUpdateTaskScoped(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "coverage-regression-p1-t1"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "round-a"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fold-back", "update", "--plan", "p1", "--slug", slug, "--task", "t1", "--observation", "round-b"); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	notes := tf.Tasks[0].Notes
	if strings.Count(notes, "(fb:"+slug+")") != 1 || !strings.Contains(notes, "round-b") || strings.Contains(notes, "round-a") {
		t.Fatalf("task notes = %q", notes)
	}
}

func TestFoldBackUpdateTaskScopedRequiresTaskFlag(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "tool-bug-p1-t1"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "first"); err != nil {
		t.Fatal(err)
	}
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"fold-back", "update", "--plan", "p1", "--slug", slug, "--observation", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when updating task-scoped fold-back without --task")
	}
}

func TestFoldBackSlugInvalid(t *testing.T) {
	repo := setupFoldBackProject(t)
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", "bad slug", "--observation", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid slug")
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

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	outAll := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(outAll, `"plan_id": "p1"`) || !strings.Contains(outAll, `"plan_id": "p2"`) {
		t.Fatalf("list all should include both plans: %s", outAll)
	}

	outP1 := executeWorkflowCommandOutput(t, repo, "fold-back", "list", "--plan", "p1")
	if !strings.Contains(outP1, `"plan_id": "p1"`) || strings.Contains(outP1, `"plan_id": "p2"`) {
		t.Fatalf("filtered list: %s", outP1)
	}
}
