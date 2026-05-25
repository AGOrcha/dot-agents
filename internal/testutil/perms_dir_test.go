package testutil_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// The behaviour MakeDirUnreadable exists to guarantee: after the helper runs,
// os.ReadDir against the target fails. POSIX denies via EACCES at the
// readdir syscall; Windows denies via ERROR_ACCESS_DENIED through a deny-ACE
// on the DACL. Both surface as fs.ErrPermission to portable Go callers — see
// MakeDirUnreadable's godoc.
func TestMakeDirUnreadableBlocksReadDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	// Drop a file inside so a successful ReadDir would return a non-empty
	// listing — that makes a silent denial bypass loud (we would see the
	// listing instead of an error).
	if err := os.WriteFile(filepath.Join(dir, "child.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write child: %v", err)
	}

	testutil.MakeDirUnreadable(t, dir)

	entries, err := os.ReadDir(dir)
	if err == nil {
		t.Fatalf("os.ReadDir succeeded after MakeDirUnreadable; expected denial. Got %d entries", len(entries))
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.ReadDir reported dir-not-exist, but the fixture is still on disk: %v", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		// Not fatal: some platforms map the denial to a different sentinel.
		// We accept any non-nil error per the helper's documented contract,
		// but call out the surprise so the cross-platform mapping stays honest.
		t.Logf("os.ReadDir denied with non-permission error (still acceptable per contract): %v", err)
	}
}

// After the test that called MakeDirUnreadable returns, the surrounding
// TempDir teardown must succeed — i.e. the helper's t.Cleanup must restore
// enough access for the runtime to remove the directory and its children.
// This exercises that by running a child subtest, then verifying we can
// re-read and remove the directory from the parent context.
func TestMakeDirUnreadableCleansUp(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "transient")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	child := filepath.Join(dir, "inside.txt")
	if err := os.WriteFile(child, []byte("y"), 0o644); err != nil {
		t.Fatalf("write child: %v", err)
	}

	t.Run("denied", func(sub *testing.T) {
		testutil.MakeDirUnreadable(sub, dir)
		if _, err := os.ReadDir(dir); err == nil {
			sub.Fatal("os.ReadDir succeeded; expected denial during subtest")
		}
	})

	// After the subtest's t.Cleanup ran, the parent must be able to list the
	// directory and remove it — this is what makes t.TempDir teardown safe.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("after subtest cleanup, ReadDir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "inside.txt" {
		t.Fatalf("after subtest cleanup, unexpected listing: %v", entries)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("after subtest cleanup, RemoveAll failed: %v", err)
	}
}
