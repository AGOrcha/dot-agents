package testutil_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// The behaviour MakeDirWriteDenied exists to guarantee: after the helper
// runs, attempts to create a new child or delete an existing child of the
// target directory fail. POSIX denies via EACCES at the unlink/open syscall;
// Windows denies via ERROR_ACCESS_DENIED through a deny-ACE on the DACL.
// Both surface as fs.ErrPermission to portable Go callers — see
// MakeDirWriteDenied's godoc.
func TestMakeDirWriteDeniedBlocksChildCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	testutil.MakeDirWriteDenied(t, dir)

	child := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(child, []byte("nope"), 0o644); err == nil {
		_ = os.Remove(child)
		t.Fatalf("os.WriteFile succeeded after MakeDirWriteDenied; expected denial")
	} else if !errors.Is(err, fs.ErrPermission) {
		// Not fatal: some platforms map the denial to a different sentinel.
		// We accept any non-nil error per the helper's documented contract,
		// but call out the surprise so the cross-platform mapping stays
		// honest.
		t.Logf("os.WriteFile denied with non-permission error (still acceptable per contract): %v", err)
	}
}

// MakeDirWriteDenied must also block deletion of an existing child — that is
// the primary use case (test asserts os.Remove / os.RemoveAll surfaces an
// error because the parent is locked).
func TestMakeDirWriteDeniedBlocksChildRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	child := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(child, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	testutil.MakeDirWriteDenied(t, dir)

	if err := os.Remove(child); err == nil {
		t.Fatalf("os.Remove succeeded after MakeDirWriteDenied; expected denial")
	} else if !errors.Is(err, fs.ErrPermission) {
		t.Logf("os.Remove denied with non-permission error (still acceptable per contract): %v", err)
	}
}

// Read-side access must remain functional — that is the explicit difference
// versus MakeDirUnreadable. Tests rely on being able to confirm the
// pre-existing contents after the write-denial assertion.
func TestMakeDirWriteDeniedPreservesReadAccess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	child := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(child, []byte("payload"), 0o644); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	testutil.MakeDirWriteDenied(t, dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir failed under MakeDirWriteDenied (read access should be preserved): %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep.txt" {
		t.Fatalf("unexpected listing after MakeDirWriteDenied: %v", entries)
	}
	data, err := os.ReadFile(child)
	if err != nil {
		t.Fatalf("os.ReadFile failed under MakeDirWriteDenied (read access should be preserved): %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("child contents corrupted: got %q want %q", data, "payload")
	}
}

// After the test that called MakeDirWriteDenied returns, the surrounding
// TempDir teardown must succeed — i.e. the helper's t.Cleanup must restore
// enough access for the runtime to remove the directory and its children.
// This exercises that by running a child subtest, then verifying we can
// re-write into and remove the directory from the parent context.
func TestMakeDirWriteDeniedCleansUp(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "transient")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	t.Run("denied", func(sub *testing.T) {
		testutil.MakeDirWriteDenied(sub, dir)
		if err := os.WriteFile(filepath.Join(dir, "nope.txt"), []byte("x"), 0o644); err == nil {
			sub.Fatal("os.WriteFile succeeded; expected denial during subtest")
		}
	})

	// After the subtest's t.Cleanup ran, the parent must be able to write
	// into the directory and remove it — this is what makes t.TempDir
	// teardown safe.
	if err := os.WriteFile(filepath.Join(dir, "after.txt"), []byte("y"), 0o644); err != nil {
		t.Fatalf("after subtest cleanup, WriteFile failed: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("after subtest cleanup, RemoveAll failed: %v", err)
	}
}
