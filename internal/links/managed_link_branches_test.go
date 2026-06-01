package links

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestSymlink_SameFileIsNoop covers the os.SameFile fast-path early return
// in Symlink (a hard link to the same canonical file is idempotent — there
// is no reparse point to Readlink).
func TestSymlink_SameFileIsNoop(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "canonical.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "hardlinked")
	if err := os.Link(target, link); err != nil {
		t.Fatal(err)
	}
	// Symlink should detect link and target are the same file and no-op.
	if err := Symlink(target, link); err != nil {
		t.Fatalf("Symlink on already-same-file should be a no-op, got %v", err)
	}
	if same, err := pathsResolveToSameFile(target, link); err != nil || !same {
		t.Errorf("expected link to remain the same file as target, same=%v err=%v", same, err)
	}
}

// TestSymlink_ReplacesStalePointingElsewhere covers the branch where an
// existing symlink points somewhere else and is removed via fsops.RemoveAll
// then recreated.
func TestSymlink_ReplacesStalePointingElsewhere(t *testing.T) {
	// testutil.SymlinkOrSkip gates on the per-process symlink capability
	// (Windows 10+ with Developer Mode satisfies it), so the windows-latest
	// runner exercises this branch instead of skipping it.
	testutil.SymlinkOrSkip(t)
	tmp := t.TempDir()
	// Canonical storage root = tmp, so a link resolving under it is a
	// dot-agents-OWNED stale managed link (prod: canonical lives under
	// ~/.agents). Such a link is safe to replace.
	t.Setenv("AGENTS_HOME", tmp)
	target := filepath.Join(tmp, "real.txt")
	stale := filepath.Join(tmp, "stale.txt")
	if err := os.WriteFile(target, []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "lnk")
	if err := os.Symlink(stale, link); err != nil {
		t.Fatal(err)
	}
	if err := Symlink(target, link); err != nil {
		t.Fatalf("Symlink replacing OWNED stale link: %v", err)
	}
	if !IsManagedLink(link, target) {
		t.Error("link should now resolve to target after replacement")
	}

	// A resolvable link pointing OUTSIDE the canonical root is a
	// user-owned link — refuse, never silently destroy it.
	outside := t.TempDir()
	userTgt := filepath.Join(outside, "users-own.txt")
	if err := os.WriteFile(userTgt, []byte("u"), 0o644); err != nil {
		t.Fatal(err)
	}
	userLink := filepath.Join(tmp, "userlnk")
	if err := os.Symlink(userTgt, userLink); err != nil {
		t.Fatal(err)
	}
	if err := Symlink(target, userLink); !errors.Is(err, ErrUnmanagedTarget) {
		t.Fatalf("want ErrUnmanagedTarget for a user-owned link, got %v", err)
	}
	if got, _ := os.Readlink(userLink); got != userTgt {
		t.Errorf("user link must be preserved, points to %q", got)
	}
}

// TestPathsResolveToSameFile_ErrorBranches covers the two stat-error
// returns (target missing, link missing).
func TestPathsResolveToSameFile_ErrorBranches(t *testing.T) {
	tmp := t.TempDir()
	exists := filepath.Join(tmp, "e.txt")
	if err := os.WriteFile(exists, []byte("e"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := pathsResolveToSameFile(filepath.Join(tmp, "missing"), exists); ok || err == nil {
		t.Error("expected error when target is missing")
	}
	if ok, err := pathsResolveToSameFile(exists, filepath.Join(tmp, "missing")); ok || err == nil {
		t.Error("expected error when link is missing")
	}
}

// TestIsManagedLink_AbsoluteTargetMatch covers the abs/clean-tolerant
// compare branch: the stored symlink dest is absolute while the caller
// passes a relative target that resolves to the same absolute path.
func TestIsManagedLink_AbsoluteTargetMatch(t *testing.T) {
	testutil.SymlinkOrSkip(t)
	tmp := t.TempDir()
	absTarget := filepath.Join(tmp, "abs-target.txt")
	if err := os.WriteFile(absTarget, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "lnk")
	if err := os.Symlink(absTarget, link); err != nil {
		t.Fatal(err)
	}
	// Pass a non-clean path that filepath.Abs+Clean reduces to absTarget.
	noisy := filepath.Join(tmp, ".", "abs-target.txt")
	if !IsManagedLink(link, noisy) {
		t.Errorf("expected abs/clean-tolerant match for %q vs stored %q", noisy, absTarget)
	}
}

// TestIsManagedLinkUnder_AbsolutePrefixBranch covers the branch where the
// raw prefix does not match but the absolute prefix does.
func TestIsManagedLinkUnder_AbsolutePrefixBranch(t *testing.T) {
	testutil.SymlinkOrSkip(t)
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
	// Stored dest is absolute (target is absolute).
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	// Pass a non-clean prefix that only matches once made absolute+clean.
	noisyPrefix := filepath.Join(tmp, "agents", ".")
	if !IsManagedLinkUnder(link, noisyPrefix) {
		t.Errorf("expected abs-prefix match for prefix %q", noisyPrefix)
	}
}
