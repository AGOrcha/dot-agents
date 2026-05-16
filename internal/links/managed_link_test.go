package links_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
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
	linktest.Link(t, target, sym)
	if !links.IsManagedLink(sym, target) {
		t.Error("symlink to target should be a managed link")
	}

	hard := filepath.Join(tmp, "via-hardlink")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	if !links.IsManagedLink(hard, target) {
		t.Error("hard link to target should be a managed link (shared inode)")
	}

	// A regular file compared to itself is not a link.
	if links.IsManagedLink(target, target) {
		t.Error("a file is not a managed link to itself")
	}

	// An unrelated regular file is not a managed link to target.
	other := filepath.Join(tmp, "other.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if links.IsManagedLink(other, target) {
		t.Error("unrelated file should not be a managed link to target")
	}

	// Missing path is not a managed link.
	if links.IsManagedLink(filepath.Join(tmp, "nope"), target) {
		t.Error("missing path should not be a managed link")
	}
}

// TestManagedLinkTarget_ResolvableVsHardlink documents that a symlink has a
// resolvable target while a hard link does not (no reparse point).
func TestManagedLinkTarget_ResolvableVsHardlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("asserts POSIX symlink has a resolvable target while a hard link does not; on Windows a file managed link is a hard link with no reparse point, so this distinction is not exercisable. Windows path covered by internal/linktest/linktest_test.go")
	}
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	if err := os.WriteFile(target, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	sym := filepath.Join(tmp, "s")
	linktest.Link(t, target, sym)
	if dest, ok := links.ManagedLinkTarget(sym); !ok || dest != target {
		t.Errorf("ManagedLinkTarget(symlink) = %q,%v; want %q,true", dest, ok, target)
	}

	hard := filepath.Join(tmp, "h")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	if _, ok := links.ManagedLinkTarget(hard); ok {
		t.Error("a hard link has no resolvable target; ManagedLinkTarget should report false")
	}
}

// TestIsManagedLinkUnder covers prefix matching on a resolvable link and the
// hard-link false case.
func TestIsManagedLinkUnder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("asserts a resolvable (symlink) target is under a prefix; on Windows a file managed link is a hard link with no reparse point, so IsManagedLinkUnder cannot resolve it. Windows path covered by internal/linktest/linktest_test.go")
	}
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
	linktest.Link(t, target, sym)
	if !links.IsManagedLinkUnder(sym, canonicalRoot) {
		t.Error("symlink resolving under canonicalRoot should match")
	}
	if links.IsManagedLinkUnder(sym, filepath.Join(tmp, "elsewhere")) {
		t.Error("should not match a prefix the target is not under")
	}

	hard := filepath.Join(tmp, "hardref")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	if links.IsManagedLinkUnder(hard, canonicalRoot) {
		t.Error("a hard link has no resolvable target; under-prefix must be false")
	}
}

// TestRemoveIfHardlinkedToAny covers the promoted cursor cleanup helper:
// removes path only when it is hard-linked to one of the candidate sources.
func TestRemoveIfHardlinkedToAny(t *testing.T) {
	tmp := t.TempDir()
	canonical := filepath.Join(tmp, "canonical.txt")
	if err := os.WriteFile(canonical, []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(tmp, "other.txt")
	if err := os.WriteFile(other, []byte("o"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Not hardlinked to any source → not removed, returns false.
	plain := filepath.Join(tmp, "plain.txt")
	if err := os.WriteFile(plain, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if links.RemoveIfHardlinkedToAny(plain, []string{canonical, other}) {
		t.Error("unrelated file should not be reported hardlinked")
	}
	if _, err := os.Stat(plain); err != nil {
		t.Error("unrelated file must remain")
	}

	// Hardlinked to a source → removed, returns true.
	managed := filepath.Join(tmp, "managed")
	if err := os.Link(canonical, managed); err != nil {
		t.Fatal(err)
	}
	if !links.RemoveIfHardlinkedToAny(managed, []string{other, canonical}) {
		t.Error("hard link to a candidate source should be detected and removed")
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Errorf("managed hard link should be removed, stat err=%v", err)
	}
}
