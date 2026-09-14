package codegraph

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Parity of the two changed-file helpers against real git.
//
// Every expectation here is ORACLED: the test runs the exact command upstream
// runs, decodes its output with upstream's own parsing rules, and asserts the
// go-git implementation returns the same set. Hand-written expectations were
// deliberately avoided — the failure mode these helpers exist to prevent is
// "a plausible set of paths that is not git's set", and a hand-reasoned
// expectation would encode the same mistake twice.
//
// The test executes git; `tools/execguard` governs shipping code, not test
// fixtures, and an oracle that did not run the real thing would not be one.

// gitOracleEnv pins identity and the settings that would otherwise make git's
// answer depend on the developer's global configuration.
var gitOracleEnv = []string{
	"GIT_AUTHOR_NAME=fixture",
	"GIT_AUTHOR_EMAIL=fixture@example.com",
	"GIT_COMMITTER_NAME=fixture",
	"GIT_COMMITTER_EMAIL=fixture@example.com",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_CONFIG_SYSTEM=/dev/null",
	"GIT_CONFIG_NOSYSTEM=1",
}

// TestReleaseChangedFilesMatchesGitDiff drives one repository through the
// cases that distinguish a correct base-vs-worktree diff from the plausible
// wrong answers: a rename (both paths), an add, a delete, an UNCOMMITTED
// edit, and a file changed after base and then reverted.
//
// The revert case is the one a `base...HEAD` implementation gets wrong: the
// file is in the commit range, so a range diff lists it, but git does not
// because the working tree matches base again.
func TestReleaseChangedFilesMatchesGitDiff(t *testing.T) {
	root := gitOracleRepo(t)

	// First commit: the baseline every case diffs against.
	gitOracleWrite(t, root, "keep.go", "package p\n\nfunc Keep() {}\n")
	gitOracleWrite(t, root, "moved.go", "package p\n\nfunc Moved() {}\n")
	gitOracleWrite(t, root, "removed.go", "package p\n\nfunc Removed() {}\n")
	gitOracleWrite(t, root, "reverted.go", "package p\n\nfunc Reverted() {}\n")
	gitOracleWrite(t, root, "edited.go", "package p\n\nfunc Edited() {}\n")
	gitOracleRun(t, root, "add", "-A")
	gitOracleRun(t, root, "commit", "-m", "baseline")

	// Second commit: a rename, an add, a delete, and an edit that the third
	// step will undo.
	gitOracleRun(t, root, "mv", "moved.go", "renamed.go")
	gitOracleRun(t, root, "rm", "-q", "removed.go")
	gitOracleWrite(t, root, "added.go", "package p\n\nfunc Added() {}\n")
	gitOracleWrite(t, root, "reverted.go", "package p\n\nfunc Reverted() { _ = 1 }\n")
	gitOracleRun(t, root, "add", "-A")
	gitOracleRun(t, root, "commit", "-m", "churn")

	// Working tree: undo the committed edit, and edit a file without
	// committing it.
	gitOracleWrite(t, root, "reverted.go", "package p\n\nfunc Reverted() {}\n")
	gitOracleWrite(t, root, "edited.go", "package p\n\nfunc Edited() { _ = 2 }\n")
	gitOracleWrite(t, root, "untracked.go", "package p\n")

	want := gitOracleChangedFiles(t, root, "HEAD~1")
	got, err := ReleaseChangedFiles(root, "HEAD~1")
	if err != nil {
		t.Fatalf("ReleaseChangedFiles: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %v (git), got %v", want, got)
	}

	// The oracle is only trustworthy if it actually exercised the cases, so
	// the interesting paths are asserted to be present or absent by name.
	for _, path := range []string{"moved.go", "renamed.go", "added.go", "removed.go", "edited.go"} {
		if !gitOracleHas(got, path) {
			t.Errorf("want %s reported as changed, got %v", path, got)
		}
	}
	for _, path := range []string{"reverted.go", "keep.go", "untracked.go"} {
		if gitOracleHas(got, path) {
			t.Errorf("want %s absent (git does not list it), got %v", path, got)
		}
	}
}

// TestReleaseChangedFilesUnresolvableBase covers upstream's retry: a base
// that does not resolve falls back to `git diff --name-status --cached`, and
// a failure there is an empty list rather than an error.
func TestReleaseChangedFilesUnresolvableBase(t *testing.T) {
	root := gitOracleRepo(t)
	gitOracleWrite(t, root, "a.go", "package p\n")
	gitOracleRun(t, root, "add", "-A")
	gitOracleRun(t, root, "commit", "-m", "one")

	// A staged change is what the retry is supposed to find, and an
	// unstaged one is what it must not.
	gitOracleWrite(t, root, "staged.go", "package p\n\nfunc Staged() {}\n")
	gitOracleRun(t, root, "add", "staged.go")
	gitOracleWrite(t, root, "a.go", "package p\n\nfunc A() {}\n")

	const missing = "0000000000000000000000000000000000000000"
	want := gitOracleChangedFiles(t, root, missing)
	got, err := ReleaseChangedFiles(root, missing)
	if err != nil {
		t.Fatalf("ReleaseChangedFiles: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %v (git), got %v", want, got)
	}
	if !gitOracleHas(got, "staged.go") {
		t.Errorf("want the staged path from the --cached retry, got %v", got)
	}
}

// TestReleaseChangedFilesRootCommit covers a repository whose only commit is
// its root commit, where `HEAD~1` does not resolve. Upstream reports no
// changes there, and the native build lifecycle depends on that not being an
// error.
func TestReleaseChangedFilesRootCommit(t *testing.T) {
	root := gitOracleRepo(t)
	gitOracleWrite(t, root, "a.go", "package p\n")
	gitOracleRun(t, root, "add", "-A")
	gitOracleRun(t, root, "commit", "-m", "root")

	want := gitOracleChangedFiles(t, root, "HEAD~1")
	got, err := ReleaseChangedFiles(root, "HEAD~1")
	if err != nil {
		t.Fatalf("ReleaseChangedFiles: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %v (git), got %v", want, got)
	}
	if len(got) != 0 {
		t.Fatalf("want no changes for a clean root-commit repository, got %v", got)
	}
}

// TestReleaseChangedFilesRejectsUnsafeRef pins upstream's ref guard. Both
// arms return an empty list rather than an error, and neither reaches git.
func TestReleaseChangedFilesRejectsUnsafeRef(t *testing.T) {
	root := gitOracleRepo(t)
	gitOracleWrite(t, root, "a.go", "package p\n")
	gitOracleRun(t, root, "add", "-A")
	gitOracleRun(t, root, "commit", "-m", "one")
	gitOracleWrite(t, root, "a.go", "package p\n\nfunc A() {}\n")

	for _, base := range []string{"", "-rf", "--output=/tmp/x", "HEAD; rm -rf /", "a ref"} {
		got, err := ReleaseChangedFiles(root, base)
		if err != nil {
			t.Fatalf("base %q: %v", base, err)
		}
		if len(got) != 0 {
			t.Errorf("base %q: want an empty list for a rejected ref, got %v", base, got)
		}
	}
}

// TestReleaseChangedFilesOutsideRepository covers the spawn-failure arm:
// `git diff` outside a repository exits non-zero, so upstream returns an
// empty list.
func TestReleaseChangedFilesOutsideRepository(t *testing.T) {
	got, err := ReleaseChangedFiles(t.TempDir(), "HEAD~1")
	if err != nil {
		t.Fatalf("ReleaseChangedFiles: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want an empty list outside a repository, got %v", got)
	}
}

// TestReleaseWorkingTreeFilesMatchesGitStatus covers the helper upstream
// falls back to when the diff finds nothing: staged, unstaged and untracked
// paths, with a STAGED RENAME reporting only its destination.
//
// The rename asymmetry is the whole reason this is not the same set as
// ReleaseChangedFiles: `git status --porcelain` collapses a rename into one
// record whose trailing source path upstream explicitly skips.
func TestReleaseWorkingTreeFilesMatchesGitStatus(t *testing.T) {
	root := gitOracleRepo(t)
	gitOracleWrite(t, root, "keep.go", "package p\n")
	gitOracleWrite(t, root, "moved.go", "package p\n\nfunc Moved() {}\n")
	gitOracleWrite(t, root, "edited.go", "package p\n")
	gitOracleWrite(t, root, "dropped.go", "package p\n")
	gitOracleRun(t, root, "add", "-A")
	gitOracleRun(t, root, "commit", "-m", "baseline")

	gitOracleRun(t, root, "mv", "moved.go", "renamed.go")
	gitOracleWrite(t, root, "staged.go", "package p\n")
	gitOracleRun(t, root, "add", "staged.go")
	gitOracleWrite(t, root, "edited.go", "package p\n\nfunc Edited() {}\n")
	if err := os.Remove(filepath.Join(root, "dropped.go")); err != nil {
		t.Fatalf("remove dropped.go: %v", err)
	}
	gitOracleWrite(t, root, "untracked.go", "package p\n")

	want := gitOracleWorkingTreeFiles(t, root)
	got, err := ReleaseWorkingTreeFiles(root)
	if err != nil {
		t.Fatalf("ReleaseWorkingTreeFiles: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %v (git), got %v", want, got)
	}
	for _, path := range []string{"renamed.go", "staged.go", "edited.go", "dropped.go", "untracked.go"} {
		if !gitOracleHas(got, path) {
			t.Errorf("want %s reported, got %v", path, got)
		}
	}
	for _, path := range []string{"moved.go", "keep.go"} {
		if gitOracleHas(got, path) {
			t.Errorf("want %s absent, got %v", path, got)
		}
	}
}

// TestReleaseWorkingTreeFilesCleanTree pins the case that makes the two
// helpers compose: a clean tree reports nothing, which is when upstream stops
// after the diff instead of substituting the working-tree set.
func TestReleaseWorkingTreeFilesCleanTree(t *testing.T) {
	root := gitOracleRepo(t)
	gitOracleWrite(t, root, "a.go", "package p\n")
	gitOracleRun(t, root, "add", "-A")
	gitOracleRun(t, root, "commit", "-m", "one")

	want := gitOracleWorkingTreeFiles(t, root)
	got, err := ReleaseWorkingTreeFiles(root)
	if err != nil {
		t.Fatalf("ReleaseWorkingTreeFiles: %v", err)
	}
	if !reflect.DeepEqual(want, got) || len(got) != 0 {
		t.Fatalf("want no working-tree changes, got %v (git: %v)", got, want)
	}
}

// TestReleaseHelpersRejectSubversionWorkingCopy pins the capability error.
//
// Upstream discovers an SVN working copy's changes with `svn status`; with no
// native equivalent, returning an empty list would report "nothing changed"
// for a checkout full of changes, so both helpers refuse instead.
func TestReleaseHelpersRejectSubversionWorkingCopy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".svn"), 0o755); err != nil {
		t.Fatalf("create .svn: %v", err)
	}
	if _, err := ReleaseChangedFiles(root, "HEAD~1"); !errors.Is(err, ErrSubversionWorkingCopy) {
		t.Errorf("ReleaseChangedFiles: want ErrSubversionWorkingCopy, got %v", err)
	}
	if _, err := ReleaseWorkingTreeFiles(root); !errors.Is(err, ErrSubversionWorkingCopy) {
		t.Errorf("ReleaseWorkingTreeFiles: want ErrSubversionWorkingCopy, got %v", err)
	}

	// A repository carrying both markers is git, matching upstream's
	// detect_vcs ordering.
	both := gitOracleRepo(t)
	if err := os.MkdirAll(filepath.Join(both, ".svn"), 0o755); err != nil {
		t.Fatalf("create .svn: %v", err)
	}
	if _, err := ReleaseWorkingTreeFiles(both); err != nil {
		t.Errorf("a repository with both markers must be treated as git, got %v", err)
	}
}

// ── git oracle ───────────────────────────────────────────────────────────────

// gitOracleRepo initialises an empty repository, skipping when git is absent
// so a machine without git reports "not run" instead of a parity failure.
func gitOracleRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitOracleRun(t, root, "init", "-q", "-b", "main", ".")
	return root
}

// gitOracleRun executes git and fails the test if it does not succeed.
func gitOracleRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	stdout, code := gitOracleCapture(t, root, args...)
	if code != 0 {
		t.Fatalf("git %s in %s: exit %d", strings.Join(args, " "), root, code)
	}
	return stdout
}

// gitOracleCapture executes git and returns its stdout and exit status, which
// is what the retry arms are keyed on.
func gitOracleCapture(t *testing.T, root string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), gitOracleEnv...)
	stdout, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return string(stdout), exit.ExitCode()
		}
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(stdout), 0
}

// gitOracleWrite writes one working-tree file.
func gitOracleWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// gitOracleChangedFiles is upstream's get_changed_files run against real git:
// the `--name-status -z` diff, the `--cached` retry on failure, and an empty
// list when both fail.
func gitOracleChangedFiles(t *testing.T, root, base string) []string {
	t.Helper()
	stdout, code := gitOracleCapture(t, root, "diff", "--name-status", "-z", base, "--")
	if code != 0 {
		stdout, code = gitOracleCapture(t, root, "diff", "--name-status", "-z", "--cached")
	}
	if code != 0 {
		return []string{}
	}
	return gitOracleSorted(gitOracleDecodeNameStatus(stdout))
}

// gitOracleDecodeNameStatus is upstream's _decode_name_status_paths: a rename
// or copy record carries TWO paths and both are emitted, which is why
// upstream reads --name-status rather than --name-only.
func gitOracleDecodeNameStatus(stdout string) []string {
	fields := make([]string, 0, 8)
	for _, field := range strings.Split(stdout, "\x00") {
		if field != "" {
			fields = append(fields, field)
		}
	}
	var paths []string
	for index := 0; index < len(fields); {
		status := fields[index]
		width := 2
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			width = 3
		}
		end := index + width
		if end > len(fields) {
			end = len(fields)
		}
		paths = append(paths, fields[index+1:end]...)
		index += width
	}
	return paths
}

// gitOracleWorkingTreeFiles is upstream's get_staged_and_unstaged run against
// real git, including its rename handling: the destination path is kept and
// the source record that follows it is skipped.
func gitOracleWorkingTreeFiles(t *testing.T, root string) []string {
	t.Helper()
	stdout, code := gitOracleCapture(
		t, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if code != 0 {
		return []string{}
	}
	records := strings.Split(stdout, "\x00")
	var paths []string
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) <= 3 {
			continue
		}
		status := record[:2]
		paths = append(paths, record[3:])
		if strings.Contains(status, "R") || strings.Contains(status, "C") {
			index++
		}
	}
	return gitOracleSorted(paths)
}

// gitOracleSorted deduplicates and orders the oracle's paths, because the
// helpers' documented contract is a deduped sorted set rather than git's
// output order.
func gitOracleSorted(paths []string) []string {
	unique := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		unique = append(unique, path)
	}
	sort.Strings(unique)
	return unique
}

func gitOracleHas(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
