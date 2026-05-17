//go:build !windows

package links

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasMultipleHardLinks(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hasMultipleHardLinks(f) {
		t.Error("single-linked regular file should report false")
	}

	hl := filepath.Join(tmp, "f.hardlink")
	if err := os.Link(f, hl); err != nil {
		t.Fatal(err)
	}
	if !hasMultipleHardLinks(f) {
		t.Error("file with a second hard link should report true")
	}
	if !hasMultipleHardLinks(hl) {
		t.Error("the hard-link entry should also report true")
	}

	if hasMultipleHardLinks(filepath.Join(tmp, "missing")) {
		t.Error("missing path should report false")
	}
}

func TestIsManagedFileLink_Unix(t *testing.T) {
	tmp := t.TempDir()

	reg := filepath.Join(tmp, "reg.txt")
	if err := os.WriteFile(reg, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsManagedFileLink(reg) {
		t.Error("plain single-linked regular file is not a managed file link")
	}

	sl := filepath.Join(tmp, "sl")
	if err := os.Symlink(reg, sl); err != nil {
		t.Fatal(err)
	}
	if !IsManagedFileLink(sl) {
		t.Error("symlink should be reported as a managed file link")
	}

	d := filepath.Join(tmp, "d")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if IsManagedFileLink(d) {
		t.Error("directory (non-regular) is not a managed file link")
	}

	// nlink>1 regular file is the POSIX-observable analogue of a Windows
	// hard-linked managed file.
	hl := filepath.Join(tmp, "hl")
	if err := os.Link(reg, hl); err != nil {
		t.Fatal(err)
	}
	if !IsManagedFileLink(hl) {
		t.Error("hard-linked regular file (nlink>1) should be a managed file link")
	}

	if IsManagedFileLink(filepath.Join(tmp, "nope")) {
		t.Error("missing path should report false")
	}
}
