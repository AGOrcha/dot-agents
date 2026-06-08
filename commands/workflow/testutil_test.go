package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/testutil"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/execabs"
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
	cmd := exec.Command("go", "run", "./cmd/da", "kg", "setup")
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "unknown command") {
			t.Skip("kg command not available in this build")
		}
		t.Fatalf("kg setup: %v\n%s", err, string(out))
	}
}

func initWorkflowTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	testutil.InitGitRepo(t, repo, map[string]string{
		".agentsrc.json":                  `{"project":"workflow-proj","version":1,"sources":[{"type":"local"}]}`,
		".agents/active/sample.plan.md":   "# Sample Plan\n\n- [ ] First pending task\n- [ ] Second pending task\n",
		".agents/active/handoffs/next.md": "# Next Handoff\n",
		".agents/lessons.md":              "- lesson one\n- lesson two\n",
		"README.md":                       "hello\n",
	})
	// Mutate the working tree post-commit so dirty-status tests have content
	// to detect.
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}
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

// marshalCheckpointFixture serialises a workflowCheckpoint via yaml exactly
// like prod's checkpoint writer. Hand-built YAML string concatenation breaks
// on Windows because t.TempDir() paths contain backslashes that a double-
// quoted YAML scalar interprets as escape sequences (\U, \a, …), so the
// fixture must round-trip through the same marshaller prod uses.
func marshalCheckpointFixture(t *testing.T, cp workflowCheckpoint) []byte {
	t.Helper()
	cp.SchemaVersion = 1
	if cp.Files.Modified == nil {
		cp.Files.Modified = []string{}
	}
	if cp.Blockers == nil {
		cp.Blockers = []string{}
	}
	data, err := yaml.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	return data
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
	var cp workflowCheckpoint
	cp.Timestamp = timestamp
	cp.Project = workflowProjectRef{Name: projectName, Path: repo}
	cp.Git = workflowGitSummary{Branch: branch, SHA: sha}
	cp.Verification.Status = verStatus
	cp.Verification.Summary = "tests passed"
	cp.NextAction = nextAction
	if err := os.WriteFile(filepath.Join(contextDir, "checkpoint.yaml"), marshalCheckpointFixture(t, cp), 0644); err != nil {
		t.Fatal(err)
	}
}

// checkpointFixtureGitOverride bundles the inputs to
// writeCheckpointFixtureWithGitOverride so the helper stays under the
// parameter limit while keeping each value individually addressable.
type checkpointFixtureGitOverride struct {
	AgentsHome  string
	ProjectName string
	Repo        string
	NextAction  string
	VerStatus   string
	Timestamp   string
	Branch      string
	SHA         string
}

func writeCheckpointFixtureWithGitOverride(t *testing.T, in checkpointFixtureGitOverride) {
	t.Helper()
	contextDir := filepath.Join(in.AgentsHome, "context", in.ProjectName)
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatal(err)
	}
	var cp workflowCheckpoint
	cp.Timestamp = in.Timestamp
	cp.Project = workflowProjectRef{Name: in.ProjectName, Path: in.Repo}
	cp.Git = workflowGitSummary{Branch: in.Branch, SHA: in.SHA}
	cp.Verification.Status = in.VerStatus
	cp.Verification.Summary = "tests passed"
	cp.NextAction = in.NextAction
	if err := os.WriteFile(filepath.Join(contextDir, "checkpoint.yaml"), marshalCheckpointFixture(t, cp), 0644); err != nil {
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

func setupFanoutBase(t *testing.T, planTitle string) (dir, plansDir string) {
	t.Helper()
	dir = t.TempDir()
	plansDir = filepath.Join(dir, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1,
		ID:            "p1",
		Title:         planTitle,
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
	return dir, plansDir
}

func setupFanoutSliceProject(t *testing.T, sliceStatus string) string {
	t.Helper()
	dir, plansDir := setupFanoutBase(t, "Fanout Test Plan")

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
	dir, plansDir := setupFanoutBase(t, "Two-task fanout")

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
  "stage_profiles": {"verifier": {"unit":{"label":"U"},"api":{"label":"A"}}},
  "execution_profile": {"by_app_type": {"api": {"topology": {"verifier_sequence": ["unit","api"]}}}}
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

// captureStdoutWhileRunning chdirs to repo, captures os.Stdout output
// from run(), then asserts every wantSubstring appears. Restores cwd
// via t.Cleanup. Replaces the ~28-line stdout-pipe pattern repeated
// in multiple state_plan/render tests.
func captureStdoutWhileRunning(t *testing.T, repo string, run func() error, wantSubstrings ...string) {
	t.Helper()
	oldwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	if err := run(); err != nil {
		t.Fatalf("run: %v", err)
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

	rendered := string(out)
	for _, want := range wantSubstrings {
		if !strings.Contains(rendered, want) {
			t.Fatalf("output missing %q:\n%s", want, rendered)
		}
	}
}

// writeActiveDelegationContract saves a minimal active DelegationContract
// pointing at planID/taskID. Used by state_plan tests that need an
// in-flight delegation gate.
func writeActiveDelegationContract(t *testing.T, repo, contractID, planID, taskID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: contractID,
		ParentPlanID: planID, ParentTaskID: taskID,
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}
}

// savePausedPlanFixture writes a CanonicalPlan + CanonicalTaskFile pair
// representing a paused-status plan with one pending task. Shared by
// state_plan tests that exercise paused-plan scope behavior.
func savePausedPlanFixture(t *testing.T, repo string) {
	t.Helper()
	plan := &CanonicalPlan{
		SchemaVersion: 1, ID: "paused-plan", Title: "Paused Plan",
		Status: "paused", Summary: "paused for planning review",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Owner:     "test", CurrentFocusTask: "needs review",
	}
	if err := saveCanonicalPlan(repo, plan); err != nil {
		t.Fatal(err)
	}
	tf := &CanonicalTaskFile{
		SchemaVersion: 1, PlanID: "paused-plan",
		Tasks: []CanonicalTask{{
			ID: "needs-review", Title: "needs review", Status: "pending",
			Owner: "test", WriteScope: []string{"commands/workflow.go"},
			VerificationRequired: true,
		}},
	}
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
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
// Mirrors the execabs+test-identity pattern used by testutil.InitGitRepo (the helper
// underlying initWorkflowTestRepo); we cannot call into testutil here because that
// helper does init+single-commit and we need to layer an additional commit onto the
// existing repo it produces.
func initWorkflowTestRepoWithCommit(t *testing.T) string {
	t.Helper()
	repo := initWorkflowTestRepo(t)
	run := func(args ...string) {
		t.Helper()
		// execabs matches the project-wide go:S4036 hardening adopted by
		// testutil.InitGitRepo and commands/workflow/*.go production code.
		cmd := execabs.Command("git", append([]string{"-C", repo}, args...)...)
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

// realYAMLMarshal allows the stub to do a real marshal for selected calls.
func realYAMLMarshal(v any) ([]byte, error) {

	return []byte("schema_version: 1\nid: x\n"), nil
}

// captureCovStdout runs fn while os.Stdout is piped, then returns the captured
// bytes. It does NOT change cwd — callers must already be in the right repo.
func captureCovStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	data, _ := io.ReadAll(r)
	_ = r.Close()
	return string(data), runErr
}

// chdirForCov chdir's to dir, registers a cleanup to restore the original cwd.
func chdirForCov(t *testing.T, dir string) {
	t.Helper()
	oldwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

func captureStdoutToString(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	buf := make([]byte, 16384)
	n, _ := r.Read(buf)
	_ = r.Close()
	return string(buf[:n])
}

func (e errorString) Error() string { return string(e) }

// seedManagedProject writes AGENTS_HOME/config.json with a single managed project entry.
// When AGENTS_HOME is set, that path is the .agents dir itself; otherwise we
// write to <home>/.agents/config.json.
func seedManagedProject(agentsHome, name, path string) error {
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		return err
	}
	cfg := map[string]any{
		"version": 1,
		"projects": map[string]any{
			name: map[string]any{
				"path":  path,
				"added": time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(agentsHome, "config.json"), data, 0644)
}

// chmodUnreadable makes a file unreadable for the duration of the test using
// the OS-native denial mechanism. Delegates to testutil.MakeFileUnreadable so
// the per-platform machinery (POSIX chmod 0 / Windows byte-range lock) lives
// in exactly one place — see internal/testutil/perms.go for the rationale and
// the root-on-POSIX skip handling.
func chmodUnreadable(t *testing.T, path string) {
	t.Helper()
	testutil.MakeFileUnreadable(t, path)
}

// chmodUnreadableDir makes a directory unenumerable for the duration of the
// test using the OS-native denial mechanism. Delegates to
// testutil.MakeDirUnreadable so the per-platform machinery (POSIX chmod 0 /
// Windows DACL deny-ACE) lives in exactly one place — see
// internal/testutil/perms_dir.go for the rationale and the root/elevated-priv
// skip handling.
func chmodUnreadableDir(t *testing.T, path string) {
	t.Helper()
	testutil.MakeDirUnreadable(t, path)
}

func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// newDriftTestCommand builds a cobra.Command with flags runWorkflowDrift reads.
func newDriftTestCommand(project string, staleDays, proposalDays int) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Int("stale-days", staleDays, "")
	c.Flags().Int("proposal-days", proposalDays, "")
	c.Flags().String("project", project, "")
	return c
}

func newFoldBackTestCmd(observation, slug, planID string) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("plan", planID, "")
	c.Flags().String("task", "", "")
	c.Flags().String("observation", observation, "")
	c.Flags().Bool("propose", false, "")
	c.Flags().String("slug", slug, "")
	return c
}

// newSweepTestCommand builds a cobra.Command with the flags runWorkflowSweep reads.
func newSweepTestCommand(apply bool, staleDays, proposalDays int) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Int("stale-days", staleDays, "")
	c.Flags().Int("proposal-days", proposalDays, "")
	c.Flags().Bool("apply", apply, "")
	return c
}

type errorString string

func newGraphQueryTestCommand(intent, scope string) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("intent", intent, "")
	c.Flags().String("scope", scope, "")
	return c
}

// errSentinelDriftLister is a fixed sentinel for the lister stub above so
// tests can assert error chain identity without string matching everywhere.
var errSentinelDriftLister = errorString("synthetic lister failure")
