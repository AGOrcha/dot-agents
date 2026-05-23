package testutil

import "testing"

// MakeFileUnreadable makes the file at path unreadable for the duration of
// the test using the OS-native denial mechanism. Callers can rely on:
//
//   - os.ReadFile(path) returns an error.
//   - bufio.Scanner(f).Scan() (the iteration path BackfillIterations uses)
//     returns an error once it tries to read bytes.
//
// The exact failure point varies by OS: POSIX denies at open; Windows denies
// at read. Tests that need a guaranteed open-failure should test their
// downstream contract (read failure) rather than os.Open directly.
//
// Why this exists. The natural-looking POSIX trick — os.WriteFile(path, data,
// 0o000) and read it back — does not work on Windows: NTFS does not honour
// POSIX mode bits, so the read succeeds and the error path the test wanted
// to exercise is silently skipped. Sprinkling `if runtime.GOOS == "windows" {
// t.Skip(...) }` around every such test silently lowers coverage on the
// Windows runner and lets real Windows-only regressions slip in.
//
// Per-platform mechanism. On POSIX (the //go:build unix file) the helper
// chmods the file to 0 and restores 0o644 in a t.Cleanup so the surrounding
// t.TempDir can complete its own teardown. On Windows (the //go:build windows
// file) the helper takes an exclusive byte-range lock spanning the entire
// file via LockFileEx; reads from any handle to the locked range fail with
// ERROR_LOCK_VIOLATION until the lock is released at t.Cleanup time. The
// Windows path intentionally does NOT rely on FILE_SHARE_NONE because CI
// runners have antivirus processes that open new files with maximum sharing,
// which defeats share-mode denial; byte-range locks are not subject to that
// interference.
//
// Failure modes the helper handles itself. Running as root on POSIX bypasses
// mode bits, so the helper t.Skips with a clear reason instead of producing a
// false negative. Callers should not duplicate that check — concentrating the
// platform policy here is the point of the abstraction.
func MakeFileUnreadable(t *testing.T, path string) {
	t.Helper()
	makeFileUnreadable(t, path)
}
