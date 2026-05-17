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

func TestHardlink_IdempotentNoop(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "s")
	if err := os.WriteFile(src, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "d")
	if err := Hardlink(src, dst); err != nil {
		t.Fatal(err)
	}
	// Second call: already hard-linked to src → early no-op return.
	if err := Hardlink(src, dst); err != nil {
		t.Errorf("idempotent Hardlink must no-op, got %v", err)
	}
	if linked, _ := AreHardlinked(src, dst); !linked {
		t.Error("still expected hard-linked after idempotent call")
	}
}

func TestPathUnder_BoundaryCases(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, ".agents")
	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{"equal", root, true},
		{"nested", filepath.Join(root, "a", "b.md"), true},
		{"sibling-prefix", filepath.Join(tmp, ".agents-old", "x"), false},
		{"parent", tmp, false},
		{"unrelated", filepath.Join(tmp, "elsewhere", "y"), false},
		{"non-clean equal", root + string(os.PathSeparator) + ".", true},
	}
	for _, tc := range cases {
		if got := pathUnder(tc.target, root); got != tc.want {
			t.Errorf("%s: pathUnder(%q,%q)=%v want %v", tc.name, tc.target, root, got, tc.want)
		}
	}
}

func TestIsManagedLinkUnder_RelativeTargetAndNonLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink primitive; Windows junction covered by internal/linktest")
	}
	tmp := t.TempDir()
	root := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	tgt := filepath.Join(root, "r.md")
	if err := os.WriteFile(tgt, []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Relative symlink target — must be resolved against the link's dir,
	// then containment-checked (exercises the !filepath.IsAbs branch).
	relLink := filepath.Join(tmp, "rel-link")
	// Relative to relLink's dir (tmp): ".agents/r.md" → tmp/.agents/r.md.
	if err := os.Symlink(filepath.Join(".agents", "r.md"), relLink); err != nil {
		t.Fatal(err)
	}
	if !IsManagedLinkUnder(relLink, root) {
		t.Error("relative symlink target resolving under root must be contained")
	}
	// Not a managed link at all → ManagedLinkTarget false → false.
	plain := filepath.Join(tmp, "plain.txt")
	if err := os.WriteFile(plain, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsManagedLinkUnder(plain, root) {
		t.Error("a regular file is not a managed link under root")
	}
}

func TestHardlinkWithPolicy_OwnedAndEmptyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX hardlink/symlink primitives; Windows covered by internal/linktest")
	}
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp) // canonical root = tmp → links under it are owned
	src := filepath.Join(tmp, "src")
	if err := os.WriteFile(src, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}

	// dst is an OWNED managed symlink (resolves under AGENTS_HOME) → Hardlink
	// replaces it without backup/refusal.
	canon := filepath.Join(tmp, "canon")
	if err := os.WriteFile(canon, []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	ownedDst := filepath.Join(tmp, "owned-dst")
	if err := os.Symlink(canon, ownedDst); err != nil {
		t.Fatal(err)
	}
	if err := Hardlink(src, ownedDst); err != nil {
		t.Fatalf("Hardlink over an owned managed link should replace it: %v", err)
	}
	if linked, _ := AreHardlinked(src, ownedDst); !linked {
		t.Error("expected hard link after replacing owned managed link")
	}

	// dst is an empty squat dir → replaced (no data, idempotent).
	emptyDst := filepath.Join(tmp, "empty-dst")
	if err := os.Mkdir(emptyDst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Hardlink(src, emptyDst); err != nil {
		t.Fatalf("Hardlink over an empty squat dir should replace it: %v", err)
	}
	if linked, _ := AreHardlinked(src, emptyDst); !linked {
		t.Error("expected hard link after replacing empty dir")
	}
}

// TestIsManagedLinkUnder_SiblingPrefixNotContained pins the path-boundary
// containment fix: a link resolving into a SIBLING directory that merely
// shares a lexical string prefix with the canonical root (e.g.
// ".agents-old" vs ".agents") must NOT be classified as managed —
// otherwise Symlink/Hardlink/RemoveIfSymlinkUnder would destroy a
// user-owned link without backup.
func TestIsManagedLinkUnder_SiblingPrefixNotContained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink primitive; Windows junction path covered by internal/linktest")
	}
	tmp := t.TempDir()
	root := filepath.Join(tmp, ".agents")
	sibling := filepath.Join(tmp, ".agents-old")
	for _, d := range []string{root, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Link into the SIBLING (user-owned), prefix is the canonical root.
	siblingTarget := filepath.Join(sibling, "user-file.md")
	if err := os.WriteFile(siblingTarget, []byte("u"), 0o644); err != nil {
		t.Fatal(err)
	}
	siblingLink := filepath.Join(tmp, "sibling-link")
	if err := os.Symlink(siblingTarget, siblingLink); err != nil {
		t.Fatal(err)
	}
	if IsManagedLinkUnder(siblingLink, root) {
		t.Errorf("%s -> %s must NOT be 'under' %s (sibling, not contained)", siblingLink, siblingTarget, root)
	}
	if ownedManagedLink(siblingLink) {
		// ownedManagedLink uses config.AgentsHome(); pin via env so the
		// predicate is exercised against root, not the real home.
		t.Setenv("AGENTS_HOME", root)
		if ownedManagedLink(siblingLink) {
			t.Errorf("ownedManagedLink must be false for a sibling-dir link")
		}
	}
	// A link genuinely under root IS contained.
	inTarget := filepath.Join(root, "rule.md")
	if err := os.WriteFile(inTarget, []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	inLink := filepath.Join(tmp, "in-link")
	if err := os.Symlink(inTarget, inLink); err != nil {
		t.Fatal(err)
	}
	if !IsManagedLinkUnder(inLink, root) {
		t.Errorf("%s -> %s must be under %s", inLink, inTarget, root)
	}
}
