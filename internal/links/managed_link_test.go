package links

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsManagedLink_SymlinkAndHardlink covers the two managed-reference
// shapes that are exercisable on POSIX: a symlink (resolvable target) and a
// hard link (shared inode, no reparse point). On Windows the symlink branch
// also covers junctions since os.Readlink resolves them.
func TestIsManagedLink_SymlinkAndHardlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "canonical.txt")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	sym := filepath.Join(tmp, "via-symlink")
	if err := os.Symlink(target, sym); err != nil {
		t.Fatal(err)
	}
	if !IsManagedLink(sym, target) {
		t.Error("symlink to target should be a managed link")
	}

	hard := filepath.Join(tmp, "via-hardlink")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	if !IsManagedLink(hard, target) {
		t.Error("hard link to target should be a managed link (shared inode)")
	}

	// A regular file compared to itself is not a link.
	if IsManagedLink(target, target) {
		t.Error("a file is not a managed link to itself")
	}

	// An unrelated regular file is not a managed link to target.
	other := filepath.Join(tmp, "other.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsManagedLink(other, target) {
		t.Error("unrelated file should not be a managed link to target")
	}

	// Missing path is not a managed link.
	if IsManagedLink(filepath.Join(tmp, "nope"), target) {
		t.Error("missing path should not be a managed link")
	}
}

// TestManagedLinkTarget_ResolvableVsHardlink documents that a symlink has a
// resolvable target while a hard link does not (no reparse point).
func TestManagedLinkTarget_ResolvableVsHardlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	if err := os.WriteFile(target, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	sym := filepath.Join(tmp, "s")
	if err := os.Symlink(target, sym); err != nil {
		t.Fatal(err)
	}
	if dest, ok := ManagedLinkTarget(sym); !ok || dest != target {
		t.Errorf("ManagedLinkTarget(symlink) = %q,%v; want %q,true", dest, ok, target)
	}

	hard := filepath.Join(tmp, "h")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	if _, ok := ManagedLinkTarget(hard); ok {
		t.Error("a hard link has no resolvable target; ManagedLinkTarget should report false")
	}
}

// TestIsManagedLinkUnder covers prefix matching on a resolvable link and the
// hard-link false case.
func TestIsManagedLinkUnder(t *testing.T) {
	tmp := t.TempDir()
	canonicalRoot := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(canonicalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(canonicalRoot, "rules", "agents.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}

	sym := filepath.Join(tmp, "AGENTS.md")
	if err := os.Symlink(target, sym); err != nil {
		t.Fatal(err)
	}
	if !IsManagedLinkUnder(sym, canonicalRoot) {
		t.Error("symlink resolving under canonicalRoot should match")
	}
	if IsManagedLinkUnder(sym, filepath.Join(tmp, "elsewhere")) {
		t.Error("should not match a prefix the target is not under")
	}

	hard := filepath.Join(tmp, "hardref")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	if IsManagedLinkUnder(hard, canonicalRoot) {
		t.Error("a hard link has no resolvable target; under-prefix must be false")
	}
}
