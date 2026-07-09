// Package fsops_test exercises the allow-missing helpers from an external
// test package, deliberately NOT `package fsops` (the convention every other
// _test.go file in this directory uses). internal/testutil imports
// internal/config, which imports internal/fsops — reusing
// testutil.MakeFileUnreadable/MakeDirUnreadable from an in-package (white-box)
// fsops test file would close that back into a self-import cycle on the
// fsops test build (see fsops_windows_test.go's makeDirUnreadable comment,
// which hits the same constraint and inlines a local copy instead). An
// external test package has a distinct import identity from fsops itself, so
// fsops_test -> testutil -> config -> fsops (production) is not a cycle, and
// we get to reuse the shared fixtures instead of duplicating them.
package fsops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

func TestReadFileAllowMissing(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		dir := t.TempDir()
		data, found, err := fsops.ReadFileAllowMissing(filepath.Join(dir, "absent.txt"))
		if err != nil {
			t.Fatalf("expected nil error for missing file, got %v", err)
		}
		if found {
			t.Fatalf("expected found=false for missing file")
		}
		if data != nil {
			t.Fatalf("expected nil data for missing file, got %q", data)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "denied.txt")
		if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
		testutil.MakeFileUnreadable(t, path)

		data, found, err := fsops.ReadFileAllowMissing(path)
		if err == nil {
			t.Fatalf("expected a real error for permission-denied file")
		}
		if found {
			t.Fatalf("expected found=false for permission-denied file")
		}
		if data != nil {
			t.Fatalf("expected nil data for permission-denied file, got %q", data)
		}
	})

	t.Run("normal", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "present.txt")
		if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}

		data, found, err := fsops.ReadFileAllowMissing(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatalf("expected found=true for present file")
		}
		if string(data) != "payload" {
			t.Fatalf("expected data=%q, got %q", "payload", data)
		}
	})
}

func TestReadDirAllowMissing(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		dir := t.TempDir()
		entries, found, err := fsops.ReadDirAllowMissing(filepath.Join(dir, "absent"))
		if err != nil {
			t.Fatalf("expected nil error for missing dir, got %v", err)
		}
		if found {
			t.Fatalf("expected found=false for missing dir")
		}
		if entries != nil {
			t.Fatalf("expected nil entries for missing dir, got %v", entries)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "denied")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("seed dir: %v", err)
		}
		testutil.MakeDirUnreadable(t, dir)

		entries, found, err := fsops.ReadDirAllowMissing(dir)
		if err == nil {
			t.Fatalf("expected a real error for permission-denied dir")
		}
		if found {
			t.Fatalf("expected found=false for permission-denied dir")
		}
		if entries != nil {
			t.Fatalf("expected nil entries for permission-denied dir, got %v", entries)
		}
	})

	t.Run("normal", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "child.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed child: %v", err)
		}

		entries, found, err := fsops.ReadDirAllowMissing(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatalf("expected found=true for present dir")
		}
		if len(entries) != 1 || entries[0].Name() != "child.txt" {
			t.Fatalf("expected single child.txt entry, got %v", entries)
		}
	})
}

func TestStatAllowMissing(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		dir := t.TempDir()
		info, found, err := fsops.StatAllowMissing(filepath.Join(dir, "absent"))
		if err != nil {
			t.Fatalf("expected nil error for missing path, got %v", err)
		}
		if found {
			t.Fatalf("expected found=false for missing path")
		}
		if info != nil {
			t.Fatalf("expected nil info for missing path, got %v", info)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		// os.Stat on a path itself only needs execute/search permission on
		// its PARENT directory, not on the path's own mode bits, so
		// chmod-0'ing the target directly does not fail a Stat of that
		// directory. To force a genuine Stat error we deny traversal into
		// the PARENT and stat a child underneath it, mirroring
		// fsops_windows_test.go's *_UnderUnreadableParent convention.
		parent := t.TempDir()
		sub := filepath.Join(parent, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatalf("seed sub dir: %v", err)
		}
		child := filepath.Join(sub, "child.txt")
		if err := os.WriteFile(child, []byte("x"), 0o644); err != nil {
			t.Fatalf("seed child: %v", err)
		}
		testutil.MakeDirUnreadable(t, sub)

		info, found, err := fsops.StatAllowMissing(child)
		if err == nil {
			t.Fatalf("expected a real error for a path under an unreadable parent")
		}
		if found {
			t.Fatalf("expected found=false for a path under an unreadable parent")
		}
		if info != nil {
			t.Fatalf("expected nil info for a path under an unreadable parent, got %v", info)
		}
	})

	t.Run("normal", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "present.txt")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}

		info, found, err := fsops.StatAllowMissing(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatalf("expected found=true for present path")
		}
		if info == nil || info.Name() != "present.txt" {
			t.Fatalf("expected info for present.txt, got %v", info)
		}
	})
}
