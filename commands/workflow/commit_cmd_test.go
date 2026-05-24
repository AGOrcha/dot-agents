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
)

// fakeGit captures every call so tests can assert on the staging set and the
// generated message without spawning a real git subprocess.
type fakeGit struct {
	status        []byte
	statusErr     error
	addedPaths    []string
	addErr        error
	commitMessage string
	commitErr     error
	addCalls      int
	commitCalls   int
}

func (g *fakeGit) Status() ([]byte, error) { return g.status, g.statusErr }
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
		status: []byte(
			"1 .M N... 100644 100644 100644 a a .agents/workflow/plans/x/PLAN.yaml\x00" +
				"1 .M N... 100644 100644 100644 a a .agents/history/y/PLAN.yaml\x00",
		),
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
		status: []byte(
			"1 .M N... 100644 100644 100644 a a .agents/active/iteration-log/iter-7.yaml\x00",
		),
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
		status: []byte("1 .M N... 100644 100644 100644 a a .agents/workflow/plans/x/PLAN.yaml\x00"),
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
// inherits that. End-to-end check that a submodule sub-state record in the
// status feed never reaches AddPaths.
func TestCommitExcludesSubmodulePointers(t *testing.T) {
	g := &fakeGit{
		status: []byte(
			"1 .M S.M. 160000 160000 160000 a a vendor/some-sub\x00" +
				"1 .M N... 100644 100644 100644 a a .agents/workflow/plans/x/PLAN.yaml\x00",
		),
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
		status: []byte("1 .M N... 100644 100644 100644 a a .agents/workflow/plans/x/PLAN.yaml\x00"),
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
		status:    []byte("1 .M N... 100644 100644 100644 a a .agents/workflow/plans/x/PLAN.yaml\x00"),
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

// Malformed porcelain v2 surfaces a parse error wrapped with the command
// identity (parse errors elsewhere are bare). Confirms the wrapper attaches
// "parse status" context.
func TestCommitWrapsParseErrorWithContext(t *testing.T) {
	g := &fakeGit{status: []byte("x not v2\x00")}
	err := runWorkflowCommit(&bytes.Buffer{}, g, false, nil)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse status") {
		t.Errorf("err missing parse-status context: %v", err)
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

// A corrupted preferences.local.yaml is treated as "could not read prefs →
// fall back to enabled" rather than crashing the command. Surfaces the
// resolvePreferences-error branch.
func TestCommitDisabledFromPrefsHandlesCorruptedPrefs(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Chdir(repo)
	// Write garbage to the local prefs path so resolvePreferences errors.
	// Path layout: $AGENTS_HOME/context/<project>/preferences.local.yaml.
	localPath := filepath.Join(agentsHome, "context", "workflow-proj", "preferences.local.yaml")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte("not: [valid yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	disabled, _ := commitDisabledFromPrefs()
	if disabled {
		t.Error("disabled = true on corrupted prefs, want false (safe default)")
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
// Paths / Commit. Verifies the spec's "documented no-op (status line states
// opt-out active)" requirement.
func TestCommitOptOutShortCircuit(t *testing.T) {
	prior := commitDisabled
	commitDisabled = func() (bool, string) { return true, "test opt-out" }
	t.Cleanup(func() { commitDisabled = prior })

	g := &fakeGit{
		status: []byte("1 .M N... 100644 100644 100644 a a .agents/workflow/plans/x/PLAN.yaml\x00"),
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

// AddPaths is a no-op on an empty path slice — runWorkflowCommit short-
// circuits before calling it, but the production wrapper still defensively
// checks the input so a future caller cannot accidentally invoke `git add --`
// (which errors with "Nothing specified, nothing added").
func TestExecGitAddPathsEmptyIsNoOp(t *testing.T) {
	if err := (execGit{}).AddPaths(nil); err != nil {
		t.Errorf("AddPaths(nil) = %v, want nil", err)
	}
}

// Drive the cobra subcommand through Execute so newWorkflowCommitCmd's RunE
// closure is covered — including arg validation (NoArgs), flag parsing,
// and the wire-up of dryRun + includes. Uses --dry-run so the real execGit
// underneath is harmless even though we're not in a git repo.
func TestNewWorkflowCommitCmdExecuteDryRun(t *testing.T) {
	dir := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	// Initialize a tiny repo so `git status` succeeds; dry-run still avoids
	// any actual staging/commit.
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@e"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(dir)

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

// End-to-end with the real execGit driver against a tiny throwaway git
// repository. Confirms `git status --porcelain=v2 -z` output shape matches
// what ParseStatus expects, and that AddPaths + Commit actually land a
// commit on disk. Skips when `git` is not on PATH (CI always has it; some
// constrained sandboxes might not).
func TestExecGitEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitDo := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_AUTHOR_DATE=2026-05-23T00:00:00Z", "GIT_COMMITTER_DATE=2026-05-23T00:00:00Z",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitDo("init", "-q")
	gitDo("config", "user.email", "test@example.com")
	gitDo("config", "user.name", "Test")
	gitDo("config", "commit.gpgsign", "false")
	// Seed an initial commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDo("add", "README")
	gitDo("commit", "-q", "-m", "seed")

	// Create a workflow-state change.
	planDir := filepath.Join(dir, ".agents", "workflow", "plans", "test-plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "PLAN.yaml"), []byte("id: test-plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run from the test repo dir.
	prior, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prior) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runWorkflowCommit(&buf, execGit{}, false, nil); err != nil {
		t.Fatalf("runWorkflowCommit: %v\noutput: %s", err, buf.String())
	}

	// HEAD now has a second commit with the workflow message and a clean
	// status afterwards proves idempotency on a second run.
	logOut, err := exec.Command("git", "-C", dir, "log", "--format=%s", "-2").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, logOut)
	}
	if !strings.Contains(string(logOut), "workflow(state)") {
		t.Errorf("expected workflow(state) commit, got:\n%s", logOut)
	}

	// Second run is a clean no-op.
	buf.Reset()
	if err := runWorkflowCommit(&buf, execGit{}, false, nil); err != nil {
		t.Fatalf("idempotent rerun: %v\noutput: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "nothing to stage") {
		t.Errorf("second run should be no-op, got: %s", buf.String())
	}
}
