package testutil

import "testing"

// MakeDirWriteDenied makes the directory at dir reject child-mutation
// operations (create, write, delete) for the duration of the test using the
// OS-native denial mechanism, while preserving read+execute. Callers can rely
// on:
//
//   - os.Create / os.WriteFile against a child path returns an error.
//   - os.Remove / os.RemoveAll against an existing child returns an error.
//   - os.ReadDir(dir) still SUCCEEDS — the helper is the dual of
//     MakeDirUnreadable: read-side stays open so the test can verify the
//     unchanged contents after the denial.
//
// The exact error varies by OS: POSIX denies with EACCES at the unlink/open
// syscall via chmod 0o500 (the write bit is cleared, read+execute remain);
// Windows denies with ERROR_ACCESS_DENIED via a deny-ACE on the directory's
// DACL that masks FILE_WRITE_DATA | FILE_APPEND_DATA | FILE_DELETE_CHILD.
// Tests that need to assert a specific errno should compare against
// errors.Is(err, fs.ErrPermission) — both platforms map to that.
//
// Why this exists alongside MakeDirUnreadable. The sibling helper denies
// list/traverse (FILE_LIST_DIRECTORY | FILE_TRAVERSE on Windows; chmod 0 on
// POSIX) and is the right tool when the test asserts a ReadDir / opendir
// failure. A second family of tests asserts that os.Remove / os.RemoveAll on
// a child fails because the *parent* refuses the unlink — that path needs
// write-side denial only. Reusing MakeDirUnreadable would also block stat /
// readdir on the parent, which can mask the test's actual assertion (the
// fault would surface at the wrong syscall). This helper isolates the
// write/delete denial so the assertion stays on the operation under test.
//
// Per-platform mechanism. On POSIX (the //go:build unix file) the helper
// chmods the directory to 0o500 (read+execute, no write) and restores 0o755
// in a t.Cleanup so the surrounding t.TempDir can complete its own teardown.
// On Windows (the //go:build windows file) the helper installs a deny-ACE on
// the directory's DACL for the current process's user SID via
// SetNamedSecurityInfo; FILE_WRITE_DATA | FILE_APPEND_DATA |
// FILE_DELETE_CHILD are denied, so any CreateFile-with-write or DeleteFile
// against a child fails with ERROR_ACCESS_DENIED. The original DACL is
// restored at t.Cleanup time so t.TempDir teardown can remove the directory.
//
// Failure modes the helper handles itself. Running as root on POSIX bypasses
// mode bits, so the helper t.Skips with a clear reason instead of producing a
// false negative. On Windows, running as a process with SeBackupPrivilege /
// SeRestorePrivilege (typical for elevated/admin contexts) bypasses DACL
// enforcement; the helper probes by attempting os.Create on a transient
// child after installing the deny-ACE and t.Skips if the denial was not
// actually applied. Tmpfs and other synthetic filesystems on Linux can also
// short-circuit chmod enforcement under some kernel configurations; the same
// post-install probe covers that case. Callers should not duplicate these
// checks — concentrating the platform policy here is the point of the
// abstraction.
func MakeDirWriteDenied(t *testing.T, dir string) {
	t.Helper()
	makeDirWriteDenied(t, dir)
}
