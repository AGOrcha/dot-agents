package lifecycle

import (
	"fmt"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// packages_pass2.go wires t2's materialize/projection mechanism
// (internal/config/materialize.go, internal/platform/resource_plan.go) into
// the packages resolver pass (config-distribution-model §6 pass 2,
// package-artifact-install spec D6/H7/H9/H10/H13). It is the ONE driver every
// install/refresh caller goes through: it turns a project's resolved
// `packages[]` set into the caller-supplied []platform.ResolvedUnit (H13)
// that projection consumes directly — t2's mechanism is never re-implemented
// here, only invoked (config.SelectPackageFetcher/FetchArtifact from t1,
// platform.MaterializeArtifact/ProjectResolvedUnits from t2).
//
// H9 splits the driver into two disjoint halves, selected by whether pass-1
// config resolution (EnsureResolved) actually rewrote the lock this call
// (EnsureResult.ReResolved) — pass-2 mirrors that same write/no-write
// decision rather than deciding independently, so it inherits the Frozen/
// Locked/Offline/fresh no-write contract for free instead of re-deriving it:
//
//   - ReResolved (pass-1 wrote): resolvePackagesUnits fetches each declared
//     ref at its manifest version-spec (may re-resolve to new upstream
//     content), materializes it, and records/refreshes a kind:artifact lock
//     unit (H10).
//   - !ReResolved (pass-1 did not write — Frozen, Locked-fresh, Offline, or
//     plain-fresh): hydratePackagesFromLock fetches each ref pinned to the
//     digest ALREADY recorded in the lock and materializes it, without
//     touching the lock at all. This is what makes a clean checkout with a
//     committed lock but an empty local CAS store work: the lock doesn't
//     need rewriting, but the store still needs populating before
//     projection.

// packageFamilyBuckets maps a packages ref's artifact-path family label
// (singular — "source-id:skill/name@version", the external-agent-sources §5 /
// fetcher_test.go fixture convention) to the resource-plan bucket name
// platform.ResolvedUnit.Family and casDirMirrorSpec require (plural —
// "skills"/"agents"/"plugins", matching every other bucket name in
// internal/platform). Anything else is rejected closed: pass-2 only drives
// the dir-mirror shapes t2 built (the file-shaped agent builders are t2b).
var packageFamilyBuckets = map[string]string{
	"skill":  "skills",
	"agent":  "agents",
	"plugin": "plugins",
}

// splitPackageArtifactFamily splits a package ref's artifact-path
// ("family/name") into the resource-plan bucket and the resource name.
func splitPackageArtifactFamily(artifactPath string) (bucket, name string, err error) {
	i := strings.IndexByte(artifactPath, '/')
	if i <= 0 || i == len(artifactPath)-1 {
		return "", "", fmt.Errorf("artifact-path %q must be \"family/name\"", artifactPath)
	}
	label := artifactPath[:i]
	rest := artifactPath[i+1:]
	if strings.IndexByte(rest, '/') >= 0 {
		return "", "", fmt.Errorf("artifact-path %q must be exactly one \"family/name\" segment pair", artifactPath)
	}
	bucket, ok := packageFamilyBuckets[label]
	if !ok {
		return "", "", fmt.Errorf("artifact-path %q: unsupported family %q (expected skill, agent, or plugin)", artifactPath, label)
	}
	return bucket, rest, nil
}

// findPackageSource returns the declared source whose id matches id.
func findPackageSource(sources []config.Source, id string) (config.Source, bool) {
	for _, s := range sources {
		if s.ID == id {
			return s, true
		}
	}
	return config.Source{}, false
}

// HydratePackagesUnits is pass 2's driver (D6). It reads the resolved
// effective config's packages[] set and returns the caller-supplied
// resolved-unit set H13 requires for platform.ProjectResolvedUnits. Pass 2 is
// a no-op (nil, nil) when the effective config declares no packages, or when
// ensureRes is nil (the dry-run caller convention — see hydrateInstallPackages/
// hydrateRefreshPackages).
func HydratePackagesUnits(projectPath, projectName string, ensureRes *config.EnsureResult) ([]platform.ResolvedUnit, error) {
	if ensureRes == nil || ensureRes.Snapshot == nil || len(ensureRes.Snapshot.Effective.Packages) == 0 {
		return nil, nil
	}
	snap := ensureRes.Snapshot
	agentsHome := config.AgentsHome()
	localScopes := []string{projectName}

	if ensureRes.ReResolved {
		return resolvePackagesUnits(projectPath, agentsHome, snap, localScopes)
	}
	return hydratePackagesFromLock(projectPath, agentsHome, snap, localScopes)
}

// resolvePackagesUnits is H9's write half: each declared ref is fetched at
// its manifest version-spec, materialized, and the project's lock gains a
// refreshed kind:artifact unit per ref (H10) — merged with whatever pass-1
// config resolution already wrote so neither write clobbers the other.
func resolvePackagesUnits(projectPath, agentsHome string, snap *config.Snapshot, localScopes []string) ([]platform.ResolvedUnit, error) {
	sources := snap.Effective.Sources
	units := make([]platform.ResolvedUnit, 0, len(snap.Effective.Packages))
	locked := make(map[string]config.LockedUnit, len(snap.Effective.Packages))
	now := time.Now().UTC().Format(time.RFC3339)

	for _, pkg := range snap.Effective.Packages {
		unit, err := fetchAndMaterializePackage(agentsHome, sources, pkg.Ref, "", localScopes)
		if err != nil {
			return nil, err
		}
		units = append(units, unit)
		locked[pkg.Ref] = config.LockedUnit{Kind: config.UnitKindArtifact, Digest: unit.Digest, FetchedAt: now, LastCheckedAt: now}
	}

	if err := mergeArtifactUnitsIntoLock(projectPath, locked); err != nil {
		return nil, fmt.Errorf("packages: recording lock units: %w", err)
	}
	return units, nil
}

// hydratePackagesFromLock is H9's no-write half: each ref is fetched PINNED
// to the digest already recorded in the lock — never the manifest's raw
// version-spec, which could have moved upstream since the lock was written —
// and materialized, without touching the lock. A packages ref with no
// existing kind:artifact lock entry is an error: there is nothing to hydrate
// from, and this path must not silently fall back to a live resolve (that
// would defeat the frozen/locked-clean no-write contract).
func hydratePackagesFromLock(projectPath, agentsHome string, snap *config.Snapshot, localScopes []string) ([]platform.ResolvedUnit, error) {
	existing, err := config.ReadUnits(projectPath)
	if err != nil {
		return nil, fmt.Errorf("packages: reading lock: %w", err)
	}
	sources := snap.Effective.Sources
	units := make([]platform.ResolvedUnit, 0, len(snap.Effective.Packages))

	for _, pkg := range snap.Effective.Packages {
		lockedUnit, ok := existing.Units[pkg.Ref]
		if !ok || lockedUnit.Kind != config.UnitKindArtifact {
			return nil, fmt.Errorf("packages ref %q: no locked artifact entry to hydrate from — run `da config sync` first", pkg.Ref)
		}
		unit, err := fetchAndMaterializePackage(agentsHome, sources, pkg.Ref, lockedUnit.Digest, localScopes)
		if err != nil {
			return nil, fmt.Errorf("hydrate: %w", err)
		}
		if unit.Digest != lockedUnit.Digest {
			return nil, fmt.Errorf("packages ref %q: hydrated digest %s does not match locked digest %s", pkg.Ref, unit.Digest, lockedUnit.Digest)
		}
		units = append(units, unit)
	}
	return units, nil
}

// fetchAndMaterializePackage parses ref and fetches it — at the manifest's
// own declared version-spec when pinDigest is empty (the resolve half, which
// may re-resolve to new upstream content), or PINNED to pinDigest when set
// (the hydrate half, which must never drift from what the lock recorded even
// if upstream has moved on) — then materializes the result through t2's
// platform.MaterializeArtifact, returning the caller-ready ResolvedUnit.
func fetchAndMaterializePackage(agentsHome string, sources []config.Source, ref, pinDigest string, localScopes []string) (platform.ResolvedUnit, error) {
	parts, err := config.ParsePackageRef(ref)
	if err != nil {
		return platform.ResolvedUnit{}, fmt.Errorf("packages ref %q: %w", ref, err)
	}
	if pinDigest != "" {
		parts.VersionSpec = "pinned:" + pinDigest
	}
	src, ok := findPackageSource(sources, parts.SourceID)
	if !ok {
		return platform.ResolvedUnit{}, fmt.Errorf("packages ref %q: no declared source %q", ref, parts.SourceID)
	}
	bucket, name, err := splitPackageArtifactFamily(parts.ArtifactPath)
	if err != nil {
		return platform.ResolvedUnit{}, fmt.Errorf("packages ref %q: %w", ref, err)
	}
	fetcher, err := config.SelectPackageFetcher(src.Type)
	if err != nil {
		return platform.ResolvedUnit{}, fmt.Errorf("packages ref %q: %w", ref, err)
	}
	fetched, err := fetcher.FetchArtifact(src, parts)
	if err != nil {
		return platform.ResolvedUnit{}, fmt.Errorf("packages ref %q: fetch: %w", ref, err)
	}
	if fetched.Bundle == nil {
		return platform.ResolvedUnit{}, fmt.Errorf("packages ref %q: artifact is not a directory-shaped bundle (tree/tarball) — a plain file blob is not installable via packages", ref)
	}
	casPath, digest, err := platform.MaterializeArtifact(agentsHome, bucket, parts.SourceID, name, *fetched.Bundle, localScopes...)
	if err != nil {
		return platform.ResolvedUnit{}, fmt.Errorf("packages ref %q: materialize: %w", ref, err)
	}
	return platform.ResolvedUnit{Family: bucket, Name: name, SourceID: parts.SourceID, Digest: digest, CASPath: casPath}, nil
}

// mergeArtifactUnitsIntoLock folds artifacts into the project's existing
// units lock (layer + profile units already written by pass-1 config
// resolution) without clobbering them: WriteUnitsLock replaces the whole
// "units" section wholesale, so every write here re-reads first (H10;
// lock_units.go's LockedUnit shape is unchanged and untouched). Every
// existing kind:artifact entry is dropped and re-derived from artifacts —
// never merely added to — so a packages[] ref removed from the manifest does
// not leave an orphaned lock entry behind (R5: staleness's declared-set
// comparison stays accurate).
func mergeArtifactUnitsIntoLock(projectPath string, artifacts map[string]config.LockedUnit) error {
	existing, err := config.ReadUnits(projectPath)
	if err != nil {
		return err
	}
	merged := make(map[string]config.LockedUnit, len(existing.Units)+len(artifacts))
	for ref, u := range existing.Units {
		if u.Kind == config.UnitKindArtifact {
			continue
		}
		merged[ref] = u
	}
	for ref, u := range artifacts {
		merged[ref] = u
	}
	return config.WriteUnitsLock(projectPath, config.UnitsLock{Units: merged, InputsDigest: existing.InputsDigest})
}
