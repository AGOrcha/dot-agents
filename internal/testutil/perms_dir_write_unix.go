//go:build unix

package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// makeDirWriteDenied on POSIX chmods the directory to 0o500 — owner keeps
// read+execute (so the surrounding test can still list the dir and stat the
// children it expects to remain), but the write bit is cleared so the kernel
// denies child create/unlink with EACCES. The original 0o755 mode is restored
// at t.Cleanup time so the enclosing t.TempDir can finish its own teardown
// without surprise.
//
// Running as root bypasses mode bits entirely (the kernel's DAC short-circuit
// for uid 0), so the helper t.Skips with a clear reason rather than producing
// a false negative. After applying the chmod, the helper probes by attempting
// to create a transient child file; if the create still succeeds (e.g. on a
// synthetic filesystem that does not enforce DAC on directory writes), the
// helper t.Skips rather than let the caller assert against a denial that
// never happened.
func makeDirWriteDenied(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root; POSIX mode bits do not deny directory write")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("MakeDirWriteDenied: chmod 0o500 %q: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	// Probe: attempt to create a transient child. If the create succeeds the
	// platform is not enforcing write denial; skip rather than mislead the
	// caller. Clean up the probe file immediately on success (it should fail
	// — but if it didn't, we don't want to leave residue behind).
	probe := filepath.Join(dir, ".makedirwritedenied-probe")
	f, err := os.OpenFile(probe, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_ = f.Close()
		_ = os.Remove(probe)
		t.Skip("filesystem does not enforce chmod-0o500 directory write denial (tmpfs/synthetic FS?); cannot exercise the assertion")
	}
}
