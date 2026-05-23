package testutil

import "testing"

// MakeFileUnreadable makes path unreadable for the duration of the test using
// the OS-native denial mechanism, so any subsequent os.Open(path) returns an
// error to the test target.
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
// file) the helper opens an exclusive windows.CreateFile handle with
// dwShareMode = 0; any other open on the path then fails with
// ERROR_SHARING_VIOLATION until the handle is closed at t.Cleanup time.
//
// Failure modes the helper handles itself. Running as root on POSIX bypasses
// mode bits, so the helper t.Skips with a clear reason instead of producing a
// false negative. Callers should not duplicate that check — concentrating the
// platform policy here is the point of the abstraction.
func MakeFileUnreadable(t *testing.T, path string) {
	t.Helper()
	makeFileUnreadable(t, path)
}
