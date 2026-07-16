package config

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	git "github.com/go-git/go-git/v6"
)

func testBundle(t *testing.T, files map[string]string) Bundle {
	t.Helper()
	var entries []BundleEntry
	for path, content := range files {
		entries = append(entries, BundleEntry{Path: path, Data: []byte(content), Mode: 0o644})
	}
	b, err := NormalizeBundle(func(emit func(RawBundleEntry) error) error {
		for _, e := range entries {
			if err := emit(RawBundleEntry{Path: e.Path, Kind: rawKindFile, Mode: e.Mode, Size: int64(len(e.Data)), Data: e.Data}); err != nil {
				return err
			}
		}
		return nil
	}, BundleLimits{})
	if err != nil {
		t.Fatalf("build test bundle: %v", err)
	}
	return b
}

// --- H2: content-addressed immutable store (confirmed sound; kept) ---------

func TestMaterializeToStoreWritesContentAddressedTree(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{
		"SKILL.md":            "# a skill\n",
		"instructions/run.md": "do the thing\n",
	})

	storePath, digest, installed, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if !installed {
		t.Fatalf("expected first materialize to report installed=true")
	}
	if digest != BundleDigest(bundle) {
		t.Fatalf("digest mismatch: got %q want %q", digest, BundleDigest(bundle))
	}
	if !strings.HasPrefix(storePath, filepath.Join(home, "cache", "artifacts", "skills")) {
		t.Fatalf("store path %q not under the H2 content-addressed root", storePath)
	}
	got, err := os.ReadFile(filepath.Join(storePath, "SKILL.md"))
	if err != nil {
		t.Fatalf("read materialized file: %v", err)
	}
	if string(got) != "# a skill\n" {
		t.Fatalf("materialized content mismatch: %q", got)
	}
	if _, err := os.Stat(filepath.Join(storePath, "instructions", "run.md")); err != nil {
		t.Fatalf("nested file missing: %v", err)
	}
}

func TestMaterializeToStoreReMaterializeIsByteIdenticalNoOp(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "content\n"})

	storePath1, digest1, installed1, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	if !installed1 {
		t.Fatalf("expected first materialize installed=true")
	}
	before, err := os.ReadFile(filepath.Join(storePath1, "SKILL.md"))
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	storePath2, digest2, installed2, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	if installed2 {
		t.Fatalf("re-materializing an unchanged digest must be a no-op (installed=false), got installed=true")
	}
	if storePath1 != storePath2 || digest1 != digest2 {
		t.Fatalf("store path/digest must be stable across re-materialize")
	}
	after, err := os.ReadFile(filepath.Join(storePath2, "SKILL.md"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("content changed across a no-op re-materialize: %q vs %q", before, after)
	}
}

func TestMaterializeToStoreChangedDigestNeverMutatesOldPath(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundleV1 := testBundle(t, map[string]string{"SKILL.md": "v1\n"})
	bundleV2 := testBundle(t, map[string]string{"SKILL.md": "v2\n"})

	storePathV1, digestV1, _, err := MaterializeToStore(home, "skills", bundleV1)
	if err != nil {
		t.Fatalf("materialize v1: %v", err)
	}
	storePathV2, digestV2, installedV2, err := MaterializeToStore(home, "skills", bundleV2)
	if err != nil {
		t.Fatalf("materialize v2: %v", err)
	}
	if !installedV2 {
		t.Fatalf("expected v2 (a different digest) to be freshly installed")
	}
	if storePathV1 == storePathV2 || digestV1 == digestV2 {
		t.Fatalf("v1 and v2 must occupy distinct digest-keyed paths")
	}
	v1Content, err := os.ReadFile(filepath.Join(storePathV1, "SKILL.md"))
	if err != nil {
		t.Fatalf("read v1 after v2 materialize: %v", err)
	}
	if string(v1Content) != "v1\n" {
		t.Fatalf("v1's store path was mutated by materializing v2: got %q", v1Content)
	}
}

func TestMaterializeToStoreConcurrentSameDigestConverges(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{
		"SKILL.md":              strings.Repeat("x", 4096),
		"references/detail.md":  strings.Repeat("y", 4096),
		"instructions/steps.md": strings.Repeat("z", 4096),
	})

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	paths := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, _, _, err := MaterializeToStore(home, "skills", bundle)
			paths[i] = p
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: materialize failed: %v", i, err)
		}
		if paths[i] != paths[0] {
			t.Fatalf("goroutine %d: store path diverged", i)
		}
	}
	for name, want := range map[string]string{
		"SKILL.md":              strings.Repeat("x", 4096),
		"references/detail.md":  strings.Repeat("y", 4096),
		"instructions/steps.md": strings.Repeat("z", 4096),
	} {
		got, err := os.ReadFile(filepath.Join(paths[0], filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %s after concurrent materialize: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("torn write detected in %s: got %d bytes, want %d", name, len(got), len(want))
		}
	}
}

func TestMaterializeToStoreRejectsEmptyFamily(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	if _, _, _, err := MaterializeToStore(home, "", bundle); err == nil {
		t.Fatalf("expected an error for an empty family")
	}
}

// --- H15: identity-component containment -----------------------------------

// TestMaterializeToStoreRejectsTraversalFamily is the H15 fail-before-fix
// guard: a family that is not a single canonical segment ("..", a
// separator-bearing string, an absolute path) is rejected BEFORE any path is
// derived, so it can never widen the store root.
func TestMaterializeToStoreRejectsTraversalFamily(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	for _, bad := range []string{"..", ".", "a/b", "../escape", `a\b`, "/abs"} {
		if _, _, _, err := MaterializeToStore(home, bad, bundle); err == nil {
			t.Fatalf("expected family %q to be rejected as a non-canonical segment", bad)
		}
	}
}

// --- H16: verify-on-hit ----------------------------------------------------

// TestMaterializeToStoreVerifyOnHitReExtractsTamperedEntry is the H16
// fail-before-fix guard: a pre-existing store dir whose content was TAMPERED
// (a file rewritten) must NOT be trusted on os.Stat — it is quarantined and
// re-extracted so the store path once again holds the true bundle content.
func TestMaterializeToStoreVerifyOnHitReExtractsTamperedEntry(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "authentic\n"})

	storePath, _, _, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	// Published store files are read-only (t3 review #2c): a tamper must first
	// escalate privilege by restoring the write bit — which is exactly the
	// deliberate act the hardening forces.
	if err := os.Chmod(filepath.Join(storePath, "SKILL.md"), 0o644); err != nil {
		t.Fatalf("restore write bit for tamper: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storePath, "SKILL.md"), []byte("TAMPERED\n"), 0o644); err != nil {
		t.Fatalf("tamper store file: %v", err)
	}

	storePath2, _, installed, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("re-materialize over tampered entry: %v", err)
	}
	if storePath2 != storePath {
		t.Fatalf("re-materialized path should be the same digest path")
	}
	if !installed {
		t.Fatalf("expected verify-on-hit to re-extract (installed=true), got a trusted no-op over tampered content")
	}
	got, err := os.ReadFile(filepath.Join(storePath, "SKILL.md"))
	if err != nil {
		t.Fatalf("read re-extracted file: %v", err)
	}
	if string(got) != "authentic\n" {
		t.Fatalf("verify-on-hit did not restore authentic content: %q", got)
	}
	quarantined, _ := filepath.Glob(storePath + ".corrupt-*")
	if len(quarantined) == 0 {
		t.Fatalf("expected the tampered entry to be quarantined aside")
	}
}

// TestMaterializeToStoreVerifyOnHitRejectsSymlinkTamper proves the shape
// check in verify-on-hit: a store entry into which a symlink was injected
// fails verification (wrong shape) and is re-extracted, never followed.
func TestMaterializeToStoreVerifyOnHitRejectsSymlinkTamper(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "authentic\n"})
	storePath, _, _, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(storePath, "evil")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, _, installed, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("re-materialize over symlink-tampered entry: %v", err)
	}
	if !installed {
		t.Fatalf("expected symlink-tampered entry to fail verification and re-extract")
	}
	if _, err := os.Lstat(filepath.Join(storePath, "evil")); !os.IsNotExist(err) {
		t.Fatalf("expected the injected symlink to be gone after re-extract, err=%v", err)
	}
}

// --- H14: CAS gitignored, verified with git's own semantics on the CAS path
// itself, before the first store byte -------------------------------------

// TestMaterializeInstallsCASIgnoreAndGitStatusIsClean is the definitive H14
// acceptance test: materialize into a REAL git repo rooted at agentsHome,
// then assert go-git's own `status` reports a clean tree — the permanent
// "cache/" pattern must hide the entire content-addressed store, checked by
// the actual git engine, not this package's bookkeeping.
func TestMaterializeInstallsCASIgnoreAndGitStatusIsClean(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	repo, err := git.PlainInit(home, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	skillBundle := testBundle(t, map[string]string{"SKILL.md": "# fetched\n", "instructions/x.md": "y\n"})
	agentBundle := testBundle(t, map[string]string{"AGENT.md": "# fetched agent\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", skillBundle); err != nil {
		t.Fatalf("materialize skills: %v", err)
	}
	if _, _, _, err := MaterializeToStore(home, "agents", agentBundle); err != nil {
		t.Fatalf("materialize agents: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, "skills", "dot-agents", "hand-authored"), 0o755); err != nil {
		t.Fatalf("mkdir local fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "dot-agents", "hand-authored", "SKILL.md"), []byte("# local\n"), 0o644); err != nil {
		t.Fatalf("write local fixture: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	status, err := wt.Status()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	for path := range status {
		if strings.HasPrefix(filepath.ToSlash(path), "cache/") {
			t.Fatalf("H14 violated: git status reports a store file under cache/: %s", path)
		}
	}
	if _, ok := status[filepath.ToSlash(filepath.Join("skills", "dot-agents", "hand-authored", "SKILL.md"))]; !ok {
		t.Fatalf("expected the local-authored fixture to be visible to git status, got %+v", status)
	}
}

// TestMaterializeRefusesWhenCASIgnoreCannotInstall is the H14 fail-before-fix
// guard: if the permanent CAS ignore cannot be installed/verified (here the
// local source's .gitignore is a DIRECTORY, so the read-modify-write fails),
// materialize must refuse BEFORE writing any store byte — a fetched artifact
// is never written into a store that git would track.
func TestMaterializeRefusesWhenCASIgnoreCannotInstall(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Make .gitignore a directory so readGitignore/WriteFileAtomic fail.
	if err := os.MkdirAll(filepath.Join(home, gitignoreFileName), 0o755); err != nil {
		t.Fatalf("seed .gitignore-as-dir: %v", err)
	}
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
		t.Fatalf("expected materialize to refuse when the CAS ignore cannot be installed")
	}
	if _, err := os.Stat(filepath.Join(home, "cache", "artifacts")); !os.IsNotExist(err) {
		t.Fatalf("expected no store bytes written when CAS ignore fails, err=%v", err)
	}
}

// TestCASPathIgnoredUsesGitSemantics proves CASPathIgnored evaluates with
// git's OWN engine (not a substring check): the permanent cache/ pattern
// covers the store; a .gitignore that does NOT carry a covering pattern
// reports the CAS path as NOT ignored (the exact miss H14's verify catches);
// and a non-store path is never falsely reported ignored.
func TestCASPathIgnoredUsesGitSemantics(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	ls := NewLocalSource(home, nil)
	if err := ls.EnsureProvenanceGitignore(nil); err != nil {
		t.Fatalf("install CAS ignore: %v", err)
	}
	ok, err := ls.CASPathIgnored(filepath.Join("cache", "artifacts", "skills", "deadbeef"))
	if err != nil {
		t.Fatalf("CASPathIgnored: %v", err)
	}
	if !ok {
		t.Fatalf("expected the store path to be ignored by the permanent cache/ pattern")
	}
	ok, err = ls.CASPathIgnored(filepath.Join("skills", "dot-agents", "x"))
	if err != nil {
		t.Fatalf("CASPathIgnored (non-cache): %v", err)
	}
	if ok {
		t.Fatalf("did not expect a non-cache path to be ignored by cache/")
	}

	// A .gitignore whose patterns do NOT cover the store: git says not
	// ignored — the miss H14's verify would refuse on.
	home2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(home2, gitignoreFileName), []byte("logs/\n*.tmp\n"), 0o644); err != nil {
		t.Fatalf("seed non-covering gitignore: %v", err)
	}
	ls2 := NewLocalSource(home2, nil)
	ok, err = ls2.CASPathIgnored(filepath.Join("cache", "artifacts", "skills", "deadbeef"))
	if err != nil {
		t.Fatalf("CASPathIgnored (non-covering): %v", err)
	}
	if ok {
		t.Fatalf("git semantics must report the store path NOT ignored when no covering pattern exists")
	}
}

// TestCASPathIgnored_LeadingWhitespaceNotSignificant is the t9 round-2
// cross-harness regression test for finding #2: a gitignore line with
// LEADING whitespace (" cache/") is NOT the same pattern as "cache/" under
// real git semantics (gitignore(5): leading whitespace is significant,
// unlike trailing whitespace which git strips). Before the fix,
// strings.TrimSpace on both ends silently canonicalized " cache/" to
// "cache/", making CASPathIgnored report "ignored" when real `git
// check-ignore` would not. Asserted against the real git binary when
// available; CASPathIgnored's own answer must agree with it.
func TestCASPathIgnored_LeadingWhitespaceNotSignificant(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if _, err := git.PlainInit(home, false); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, gitignoreFileName), []byte(" cache/\n"), 0o644); err != nil {
		t.Fatalf("seed leading-whitespace gitignore: %v", err)
	}
	casRel := filepath.Join("cache", "artifacts", "skills", "deadbeef")

	ls := NewLocalSource(home, nil)
	ok, err := ls.CASPathIgnored(casRel)
	if err != nil {
		t.Fatalf("CASPathIgnored: %v", err)
	}
	if ok {
		t.Fatalf(`CASPathIgnored must NOT treat " cache/" (leading whitespace) as matching "cache/" — real git does not`)
	}

	assertRealGitDoesNotIgnore(t, home, filepath.Join("cache", "artifacts", "skills", "deadbeef", "layer.json"))
}

// TestCASPathIgnored_TrailingTabNotStripped is the t9 round-3 cross-harness
// regression test isolating finding #2 on its own (independent of the
// structural terminal-block fix): a gitignore line "cache/<TAB>" is a
// DIFFERENT pattern from "cache/" under real git — git strips only trailing
// UNESCAPED SPACES, never tabs. Before the round-3 fix, CASPathIgnored's
// per-line trim stripped " \t\r" (spaces AND tabs), silently canonicalizing
// "cache/\t" to "cache/" and reporting "ignored" when real git would not.
func TestCASPathIgnored_TrailingTabNotStripped(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if _, err := git.PlainInit(home, false); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, gitignoreFileName), []byte("cache/\t\n"), 0o644); err != nil {
		t.Fatalf("seed trailing-tab gitignore: %v", err)
	}
	casRel := filepath.Join("cache", "artifacts", "skills", "deadbeef")

	ls := NewLocalSource(home, nil)
	ok, err := ls.CASPathIgnored(casRel)
	if err != nil {
		t.Fatalf("CASPathIgnored: %v", err)
	}
	if ok {
		t.Fatalf(`CASPathIgnored must NOT treat "cache/<TAB>" as matching "cache/" — real git strips only trailing spaces, never tabs`)
	}

	assertRealGitDoesNotIgnore(t, home, filepath.Join("cache", "artifacts", "skills", "deadbeef", "layer.json"))
}

// TestEnsureAndVerifyCASIgnore_LeadingWhitespaceCanonicalizes is the t9
// round-2 regression test proving the fast path in EnsureAndVerifyCASIgnore
// does not trust a non-canonical " cache/" gitignore: it must fall through
// to the full install (canonicalizing rewrite) rather than fast-returning on
// a CASPathIgnored false positive, and after the call a real git check
// confirms the store path is genuinely ignored.
func TestEnsureAndVerifyCASIgnore_LeadingWhitespaceCanonicalizes(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if _, err := git.PlainInit(home, false); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, gitignoreFileName), []byte(" cache/\n"), 0o644); err != nil {
		t.Fatalf("seed leading-whitespace gitignore: %v", err)
	}

	// The non-canonical file must not satisfy the fast-path gate.
	if canonical, err := gitignoreIsCanonicalCASIgnore(filepath.Join(home, gitignoreFileName)); err != nil || canonical {
		t.Fatalf("gitignoreIsCanonicalCASIgnore(leading-whitespace) = (%v, %v), want (false, nil)", canonical, err)
	}

	bundle := testBundle(t, map[string]string{"SKILL.md": "# fetched\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err != nil {
		t.Fatalf("MaterializeToStore: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, gitignoreFileName))
	if err != nil {
		t.Fatalf("read canonicalized .gitignore: %v", err)
	}
	if !strings.Contains(string(data), gitignoreBlockBegin) {
		t.Fatalf("expected the managed block to be installed after canonicalization, got %q", data)
	}

	assertRealGitDoesIgnore(t, home, "cache/artifacts/skills/deadbeef/layer.json")
}

// TestEnsureAndVerifyCASIgnore_SymlinkedGitignoreCanonicalizes is the t9
// round-2 cross-harness regression test for finding #1: a SYMLINKED
// .gitignore whose target contains "cache/" makes CASPathIgnored (which
// reads via os.ReadFile, following symlinks) report "ignored" — but real
// git refuses to read a symlinked .gitignore (treats it as absent), so
// `git status`/`git check-ignore` would NOT ignore the path. The fast path
// must not trust this divergent form; EnsureAndVerifyCASIgnore must fall
// through to the canonicalizing install, which replaces the symlink with a
// real regular file (os.Rename on a symlink path replaces the link itself,
// never the target) before verifying success.
func TestEnsureAndVerifyCASIgnore_SymlinkedGitignoreCanonicalizes(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if _, err := git.PlainInit(home, false); err != nil {
		t.Fatalf("git init: %v", err)
	}
	target := filepath.Join(home, "real-gitignore-target")
	if err := os.WriteFile(target, []byte("cache/\n"), 0o644); err != nil {
		t.Fatalf("seed symlink target: %v", err)
	}
	gitignorePath := filepath.Join(home, gitignoreFileName)
	if err := os.Symlink(target, gitignorePath); err != nil {
		t.Skipf("cannot create symlinks on this platform: %v", err)
	}

	// The symlinked occupant must not satisfy the fast-path gate, even
	// though CASPathIgnored (reading through the symlink) would say "ignored".
	if canonical, err := gitignoreIsCanonicalCASIgnore(gitignorePath); err != nil || canonical {
		t.Fatalf("gitignoreIsCanonicalCASIgnore(symlink) = (%v, %v), want (false, nil)", canonical, err)
	}
	ls := NewLocalSource(home, nil)
	if ok, err := ls.CASPathIgnored(filepath.Join("cache", "artifacts", "skills", "deadbeef")); err != nil || !ok {
		t.Fatalf("expected CASPathIgnored to (falsely) report ignored through the symlink target, got ok=%v err=%v", ok, err)
	}
	assertRealGitDoesNotIgnore(t, home, "cache/artifacts/skills/deadbeef/layer.json")

	bundle := testBundle(t, map[string]string{"SKILL.md": "# fetched\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err != nil {
		t.Fatalf("MaterializeToStore: %v", err)
	}

	info, err := os.Lstat(gitignorePath)
	if err != nil {
		t.Fatalf("lstat canonicalized .gitignore: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected the symlink to be replaced by a regular file, still a symlink")
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("expected a regular file after canonicalization, mode=%v", info.Mode())
	}

	assertRealGitDoesIgnore(t, home, "cache/artifacts/skills/deadbeef/layer.json")
}

// TestEnsureAndVerifyCASIgnore_TrailingTabNegationShadowCanonicalizes is the
// t9 round-3 cross-harness regression test for the third variant of the
// same divergence class: a canonical managed block FOLLOWED BY a
// re-inclusion ("!cache/") and a trailing-tab shadow variant ("cache/\t").
//
//	# >>> dot-agents managed (local source provenance) >>>
//	cache/
//	# <<< dot-agents managed (local source provenance) <<<
//	!cache/
//	cache/<TAB>
//
// Under real git's last-match-wins semantics, "!cache/" is the last rule
// that actually matches "cache/..." paths (git strips only trailing
// unescaped SPACES, never tabs, so "cache/\t" is a distinct, non-matching
// pattern and does NOT re-shadow the negation back to ignored) — so real
// git does NOT ignore the store. Before the round-3 fix,
// gitignoreIsCanonicalCASIgnore only checked that the block CONTAINED the
// managed line (true here) without checking what followed it, so it wrongly
// reported this file as canonical and the fast path skipped the
// canonicalizing install — leaving the store git-tracked.
func TestEnsureAndVerifyCASIgnore_TrailingTabNegationShadowCanonicalizes(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if _, err := git.PlainInit(home, false); err != nil {
		t.Fatalf("git init: %v", err)
	}
	content := gitignoreBlockBegin + "\n" +
		"cache/\n" +
		gitignoreBlockEnd + "\n" +
		"!cache/\n" +
		"cache/\t\n"
	gitignorePath := filepath.Join(home, gitignoreFileName)
	if err := os.WriteFile(gitignorePath, []byte(content), 0o644); err != nil {
		t.Fatalf("seed trailing-tab negation-shadow gitignore: %v", err)
	}

	// The structural terminal-block check must reject this file: a pattern
	// line (the negation) follows the managed block's closing marker.
	if canonical, err := gitignoreIsCanonicalCASIgnore(gitignorePath); err != nil || canonical {
		t.Fatalf("gitignoreIsCanonicalCASIgnore(negation-shadowed) = (%v, %v), want (false, nil)", canonical, err)
	}

	// Ground truth: real git does NOT ignore the store here (the negation
	// wins), confirming the scenario is a genuine divergence, not a test
	// artifact.
	assertRealGitDoesNotIgnore(t, home, "cache/artifacts/skills/deadbeef/layer.json")

	bundle := testBundle(t, map[string]string{"SKILL.md": "# fetched\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err != nil {
		t.Fatalf("MaterializeToStore: %v", err)
	}

	// After canonicalization the negation/shadow lines must be gone (the
	// managed block owner rewrites the WHOLE file's managed content; the
	// stray post-block lines are user-authored content EnsureProvenanceGitignore
	// preserves verbatim outside the block, so assert the STORE is now
	// genuinely ignored per real git rather than assuming the shadow lines
	// were dropped).
	assertRealGitDoesIgnore(t, home, "cache/artifacts/skills/deadbeef/layer.json")
}

// TestGitignoreIsCanonicalCASIgnore_TerminalBlockHappyPathFastReturns proves
// the perf win survives the round-3 structural fix: a canonical regular
// file whose managed block is genuinely TERMINAL (nothing but blank/comment
// lines follow it) still reports canonical, so EnsureAndVerifyCASIgnore's
// fast path still fires on the steady-state happy path.
func TestGitignoreIsCanonicalCASIgnore_TerminalBlockHappyPathFastReturns(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	ls := NewLocalSource(home, nil)
	if err := ls.EnsureProvenanceGitignore(nil); err != nil {
		t.Fatalf("install CAS ignore: %v", err)
	}
	gitignorePath := filepath.Join(home, gitignoreFileName)
	canonical, err := gitignoreIsCanonicalCASIgnore(gitignorePath)
	if err != nil {
		t.Fatalf("gitignoreIsCanonicalCASIgnore: %v", err)
	}
	if !canonical {
		data, _ := os.ReadFile(gitignorePath)
		t.Fatalf("expected the canonical happy-path .gitignore to satisfy the fast-path gate, content=%q", data)
	}

	// A trailing comment or blank line after the block must still be
	// tolerated (the terminal-block rule only disqualifies PATTERN lines).
	withTrailingComment := string(mustReadFile(t, gitignorePath)) + "\n# a trailing user comment\n\n"
	if err := os.WriteFile(gitignorePath, []byte(withTrailingComment), 0o644); err != nil {
		t.Fatalf("seed trailing comment: %v", err)
	}
	canonical, err = gitignoreIsCanonicalCASIgnore(gitignorePath)
	if err != nil {
		t.Fatalf("gitignoreIsCanonicalCASIgnore (trailing comment): %v", err)
	}
	if !canonical {
		t.Fatalf("expected a trailing comment/blank line after the block to still satisfy the fast-path gate")
	}
}

// mustReadFile is a tiny t.Fatal-on-error os.ReadFile wrapper, local to this
// file's real-git-backed regression tests.
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// realGitAvailable reports whether the real `git` binary can be invoked, and
// skips the calling test if not (CI-portable: these regression tests assert
// against the REAL git engine, not go-git, per the t9 round-2 finding that
// go-git-backed approximations are exactly what diverged from git).
func realGitAvailable(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("real git binary not available")
	}
	return bin
}

// assertRealGitDoesIgnore plants relPath under repoRoot (a real git
// repository) and asserts the real `git status --porcelain` does NOT report
// it — i.e., real git ignores it.
func assertRealGitDoesIgnore(t *testing.T, repoRoot, relPath string) {
	t.Helper()
	assertRealGitIgnoreState(t, repoRoot, relPath, true)
}

// assertRealGitDoesNotIgnore is the negative counterpart of
// assertRealGitDoesIgnore.
func assertRealGitDoesNotIgnore(t *testing.T, repoRoot, relPath string) {
	t.Helper()
	assertRealGitIgnoreState(t, repoRoot, relPath, false)
}

func assertRealGitIgnoreState(t *testing.T, repoRoot, relPath string, wantIgnored bool) {
	t.Helper()
	gitBin := realGitAvailable(t)
	full := filepath.Join(repoRoot, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir probe path: %v", err)
	}
	if err := os.WriteFile(full, []byte("probe\n"), 0o644); err != nil {
		t.Fatalf("write probe path: %v", err)
	}
	defer os.RemoveAll(filepath.Join(repoRoot, strings.SplitN(filepath.ToSlash(relPath), "/", 2)[0]))

	// --untracked-files=all asks git not to collapse an untracked DIRECTORY
	// to a single line; git still collapses a wholly-IGNORED directory to
	// its own line (by design — it does not recurse into an ignored tree),
	// so the match below checks whether any reported entry is a PREFIX of
	// our probe path (covers both the exact-file and the collapsed-dir
	// cases) rather than requiring an exact path match.
	//
	// t9 round-3 hermeticity hardening: -c core.excludesFile=/dev/null plus
	// GIT_CONFIG_GLOBAL/GIT_CONFIG_SYSTEM=/dev/null neutralize the invoking
	// user's/machine's global and system git config for this subprocess, so
	// a developer or CI image with a global ignore of "cache/" (or any
	// other rule that happens to affect these probe paths) cannot silently
	// pollute the NEGATIVE assertions (assertRealGitDoesNotIgnore) into a
	// false pass.
	cmd := exec.Command(gitBin, "-C", repoRoot, "-c", "core.excludesFile=/dev/null",
		"status", "--porcelain=v1", "--ignored=matching", "--untracked-files=all")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status --porcelain --ignored: %v\n%s", err, out)
	}
	slash := filepath.ToSlash(relPath)
	reportedIgnored, reportedUntracked := false, false
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		marker, reportedPath := line[:2], line[3:]
		covers := slash == reportedPath || strings.HasPrefix(slash, strings.TrimSuffix(reportedPath, "/")+"/")
		if !covers {
			continue
		}
		switch marker {
		case "!!":
			reportedIgnored = true
		case "??":
			reportedUntracked = true
		}
	}
	switch {
	case wantIgnored && !reportedIgnored:
		t.Fatalf("expected real git to report %s as ignored (!!), got:\n%s", slash, out)
	case !wantIgnored && reportedIgnored:
		t.Fatalf("expected real git to NOT ignore %s, but it was reported ignored (!!):\n%s", slash, out)
	case !wantIgnored && !reportedUntracked:
		t.Fatalf("expected real git to report %s as untracked (??) since it is not ignored, got:\n%s", slash, out)
	}
}

// TestMaterializeToStorePreservesExplicitDirEntry covers the explicit
// directory-entry write path.
func TestMaterializeToStorePreservesExplicitDirEntry(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle, err := NormalizeBundle(func(emit func(RawBundleEntry) error) error {
		if err := emit(RawBundleEntry{Path: "instructions", Kind: rawKindDir, Mode: fs.FileMode(0o755)}); err != nil {
			return err
		}
		return emit(RawBundleEntry{Path: "instructions/x.md", Kind: rawKindFile, Mode: 0o644, Size: 1, Data: []byte("x")})
	}, BundleLimits{})
	if err != nil {
		t.Fatalf("build bundle: %v", err)
	}
	storePath, _, _, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storePath, "instructions", "x.md")); err != nil {
		t.Fatalf("nested file under explicit dir entry missing: %v", err)
	}
}

// --- H7: VerifyArtifactStoreDigest (read-only integrity check) -------------

func TestVerifyArtifactStoreDigest_AbsentEntryReportsNotPresent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "# a\n"})
	present, matches := VerifyArtifactStoreDigest(home, "skills", "sha256:"+strings.Repeat("0", 64), bundle)
	if present || matches {
		t.Fatalf("expected (false, false) for a CAS entry that was never materialized, got (%v, %v)", present, matches)
	}
}

func TestVerifyArtifactStoreDigest_VerifiesUntamperedEntry(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "# a\n"})
	_, digest, _, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	present, matches := VerifyArtifactStoreDigest(home, "skills", digest, bundle)
	if !present || !matches {
		t.Fatalf("expected an untampered materialized entry to verify, got present=%v matches=%v", present, matches)
	}
}

// TestVerifyArtifactStoreDigest_DetectsTamperWithoutWriting is the H7
// no-self-heal contract: unlike MaterializeToStore, a read-only verify call
// must report the tamper it finds rather than silently quarantining/
// re-extracting it — a caller trying to DETECT tampering must not have the
// evidence erased by the act of checking.
func TestVerifyArtifactStoreDigest_DetectsTamperWithoutWriting(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "# a\n"})
	storePath, digest, _, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	// Restore the write bit first (published store files are read-only, t3
	// review #2c) to simulate a privileged tamper.
	if err := os.Chmod(filepath.Join(storePath, "SKILL.md"), 0o644); err != nil {
		t.Fatalf("restore write bit for tamper: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storePath, "SKILL.md"), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	present, matches := VerifyArtifactStoreDigest(home, "skills", digest, bundle)
	if !present {
		t.Fatal("expected the tampered entry to still be reported present")
	}
	if matches {
		t.Fatal("expected the tampered entry to fail verification")
	}

	// The evidence must survive the check: still tampered, not
	// quarantined/re-extracted, and no ".corrupt-" sibling created.
	data, err := os.ReadFile(filepath.Join(storePath, "SKILL.md"))
	if err != nil || string(data) != "TAMPERED" {
		t.Fatalf("expected the tamper to survive a read-only verify call, data=%q err=%v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(storePath))
	if err != nil {
		t.Fatalf("read store root: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt-") {
			t.Fatalf("expected NO quarantine side effect from a read-only verify, found %s", e.Name())
		}
	}
}

// --- t3b: CAS orphan GC (H11/H17) -------------------------------------------

// lockLiveArtifact registers project at projectPath as a bound project in
// cfg/home (t.Setenv("AGENTS_HOME", home) must already be in effect) and
// writes its .agentsrc.lock so it references digest as a kind:artifact unit —
// the shape LiveArtifactDigests() reads.
func lockLiveArtifact(t *testing.T, home, projectName, projectPath, ref, digest string) {
	t.Helper()
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AddProject(projectName, projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := WriteUnitsLock(projectPath, UnitsLock{Units: map[string]LockedUnit{
		ref: {Kind: UnitKindArtifact, Digest: digest, FetchedAt: "2026-01-01T00:00:00Z"},
	}}); err != nil {
		t.Fatalf("write units lock: %v", err)
	}
}

// TestGCOrphanedArtifactStore_RemovesOnlyTheOrphan is the core H11 positive
// case: a store with one digest referenced by a project's lock (live) and one
// digest referenced by nobody (orphan) — GC must remove EXACTLY the orphan.
func TestGCOrphanedArtifactStore_RemovesOnlyTheOrphan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	liveBundle := testBundle(t, map[string]string{"SKILL.md": "# live\n"})
	livePath, liveDigest, _, err := MaterializeToStore(home, "skills", liveBundle)
	if err != nil {
		t.Fatalf("materialize live: %v", err)
	}
	orphanBundle := testBundle(t, map[string]string{"SKILL.md": "# orphan\n"})
	orphanPath, orphanDigest, _, err := MaterializeToStore(home, "skills", orphanBundle)
	if err != nil {
		t.Fatalf("materialize orphan: %v", err)
	}

	lockLiveArtifact(t, home, "proj", filepath.Join(t.TempDir(), "proj"), "src:skill/live@main", liveDigest)

	live, err := LiveArtifactDigests()
	if err != nil {
		t.Fatalf("LiveArtifactDigests: %v", err)
	}
	if !live[liveDigest] {
		t.Fatalf("expected %q to be in the live digest set", liveDigest)
	}
	if live[orphanDigest] {
		t.Fatalf("did not expect the orphan digest %q to be live", orphanDigest)
	}

	removed, err := GCOrphanedArtifactStore(home, "skills", live)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(removed) != 1 || removed[0] != orphanDigest {
		t.Fatalf("expected GC to remove exactly [%s], got %v", orphanDigest, removed)
	}
	if _, err := os.Stat(livePath); err != nil {
		t.Fatalf("expected the LIVE store entry to survive GC, got err=%v", err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("expected the orphan store entry to be removed, got err=%v", err)
	}
}

// TestGCOrphanedArtifactStore_ConcurrentlyAddedLockSurvives proves the H11
// two-step contract end to end: a digest that looked orphaned when first
// materialized is protected the moment ANOTHER project's lock references it
// and LiveArtifactDigests is recomputed BEFORE GC runs — a caller who
// recomputes liveDigests right before calling GC never deletes a
// newly-referenced digest, even though it existed in the store unreferenced
// for a while first.
func TestGCOrphanedArtifactStore_ConcurrentlyAddedLockSurvives(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	// D_new starts life unreferenced by any lock — a naive GC computed right
	// now would consider it an orphan.
	newBundle := testBundle(t, map[string]string{"SKILL.md": "# will become live\n"})
	newPath, newDigest, _, err := MaterializeToStore(home, "skills", newBundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	trueOrphanBundle := testBundle(t, map[string]string{"SKILL.md": "# never referenced\n"})
	trueOrphanPath, trueOrphanDigest, _, err := MaterializeToStore(home, "skills", trueOrphanBundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	// A second project's install races in AFTER materialize but BEFORE GC:
	// it resolves and locks D_new.
	lockLiveArtifact(t, home, "proj2", filepath.Join(t.TempDir(), "proj2"), "src:skill/newer@main", newDigest)

	// The caller does the right thing: recompute liveDigests fresh, THEN GC —
	// never reuses a set snapshotted before the concurrent lock landed.
	live, err := LiveArtifactDigests()
	if err != nil {
		t.Fatalf("LiveArtifactDigests: %v", err)
	}
	if !live[newDigest] {
		t.Fatalf("expected the concurrently-locked digest %q to be captured in the fresh union", newDigest)
	}

	removed, err := GCOrphanedArtifactStore(home, "skills", live)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(removed) != 1 || removed[0] != trueOrphanDigest {
		t.Fatalf("expected GC to remove exactly the true orphan [%s], got %v", trueOrphanDigest, removed)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected the concurrently-referenced entry to survive GC, got err=%v", err)
	}
	if _, err := os.Stat(trueOrphanPath); !os.IsNotExist(err) {
		t.Fatalf("expected the true orphan to be removed, got err=%v", err)
	}
}

// TestGCOrphanedArtifactStore_NeverTouchesNonDigestEntries is the H17
// negative case: a quarantine sibling (".corrupt-<nanos>") and a leftover
// staging directory (".materialize-staging-*") sit alongside real digest
// entries in the store root — GC must leave BOTH alone; only a directory
// whose name is exactly a 64-hex digest is ever a delete candidate.
func TestGCOrphanedArtifactStore_NeverTouchesNonDigestEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	orphanBundle := testBundle(t, map[string]string{"SKILL.md": "# orphan\n"})
	orphanPath, orphanDigest, _, err := MaterializeToStore(home, "skills", orphanBundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	root := ArtifactStoreRoot(home, "skills")
	quarantine := orphanPath + ".corrupt-999"
	if err := os.MkdirAll(quarantine, 0o755); err != nil {
		t.Fatalf("mkdir quarantine sibling: %v", err)
	}
	staging := filepath.Join(root, ".materialize-staging-leftover")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("mkdir staging leftover: %v", err)
	}

	removed, err := GCOrphanedArtifactStore(home, "skills", map[string]bool{})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(removed) != 1 || removed[0] != orphanDigest {
		t.Fatalf("expected GC to remove exactly the real orphan digest [%s], got %v", orphanDigest, removed)
	}
	if _, err := os.Stat(quarantine); err != nil {
		t.Fatalf("expected the quarantine sibling to survive GC untouched, got err=%v", err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("expected the staging leftover to survive GC untouched, got err=%v", err)
	}
}

// TestGCOrphanedArtifactStore_MissingStoreRootIsNotAnError covers the
// not-yet-populated store: nothing to collect, H11 vacuously satisfied.
func TestGCOrphanedArtifactStore_MissingStoreRootIsNotAnError(t *testing.T) {
	home := t.TempDir()
	removed, err := GCOrphanedArtifactStore(home, "skills", map[string]bool{})
	if err != nil {
		t.Fatalf("expected a missing store root to be a no-op, got err=%v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected nothing removed from a missing store root, got %v", removed)
	}
}

// TestLiveArtifactDigests_UnreadableBoundLockFailsClosed is the t3b round-2
// data-loss BLOCKER regression: a BOUND project whose .agentsrc.lock EXISTS
// but cannot be parsed (a concurrent agentslock.Update mid-write, an IO/NFS
// hiccup, a partial/corrupt write) must make LiveArtifactDigests return an
// ERROR — never a silently-partial union that would let GC treat that
// project's live digests as orphans and delete them. The critical assertion
// is the full sequence a real caller runs: LiveArtifactDigests errors, so GC
// is NEVER reached, so the store entry that project references SURVIVES.
func TestLiveArtifactDigests_UnreadableBoundLockFailsClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	// A CAS entry only this project references — if the union under-collects,
	// GC would delete it. It must survive the whole test.
	liveBundle := testBundle(t, map[string]string{"SKILL.md": "# still referenced\n"})
	livePath, liveDigest, _, err := MaterializeToStore(home, "skills", liveBundle)
	if err != nil {
		t.Fatalf("materialize live: %v", err)
	}

	// Bind the project and write a LEGITIMATE lock referencing liveDigest…
	projPath := filepath.Join(t.TempDir(), "proj")
	lockLiveArtifact(t, home, "proj", projPath, "src:skill/live@main", liveDigest)
	// …then CORRUPT that on-disk lock so ReadUnits fails to parse it (the file
	// exists — this is the UNSAFE case, distinct from a never-written lock).
	lockPath := AgentsLockPath(projPath)
	if err := os.WriteFile(lockPath, []byte("{ this is not valid json"), 0o644); err != nil {
		t.Fatalf("corrupt lock: %v", err)
	}

	// Fail-closed contract: the union must be an ERROR, not a partial set.
	live, err := LiveArtifactDigests()
	if err == nil {
		t.Fatalf("expected LiveArtifactDigests to fail closed on an unreadable bound lock, got live=%v", live)
	}
	if live != nil {
		t.Fatalf("expected a nil digest set alongside the error, got %v", live)
	}

	// The real caller aborts here (never calls GC on an errored union). Prove
	// the live entry is untouched — the data-loss path is closed.
	if _, statErr := os.Stat(livePath); statErr != nil {
		t.Fatalf("expected the still-referenced CAS entry to survive when the union errors, got err=%v", statErr)
	}
}

// TestLiveArtifactDigests_MissingLockContributesEmptyNotError is the other
// half of the round-2 fix: a BOUND, never-installed project (no .agentsrc.lock
// on disk at all) is the SAFE case — it provably has zero artifact units, so
// it contributes nothing WITHOUT erroring. The fail-closed branch must fire
// only for a present-but-unreadable lock, not for a legitimately-absent one,
// or GC could never run whenever any registered project simply never installed
// packages.
func TestLiveArtifactDigests_MissingLockContributesEmptyNotError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	// Bind a project but write NO lock for it (never installed anything).
	emptyProj := filepath.Join(t.TempDir(), "empty-proj")
	if err := os.MkdirAll(emptyProj, 0o755); err != nil {
		t.Fatalf("mkdir empty project: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AddProject("empty-proj", emptyProj)
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if _, statErr := os.Stat(AgentsLockPath(emptyProj)); !os.IsNotExist(statErr) {
		t.Fatalf("precondition: expected NO lock on disk for the never-installed project, stat err=%v", statErr)
	}

	// A second project with a REAL artifact lock, so the union is non-empty and
	// we prove the missing-lock project neither errored nor polluted it.
	liveBundle := testBundle(t, map[string]string{"SKILL.md": "# live\n"})
	_, liveDigest, _, err := MaterializeToStore(home, "skills", liveBundle)
	if err != nil {
		t.Fatalf("materialize live: %v", err)
	}
	lockLiveArtifact(t, home, "installed-proj", filepath.Join(t.TempDir(), "installed"), "src:skill/live@main", liveDigest)

	live, err := LiveArtifactDigests()
	if err != nil {
		t.Fatalf("expected a missing (never-installed) lock to be a clean empty contribution, got err=%v", err)
	}
	if !live[liveDigest] {
		t.Fatalf("expected the installed project's digest %q in the union", liveDigest)
	}
	if len(live) != 1 {
		t.Fatalf("expected exactly the one real digest in the union, got %v", live)
	}
}

// TestGCOrphanedArtifactStore_ConcurrentCallersDoNotRace exercises repeated
// concurrent GC sweeps over the SAME store+family (e.g. two overlapping
// install/refresh invocations both triggering GC) under -race: every
// goroutine must return without a data race, and the end state must be
// exactly the live set surviving, the orphan gone.
func TestGCOrphanedArtifactStore_ConcurrentCallersDoNotRace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	liveBundle := testBundle(t, map[string]string{"SKILL.md": "# live\n"})
	livePath, liveDigest, _, err := MaterializeToStore(home, "skills", liveBundle)
	if err != nil {
		t.Fatalf("materialize live: %v", err)
	}
	orphanBundle := testBundle(t, map[string]string{"SKILL.md": "# orphan\n"})
	orphanPath, _, _, err := MaterializeToStore(home, "skills", orphanBundle)
	if err != nil {
		t.Fatalf("materialize orphan: %v", err)
	}
	live := map[string]bool{liveDigest: true}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := GCOrphanedArtifactStore(home, "skills", live)
			errs[idx] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent GC call %d: %v", i, err)
		}
	}
	if _, err := os.Stat(livePath); err != nil {
		t.Fatalf("expected the live entry to survive concurrent GC, got err=%v", err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("expected the orphan entry to be gone after concurrent GC, got err=%v", err)
	}
}
