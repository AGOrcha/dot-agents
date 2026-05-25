//go:build unix

package testutil

import (
	"os"
	"testing"
)

// makeDirUnreadable on POSIX chmods the directory to 0 so subsequent
// os.ReadDir / opendir / readdir return EACCES. The original 0o755 mode is
// restored at t.Cleanup time so the enclosing t.TempDir can finish its own
// teardown without surprise.
//
// Running as root bypasses mode bits entirely (the kernel's DAC short-circuit
// for uid 0), so the helper t.Skips with a clear reason rather than producing
// a false negative. After applying the chmod, the helper probes with
// os.ReadDir; if the read still succeeds (e.g. on a synthetic filesystem that
// does not enforce DAC for directories), the helper t.Skips rather than let
// the caller assert against a denial that never happened.
func makeDirUnreadable(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root; POSIX mode bits do not deny directory read")
	}
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatalf("MakeDirUnreadable: chmod 0 %q: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("filesystem does not enforce chmod-0 directory denial (tmpfs/synthetic FS?); cannot exercise the assertion")
	}
}
