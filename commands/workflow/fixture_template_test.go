package workflow

// Shared committed-repo TEMPLATE fixtures (perf H1, test-speed).
//
// testutil.InitGitRepo — and the equivalent inline bootstraps used by
// closeTaskTestRepo / gogitTestRepoWithCommit — spend FIVE-to-SIX git
// subprocesses (init, config×2-3, add, commit) to stand up one committed repo.
// Those bootstraps run serially, once per test, and dominate the
// commands/workflow suite's Windows wall-clock (~10× the Mac git-spawn cost).
//
// This file builds the ONE canonical committed repo exactly once (a real-git
// bootstrap, guarded by sync.Once) and hands every test a cheap, isolated
// filesystem COPY of that repo directory — including .git — into its own
// t.TempDir(). A copied git worktree is a fully valid local repo (git uses
// location-independent relative paths internally), so the copy carries the
// byte-identical committed HEAD / refs / objects the per-test bootstrap
// produced, with ZERO new git subprocesses. See TestSharedFixtureTemplate*.
//
// Instrumented proof seam: templateBuildCount counts real-git bootstraps
// (expected: 1 for the whole binary); templateCopyCount counts the cheap
// copies that replaced the per-test bootstraps.

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

var (
	committedRepoTemplateOnce sync.Once
	committedRepoTemplatePath string
	committedRepoTemplateErr  error

	// templateBuildCount — number of real-git bootstraps performed (the
	// expensive init+config+add+commit sequence). Expected to be exactly 1
	// across a full test-binary run regardless of how many fixtures are
	// requested. templateCopyCount — number of cheap template copies that
	// took the place of a per-test bootstrap. Both are the H1 proof metric.
	templateBuildCount int64
	templateCopyCount  int64
)

// committedRepoTemplate lazily bootstraps the canonical committed repo once
// and returns its path. The repo is byte-identical to what closeTaskTestRepo's
// old inline bootstrap and gogitTestRepoWithCommit produced per call: a single
// committed README ("hi") authored/committed as Test <t@e> at a fixed date,
// user.name/user.email set in local config so go-git's identity lookup
// succeeds, and commit.gpgsign disabled. The directory lives for the lifetime
// of the test process (read-only, shared across every copy).
func committedRepoTemplate() (string, error) {
	committedRepoTemplateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "wf-committed-tmpl-")
		if err != nil {
			committedRepoTemplateErr = err
			return
		}
		run := func(args ...string) error {
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
				"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
				"GIT_AUTHOR_DATE=2026-05-23T00:00:00Z", "GIT_COMMITTER_DATE=2026-05-23T00:00:00Z",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				return &fixtureGitError{args: args, err: err, out: out}
			}
			return nil
		}
		if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hi"), 0o644); err != nil {
			committedRepoTemplateErr = err
			return
		}
		for _, args := range [][]string{
			{"init", "-q"},
			{"config", "user.email", "t@e"},
			{"config", "user.name", "t"},
			{"config", "commit.gpgsign", "false"},
			{"add", "README"},
			{"commit", "-q", "-m", "seed"},
		} {
			if err := run(args...); err != nil {
				committedRepoTemplateErr = err
				return
			}
		}
		atomic.AddInt64(&templateBuildCount, 1)
		committedRepoTemplatePath = dir
	})
	return committedRepoTemplatePath, committedRepoTemplateErr
}

type fixtureGitError struct {
	args []string
	err  error
	out  []byte
}

func (e *fixtureGitError) Error() string {
	return "git " + filepath.Join(e.args...) + ": " + e.err.Error() + "\n" + string(e.out)
}

// sharedCommittedRepo returns a fresh, isolated copy of the committed-repo
// template inside t.TempDir(): a valid local git repo (README committed, HEAD
// present, clean worktree) produced with ZERO new git subprocesses. Drop-in
// replacement for gogitTestRepoWithCommit / the per-test real-git bootstrap.
func sharedCommittedRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH (still needed to bootstrap the shared fixture template)")
	}
	tmpl, err := committedRepoTemplate()
	if err != nil {
		t.Fatalf("build committed-repo template: %v", err)
	}
	dst := t.TempDir()
	if err := copyDirContents(tmpl, dst); err != nil {
		t.Fatalf("copy committed-repo template into %s: %v", dst, err)
	}
	atomic.AddInt64(&templateCopyCount, 1)
	return dst
}

// copyDirContents recursively copies the children of src into dst,
// preserving file contents, permission bits, symlinks, and modification
// times. Preserving mtimes keeps the copied git index racily-clean rather
// than forcing git to re-stat, so the copy's `git status` stays identical to
// the source's. dst is expected to already exist (a t.TempDir()).
func copyDirContents(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
				return err
			}
			mt := info.ModTime()
			return os.Chtimes(target, mt, mt)
		}
	})
}

// TestSharedFixtureTemplateReusesSingleBootstrap is the instrumented H1 proof:
// once the template exists, requesting N more fixtures performs ZERO additional
// real-git bootstraps and exactly N cheap copies. Delta-based so it is
// independent of how many other tests already exercised the fixture.
func TestSharedFixtureTemplateReusesSingleBootstrap(t *testing.T) {
	_ = sharedCommittedRepo(t) // ensure the template is built

	startBuilds := atomic.LoadInt64(&templateBuildCount)
	startCopies := atomic.LoadInt64(&templateCopyCount)

	const n = 8
	for range n {
		_ = sharedCommittedRepo(t)
	}

	if extra := atomic.LoadInt64(&templateBuildCount) - startBuilds; extra != 0 {
		t.Errorf("expected 0 additional real-git bootstraps across %d fixtures, got %d", n, extra)
	}
	if copies := atomic.LoadInt64(&templateCopyCount) - startCopies; copies != int64(n) {
		t.Errorf("expected %d template copies, got %d", n, copies)
	}
	if total := atomic.LoadInt64(&templateBuildCount); total != 1 {
		t.Errorf("expected exactly 1 real-git bootstrap for the whole binary, got %d", total)
	}
}

// TestSharedFixtureTemplateCopyIsValidCommittedRepo proves the copy is a
// byte-faithful, usable git repo: README committed, HEAD present, clean tree.
func TestSharedFixtureTemplateCopyIsValidCommittedRepo(t *testing.T) {
	repo := sharedCommittedRepo(t)

	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		t.Fatalf(".git missing in copied fixture: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repo, "README"))
	if err != nil {
		t.Fatalf("README missing in copied fixture: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("README content = %q, want %q", string(data), "hi")
	}
	head, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD on copy: %v\n%s", err, head)
	}
	status, err := exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status on copy: %v\n%s", err, status)
	}
	if len(status) != 0 {
		t.Errorf("copied fixture worktree not clean: %q", string(status))
	}
}
