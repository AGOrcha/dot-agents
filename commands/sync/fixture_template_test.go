package sync

// Shared seeded-repo TEMPLATE fixture (perf H5, cross-package test-speed).
//
// initEmptyRepo bootstraps a git repo with `git init`, two `git config`
// calls, and an initial `--allow-empty` commit — FOUR git subprocesses — and
// ~26 tests in this package call it, each paying that bootstrap serially. On
// Windows the git-spawn cost is ~10× Mac, so these repeated bootstraps
// dominate the package's test wall-clock.
//
// This file builds ONE canonical configured repo carrying a single empty seed
// commit exactly once (guarded by sync.Once) and hands every initEmptyRepo
// call a byte-faithful filesystem COPY of that repo (including .git) into the
// caller's dir, with ZERO new git subprocesses. A copied git repo is a fully
// valid local repo (git's internal paths are relative), so the copy carries
// the identical HEAD / refs / config the per-test bootstrap produced. The
// seed commit is built with a fixed identity+date so every copy is
// byte-identical regardless of which test triggers the one-time build.
//
// Instrumented proof seam: syncTemplateBuildCount counts real bootstraps
// (expected: 1 for the whole test binary); syncTemplateCopyCount counts the
// cheap copies that replaced the per-test bootstraps.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/sys/execabs"
)

var (
	syncRepoTemplateOnce sync.Once
	syncRepoTemplatePath string
	syncRepoTemplateErr  error

	// syncTemplateBuildCount — real bootstraps performed (the expensive
	// init+config+empty-commit sequence). Expected to be exactly 1 across a
	// full test-binary run. syncTemplateCopyCount — cheap template copies
	// that took the place of a per-test bootstrap. Both are the H5 proof
	// metric.
	syncTemplateBuildCount int64
	syncTemplateCopyCount  int64
)

// syncRepoTemplate lazily bootstraps the canonical seeded repo once and
// returns its path. The repo is equivalent to what initEmptyRepo's old inline
// sequence produced: `git init`, user.name=Test / user.email=test@example.com
// in local config, and a single empty seed commit so HEAD exists. Identity
// and dates are pinned so the one-time build is deterministic. The directory
// lives for the lifetime of the test process (read-only, shared across every
// copy).
func syncRepoTemplate() (string, error) {
	syncRepoTemplateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sync-git-tmpl-")
		if err != nil {
			syncRepoTemplateErr = err
			return
		}
		run := func(args ...string) error {
			cmd := execabs.Command("git", append([]string{"-C", dir}, args...)...)
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
				"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
				"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git %v: %w\n%s", args, err, out)
			}
			return nil
		}
		for _, args := range [][]string{
			{"init"},
			{"config", "user.name", "Test"},
			{"config", "user.email", "test@example.com"},
			{"commit", "--allow-empty", "-m", "seed"},
		} {
			if err := run(args...); err != nil {
				syncRepoTemplateErr = err
				return
			}
		}
		atomic.AddInt64(&syncTemplateBuildCount, 1)
		syncRepoTemplatePath = dir
	})
	return syncRepoTemplatePath, syncRepoTemplateErr
}

// copyGitTemplate recursively copies the children of src into dst, preserving
// file contents, permission bits, symlinks, and modification times.
// Preserving mtimes keeps the copied git index racily-clean rather than
// forcing git to re-stat, so the copy's `git status` stays identical to the
// source's. dst is expected to already exist.
func copyGitTemplate(src, dst string) error {
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

// TestSyncRepoTemplateReusesSingleBootstrap is the instrumented H5 proof: once
// the template exists, requesting N more fixtures performs ZERO additional
// real bootstraps and exactly N cheap copies. Delta-based so it is independent
// of how many other tests already exercised the fixture.
func TestSyncRepoTemplateReusesSingleBootstrap(t *testing.T) {
	requireGit(t)
	initEmptyRepo(t, t.TempDir()) // ensure the template is built

	startBuilds := atomic.LoadInt64(&syncTemplateBuildCount)
	startCopies := atomic.LoadInt64(&syncTemplateCopyCount)

	const n = 8
	for range n {
		initEmptyRepo(t, t.TempDir())
	}

	if extra := atomic.LoadInt64(&syncTemplateBuildCount) - startBuilds; extra != 0 {
		t.Errorf("expected 0 additional bootstraps across %d fixtures, got %d", n, extra)
	}
	if copies := atomic.LoadInt64(&syncTemplateCopyCount) - startCopies; copies != int64(n) {
		t.Errorf("expected %d template copies, got %d", n, copies)
	}
	if total := atomic.LoadInt64(&syncTemplateBuildCount); total != 1 {
		t.Errorf("expected exactly 1 bootstrap for the whole binary, got %d", total)
	}
}
