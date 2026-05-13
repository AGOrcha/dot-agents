package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestImportCandidate_DestPath exercises the trivial joiner that maps a
// candidate's destRel to an absolute path under agentsHome.
func TestImportCandidate_DestPath(t *testing.T) {
	c := importCandidate{destRel: "rules/global/foo.md"}
	got := c.destPath("/agents-home")
	want := filepath.Join("/agents-home", "rules/global/foo.md")
	if got != want {
		t.Errorf("destPath = %q, want %q", got, want)
	}
}

// fakeFileInfo provides a minimal os.FileInfo for ModTime() callers.
type fakeFileInfo struct {
	mtime time.Time
}

func (f fakeFileInfo) Name() string       { return "fake" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0o644 }
func (f fakeFileInfo) ModTime() time.Time { return f.mtime }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

func TestImportReplaceMessage_SourceNewer(t *testing.T) {
	now := time.Now().UTC()
	src := fakeFileInfo{mtime: now}
	dst := fakeFileInfo{mtime: now.Add(-1 * time.Hour)}
	c := importCandidate{destRel: "rules/global/foo.md"}

	msg := importReplaceMessage(c, src, dst)
	if !strings.Contains(msg, "newer=source") {
		t.Errorf("expected newer=source, got %q", msg)
	}
	if !strings.Contains(msg, "rules/global/foo.md") {
		t.Errorf("expected destRel in message, got %q", msg)
	}
}

func TestImportReplaceMessage_DestNewer(t *testing.T) {
	now := time.Now().UTC()
	src := fakeFileInfo{mtime: now.Add(-2 * time.Hour)}
	dst := fakeFileInfo{mtime: now}
	c := importCandidate{destRel: "skills/x/SKILL.md"}

	msg := importReplaceMessage(c, src, dst)
	if !strings.Contains(msg, "newer=destination") {
		t.Errorf("expected newer=destination, got %q", msg)
	}
}

// withFlags temporarily mutates the package-level Flags singleton and
// restores it. Flags drives DryRun/Yes branches in import helpers.
func withFlags(t *testing.T, f GlobalFlags) {
	t.Helper()
	orig := Flags
	Flags = f
	t.Cleanup(func() { Flags = orig })
}

func TestImportMissingCandidate_DryRunSkipsCopy(t *testing.T) {
	withFlags(t, GlobalFlags{DryRun: true, Yes: true})
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.md")
	if err := os.WriteFile(src, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(tmp, "dest", "out.md") // intentionally not created

	c := importCandidate{
		project:    "demo",
		sourceRoot: tmp,
		sourcePath: src,
		destRel:    "rules/demo/out.md",
	}
	res := importMissingCandidate(c, dest, "2026-01-01T00-00-00Z")
	if res.imported != 1 {
		t.Fatalf("expected imported=1 in dry run, got %+v", res)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("dest should not be created under dry run, stat err: %v", err)
	}
}

func TestReplaceImportCandidate_DryRunYes(t *testing.T) {
	withFlags(t, GlobalFlags{DryRun: true, Yes: true})
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.md")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(tmp, "dest.md")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcInfo, _ := os.Stat(src)
	destInfo, _ := os.Stat(dest)

	c := importCandidate{
		project:    "demo",
		sourceRoot: tmp,
		sourcePath: src,
		destRel:    "rules/demo/foo.md",
	}
	res := replaceImportCandidate(c, tmp, dest, "2026-01-01T00-00-00Z", srcInfo, destInfo)
	if res.imported != 1 {
		t.Fatalf("expected imported=1 in dry run with --yes, got %+v", res)
	}
	// Dest content must remain unchanged in dry run.
	got, _ := os.ReadFile(dest)
	if string(got) != "old" {
		t.Fatalf("dest mutated in dry run, got %q", got)
	}
}
