package links

import (
	"os"
	"path/filepath"
	"testing"
)

func assertLinkPointsToSameTarget(t *testing.T, gotPath, wantPath string) {
	t.Helper()
	gotInfo, err := os.Stat(gotPath)
	if err != nil {
		t.Fatalf("Stat(%s): %v", gotPath, err)
	}
	wantInfo, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("Stat(%s): %v", wantPath, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		if gotInfo.Mode().IsRegular() && wantInfo.Mode().IsRegular() {
			gotContent, err := os.ReadFile(gotPath)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", gotPath, err)
			}
			wantContent, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", wantPath, err)
			}
			if string(gotContent) == string(wantContent) {
				return
			}
		}
		t.Fatalf("expected %s to resolve to %s", gotPath, wantPath)
	}
}

func TestSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target.txt")
	linkPath := filepath.Join(tmp, "link.txt")

	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink
	if err := Symlink(target, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	assertLinkPointsToSameTarget(t, linkPath, target)

	// Idempotent — calling again should be a no-op
	if err := Symlink(target, linkPath); err != nil {
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

	Symlink(target1, linkPath)

	// Update to new target
	if err := Symlink(target2, linkPath); err != nil {
		t.Fatalf("Symlink update: %v", err)
	}

	assertLinkPointsToSameTarget(t, linkPath, target2)
}

func TestHardlink(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")

	if err := os.WriteFile(src, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink: %v", err)
	}

	// Verify same inode
	linked, err := AreHardlinked(src, dst)
	if err != nil {
		t.Fatalf("AreHardlinked: %v", err)
	}
	if !linked {
		t.Error("expected src and dst to be hard-linked")
	}

	// Idempotent
	if err := Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink (idempotent): %v", err)
	}
}

func TestAreHardlinkedNegative(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.txt")
	b := filepath.Join(tmp, "b.txt")

	os.WriteFile(a, []byte("a"), 0644)
	os.WriteFile(b, []byte("b"), 0644)

	linked, err := AreHardlinked(a, b)
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

	found := FindFile(base, []string{"md", "mdc", "txt"})
	if found != base+".mdc" {
		t.Errorf("expected %s.mdc, got %s", base, found)
	}

	// Non-existent
	found2 := FindFile(filepath.Join(tmp, "missing"), []string{"md"})
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
	os.Symlink(target, linkPath)

	if !IsSymlinkUnder(linkPath, agentsHome) {
		t.Error("expected link to be under agentsHome")
	}
	if IsSymlinkUnder(linkPath, "/some/other/path") {
		t.Error("should not match different prefix")
	}
}
