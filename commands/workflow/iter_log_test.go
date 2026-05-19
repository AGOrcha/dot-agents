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

func assertIterLogHeader(t *testing.T, content string) {
	t.Helper()
	if !strings.HasPrefix(content, "# yaml-language-server:") {
		t.Errorf("missing yaml-language-server header; got: %q", content[:min(len(content), 80)])
	}
	if !strings.Contains(content, "workflow-iter-log.schema.json") {
		t.Errorf("header does not reference schema: %s", content[:min(len(content), 120)])
	}
}

func assertIterLogDeterministicFields(t *testing.T, entry iterLogEntry, iterN int) {
	t.Helper()
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
	if entry.Commit == "" {
		t.Errorf("commit sha is empty; expected a git SHA")
	}
	if entry.FilesChanged < 0 {
		t.Errorf("files_changed = %d, want >= 0", entry.FilesChanged)
	}
}

func assertIterLogImplDefaults(t *testing.T, entry iterLogEntry) {
	t.Helper()
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
}

func assertIterLogSelfAssessmentDefaults(t *testing.T, entry iterLogEntry) {
	t.Helper()
	isa := entry.Impl.SelfAssessment
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
}

func assertIterLogVerifierDefaults(t *testing.T, entry iterLogEntry) {
	t.Helper()
	if len(entry.Verifiers) != 0 {
		t.Errorf("verifiers = %v, want empty", entry.Verifiers)
	}
	if entry.Review.Phase1Decision != "" || entry.Review.Phase2Decision != "" || entry.Review.OverallDecision != "" {
		t.Errorf("review decisions should be empty stubs, got %#v", entry.Review)
	}
}

func writeDelegationFlowArtifacts(t *testing.T, delegDir, bundleDir, taskID, bundleID string) {
	t.Helper()
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
}

func patchIterLogImplItem(t *testing.T, repo string, iter int, item string) {
	t.Helper()
	iterPath := filepath.Join(repo, ".agents", "active", "iteration-log", fmt.Sprintf("iter-%d.yaml", iter))
	raw, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatal(err)
	}
	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	entry.Impl.Item = item
	body, err := yaml.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	const header = "# yaml-language-server: $schema=../../../../schemas/workflow-iter-log.schema.json\n"
	if err := os.WriteFile(iterPath, append([]byte(header), body...), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeVerifierResultFixture(t *testing.T, repo, taskID string) {
	t.Helper()
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
}

func assertVerifierMergePreservedImpl(t *testing.T, repo string, iter int) {
	t.Helper()
	iterPath := filepath.Join(repo, ".agents", "active", "iteration-log", fmt.Sprintf("iter-%d.yaml", iter))
	raw, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatal(err)
	}
	var out iterLogEntry
	if err := yaml.Unmarshal(raw, &out); err != nil {
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

	t.Run("header-schema", func(t *testing.T) {
		assertIterLogHeader(t, content)
	})

	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal iter-38.yaml: %v", err)
	}

	t.Run("cli-deterministic", func(t *testing.T) {
		assertIterLogDeterministicFields(t, entry, iterN)
	})

	t.Run("impl-stubs", func(t *testing.T) {
		assertIterLogImplDefaults(t, entry)
	})

	t.Run("self-assessment-defaults", func(t *testing.T) {
		assertIterLogSelfAssessmentDefaults(t, entry)
	})

	t.Run("verifiers-and-review", func(t *testing.T) {
		assertIterLogVerifierDefaults(t, entry)
	})

	t.Run("schema-validation", func(t *testing.T) {
		if err := validateWorkflowIterLogEntry(&entry); err != nil {
			t.Fatalf("schema validation failed for valid stub: %v", err)
		}
	})
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

// setupDelegationFlowEnv combines initWorkflowTestRepoWithCommit + a
// fresh AGENTS_HOME + delegation/delegation-bundles dirs. Returns
// (repo, delegDir, bundleDir).
func setupDelegationFlowEnv(t *testing.T) (repo, delegDir, bundleDir string) {
	t.Helper()
	repo = initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	delegDir = filepath.Join(repo, ".agents", "active", "delegation")
	bundleDir = filepath.Join(repo, ".agents", "active", "delegation-bundles")
	if err := os.MkdirAll(delegDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	return
}

// runCheckpointLogToIter sets AGENTS_HOME, runs `workflow checkpoint
// --log-to-iter <iter>`, reads the resulting iter-<iter>.yaml, and
// unmarshals it. Returns the entry. Used by TestCheckpointLogToIter*
// tests that all share this run-and-read shape.
func runCheckpointLogToIter(t *testing.T, repo string, iter int) iterLogEntry {
	t.Helper()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	iterStr := fmt.Sprintf("%d", iter)
	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", iterStr); err != nil {
		t.Fatalf("checkpoint --log-to-iter %d: %v", iter, err)
	}
	iterPath := filepath.Join(repo, ".agents", "active", "iteration-log", "iter-"+iterStr+".yaml")
	raw, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatalf("iter-%d.yaml not created: %v", iter, err)
	}
	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return entry
}

func TestCheckpointLogToIterFirstCommit(t *testing.T) {
	// Repo with only one commit: HEAD~1 does not exist → first_commit: true, counts 0
	repo := initWorkflowTestRepo(t)
	entry := runCheckpointLogToIter(t, repo, 1)

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
	entry := runCheckpointLogToIter(t, repo, 5)
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

func TestCheckpointLogToIterIgnoresCompletedDelegationContracts(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	delegDir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(delegDir, 0755); err != nil {
		t.Fatal(err)
	}
	completed := `schema_version: 1
id: del-a-completed
parent_plan_id: plan-a
parent_task_id: a-completed
title: completed
write_scope: []
status: completed
created_at: "2026-04-18T00:00:00Z"
updated_at: "2026-04-18T00:00:00Z"
`
	if err := os.WriteFile(filepath.Join(delegDir, "a-completed.yaml"), []byte(completed), 0644); err != nil {
		t.Fatal(err)
	}
	active := `schema_version: 1
id: del-z-active
parent_plan_id: plan-z
parent_task_id: z-active
title: active
write_scope: []
status: active
created_at: "2026-04-18T00:00:00Z"
updated_at: "2026-04-18T00:00:00Z"
`
	if err := os.WriteFile(filepath.Join(delegDir, "z-active.yaml"), []byte(active), 0644); err != nil {
		t.Fatal(err)
	}

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "6"); err != nil {
		t.Fatalf("checkpoint --log-to-iter 6: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "iteration-log", "iter-6.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Wave != "plan-z" {
		t.Errorf("wave = %q, want active delegation plan-z", entry.Wave)
	}
	if entry.TaskID != "z-active" {
		t.Errorf("task_id = %q, want active delegation z-active", entry.TaskID)
	}
}

func TestCheckpointLogToIterReviewMergeMarksVerifyRecordAppended(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	delegDir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(delegDir, 0755); err != nil {
		t.Fatal(err)
	}
	const taskID = "review-task"
	contract := `schema_version: 1
id: del-review-task
parent_plan_id: plan-review
parent_task_id: review-task
title: review
write_scope: []
status: active
created_at: "2026-04-18T00:00:00Z"
updated_at: "2026-04-18T00:00:00Z"
`
	if err := os.WriteFile(filepath.Join(delegDir, taskID+".yaml"), []byte(contract), 0644); err != nil {
		t.Fatal(err)
	}
	verDir := filepath.Join(repo, ".agents", "active", "verification", taskID)
	if err := os.MkdirAll(verDir, 0755); err != nil {
		t.Fatal(err)
	}
	decision := `schema_version: 1
task_id: review-task
parent_plan_id: plan-review
phase_1_decision: accept
phase_2_decision: accept
overall_decision: accept
failed_gates: []
reviewer_notes: ok
recorded_at: "2026-04-18T12:00:00Z"
recorded_by: test
`
	if err := os.WriteFile(filepath.Join(verDir, "review-decision.yaml"), []byte(decision), 0644); err != nil {
		t.Fatal(err)
	}

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "7", "--role", "review"); err != nil {
		t.Fatalf("checkpoint --role review: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "iteration-log", "iter-7.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Review.OverallDecision != "accept" {
		t.Errorf("overall_decision = %q, want accept", entry.Review.OverallDecision)
	}
	if !entry.Review.VerifyRecordAppended {
		t.Error("verify_record_appended = false, want true when review-decision.yaml was merged")
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
	repo, delegDir, bundleDir := setupDelegationFlowEnv(t)
	const taskID = "slice-task"
	const bundleID = "del-slice-task-999001"
	writeDelegationFlowArtifacts(t, delegDir, bundleDir, taskID, bundleID)

	t.Run("stub-and-patch", func(t *testing.T) {
		if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "77"); err != nil {
			t.Fatalf("stub: %v", err)
		}
		patchIterLogImplItem(t, repo, 77, "keep-me")
	})

	writeVerifierResultFixture(t, repo, taskID)

	t.Run("verifier-merge-preserves", func(t *testing.T) {
		if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "77", "--role", "verifier", "--verifier-type", "unit"); err != nil {
			t.Fatalf("verifier merge: %v", err)
		}
		assertVerifierMergePreservedImpl(t, repo, 77)
	})
}

func TestCheckpointLogToIterBundleFeedbackGoalOnStub(t *testing.T) {
	repo, delegDir, bundleDir := setupDelegationFlowEnv(t)
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

// TestParseAIAgentEnv covers the AI_AGENT decoder used to pin the active
// platform without probing session env vars.
func TestParseAIAgentEnv(t *testing.T) {
	cases := []struct {
		raw, wantHarness, wantVersion string
	}{
		{"claude_2-1-138_agent", "claude", "2.1.138"},
		{"codex_0-9-0_agent", "codex", "0.9.0"},
		{"cursor_agent", "cursor", ""}, // no underscore in version
		{"unknown", "unknown", ""},     // no underscore at all
	}
	for _, tc := range cases {
		h, v := parseAIAgentEnv(tc.raw)
		if h != tc.wantHarness || v != tc.wantVersion {
			t.Errorf("parseAIAgentEnv(%q) = (%q, %q), want (%q, %q)", tc.raw, h, v, tc.wantHarness, tc.wantVersion)
		}
	}
}

// clearAgentSessionEnv unsets AI_AGENT and every platform's session/entrypoint
// env vars so resolveAgentBlock has no signals to discover.
func clearAgentSessionEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"AI_AGENT",
		"CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_ENTRYPOINT",
		"CODEX_SESSION_ID",
		"CURSOR_SESSION_ID",
		"OPENCODE_SESSION_ID",
	} {
		t.Setenv(name, "")
	}
}

// TestResolveAgentBlock_NilWhenNoSignals returns nil when neither AI_AGENT
// nor any platform session-env vars are set.
func TestResolveAgentBlock_NilWhenNoSignals(t *testing.T) {
	clearAgentSessionEnv(t)
	block := resolveAgentBlock(t.TempDir())
	if block != nil {
		t.Fatalf("expected nil agent block when no signals present, got %+v", block)
	}
}

// TestResolveAgentBlock_UsesAIAgentEnv parses the harness pin even when there
// is no session reader to match.
func TestResolveAgentBlock_UsesAIAgentEnv(t *testing.T) {
	clearAgentSessionEnv(t)
	t.Setenv("AI_AGENT", "codex_1-2-3_agent")
	block := resolveAgentBlock(t.TempDir())
	if block == nil {
		t.Fatal("expected non-nil block when AI_AGENT is set")
	}
	if block.Harness != "codex" {
		t.Errorf("harness = %q, want codex", block.Harness)
	}
	if block.HarnessVersion != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", block.HarnessVersion)
	}
}

// TestFirstEnv covers the env-var-first-wins helper used by resolveAgentBlock.
func TestFirstEnv(t *testing.T) {
	t.Setenv("PR3B_FIRST_ENV_A", "")
	t.Setenv("PR3B_FIRST_ENV_B", "second")
	got := firstEnv([]string{"PR3B_FIRST_ENV_A", "PR3B_FIRST_ENV_B"})
	if got != "second" {
		t.Fatalf("firstEnv = %q, want 'second'", got)
	}
	got = firstEnv([]string{"PR3B_FIRST_ENV_MISSING"})
	if got != "" {
		t.Fatalf("firstEnv missing = %q, want empty", got)
	}
}

// TestValidateIterLogRoleFlags covers the role/verifier-type validation matrix.
func TestValidateIterLogRoleFlags(t *testing.T) {
	cases := []struct {
		role, vt string
		wantErr  bool
	}{
		{"", "", false},
		{"impl", "", false},
		{"review", "", false},
		{"verifier", "unit", false},
		{"verifier", "", true}, // verifier requires verifier-type
		{"", "unit", true},     // verifier-type without role
		{"impl", "unit", true}, // verifier-type only valid with verifier role
		{"unknown", "", true},
	}
	for _, tc := range cases {
		err := validateIterLogRoleFlags(tc.role, tc.vt)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateIterLogRoleFlags(role=%q, vt=%q) err=%v, wantErr=%v", tc.role, tc.vt, err, tc.wantErr)
		}
	}
}

// TestLoadPrevCheckpointAt_FirstIterReturnsEmpty covers the n<=1 short-circuit
// and the missing-file fall-through.
func TestLoadPrevCheckpointAt_FirstIterReturnsEmpty(t *testing.T) {
	if got := loadPrevCheckpointAt(t.TempDir(), 1); got != "" {
		t.Fatalf("iter 1 should return empty, got %q", got)
	}
	if got := loadPrevCheckpointAt(t.TempDir(), 5); got != "" {
		t.Fatalf("missing prev iter should return empty, got %q", got)
	}
}

// TestLoadPrevCheckpointAt_ReadsPrior returns the checkpoint_at from the prior
// iter when present.
func TestLoadPrevCheckpointAt_ReadsPrior(t *testing.T) {
	dir := t.TempDir()
	prior := `schema_version: 2
iteration: 4
checkpoint_at: "2026-04-18T12:34:56Z"
`
	if err := os.WriteFile(filepath.Join(dir, "iter-4.yaml"), []byte(prior), 0644); err != nil {
		t.Fatal(err)
	}
	got := loadPrevCheckpointAt(dir, 5)
	if got != "2026-04-18T12:34:56Z" {
		t.Fatalf("got %q, want '2026-04-18T12:34:56Z'", got)
	}
}

// TestCheckpointLogToIter_RecordsAgentBlockFromAIAgentEnv exercises the iter
// log path with AI_AGENT set so the agent block is populated.
func TestCheckpointLogToIter_RecordsAgentBlockFromAIAgentEnv(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	clearAgentSessionEnv(t)
	t.Setenv("AI_AGENT", "codex_9-9-9_agent")

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "42"); err != nil {
		t.Fatalf("checkpoint --log-to-iter 42: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "iteration-log", "iter-42.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var entry iterLogEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Agent == nil {
		t.Fatal("expected agent block recorded when AI_AGENT is set")
	}
	if entry.Agent.Harness != "codex" {
		t.Fatalf("agent.harness = %q, want codex", entry.Agent.Harness)
	}
	if entry.Agent.HarnessVersion != "9.9.9" {
		t.Fatalf("agent.harness_version = %q, want 9.9.9", entry.Agent.HarnessVersion)
	}
}

// TestCheckpointLogToIter_VerifierMissingResultFileFails ensures the verifier
// merge surfaces a clear error when the verifier result file is absent.
func TestCheckpointLogToIter_VerifierMissingResultFileFails(t *testing.T) {
	repo, delegDir, bundleDir := setupDelegationFlowEnv(t)
	const taskID = "no-result"
	const bundleID = "del-no-result-999003"
	writeDelegationFlowArtifacts(t, delegDir, bundleDir, taskID, bundleID)

	// Stub the iter first.
	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "88"); err != nil {
		t.Fatalf("stub: %v", err)
	}
	// No verifier result file exists yet — merge must fail with a read error.
	err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "88", "--role", "verifier", "--verifier-type", "unit")
	if err == nil {
		t.Fatal("expected error when verifier result is missing")
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

func TestScanActiveDelegationContract_NoContracts(t *testing.T) {
	dir := t.TempDir()
	wave, taskID := scanActiveDelegationContract(dir)
	if wave != "" || taskID != "" {
		t.Errorf("expected empty pair for no contracts, got (%q,%q)", wave, taskID)
	}
}

func TestScanActiveDelegationContract_ActiveContractReturned(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-a", ParentPlanID: "plan-x", ParentTaskID: "task-y",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, c); err != nil {
		t.Fatal(err)
	}
	wave, taskID := scanActiveDelegationContract(dir)
	if wave != "plan-x" || taskID != "task-y" {
		t.Errorf("scanActiveDelegationContract = (%q,%q), want (plan-x,task-y)", wave, taskID)
	}
}

func TestScanActiveDelegationContract_ClosedContractsSkipped(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	closed := &DelegationContract{
		SchemaVersion: 1, ID: "del-closed", ParentPlanID: "plan-closed", ParentTaskID: "task-closed",
		Title: "x", WriteScope: []string{"commands/"}, Status: "completed",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, closed); err != nil {
		t.Fatal(err)
	}
	wave, taskID := scanActiveDelegationContract(dir)
	if wave != "" || taskID != "" {
		t.Errorf("expected empty for only-closed contracts, got (%q,%q)", wave, taskID)
	}
}

func TestMergeImplIterLog_NoBundleFallsBackEmpty(t *testing.T) {
	dir := t.TempDir()
	entry := &iterLogEntry{}

	mergeImplIterLog(entry, nil, dir)
	if entry.Impl.FeedbackGoal != "" {
		t.Errorf("expected empty feedback goal with nil contract, got %q", entry.Impl.FeedbackGoal)
	}
}

func TestMergeImplIterLog_WithBundle(t *testing.T) {
	dir := t.TempDir()
	bundlesDir := delegationBundlesDir(dir)
	if err := os.MkdirAll(bundlesDir, 0755); err != nil {
		t.Fatal(err)
	}
	bundle := delegationBundleYAML{}
	bundle.Verification.FeedbackGoal = "tests must cover new behavior"
	data, err := yaml.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundlesDir, "del-id.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	c := &DelegationContract{ID: "del-id"}
	entry := &iterLogEntry{}
	mergeImplIterLog(entry, c, dir)
	if entry.Impl.FeedbackGoal != "tests must cover new behavior" {
		t.Errorf("expected feedback goal merged from bundle, got %q", entry.Impl.FeedbackGoal)
	}
}

func TestMergeReviewIterLog_NoFile(t *testing.T) {
	dir := t.TempDir()
	entry := &iterLogEntry{}
	if err := mergeReviewIterLog(entry, dir, "t1"); err != nil {
		t.Fatal(err)
	}

	if entry.Review.Phase1Decision != "" {
		t.Errorf("expected no phase1 decision when file absent, got %q", entry.Review.Phase1Decision)
	}
	if entry.Review.DecisionArtifact == "" {
		t.Errorf("expected decision artifact path populated")
	}
}

func TestMergeReviewIterLog_FromFile(t *testing.T) {
	dir := t.TempDir()
	taskID := "task-review"
	rel := iterLogReviewDecisionPath(taskID)
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	doc := []byte(`phase_1_decision: accept
phase_2_decision: reject
overall_decision: reject
failed_gates:
  - unit
  - integration
escalation_reason: needs more work
reviewer_notes: detailed
`)
	if err := os.WriteFile(full, doc, 0644); err != nil {
		t.Fatal(err)
	}
	entry := &iterLogEntry{}
	if err := mergeReviewIterLog(entry, dir, taskID); err != nil {
		t.Fatal(err)
	}
	if entry.Review.Phase1Decision != "accept" || entry.Review.Phase2Decision != "reject" {
		t.Errorf("phase decisions = %q/%q, want accept/reject",
			entry.Review.Phase1Decision, entry.Review.Phase2Decision)
	}
	if entry.Review.OverallDecision != "reject" {
		t.Errorf("overall = %q want reject", entry.Review.OverallDecision)
	}
	if len(entry.Review.FailedGates) != 2 {
		t.Errorf("failed gates = %v, want 2 entries", entry.Review.FailedGates)
	}
	if !entry.Review.VerifyRecordAppended {
		t.Error("expected VerifyRecordAppended = true after parsing file")
	}
}

func TestMergeReviewIterLog_EmptyTaskID(t *testing.T) {
	dir := t.TempDir()
	entry := &iterLogEntry{}
	if err := mergeReviewIterLog(entry, dir, ""); err != nil {
		t.Errorf("empty task id should not error, got: %v", err)
	}
}

func TestFirstReadableDelegationContract_PrefersActive(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	closed := &DelegationContract{
		SchemaVersion: 1, ID: "del-closed", ParentPlanID: "pc", ParentTaskID: "tc",
		Title: "x", WriteScope: []string{"commands/"}, Status: "completed",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, closed); err != nil {
		t.Fatal(err)
	}
	active := &DelegationContract{
		SchemaVersion: 1, ID: "del-active", ParentPlanID: "pa", ParentTaskID: "ta",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, active); err != nil {
		t.Fatal(err)
	}
	c := firstReadableDelegationContract(dir)
	if c == nil || c.Status != "active" {
		t.Fatalf("expected active contract, got %+v", c)
	}
}

func TestLoadIterLogDocument_InvalidYAML(t *testing.T) {
	if _, err := loadIterLogDocument([]byte("not: : valid")); err == nil {
		t.Error("expected parse error for invalid YAML")
	}
}

// TestLoadIterLogDocument_V2UnmarshalError drives the v2 yaml.Unmarshal error
// branch: the schema_version probe succeeds (it only reads an int field) but
// a type-mismatched field makes the full iterLogEntry unmarshal fail.
func TestLoadIterLogDocument_V2UnmarshalError(t *testing.T) {
	bad := []byte("schema_version: 2\niteration: [not, an, int]\n")
	_, err := loadIterLogDocument(bad)
	if err == nil {
		t.Fatal("expected v2 unmarshal error, got nil")
	}
	if !strings.Contains(err.Error(), "parse iteration log v2") {
		t.Errorf("expected v2 parse error, got %q", err.Error())
	}
}

// TestLoadOrInitIterLogEntry_CorruptFile drives the loadIterLogDocument-error
// propagation branch in loadOrInitIterLogEntry via a corrupt on-disk file.
func TestLoadOrInitIterLogEntry_CorruptFile(t *testing.T) {
	iterPath := filepath.Join(t.TempDir(), "iter-1.yaml")
	if err := os.WriteFile(iterPath, []byte("not: : valid"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrInitIterLogEntry(iterPath, "goal", "t1"); err == nil {
		t.Fatal("expected error from corrupt iter log, got nil")
	}
}

// TestWriteIterLogEntry_ValidationError drives the validate-error branch in
// writeIterLogEntry with a schema-invalid entry.
func TestWriteIterLogEntry_ValidationError(t *testing.T) {
	iterPath := filepath.Join(t.TempDir(), "iter-1.yaml")
	if err := writeIterLogEntry(iterPath, &iterLogEntry{}); err == nil {
		t.Fatal("expected schema validation error for empty entry, got nil")
	}
}

// TestFindReaderByHarness_NoMatch drives the no-match `return nil` branch.
func TestFindReaderByHarness_NoMatch(t *testing.T) {
	if r := findReaderByHarness("definitely-not-a-real-harness-xyz"); r != nil {
		t.Errorf("expected nil for unknown harness, got %v", r)
	}
}

func TestLoadIterLogDocument_V1Migration(t *testing.T) {
	v1 := []byte(`schema_version: 1
iteration: 5
date: "2026-04-30"
wave: w
task_id: t1
commit: abc
files_changed: 3
lines_added: 10
lines_removed: 2
first_commit: true
item: thing
summary: did it
scope_note: ok
feedback_goal: cover
retries: 0
tests_added: 1
tests_total_pass: 1
self_assessment:
  read_loop_state: true
  one_item_only: true
  committed_after_tests: true
  aligned_with_canonical_tasks: true
  persisted_via_workflow_commands: true
  stayed_under_10_files: true
  no_destructive_commands: true
`)
	got, err := loadIterLogDocument(v1)
	if err != nil {
		t.Fatalf("loadIterLogDocument(v1): %v", err)
	}
	if got.SchemaVersion != 2 {
		t.Errorf("expected migration to schema_version=2, got %d", got.SchemaVersion)
	}
	if got.Iteration != 5 {
		t.Errorf("expected iteration=5, got %d", got.Iteration)
	}
}

func TestLoadIterLogDocument_V2(t *testing.T) {
	v2 := []byte(`schema_version: 2
iteration: 7
date: "2026-04-30"
wave: w
task_id: t1
commit: abc
impl:
  feedback_goal: cover
`)
	got, err := loadIterLogDocument(v2)
	if err != nil {
		t.Fatalf("loadIterLogDocument(v2): %v", err)
	}
	if got.SchemaVersion != 2 || got.Iteration != 7 {
		t.Errorf("unexpected entry: %+v", got)
	}
	if got.Verifiers == nil {
		t.Error("expected non-nil verifiers slice")
	}
}

func TestMigrateIterLogV1Legacy_BasicFields(t *testing.T) {
	v1 := &iterLogV1Legacy{
		SchemaVersion: 1,
		Iteration:     3,
		Date:          "2026-04-30",
		Wave:          "w",
		TaskID:        "t1",
		Item:          "item",
		Summary:       "did it",
	}
	got := migrateIterLogV1Legacy(v1)
	if got.SchemaVersion != 2 {
		t.Errorf("schema=%d, want 2", got.SchemaVersion)
	}
	if got.Impl.Item != "item" || got.Impl.Summary != "did it" {
		t.Errorf("expected fields migrated to Impl block, got %+v", got.Impl)
	}
	if got.Verifiers == nil {
		t.Error("verifiers should be initialized")
	}
}

// Invalid verifier type stem rejected by verificationResultFilePath.
func TestUpsertVerifierIterLog_InvalidVerifierType(t *testing.T) {
	dst := &iterLogEntry{}
	err := upsertVerifierIterLog(dst, t.TempDir(), "task-1", "BAD-TYPE")
	if err == nil {
		t.Fatal("expected error for invalid verifier type")
	}
}

// Verifier result YAML missing on disk — ReadFile error path.
func TestUpsertVerifierIterLog_ResultFileMissing(t *testing.T) {
	dst := &iterLogEntry{}
	err := upsertVerifierIterLog(dst, t.TempDir(), "task-1", "merge-back")
	if err == nil || !strings.Contains(err.Error(), "read verifier result") {
		t.Fatalf("expected read error, got %v", err)
	}
}

// Verifier result YAML present but malformed — YAML parse error path.
func TestUpsertVerifierIterLog_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	resultDir := filepath.Join(repo, ".agents", "active", "verification", "task-1")
	if err := osMkdirAll(resultDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(filepath.Join(resultDir, "merge-back.result.yaml"), []byte(":\n - oops"), 0644); err != nil {
		t.Fatal(err)
	}

	dst := &iterLogEntry{}
	err := upsertVerifierIterLog(dst, repo, "task-1", "merge-back")
	if err == nil || !strings.Contains(err.Error(), "parse verifier result") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// Verifier result fails schema validation (missing required fields).
func TestUpsertVerifierIterLog_SchemaInvalid(t *testing.T) {
	repo := t.TempDir()
	resultDir := filepath.Join(repo, ".agents", "active", "verification", "task-1")
	if err := osMkdirAll(resultDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := osWriteFile(filepath.Join(resultDir, "merge-back.result.yaml"), []byte("task_id: t1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dst := &iterLogEntry{}
	err := upsertVerifierIterLog(dst, repo, "task-1", "merge-back")
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected schema invalid error, got %v", err)
	}
}

// Happy path: append a new verifier entry into an empty dst.
func TestUpsertVerifierIterLog_AppendNew(t *testing.T) {
	repo := t.TempDir()
	doc := newValidVerificationResultDoc()
	if err := writeVerificationResultYAML(repo, doc); err != nil {
		t.Fatal(err)
	}

	dst := &iterLogEntry{}
	if err := upsertVerifierIterLog(dst, repo, doc.TaskID, doc.VerifierType); err != nil {
		t.Fatalf("upsertVerifierIterLog: %v", err)
	}
	if len(dst.Verifiers) != 1 || dst.Verifiers[0].Type != "merge-back" {
		t.Fatalf("verifier not appended: %+v", dst.Verifiers)
	}
	if !dst.Verifiers[0].GatePassed {
		t.Errorf("expected GatePassed=true for status=pass")
	}
}

// Replace path: an existing verifier of the same type is overwritten,
// preserving carried-over fields (TestsAdded, ScenarioTags, etc.).
func TestUpsertVerifierIterLog_ReplaceExisting(t *testing.T) {
	repo := t.TempDir()
	doc := newValidVerificationResultDoc()
	if err := writeVerificationResultYAML(repo, doc); err != nil {
		t.Fatal(err)
	}

	dst := &iterLogEntry{
		Verifiers: []iterLogVerifierEntry{
			{
				Type:           "merge-back",
				Status:         "fail",
				TestsAdded:     5,
				TestsTotalPass: 3,
				ScenarioTags:   []string{"smoke"},
				Retries:        1,
			},
			{
				Type:   "lint",
				Status: "pass",
			},
		},
	}
	if err := upsertVerifierIterLog(dst, repo, doc.TaskID, doc.VerifierType); err != nil {
		t.Fatalf("upsertVerifierIterLog: %v", err)
	}
	if len(dst.Verifiers) != 2 {
		t.Fatalf("expected 2 verifiers, got %d", len(dst.Verifiers))
	}
	got := dst.Verifiers[0]
	if got.Status != "pass" {
		t.Errorf("expected replaced status=pass, got %q", got.Status)
	}
	if got.TestsAdded != 5 {
		t.Errorf("expected TestsAdded preserved as 5, got %d", got.TestsAdded)
	}
	if got.Retries != 1 {
		t.Errorf("expected Retries preserved as 1, got %d", got.Retries)
	}
	if len(got.ScenarioTags) != 1 || got.ScenarioTags[0] != "smoke" {
		t.Errorf("expected ScenarioTags preserved, got %v", got.ScenarioTags)
	}
}

func TestLoadPrevCheckpointAt_MissingReturnsEmpty(t *testing.T) {
	got := loadPrevCheckpointAt(t.TempDir(), 2)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestLoadPrevCheckpointAt_NLessThanOrEqual1(t *testing.T) {
	got := loadPrevCheckpointAt(t.TempDir(), 1)
	if got != "" {
		t.Fatalf("expected empty for n<=1, got %q", got)
	}
}

func TestLoadPrevCheckpointAt_HappyPath(t *testing.T) {
	dir := t.TempDir()
	prev := filepath.Join(dir, "iter-1.yaml")
	if err := os.WriteFile(prev, []byte("checkpoint_at: \"2026-05-12T10:00:00Z\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := loadPrevCheckpointAt(dir, 2)
	if got != "2026-05-12T10:00:00Z" {
		t.Fatalf("expected timestamp, got %q", got)
	}
}

func TestLoadPrevCheckpointAt_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "iter-1.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	got := loadPrevCheckpointAt(dir, 2)
	if got != "" {
		t.Fatalf("expected empty on malformed YAML, got %q", got)
	}
}

func TestFirstReadableDelegationContract_NoDir(t *testing.T) {
	got := firstReadableDelegationContract(t.TempDir())
	if got != nil {
		t.Fatalf("expected nil for missing delegation dir, got %+v", got)
	}
}

func TestFirstReadableDelegationContract_SkipsInactive(t *testing.T) {
	repo := t.TempDir()

	saveTestDelegationContract(t, repo, "task-c", "plan-c", "deleg-c")

	c, err := loadDelegationContract(repo, "task-c")
	if err != nil {
		t.Fatal(err)
	}
	c.Status = "completed"
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}
	got := firstReadableDelegationContract(repo)
	if got != nil {
		t.Fatalf("expected nil (completed skipped), got %+v", got)
	}
}

func TestMergeReviewIterLog_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	taskID := "task-mr"
	dir := filepath.Join(repo, ".agents", "active", "verification", taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review-decision.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	entry := &iterLogEntry{}
	err := mergeReviewIterLog(entry, repo, taskID)
	if err == nil || !strings.Contains(err.Error(), "parse review decision") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestMergeReviewIterLog_ReadError(t *testing.T) {
	repo := t.TempDir()
	taskID := "task-rr"
	dir := filepath.Join(repo, ".agents", "active", "verification", taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "review-decision.yaml")
	if err := os.WriteFile(path, []byte("overall_decision: accept\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, path)
	entry := &iterLogEntry{}
	err := mergeReviewIterLog(entry, repo, taskID)
	if err == nil || !strings.Contains(err.Error(), "read review decision") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestLoadOrInitIterLogEntry_UnsupportedSchemaVersion(t *testing.T) {
	tmp := t.TempDir()
	iterPath := filepath.Join(tmp, "iter-1.yaml")

	if err := os.WriteFile(iterPath, []byte("schema_version: 99\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrInitIterLogEntry(iterPath, "goal", "t1")
	if err == nil || !strings.Contains(err.Error(), "unsupported schema_version") {
		t.Fatalf("expected schema_version error, got %v", err)
	}
}

func TestLoadOrInitIterLogEntry_ReadError(t *testing.T) {
	tmp := t.TempDir()
	iterPath := filepath.Join(tmp, "iter-1.yaml")
	if err := os.WriteFile(iterPath, []byte("schema_version: 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, iterPath)
	_, err := loadOrInitIterLogEntry(iterPath, "goal", "t1")
	if err == nil || !strings.Contains(err.Error(), "read existing iteration log") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestLoadOrInitIterLogEntry_LoadsV2(t *testing.T) {
	tmp := t.TempDir()
	iterPath := filepath.Join(tmp, "iter-1.yaml")
	if err := os.WriteFile(iterPath, []byte("schema_version: 2\niteration: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	entry, err := loadOrInitIterLogEntry(iterPath, "goal", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.SchemaVersion != 2 {
		t.Fatalf("expected v2, got %d", entry.SchemaVersion)
	}
}

func TestLoadOrInitIterLogEntry_InitFromMissing(t *testing.T) {
	tmp := t.TempDir()
	entry, err := loadOrInitIterLogEntry(filepath.Join(tmp, "iter-1.yaml"), "goal", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.SchemaVersion != 2 {
		t.Fatalf("expected v2 init, got %d", entry.SchemaVersion)
	}
	if entry.Impl.FeedbackGoal != "goal" {
		t.Fatalf("expected feedback goal set, got %q", entry.Impl.FeedbackGoal)
	}
}

func TestApplyIterLogRole_EmptyRole(t *testing.T) {
	entry := &iterLogEntry{SchemaVersion: 2}
	if err := applyIterLogRole(entry, "", "", "goal", t.TempDir(), "t1", nil); err != nil {
		t.Fatal(err)
	}
	if entry.Impl.FeedbackGoal != "goal" {
		t.Fatalf("expected goal applied for empty role, got %q", entry.Impl.FeedbackGoal)
	}
}

func TestApplyIterLogRole_ImplRoleSetsBlock(t *testing.T) {
	entry := &iterLogEntry{SchemaVersion: 2}
	c := &DelegationContract{ID: "d1", ParentTaskID: "t1"}
	if err := applyIterLogRole(entry, "impl", "", "g", t.TempDir(), "t1", c); err != nil {
		t.Fatal(err)
	}
}

func TestWriteIterLogEntry_Happy(t *testing.T) {
	tmp := t.TempDir()
	iterPath := filepath.Join(tmp, "iter-1.yaml")
	entry := newValidIterLogEntry()
	if err := writeIterLogEntry(iterPath, entry); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "iteration: 1") {
		t.Fatalf("expected file content, got %s", string(data))
	}
}

func TestScanActiveDelegationContract_None(t *testing.T) {
	wave, tid := scanActiveDelegationContract(t.TempDir())
	if wave != "" || tid != "" {
		t.Fatalf("expected empty values, got wave=%q tid=%q", wave, tid)
	}
}

func TestScanActiveDelegationContract_Active(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-act", "plan-act", "d-act")
	wave, tid := scanActiveDelegationContract(repo)
	if wave != "plan-act" || tid != "task-act" {
		t.Fatalf("expected plan-act/task-act, got %q/%q", wave, tid)
	}
}

func TestRunWorkflowCheckpointLogToIter_NoIter(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)

	if err := runWorkflowCheckpoint("hello", "pass", "summary"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
}
