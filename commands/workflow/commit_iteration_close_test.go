package workflow

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedSimplePlanForAdvance creates a minimal plan with a single pending task
// so runWorkflowAdvance has something to flip. Keeps the iteration-close
// tests self-contained — they only need a target the advance command will
// accept, not the full delegation surface.
func seedSimplePlanForAdvance(t *testing.T, repo, planID, taskID string) {
	t.Helper()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", planID)
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	planYAML := []byte("schema_version: 1\nid: " + planID + "\ntitle: t\nstatus: active\n")
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	tasksYAML := []byte("schema_version: 1\nplan_id: " + planID +
		"\ntasks:\n  - id: " + taskID + "\n    title: t\n    status: pending\n    owner: dot-agents\n    write_scope: []\n    verification_required: false\n")
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), tasksYAML, 0o644); err != nil {
		t.Fatal(err)
	}
}

// --commit-state on advance fires iterationCloseCommit after the status
// flip succeeds. Without the flag, no commit fires — the existing
// behaviour is preserved for callers that have not opted in.
func TestAdvanceCommitStateFiresClosePathCommit(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	seedSimplePlanForAdvance(t, repo, "sample-plan", "t1")

	// Stub the close-path commit so this test does not need a real git
	// invocation; we only care that the hook fires on success and skips
	// when the flag is absent.
	var calls int
	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error {
		calls++
		return nil
	}
	t.Cleanup(func() { iterationCloseCommit = prior })

	// Without --commit-state: zero calls.
	cmd := newWorkflowAdvanceCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sample-plan", "--task", "t1", "--status", "in_progress"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("advance (no commit-state): %v", err)
	}
	if calls != 0 {
		t.Errorf("close-path commit fired without --commit-state flag: calls=%d", calls)
	}

	// With --commit-state: one call.
	cmd2 := newWorkflowAdvanceCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetErr(&bytes.Buffer{})
	cmd2.SetArgs([]string{"sample-plan", "--task", "t1", "--status", "completed", "--commit-state"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("advance --commit-state: %v", err)
	}
	if calls != 1 {
		t.Errorf("close-path commit calls = %d, want 1", calls)
	}
}

// If the core advance fails, the close-path commit is NOT fired — atomicity
// means commit only on success. Otherwise we would persist a state commit
// that does not match a successful flip.
func TestAdvanceCommitStateSkippedOnAdvanceFailure(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	// Intentionally don't seed a plan so runWorkflowAdvance errors on
	// "plan not found" before reaching the close-path hook.

	calls := 0
	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { calls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	cmd := newWorkflowAdvanceCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"no-such-plan", "--task", "t1", "--status", "completed", "--commit-state"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected advance error on missing plan, got nil")
	}
	if calls != 0 {
		t.Errorf("close-path commit fired despite advance failure: calls=%d", calls)
	}
}

// If the core advance succeeds but the close-path commit returns an error,
// the error propagates so the operator sees it instead of a silent partial
// success.
func TestAdvanceCommitStateSurfacesCommitError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	seedSimplePlanForAdvance(t, repo, "sample-plan", "t1")

	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { return errors.New("commit boom") }
	t.Cleanup(func() { iterationCloseCommit = prior })

	cmd := newWorkflowAdvanceCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sample-plan", "--task", "t1", "--status", "completed", "--commit-state"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected commit error to propagate, got nil")
	}
	if !strings.Contains(err.Error(), "commit boom") {
		t.Errorf("wrong error wrapped: %v", err)
	}
}

// Both advance and merge-back expose the --commit-state flag. The flag's
// wire-up (default false, bool type) is the cobra surface that the close
// flow / client commands depend on; pin it so a rename or type change
// becomes a test failure immediately.
func TestCommitStateFlagWiredOnAdvanceAndMergeBack(t *testing.T) {
	for _, c := range []struct {
		name string
		cmd  string
		new  func() interface{ Flags() interface{} }
	}{} {
		_ = c
	}
	adv := newWorkflowAdvanceCmd()
	mb := newWorkflowMergeBackCmd()
	for _, cmd := range []struct {
		name string
		f    interface {
			Lookup(string) interface{}
		}
	}{} {
		_ = cmd
	}
	if f := adv.Flags().Lookup("commit-state"); f == nil {
		t.Error("advance: --commit-state flag missing")
	} else if f.Value.Type() != "bool" || f.DefValue != "false" {
		t.Errorf("advance --commit-state: type=%s default=%s, want bool/false", f.Value.Type(), f.DefValue)
	}
	if f := mb.Flags().Lookup("commit-state"); f == nil {
		t.Error("merge-back: --commit-state flag missing")
	} else if f.Value.Type() != "bool" || f.DefValue != "false" {
		t.Errorf("merge-back --commit-state: type=%s default=%s, want bool/false", f.Value.Type(), f.DefValue)
	}
}

// iterationCloseCommit's default value is wired to runWorkflowCommit with
// execGit{}, so out-of-band callers (the iteration-close skill,
// workflow-client-commands close-task) get the same close-path behaviour
// without re-implementing the integration.
func TestIterationCloseCommitDefaultPath(t *testing.T) {
	if iterationCloseCommit == nil {
		t.Fatal("iterationCloseCommit default value is nil")
	}
}
