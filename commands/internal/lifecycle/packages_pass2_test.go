package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
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

// --- HydratePackagesUnits: D6 no-op / errors ---------------------------------

func TestHydratePackagesUnits_NoPackagesIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	res := &config.EnsureResult{Snapshot: &config.Snapshot{}, ReResolved: true}
	units, err := HydratePackagesUnits(proj, "proj", res)
	if err != nil || units != nil {
		t.Fatalf("expected a no-op for zero declared packages, got units=%v err=%v", units, err)
	}
}

func TestHydratePackagesUnits_NilEnsureResultIsNoop(t *testing.T) {
	units, err := HydratePackagesUnits("/nonexistent", "proj", nil)
	if err != nil || units != nil {
		t.Fatalf("expected a no-op for a nil EnsureResult, got units=%v err=%v", units, err)
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
	if _, err := HydratePackagesUnits(proj, "proj", res); err == nil {
		t.Fatal("expected an unsupported artifact-path family to be rejected")
	}
}

func TestHydratePackagesUnits_UnknownSourceErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	res := &config.EnsureResult{Snapshot: snapWithPackages(nil, "dev:skill/x@1"), ReResolved: true}
	if _, err := HydratePackagesUnits(proj, "proj", res); err == nil {
		t.Fatal("expected an undeclared source to be rejected")
	}
}

// --- HydratePackagesUnits: resolve half (H9 write, H10 lock, H13 units) -----

// TestResolvePackagesUnits_MaterializesLocksAndReturnsUnit is the R1/R3
// acceptance shape at the driver level: a declared packages[] ref
// materializes into the CAS store, the resolved-unit set is caller-ready for
// projection (H13), and a kind:artifact lock unit is recorded (H10) without
// an install_path field ever entering the picture.
func TestResolvePackagesUnits_MaterializesLocksAndReturnsUnit(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	res := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: true}

	units, err := HydratePackagesUnits(proj, "proj", res)
	if err != nil {
		t.Fatalf("HydratePackagesUnits: %v", err)
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

	lock, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	locked, ok := lock.Units["da-agc:skill/demo@1"]
	if !ok {
		t.Fatalf("expected a locked unit for the packages ref, got %v", lock.Units)
	}
	if locked.Kind != config.UnitKindArtifact {
		t.Fatalf("expected kind:artifact, got %q", locked.Kind)
	}
	if locked.Digest != u.Digest {
		t.Fatalf("locked digest %q != resolved unit digest %q", locked.Digest, u.Digest)
	}
}

// TestResolvePackagesUnits_PreservesExistingLayerUnits proves H10's merge:
// pass-2's lock write must not clobber a kind:layer unit pass-1 already
// wrote — WriteUnitsLock replaces the whole "units" section, so a naive
// write here would silently delete every layer unit on the next packages
// resolve.
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
	if _, err := HydratePackagesUnits(proj, "proj", res); err != nil {
		t.Fatalf("HydratePackagesUnits: %v", err)
	}

	lock, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	layer, ok := lock.Units["da-agc:layer.json@main"]
	if !ok || layer.Kind != config.UnitKindLayer || layer.Digest != "abc123" {
		t.Fatalf("expected the pre-existing layer unit to survive a packages resolve, got %+v", lock.Units)
	}
	if _, ok := lock.Units["da-agc:skill/demo@1"]; !ok {
		t.Fatalf("expected the new artifact unit alongside the preserved layer unit")
	}
}

// TestResolvePackagesUnits_PrunesRemovedRef is R5 at the lock level: an
// artifact ref no longer declared must not linger in the lock as an orphan
// (which would otherwise permanently desync staleness's declared-set
// comparison).
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
	if _, err := HydratePackagesUnits(proj, "proj", first); err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	second := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/keep@1"), ReResolved: true}
	if _, err := HydratePackagesUnits(proj, "proj", second); err != nil {
		t.Fatalf("second resolve: %v", err)
	}

	lock, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	if _, ok := lock.Units["da-agc:skill/drop@1"]; ok {
		t.Fatalf("expected the removed packages ref's lock unit to be pruned, got %v", lock.Units)
	}
	if _, ok := lock.Units["da-agc:skill/keep@1"]; !ok {
		t.Fatalf("expected the still-declared ref's lock unit to remain")
	}
}

// --- HydratePackagesUnits: hydrate half (H9 no-write) ------------------------

// TestHydratePackagesFromLock_NoWriteOnFrozenPath is the H9 done-criterion:
// when pass-1 did NOT rewrite the lock (ReResolved=false — the Frozen/
// Locked-fresh/plain-fresh signal), pass-2 must materialize from the
// EXISTING lock entry without writing to the lock at all.
func TestHydratePackagesFromLock_NoWriteOnFrozenPath(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	resolveRes := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: true}
	if _, err := HydratePackagesUnits(proj, "proj", resolveRes); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}

	lockPath := config.AgentsLockPath(proj)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock before hydrate: %v", err)
	}

	// Simulate a fresh CAS store on this machine (a clean checkout scenario)
	// by wiping the artifact cache while the lock stays committed.
	if err := os.RemoveAll(filepath.Join(agentsHome, "cache", "artifacts")); err != nil {
		t.Fatalf("clear CAS store: %v", err)
	}

	hydrateRes := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: false}
	units, err := HydratePackagesUnits(proj, "proj", hydrateRes)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("expected the hydrate half to materialize 1 unit, got %d", len(units))
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

// --- End-to-end via RunInstall: R1/R2/R3/R4, H13 projection -----------------

// TestRunInstall_PackagesRefMaterializesLocksAndProjects is the driver-level
// DC1/R1 shape: a `packages[]` ref against a local git-source-shaped tree
// materializes into the CAS store, gains a kind:artifact lock unit (R3), and
// projects into the enabled platform's skills dir so the skill is invocable
// (H13) — all through `da install`, exercising the SAME code path install
// wires (hydrateInstallPackages/runInstallSharedTargetsFor), not a bypass.
// A second install run is a no-op re-run (R4): it succeeds, keeps the same
// resolved digest, and leaves the projected content byte-identical.
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
		Version: 1,
		Project: "proj",
		Sources: []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}},
		Packages: []config.PackageRef{
			{Ref: "da-agc:skill/demo@1"},
		},
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
		t.Fatalf("expected a kind:artifact lock unit for the packages ref, got %v", lock.Units)
	}

	projected := filepath.Join(projDir, ".claude", "skills", "demo", "SKILL.md")
	data, err := os.ReadFile(projected)
	if err != nil {
		t.Fatalf("expected the packages skill to be projected and readable at %s: %v", projected, err)
	}
	if string(data) != "# demo skill\n" {
		t.Fatalf("projected skill content = %q, want the source body", data)
	}

	// R4: unchanged upstream, re-run is a no-op — same digest, same projected
	// content, no error.
	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("second RunInstall (re-run) failed: %v", err)
	}
	lock2, err := config.ReadUnits(projDir)
	if err != nil {
		t.Fatalf("ReadUnits after re-run: %v", err)
	}
	if lock2.Units["da-agc:skill/demo@1"].Digest != locked.Digest {
		t.Fatalf("expected the re-run digest to be unchanged: before=%s after=%s", locked.Digest, lock2.Units["da-agc:skill/demo@1"].Digest)
	}
	data2, err := os.ReadFile(projected)
	if err != nil || string(data2) != "# demo skill\n" {
		t.Fatalf("expected the projected skill to survive a no-op re-run unchanged, data=%q err=%v", data2, err)
	}
}

// TestHydratePackagesFromLock_MissingEntryErrors proves the hydrate half
// fails closed instead of silently falling back to a live resolve when
// there is nothing locked to hydrate from.
func TestHydratePackagesFromLock_MissingEntryErrors(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	res := &config.EnsureResult{Snapshot: snapWithPackages(sources, "da-agc:skill/demo@1"), ReResolved: false}
	if _, err := HydratePackagesUnits(proj, "proj", res); err == nil {
		t.Fatal("expected an error hydrating a ref with no locked artifact entry")
	}
	if _, err := os.Stat(config.AgentsLockPath(proj)); !os.IsNotExist(err) {
		t.Fatalf("expected no lock to have been written on the failed hydrate path, stat err=%v", err)
	}
}
