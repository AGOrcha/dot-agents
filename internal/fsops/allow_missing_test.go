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

// The three helpers share one contract — absent -> (zero,false,nil), real
// error -> (zero,false,err), present -> (value,true,nil) — so the found/err
// half of every assertion is factored into these helpers. Keeping the
// multi-branch checks out of the test bodies holds each Test* function's
// cognitive complexity under the S3776 limit while still exercising all three
// branches of every helper (only the type-specific value check stays inline).

func assertAbsent(t *testing.T, found bool, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected nil error for a legitimately-absent path, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false for a legitimately-absent path")
	}
}

func assertRealError(t *testing.T, found bool, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a real error to be surfaced, not conflated with absence")
	}
	if found {
		t.Fatalf("expected found=false when a real error is surfaced")
	}
}

func assertPresent(t *testing.T, found bool, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error for a present path: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true for a present path")
	}
}

func seedFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed file %s: %v", path, err)
	}
}

func seedDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("seed dir %s: %v", path, err)
	}
}

func TestReadFileAllowMissing(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		data, found, err := fsops.ReadFileAllowMissing(filepath.Join(t.TempDir(), "absent.txt"))
		assertAbsent(t, found, err)
		if data != nil {
			t.Fatalf("expected nil data for a missing file, got %q", data)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "denied.txt")
		seedFile(t, path, "secret")
		testutil.MakeFileUnreadable(t, path)

		data, found, err := fsops.ReadFileAllowMissing(path)
		assertRealError(t, found, err)
		if data != nil {
			t.Fatalf("expected nil data for a permission-denied file, got %q", data)
		}
	})

	t.Run("normal", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "present.txt")
		seedFile(t, path, "payload")

		data, found, err := fsops.ReadFileAllowMissing(path)
		assertPresent(t, found, err)
		if string(data) != "payload" {
			t.Fatalf("expected data=%q, got %q", "payload", data)
		}
	})
}

func TestReadDirAllowMissing(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		entries, found, err := fsops.ReadDirAllowMissing(filepath.Join(t.TempDir(), "absent"))
		assertAbsent(t, found, err)
		if entries != nil {
			t.Fatalf("expected nil entries for a missing dir, got %v", entries)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "denied")
		seedDir(t, dir)
		testutil.MakeDirUnreadable(t, dir)

		entries, found, err := fsops.ReadDirAllowMissing(dir)
		assertRealError(t, found, err)
		if entries != nil {
			t.Fatalf("expected nil entries for a permission-denied dir, got %v", entries)
		}
	})

	t.Run("normal", func(t *testing.T) {
		dir := t.TempDir()
		seedFile(t, filepath.Join(dir, "child.txt"), "x")

		entries, found, err := fsops.ReadDirAllowMissing(dir)
		assertPresent(t, found, err)
		if len(entries) != 1 || entries[0].Name() != "child.txt" {
			t.Fatalf("expected single child.txt entry, got %v", entries)
		}
	})
}

func TestStatAllowMissing(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		info, found, err := fsops.StatAllowMissing(filepath.Join(t.TempDir(), "absent"))
		assertAbsent(t, found, err)
		if info != nil {
			t.Fatalf("expected nil info for a missing path, got %v", info)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		// os.Stat on a path itself only needs execute/search permission on its
		// PARENT directory, not on the path's own mode bits, so chmod-0'ing the
		// target directly does not fail a Stat of that directory. To force a
		// genuine Stat error we deny traversal into the PARENT and stat a child
		// underneath it, mirroring fsops_windows_test.go's
		// *_UnderUnreadableParent convention.
		sub := filepath.Join(t.TempDir(), "sub")
		seedDir(t, sub)
		child := filepath.Join(sub, "child.txt")
		seedFile(t, child, "x")
		testutil.MakeDirUnreadable(t, sub)

		info, found, err := fsops.StatAllowMissing(child)
		assertRealError(t, found, err)
		if info != nil {
			t.Fatalf("expected nil info for a path under an unreadable parent, got %v", info)
		}
	})

	t.Run("normal", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "present.txt")
		seedFile(t, path, "x")

		info, found, err := fsops.StatAllowMissing(path)
		assertPresent(t, found, err)
		if info == nil || info.Name() != "present.txt" {
			t.Fatalf("expected info for present.txt, got %v", info)
		}
	})
}
