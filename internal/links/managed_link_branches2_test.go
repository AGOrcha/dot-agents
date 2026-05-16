package links

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSymlink_AlreadyCorrectIsNoop covers the existing==target early
// return (the link already points exactly where requested).
func TestSymlink_AlreadyCorrectIsNoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink primitive; Windows path covered by internal/linktest/linktest_test.go")
	}
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "lnk")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Symlink(target, link); err != nil {
		t.Fatalf("Symlink on already-correct link should no-op: %v", err)
	}
	if !IsManagedLink(link, target) {
		t.Error("link should still resolve to target")
	}
}

// TestSymlink_ReplacesRegularFileNonSymlink covers the non-symlink branch:
// linkPath exists as a regular file (Readlink fails, not IsNotExist), so
// it is Lstat'd and removed before the link is created.
func TestSymlink_ReplacesRegularFileNonSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t.txt")
	if err := os.WriteFile(target, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "occupied")
	if err := os.WriteFile(link, []byte("squatter"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Contract: plain Symlink refuses an unmanaged regular file.
	if err := Symlink(target, link); !errors.Is(err, ErrUnmanagedTarget) {
		t.Fatalf("want ErrUnmanagedTarget, got %v", err)
	}
	if b, _ := os.ReadFile(link); string(b) != "squatter" {
		t.Errorf("unmanaged file must be preserved, got %q", string(b))
	}
	// The explicit replace path (with backup) does replace it.
	if err := SymlinkReplacing(target, link, func(string) error { return nil }); err != nil {
		t.Fatalf("SymlinkReplacing: %v", err)
	}
	if !IsManagedLink(link, target) {
		t.Error("link should resolve to target after explicit backed-up replace")
	}
}

// TestIsManagedLink_And_Under_AbsoluteBranches drives the abs-tolerant
// compare branches in IsManagedLink and IsManagedLinkUnder by storing an
// absolute symlink dest and querying with a relative target/prefix from a
// known working directory.
func TestIsManagedLink_And_Under_AbsoluteBranches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink primitive; Windows path covered by internal/linktest/linktest_test.go")
	}
	tmp := t.TempDir()
	root := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "r.md")
	if err := os.WriteFile(target, []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "L.md")
	if err := os.Symlink(target, link); err != nil { // dest stored absolute
		t.Fatal(err)
	}

	// Non-clean absolute target: literal string differs from the stored
	// dest, but filepath.Abs+Clean reduces it to dest → exercises the
	// abs/clean-tolerant match branch in IsManagedLink. No cwd dependency
	// (avoids the macOS /var->/private/var Getwd symlink artifact).
	noisyTarget := root + string(os.PathSeparator) + "." + string(os.PathSeparator) + "r.md"
	if noisyTarget == target {
		t.Skip("noisy path unexpectedly equals dest; abs branch not exercised")
	}
	if !IsManagedLink(link, noisyTarget) {
		t.Errorf("IsManagedLink abs branch: %q should clean-match stored dest %q", noisyTarget, target)
	}

	// Non-clean absolute prefix: HasPrefix(dest, raw) is false ("root/."
	// is not a literal prefix of "root/r.md"), but filepath.Abs(prefix)
	// cleans to root which IS a prefix → exercises the abs-prefix branch.
	noisyPrefix := root + string(os.PathSeparator) + "."
	if !IsManagedLinkUnder(link, noisyPrefix) {
		t.Errorf("IsManagedLinkUnder abs-prefix branch should match for %q", noisyPrefix)
	}
}
