package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

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
		t.Fatal("commands/workflow/static/workflow-iter-log.schema.json is out of sync with schemas/workflow-iter-log.schema.json — copy the canonical file after editing")
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
