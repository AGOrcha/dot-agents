package testutil_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// The behaviour the helper exists to guarantee: after MakeFileReadOnly, the
// file rejects writes. os.WriteFile is the cross-platform contract — on POSIX
// the kernel denies at open via EACCES; on Windows the runtime sees
// FILE_ATTRIBUTE_READONLY and rejects O_WRONLY with ERROR_ACCESS_DENIED. Both
// surface as a non-nil error from os.WriteFile.
func TestMakeFileReadOnlyBlocksWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	testutil.MakeFileReadOnly(t, path)

	err := os.WriteFile(path, []byte("overwritten"), 0o644)
	if err == nil {
		t.Fatalf("os.WriteFile succeeded after MakeFileReadOnly; expected denial")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.WriteFile reported file-not-exist, but the fixture is still on disk: %v", err)
	}

	// Reads must still succeed — the helper only denies writes. This locks
	// the helper into its "read-only" contract: if someone later changes the
	// implementation to also deny reads, this assertion fails and forces a
	// rename to MakeFileUnreadable (which already exists for that case).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile failed after MakeFileReadOnly; helper must deny writes only: %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("file contents mutated despite write denial: got %q want %q", data, "original")
	}
}

// After the subtest that called MakeFileReadOnly returns, the parent must be
// able to write to and remove the file — this is what makes t.TempDir
// teardown safe on Windows, where the readonly attribute would otherwise
// block os.Remove.
func TestMakeFileReadOnlyCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transient.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Run("denied", func(sub *testing.T) {
		testutil.MakeFileReadOnly(sub, path)
		if err := os.WriteFile(path, []byte("y"), 0o644); err == nil {
			sub.Fatal("os.WriteFile succeeded; expected denial during subtest")
		}
	})

	if err := os.WriteFile(path, []byte("z"), 0o644); err != nil {
		t.Fatalf("after subtest cleanup, WriteFile failed: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("after subtest cleanup, Remove failed: %v", err)
	}
}
