package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
)

func testMaterializeBundle(files map[string]string) config.Bundle {
	entries := make([]config.BundleEntry, 0, len(files))
	for path, content := range files {
		entries = append(entries, config.BundleEntry{Path: path, Data: []byte(content), Mode: 0o644})
	}
	return config.Bundle{Entries: entries}
}

// materializeUnit materializes a bundle into the store and returns the fully
// populated ResolvedUnit a caller (t3) would hand to ProjectResolvedUnits.
func materializeUnit(t *testing.T, home, family, sourceID, name string, files map[string]string) ResolvedUnit {
	t.Helper()
	casPath, digest, err := MaterializeArtifact(home, family, sourceID, name, testMaterializeBundle(files))
	if err != nil {
		t.Fatalf("MaterializeArtifact(%s/%s): %v", family, name, err)
	}
	return ResolvedUnit{Family: family, Name: name, SourceID: sourceID, Digest: digest, CASPath: casPath}
}

// --- H15: identity-component containment ------------------------------------

func TestValidateResolvedUnitIdentity_RejectsTraversalAndReservedScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                    string
		family, sourceID, unitN string
		localScopes             []string
	}{
		{"dotdot source id", "skills", "..", "x", nil},
		{"separator source id", "skills", "a/b", "x", nil},
		{"backslash source id", "skills", `a\b`, "x", nil},
		{"absolute source id", "skills", "/abs", "x", nil},
		{"global source id", "skills", "global", "x", nil},
		{"project-scope source id", "skills", "dot-agents", "x", []string{"dot-agents"}},
		{"dotdot name", "skills", "da-agc", "..", nil},
		{"separator name", "skills", "da-agc", "a/b", nil},
		{"dotdot family", "..", "da-agc", "x", nil},
		{"separator family", "a/b", "da-agc", "x", nil},
		{"empty source id", "skills", "", "x", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateResolvedUnitIdentity(tc.family, tc.sourceID, tc.unitN, tc.localScopes...); err == nil {
				t.Fatalf("expected %s to be rejected", tc.name)
			}
		})
	}
}

func TestValidateResolvedUnitIdentity_ReservedIsErrorsIs(t *testing.T) {
	t.Parallel()
	if err := ValidateResolvedUnitIdentity("skills", "global", "x"); !errors.Is(err, ErrReservedSourceID) {
		t.Fatalf("expected ErrReservedSourceID for a global source id, got %v", err)
	}
}

func TestValidateResolvedUnitIdentity_AcceptsCanonical(t *testing.T) {
	t.Parallel()
	if err := ValidateResolvedUnitIdentity("skills", "da-agc", "release-docs-refresh", "dot-agents"); err != nil {
		t.Fatalf("expected a canonical unit to be accepted, got %v", err)
	}
}

// TestMaterializeArtifact_RejectsTraversalBeforeAnyWrite proves the H15 gate
// runs BEFORE any store write: a rejected identity leaves no cache dir behind.
func TestMaterializeArtifact_RejectsTraversalBeforeAnyWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if _, _, err := MaterializeArtifact(home, "skills", "..", "x", testMaterializeBundle(map[string]string{"SKILL.md": "x\n"})); err == nil {
		t.Fatalf("expected a traversal source id to be rejected")
	}
	if _, err := os.Stat(filepath.Join(home, "cache")); !os.IsNotExist(err) {
		t.Fatalf("expected no store write before the H15 gate rejects, err=%v", err)
	}
}

// --- H2/H16 via the platform entry point ------------------------------------

func TestMaterializeArtifact_ReturnsCASPathForDigest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	casPath, digest, err := MaterializeArtifact(home, "skills", "da-agc", "release-docs-refresh", testMaterializeBundle(map[string]string{"SKILL.md": "# s\n"}))
	if err != nil {
		t.Fatalf("MaterializeArtifact: %v", err)
	}
	if casPath != config.ArtifactStorePath(home, "skills", digest) {
		t.Fatalf("casPath %q != ArtifactStorePath for digest %q", casPath, digest)
	}
	if !strings.HasPrefix(casPath, filepath.Join(home, "cache", "artifacts", "skills")) {
		t.Fatalf("casPath is not under the CAS root: %q", casPath)
	}
}

// --- ValidateResolvedUnit: CAS-path binding (H13/H16) -----------------------

func TestValidateResolvedUnit_RejectsForeignCASPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	u := materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "# s\n"})
	// A caller that tampers CASPath to point outside the store must be rejected.
	u.CASPath = filepath.Join(home, "evil", "elsewhere")
	if err := ValidateResolvedUnit(home, u); err == nil {
		t.Fatalf("expected a foreign CAS path to be rejected")
	}
}

func TestValidateResolvedUnit_RejectsDigestMismatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	u := materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "# s\n"})
	u.Digest = "sha256:" + strings.Repeat("0", 64) // valid shape, wrong digest → CASPath no longer matches
	if err := ValidateResolvedUnit(home, u); err == nil {
		t.Fatalf("expected a digest/CAS-path mismatch to be rejected")
	}
}

// --- H13 CRITICAL: per-project isolation, caller-driven ---------------------

// TestProjectResolvedUnits_PerProjectIsolation is the CRITICAL H13 acceptance
// test: two DIFFERENT projects resolving DIFFERENT digests of the SAME
// source/name project to their OWN repos, each linking directly to its own
// immutable CAS digest — neither repoints the other, and there is no shared
// mutable alias. This is exactly the collision the old global _sourced scan
// caused.
func TestProjectResolvedUnits_PerProjectIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repoA := filepath.Join(t.TempDir(), "repoA")
	repoB := filepath.Join(t.TempDir(), "repoB")

	// Same source/name, different content ⇒ different digests.
	unitA := materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\nversion A\n"})
	unitB := materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\nversion B\n"})
	if unitA.Digest == unitB.Digest {
		t.Fatalf("expected distinct digests for distinct content")
	}

	platforms := []Platform{NewClaude()}
	if _, err := ProjectResolvedUnits("projA", repoA, []ResolvedUnit{unitA}, platforms, false, true, "projA"); err != nil {
		t.Fatalf("project A: %v", err)
	}
	if _, err := ProjectResolvedUnits("projB", repoB, []ResolvedUnit{unitB}, platforms, false, true, "projB"); err != nil {
		t.Fatalf("project B: %v", err)
	}

	linkA := filepath.Join(repoA, ".claude", "skills", "release-docs-refresh")
	linkB := filepath.Join(repoB, ".claude", "skills", "release-docs-refresh")
	if !links.IsManagedLink(linkA, unitA.CASPath) {
		t.Fatalf("repo A link does not point at unit A's CAS path")
	}
	if !links.IsManagedLink(linkB, unitB.CASPath) {
		t.Fatalf("repo B link does not point at unit B's CAS path")
	}
	// The decisive isolation check: A's link was NOT repointed by projecting B.
	gotA, err := os.ReadFile(filepath.Join(linkA, "SKILL.md"))
	if err != nil {
		t.Fatalf("read A: %v", err)
	}
	if !strings.Contains(string(gotA), "version A") {
		t.Fatalf("H13 violated: repo A now resolves B's content: %q", gotA)
	}
	gotB, err := os.ReadFile(filepath.Join(linkB, "SKILL.md"))
	if err != nil {
		t.Fatalf("read B: %v", err)
	}
	if !strings.Contains(string(gotB), "version B") {
		t.Fatalf("repo B resolves the wrong content: %q", gotB)
	}
}

// TestProjectResolvedUnits_AuthorityIsCallerSet proves H13 "do not
// self-discover": a unit that was materialized into the store but is NOT in
// the caller's set is NEVER projected. Authority flows IN, not from a store
// scan.
func TestProjectResolvedUnits_AuthorityIsCallerSet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")

	included := materializeUnit(t, home, "skills", "da-agc", "included", map[string]string{"SKILL.md": "---\nname: included\n---\n"})
	// materialized but deliberately NOT passed to ProjectResolvedUnits
	_ = materializeUnit(t, home, "skills", "da-agc", "excluded", map[string]string{"SKILL.md": "---\nname: excluded\n---\n"})

	platforms := []Platform{NewClaude()}
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{included}, platforms, false, true, "proj"); err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "skills", "included")); err != nil {
		t.Fatalf("expected the included unit to be projected: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "skills", "excluded")); !os.IsNotExist(err) {
		t.Fatalf("H13 violated: a store unit NOT in the caller set was projected (self-discovery), err=%v", err)
	}
}

// TestProjectResolvedUnits_ExactPruneRemovesDroppedUnit proves R5 through the
// caller-driven path: when a unit is dropped from the resolved set (while at
// least one sibling remains, keeping the bucket dir in the exact scan), its
// projected link is pruned on the next exact projection, and the store entry
// is untouched. (Exact/prune scans only directories a wanted target still
// owns — the established projection semantics.)
func TestProjectResolvedUnits_ExactPruneRemovesDroppedUnit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")

	keep := materializeUnit(t, home, "skills", "da-agc", "keep-me", map[string]string{"SKILL.md": "---\nname: keep-me\n---\n"})
	drop := materializeUnit(t, home, "skills", "da-agc", "drop-me", map[string]string{"SKILL.md": "---\nname: drop-me\n---\n"})
	platforms := []Platform{NewClaude()}
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{keep, drop}, platforms, false, true, "proj"); err != nil {
		t.Fatalf("project (both): %v", err)
	}
	dropLink := filepath.Join(repo, ".claude", "skills", "drop-me")
	keepLink := filepath.Join(repo, ".claude", "skills", "keep-me")
	if _, err := os.Lstat(dropLink); err != nil {
		t.Fatalf("expected drop-me projected: %v", err)
	}

	// Re-project with only keep-me → exact/prune removes drop-me's link.
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{keep}, platforms, false, true, "proj"); err != nil {
		t.Fatalf("project (keep only): %v", err)
	}
	if _, err := os.Lstat(dropLink); !os.IsNotExist(err) {
		t.Fatalf("expected the dropped unit's projection to be pruned, err=%v", err)
	}
	if !links.IsManagedLink(keepLink, keep.CASPath) {
		t.Fatalf("expected the kept unit's projection to survive")
	}
	// Store entry survives the prune (store GC is deferred).
	if _, err := os.Stat(drop.CASPath); err != nil {
		t.Fatalf("store entry must survive projection prune: %v", err)
	}
}

// --- H17: no RemoveAll-after-check on the projection replace ----------------

// TestProjectResolvedUnits_RefusesToDeleteRealUserDir is the H17
// fail-before-fix guard: a REAL user directory (with content) sitting at the
// repo projection target must NEVER be deleted — the CAS-direct link step
// refuses to replace a non-symlink occupant (no RemoveAll-after-check).
func TestProjectResolvedUnits_RefusesToDeleteRealUserDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")

	u := materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\n"})

	// Seed a REAL user directory (with a skill marker + user content) exactly
	// where the projection would land.
	target := filepath.Join(repo, ".claude", "skills", "release-docs-refresh")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("seed user dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("USER CONTENT\n"), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	platforms := []Platform{NewClaude()}
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{u}, platforms, false, true, "proj"); err == nil {
		t.Fatalf("expected projection to refuse replacing a real user directory")
	}
	got, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatalf("user content was removed or made unreadable: %v", err)
	}
	if string(got) != "USER CONTENT\n" {
		t.Fatalf("user content changed: %q", got)
	}
}

// TestProjectResolvedUnits_RepointsExistingManagedSymlink proves the H17
// atomic swap correctly REPOINTS an existing managed symlink (the normal
// digest-change case) without a RemoveAll and with no absent window.
func TestProjectResolvedUnits_RepointsExistingManagedSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")

	v1 := materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\nv1\n"})
	v2 := materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\nv2\n"})

	platforms := []Platform{NewClaude()}
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{v1}, platforms, false, true, "proj"); err != nil {
		t.Fatalf("project v1: %v", err)
	}
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{v2}, platforms, false, true, "proj"); err != nil {
		t.Fatalf("project v2 (repoint): %v", err)
	}
	link := filepath.Join(repo, ".claude", "skills", "release-docs-refresh")
	if !links.IsManagedLink(link, v2.CASPath) {
		t.Fatalf("expected the managed symlink to be repointed to v2's CAS path")
	}
	got, err := os.ReadFile(filepath.Join(link, "SKILL.md"))
	if err != nil {
		t.Fatalf("read after repoint: %v", err)
	}
	if !strings.Contains(string(got), "v2") {
		t.Fatalf("repoint did not update content: %q", got)
	}
}

// TestProjectResolvedUnits_RejectsInvalidUnit proves a single invalid unit
// fails the whole call closed (before any projection).
func TestProjectResolvedUnits_RejectsInvalidUnit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	bad := ResolvedUnit{Family: "skills", Name: "x", SourceID: "..", Digest: "sha256:" + strings.Repeat("0", 64), CASPath: config.ArtifactStorePath(home, "skills", "sha256:"+strings.Repeat("0", 64))}
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{bad}, []Platform{NewClaude()}, false, true); err == nil {
		t.Fatalf("expected an invalid unit to fail the projection closed")
	}
}

// --- FIX 1: concurrency isolation (no package-global projection context) ----

// TestProjectResolvedUnits_ConcurrentNoCrossContamination is the fail-before-fix
// guard for the concurrency leak: two projections running CONCURRENTLY with
// DIFFERENT resolved-unit sets must never leak one project's package into the
// other. With the previous package-global projection context a concurrent
// plain/other projection could read the foreign units; now the units are pure
// invocation-local parameters, so cross-contamination is impossible. Run under
// -race, and repeated to widen the interleaving window.
func TestProjectResolvedUnits_ConcurrentNoCrossContamination(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	unitA := materializeUnit(t, home, "skills", "da-agc", "only-a", map[string]string{"SKILL.md": "---\nname: only-a\n---\n"})
	unitB := materializeUnit(t, home, "skills", "da-agc", "only-b", map[string]string{"SKILL.md": "---\nname: only-b\n---\n"})
	platforms := []Platform{NewClaude()}

	for iter := 0; iter < 12; iter++ {
		repoA := filepath.Join(t.TempDir(), "A")
		repoB := filepath.Join(t.TempDir(), "B")
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, errs[0] = ProjectResolvedUnits("projA", repoA, []ResolvedUnit{unitA}, platforms, false, true, "projA")
		}()
		go func() {
			defer wg.Done()
			_, errs[1] = ProjectResolvedUnits("projB", repoB, []ResolvedUnit{unitB}, platforms, false, true, "projB")
		}()
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("iter %d goroutine %d: %v", iter, i, err)
			}
		}
		// repoA must contain ONLY unit A; repoB ONLY unit B.
		if _, err := os.Lstat(filepath.Join(repoA, ".claude", "skills", "only-a")); err != nil {
			t.Fatalf("iter %d: repoA missing its own unit: %v", iter, err)
		}
		if _, err := os.Lstat(filepath.Join(repoA, ".claude", "skills", "only-b")); !os.IsNotExist(err) {
			t.Fatalf("iter %d: CROSS-CONTAMINATION — projB's unit leaked into repoA (err=%v)", iter, err)
		}
		if _, err := os.Lstat(filepath.Join(repoB, ".claude", "skills", "only-b")); err != nil {
			t.Fatalf("iter %d: repoB missing its own unit: %v", iter, err)
		}
		if _, err := os.Lstat(filepath.Join(repoB, ".claude", "skills", "only-a")); !os.IsNotExist(err) {
			t.Fatalf("iter %d: CROSS-CONTAMINATION — projA's unit leaked into repoB (err=%v)", iter, err)
		}
	}
}

// --- FIX 2: H17 foreign-symlink refusal, no RemoveAll fallback ---------------

// TestProjectResolvedUnits_RefusesForeignSymlink is the fail-before-fix guard
// for the H17 gap: a FOREIGN user symlink (pointing OUTSIDE managed CAS
// storage) sitting at the projection target must be left completely intact —
// the atomic swap replaces only an identity-verified managed CAS link, and
// fails closed otherwise (no RemoveAll fallback).
func TestProjectResolvedUnits_RefusesForeignSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")

	u := materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\n"})

	// A real user directory the foreign symlink points at (outside managed CAS).
	userDir := filepath.Join(t.TempDir(), "user-owned")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("seed user dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "keepme.txt"), []byte("precious"), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}
	target := filepath.Join(repo, ".claude", "skills", "release-docs-refresh")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir target parent: %v", err)
	}
	if err := os.Symlink(userDir, target); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{u}, []Platform{NewClaude()}, false, true, "proj"); err == nil {
		t.Fatalf("expected projection to fail closed against a foreign symlink")
	}
	// The foreign symlink must be untouched (still points at the user dir).
	dest, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("foreign symlink was removed/altered: %v", err)
	}
	if dest != userDir {
		t.Fatalf("foreign symlink was repointed: got %q want %q", dest, userDir)
	}
	if _, err := os.Stat(filepath.Join(userDir, "keepme.txt")); err != nil {
		t.Fatalf("user content behind the foreign symlink was disturbed: %v", err)
	}
}

// --- FIX 3: one-to-zero prune -----------------------------------------------

// TestProjectResolvedUnits_OneToZeroPrunes is the fail-before-fix guard for the
// one-to-zero prune leak: projecting {unit} then {} (a bucket reduced to ZERO
// units, with NO local sibling to keep the directory in the wanted scan) must
// still prune the removed unit's managed link. The deterministic per-platform
// dir-mirror prune roots make the bucket dir get scanned regardless of the
// desired set.
func TestProjectResolvedUnits_OneToZeroPrunes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")

	u := materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\n"})
	platforms := []Platform{NewClaude()}

	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{u}, platforms, false, true, "proj"); err != nil {
		t.Fatalf("project {unit}: %v", err)
	}
	link := filepath.Join(repo, ".claude", "skills", "release-docs-refresh")
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("expected the link present after {unit}: %v", err)
	}

	// Reduce to ZERO units — no local sibling exists in this project.
	if _, err := ProjectResolvedUnits("proj", repo, nil, platforms, false, true, "proj"); err != nil {
		t.Fatalf("project {}: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("one-to-zero prune leak: the removed unit's link survived, err=%v", err)
	}
	// Store entry survives (store GC deferred).
	if _, err := os.Stat(u.CASPath); err != nil {
		t.Fatalf("store entry must survive projection prune: %v", err)
	}
}
