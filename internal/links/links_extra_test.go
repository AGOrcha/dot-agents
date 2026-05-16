package links_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
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
	if err := links.Symlink(target, linkPath); err != nil {
		t.Fatalf("Symlink should replace regular file, got %v", err)
	}
	if !links.IsManagedLink(linkPath, target) {
		t.Errorf("expected %s to be a managed link to %s after replacing regular file", linkPath, target)
	}
}

func TestSymlinkParentDirCreation(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "tgt.txt")
	os.WriteFile(target, []byte("x"), 0644)
	linkPath := filepath.Join(tmp, "nested", "deep", "link.txt")
	if err := links.Symlink(target, linkPath); err != nil {
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

	if err := links.Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink replace: %v", err)
	}
	linked, err := links.AreHardlinked(src, dst)
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
	if err := links.Hardlink(src, dst); err != nil {
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
	linktest.Link(t, target, link)
	if !links.IsSymlinkTo(link, target) {
		t.Error("should report symlink to target")
	}
	if links.IsSymlinkTo(link, other) {
		t.Error("should not match unrelated target")
	}
	if links.IsSymlinkTo(filepath.Join(tmp, "missing"), target) {
		t.Error("missing path should return false")
	}
	// non-symlink (regular file)
	if links.IsSymlinkTo(target, target) {
		t.Error("regular file should not appear as symlink")
	}
}

func TestRemoveIfSymlinkUnder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("RemoveIfSymlinkUnder matches a resolvable link's target against a prefix; on Windows a file managed link is a hard link with no reparse point, so there is no resolvable target to compare. Windows path covered by internal/linktest/linktest_test.go")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(agentsHome, "x.md")
	os.WriteFile(target, []byte("x"), 0644)

	link := filepath.Join(tmp, "link.md")
	linktest.Link(t, target, link)

	if err := links.RemoveIfSymlinkUnder(link, agentsHome); err != nil {
		t.Fatalf("RemoveIfSymlinkUnder: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("link should be removed")
	}

	// Non-matching prefix → no-op
	other := filepath.Join(tmp, "other.md")
	os.WriteFile(target, []byte("x"), 0644)
	linktest.Link(t, target, other)
	if err := links.RemoveIfSymlinkUnder(other, "/elsewhere"); err != nil {
		t.Errorf("non-matching prefix: %v", err)
	}
	if _, err := os.Lstat(other); err != nil {
		t.Error("non-matching link should remain")
	}

	// Plain file (not a symlink) → no-op even with matching prefix
	plain := filepath.Join(tmp, "plain.md")
	os.WriteFile(plain, []byte("plain"), 0644)
	if err := links.RemoveIfSymlinkUnder(plain, agentsHome); err != nil {
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
	if !links.IsDirEntry(dir) {
		t.Error("dir should be reported as dir")
	}

	file := filepath.Join(tmp, "file.txt")
	os.WriteFile(file, []byte("x"), 0644)
	if links.IsDirEntry(file) {
		t.Error("file should not be reported as dir")
	}

	// Managed link to dir → true (Stat follows the link)
	link := filepath.Join(tmp, "linkdir")
	linktest.Link(t, dir, link)
	if !links.IsDirEntry(link) {
		t.Error("symlink to dir should be reported as dir")
	}

	// Missing path
	if links.IsDirEntry(filepath.Join(tmp, "nope")) {
		t.Error("missing path should return false")
	}
}

func TestAreHardlinkedMissingFiles(t *testing.T) {
	tmp := t.TempDir()
	_, err := links.AreHardlinked(filepath.Join(tmp, "nope"), filepath.Join(tmp, "also-nope"))
	if err == nil {
		t.Error("expected error for missing files")
	}
}

func TestSymlinkRefusesUnmanagedNonEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	os.WriteFile(target, []byte("x"), 0644)

	// linkPath is an UNMANAGED non-empty directory holding local work.
	// Symlink must NOT recursively delete it (the prior fsops.RemoveAll
	// behavior was an irreversible-data-loss path flagged by adversarial
	// review). It must refuse with an actionable error and leave the
	// directory and its contents intact. (Managed junctions are
	// symlink-class and replaced via the Readlink branch; a file or an
	// empty squat is still replaced — covered elsewhere.)
	linkPath := filepath.Join(tmp, "linkdir")
	os.MkdirAll(linkPath, 0755)
	child := filepath.Join(linkPath, "child")
	os.WriteFile(child, []byte("y"), 0644)

	err := links.Symlink(target, linkPath)
	if err == nil {
		t.Fatal("expected Symlink to refuse replacing an unmanaged non-empty dir")
	}
	if !strings.Contains(err.Error(), "non-empty directory") {
		t.Errorf("error should explain the refusal, got: %v", err)
	}
	if b, rerr := os.ReadFile(child); rerr != nil || string(b) != "y" {
		t.Errorf("user data must be preserved; child read=%q err=%v", string(b), rerr)
	}
	if links.IsSymlinkTo(linkPath, target) {
		t.Error("linkPath must NOT have been converted to a managed link")
	}
}

func TestSymlinkReplacesEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	os.WriteFile(target, []byte("x"), 0644)
	// An EMPTY squatting dir (e.g. left by a prior aborted run) is safe to
	// replace — no data loss — so idempotent re-link still works.
	linkPath := filepath.Join(tmp, "emptydir")
	os.MkdirAll(linkPath, 0755)
	if err := links.Symlink(target, linkPath); err != nil {
		t.Fatalf("expected Symlink to replace an empty squat dir, got: %v", err)
	}
	if !links.IsSymlinkTo(linkPath, target) {
		t.Errorf("expected %s to be a managed link to %s", linkPath, target)
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
	if err := links.Symlink(target, linkPath); err == nil {
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
	if err := links.Hardlink(src, dst); err == nil {
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
	if err := links.Hardlink(src, dst); err == nil {
		t.Error("expected MkdirAll failure when parent is a regular file")
	}
}

func TestSymlinkIdempotentExisting(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	os.WriteFile(target, []byte("x"), 0644)
	link := filepath.Join(tmp, "link")
	if err := links.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	// Second call: detected as already-correct
	if err := links.Symlink(target, link); err != nil {
		t.Errorf("idempotent call: %v", err)
	}
}
