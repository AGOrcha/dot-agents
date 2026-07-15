package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// packagesDigestResolverFixture materializes one packages artifact through
// the real resolve half (so the lock, the CAS store, and the pinned
// version-spec form the resolver reads are all genuine, not hand-assembled)
// and returns the project path plus the resulting ref/digest for the caller
// to tamper with.
func packagesDigestResolverFixture(t *testing.T) (proj, ref, digest string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	proj = filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)

	sources := []config.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	ref = "da-agc:skill/demo@1"
	rc := &config.AgentsRC{Version: 1, Project: "proj", Sources: sources, Packages: pkgRefs(ref)}
	if err := rc.Save(proj); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	res := &config.EnsureResult{Snapshot: snapWithPackages(sources, ref), ReResolved: true}
	units, err := HydratePackagesUnits(proj, "proj", res)
	if err != nil {
		t.Fatalf("seed resolve: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 seeded unit, got %d", len(units))
	}
	digest = units[0].Digest
	// Re-key the lock entry to the PINNED form the packages ref actually
	// resolves to on a subsequent fetch, matching what a real resolve of a
	// "pinned:sha256:..." manifest ref would have keyed under. The fixture's
	// manifest ref carries a plain "@1" version-spec (matching declared-set
	// identity elsewhere in this file), so the resolver test below drives
	// PackagesArtifactDigestResolver directly against a synthetic pinned ref
	// instead of relying on re-resolution here.
	return proj, ref, digest
}

func TestPackagesArtifactDigestResolver_VerifiesUntamperedContent(t *testing.T) {
	proj, _, digest := packagesDigestResolverFixture(t)
	pinnedRef := "da-agc:skill/demo@pinned:" + digest
	seedPinnedArtifactLockEntry(t, proj, pinnedRef, digest)

	got, ok := PackagesArtifactDigestResolver(proj)(pinnedRef)
	if !ok {
		t.Fatal("expected the resolver to verify an untampered, locally-cacheable artifact")
	}
	if got != digest {
		t.Fatalf("expected recompute to echo the locked digest %q on a clean verify, got %q", digest, got)
	}
}

func TestPackagesArtifactDigestResolver_DetectsStoreTamper(t *testing.T) {
	proj, _, digest := packagesDigestResolverFixture(t)
	pinnedRef := "da-agc:skill/demo@pinned:" + digest
	seedPinnedArtifactLockEntry(t, proj, pinnedRef, digest)

	casPath := config.ArtifactStorePath(config.AgentsHome(), "skills", digest)
	if err := os.WriteFile(filepath.Join(casPath, "SKILL.md"), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatalf("tamper CAS content: %v", err)
	}

	got, ok := PackagesArtifactDigestResolver(proj)(pinnedRef)
	if !ok {
		t.Fatal("expected the resolver to report a verifiable (tampered) result, not skip")
	}
	if got == digest {
		t.Fatal("expected a tampered store entry to recompute to something OTHER than the locked digest")
	}
}

func TestPackagesArtifactDigestResolver_NotHydratedYetSkips(t *testing.T) {
	proj, _, digest := packagesDigestResolverFixture(t)
	pinnedRef := "da-agc:skill/demo@pinned:" + digest
	seedPinnedArtifactLockEntry(t, proj, pinnedRef, digest)

	if err := os.RemoveAll(filepath.Join(config.AgentsHome(), "cache", "artifacts")); err != nil {
		t.Fatalf("clear CAS store: %v", err)
	}

	if _, ok := PackagesArtifactDigestResolver(proj)(pinnedRef); ok {
		t.Fatal("expected a not-yet-hydrated unit to skip (ok=false), not report a driver event")
	}
}

func TestPackagesArtifactDigestResolver_NonArtifactUnitSkips(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)
	if err := config.WriteUnitsLock(proj, config.UnitsLock{
		Units:        map[string]config.LockedUnit{"src:layer.json@main": {Kind: config.UnitKindLayer, Digest: "abc"}},
		InputsDigest: "sha256:seed",
	}); err != nil {
		t.Fatalf("seed layer unit: %v", err)
	}

	if _, ok := PackagesArtifactDigestResolver(proj)("src:layer.json@main"); ok {
		t.Fatal("expected a kind:layer unit to be skipped by the artifact-only resolver")
	}
}

func TestPackagesArtifactDigestResolver_UnpinnedRefSkips(t *testing.T) {
	proj, ref, digest := packagesDigestResolverFixture(t)
	seedPinnedArtifactLockEntry(t, proj, ref, digest) // keyed under the plain "@1" ref, not "pinned:<digest>"

	// A tag/branch-shaped version-spec cannot be re-verified without a
	// network round-trip, so Staleness's no-network contract means this must
	// be a clean skip even though the ref IS a locked kind:artifact unit.
	if _, ok := PackagesArtifactDigestResolver(proj)(ref); ok {
		t.Fatal("expected an unpinned packages ref to skip rather than force a network check")
	}
}

// seedPinnedArtifactLockEntry writes a single kind:artifact lock unit keyed
// by ref, independent of whatever resolvePackagesUnits/mergeArtifactUnitsIntoLock
// last wrote — the resolver tests want precise control over the exact ref
// string under test (a pinned form, or the fixture's own plain-version ref).
func seedPinnedArtifactLockEntry(t *testing.T, proj, ref, digest string) {
	t.Helper()
	existing, err := config.ReadUnits(proj)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	units := existing.Units
	if units == nil {
		units = map[string]config.LockedUnit{}
	}
	units[ref] = config.LockedUnit{Kind: config.UnitKindArtifact, Digest: digest, FetchedAt: "2026-01-01T00:00:00Z"}
	if err := config.WriteUnitsLock(proj, config.UnitsLock{Units: units, InputsDigest: existing.InputsDigest}); err != nil {
		t.Fatalf("seed pinned artifact lock entry: %v", err)
	}
}
