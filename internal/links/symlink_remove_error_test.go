package links

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestSymlink_DanglingButCorrectIsNoop covers the existing==target return
// when the SameFile fast-path cannot fire because the target does not
// exist (a dangling symlink whose stored text already equals target).
func TestSymlink_DanglingButCorrectIsNoop(t *testing.T) {
	// testutil.SymlinkOrSkip gates on the capability to create a symbolic
	// link in the current process rather than the OS — Windows 10+ with
	// Developer Mode (and the windows-latest GH Actions runner) can create
	// symlinks, picking up coverage the runtime.GOOS gate threw away.
	testutil.SymlinkOrSkip(t)
	tmp := t.TempDir()
	missingTarget := filepath.Join(tmp, "not-created.txt")
	link := filepath.Join(tmp, "lnk")
	if err := os.Symlink(missingTarget, link); err != nil {
		t.Fatal(err)
	}
	// pathsResolveToSameFile errors (target missing) → falls through to
	// Readlink, existing == missingTarget == target → early nil return.
	if err := Symlink(missingTarget, link); err != nil {
		t.Fatalf("Symlink on dangling-but-correct link should no-op: %v", err)
	}
}

// TestSymlink_RemoveAllErrorBranches fault-injects the removal seams to
// cover the reachable "failed to remove" returns: (a) a stale managed
// symlink pointing elsewhere (fsopsRemoveAll), (b) an empty squat dir
// (fsopsRemove single-entry), (c) SymlinkReplacing over an unmanaged
// regular file after a successful backup (fsopsRemoveAll). An unmanaged
// regular file via plain Symlink is now refused with ErrUnmanagedTarget
// BEFORE any removal, so that is no longer a removal-error branch.
func TestSymlink_RemoveAllErrorBranches(t *testing.T) {
	testutil.SymlinkOrSkip(t)
	tmp := t.TempDir()
	// Canonical root = tmp so a link resolving under it is OWNED and the
	// stale-managed-link removal branch (fsopsRemoveAll) is reachable.
	t.Setenv("AGENTS_HOME", tmp)
	target := filepath.Join(tmp, "target.txt")
	if err := os.WriteFile(target, []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("injected remove failure")
	origAll := fsopsRemoveAll
	origOne := fsopsRemove
	fsopsRemoveAll = func(string) error { return sentinel } // stale-symlink branch
	fsopsRemove = func(string) error { return sentinel }    // occupying-entry branch
	t.Cleanup(func() { fsopsRemoveAll = origAll; fsopsRemove = origOne })

	// (a) symlink pointing elsewhere → RemoveAll path → injected error.
	elsewhere := filepath.Join(tmp, "elsewhere.txt")
	if err := os.WriteFile(elsewhere, []byte("e"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleLink := filepath.Join(tmp, "stale")
	if err := os.Symlink(elsewhere, staleLink); err != nil {
		t.Fatal(err)
	}
	if err := Symlink(target, staleLink); !errors.Is(err, sentinel) {
		t.Errorf("expected injected remove error for stale symlink, got %v", err)
	}

	// (b) empty squat dir → single-entry fsopsRemove → injected error.
	emptyDir := filepath.Join(tmp, "emptydir")
	if err := os.Mkdir(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Symlink(target, emptyDir); !errors.Is(err, sentinel) {
		t.Errorf("expected injected remove error for empty squat dir, got %v", err)
	}

	// (c) SymlinkReplacing over an unmanaged regular file: backup ok →
	//     fsopsRemoveAll injected error (entry left for the caller).
	occupied := filepath.Join(tmp, "occupied")
	if err := os.WriteFile(occupied, []byte("o"), 0o644); err != nil {
		t.Fatal(err)
	}
	bkErr := SymlinkReplacing(target, occupied, func(string) error { return nil })
	if !errors.Is(bkErr, sentinel) {
		t.Errorf("expected injected remove error after backup, got %v", bkErr)
	}
}

// TestHandleUnmanagedOccupant_LstatRealErrorSurfaces exercises the
// should-be-LOUD fix: a real (non-NotExist) Lstat failure must not be read
// as "vanished, nothing to protect" — the caller would otherwise proceed to
// overwrite an entry it never actually inspected.
func TestHandleUnmanagedOccupant_LstatRealErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	occupant := filepath.Join(parent, "occupant")
	if err := os.WriteFile(occupant, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, parent)

	err := handleUnmanagedOccupant(occupant, nil)
	if err == nil {
		t.Fatal("want a surfaced error for a real Lstat failure, not a silent nil")
	}
	if errors.Is(err, ErrUnmanagedTarget) {
		t.Errorf("a real Lstat error must not be reported as ErrUnmanagedTarget: %v", err)
	}
}

// TestHandleUnmanagedOccupant_AbsentPathIsNoop covers the legitimate-absence
// case unchanged by the fix: a path that genuinely vanished has nothing to
// protect.
func TestHandleUnmanagedOccupant_AbsentPathIsNoop(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if err := handleUnmanagedOccupant(missing, nil); err != nil {
		t.Fatalf("want nil for a legitimately absent path, got %v", err)
	}
}

// TestIsManagedFileLink_RealLstatErrorFailsSafe exercises the should-be-LOUD
// fix: a real Lstat failure must not be reported as "not ours, safe to
// delete" (the unsafe direction platform/hooks.go and platform/claude.go
// gate destructive decisions on) — it must fail safe as "protected."
func TestIsManagedFileLink_RealLstatErrorFailsSafe(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, parent)

	if !IsManagedFileLink(target) {
		t.Error("a real Lstat error must fail safe (report true/protected), not the unsafe false")
	}
}
