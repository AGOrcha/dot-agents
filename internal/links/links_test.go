package links_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
)

func TestSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target.txt")
	linkPath := filepath.Join(tmp, "link.txt")

	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink
	if err := links.Symlink(target, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// Verify it is a managed link resolving to target.
	if !links.IsManagedLink(linkPath, target) {
		t.Errorf("expected %s to be a managed link to %s", linkPath, target)
	}

	// Idempotent — calling again should be a no-op
	if err := links.Symlink(target, linkPath); err != nil {
		t.Fatalf("Symlink (idempotent): %v", err)
	}
}

func TestSymlinkUpdatesStaleLink(t *testing.T) {
	tmp := t.TempDir()
	target1 := filepath.Join(tmp, "t1.txt")
	target2 := filepath.Join(tmp, "t2.txt")
	linkPath := filepath.Join(tmp, "link.txt")

	os.WriteFile(target1, []byte("a"), 0644)
	os.WriteFile(target2, []byte("b"), 0644)

	links.Symlink(target1, linkPath)

	// Update to new target
	if err := links.Symlink(target2, linkPath); err != nil {
		t.Fatalf("Symlink update: %v", err)
	}

	if !links.IsManagedLink(linkPath, target2) {
		t.Errorf("expected updated link to resolve to %s", target2)
	}
	if links.IsManagedLink(linkPath, target1) {
		t.Errorf("link should no longer resolve to stale target %s", target1)
	}
}

func TestHardlink(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")

	if err := os.WriteFile(src, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := links.Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink: %v", err)
	}

	// Verify same inode
	linked, err := links.AreHardlinked(src, dst)
	if err != nil {
		t.Fatalf("AreHardlinked: %v", err)
	}
	if !linked {
		t.Error("expected src and dst to be hard-linked")
	}

	// Idempotent
	if err := links.Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink (idempotent): %v", err)
	}
}

func TestAreHardlinkedNegative(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.txt")
	b := filepath.Join(tmp, "b.txt")

	os.WriteFile(a, []byte("a"), 0644)
	os.WriteFile(b, []byte("b"), 0644)

	linked, err := links.AreHardlinked(a, b)
	if err != nil {
		t.Fatalf("AreHardlinked: %v", err)
	}
	if linked {
		t.Error("distinct files should not be hard-linked")
	}
}

func TestFindFile(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "rules")

	// Create rules.mdc
	os.WriteFile(base+".mdc", []byte("content"), 0644)

	found := links.FindFile(base, []string{"md", "mdc", "txt"})
	if found != base+".mdc" {
		t.Errorf("expected %s.mdc, got %s", base, found)
	}

	// Non-existent
	found2 := links.FindFile(filepath.Join(tmp, "missing"), []string{"md"})
	if found2 != "" {
		t.Errorf("expected empty string for missing file, got %s", found2)
	}
}

func TestIsSymlinkUnder(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	target := filepath.Join(agentsHome, "rules", "global", "rules.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("rules"), 0644)

	linkPath := filepath.Join(tmp, "link.md")
	linktest.Link(t, target, linkPath)

	if !links.IsSymlinkUnder(linkPath, agentsHome) {
		t.Error("expected link to be under agentsHome")
	}
	if links.IsSymlinkUnder(linkPath, "/some/other/path") {
		t.Error("should not match different prefix")
	}
}
