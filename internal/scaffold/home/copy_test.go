package home

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeDirEntry implements fs.DirEntry for unit-testing copyStarterEntry
// branches that the real embedded tree does not exercise (notably the .sh
// suffix mode-bump and the rel == "." early-return).
type fakeDirEntry struct {
	name string
	dir  bool
}

func (f fakeDirEntry) Name() string               { return f.name }
func (f fakeDirEntry) IsDir() bool                { return f.dir }
func (f fakeDirEntry) Type() fs.FileMode          { return 0 }
func (f fakeDirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("not used") }

func TestCopyMissingStarterAssetsCopiesStarterBundle(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	for _, rel := range []string{
		".gitignore",
		"README.md",
		"rules/global/rules.mdc",
		"settings/global/claude-code.json",
		"skills/global/agent-start/SKILL.md",
		"skills/global/review-pr/templates/review-output.md",
	} {
		if _, err := os.Stat(filepath.Join(tmp, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}
}

func TestCopyMissingStarterAssetsPreservesExistingFiles(t *testing.T) {
	tmp := t.TempDir()
	skill := filepath.Join(tmp, "skills", "global", "agent-start", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatal(err)
	}
	want := "# custom\n"
	if err := os.WriteFile(skill, []byte(want), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("starter skill overwritten:\n got: %s\nwant: %s", string(got), want)
	}
}

// TestCopyStarterEntryRelDotSkips covers the rel == "." early-return at the
// top of the walk function — that branch never fires through normal
// CopyMissingStarterAssets because filepath.Rel returns "." only for the
// root, and the WalkDir callback skips the directory itself in practice.
func TestCopyStarterEntryRelDotSkips(t *testing.T) {
	tmp := t.TempDir()
	err := copyStarterEntry(tmp, "starter", fakeDirEntry{name: "starter", dir: true})
	if err != nil {
		t.Fatalf("copyStarterEntry(starter): %v", err)
	}
	// Should not have created tmp/starter — rel == "." causes a no-op return.
	if _, err := os.Stat(filepath.Join(tmp, "starter")); err == nil {
		t.Errorf("expected no tmp/starter created when rel == .")
	}
}

// TestCopyStarterEntryShSuffixSetsExecBit covers the .sh-suffix branch of
// copyStarterEntry by routing a synthetic dir-entry through the real
// embedded read. The embedded tree has no .sh files of its own, so this
// branch is otherwise unreachable.
func TestCopyStarterEntryShSuffixSetsExecBit(t *testing.T) {
	// POSIX-only: this test asserts the 0o755 exec-bit is set on copied
	// `.sh` files. The POSIX exec bit has no Windows analog — NTFS does
	// not encode per-user execute permission as a file mode bit, and Go's
	// os.Stat on Windows synthesizes mode bits from file attributes
	// (FILE_ATTRIBUTE_READONLY → 0o444 vs 0o666), never 0o755.
	//
	// Classification: [genuine-posix] (see
	// .agents/workflow/plans/cross-platform-test-skips-audit/ and the
	// catalogue findings.md entry for copy_test.go:89). This is NOT a
	// shortcut that a testutil helper can paper over — the assertion
	// itself is about a POSIX-only semantic. Do NOT try to "abstract"
	// this skip away; doing so would change what the test asserts.
	//
	// The matching scaffolder behavior is intentionally a no-op on
	// Windows (the exec-bit branch in copyStarterEntry has no effect on
	// NTFS), so there is no Windows-side assertion to cover here.
	if runtime.GOOS == "windows" {
		t.Skip("file modes differ on windows")
	}
	tmp := t.TempDir()
	// Pick a real embedded file path, but pretend its base name ends in .sh.
	// We borrow README.md content via embedded.ReadFile in copyStarterEntry.
	srcPath := "starter/README.md"
	// Force the destination filename to end in .sh so the mode branch fires.
	// copyStarterEntry computes dstPath from filepath.Rel("starter", path);
	// path "starter/x.sh" yields rel "x.sh" — but we need that path to exist
	// in the embed FS, so we pass d.Name() ending in .sh while srcPath is
	// a real file. The function only consults d.Name() for the suffix check
	// and uses `path` for embedded.ReadFile.
	err := copyStarterEntry(tmp, srcPath, fakeDirEntry{name: "fake.sh"})
	if err != nil {
		t.Fatalf("copyStarterEntry: %v", err)
	}
	// rel of srcPath under starter/ is README.md, so file is written there.
	dst := filepath.Join(tmp, "README.md")
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if fi.Mode().Perm()&0111 == 0 {
		t.Errorf("expected exec bit on .sh-named entry; got mode %v", fi.Mode())
	}
}

// TestCopyStarterEntryStatErrorPropagates covers the non-IsNotExist branch
// in copyStarterEntry by passing a destination path whose parent component
// is a regular file rather than a directory — os.Stat then returns
// ENOTDIR, which is not os.IsNotExist.
func TestCopyStarterEntryStatErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	// Create a regular file at <tmp>/blocker so any child stat returns ENOTDIR.
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// dstRoot = blocker means filepath.Join(blocker, rel) becomes
	// blocker/README.md and os.Stat on that returns ENOTDIR.
	err := copyStarterEntry(blocker, "starter/README.md", fakeDirEntry{name: "README.md"})
	if err == nil {
		t.Fatal("expected error from stat on path under a regular file")
	}
	if strings.Contains(err.Error(), "no such") {
		t.Errorf("got IsNotExist style error; wanted other (ENOTDIR/etc): %v", err)
	}
}
