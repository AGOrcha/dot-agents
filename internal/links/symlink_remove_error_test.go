package links

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestSymlink_DanglingButCorrectIsNoop covers the existing==target return
// when the SameFile fast-path cannot fire because the target does not
// exist (a dangling symlink whose stored text already equals target).
func TestSymlink_DanglingButCorrectIsNoop(t *testing.T) {
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

// TestSymlink_RemoveAllErrorBranches fault-injects fsopsRemoveAll to cover
// the two "failed to remove" error returns: (a) an existing symlink that
// points elsewhere, (b) an existing regular file occupying linkPath.
func TestSymlink_RemoveAllErrorBranches(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target.txt")
	if err := os.WriteFile(target, []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("injected remove failure")
	orig := fsopsRemoveAll
	fsopsRemoveAll = func(string) error { return sentinel }
	t.Cleanup(func() { fsopsRemoveAll = orig })

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

	// (b) regular file occupying linkPath → Lstat ok → RemoveAll → error.
	occupied := filepath.Join(tmp, "occupied")
	if err := os.WriteFile(occupied, []byte("o"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Symlink(target, occupied); !errors.Is(err, sentinel) {
		t.Errorf("expected injected remove error for occupying file, got %v", err)
	}
}
