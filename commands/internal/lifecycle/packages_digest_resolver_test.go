package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// packagesDigestResolverFixture materializes one packages artifact through the
// real resolve half (so the lock, the CAS store, and the content-integrity
// anchor are all genuine) using an ORDINARY `@1` ref — review #1 requires the
// H7 resolver to work for ordinary refs, NOT only `@pinned:` ones, so the
// fixture deliberately does not re-key to a pinned form. Returns the project
// path, the ordinary ref, the resolved store-addressing digest, and the CAS
// marker file path for tampering.
func packagesDigestResolverFixture(t *testing.T) (proj, ref, digest, casFile string) {
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
	units, _, err := HydratePackagesUnits(proj, "proj", res)
	if err != nil {
		t.Fatalf("seed resolve: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 seeded unit, got %d", len(units))
	}
	digest = units[0].Digest
	casFile = filepath.Join(units[0].CASPath, "SKILL.md")
	return proj, ref, digest, casFile
}

// TestPackagesArtifactDigestResolver_VerifiesUntamperedOrdinaryRef is review #1:
// an ORDINARY `@1` ref (not `@pinned:`) verifies clean.
func TestPackagesArtifactDigestResolver_VerifiesUntamperedOrdinaryRef(t *testing.T) {
	proj, ref, digest, _ := packagesDigestResolverFixture(t)

	got, ok := PackagesArtifactDigestResolver(proj)(ref)
	if !ok {
		t.Fatal("expected the resolver to verify an untampered ordinary-ref artifact")
	}
	if got != digest {
		t.Fatalf("expected recompute to echo the locked digest %q on a clean verify, got %q", digest, got)
	}
}

// TestPackagesArtifactDigestResolver_DetectsOrdinaryRefStoreTamper is the
// review #1 BLOCKER fix: a tamper of an ORDINARY `@1` ref's CAS content is
// detected (would previously be skipped, letting config verify report clean).
func TestPackagesArtifactDigestResolver_DetectsOrdinaryRefStoreTamper(t *testing.T) {
	proj, ref, digest, casFile := packagesDigestResolverFixture(t)

	tamperCASFile(t, casFile, "TAMPERED")

	got, ok := PackagesArtifactDigestResolver(proj)(ref)
	if !ok {
		t.Fatal("expected the resolver to report a verifiable (tampered) result, not skip")
	}
	if got == digest {
		t.Fatal("expected a tampered store entry to recompute to something OTHER than the locked digest")
	}
}

func TestPackagesArtifactDigestResolver_NotHydratedYetSkips(t *testing.T) {
	proj, ref, _, _ := packagesDigestResolverFixture(t)

	if err := os.RemoveAll(filepath.Join(config.AgentsHome(), "cache", "artifacts")); err != nil {
		t.Fatalf("clear CAS store: %v", err)
	}

	if _, ok := PackagesArtifactDigestResolver(proj)(ref); ok {
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

// TestPackagesArtifactDigestResolver_NoContentAnchorSkips proves a
// kind:artifact unit with no committed content-integrity anchor (e.g. a lock
// from before pass-2 recorded anchors) skips rather than false-alarms.
func TestPackagesArtifactDigestResolver_NoContentAnchorSkips(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	proj := filepath.Join(tmp, "proj")
	os.MkdirAll(proj, 0o755)
	if err := config.WriteUnitsLock(proj, config.UnitsLock{
		Units:        map[string]config.LockedUnit{"da-agc:skill/demo@1": {Kind: config.UnitKindArtifact, Digest: "sha256:x"}},
		InputsDigest: "sha256:seed",
	}); err != nil {
		t.Fatalf("seed artifact unit without anchor: %v", err)
	}

	if _, ok := PackagesArtifactDigestResolver(proj)("da-agc:skill/demo@1"); ok {
		t.Fatal("expected a unit with no content anchor to skip (cannot verify offline)")
	}
}
