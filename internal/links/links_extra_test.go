package links

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinkReplacesRegularFile(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target.txt")
	if err := os.WriteFile(target, []byte("t"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(tmp, "link.txt")
	if err := os.WriteFile(linkPath, []byte("preexisting regular"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Symlink(target, linkPath); err != nil {
		t.Fatalf("Symlink should replace regular file, got %v", err)
	}
	dest, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if dest != target {
		t.Errorf("expected target %q, got %q", target, dest)
	}
}

func TestSymlinkParentDirCreation(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "tgt.txt")
	os.WriteFile(target, []byte("x"), 0644)
	linkPath := filepath.Join(tmp, "nested", "deep", "link.txt")
	if err := Symlink(target, linkPath); err != nil {
		t.Fatalf("Symlink should create parent dirs: %v", err)
	}
	if _, err := os.Lstat(linkPath); err != nil {
		t.Errorf("link not created: %v", err)
	}
}

func TestHardlinkReplacesExisting(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	os.WriteFile(src, []byte("src"), 0644)
	os.WriteFile(dst, []byte("preexisting"), 0644)

	if err := Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink replace: %v", err)
	}
	linked, err := AreHardlinked(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !linked {
		t.Error("expected linked after replace")
	}
}

func TestHardlinkParentDir(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	os.WriteFile(src, []byte("x"), 0644)
	dst := filepath.Join(tmp, "a", "b", "dst.txt")
	if err := Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink should create parents: %v", err)
	}
}

func TestIsSymlinkTo(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	other := filepath.Join(tmp, "o.txt")
	link := filepath.Join(tmp, "link.txt")
	os.WriteFile(target, []byte("t"), 0644)
	os.WriteFile(other, []byte("o"), 0644)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if !IsSymlinkTo(link, target) {
		t.Error("should report symlink to target")
	}
	if IsSymlinkTo(link, other) {
		t.Error("should not match unrelated target")
	}
	if IsSymlinkTo(filepath.Join(tmp, "missing"), target) {
		t.Error("missing path should return false")
	}
	// non-symlink (regular file)
	if IsSymlinkTo(target, target) {
		t.Error("regular file should not appear as symlink")
	}
}

func TestRemoveIfSymlinkUnder(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(agentsHome, "x.md")
	os.WriteFile(target, []byte("x"), 0644)

	link := filepath.Join(tmp, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := RemoveIfSymlinkUnder(link, agentsHome); err != nil {
		t.Fatalf("RemoveIfSymlinkUnder: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("link should be removed")
	}

	// Non-matching prefix → no-op
	other := filepath.Join(tmp, "other.md")
	os.WriteFile(target, []byte("x"), 0644)
	os.Symlink(target, other)
	if err := RemoveIfSymlinkUnder(other, "/elsewhere"); err != nil {
		t.Errorf("non-matching prefix: %v", err)
	}
	if _, err := os.Lstat(other); err != nil {
		t.Error("non-matching link should remain")
	}

	// Plain file (not a symlink) → no-op even with matching prefix
	plain := filepath.Join(tmp, "plain.md")
	os.WriteFile(plain, []byte("plain"), 0644)
	if err := RemoveIfSymlinkUnder(plain, agentsHome); err != nil {
		t.Errorf("plain file: %v", err)
	}
	if _, err := os.Lstat(plain); err != nil {
		t.Error("plain file should not be removed")
	}
}

func TestIsDirEntry(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "subdir")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if !IsDirEntry(dir) {
		t.Error("dir should be reported as dir")
	}

	file := filepath.Join(tmp, "file.txt")
	os.WriteFile(file, []byte("x"), 0644)
	if IsDirEntry(file) {
		t.Error("file should not be reported as dir")
	}

	// Symlink to dir → true (Stat follows symlinks)
	link := filepath.Join(tmp, "linkdir")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if !IsDirEntry(link) {
		t.Error("symlink to dir should be reported as dir")
	}

	// Missing path
	if IsDirEntry(filepath.Join(tmp, "nope")) {
		t.Error("missing path should return false")
	}
}

func TestAreHardlinkedMissingFiles(t *testing.T) {
	tmp := t.TempDir()
	_, err := AreHardlinked(filepath.Join(tmp, "nope"), filepath.Join(tmp, "also-nope"))
	if err == nil {
		t.Error("expected error for missing files")
	}
}

func TestSymlinkRemoveExistingDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	os.WriteFile(target, []byte("x"), 0644)

	// linkPath is a non-empty regular directory — Readlink fails (not IsNotExist),
	// Lstat succeeds, Remove fails (non-empty dir), exercising the error branch.
	linkPath := filepath.Join(tmp, "linkdir")
	os.MkdirAll(linkPath, 0755)
	os.WriteFile(filepath.Join(linkPath, "child"), []byte("y"), 0644)

	if err := Symlink(target, linkPath); err == nil {
		t.Error("expected error when linkPath is a non-empty dir that cannot be Remove()d")
	}
}

func TestSymlinkMkdirAllFails(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	os.WriteFile(target, []byte("x"), 0644)
	// Parent dir slot is occupied by a regular file → MkdirAll fails.
	conflict := filepath.Join(tmp, "file-as-dir")
	os.WriteFile(conflict, []byte("blocker"), 0644)
	linkPath := filepath.Join(conflict, "link.txt")
	if err := Symlink(target, linkPath); err == nil {
		t.Error("expected MkdirAll failure when parent is a regular file")
	}
}

func TestHardlinkRemoveExistingDirFails(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	os.WriteFile(src, []byte("x"), 0644)
	// dst is a non-empty dir — Remove will fail
	dst := filepath.Join(tmp, "dst-dir")
	os.MkdirAll(dst, 0755)
	os.WriteFile(filepath.Join(dst, "child"), []byte("y"), 0644)
	if err := Hardlink(src, dst); err == nil {
		t.Error("expected Hardlink failure on non-empty dir at dst")
	}
}

func TestHardlinkMkdirAllFails(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	os.WriteFile(src, []byte("x"), 0644)
	conflict := filepath.Join(tmp, "file-as-dir")
	os.WriteFile(conflict, []byte("blocker"), 0644)
	dst := filepath.Join(conflict, "dst")
	if err := Hardlink(src, dst); err == nil {
		t.Error("expected MkdirAll failure when parent is a regular file")
	}
}

func TestSymlinkIdempotentExisting(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	os.WriteFile(target, []byte("x"), 0644)
	link := filepath.Join(tmp, "link")
	if err := Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	// Second call: detected as already-correct
	if err := Symlink(target, link); err != nil {
		t.Errorf("idempotent call: %v", err)
	}
}
