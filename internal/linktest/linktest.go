// Package linktest provides cross-platform managed-link fixtures for tests.
//
// dot-agents historically created link fixtures with os.Symlink, which
// fails on Windows without SeCreateSymbolicLinkPrivilege (Developer Mode /
// admin). The production link model uses junctions (dirs) and hard links
// (files) on Windows; test fixtures must mirror that so the contract
// checks (links.IsManagedLink / CollectBrokenUserLinks / doctor repair)
// see the same shapes they will in the field.
//
// Use Link for a valid managed reference and DanglingLink for a broken one.
package linktest

import (
	"os"
	"path/filepath"
	"testing"
)

// Link creates a managed link at link pointing to target. target MUST
// already exist. POSIX: a symlink. Windows: a directory junction when
// target is a directory, a hard link when target is a file. The parent of
// link is created if necessary.
func Link(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("linktest.Link: mkdir parent of %s: %v", link, err)
	}
	if err := createManagedLink(target, link); err != nil {
		t.Fatalf("linktest.Link: %s -> %s: %v", link, target, err)
	}
}

// DanglingLink creates a broken managed link at link and returns the
// absolute target path it references, which does not exist (so the link is
// "dangling"). Callers that assert the resolved target should compare
// against the returned value rather than a hard-coded path, since the
// Windows junction mechanism dictates the concrete target location.
//
// POSIX: a symlink to a never-created path under a temp dir.
// Windows: a junction created against a real temp dir which is then
// removed, leaving a reparse point whose target is gone — the closest
// analogue, since a hard link cannot dangle (its target must exist).
func DanglingLink(t *testing.T, link string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("linktest.DanglingLink: mkdir parent of %s: %v", link, err)
	}
	missing := filepath.Join(t.TempDir(), "dangling-target")
	abs, err := filepath.Abs(missing)
	if err != nil {
		abs = missing
	}
	if err := createDanglingLink(link, abs); err != nil {
		t.Fatalf("linktest.DanglingLink: %s: %v", link, err)
	}
	return abs
}
