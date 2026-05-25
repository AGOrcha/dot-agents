package testutil

import (
	"os"
	"testing"
)

// MakeFileReadOnly makes the file at path reject writes for the duration of
// the test. After this call, os.WriteFile(path, ...) and
// os.OpenFile(path, O_WRONLY|O_APPEND, ...) both return an error until the
// surrounding test's Cleanup runs.
//
// Why this is cross-platform (no build-tag split). os.Chmod is one of the few
// POSIX permission calls Go translates verbatim on Windows: the runtime
// inspects the user-write bit (0o200) and toggles FILE_ATTRIBUTE_READONLY on
// the file accordingly (see Go's syscall_windows.go). Mode 0o444 has the
// user-write bit clear, so on both POSIX and Windows the file is rejected for
// write. This is the inverse of MakeFileUnreadable — POSIX/Windows agree on
// the write-denial semantics of chmod, while they disagree on read-denial,
// which is what forces MakeFileUnreadable into a platform-split.
//
// Failure modes the helper handles itself. On POSIX, running as root bypasses
// DAC mode bits for writes (the kernel's uid-0 short-circuit), so the helper
// would silently produce a false negative: the test would see the write
// succeed and conclude the read-only enforcement is broken. We probe by
// attempting a write after the chmod; if it succeeds we restore the mode and
// t.Skip with a clear reason. Windows has no equivalent bypass — the readonly
// attribute is enforced for every caller.
//
// Cleanup restores 0o644 so the enclosing t.TempDir teardown can remove the
// file. Without the restore, t.TempDir's recursive remove still works on
// POSIX (the parent dir's write bit is what governs removal), but on Windows
// the readonly attribute blocks os.Remove — leaving the t.TempDir teardown to
// log a noisy (non-fatal) cleanup error per file. Restoring 0o644 keeps both
// platforms quiet.
func MakeFileReadOnly(t *testing.T, path string) {
	t.Helper()

	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatalf("MakeFileReadOnly: chmod 0o444 %q: %v", path, err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(path, 0o644)
	})

	// Probe: if a write still succeeds, the platform/uid combination does
	// not enforce mode-bit write denial (root on POSIX). Skip with a clear
	// reason rather than handing the caller a helper that silently no-ops.
	if f, err := os.OpenFile(path, os.O_WRONLY, 0o444); err == nil {
		_ = f.Close()
		t.Skip("MakeFileReadOnly: process can still write to a 0o444 file (likely running as root); mode bits do not deny writes")
	}
}
