package testutil

import "testing"

// MakeDirUnreadable makes the directory at dir unenumerable for the duration
// of the test using the OS-native denial mechanism. Callers can rely on:
//
//   - os.ReadDir(dir) returns an error.
//   - filepath.WalkDir's visit on dir's children returns an error.
//
// The exact error varies by OS: POSIX denies with EACCES at the readdir
// system call; Windows denies with ERROR_ACCESS_DENIED via a deny-ACE on the
// directory's DACL. Tests that need to assert a specific errno should compare
// against errors.Is(err, fs.ErrPermission) — both platforms map to that.
//
// Why this exists. The sibling helper MakeFileUnreadable handles per-file
// denial; many tests also need to assert the "cannot list a directory" path
// (e.g. iteration over a stale plans/ tree, scanning a hooks dir, walking a
// resource snapshot). The POSIX-only `os.Chmod(dir, 0)` trick does not work on
// Windows: NTFS does not honour POSIX mode bits, so the readdir succeeds and
// the error path the test wanted to exercise is silently skipped. Sprinkling
// `if runtime.GOOS == "windows" { t.Skip(...) }` around every such test
// silently lowers Windows coverage.
//
// Per-platform mechanism. On POSIX (the //go:build unix file) the helper
// chmods the directory to 0 and restores 0o755 in a t.Cleanup so the
// surrounding t.TempDir can complete its own teardown. On Windows (the
// //go:build windows file) the helper installs a deny-ACE on the directory's
// DACL for the current process's user SID via SetNamedSecurityInfo;
// FILE_LIST_DIRECTORY is denied, so any FindFirstFile/ReadDir against the dir
// fails with ERROR_ACCESS_DENIED. The original DACL is restored at t.Cleanup
// time so t.TempDir teardown can remove the directory.
//
// Failure modes the helper handles itself. Running as root on POSIX bypasses
// mode bits, so the helper t.Skips with a clear reason instead of producing a
// false negative. On Windows, running as a process with SeBackupPrivilege /
// SeRestorePrivilege (typical for elevated/admin contexts) bypasses DACL
// enforcement; the helper probes by attempting os.ReadDir after installing
// the deny-ACE and t.Skips if the denial was not actually applied. Tmpfs and
// other synthetic filesystems on Linux can also short-circuit chmod
// enforcement under some kernel configurations; the same post-install probe
// covers that case. Callers should not duplicate these checks — concentrating
// the platform policy here is the point of the abstraction.
func MakeDirUnreadable(t *testing.T, dir string) {
	t.Helper()
	makeDirUnreadable(t, dir)
}
