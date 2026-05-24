package workflow

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// Happy-path start-task: flip status to active, set focus, derive scope
// (the derive-scope call writes a placeholder sidecar since the test
// project has no real source for the derivation), fire workflow commit.
// iterationCloseCommit is stubbed; the existing TestExecGitEndToEnd
// covers the real-git surface.
func TestStartTaskOrchestratesChain(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	commitCalls := 0
	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { commitCalls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	var buf bytes.Buffer
	err := runWorkflowStartTask(&buf, startTaskOpts{
		planID:        planID,
		taskID:        taskID,
		noDeriveScope: true, // derive-scope needs real code-graph fixtures; skip in unit test
	})
	if err != nil {
		t.Fatalf("runWorkflowStartTask: %v\n%s", err, buf.String())
	}
	if commitCalls != 1 {
		t.Errorf("commit calls = %d, want 1", commitCalls)
	}
	out := buf.String()
	for _, frag := range []string{"start-task " + planID + "/" + taskID, "plan activated", "workflow state committed"} {
		if !strings.Contains(out, frag) {
			t.Errorf("summary missing %q in:\n%s", frag, out)
		}
	}
}

// --no-commit skips the workflow commit step. --no-derive-scope skips
// the sidecar derivation. Both flags reflected in the rendered summary.
func TestStartTaskNoCommitNoDeriveScope(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	commitCalls := 0
	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { commitCalls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	var buf bytes.Buffer
	if err := runWorkflowStartTask(&buf, startTaskOpts{
		planID: planID, taskID: taskID, noDeriveScope: true, noCommit: true,
	}); err != nil {
		t.Fatalf("runWorkflowStartTask: %v", err)
	}
	if commitCalls != 0 {
		t.Errorf("commit fired despite --no-commit: calls=%d", commitCalls)
	}
	out := buf.String()
	for _, frag := range []string{"scope derivation skipped", "NOT committed"} {
		if !strings.Contains(out, frag) {
			t.Errorf("missing %q in summary:\n%s", frag, out)
		}
	}
}

// --json emits the structured startTaskResult with every step's outcome
// surfaced as a boolean field. Callers parse this directly.
func TestStartTaskJSONOutput(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })
	priorJSON := deps.Flags.JSON
	jsonOn := true
	deps.Flags.JSON = func() bool { return jsonOn }
	t.Cleanup(func() { deps.Flags.JSON = priorJSON })

	var buf bytes.Buffer
	if err := runWorkflowStartTask(&buf, startTaskOpts{
		planID: planID, taskID: taskID, noDeriveScope: true,
	}); err != nil {
		t.Fatalf("runWorkflowStartTask: %v\n%s", err, buf.String())
	}
	var got startTaskResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got.PlanID != planID || got.TaskID != taskID {
		t.Errorf("plan/task wrong: %+v", got)
	}
	if !got.ActivatedPlan || !got.FocusedTask {
		t.Error("ActivatedPlan / FocusedTask should both be true")
	}
	if got.DerivedScope {
		t.Error("DerivedScope = true with --no-derive-scope, want false")
	}
	if !got.WorkflowCommit {
		t.Error("WorkflowCommit = false, want true (default no-commit=false)")
	}
}

// Drive the cobra subcommand through Execute so newWorkflowStartTaskCmd's
// RunE closure is covered. --task required-flag check + positional plan
// ID validation come along for free.
func TestStartTaskSubcommandExecute(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	cmd := newWorkflowStartTaskCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{planID, "--task", taskID, "--no-derive-scope", "--no-commit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "start-task") {
		t.Errorf("summary not rendered: %s", buf.String())
	}
}
