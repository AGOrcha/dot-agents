package testutil_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// The behaviour the helper exists to guarantee: after MakeFileUnreadable, the
// file's contents cannot be read. POSIX denies at open; Windows denies at
// read. Asserting via os.ReadFile (which does both) is the cross-platform
// contract — see MakeFileUnreadable's godoc.
func TestMakeFileUnreadableBlocksReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	testutil.MakeFileUnreadable(t, path)

	data, err := os.ReadFile(path)
	if err == nil {
		t.Fatalf("os.ReadFile succeeded after MakeFileUnreadable; expected denial. Got: %q", data)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.ReadFile reported file-not-exist, but the fixture is still on disk: %v", err)
	}
}

// After the test that called MakeFileUnreadable returns, the surrounding
// TempDir teardown must succeed — i.e. the helper's t.Cleanup must restore
// enough access for the runtime to remove the file. This test exercises that
// by running a child subtest, then verifying the file is gone (and removable)
// from the parent context.
func TestMakeFileUnreadableCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transient.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Run("denied", func(sub *testing.T) {
		testutil.MakeFileUnreadable(sub, path)
		if _, err := os.ReadFile(path); err == nil {
			sub.Fatal("os.ReadFile succeeded; expected denial during subtest")
		}
	})

	// After the subtest's t.Cleanup ran, the parent must be able to read and
	// then remove the file — this is what makes t.TempDir teardown safe.
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("after subtest cleanup, ReadFile failed: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("after subtest cleanup, Remove failed: %v", err)
	}
}
