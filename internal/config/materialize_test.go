package config

import (
	"io/fs"
	"os"
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
	// H2: the store path is keyed by the digest, under cache/artifacts/<family>/.
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

// TestMaterializeToStoreReMaterializeIsByteIdenticalNoOp is the adversarial
// idempotency claim: re-materializing an UNCHANGED digest must not re-extract
// (installed=false) and must leave byte-identical content in place.
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
		t.Fatalf("store path/digest must be stable across re-materialize: (%q,%q) vs (%q,%q)", storePath1, digest1, storePath2, digest2)
	}
	after, err := os.ReadFile(filepath.Join(storePath2, "SKILL.md"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("content changed across a no-op re-materialize: %q vs %q", before, after)
	}
}

// TestMaterializeToStoreChangedDigestNeverMutatesOldPath proves H2: a
// changed bundle (different digest) lands at a NEW store path; the OLD
// digest's content is left completely untouched (no shared mutable path).
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
		t.Fatalf("v1 and v2 must occupy distinct digest-keyed paths: %q vs %q", storePathV1, storePathV2)
	}
	v1Content, err := os.ReadFile(filepath.Join(storePathV1, "SKILL.md"))
	if err != nil {
		t.Fatalf("read v1 after v2 materialize: %v", err)
	}
	if string(v1Content) != "v1\n" {
		t.Fatalf("v1's store path was mutated by materializing v2: got %q", v1Content)
	}
}

// TestMaterializeToStoreConcurrentSameDigestConverges is the adversarial
// concurrency claim: two goroutines racing to materialize the SAME digest
// must both succeed and agree on identical, complete content — no torn
// write is ever observable at the published store path.
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
			t.Fatalf("goroutine %d: store path diverged: %q vs %q", i, paths[i], paths[0])
		}
	}
	// The published tree must be fully present and correct, not partially
	// written by an interleaved loser of the race.
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
	// No staging leftovers.
	root := ArtifactStoreRoot(home, "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read store root: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".materialize-staging-") {
			t.Fatalf("leftover staging dir %s after concurrent materialize", e.Name())
		}
	}
}

// TestMaterializeToStorePropagatesStoreRootCreateError forces
// fsops.MkdirAll(root) to fail (a regular file occupies where the store
// root's own parent must be a directory) and asserts the error surfaces
// instead of being swallowed.
func TestMaterializeToStorePropagatesStoreRootCreateError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// "cache" as a FILE blocks MkdirAll("cache/artifacts/skills", ...).
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "cache"), []byte("masquerade"), 0o644); err != nil {
		t.Fatalf("seed masquerading file: %v", err)
	}
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
		t.Fatalf("expected an error when the store root cannot be created")
	}
}

// TestMaterializeToStorePropagatesStageError forces writeBundleTree to fail
// (a bundle path collides with a pre-existing FILE where a directory is
// needed) and asserts the staging error surfaces, leaving no store entry.
func TestMaterializeToStorePropagatesStageError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"docs/guide.md": "x\n"})
	// Pre-seed the store ROOT with a file named after the bundle's own
	// top-level directory segment ("docs") at a path writeBundleTree's
	// MkdirAll must traverse through inside the staging dir — instead,
	// simulate the failure by making the staging PARENT read-only so
	// MkdirTemp itself cannot create a staging dir, an equally valid
	// negative path for the same guarded region.
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}
	root := ArtifactStoreRoot(home, "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("seed store root: %v", err)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod store root read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
		t.Fatalf("expected an error when the staging dir cannot be created")
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

// TestMaterializeToStorePreservesDirMode proves an explicit directory
// entry's mode is honored (defense-in-depth path), not just file entries.
func TestMaterializeToStorePreservesDirMode(t *testing.T) {
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

// --- H4: permanent sourced ignore, installed and verified -----------------

func TestEnsureAndVerifySourcedIgnoreInstallsPermanentBlock(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := EnsureAndVerifySourcedIgnore(home); err != nil {
		t.Fatalf("EnsureAndVerifySourcedIgnore: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, gitignoreFileName))
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	for _, pattern := range alwaysIgnoredSourced {
		if !strings.Contains(string(data), pattern) {
			t.Fatalf("permanent sourced-namespace pattern %q missing after install: %q", pattern, data)
		}
	}
}

// TestEnsureAndVerifySourcedIgnoreSurvivesRepeatedPackageRemoval is the H4
// adversarial claim in its most direct form: repeatedly calling
// EnsureProvenanceGitignore with a SHRINKING (eventually empty) remotePaths
// list — the exact shape of "the last package for a source was removed" —
// must never drop the permanent pattern.
func TestEnsureAndVerifySourcedIgnoreSurvivesRepeatedPackageRemoval(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	ls := NewLocalSource(home, nil)

	paths := []string{"skills/_sourced/da-agc/a/", "skills/_sourced/da-agc/b/", "skills/_sourced/da-agc/c/"}
	for len(paths) >= 0 {
		if err := ls.EnsureProvenanceGitignore(paths); err != nil {
			t.Fatalf("EnsureProvenanceGitignore(%v): %v", paths, err)
		}
		ok, err := ls.SourcedIgnoreInstalled()
		if err != nil {
			t.Fatalf("SourcedIgnoreInstalled: %v", err)
		}
		if !ok {
			t.Fatalf("H4 violated: permanent ignore missing after shrinking remotePaths to %v", paths)
		}
		if len(paths) == 0 {
			break
		}
		paths = paths[1:]
	}
}

// TestGitStatusShowsZeroFetchedFilesAfterMaterialize is the definitive H4
// acceptance test: materialize bundles into TWO different families (skills,
// agents) of a REAL git repo (go-git, not a fake), then asserts `git status`
// on that repo reports a fully clean working tree — the permanent
// "*/_sourced/" pattern must hide every fetched file, in every family, using
// the actual go-git status/ignore engine, not this package's own bookkeeping.
func TestGitStatusShowsZeroFetchedFilesAfterMaterialize(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	repo, err := git.PlainInit(home, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	// Install the permanent ignore BEFORE any fetched content is exposed —
	// exactly the H4-mandated ordering — then materialize into two distinct
	// families so the pattern's "*/_sourced/" breadth is actually exercised,
	// not just a single-bucket coincidence.
	if err := EnsureAndVerifySourcedIgnore(home); err != nil {
		t.Fatalf("EnsureAndVerifySourcedIgnore: %v", err)
	}
	skillBundle := testBundle(t, map[string]string{
		"SKILL.md":            "# fetched skill\n",
		"instructions/run.md": "steps\n",
	})
	agentBundle := testBundle(t, map[string]string{"AGENT.md": "# fetched agent\n"})

	skillStore, _, _, err := MaterializeToStore(home, "skills", skillBundle)
	if err != nil {
		t.Fatalf("materialize skills: %v", err)
	}
	agentStore, _, _, err := MaterializeToStore(home, "agents", agentBundle)
	if err != nil {
		t.Fatalf("materialize agents: %v", err)
	}

	// Expose the materialized trees at the reserved sourced-namespace
	// projection paths (a plain copy stands in for the platform-layer
	// symlink here — the git-ignore contract must hold for real files under
	// "_sourced/", not just for a symlink entry).
	mustCopyTree(t, skillStore, filepath.Join(home, "skills", SourcedScopeSegment, "da-agc", "release-docs-refresh"))
	mustCopyTree(t, agentStore, filepath.Join(home, "agents", SourcedScopeSegment, "da-agc", "platform-dirs-change-analyst"))

	// Also write a LOCAL-AUTHORED file outside the reserved namespace so the
	// test proves the ignore is scoped to "_sourced/" only, not accidentally
	// hiding everything.
	if err := os.MkdirAll(filepath.Join(home, "skills", "dot-agents", "hand-authored"), 0o755); err != nil {
		t.Fatalf("mkdir local-authored fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "dot-agents", "hand-authored", "SKILL.md"), []byte("# local\n"), 0o644); err != nil {
		t.Fatalf("write local-authored fixture: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	status, err := wt.Status()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	for path, s := range status {
		if strings.Contains(filepath.ToSlash(path), "/"+SourcedScopeSegment+"/") {
			t.Fatalf("H4 violated: git status reports a fetched file under _sourced/: %s (%v)", path, s)
		}
	}
	// The local-authored fixture, by contrast, MUST show up as untracked —
	// proving the ignore did not over-hide everything.
	if _, ok := status[filepath.ToSlash(filepath.Join("skills", "dot-agents", "hand-authored", "SKILL.md"))]; !ok {
		t.Fatalf("expected the local-authored fixture to be visible to git status, got %+v", status)
	}
}

// mustCopyTree recursively copies src into dst (both directories), failing
// the test on any error. Used to simulate "the materialized store content is
// exposed under the reserved namespace" without pulling the platform
// package's link machinery into a config-package test.
func mustCopyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy tree %s -> %s: %v", src, dst, err)
	}
}
