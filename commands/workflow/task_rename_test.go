package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

const taskRepointTestPlanID = "task-repoint-plan"

func setupTaskRepointRepo(t *testing.T, tasks []CanonicalTask) string {
	t.Helper()
	repo := initWorkflowTestRepo(t)
	plan := &CanonicalPlan{
		SchemaVersion: 1,
		ID:            taskRepointTestPlanID,
		Title:         "Task repoint plan",
		Status:        "active",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveCanonicalPlan(repo, plan); err != nil {
		t.Fatal(err)
	}
	if err := saveCanonicalTasks(repo, &CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        taskRepointTestPlanID,
		Tasks:         tasks,
	}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	return repo
}

func taskByID(t *testing.T, tf *CanonicalTaskFile, id string) CanonicalTask {
	t.Helper()
	for _, task := range tf.Tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %q not found", id)
	return CanonicalTask{}
}

func assertNoTaskReference(t *testing.T, tf *CanonicalTaskFile, oldID string) {
	t.Helper()
	raw, err := yaml.Marshal(tf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), oldID) {
		t.Fatalf("old task ID %q remains in TASKS.yaml:\n%s", oldID, raw)
	}
}

func TestRunWorkflowTaskRename_RepointsDependenciesAndFoldBackTags(t *testing.T) {
	repo := setupTaskRepointRepo(t, []CanonicalTask{
		{ID: "task-old", Title: "Original", Status: "completed", Notes: "- (fb:coverage-task-old) fixed\n- (fb:unrelated) keep"},
		{ID: "dependent", Title: "Dependent", Status: "pending", DependsOn: []string{"task-old", "stable"}},
		{ID: "blocker", Title: "Blocker", Status: "pending", Blocks: []string{"task-old"}},
		{ID: "qualified", Title: "Qualified", Status: "pending", DependsOn: []string{taskRepointTestPlanID + "/task-old"}},
	})

	var out bytes.Buffer
	if err := runWorkflowTaskRename(&out, taskRepointTestPlanID, "task-old", "task-new", false, false); err != nil {
		t.Fatalf("rename: %v", err)
	}
	tf, err := loadCanonicalTasks(repo, taskRepointTestPlanID)
	if err != nil {
		t.Fatal(err)
	}
	assertNoTaskReference(t, tf, "task-old")
	renamed := taskByID(t, tf, "task-new")
	if !strings.Contains(renamed.Notes, "(fb:coverage-task-new)") || !strings.Contains(renamed.Notes, "(fb:unrelated)") {
		t.Fatalf("fold-back tags not safely repointed: %q", renamed.Notes)
	}
	if got := taskByID(t, tf, "dependent").DependsOn; !slices.Equal(got, []string{"task-new", "stable"}) {
		t.Fatalf("dependent depends_on = %v", got)
	}
	if got := taskByID(t, tf, "blocker").Blocks; !slices.Equal(got, []string{"task-new"}) {
		t.Fatalf("blocker blocks = %v", got)
	}
	if got := taskByID(t, tf, "qualified").DependsOn; !slices.Equal(got, []string{taskRepointTestPlanID + "/task-new"}) {
		t.Fatalf("qualified depends_on = %v", got)
	}
	if rendered := out.String(); !strings.Contains(rendered, "dependent: depends_on") || !strings.Contains(rendered, "blocker: blocks") {
		t.Fatalf("rename output did not list repointed dependents:\n%s", rendered)
	}
}

func TestRunWorkflowTaskSupersede_RemovesOldAndRepoints(t *testing.T) {
	repo := setupTaskRepointRepo(t, []CanonicalTask{
		{ID: "task-old", Title: "Old", Status: "pending"},
		{ID: "task-new", Title: "New", Status: "completed", DependsOn: []string{"task-old"}, Blocks: []string{"task-old"}},
		{ID: "dependent", Title: "Dependent", Status: "pending", DependsOn: []string{"task-old", "task-new"}, Blocks: []string{"task-old"}},
	})

	if err := runWorkflowTaskSupersede(&bytes.Buffer{}, taskRepointTestPlanID, "task-old", "task-new", false, false); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	tf, err := loadCanonicalTasks(repo, taskRepointTestPlanID)
	if err != nil {
		t.Fatal(err)
	}
	assertNoTaskReference(t, tf, "task-old")
	if len(tf.Tasks) != 2 {
		t.Fatalf("task count = %d, want 2", len(tf.Tasks))
	}
	replacement := taskByID(t, tf, "task-new")
	if len(replacement.DependsOn) != 0 || len(replacement.Blocks) != 0 {
		t.Fatalf("replacement retained a self-edge: depends_on=%v blocks=%v", replacement.DependsOn, replacement.Blocks)
	}
	dependent := taskByID(t, tf, "dependent")
	if !slices.Equal(dependent.DependsOn, []string{"task-new"}) {
		t.Fatalf("deduplicated depends_on = %v", dependent.DependsOn)
	}
	if !slices.Equal(dependent.Blocks, []string{"task-new"}) {
		t.Fatalf("blocks = %v", dependent.Blocks)
	}
}

func TestRunWorkflowTaskRepoint_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		oldID     string
		newID     string
		want      string
	}{
		{name: "rename missing old", operation: "rename", oldID: "missing", newID: "fresh", want: "not found"},
		{name: "rename collision", operation: "rename", oldID: "task-old", newID: "task-new", want: "already exists"},
		{name: "supersede missing old", operation: "supersede", oldID: "missing", newID: "task-new", want: "not found"},
		{name: "supersede missing replacement", operation: "supersede", oldID: "task-old", newID: "missing", want: "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTaskRepointRepo(t, []CanonicalTask{
				{ID: "task-old", Title: "Old", Status: "pending"},
				{ID: "task-new", Title: "New", Status: "pending"},
			})
			var err error
			if tt.operation == "rename" {
				err = runWorkflowTaskRename(&bytes.Buffer{}, taskRepointTestPlanID, tt.oldID, tt.newID, false, false)
			} else {
				err = runWorkflowTaskSupersede(&bytes.Buffer{}, taskRepointTestPlanID, tt.oldID, tt.newID, false, false)
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestWorkflowTaskRename_DryRunJSONMutatesNothing(t *testing.T) {
	repo := setupTaskRepointRepo(t, []CanonicalTask{
		{ID: "task-old", Title: "Old", Status: "completed"},
		{ID: "dependent", Title: "Dependent", Status: "pending", DependsOn: []string{"task-old"}},
	})
	tasksPath := filepath.Join(plansBaseDir(repo), taskRepointTestPlanID, workflowTasksFileName)
	before, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := newWorkflowTaskCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rename", taskRepointTestPlanID, "--from", "task-old", "--to", "task-new", "-n", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run command: %v\n%s", err, out.String())
	}
	after, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("--dry-run changed TASKS.yaml")
	}
	var result taskRepointResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out.String())
	}
	if !result.DryRun || len(result.Repointed) != 1 || result.Repointed[0].TaskID != "dependent" {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
}

func TestWorkflowTaskSupersede_DryRunMutatesNothing(t *testing.T) {
	repo := setupTaskRepointRepo(t, []CanonicalTask{
		{ID: "task-old", Title: "Old", Status: "completed"},
		{ID: "task-new", Title: "New", Status: "completed"},
		{ID: "dependent", Title: "Dependent", Status: "pending", DependsOn: []string{"task-old"}},
	})
	tasksPath := filepath.Join(plansBaseDir(repo), taskRepointTestPlanID, workflowTasksFileName)
	before, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := newWorkflowTaskCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"supersede", taskRepointTestPlanID, "--old", "task-old", "--new", "task-new", "-n"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run command: %v\n%s", err, out.String())
	}
	after, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("supersede --dry-run changed TASKS.yaml")
	}
	if rendered := out.String(); !strings.Contains(rendered, "Would supersede") || !strings.Contains(rendered, "dependent: depends_on") {
		t.Fatalf("unexpected dry-run output:\n%s", rendered)
	}
}

func TestRunWorkflowTaskRename_GitRefBackendRemovesOldBlobAndMirrorsDependents(t *testing.T) {
	tasksYAML := "schema_version: 1\nplan_id: \"" + stateRefTestPlanID + "\"\ntasks:\n" +
		"  - id: \"task-old\"\n    title: \"Old\"\n    status: \"completed\"\n    verification_required: true\n" +
		"  - id: \"dependent\"\n    title: \"Dependent\"\n    status: \"pending\"\n    depends_on: [\"task-old\"]\n    verification_required: true\n"
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAML)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	if err := mirrorTransitionToStateRef(repo, stateRefTestPlanID, "task-old"); err != nil {
		t.Fatalf("seed state ref: %v", err)
	}
	before := stateRefHead(repo)

	if err := runWorkflowTaskRename(&bytes.Buffer{}, stateRefTestPlanID, "task-old", "task-new", false, false); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if after := stateRefHead(repo); after == "" || after == before {
		t.Fatalf("state ref did not move: before=%q after=%q", before, after)
	}
	projected, err := projectPlanTasksFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatal(err)
	}
	assertNoTaskReference(t, projected, "task-old")
	if got := taskByID(t, projected, "dependent").DependsOn; !slices.Equal(got, []string{"task-new"}) {
		t.Fatalf("ref dependent depends_on = %v", got)
	}
}

func TestRunWorkflowTaskRename_GitRefBackendPreservesConcurrentTransition(t *testing.T) {
	tasksYAML := "schema_version: 1\nplan_id: \"" + stateRefTestPlanID + "\"\ntasks:\n" +
		"  - id: \"task-old\"\n    title: \"Old\"\n    status: \"completed\"\n    verification_required: true\n" +
		"  - id: \"dependent\"\n    title: \"Dependent\"\n    status: \"pending\"\n    depends_on: [\"task-old\"]\n    verification_required: true\n"
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAML)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	if err := mirrorTransitionToStateRef(repo, stateRefTestPlanID, "task-old"); err != nil {
		t.Fatalf("seed state ref: %v", err)
	}

	previousCAS := casSwapFn
	t.Cleanup(func() { casSwapFn = previousCAS })
	attempts := 0
	var racingCAS func(string, string, string) error
	racingCAS = func(projectPath, newCommit, old string) error {
		attempts++
		if attempts == 1 {
			current, err := projectPlanTasksFromStateRef(projectPath, stateRefTestPlanID)
			if err != nil {
				return err
			}
			for i := range current.Tasks {
				if current.Tasks[i].ID == "dependent" {
					current.Tasks[i].Status = "completed"
				}
			}
			for _, rec := range splitCanonicalTaskFile(current) {
				if rec.Task.ID != "dependent" {
					continue
				}
				content, err := yamlMarshal(rec)
				if err != nil {
					return err
				}
				rel, err := planTaskStateRefRelPath(projectPath, stateRefTestPlanID, "dependent")
				if err != nil {
					return err
				}
				casSwapFn = compareAndSwapStateRef
				err = writeStateRefCAS(projectPath, []stateRefFile{{relPath: rel, content: content}})
				casSwapFn = racingCAS
				if err != nil {
					return err
				}
				break
			}
		}
		return compareAndSwapStateRef(projectPath, newCommit, old)
	}
	casSwapFn = racingCAS

	if err := runWorkflowTaskRename(&bytes.Buffer{}, stateRefTestPlanID, "task-old", "task-new", false, false); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if attempts < 2 {
		t.Fatalf("expected a CAS retry, got %d attempt(s)", attempts)
	}
	projected, err := projectPlanTasksFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatal(err)
	}
	dependent := taskByID(t, projected, "dependent")
	if dependent.Status != "completed" {
		t.Fatalf("concurrent status transition was lost: %q", dependent.Status)
	}
	if !slices.Equal(dependent.DependsOn, []string{"task-new"}) {
		t.Fatalf("concurrent projection depends_on = %v", dependent.DependsOn)
	}
}
