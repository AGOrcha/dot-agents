package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRename(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old")
	newPath := filepath.Join(dir, "new")
	if err := os.Mkdir(oldPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Rename(oldPath, newPath); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("target absent after rename: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("source still present after rename: stat err=%v", err)
	}
	// Single-shot contract: a missing source surfaces the error unchanged (no
	// retry, no fallback) so lock-lifecycle callers can distinguish "lost the
	// rename race" from success.
	if err := Rename(oldPath, filepath.Join(dir, "other")); !os.IsNotExist(err) {
		t.Fatalf("expected IsNotExist from renaming a missing source, got: %v", err)
	}
}
