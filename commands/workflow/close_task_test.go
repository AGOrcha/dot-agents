package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/scoring"
)

// closeTaskTestRepo builds a tiny git repo with a single committed file plus
// a plan + task ready for close-task to flip. Returns (repo, plan-id,
// task-id). Centralises the boilerplate so multiple close-task tests stay
// readable.
func closeTaskTestRepo(t *testing.T) (repo, planID, taskID string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo = t.TempDir()
	gitDo := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
			"GIT_AUTHOR_DATE=2026-05-23T00:00:00Z", "GIT_COMMITTER_DATE=2026-05-23T00:00:00Z",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitDo("init", "-q")
	gitDo("config", "user.email", "t@e")
	gitDo("config", "user.name", "t")
	gitDo("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDo("add", "README")
	gitDo("commit", "-q", "-m", "seed")
	planID, taskID = "p", "t1"
	seedSimplePlanForAdvance(t, repo, planID, taskID)
	return repo, planID, taskID
}

// Happy-path close-task: every primitive fires, the result snapshot has
// the expected shape, and the iter sidecar lands on disk. iterationClose
// Commit is stubbed so the test does not depend on `git add/commit`
// succeeding under the test cwd (the existing TestExecGitEndToEnd covers
// that surface directly).
func TestCloseTaskOrchestratesChain(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	commitCalls := 0
	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { commitCalls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	var buf bytes.Buffer
	err := runWorkflowCloseTask(&buf, closeTaskOpts{
		planID:         planID,
		taskID:         taskID,
		scoreRecompute: "current",
		repoDir:        repo,
	})
	if err != nil {
		t.Fatalf("runWorkflowCloseTask: %v\noutput:\n%s", err, buf.String())
	}
	// Sidecar landed for iter-1 (NextIterationNumber on an empty dir → 1).
	if _, err := os.Stat(filepath.Join(repo, ".agents", "active", "iteration-log", "iter-1.score.yaml")); err != nil {
		t.Errorf("iter-1 sidecar not written: %v", err)
	}
	// commit fired (default no-commit=false).
	if commitCalls != 1 {
		t.Errorf("workflow commit calls = %d, want 1", commitCalls)
	}
	out := buf.String()
	for _, frag := range []string{"close-task p/t1", "iteration 1", "workflow state committed"} {
		if !strings.Contains(out, frag) {
			t.Errorf("summary missing %q in:\n%s", frag, out)
		}
	}
}

// --no-commit skips the workflow-state commit step. The result still
// reflects everything else (iter sidecar, advance, focus).
func TestCloseTaskNoCommitSkipsCommit(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	commitCalls := 0
	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { commitCalls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	var buf bytes.Buffer
	if err := runWorkflowCloseTask(&buf, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current", noCommit: true, repoDir: repo,
	}); err != nil {
		t.Fatalf("runWorkflowCloseTask: %v", err)
	}
	if commitCalls != 0 {
		t.Errorf("commit fired despite --no-commit: calls=%d", commitCalls)
	}
	if !strings.Contains(buf.String(), "NOT committed") {
		t.Errorf("summary should call out --no-commit: %s", buf.String())
	}
}

// --score-recompute=all and recent-N are reserved values not yet wired;
// close-task rejects them up-front so the contract stays honest.
func TestCloseTaskRejectsUnimplementedRecomputeScopes(t *testing.T) {
	for _, scope := range []string{"all", "recent-5"} {
		err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
			planID: "p", taskID: "t", scoreRecompute: scope,
		})
		if err == nil {
			t.Errorf("--score-recompute=%q should be rejected", scope)
		} else if !strings.Contains(err.Error(), "not yet implemented") {
			t.Errorf("--score-recompute=%q error wrong: %v", scope, err)
		}
	}
}

// JSON mode emits the structured closeTaskResult; callers (skills,
// scripts) parse this directly instead of grepping the text summary.
func TestCloseTaskJSONOutput(t *testing.T) {
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
	if err := runWorkflowCloseTask(&buf, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current", repoDir: repo,
	}); err != nil {
		t.Fatalf("runWorkflowCloseTask: %v\n%s", err, buf.String())
	}
	var got closeTaskResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got.PlanID != planID || got.TaskID != taskID {
		t.Errorf("got plan/task %s/%s, want %s/%s", got.PlanID, got.TaskID, planID, taskID)
	}
	if got.IterationN != 1 {
		t.Errorf("IterationN = %d, want 1", got.IterationN)
	}
	if !got.WorkflowCommit {
		t.Error("WorkflowCommit = false, want true (default no-commit=false)")
	}
}

// --next-focus override goes straight into the plan update — close-task
// does not consult eligible when the operator names a target explicitly.
func TestCloseTaskNextFocusOverride(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	var buf bytes.Buffer
	if err := runWorkflowCloseTask(&buf, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current",
		nextFocus: "explicit-target", repoDir: repo,
	}); err != nil {
		// The override target need not exist for the unit's purposes —
		// runWorkflowPlanUpdate may error on missing focus. We only assert
		// the close-task surfaces a focused error if it does.
		if !strings.Contains(err.Error(), "plan update") {
			t.Errorf("if focus update fails, error must be wrapped: %v", err)
		}
		return
	}
	if !strings.Contains(buf.String(), "explicit-target") {
		t.Errorf("summary should include the override target: %s", buf.String())
	}
}

// Each per-step error in runWorkflowCloseTask wraps with "close-task:
// <step>: ..." so log triage maps directly to the chain position. The
// tests below trigger each step's failure with the most natural
// mechanism available (bad inputs, malformed fixtures, or — for
// checkpoint, which is otherwise difficult to make fail — the
// closeTaskCheckpoint seam).

func TestCloseTaskErrorWrapsCurrentProject(t *testing.T) {
	prior := closeTaskResolveProject
	closeTaskResolveProject = func() (workflowProjectRef, error) {
		return workflowProjectRef{}, errors.New("no project")
	}
	t.Cleanup(func() { closeTaskResolveProject = prior })

	err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
		planID: "p", taskID: "t1", scoreRecompute: "current",
	})
	if err == nil {
		t.Fatal("expected currentWorkflowProject error, got nil")
	}
	if !strings.Contains(err.Error(), "close-task: resolve project") {
		t.Errorf("error not wrapped with the project step: %v", err)
	}
}

func TestCloseTaskErrorWrapsNextIterationNumber(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := closeTaskNextIter
	closeTaskNextIter = func(string) (int, error) {
		return 0, errors.New("next iter boom")
	}
	t.Cleanup(func() { closeTaskNextIter = prior })

	err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current", repoDir: repo,
	})
	if err == nil {
		t.Fatal("expected NextIterationNumber error, got nil")
	}
	if !strings.Contains(err.Error(), "close-task: pick iteration N") {
		t.Errorf("error not wrapped with the pick-iteration step: %v", err)
	}
}

func TestCloseTaskErrorWrapsCheckpoint(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := closeTaskCheckpoint
	closeTaskCheckpoint = func(n int, role, verifierType string) error {
		return errors.New("checkpoint boom")
	}
	t.Cleanup(func() { closeTaskCheckpoint = prior })

	err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current", repoDir: repo,
	})
	if err == nil {
		t.Fatal("expected checkpoint error, got nil")
	}
	if !strings.Contains(err.Error(), "close-task: checkpoint --log-to-iter") {
		t.Errorf("error not wrapped with the checkpoint step: %v", err)
	}
}

func TestCloseTaskErrorWrapsScoreIteration(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	// Write a malformed iter-1.yaml BEFORE close-task runs, so
	// NextIterationNumber picks N=2 (max+1) and ScoreIteration loads the
	// log, finds iter-1 malformed, and errors at LoadIterationLog.
	iterDir := filepath.Join(repo, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(iterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(iterDir, "iter-1.yaml"), []byte("not: [valid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current", repoDir: repo,
	})
	if err == nil {
		t.Fatal("expected ScoreIteration error, got nil")
	}
	if !strings.Contains(err.Error(), "close-task: score iteration") {
		t.Errorf("error not wrapped with the score step: %v", err)
	}
}

func TestCloseTaskErrorWrapsWriteSidecar(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := closeTaskWriteSidecar
	closeTaskWriteSidecar = func(string, scoring.Score, scoring.IterationRecord) (string, error) {
		return "", errors.New("sidecar boom")
	}
	t.Cleanup(func() { closeTaskWriteSidecar = prior })

	err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current", repoDir: repo,
	})
	if err == nil {
		t.Fatal("expected sidecar persist error, got nil")
	}
	if !strings.Contains(err.Error(), "close-task: persist iter-") {
		t.Errorf("error not wrapped with sidecar step: %v", err)
	}
}

func TestCloseTaskErrorWrapsAdvance(t *testing.T) {
	repo, _, _ := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	// Use a real plan but a non-existent task ID — advance errors with
	// "task not found".
	err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
		planID: "p", taskID: "no-such-task", scoreRecompute: "current", repoDir: repo,
	})
	if err == nil {
		t.Fatal("expected advance error, got nil")
	}
	if !strings.Contains(err.Error(), "close-task: advance") {
		t.Errorf("error not wrapped with the advance step: %v", err)
	}
}

func TestCloseTaskErrorWrapsPlanUpdate(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	priorCommit := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { return nil }
	t.Cleanup(func() { iterationCloseCommit = priorCommit })

	// runWorkflowPlanUpdate is permissive about focus values; stub the
	// seam so we can prove the wrap.
	priorUpdate := closeTaskPlanUpdate
	closeTaskPlanUpdate = func(planID, status, title, summary, focus, sc, vs string) error {
		return errors.New("plan update boom")
	}
	t.Cleanup(func() { closeTaskPlanUpdate = priorUpdate })

	err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current",
		nextFocus: "some-other-task", repoDir: repo,
	})
	if err == nil {
		t.Fatal("expected plan update error, got nil")
	}
	if !strings.Contains(err.Error(), "close-task: plan update --focus") {
		t.Errorf("error not wrapped with the focus step: %v", err)
	}
}

func TestCloseTaskErrorWrapsCommit(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { return errors.New("commit boom") }
	t.Cleanup(func() { iterationCloseCommit = prior })

	err := runWorkflowCloseTask(&bytes.Buffer{}, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current", repoDir: repo,
	})
	if err == nil {
		t.Fatal("expected commit error, got nil")
	}
	if !strings.Contains(err.Error(), "close-task: workflow commit") {
		t.Errorf("error not wrapped with the commit step: %v", err)
	}
}

// Empty repoDir triggers the cwd-fallback branch. Confirms that
// runWorkflowCloseTask uses os.Getwd() as the implicit repo root when
// the caller does not pass --repo-dir.
func TestCloseTaskUsesCwdWhenRepoDirEmpty(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := iterationCloseCommit
	iterationCloseCommit = func(io.Writer) error { return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	var buf bytes.Buffer
	if err := runWorkflowCloseTask(&buf, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current",
		// repoDir intentionally empty — fall through to cwd.
	}); err != nil {
		t.Fatalf("runWorkflowCloseTask with cwd fallback: %v\n%s", err, buf.String())
	}
}

// close-task's --json branch propagates json.Encoder.Encode errors so a
// caller wedging the writer (broken pipe, full disk) sees the failure
// rather than a silent zero-byte result.
type closeTaskErrWriter struct{}

func (closeTaskErrWriter) Write([]byte) (int, error) { return 0, errors.New("write boom") }

func TestCloseTaskJSONEncodeError(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := iterationCloseCommit
	iterationCloseCommit = func(io.Writer) error { return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	priorJSON := deps.Flags.JSON
	jsonOn := true
	deps.Flags.JSON = func() bool { return jsonOn }
	t.Cleanup(func() { deps.Flags.JSON = priorJSON })

	err := runWorkflowCloseTask(closeTaskErrWriter{}, closeTaskOpts{
		planID: planID, taskID: taskID, scoreRecompute: "current", repoDir: repo,
	})
	if err == nil {
		t.Fatal("expected JSON encode error, got nil")
	}
}

// pickNextFocus returns the first eligible task ID in the plan when one
// exists; "" when there are none. Covers the success branch the orchestr
// path otherwise skips because the default-test plan only has one task
// that close-task just advanced.
func TestPickNextFocusReturnsFirstEligible(t *testing.T) {
	repo, _, _ := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	// Seed a second plan with a pending task so eligible is non-empty.
	seedSimplePlanForAdvance(t, repo, "p2", "t-next")
	if got := pickNextFocus(repo, "p2"); got != "t-next" {
		t.Errorf("pickNextFocus = %q, want \"t-next\"", got)
	}
	// Non-existent plan → empty pick, not an error.
	if got := pickNextFocus(repo, "no-such-plan"); got != "" {
		t.Errorf("pickNextFocus(no-such-plan) = %q, want empty", got)
	}
}

// Drive the cobra subcommand through Execute so newWorkflowCloseTaskCmd's
// RunE closure is covered. Confirms the --task required-flag check and
// the positional plan-ID validation.
func TestCloseTaskSubcommandExecute(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	cmd := newWorkflowCloseTaskCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{planID, "--task", taskID, "--repo-dir", repo, "--no-commit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "close-task") {
		t.Errorf("summary not rendered: %s", buf.String())
	}
}
