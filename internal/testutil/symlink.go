package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// SymlinkOrSkip gates a test on the *capability* to create a symbolic link
// in the current process, NOT on the operating system. It probes by
// attempting an os.Symlink inside t.TempDir(); if the probe succeeds the
// helper returns and the caller may use real symlinks for the remainder
// of the test. If the probe fails the helper calls t.Skip with a
// Windows-aware reason and the test stops.
//
// # Why capability, not OS
//
// Existing tests across the tree gate symlink coverage with
//
//	if runtime.GOOS == "windows" { t.Skip(...) }
//
// That gate is too coarse. Windows 10+ with Developer Mode enabled — and
// the default GitHub Actions windows-latest runner, which grants
// SeCreateSymbolicLinkPrivilege to the build user — *can* create
// symlinks. Skipping unconditionally on Windows surrenders coverage that
// is in fact available, and a windows-only path bug in the production
// link layer would never be observed by CI. The right question is
// "can THIS process call os.Symlink right now?" — answered by trying it.
//
// On POSIX the probe always succeeds (modulo a write-only TempDir, which
// is exotic enough we let the t.Skip path handle it). On Windows the
// probe fails with ERROR_PRIVILEGE_NOT_HELD when the process lacks the
// privilege, and the test is skipped with a message naming Developer
// Mode as the remedy. On a properly-configured Windows host the probe
// succeeds and the test runs end-to-end, picking up the coverage that
// the runtime.GOOS gate threw away.
//
// # Probe shape
//
// The probe symlinks a non-existent target ("probe-target") to a
// freshly-created link ("probe-link") under t.TempDir(). The target does
// not need to exist for os.Symlink to succeed — the call only needs the
// process privilege. Both paths are inside t.TempDir(), so cleanup is
// automatic and no fixture is left behind whether the probe succeeds or
// fails. The helper does NOT pre-clean the probe link; t.TempDir's
// teardown removes the whole tree.
//
// # Usage
//
//	func TestSomethingThatNeedsSymlinks(t *testing.T) {
//	    testutil.SymlinkOrSkip(t)
//	    // ... call os.Symlink freely from here
//	}
//
// # Migration targets (10 sites, deferred to migrate-sites task)
//
// Per the catalogue in
// .agents/workflow/plans/cross-platform-test-skips-audit/findings.md,
// the [shortcut-symlink] class covers these sites:
//   - internal/links/symlink_remove_error_test.go (2 tests)
//   - internal/links/managed_link_branches_test.go (3 tests)
//   - internal/links/managed_link_branches2_test.go (5 tests, one mixed
//     with hardlink — verify hardlink coverage isn't lost when migrating)
//
// Replace each `if runtime.GOOS == "windows" { t.Skip(...) }` preamble
// with a single call to SymlinkOrSkip(t). Do NOT migrate sites in the
// [genuine-posix] class (Windows-file-managed-link semantics differ from
// POSIX symlinks at a higher level than the syscall — that's an
// abstraction problem, not a privilege problem).
func SymlinkOrSkip(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	link := filepath.Join(dir, "probe-link")
	if err := os.Symlink("probe-target", link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("SymlinkOrSkip: this process cannot create symlinks (%v); "+
				"enable Developer Mode or grant SeCreateSymbolicLinkPrivilege to run", err)
			return
		}
		t.Skipf("SymlinkOrSkip: os.Symlink probe failed (%v)", err)
		return
	}
}
