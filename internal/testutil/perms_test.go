package testutil_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// The behaviour the helper exists to guarantee: after MakeFileUnreadable, the
// same os.Open call the test target uses returns an error. The test runs on
// every platform we ship for; CI on ubuntu-latest, macos-latest, and
// windows-latest all exercise the same assertion through the platform-specific
// implementation under the build tags.
func TestMakeFileUnreadableBlocksOsOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	testutil.MakeFileUnreadable(t, path)

	f, err := os.Open(path)
	if err == nil {
		_ = f.Close()
		t.Fatal("os.Open succeeded after MakeFileUnreadable; expected denial")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Open reported file-not-exist, but the fixture is still on disk: %v", err)
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
		if _, err := os.Open(path); err == nil {
			sub.Fatal("os.Open succeeded; expected denial during subtest")
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
