package harness

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/runner"
)

// commitRepo inits a git repo with one committed file and returns its path — a
// clean working tree the change detector should report as unmodified.
func commitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), "hi\n")
	if _, err := wt.Add("a.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	sig := &object.Signature{Name: "T", Email: "t@e.com", When: time.Now()}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return dir
}

// authHarness builds a unit harness with the solution gate pinned to
// producedSolution, for direct detectAuthFailure unit tests.
func authHarness(t *testing.T, producedSolution bool) *Harness {
	t.Helper()
	h := unitHarness(t,
		&fakeSandbox{inst: fakeInstance(t, "r1")},
		&runner.FakeRunner{},
		&fakeVerifier{lang: eval.LanguageGo},
		fakeGenerator{lang: eval.LanguageGo, spec: validSpec()},
	)
	h.producedSolution = func(string) bool { return producedSolution }
	return h
}

// ---- authStartupSignature -----------------------------------------------------

func TestAuthStartupSignature(t *testing.T) {
	if _, ok := authStartupSignature([]byte("Error: Not logged in")); !ok {
		t.Error("narrow startup signature should match")
	}
	if _, ok := authStartupSignature([]byte("HTTP 401 unauthorized: authentication failed")); ok {
		t.Error("bare 'unauthorized'/'authentication failed' must NOT match (narrowed)")
	}
	if _, ok := authStartupSignature(nil); ok {
		t.Error("empty stderr must not match")
	}
}

// ---- detectWorktreeChanges ----------------------------------------------------

func TestDetectWorktreeChanges_NonGit(t *testing.T) {
	if !detectWorktreeChanges(t.TempDir()) {
		t.Error("non-git dir must fail-safe to true (assume a solution was produced)")
	}
}

func TestDetectWorktreeChanges_Clean(t *testing.T) {
	if detectWorktreeChanges(commitRepo(t)) {
		t.Error("a clean committed repo must report no changes")
	}
}

func TestDetectWorktreeChanges_Dirty(t *testing.T) {
	dir := commitRepo(t)
	writeFile(t, filepath.Join(dir, "new.txt"), "solution\n")
	if !detectWorktreeChanges(dir) {
		t.Error("an untracked file must report a working-tree change")
	}
}

func TestDetectWorktreeChanges_BareRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, true); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	if !detectWorktreeChanges(dir) {
		t.Error("a bare repo (no worktree) must fail-safe to true")
	}
}

func TestDetectWorktreeChanges_CorruptIndex(t *testing.T) {
	dir := commitRepo(t)
	writeFile(t, filepath.Join(dir, ".git", "index"), "not-a-valid-git-index")
	if !detectWorktreeChanges(dir) {
		t.Error("an unreadable index (Status errors) must fail-safe to true")
	}
}

// ---- detectAuthFailure --------------------------------------------------------

func TestDetectAuthFailure_NotClassified(t *testing.T) {
	inst := fakeInstance(t, "r1")
	h := authHarness(t, false)
	if h.detectAuthFailure(inst, runner.Result{ExitCode: 0, Stderr: []byte("not logged in")}) != nil {
		t.Error("exit 0 must not classify as auth")
	}
	if h.detectAuthFailure(inst, runner.Result{ExitCode: 1, Stderr: []byte("compile error")}) != nil {
		t.Error("a non-zero exit with no startup signature must not classify")
	}
}

func TestDetectAuthFailure_AuthWhenNoSolution(t *testing.T) {
	se := authHarness(t, false).detectAuthFailure(
		fakeInstance(t, "r1"),
		runner.Result{ExitCode: 1, Stderr: []byte("not logged in"), Telemetry: runner.AgentTelemetry{Harness: "claude-code"}},
	)
	if se == nil || se.Reason != runner.ReasonAuth || se.Agent != "claude-code" {
		t.Fatalf("want auth AgentStartError for claude-code, got %+v", se)
	}
}

func TestDetectAuthFailure_GatedBySolution(t *testing.T) {
	if authHarness(t, true).detectAuthFailure(
		fakeInstance(t, "r1"),
		runner.Result{ExitCode: 1, Stderr: []byte("not logged in")},
	) != nil {
		t.Error("a produced solution must gate OFF the auth-abort (scored instead)")
	}
}
