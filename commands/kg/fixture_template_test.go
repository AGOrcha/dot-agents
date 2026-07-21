package kg

// Shared empty-repo TEMPLATE fixture (perf H5, cross-package test-speed).
//
// initGitRepo bootstraps a fresh git repo with `git init` + two `git config`
// calls — THREE git subprocesses — and ~23 tests in this package call it, each
// paying that bootstrap serially. On Windows the git-spawn cost is ~10× Mac,
// so these repeated bootstraps dominate the package's test wall-clock.
//
// This file builds ONE canonical empty-but-configured repo exactly once
// (guarded by sync.Once) and hands every initGitRepo call a byte-faithful
// filesystem COPY of that repo's .git into the caller's dir, with ZERO new
// git subprocesses. A copied .git directory is a fully valid local repo
// (git's internal paths are relative), so the copy carries the identical
// (empty) HEAD / config the per-test bootstrap produced; tests then commit
// their own content on top exactly as before.
//
// Instrumented proof seam: kgTemplateBuildCount counts real `git init`
// bootstraps (expected: 1 for the whole test binary); kgTemplateCopyCount
// counts the cheap copies that replaced the per-test bootstraps.

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

var (
	kgRepoTemplateOnce sync.Once
	kgRepoTemplatePath string
	kgRepoTemplateErr  error

	// kgTemplateBuildCount — real `git init`+config bootstraps performed.
	// Expected to be exactly 1 across a full test-binary run regardless of
	// how many fixtures are requested. kgTemplateCopyCount — cheap template
	// copies that took the place of a per-test bootstrap. Both are the H5
	// proof metric.
	kgTemplateBuildCount int64
	kgTemplateCopyCount  int64
)

// kgRepoTemplate lazily bootstraps the canonical empty-but-configured repo
// once and returns its path. The repo is byte-identical to what initGitRepo's
// old inline sequence produced: `git init` plus user.name=test /
// user.email=test@example.com in local config, no commits. The directory
// lives for the lifetime of the test process (read-only, shared across every
// copy).
func kgRepoTemplate() (string, error) {
	kgRepoTemplateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "kg-git-tmpl-")
		if err != nil {
			kgRepoTemplateErr = err
			return
		}
		run := func(args ...string) error {
			if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
				return fmt.Errorf("git %v: %w\n%s", args, err, out)
			}
			return nil
		}
		for _, args := range [][]string{
			{"init"},
			{"config", "user.name", "test"},
			{"config", "user.email", "test@example.com"},
		} {
			if err := run(args...); err != nil {
				kgRepoTemplateErr = err
				return
			}
		}
		atomic.AddInt64(&kgTemplateBuildCount, 1)
		kgRepoTemplatePath = dir
	})
	return kgRepoTemplatePath, kgRepoTemplateErr
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

// TestKGRepoTemplateReusesSingleBootstrap is the instrumented H5 proof: once
// the template exists, requesting N more fixtures performs ZERO additional
// `git init` bootstraps and exactly N cheap copies. Delta-based so it is
// independent of how many other tests already exercised the fixture.
func TestKGRepoTemplateReusesSingleBootstrap(t *testing.T) {
	initGitRepo(t, t.TempDir()) // ensure the template is built

	startBuilds := atomic.LoadInt64(&kgTemplateBuildCount)
	startCopies := atomic.LoadInt64(&kgTemplateCopyCount)

	const n = 8
	for range n {
		initGitRepo(t, t.TempDir())
	}

	if extra := atomic.LoadInt64(&kgTemplateBuildCount) - startBuilds; extra != 0 {
		t.Errorf("expected 0 additional git-init bootstraps across %d fixtures, got %d", n, extra)
	}
	if copies := atomic.LoadInt64(&kgTemplateCopyCount) - startCopies; copies != int64(n) {
		t.Errorf("expected %d template copies, got %d", n, copies)
	}
	if total := atomic.LoadInt64(&kgTemplateBuildCount); total != 1 {
		t.Errorf("expected exactly 1 git-init bootstrap for the whole binary, got %d", total)
	}
}
