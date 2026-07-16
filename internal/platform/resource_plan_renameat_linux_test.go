//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRenameNoClobber_Linux_SucceedsWhenTargetAbsent is the positive case:
// tmp renames onto an absent target and target ends up resolving to src.
func TestRenameNoClobber_Linux_SucceedsWhenTargetAbsent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "cas-entry")
	tmp := filepath.Join(dir, "target.casswap-1")
	target := filepath.Join(dir, "target")
	if err := os.Symlink(src, tmp); err != nil {
		t.Fatalf("seed tmp symlink: %v", err)
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

// TestRenameNoClobber_Linux_FailsClosedWhenTargetReappears is the t2b
// TOCTOU-hardening negative case: RENAME_NOREPLACE must refuse the rename
// (not silently clobber) when something has landed at target since it was
// unlinked — simulating a racer that recreated the slot between
// atomicManagedSymlinkSwap's unlink of the verified old managed link and
// this rename call.
func TestRenameNoClobber_Linux_FailsClosedWhenTargetReappears(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "cas-entry")
	tmp := filepath.Join(dir, "target.casswap-1")
	target := filepath.Join(dir, "target")
	if err := os.Symlink(src, tmp); err != nil {
		t.Fatalf("seed tmp symlink: %v", err)
	}
	// A racer's real file lands at target in the window this call is meant to
	// close.
	if err := os.WriteFile(target, []byte("racer content"), 0o644); err != nil {
		t.Fatalf("seed racer file: %v", err)
	}
	if err := renameNoClobber(tmp, target, src); err == nil {
		t.Fatal("expected RENAME_NOREPLACE to fail closed against a racer occupant")
	}
	// The racer's content must be untouched.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("racer file was removed: %v", err)
	}
	if string(got) != "racer content" {
		t.Fatalf("racer content changed: %q", got)
	}
}
