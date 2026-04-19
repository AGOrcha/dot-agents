package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// workflowTestJSON toggles JSON output in workflow tests (replaces commands.Flags.JSON for isolated workflow command runs).
var workflowTestJSON bool

func init() {
	// Self-contained deps so workflow tests do not import package commands (import cycle: commands → workflow).
	InitTestDeps(Deps{
		ErrNoProject: errors.New("workflow commands must run inside a project directory"),
		Flags: GlobalFlags{
			JSON: func() bool { return workflowTestJSON },
			Yes:  func() bool { return false },
		},
		ErrorWithHints: func(msg string, hints ...string) error {
			return errors.New(strings.TrimSpace(msg))
		},
		UsageError: func(msg string, hints ...string) error {
			return errors.New(strings.TrimSpace(msg))
		},
		NoArgsWithHints: func(hints ...string) cobra.PositionalArgs {
			return func(cmd *cobra.Command, args []string) error {
				if len(args) == 0 {
					return nil
				}
				return fmt.Errorf("%s does not accept positional arguments (got %d)", cmd.CommandPath(), len(args))
			}
		},
		ExactArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return func(cmd *cobra.Command, args []string) error {
				if len(args) == n {
					return nil
				}
				noun := "arguments"
				if n == 1 {
					noun = "argument"
				}
				return fmt.Errorf("%s expects %d %s, got %d", cmd.CommandPath(), n, noun, len(args))
			}
		},
		MaximumNArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return func(cmd *cobra.Command, args []string) error {
				if len(args) <= n {
					return nil
				}
				return fmt.Errorf("%s accepts at most %d argument(s), got %d", cmd.CommandPath(), n, len(args))
			}
		},
		ExampleBlock: func(lines ...string) string {
			return strings.Join(lines, "\n")
		},
	})
}

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

// runKGSetupViaCLI initializes KG_HOME using the real CLI (avoids importing package commands from workflow tests).
func runKGSetupViaCLI(t *testing.T) {
	t.Helper()
	repoRoot := dotAgentsRepoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/dot-agents", "kg", "setup")
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kg setup: %v\n%s", err, string(out))
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

// addObs1776217867311807000PlanFixture seeds a synthetic plan modeled on fold-back proposal
// obs-1776217867311807000 (advance reported success while TASKS row looked stale long task id).
func addObs1776217867311807000PlanFixture(t *testing.T, repo string) {
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
	write(".agents/workflow/plans/wf-advance-regress/PLAN.yaml", `schema_version: 1
id: "wf-advance-regress"
title: "Regression fixture for workflow advance TASKS persistence"
status: "active"
summary: "Synthetic plan for proposal obs-1776217867311807000"
created_at: "2026-04-18T12:00:00Z"
updated_at: "2026-04-18T12:00:00Z"
owner: "test"
success_criteria: "TASKS.yaml updates with advance"
verification_strategy: "go test"
current_focus_task: ""
`)
	write(".agents/workflow/plans/wf-advance-regress/TASKS.yaml", `schema_version: 1
plan_id: "wf-advance-regress"
tasks:
  - id: "phase-5d-iter-log-schema"
    title: "iter log schema alignment"
    status: "pending"
    depends_on: []
    blocks: []
    owner: "test"
    write_scope: ["commands/workflow/iter_log.go"]
    verification_required: true
    notes: ""
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
func saveTestDelegationContract(t *testing.T, repo, taskID, planID, contractID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1,
		ID:            contractID,
		ParentPlanID:  planID,
		ParentTaskID:  taskID,
		Title:         "test delegation",
		WriteScope:    []string{"commands/"},
		Status:        "active",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatalf("save delegation: %v", err)
	}
}
func newTempKGForWorkflow(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KG_HOME", dir)
	return dir
}
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
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
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
func setupVerifierDispatchProject(t *testing.T, taskAppType, planDefaultAppType string) string {
	t.Helper()
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans", "plan-vd")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1, ID: "plan-vd", Title: "Verifier dispatch", Status: "active",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z",
		DefaultAppType: planDefaultAppType,
	}
	planData, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planData, 0644); err != nil {
		t.Fatalf("write PLAN.yaml: %v", err)
	}
	task := CanonicalTask{
		ID: "task-vd", Title: "VD task", Status: "pending",
		WriteScope: []string{"docs/"}, VerificationRequired: true,
		AppType: taskAppType,
	}
	tf := CanonicalTaskFile{SchemaVersion: 1, PlanID: "plan-vd", Tasks: []CanonicalTask{task}}
	td, err := yaml.Marshal(tf)
	if err != nil {
		t.Fatalf("marshal tasks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), td, 0644); err != nil {
		t.Fatalf("write TASKS.yaml: %v", err)
	}
	rc := `{
  "version": 1,
  "project": "tmp",
  "hooks": false,
  "mcp": false,
  "settings": false,
  "sources": [{"type":"local"}],
  "verifier_profiles": {"unit":{"label":"U"},"api":{"label":"A"}},
  "app_type_verifier_map": {"api":["unit","api"]}
}`
	if err := os.WriteFile(filepath.Join(dir, ".agentsrc.json"), []byte(rc), 0644); err != nil {
		t.Fatalf("write .agentsrc.json: %v", err)
	}
	return dir
}

func loadFanoutBundle(t *testing.T, repo string, taskID string) delegationBundleYAML {
	t.Helper()
	c, err := loadDelegationContract(repo, taskID)
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
	return bundle
}
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
	cmd := NewCmdForTest()
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
