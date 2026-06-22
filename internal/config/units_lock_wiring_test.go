package config

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
)

// units_lock_wiring_test.go covers the §7A units-lock cutover
// (section-7a-units-lock-wiring): the units section + inputs_digest is the
// authoritative lock, written from one path in Resolve so every resolved
// project — including flat/local-only — gets a coherent lock; offline/verify
// read the units model (one-time-migrating a legacy lock on first read); and
// verify tracks local-scope drift. There is no permanent dual-read of the
// retired legacy "config" section.

// TestResolveWritesUnitsLockForFlatProject is the flat-project property: a
// local-only project (no extends) that resolves through LayeredResolver.Resolve
// gets a .agentsrc.lock carrying a non-empty inputs_digest and an (empty) units
// map — the lockfile §7A says EVERY resolved project should have, not just ones
// with remote extends.
func TestResolveWritesUnitsLockForFlatProject(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version": 2, "repo_id": "github.com/acme/flat", "skills": ["repo-skill"]}`)

	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	lock, err := ReadUnits(repo)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	if lock.InputsDigest == "" {
		t.Errorf("flat project lock must carry a non-empty inputs_digest, got empty")
	}
	// inputs_digest must equal a fresh ComputeInputsDigest over the same scopes.
	want, err := ComputeInputsDigest(repo, "")
	if err != nil {
		t.Fatalf("ComputeInputsDigest: %v", err)
	}
	if lock.InputsDigest != want {
		t.Errorf("recorded inputs_digest = %q, want %q", lock.InputsDigest, want)
	}
	if len(lock.Units) != 0 {
		t.Errorf("flat project has no extends, units must be empty, got %v", lock.Units)
	}
}

// TestResolveWritesUnitsOnlyAuthoritativeLock is the cutover's steady-state
// guarantee: a single resolve writes the §7A units section + inputs_digest as
// the AUTHORITATIVE lock and no longer writes the legacy "config" section. Every
// resolved extends ref appears in the units section as a UnitKindLayer carrying
// its SHA + cache key; the raw lockfile has no "config" key.
func TestResolveWritesUnitsOnlyAuthoritativeLock(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/app",
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": ["acme:org/base.json", "acme:team/frontend.json"]
	}`)

	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	units, err := ReadUnits(repo)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	for _, ref := range []string{"acme:org/base.json", "acme:team/frontend.json"} {
		u, ok := units.Units[ref]
		if !ok {
			t.Fatalf("units section missing %q", ref)
		}
		if u.Kind != UnitKindLayer {
			t.Errorf("unit %q kind = %q, want %q", ref, u.Kind, UnitKindLayer)
		}
		if u.Digest == "" {
			t.Errorf("unit %q must carry a resolved SHA digest", ref)
		}
		if u.CacheKey == "" {
			t.Errorf("unit %q must carry the §7A.4 cache key (gate must survive cutover)", ref)
		}
	}
	if units.InputsDigest == "" {
		t.Errorf("authoritative lock must carry inputs_digest, got empty")
	}

	// The legacy "config" section is no longer written.
	if rawHasSection(t, repo, LockSectionConfig) {
		t.Errorf("cutover must not write the legacy %q section", LockSectionConfig)
	}
	if !rawHasSection(t, repo, LockSectionUnits) {
		t.Errorf("authoritative lock must carry the %q section", LockSectionUnits)
	}
}

// TestResolveProducesFreshLockNoSplitBrain proves the cutover removes the
// stale-repair split-brain: immediately after a resolve, a Staleness check
// reports Fresh. Before the wiring, Resolve wrote only the config section, so
// inputs_digest stayed empty and Staleness fired ReasonInputsDigest on a
// freshly-resolved project. This asserts the units section + inputs_digest are
// written together by the single authoritative resolve path.
func TestResolveProducesFreshLockNoSplitBrain(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`"}],
		"extends": ["acme:org/base.json"]
	}`)

	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	stale, err := Staleness(repo, "", nil)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !stale.Fresh {
		t.Errorf("freshly-resolved project must be Fresh, got reasons %v", stale.Reasons)
	}

	// And the recorded inputs_digest matches ComputeInputsDigest post-resolve.
	want, err := ComputeInputsDigest(repo, "")
	if err != nil {
		t.Fatalf("ComputeInputsDigest: %v", err)
	}
	lock, err := ReadUnits(repo)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	if lock.InputsDigest != want {
		t.Errorf("post-resolve inputs_digest %q != ComputeInputsDigest %q", lock.InputsDigest, want)
	}
}

// TestEnsureResolvedStaleRepairRefreshesUnitsLock drives the stale-repair path
// END-TO-END through the production resolver: a project whose lock records a
// stale inputs_digest is re-resolved by EnsureResolved's default/stale branch,
// and the resulting lock is coherent (Fresh) — units AND inputs_digest refreshed
// together. This is the exact split-brain scenario the task calls out: staleness
// is detected via the units section, and the repair must rewrite that same
// section.
func TestEnsureResolvedStaleRepairRefreshesUnitsLock(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version": 2, "repo_id": "github.com/acme/flat"}`)

	// Seed a lock with a stale inputs_digest so EnsureResolved sees drift.
	if err := WriteUnitsLock(repo, UnitsLock{InputsDigest: "sha256:stale"}); err != nil {
		t.Fatalf("seed stale lock: %v", err)
	}

	// Real resolver (no fake): the default/stale branch must call Resolve, which
	// rewrites the §7A lock model coherently.
	res, err := EnsureResolved(repo, EnsureOpts{})
	if err != nil {
		t.Fatalf("EnsureResolved: %v", err)
	}
	if !res.ReResolved {
		t.Fatalf("stale lock must trigger a re-resolve, got %+v", res)
	}

	stale, err := Staleness(repo, "", nil)
	if err != nil {
		t.Fatalf("Staleness post-repair: %v", err)
	}
	if !stale.Fresh {
		t.Errorf("post-repair lock must be Fresh (no split-brain), got reasons %v", stale.Reasons)
	}
}

// TestResolveLockedFromUnitsOnlyLock is the offline read against the cutover's
// steady-state lock: a units-only lockfile (exactly what Resolve now writes)
// resolves offline via ResolveLocked without the "no resolved SHA" transport
// error, reconstructing the same effective config as the online resolve.
func TestResolveLockedFromUnitsOnlyLock(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": ["acme:org/base.json", "acme:team/frontend.json"],
		"skills": ["repo-skill"]
	}`)

	// Online resolve populates the cache AND writes the authoritative units lock.
	online, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("online Resolve: %v", err)
	}
	// Confirm the steady-state lock is units-only (no config section).
	if rawHasSection(t, repo, LockSectionConfig) {
		t.Fatalf("precondition: resolve should write a units-only lock")
	}

	locked, err := NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked from units-only lock: %v", err)
	}
	if !reflect.DeepEqual(locked.Effective, online.Effective) {
		t.Errorf("units-only locked effective != online effective\nlocked=%+v\nonline=%+v", locked.Effective, online.Effective)
	}
	wantIDs := []string{LayerProductDefaults, "acme:org/base.json", "acme:team/frontend.json", LayerRepoLocal}
	if got := layerIDs(locked.Layers); !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("layer ids = %v, want %v", got, wantIDs)
	}
}

// TestResolveLockedMigratesLegacyConfigOnlyLock is the cutover's backward-compat
// guard: a pre-§7A lockfile carrying ONLY the legacy "config" section is
// transparently upgraded to the units model on first read (ReadUnits ->
// migrateLegacyUnits) and resolves offline unchanged — no permanent dual-read,
// just a one-time on-read migration.
func TestResolveLockedMigratesLegacyConfigOnlyLock(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": ["acme:org/base.json"]
	}`)

	// Produce a resolve (populates the cache + writes units), then synthesize a
	// LEGACY config-only lock from the resolved layers — the pre-§7A shape.
	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}
	legacy := layersFromUnits(mustReadUnits(t, repo).Units)
	writeLegacyConfigOnlyLock(t, repo, legacy)

	// On first read the legacy lock is migrated to units and resolves offline.
	locked, err := NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked from legacy config-only lock: %v", err)
	}
	if got := layerIDs(locked.Layers); len(got) != 3 {
		t.Errorf("expected product/import/repo layers, got %v", got)
	}
	// The migration surfaces the legacy SHA through the units view.
	migrated, err := ReadUnits(repo)
	if err != nil {
		t.Fatalf("ReadUnits after migration: %v", err)
	}
	if u, ok := migrated.Units["acme:org/base.json"]; !ok || u.Kind != UnitKindLayer || u.Digest == "" {
		t.Errorf("legacy lock not migrated into units view: %+v", migrated.Units)
	}
}

// TestVerifyLayerLocksFromUnitsLock is the verify side of the cutover:
// VerifyLayerLocks reads the authoritative units section and confirms a pinned
// layer instead of reporting it unpinned.
func TestVerifyLayerLocksFromUnitsLock(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": ["acme:org/base.json"]
	}`)

	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}

	statuses, err := VerifyLayerLocks(repo)
	if err != nil {
		t.Fatalf("VerifyLayerLocks: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d: %+v", len(statuses), statuses)
	}
	st := statuses[0]
	if !st.Locked {
		t.Errorf("layer must read as Locked from the units section, got %+v", st)
	}
	if !st.OK() {
		t.Errorf("units-locked layer must verify clean, got problem %q", st.Problem)
	}
}

// TestVerifyLayerLocksMigratesLegacyLock proves verify also one-time-migrates a
// legacy config-only lock on read, so an un-migrated project verifies cleanly
// instead of reporting every layer unpinned.
func TestVerifyLayerLocksMigratesLegacyLock(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": ["acme:org/base.json"]
	}`)

	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}
	legacy := layersFromUnits(mustReadUnits(t, repo).Units)
	writeLegacyConfigOnlyLock(t, repo, legacy)

	statuses, err := VerifyLayerLocks(repo)
	if err != nil {
		t.Fatalf("VerifyLayerLocks: %v", err)
	}
	if len(statuses) != 1 || !statuses[0].Locked || !statuses[0].OK() {
		t.Errorf("legacy lock should verify clean via on-read migration, got %+v", statuses)
	}
}

// TestLayersFromUnitsSkipsArtifacts asserts the units→layers projection only
// surfaces UnitKindLayer entries — an artifact unit is not an extends layer and
// must not appear in the offline resolved-layer view — and that it round-trips
// the cache key so the §7A.4 gate keeps working.
func TestLayersFromUnitsSkipsArtifacts(t *testing.T) {
	units := map[string]LockedUnit{
		"acme:org/base@a1": {Kind: UnitKindLayer, Digest: "sha256:layer", CacheKey: "ck:git:sha256:layer"},
		"oci:tool/fmt@1.2": {Kind: UnitKindArtifact, Digest: "sha256:art"},
	}
	got := layersFromUnits(units)
	if len(got) != 1 {
		t.Fatalf("expected only the layer unit, got %d: %+v", len(got), got)
	}
	l, ok := got["acme:org/base@a1"]
	if !ok || l.ResolvedSHA != "sha256:layer" {
		t.Fatalf("layer unit not projected correctly: %+v", got)
	}
	if l.CacheKey != "ck:git:sha256:layer" {
		t.Errorf("cache key must round-trip through the units view, got %q", l.CacheKey)
	}
	if _, ok := got["oci:tool/fmt@1.2"]; ok {
		t.Errorf("artifact unit must not appear in the layer view")
	}
}

// mustReadUnits is a tiny ReadUnits wrapper that fails the test on error.
func mustReadUnits(t *testing.T, repo string) UnitsLock {
	t.Helper()
	u, err := ReadUnits(repo)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	return u
}

// writeLegacyConfigOnlyLock writes a lockfile containing ONLY the legacy
// "config" section (no "units" key, no inputs_digest) — the exact pre-§7A shape
// a project resolved by an old binary would carry, so the on-read migration can
// be exercised. It writes the JSON document directly so the "units" key is truly
// absent (ReadUnits treats even an empty present units section as authoritative,
// which would skip the migration path under test).
func writeLegacyConfigOnlyLock(t *testing.T, repo string, layers map[string]LockedLayer) {
	t.Helper()
	doc := legacyOnlyDoc{LockVersion: agentslock.LockVersion, Config: layers}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy lock: %v", err)
	}
	if err := os.WriteFile(AgentsLockPath(repo), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write legacy lock: %v", err)
	}
}

type legacyOnlyDoc struct {
	LockVersion int                    `json:"lock_version"`
	Config      map[string]LockedLayer `json:"config"`
}

func rawHasSection(t *testing.T, repo, section string) bool {
	t.Helper()
	lf, err := agentslock.Open(AgentsLockPath(repo))
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	present, err := lf.Section(section, &map[string]any{})
	if err != nil {
		t.Fatalf("read section %q: %v", section, err)
	}
	return present
}
