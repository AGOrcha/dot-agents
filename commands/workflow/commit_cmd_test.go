package workflow

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
)

// fakeGit captures every call so tests can assert on the staging set and the
// generated message without spawning a real git process. Status returns the
// structured []StatusEntry shape gogitImpl produces in production, so tests
// fixture synthetic entries directly (the porcelain v2 parser is exercised
// separately by TestParseStatus*).
type fakeGit struct {
	status        []StatusEntry
	statusErr     error
	addedPaths    []string
	addErr        error
	commitMessage string
	commitErr     error
	addCalls      int
	commitCalls   int
}

func (g *fakeGit) Status() ([]StatusEntry, error) { return g.status, g.statusErr }
func (g *fakeGit) AddPaths(paths []string) error {
	g.addCalls++
	g.addedPaths = paths
	return g.addErr
}
func (g *fakeGit) Commit(message string) error {
	g.commitCalls++
	g.commitMessage = message
	return g.commitErr
}

// Empty status (clean worktree) is the idempotent no-op case: the command
// prints a status line, makes no calls to add/commit, and returns nil.
// "Second run does nothing" rests on this.
func TestCommitNoOpWhenClean(t *testing.T) {
	g := &fakeGit{status: nil}
	var buf bytes.Buffer
	if err := runWorkflowCommit(&buf, g, false, nil); err != nil {
		t.Fatalf("runWorkflowCommit: %v", err)
	}
	if g.addCalls != 0 || g.commitCalls != 0 {
		t.Errorf("add=%d commit=%d, want 0/0", g.addCalls, g.commitCalls)
	}
	if !strings.Contains(buf.String(), "nothing to stage") {
		t.Errorf("missing no-op status line in: %q", buf.String())
	}
}

// Managed-root changes are derived → staged → committed; the generated
// message names the workflow-state identity and enumerates the exact path
// set so reviewers can verify the "never -A" rule held.
func TestCommitStagesAndCommitsManagedRootChanges(t *testing.T) {
	g := &fakeGit{
		status: []StatusEntry{
			{Path: ".agents/workflow/plans/x/PLAN.yaml", XY: ".M"},
			{Path: ".agents/history/y/PLAN.yaml", XY: ".M"},
		},
	}
	var buf bytes.Buffer
	if err := runWorkflowCommit(&buf, g, false, nil); err != nil {
		t.Fatalf("runWorkflowCommit: %v", err)
	}
	want := []string{
		".agents/history/y/PLAN.yaml",
		".agents/workflow/plans/x/PLAN.yaml",
	}
	if !reflect.DeepEqual(g.addedPaths, want) {
		t.Errorf("addedPaths = %v, want %v", g.addedPaths, want)
	}
	if g.commitCalls != 1 {
		t.Errorf("commitCalls = %d, want 1", g.commitCalls)
	}
	for _, frag := range []string{"workflow(state)", "Distinct from code commits", ".agents/workflow/plans/x/PLAN.yaml"} {
		if !strings.Contains(g.commitMessage, frag) {
			t.Errorf("commit message missing %q:\n%s", frag, g.commitMessage)
		}
	}
	if !strings.Contains(buf.String(), "staged 2 path(s)") {
		t.Errorf("missing summary line: %q", buf.String())
	}
}

// --include opts in a non-managed session-touched path (e.g. an iter-log
// file under .agents/active/). The opt-in is the "session-touched state
// paths" clause from the spec.
func TestCommitIncludeOptsInNonManagedPath(t *testing.T) {
	g := &fakeGit{
		status: []StatusEntry{
			{Path: ".agents/active/iteration-log/iter-7.yaml", XY: ".M"},
		},
	}
	var buf bytes.Buffer
	if err := runWorkflowCommit(&buf, g, false,
		[]string{".agents/active/iteration-log/iter-7.yaml"}); err != nil {
		t.Fatalf("runWorkflowCommit: %v", err)
	}
	if !reflect.DeepEqual(g.addedPaths, []string{".agents/active/iteration-log/iter-7.yaml"}) {
		t.Errorf("addedPaths = %v", g.addedPaths)
	}
}

// --dry-run prints the staging set and the generated message but makes no
// git calls. This is the spec's "prints the exact `git add` set and the
// commit message, makes no changes" requirement.
func TestCommitDryRunMakesNoMutations(t *testing.T) {
	g := &fakeGit{
		status: []StatusEntry{
			{Path: ".agents/workflow/plans/x/PLAN.yaml", XY: ".M"},
		},
	}
	var buf bytes.Buffer
	if err := runWorkflowCommit(&buf, g, true, nil); err != nil {
		t.Fatalf("runWorkflowCommit: %v", err)
	}
	if g.addCalls != 0 || g.commitCalls != 0 {
		t.Errorf("dry-run mutated: add=%d commit=%d", g.addCalls, g.commitCalls)
	}
	out := buf.String()
	for _, frag := range []string{"dry-run", ".agents/workflow/plans/x/PLAN.yaml", "with message", "workflow(state)"} {
		if !strings.Contains(out, frag) {
			t.Errorf("dry-run output missing %q:\n%s", frag, out)
		}
	}
}

// Submodule entries are excluded by DerivePathSet — the commit subcommand
// inherits that. End-to-end check that a submodule entry in the status
// feed never reaches AddPaths.
func TestCommitExcludesSubmodulePointers(t *testing.T) {
	g := &fakeGit{
		status: []StatusEntry{
			{Path: "vendor/some-sub", XY: ".M", Submodule: true},
			{Path: ".agents/workflow/plans/x/PLAN.yaml", XY: ".M"},
		},
	}
	if err := runWorkflowCommit(&bytes.Buffer{}, g, false, nil); err != nil {
		t.Fatalf("runWorkflowCommit: %v", err)
	}
	for _, p := range g.addedPaths {
		if strings.HasPrefix(p, "vendor/") {
			t.Errorf("submodule path leaked into staging: %v", g.addedPaths)
		}
	}
}

// git status errors propagate with a "workflow commit" prefix so log triage
// can find the source quickly.
func TestCommitSurfacesStatusError(t *testing.T) {
	g := &fakeGit{statusErr: errors.New("status boom")}
	err := runWorkflowCommit(&bytes.Buffer{}, g, false, nil)
	if err == nil || !strings.Contains(err.Error(), "workflow commit") {
		t.Errorf("err = %v, want wrapped workflow commit error", err)
	}
}

// Stage failures stop before commit and surface the underlying error.
func TestCommitSurfacesAddError(t *testing.T) {
	g := &fakeGit{
		status: []StatusEntry{{Path: ".agents/workflow/plans/x/PLAN.yaml", XY: ".M"}},
		addErr: errors.New("add boom"),
	}
	err := runWorkflowCommit(&bytes.Buffer{}, g, false, nil)
	if err == nil {
		t.Fatal("expected add error, got nil")
	}
	if g.commitCalls != 0 {
		t.Errorf("commit attempted after add failure: commitCalls=%d", g.commitCalls)
	}
}

// Commit failures surface with the staging already done — the caller can
// rerun to re-attempt the commit step alone.
func TestCommitSurfacesCommitError(t *testing.T) {
	g := &fakeGit{
		status:    []StatusEntry{{Path: ".agents/workflow/plans/x/PLAN.yaml", XY: ".M"}},
		commitErr: errors.New("commit boom"),
	}
	err := runWorkflowCommit(&bytes.Buffer{}, g, false, nil)
	if err == nil {
		t.Fatal("expected commit error, got nil")
	}
	if g.addCalls != 1 {
		t.Errorf("staging should have succeeded before commit: addCalls=%d", g.addCalls)
	}
}

// buildCommitMessage carries the workflow-state header line so a glance at
// `git log` separates the two flows; the per-path bullet list lets reviewers
// re-derive the "never -A" assertion.
func TestBuildCommitMessageShape(t *testing.T) {
	msg := buildCommitMessage([]string{"a/b", "c/d"})
	for _, line := range []string{
		"workflow(state) Sync canonical-store changes",
		"Distinct from code commits",
		"- a/b",
		"- c/d",
	} {
		if !strings.Contains(msg, line) {
			t.Errorf("message missing %q:\n%s", line, msg)
		}
	}
}

// indentMessage prefixes non-empty lines with two spaces; empty lines stay
// empty so the dry-run output reads cleanly.
func TestIndentMessagePreservesEmptyLines(t *testing.T) {
	got := indentMessage("a\n\nb\n")
	want := "  a\n\n  b\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// commitDisabledFromPrefs (the production resolver) returns "not disabled"
// when no project is set up — the safe default that prevents a silent skip
// on the very first run before any preferences exist.
func TestCommitDisabledFromPrefsNoProject(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	disabled, reason := commitDisabledFromPrefs()
	if disabled {
		t.Errorf("disabled = true outside any project, want false (reason=%q)", reason)
	}
}

// A real project that has not opted out reports "not disabled" — the common
// case for the vast majority of repos.
func TestCommitDisabledFromPrefsDefaultFalse(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)
	disabled, _ := commitDisabledFromPrefs()
	if disabled {
		t.Error("disabled = true with no commit.disable set, want false (default off)")
	}
}

// A corrupted preferences.local.yaml is a REAL read error (not "no prefs
// file"), so commitDisabledFromPrefs fails safe: skip the auto-commit
// (disabled=true) rather than silently falling back to "not disabled",
// which would run the commit flow even though the actual (unreadable)
// preference might be commit.disable=true. Surfaces the
// resolvePreferences-error branch.
func TestCommitDisabledFromPrefsHandlesCorruptedPrefs(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Chdir(repo)
	localPath := filepath.Join(agentsHome, "context", "workflow-proj", "preferences.local.yaml")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte("not: [valid yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	disabled, reason := commitDisabledFromPrefs()
	if !disabled {
		t.Error("disabled = false on corrupted prefs, want true (fail-safe: skip auto-commit)")
	}
	if !strings.Contains(reason, "unreadable") {
		t.Errorf("reason should explain the prefs were unreadable, got: %q", reason)
	}
}

// With a real project + commit.disable=true persisted via setLocalPreference,
// commitDisabledFromPrefs reports the opt-out and the human-readable reason.
func TestCommitDisabledFromPrefsReadsPrefs(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Chdir(repo)

	if err := setLocalPreference("workflow-proj", "commit.disable", "true"); err != nil {
		t.Fatalf("setLocalPreference: %v", err)
	}
	disabled, reason := commitDisabledFromPrefs()
	if !disabled {
		t.Fatal("disabled = false, want true after commit.disable=true")
	}
	if !strings.Contains(reason, "commit.disable=true") {
		t.Errorf("reason missing key name: %q", reason)
	}
}

// The opt-out short-circuit: when commitDisabled reports true, runWorkflow
// Commit prints a status line and returns nil without calling Status / Add
// Paths / Commit.
func TestCommitOptOutShortCircuit(t *testing.T) {
	prior := commitDisabled
	commitDisabled = func() (bool, string) { return true, "test opt-out" }
	t.Cleanup(func() { commitDisabled = prior })

	g := &fakeGit{
		status: []StatusEntry{{Path: ".agents/workflow/plans/x/PLAN.yaml", XY: ".M"}},
	}
	var buf bytes.Buffer
	if err := runWorkflowCommit(&buf, g, false, nil); err != nil {
		t.Fatalf("opt-out should be a no-op error-wise: %v", err)
	}
	if g.addCalls != 0 || g.commitCalls != 0 {
		t.Errorf("opt-out leaked downstream calls: add=%d commit=%d", g.addCalls, g.commitCalls)
	}
	out := buf.String()
	if !strings.Contains(out, "opt-out active") {
		t.Errorf("status line missing opt-out marker: %q", out)
	}
	if !strings.Contains(out, "test opt-out") {
		t.Errorf("status line missing reason: %q", out)
	}
}

// --- gogitImpl real-git tests --------------------------------------------

// newGogitImpl errors clearly when cwd is not inside a git worktree —
// downstream callers (the cobra closure, iteration-close hook) wrap this
// as "workflow commit: ..." so triage points at the cause.
func TestNewGogitImplOutsideRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := newGogitImpl()
	if err == nil {
		t.Fatal("expected open-repo error outside a worktree, got nil")
	}
	if !strings.Contains(err.Error(), "open git repo") {
		t.Errorf("error should mention the failing step: %v", err)
	}
}

// gogitImpl.AddPaths on an empty slice is a no-op — runWorkflowCommit
// short-circuits before calling it, but the wrapper still defensively
// guards so a future caller cannot fall through to "git add --" semantics.
func TestGogitImplAddPathsEmptyIsNoOp(t *testing.T) {
	gg := newGogitImplForTest(t)
	if err := gg.AddPaths(nil); err != nil {
		t.Errorf("AddPaths(nil) = %v, want nil", err)
	}
}

// AddPaths surfaces an error when go-git rejects the path set (e.g. a path
// not present in the worktree). go-git's wt.Add returns "entry not found"
// for a missing path; the wrapper wraps it with "git add <path>:" so log
// triage can pinpoint the file.
func TestGogitImplAddPathsErrorOnMissingPath(t *testing.T) {
	gg := newGogitImplForTest(t)
	err := gg.AddPaths([]string{"definitely-not-a-real-path-12345"})
	if err == nil {
		t.Fatal("expected go-git add error on missing path, got nil")
	}
	if !strings.Contains(err.Error(), "git add") {
		t.Errorf("error should mention the failing step: %v", err)
	}
}

// Commit on an empty staging surfaces go-git's ErrEmptyCommit through the
// wrapper. Mirrors the CLI's "nothing to commit" path.
func TestGogitImplCommitErrorWhenNothingStaged(t *testing.T) {
	gg := newGogitImplForTest(t)
	err := gg.Commit("test message\n")
	if err == nil {
		t.Fatal("expected commit error on empty staging, got nil")
	}
	if !strings.Contains(err.Error(), "git commit") {
		t.Errorf("error should mention the failing step: %v", err)
	}
}

// userIdentity returns a clear error when neither local nor global config
// carries user.name / user.email — the operator otherwise sees a garbled
// "author field is required" deep from go-git.
func TestGogitImplUserIdentityMissing(t *testing.T) {
	repo := gogitTestRepoNoIdentity(t)
	t.Chdir(repo)
	t.Setenv("HOME", t.TempDir()) // ensure global config is empty
	gg, err := newGogitImpl()
	if err != nil {
		t.Fatalf("newGogitImpl: %v", err)
	}
	if _, _, err := gg.userIdentity(); err == nil {
		t.Fatal("expected user-identity error, got nil")
	}
}

// Both user.name and user.email present in local config → userIdentity
// returns them via the early-return inside the scope loop on the first
// iteration. Confirms the happy path (line A in the loop body) is hit,
// distinct from the end-of-loop fallthrough.
func TestGogitImplUserIdentityLocal(t *testing.T) {
	gg := newGogitImplForTest(t)
	name, email, err := gg.userIdentity()
	if err != nil {
		t.Fatalf("userIdentity: %v", err)
	}
	if name == "" || email == "" {
		t.Errorf("expected non-empty name+email, got %q/%q", name, email)
	}
}

// The default iterationCloseCommit closure opens go-git and runs
// runWorkflowCommit — this is what advance/merge-back's --commit-state
// fires in production. Direct invocation in a clean test repo exercises
// the closure body without going through any other primitive.
func TestIterationCloseCommitDefaultRunsRealGit(t *testing.T) {
	gogitTestRepoWithCommit(t) // chdir handled below
	dir := gogitTestRepoWithCommit(t)
	t.Chdir(dir)
	var buf bytes.Buffer
	if err := iterationCloseCommit(&buf); err != nil {
		t.Fatalf("iterationCloseCommit: %v\noutput: %s", err, buf.String())
	}
	// Clean repo → no managed paths to stage → idempotent no-op.
	if !strings.Contains(buf.String(), "nothing to stage") {
		t.Errorf("expected no-op message, got: %s", buf.String())
	}
}

// Drive the cobra subcommand through Execute so newWorkflowCommitCmd's
// RunE closure is covered — including arg validation (NoArgs), flag
// parsing, and the wire-up of dryRun + includes. Uses --dry-run so the
// real go-git underneath only reads the worktree.
func TestNewWorkflowCommitCmdExecuteDryRun(t *testing.T) {
	gg := newGogitImplForTest(t)
	_ = gg // ensure cwd is set up; the cobra path calls newGogitImpl itself

	cmd := newWorkflowCommitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "nothing to stage") {
		t.Errorf("clean dry-run should say nothing to stage; got: %s", buf.String())
	}
}

// newWorkflowCommitCmd's RunE surfaces newGogitImpl errors via the
// "workflow commit: open git repo at ..." wrap when cwd is not a worktree.
func TestNewWorkflowCommitCmdExecuteOutsideRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := newWorkflowCommitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected workflow-commit error outside a worktree, got nil")
	}
	if !strings.Contains(err.Error(), "workflow commit") {
		t.Errorf("error not wrapped with the subcommand identity: %v", err)
	}
}

// The global -n/--dry-run is OR-merged into the local --dry-run so
// `da -n workflow commit` is honored without the subcommand flag. The test
// deps leave Flags.DryRun nil, so pointing it at a true accessor here also
// exercises the new non-nil branch in the RunE closure.
func TestNewWorkflowCommitCmdHonorsGlobalDryRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH (needed to bootstrap the fixture)")
	}
	dir := gogitTestRepoWithCommit(t)
	t.Chdir(dir)

	// Stage-eligible workflow-state change so a real commit *would* land
	// absent dry-run.
	planDir := filepath.Join(dir, ".agents", "workflow", "plans", "test-plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "PLAN.yaml"), []byte("id: test-plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	saved := deps.Flags.DryRun
	deps.Flags.DryRun = func() bool { return true }
	t.Cleanup(func() { deps.Flags.DryRun = saved })

	cmd := newWorkflowCommitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{}) // no local --dry-run; the global -n must carry it
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "dry-run") {
		t.Errorf("global -n should force the dry-run preview; got: %s", buf.String())
	}
	logOut, _ := exec.Command("git", "-C", dir, "log", "--format=%s", "-5").CombinedOutput()
	if strings.Contains(string(logOut), "workflow(state)") {
		t.Errorf("global dry-run still committed:\n%s", logOut)
	}
}

// End-to-end against a real throwaway repo: gogitImpl.Status + AddPaths +
// Commit land a workflow(state) commit on disk, and a second invocation
// is a clean no-op.
func TestGogitEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH (still needed to bootstrap the test fixture)")
	}
	dir := gogitTestRepoWithCommit(t)
	t.Chdir(dir)

	// Create a workflow-state change.
	planDir := filepath.Join(dir, ".agents", "workflow", "plans", "test-plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "PLAN.yaml"), []byte("id: test-plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gg, err := newGogitImpl()
	if err != nil {
		t.Fatalf("newGogitImpl: %v", err)
	}
	var buf bytes.Buffer
	if err := runWorkflowCommit(&buf, gg, false, nil); err != nil {
		t.Fatalf("runWorkflowCommit: %v\noutput: %s", err, buf.String())
	}

	logOut, err := exec.Command("git", "-C", dir, "log", "--format=%s", "-2").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, logOut)
	}
	if !strings.Contains(string(logOut), "workflow(state)") {
		t.Errorf("expected workflow(state) commit, got:\n%s", logOut)
	}

	// Second run is a clean no-op.
	gg2, err := newGogitImpl()
	if err != nil {
		t.Fatalf("newGogitImpl (rerun): %v", err)
	}
	buf.Reset()
	if err := runWorkflowCommit(&buf, gg2, false, nil); err != nil {
		t.Fatalf("idempotent rerun: %v\noutput: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "nothing to stage") {
		t.Errorf("second run should be no-op, got: %s", buf.String())
	}
}

// stubWorktree implements gogitWorktree so the per-method error branches
// in gogitImpl (Status / AddPaths / Commit) and statusToEntries
// (Submodules) become testable without standing up a corrupt real repo
// per branch. Each method returns the configured error or value.
type stubWorktree struct {
	status        git.Status
	statusErr     error
	addErr        error
	commitErr     error
	submodules    git.Submodules
	submodulesErr error
}

func (s *stubWorktree) Status() (git.Status, error)       { return s.status, s.statusErr }
func (s *stubWorktree) Add(string) (plumbing.Hash, error) { return plumbing.ZeroHash, s.addErr }
func (s *stubWorktree) Submodules() (git.Submodules, error) {
	return s.submodules, s.submodulesErr
}
func (s *stubWorktree) Commit(string, *git.CommitOptions) (plumbing.Hash, error) {
	return plumbing.ZeroHash, s.commitErr
}

// gogitImpl.Status wraps wt.Status errors with the "status: ..." prefix.
func TestGogitImplStatusErrorWrap(t *testing.T) {
	gg := &gogitImpl{wt: &stubWorktree{statusErr: errors.New("status boom")}}
	_, err := gg.Status()
	if err == nil || !strings.Contains(err.Error(), "status:") {
		t.Errorf("err = %v, want wrapped status error", err)
	}
}

// gogitImpl.AddPaths wraps wt.Add errors with the failing path.
func TestGogitImplAddPathsErrorWrap(t *testing.T) {
	gg := &gogitImpl{wt: &stubWorktree{addErr: errors.New("add boom")}}
	err := gg.AddPaths([]string{"a/b"})
	if err == nil || !strings.Contains(err.Error(), "git add a/b") {
		t.Errorf("err = %v, want path-qualified add error", err)
	}
}

// statusToEntries wraps wt.Submodules errors with the "submodules: ..."
// prefix. Trigger via stubWorktree without going through gogitImpl.Status.
func TestStatusToEntriesSubmodulesErrorWrap(t *testing.T) {
	wt := &stubWorktree{submodulesErr: errors.New("subs boom")}
	_, err := statusToEntries(wt, git.Status{})
	if err == nil || !strings.Contains(err.Error(), "submodules:") {
		t.Errorf("err = %v, want submodules-wrap", err)
	}
}

// statusToEntries converts a populated git.Status into per-path
// StatusEntry values, mapping Untracked and rename-source (Extra) fields.
// Drives the per-entry loop body via a stub with realistic content so
// the population statements (XY assembly, Untracked detection, OrigPath
// copy) are exercised.
func TestStatusToEntriesPopulatesFields(t *testing.T) {
	wt := &stubWorktree{}
	status := git.Status{
		".agents/workflow/plans/x/PLAN.yaml": {Staging: git.Modified, Worktree: git.Unmodified},
		".agents/active/iteration-log/iter-1.yaml": {
			Staging:  git.Unmodified,
			Worktree: git.Untracked,
		},
		".agents/history/y/PLAN.yaml": {Staging: git.Renamed, Worktree: git.Unmodified, Extra: ".agents/workflow/plans/y/PLAN.yaml"},
	}
	entries, err := statusToEntries(wt, status)
	if err != nil {
		t.Fatalf("statusToEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries len = %d, want 3", len(entries))
	}
	byPath := make(map[string]StatusEntry, len(entries))
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if e := byPath[".agents/active/iteration-log/iter-1.yaml"]; !e.Untracked {
		t.Errorf("expected Untracked=true for iter-1.yaml, got %+v", e)
	}
	if e := byPath[".agents/history/y/PLAN.yaml"]; e.OrigPath != ".agents/workflow/plans/y/PLAN.yaml" {
		t.Errorf("expected rename-source in OrigPath, got %+v", e)
	}
	if e := byPath[".agents/workflow/plans/x/PLAN.yaml"]; e.XY == "" {
		t.Errorf("expected XY populated, got %+v", e)
	}
}

// gogitImpl.Commit propagates userIdentity errors before reaching go-git.
// Exercises the early-return on missing user.name/user.email.
func TestGogitImplCommitErrorOnMissingIdentity(t *testing.T) {
	repo := gogitTestRepoNoIdentity(t)
	t.Chdir(repo)
	t.Setenv("HOME", t.TempDir())
	gg, err := newGogitImpl()
	if err != nil {
		t.Fatalf("newGogitImpl: %v", err)
	}
	err = gg.Commit("message")
	if err == nil {
		t.Fatal("expected userIdentity error to propagate from Commit, got nil")
	}
	if !strings.Contains(err.Error(), "user.name") {
		t.Errorf("error should mention user identity: %v", err)
	}
}

// --- test-helper fixtures ------------------------------------------------

// newGogitImplForTest builds a tiny throwaway git repo with user identity
// configured and chdirs into it, then returns the gogitImpl pointing at it.
// Centralises the boilerplate the AddPaths / Commit error tests share.
func newGogitImplForTest(t *testing.T) *gogitImpl {
	t.Helper()
	dir := gogitTestRepo(t)
	t.Chdir(dir)
	gg, err := newGogitImpl()
	if err != nil {
		t.Fatalf("newGogitImpl: %v", err)
	}
	return gg
}

// gogitTestRepo initialises an empty git repo with user.name / user.email
// set in local config so go-git's userIdentity lookup succeeds. Returns
// the worktree path.
func gogitTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH (still needed to bootstrap the test fixture)")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@e"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// gogitTestRepoNoIdentity is like gogitTestRepo but skips user.name /
// user.email so userIdentity has nothing to find.
func gogitTestRepoNoIdentity(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH (still needed to bootstrap the test fixture)")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

// gogitTestRepoWithCommit seeds a repo with one initial commit so HEAD
// exists. Required for the end-to-end test which then layers a workflow-
// state change on top and asserts a second commit lands.
func gogitTestRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := gogitTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDo := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE=2026-05-23T00:00:00Z", "GIT_COMMITTER_DATE=2026-05-23T00:00:00Z",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitDo("add", "README")
	gitDo("commit", "-q", "-m", "seed")
	return dir
}
