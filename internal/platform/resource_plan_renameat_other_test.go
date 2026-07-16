//go:build !linux

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRenameNoClobber_Other_SucceedsAndReadsBack is the positive case: after
// a plain rename, target resolves to src and no error is returned.
func TestRenameNoClobber_Other_SucceedsAndReadsBack(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "cas-entry")
	tmp := filepath.Join(dir, "target.casswap-1")
	target := filepath.Join(dir, "target")
	if err := os.Symlink(src, tmp); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := renameNoClobber(tmp, target, src); err != nil {
		t.Fatalf("renameNoClobber: %v", err)
	}
	got, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("readlink target: %v", err)
	}
	if got != src {
		t.Fatalf("target resolves to %q, want %q", got, src)
	}
}

// TestRenameNoClobber_Other_DetectsMismatchAfterRename is the t2b
// verify-after-rename negative case: this platform has no atomic no-clobber
// rename primitive, so it cannot prevent a racer's content from being
// overwritten by our own rename — but it MUST detect and report when the
// live target does not resolve to the src the caller expected, rather than
// silently reporting success on a result that does not match what was
// requested (e.g. a second racer having repointed tmp/target's content
// between the rename and the read-back).
func TestRenameNoClobber_Other_DetectsMismatchAfterRename(t *testing.T) {
	dir := t.TempDir()
	actualDest := filepath.Join(dir, "actual-cas-entry")
	claimedSrc := filepath.Join(dir, "claimed-cas-entry") // caller's expectation, deliberately wrong
	tmp := filepath.Join(dir, "target.casswap-1")
	target := filepath.Join(dir, "target")
	if err := os.Symlink(actualDest, tmp); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	err := renameNoClobber(tmp, target, claimedSrc)
	if err == nil {
		t.Fatal("expected a mismatch between the live target and the claimed src to be reported")
	}
	// The rename itself still happened (best-effort primitive) — target
	// resolves to what was actually renamed in, not silently to nothing.
	got, readErr := os.Readlink(target)
	if readErr != nil {
		t.Fatalf("readlink target: %v", readErr)
	}
	if got != actualDest {
		t.Fatalf("target resolves to %q, want %q", got, actualDest)
	}
}
