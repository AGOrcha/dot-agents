package links_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/testutil"
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
	// Canonical root = tmp so the managed link we create resolves under
	// it and is OWNED — updating it to a new canonical is allowed
	// (prod: canonical sources live under ~/.agents).
	t.Setenv("AGENTS_HOME", tmp)
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

// A stale *managed hard link* (the Windows file-link model; also valid on
// POSIX) must be recognized as owned and updated to a new canonical
// target, NOT refused as an unmanaged occupant. os.Readlink cannot
// resolve a hard link, so this exercises the ownedManagedHardlink branch.
func TestSymlinkUpdatesStaleManagedHardlink(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	target1 := filepath.Join(tmp, "h1.txt")
	target2 := filepath.Join(tmp, "h2.txt")
	linkPath := filepath.Join(tmp, "hlink.txt")

	if err := os.WriteFile(target1, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target2, []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-existing managed hard link to target1 (link count >= 2).
	if err := os.Link(target1, linkPath); err != nil {
		t.Fatalf("seed hard link: %v", err)
	}

	if err := links.Symlink(target2, linkPath); err != nil {
		t.Fatalf("Symlink update over managed hard link: %v", err)
	}
	if !links.IsManagedLink(linkPath, target2) {
		t.Errorf("expected updated link to resolve to %s", target2)
	}
}

// A plain user file (link count 1) at the link path is NOT a managed hard
// link and must still be refused with ErrUnmanagedTarget.
func TestSymlinkRefusesPlainUserFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	target := filepath.Join(tmp, "canon.txt")
	linkPath := filepath.Join(tmp, "user.txt")
	if err := os.WriteFile(target, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linkPath, []byte("user data"), 0644); err != nil {
		t.Fatal(err)
	}
	err := links.Symlink(target, linkPath)
	if !errors.Is(err, links.ErrUnmanagedTarget) {
		t.Fatalf("expected ErrUnmanagedTarget for plain user file, got %v", err)
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
	if runtime.GOOS == "windows" {
		t.Skip("IsSymlinkUnder resolves a managed link's target and tests it against a prefix; on Windows a file managed link is a hard link with no reparse point, so there is no resolvable target to compare (parity with the documented IsManagedLinkUnder behavior). Windows managed-link shape is covered by internal/linktest/linktest_test.go")
	}
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

func TestSymlinkRefusesUnmanagedRegularFile(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target.txt")
	if err := os.WriteFile(target, []byte("t"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(tmp, "link.txt")
	if err := os.WriteFile(linkPath, []byte("preexisting regular"), 0644); err != nil {
		t.Fatal(err)
	}

	err := links.Symlink(target, linkPath)
	if !errors.Is(err, links.ErrUnmanagedTarget) {
		t.Fatalf("want ErrUnmanagedTarget for unmanaged regular file, got %v", err)
	}
	if b, _ := os.ReadFile(linkPath); string(b) != "preexisting regular" {
		t.Errorf("user file must be preserved, got %q", string(b))
	}
	if links.IsManagedLink(linkPath, target) {
		t.Error("linkPath must NOT have been converted to a managed link")
	}
}

func TestSymlinkReplacingBacksUpThenReplaces(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target.txt")
	os.WriteFile(target, []byte("t"), 0644)
	linkPath := filepath.Join(tmp, "link.txt")
	os.WriteFile(linkPath, []byte("user content"), 0644)

	var backedUp string
	err := links.SymlinkReplacing(target, linkPath, func(p string) error {
		b, _ := os.ReadFile(p)
		backedUp = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("SymlinkReplacing: %v", err)
	}
	if backedUp != "user content" {
		t.Errorf("backup must see the original content, got %q", backedUp)
	}
	if !links.IsManagedLink(linkPath, target) {
		t.Error("linkPath should be a managed link after backup+replace")
	}

	other := filepath.Join(tmp, "other.txt")
	os.WriteFile(other, []byte("keep me"), 0644)
	bErr := links.SymlinkReplacing(target, other, func(string) error { return errFakeBackup })
	if !errors.Is(bErr, errFakeBackup) {
		t.Errorf("backup failure must propagate, got %v", bErr)
	}
	if b, _ := os.ReadFile(other); string(b) != "keep me" {
		t.Errorf("entry must be untouched when backup fails, got %q", string(b))
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

func TestRemoveIfHardlinkedToAny_RemovalFailurePropagates(t *testing.T) {
	tmp := t.TempDir()
	canonical := filepath.Join(tmp, "canonical")
	if err := os.WriteFile(canonical, []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	holder := filepath.Join(tmp, "holder")
	if err := os.MkdirAll(holder, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(holder, "f")
	if err := os.Link(canonical, managed); err != nil {
		t.Fatal(err)
	}

	testutil.MakeDirWriteDenied(t, holder)

	ok, err := links.RemoveIfHardlinkedToAny(managed, []string{canonical})
	if !ok || err == nil {
		t.Fatalf("want (true, non-nil) on a removal failure, got (%v, %v)", ok, err)
	}
}

func TestHardlinkRefusesUnmanagedThenReplacingBacksUp(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	os.WriteFile(src, []byte("src"), 0644)
	os.WriteFile(dst, []byte("preexisting user content"), 0644)

	if err := links.Hardlink(src, dst); !errors.Is(err, links.ErrUnmanagedTarget) {
		t.Fatalf("want ErrUnmanagedTarget, got %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "preexisting user content" {
		t.Errorf("unmanaged file must be preserved, got %q", string(b))
	}

	// Explicit replace path backs up then hard-links.
	var saved string
	if err := links.HardlinkReplacing(src, dst, func(p string) error {
		b, _ := os.ReadFile(p)
		saved = string(b)
		return nil
	}); err != nil {
		t.Fatalf("HardlinkReplacing: %v", err)
	}
	if saved != "preexisting user content" {
		t.Errorf("backup must capture the original, got %q", saved)
	}
	linked, err := links.AreHardlinked(src, dst)
	if err != nil || !linked {
		t.Errorf("expected hard-linked after explicit replace, linked=%v err=%v", linked, err)
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

	other := filepath.Join(tmp, "other.md")
	os.WriteFile(target, []byte("x"), 0644)
	linktest.Link(t, target, other)
	if err := links.RemoveIfSymlinkUnder(other, "/elsewhere"); err != nil {
		t.Errorf("non-matching prefix: %v", err)
	}
	if _, err := os.Lstat(other); err != nil {
		t.Error("non-matching link should remain")
	}

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

	link := filepath.Join(tmp, "linkdir")
	linktest.Link(t, dir, link)
	if !links.IsDirEntry(link) {
		t.Error("symlink to dir should be reported as dir")
	}

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

	linkPath := filepath.Join(tmp, "linkdir")
	os.MkdirAll(linkPath, 0755)
	child := filepath.Join(linkPath, "child")
	os.WriteFile(child, []byte("y"), 0644)

	err := links.Symlink(target, linkPath)
	if !errors.Is(err, links.ErrUnmanagedTarget) {
		t.Fatalf("want ErrUnmanagedTarget refusing unmanaged non-empty dir, got %v", err)
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

	if err := links.Symlink(target, link); err != nil {
		t.Errorf("idempotent call: %v", err)
	}
}
