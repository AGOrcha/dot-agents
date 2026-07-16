package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// --- fixtures ----------------------------------------------------------------

// newPackagesSourceTree lays out a local git-source-shaped tree at
// <root>/<label>/<name>/ (the fetcher's "family/name" subtree convention,
// mirroring fetcher_test.go's own "skill/review" fixtures) with a SKILL.md
// marker, and returns the source root.
func newPackagesSourceTree(t *testing.T, root, label, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, label, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func pkgRefs(refs ...string) []config.PackageRef {
	out := make([]config.PackageRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, config.PackageRef{Ref: r})
	}
	return out
}

func snapWithPackages(sources []config.Source, refs ...string) *config.Snapshot {
	return &config.Snapshot{Effective: config.AgentsRC{Sources: sources, Packages: pkgRefs(refs...)}}
}

// tamperCASFile restores the write bit on a read-only published store file
// (t3 review #2c hardening) and overwrites its content — simulating a
// privileged post-install tamper the integrity checks must catch.
func tamperCASFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("restore write bit: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("tamper %s: %v", path, err)
	}
}

// --- HydratePackagesUnits: D6 no-op / errors ---------------------------------

func TestHydratePackagesUnits_NoPackagesIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	res := &config.EnsureResult{Snapshot: &config.Snapshot{}, ReResolved: true}
	units, participated, err := HydratePackagesUnits(proj, "proj", res)
	if err != nil || units != nil || participated {
		t.Fatalf("expected a no-op (no packages, no artifact units), got units=%v participated=%v err=%v", units, participated, err)
	}
}

func TestHydratePackagesUnits_NilEnsureResultIsNoop(t *testing.T) {
	units, participated, err := HydratePackagesUnits("/nonexistent", "proj", nil)
	if err != nil || units != nil || participated {
		t.Fatalf("expected a no-op for a nil EnsureResult, got units=%v participated=%v err=%v", units, participated, err)
	}
}

func TestHydratePackagesUnits_UnsupportedFamilyErrors(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "widget", "thing", "# x\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "dev", Path: srcRoot}}
	res := &config.EnsureResult{Snapshot: snapWithPackages(sources, "dev:widget/thing@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", res); err == nil {
		t.Fatal("expected an unsupported artifact-path family to be rejected")
	}
}

func TestHydratePackagesUnits_UnknownSourceErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	res := &config.EnsureResult{Snapshot: snapWithPackages(nil, "dev:skill/x@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", res); err == nil {
		t.Fatal("expected an undeclared source to be rejected")
	}
}

// TestHydratePackagesUnits_OCISourceFailsGracefully is review #5: an oci-source
// packages ref must fail with a clear "not yet wired (t6)" message, not the
// confusing "not a directory-shaped bundle" a nil OCI Bundle would produce.
// TestHydratePackagesUnits_OCISourceReachesWirePull confirms t6 wired OCI
// consume into pass-2: an oci-source packages ref is no longer short-circuited
// by the old "not yet wired" stopgap — it flows through the real consume path
// (fetch -> digest/type gate -> materialize) and fails only at the still-stubbed
// live wire pull (ociPull), not at a lifecycle guard. When the wire protocol
// lands this becomes a full round-trip; until then the failure must come from
// the registry pull, never the removed stopgap or the confusing bundle-shape
// message a nil Bundle used to produce.
func TestHydratePackagesUnits_OCISourceReachesWirePull(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "oci", ID: "reg", URL: "oci://reg.example"}}
	res := &config.EnsureResult{Snapshot: snapWithPackages(sources, "reg:skill/demo@1"), ReResolved: true}
	_, _, err := HydratePackagesUnits(proj, "proj", res)
	if err == nil {
		t.Fatal("expected an oci-source packages ref to fail at the unwired wire pull")
	}
	if !strings.Contains(err.Error(), "oci wire protocol not yet wired") {
		t.Fatalf("expected the real ociPull transport-stub error, got: %v", err)
	}
	if strings.Contains(err.Error(), "not a directory-shaped bundle") {
		t.Fatalf("OCI now populates Bundle — must not hit the bundle-shape rejection: %v", err)
	}
	if strings.Contains(err.Error(), "not yet wired (tracked in t6") {
		t.Fatalf("the pass-2 OCI stopgap should be gone, got: %v", err)
	}
}

// --- resolve half (H9 write, H10 lock, H13 units, content anchor) -----------

// TestResolvePackagesUnits_MaterializesLocksAndReturnsUnit is the R1/R3
// acceptance shape at the driver level: a declared packages[] ref
// materializes into the CAS store, the resolved-unit set is caller-ready for
// projection (H13), a kind:artifact lock unit is recorded (H10), a
// content-integrity anchor is committed alongside it, and the published CAS
// file is read-only (review #2c).
func TestResolvePackagesUnits_MaterializesLocksAndReturnsUnit(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	res := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: true}

	units, participated, err := HydratePackagesUnits(proj, "proj", res)
	if err != nil {
		t.Fatalf("HydratePackagesUnits: %v", err)
	}
	if !participated {
		t.Fatal("expected participated=true when a package is declared")
	}
	if len(units) != 1 {
		t.Fatalf("expected exactly 1 resolved unit, got %d", len(units))
	}
	u := units[0]
	if u.Family != "skills" || u.Name != "demo" || u.SourceID != "da-agc" {
		t.Fatalf("unexpected resolved unit: %+v", u)
	}
	if _, err := os.Stat(filepath.Join(u.CASPath, "SKILL.md")); err != nil {
		t.Fatalf("expected materialized CAS content: %v", err)
	}

	// #2c: published store file is read-only.
	fi, err := os.Stat(filepath.Join(u.CASPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o222 != 0 {
		t.Fatalf("expected the published CAS file to be read-only, mode=%v", fi.Mode())
	}

	lock, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	locked, ok := lock.Units["da-agc:skill/demo@1"]
	if !ok || locked.Kind != config.UnitKindArtifact {
		t.Fatalf("expected a kind:artifact lock unit, got %v", lock.Units)
	}
	if locked.Digest != u.Digest {
		t.Fatalf("locked digest %q != resolved unit digest %q", locked.Digest, u.Digest)
	}
	if anchor := readArtifactContentDigests(proj)["da-agc:skill/demo@1"]; anchor == "" {
		t.Fatal("expected a committed content-integrity anchor for the ref")
	}
}

// TestResolvePackagesUnits_PreservesExistingLayerUnits proves pass-2's lock
// write does not clobber a kind:layer unit pass-1 already wrote (H10).
func TestResolvePackagesUnits_PreservesExistingLayerUnits(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	if err := config.WriteUnitsLock(proj, config.UnitsLock{
		Units:        map[string]config.LockedUnit{"da-agc:layer.json@main": {Kind: config.UnitKindLayer, Digest: "abc123"}},
		InputsDigest: "sha256:seed",
	}); err != nil {
		t.Fatalf("seed layer unit: %v", err)
	}

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	res := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", res); err != nil {
		t.Fatalf("HydratePackagesUnits: %v", err)
	}

	lock, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	layer, ok := lock.Units["da-agc:layer.json@main"]
	if !ok || layer.Kind != config.UnitKindLayer || layer.Digest != "abc123" {
		t.Fatalf("expected the pre-existing layer unit to survive, got %+v", lock.Units)
	}
	if _, ok := lock.Units["da-agc:skill/demo@1"]; !ok {
		t.Fatalf("expected the new artifact unit alongside the preserved layer unit")
	}
	if lock.InputsDigest != "sha256:seed" {
		t.Fatalf("expected inputs_digest preserved, got %q", lock.InputsDigest)
	}
}

// TestResolvePackagesUnits_PrunesRemovedRef is R5 at the lock level: an
// artifact ref no longer declared must not linger in the lock (unit OR content
// anchor).
func TestResolvePackagesUnits_PrunesRemovedRef(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	src := filepath.Join(tmp, "src")
	newPackagesSourceTree(t, src, "skill", "keep", "# keep\n")
	newPackagesSourceTree(t, src, "skill", "drop", "# drop\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: src}}
	first := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/keep@1", "da-agc:skill/drop@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", first); err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	second := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/keep@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", second); err != nil {
		t.Fatalf("second resolve: %v", err)
	}

	lock, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	if _, ok := lock.Units["da-agc:skill/drop@1"]; ok {
		t.Fatalf("expected the removed ref's lock unit to be pruned, got %v", lock.Units)
	}
	if _, ok := lock.Units["da-agc:skill/keep@1"]; !ok {
		t.Fatalf("expected the still-declared ref's lock unit to remain")
	}
	if _, ok := readArtifactContentDigests(proj)["da-agc:skill/drop@1"]; ok {
		t.Fatalf("expected the removed ref's content anchor to be pruned too")
	}
}

// TestHydratePackagesUnits_LastRemovalStillParticipates is review #4: when the
// LAST package is removed, the manifest declares zero packages but the lock
// still carries artifact units — pass-2 must still participate (so the caller
// runs the CAS one-to-zero prune) and must clean the orphaned lock units.
func TestHydratePackagesUnits_LastRemovalStillParticipates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	first := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", first); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}

	// Manifest now declares NO packages, but the lock still has the artifact
	// unit. participated must stay true so the caller prunes the CAS link.
	removed := &config.EnsureResult{Snapshot: snapWithPackages(sources), ReResolved: true}
	units, participated, err := HydratePackagesUnits(proj, "proj", removed)
	if err != nil {
		t.Fatalf("removal resolve: %v", err)
	}
	if !participated {
		t.Fatal("expected participated=true on the last-package removal so the one-to-zero prune runs")
	}
	if len(units) != 0 {
		t.Fatalf("expected an empty resolved-unit set after removing all packages, got %d", len(units))
	}
	lock, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	if anyArtifactUnit(lock.Units) {
		t.Fatalf("expected the orphaned artifact lock units to be cleaned, got %v", lock.Units)
	}
}

// --- projection-boundary re-verify (review #2b) ------------------------------

// TestVerifyProjectionInputs_CatchesPostMaterializeTamper drives the exact
// re-check the driver runs immediately before projection: a store tampered
// AFTER materialize but BEFORE projection fails the pass closed, so a tampered
// artifact is never linked.
func TestVerifyProjectionInputs_CatchesPostMaterializeTamper(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	bundle := config.Bundle{Entries: []config.BundleEntry{{Path: "SKILL.md", Data: []byte("# demo\n"), Mode: 0o644}}}
	casPath, digest, err := platform.MaterializeArtifact(agentsHome, "skills", "da-agc", "demo", bundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	content := config.BundleContentDigest(bundle)
	unit := platform.ResolvedUnit{Family: "skills", Name: "demo", SourceID: "da-agc", Digest: digest, CASPath: casPath}

	// Clean: passes.
	if err := verifyProjectionInputs(agentsHome, []platform.ResolvedUnit{unit}, []string{content}); err != nil {
		t.Fatalf("expected a clean re-verify to pass, got %v", err)
	}

	// Tamper the CAS bytes, then re-verify → must fail closed.
	tamperCASFile(t, filepath.Join(casPath, "SKILL.md"), "TAMPERED")
	if err := verifyProjectionInputs(agentsHome, []platform.ResolvedUnit{unit}, []string{content}); err == nil {
		t.Fatal("expected verifyProjectionInputs to fail closed on a post-materialize CAS tamper")
	}
}

// --- atomicity (review #3) ---------------------------------------------------

// TestResolvePackagesUnits_MidPassFailureLeavesPriorLockIntact proves pass-2's
// all-or-nothing lock commit: a resolve where a later ref fails must not have
// written the lock at all, so a prior artifact lock survives unchanged.
func TestResolvePackagesUnits_MidPassFailureLeavesPriorLockIntact(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	src := filepath.Join(tmp, "src")
	newPackagesSourceTree(t, src, "skill", "good", "# good\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: src}}
	seed := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/good@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", seed); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}
	before, err := os.ReadFile(config.AgentsLockPath(proj))
	if err != nil {
		t.Fatalf("read lock before: %v", err)
	}

	// A resolve where the SECOND ref fails (missing source tree). The first
	// fetch succeeds, the second errors → no combined lock write at all.
	failing := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/good@1", "da-agc:skill/missing@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", failing); err == nil {
		t.Fatal("expected the resolve to fail on the missing second ref")
	}

	after, err := os.ReadFile(config.AgentsLockPath(proj))
	if err != nil {
		t.Fatalf("read lock after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected the prior lock to be byte-unchanged after a mid-pass failure\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestEnsureResolved_Pass1PreservesArtifactUnits proves the cross-pass fix
// (review #3): pass-1 (config.EnsureResolved → LayeredResolver.Resolve, which
// rewrites the units section with layers/profiles) must NOT delete a
// kind:artifact unit a prior packages pass recorded — otherwise a mid-pass-2
// failure would find the artifact lock already gone.
func TestEnsureResolved_Pass1PreservesArtifactUnits(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	os.MkdirAll(filepath.Join(home, ".agents"), 0o755)
	t.Setenv("HOME", home)
	t.Setenv("AGENTS_HOME", filepath.Join(home, ".agents"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))

	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)
	rc := &config.AgentsRC{Version: 1, Project: "proj"}
	if err := rc.Save(proj); err != nil {
		t.Fatal(err)
	}
	// Seed a lock carrying a kind:artifact unit as a prior packages pass would.
	if err := config.WriteUnitsLock(proj, config.UnitsLock{
		Units:        map[string]config.LockedUnit{"da-agc:skill/demo@1": {Kind: config.UnitKindArtifact, Digest: "sha256:deadbeef"}},
		InputsDigest: "sha256:stale",
	}); err != nil {
		t.Fatalf("seed artifact unit: %v", err)
	}

	// A resolve rewrites the units section (pass-1). The artifact unit must
	// survive.
	if _, err := config.EnsureResolved(proj, config.EnsureOpts{}); err != nil {
		t.Fatalf("EnsureResolved: %v", err)
	}
	lock, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	if u, ok := lock.Units["da-agc:skill/demo@1"]; !ok || u.Kind != config.UnitKindArtifact {
		t.Fatalf("expected pass-1 to preserve the kind:artifact unit, got %v", lock.Units)
	}
}

// --- hydrate half (H9 no-write) ---------------------------------------------

// TestHydratePackagesFromLock_NoWriteOnFrozenPath is the H9 done-criterion:
// when pass-1 did NOT rewrite the lock (ReResolved=false), pass-2 materializes
// from the EXISTING lock entry without writing the lock at all.
func TestHydratePackagesFromLock_NoWriteOnFrozenPath(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	resolveRes := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", resolveRes); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}

	lockPath := config.AgentsLockPath(proj)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock before hydrate: %v", err)
	}

	// Simulate a fresh CAS store on this machine (clean checkout) by wiping
	// the artifact cache while the lock stays committed.
	if err := os.RemoveAll(filepath.Join(agentsHome, "cache", "artifacts")); err != nil {
		t.Fatalf("clear CAS store: %v", err)
	}

	hydrateRes := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: false}
	units, participated, err := HydratePackagesUnits(proj, "proj", hydrateRes)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if !participated || len(units) != 1 {
		t.Fatalf("expected the hydrate half to materialize 1 unit and participate, got participated=%v units=%d", participated, len(units))
	}
	if _, err := os.Stat(filepath.Join(units[0].CASPath, "SKILL.md")); err != nil {
		t.Fatalf("expected the hydrate half to repopulate the CAS store: %v", err)
	}

	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock after hydrate: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected the lock to be byte-unchanged after a no-write hydrate\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestHydratePackagesFromLock_MissingEntryErrors proves the hydrate half fails
// closed rather than silently resolving live when nothing is locked.
func TestHydratePackagesFromLock_MissingEntryErrors(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	// Seed a lock that has SOME artifact unit (so pass-2 participates) but NOT
	// the one being hydrated, so the hydrate path is reached and must error.
	if err := config.WriteUnitsLock(proj, config.UnitsLock{
		Units:        map[string]config.LockedUnit{"da-agc:skill/other@1": {Kind: config.UnitKindArtifact, Digest: "sha256:x"}},
		InputsDigest: "sha256:seed",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, _ := os.ReadFile(config.AgentsLockPath(proj))

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	res := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: false}
	if _, _, err := HydratePackagesUnits(proj, "proj", res); err == nil {
		t.Fatal("expected an error hydrating a ref with no locked artifact entry")
	}
	after, _ := os.ReadFile(config.AgentsLockPath(proj))
	if string(before) != string(after) {
		t.Fatal("expected no lock write on the failed hydrate path")
	}
}

// --- End-to-end via RunInstall: R1/R3/R4, H13 projection ---------------------

// TestRunInstall_PackagesRefMaterializesLocksAndProjects is the driver-level
// DC1/R1 shape through `da install`: a packages ref materializes, locks a
// kind:artifact unit, and projects into the enabled platform's skills dir so
// the skill is invocable (H13). A second run is a no-op (R4).
func TestRunInstall_PackagesRefMaterializesLocksAndProjects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)

	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo skill\n")

	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	rc := &config.AgentsRC{
		Version:  1,
		Project:  "proj",
		Sources:  []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}},
		Packages: []config.PackageRef{{Ref: "da-agc:skill/demo@1"}},
	}
	if err := rc.Save(projDir); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstall: %v", err)
	}

	lock, err := config.ReadUnits(projDir)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	locked, ok := lock.Units["da-agc:skill/demo@1"]
	if !ok || locked.Kind != config.UnitKindArtifact {
		t.Fatalf("expected a kind:artifact lock unit, got %v", lock.Units)
	}

	projected := filepath.Join(projDir, ".claude", "skills", "demo", "SKILL.md")
	data, err := os.ReadFile(projected)
	if err != nil {
		t.Fatalf("expected the packages skill projected and readable at %s: %v", projected, err)
	}
	if string(data) != "# demo skill\n" {
		t.Fatalf("projected skill content = %q, want the source body", data)
	}

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("second RunInstall (re-run) failed: %v", err)
	}
	lock2, err := config.ReadUnits(projDir)
	if err != nil {
		t.Fatalf("ReadUnits after re-run: %v", err)
	}
	if lock2.Units["da-agc:skill/demo@1"].Digest != locked.Digest {
		t.Fatalf("expected the re-run digest unchanged: before=%s after=%s", locked.Digest, lock2.Units["da-agc:skill/demo@1"].Digest)
	}
	data2, err := os.ReadFile(projected)
	if err != nil || string(data2) != "# demo skill\n" {
		t.Fatalf("expected the projected skill to survive a no-op re-run, data=%q err=%v", data2, err)
	}
}

// TestCommitArtifactLock_InterleavedWithPass1PreservesBothKeys is the review #3
// cross-pass lost-update proof: the REAL pass-2 writer (commitArtifactLock) and
// a pass-1-shaped layer writer (the same agentslock.Update read-preserve-write
// resolver.writeUnitsLock performs) run interleaved against one lock; afterward
// BOTH a layer unit and an artifact unit must survive. Under the previous
// read-outside-the-flush-lock shape, a pass-2 that read units before a
// concurrent pass-1 layer write clobbered the layer key with its stale snapshot.
func TestCommitArtifactLock_InterleavedWithPass1PreservesBothKeys(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)
	lockPath := config.AgentsLockPath(proj)

	const rounds = 24
	artUnits := map[string]config.LockedUnit{"da-agc:skill/demo@1": {Kind: config.UnitKindArtifact, Digest: "sha256:art"}}
	artAnchors := map[string]string{"da-agc:skill/demo@1": "sha256:content"}
	layerUnit := config.LockedUnit{Kind: config.UnitKindLayer, Digest: "sha256:layer"}

	// pass-1 mimic: exactly resolver.writeUnitsLock's Update shape — read units
	// under the lock, preserve existing artifact units, (re)write the layer unit.
	pass1 := func() error {
		return agentslock.Update(lockPath, func(lf *agentslock.Lockfile) error {
			existing := map[string]config.LockedUnit{}
			if _, err := lf.Section(config.LockSectionUnits, &existing); err != nil {
				return err
			}
			merged := map[string]config.LockedUnit{"da-agc:layer.json@main": layerUnit}
			for ref, u := range existing {
				if u.Kind == config.UnitKindArtifact {
					merged[ref] = u
				}
			}
			return lf.SetSection(config.LockSectionUnits, merged)
		})
	}

	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)
	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); errs <- commitArtifactLock(proj, artUnits, artAnchors) }()
		go func() { defer wg.Done(); errs <- pass1() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("interleaved write: %v", err)
		}
	}

	lock, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatal(err)
	}
	if u, ok := lock.Units["da-agc:layer.json@main"]; !ok || u.Kind != config.UnitKindLayer {
		t.Fatalf("pass-2 clobbered the concurrent pass-1 layer unit: %v", lock.Units)
	}
	if u, ok := lock.Units["da-agc:skill/demo@1"]; !ok || u.Kind != config.UnitKindArtifact {
		t.Fatalf("pass-1 clobbered the concurrent pass-2 artifact unit: %v", lock.Units)
	}
}
