package links

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSymlink_DanglingButCorrectIsNoop covers the existing==target return
// when the SameFile fast-path cannot fire because the target does not
// exist (a dangling symlink whose stored text already equals target).
func TestSymlink_DanglingButCorrectIsNoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink primitive; Windows path covered by internal/linktest/linktest_test.go")
	}
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
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink primitive; Windows path covered by internal/linktest/linktest_test.go")
	}
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
