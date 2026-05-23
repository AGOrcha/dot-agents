//go:build unix

package testutil

import (
	"os"
	"testing"
)

// makeFileUnreadable on POSIX chmods the file to 0 so subsequent os.Open
// returns EACCES. The original 0o644 mode is restored at t.Cleanup time so
// the enclosing t.TempDir can finish its own teardown without surprise.
//
// Running as root bypasses mode bits entirely (the kernel's DAC short-circuit
// for uid 0), so the helper t.Skips with a clear reason rather than producing
// a false negative.
func makeFileUnreadable(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root; POSIX mode bits do not deny read")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatalf("MakeFileUnreadable: chmod 0 %q: %v", path, err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(path, 0o644)
	})
}
