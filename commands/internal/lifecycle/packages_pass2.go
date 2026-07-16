package lifecycle

import (
	"fmt"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
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
//     content), materializes it, and commits ONE combined lock — layer/
//     profile units (carried forward by pass-1) plus the fresh kind:artifact
//     units plus their content-integrity anchors — only after EVERY package
//     fetch + materialization succeeds (H10 + review #3 atomicity).
//   - !ReResolved (pass-1 did not write — Frozen, Locked-fresh, Offline, or
//     plain-fresh): hydratePackagesFromLock fetches each ref pinned to the
//     digest ALREADY recorded in the lock and materializes it, without
//     touching the lock at all. This is what makes a clean checkout with a
//     committed lock but an empty local CAS store work: the lock doesn't
//     need rewriting, but the store still needs populating before
//     projection.

// artifactContentLockSection is the sibling lock section (alongside "units")
// that records, per resolved packages ref, the H16 content-integrity digest
// (config.BundleContentDigest) of the materialized artifact. It is the
// git-tracked anchor the offline H7 resolver re-verifies the CAS entry
// against for ANY ref, independent of source type or declared version syntax
// — the store-addressing digest in the units section (BundleDigest) embeds
// modes + explicit dir entries and does NOT round-trip from an on-disk walk,
// so it cannot serve as the from-disk integrity reference; the content digest
// can. It is a plain sibling section (like the install stamp), so lock_units.go
// / LockedUnit stay unchanged (out of scope).
const artifactContentLockSection = "artifact-content"

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

// anyArtifactUnit reports whether units contains a kind:artifact entry — the
// signal that this project has installed packages (so pass-2 must run to
// prune them even when the manifest now declares none, review #4).
func anyArtifactUnit(units map[string]config.LockedUnit) bool {
	for _, u := range units {
		if u.Kind == config.UnitKindArtifact {
			return true
		}
	}
	return false
}

// HydratePackagesUnits is pass 2's driver (D6). It reads the resolved
// effective config's packages[] set and returns the caller-supplied
// resolved-unit set H13 requires for platform.ProjectResolvedUnits, plus a
// `participated` flag.
//
// participated reports whether pass-2 had anything to do at all — either the
// manifest declares packages, OR the lock still carries kind:artifact units
// that a now-empty manifest must prune (review #4: without this the caller
// would skip ProjectResolvedUnits on the last removal, and t2's one-to-zero
// prune would never run, leaving the final CAS link orphaned). When
// participated is true the caller MUST route projection through
// ProjectResolvedUnits with exactly the returned set (even when empty), so the
// prune scan runs; when false it takes the plain projection path unchanged
// (R6 byte-parity for projects that never used packages).
func HydratePackagesUnits(projectPath, projectName string, ensureRes *config.EnsureResult) (units []platform.ResolvedUnit, participated bool, err error) {
	if ensureRes == nil || ensureRes.Snapshot == nil {
		return nil, false, nil
	}
	declared := ensureRes.Snapshot.Effective.Packages
	existing, err := config.ReadUnits(projectPath)
	if err != nil {
		return nil, false, fmt.Errorf("packages: reading lock: %w", err)
	}
	if len(declared) == 0 && !anyArtifactUnit(existing.Units) {
		return nil, false, nil
	}

	agentsHome := config.AgentsHome()
	localScopes := []string{projectName}

	if ensureRes.ReResolved {
		units, err = resolvePackagesUnits(projectPath, agentsHome, ensureRes.Snapshot, localScopes)
	} else {
		units, err = hydratePackagesFromLock(projectPath, agentsHome, ensureRes.Snapshot, localScopes)
	}
	if err != nil {
		return nil, true, err
	}
	return units, true, nil
}

// resolvePackagesUnits is H9's write half: every declared ref is fetched at
// its manifest version-spec and materialized FIRST, accumulating the lock +
// content-integrity candidates in memory; only after ALL of them succeed is a
// SINGLE combined lock committed (review #3 atomicity — a mid-loop fetch
// failure returns before any lock write, so the prior artifact lock survives
// intact). A now-empty packages[] set commits an empty artifact set, dropping
// stale artifact lock units + their content anchors (R5).
func resolvePackagesUnits(projectPath, agentsHome string, snap *config.Snapshot, localScopes []string) ([]platform.ResolvedUnit, error) {
	sources := snap.Effective.Sources
	units := make([]platform.ResolvedUnit, 0, len(snap.Effective.Packages))
	contentByUnit := make([]string, 0, len(snap.Effective.Packages))
	artifactUnits := make(map[string]config.LockedUnit, len(snap.Effective.Packages))
	contentDigests := make(map[string]string, len(snap.Effective.Packages))
	now := time.Now().UTC().Format(time.RFC3339)

	for _, pkg := range snap.Effective.Packages {
		unit, contentDigest, err := fetchAndMaterializePackage(agentsHome, sources, pkg.Ref, "", localScopes)
		if err != nil {
			return nil, err
		}
		units = append(units, unit)
		contentByUnit = append(contentByUnit, contentDigest)
		artifactUnits[pkg.Ref] = config.LockedUnit{Kind: config.UnitKindArtifact, Digest: unit.Digest, FetchedAt: now, LastCheckedAt: now}
		contentDigests[pkg.Ref] = contentDigest
	}

	if err := commitArtifactLock(projectPath, artifactUnits, contentDigests); err != nil {
		return nil, fmt.Errorf("packages: recording lock units: %w", err)
	}
	if err := verifyProjectionInputs(agentsHome, units, contentByUnit); err != nil {
		return nil, err
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
	contentByUnit := make([]string, 0, len(snap.Effective.Packages))

	for _, pkg := range snap.Effective.Packages {
		lockedUnit, ok := existing.Units[pkg.Ref]
		if !ok || lockedUnit.Kind != config.UnitKindArtifact {
			return nil, fmt.Errorf("packages ref %q: no locked artifact entry to hydrate from — run `da config sync` first", pkg.Ref)
		}
		unit, contentDigest, err := fetchAndMaterializePackage(agentsHome, sources, pkg.Ref, lockedUnit.Digest, localScopes)
		if err != nil {
			return nil, fmt.Errorf("hydrate: %w", err)
		}
		if unit.Digest != lockedUnit.Digest {
			return nil, fmt.Errorf("packages ref %q: hydrated digest %s does not match locked digest %s", pkg.Ref, unit.Digest, lockedUnit.Digest)
		}
		units = append(units, unit)
		contentByUnit = append(contentByUnit, contentDigest)
	}
	if err := verifyProjectionInputs(agentsHome, units, contentByUnit); err != nil {
		return nil, err
	}
	return units, nil
}

// fetchAndMaterializePackage parses ref and fetches it — at the manifest's
// own declared version-spec when pinDigest is empty (the resolve half, which
// may re-resolve to new upstream content), or PINNED to pinDigest when set
// (the hydrate half, which must never drift from what the lock recorded even
// if upstream has moved on) — then materializes the result through t2's
// platform.MaterializeArtifact, returning the caller-ready ResolvedUnit plus
// the artifact's content-integrity digest (config.BundleContentDigest).
func fetchAndMaterializePackage(agentsHome string, sources []config.Source, ref, pinDigest string, localScopes []string) (platform.ResolvedUnit, string, error) {
	parts, err := config.ParsePackageRef(ref)
	if err != nil {
		return platform.ResolvedUnit{}, "", fmt.Errorf("packages ref %q: %w", ref, err)
	}
	if pinDigest != "" {
		parts.VersionSpec = "pinned:" + pinDigest
	}
	src, ok := findPackageSource(sources, parts.SourceID)
	if !ok {
		return platform.ResolvedUnit{}, "", fmt.Errorf("packages ref %q: no declared source %q", ref, parts.SourceID)
	}
	// OCI consume is deferred to t6 (package-artifact-install t3 review #5): the
	// OCI fetcher returns only Data/Digest, never a normalized Bundle, so an
	// oci-source packages ref cannot be materialized yet. Fail with a clear,
	// actionable message instead of the misleading "not a directory-shaped
	// bundle" a nil Bundle would otherwise produce downstream.
	if src.Type == "oci" {
		return platform.ResolvedUnit{}, "", fmt.Errorf("packages ref %q: OCI package consume is not yet wired (tracked in t6-oci-consume); use a git or local source for now", ref)
	}
	bucket, name, err := splitPackageArtifactFamily(parts.ArtifactPath)
	if err != nil {
		return platform.ResolvedUnit{}, "", fmt.Errorf("packages ref %q: %w", ref, err)
	}
	fetcher, err := config.SelectPackageFetcher(src.Type)
	if err != nil {
		return platform.ResolvedUnit{}, "", fmt.Errorf("packages ref %q: %w", ref, err)
	}
	fetched, err := fetcher.FetchArtifact(src, parts)
	if err != nil {
		return platform.ResolvedUnit{}, "", fmt.Errorf("packages ref %q: fetch: %w", ref, err)
	}
	if fetched.Bundle == nil {
		return platform.ResolvedUnit{}, "", fmt.Errorf("packages ref %q: artifact is not a directory-shaped bundle (tree/tarball) — a plain file blob is not installable via packages", ref)
	}
	casPath, digest, err := platform.MaterializeArtifact(agentsHome, bucket, parts.SourceID, name, *fetched.Bundle, localScopes...)
	if err != nil {
		return platform.ResolvedUnit{}, "", fmt.Errorf("packages ref %q: materialize: %w", ref, err)
	}
	contentDigest := config.BundleContentDigest(*fetched.Bundle)
	return platform.ResolvedUnit{Family: bucket, Name: name, SourceID: parts.SourceID, Digest: digest, CASPath: casPath, ContentDigest: contentDigest}, contentDigest, nil
}

// verifyProjectionInputs is the projection-boundary integrity re-check
// (package-artifact-install t3 review #2b): every resolved unit's on-disk CAS
// content is re-verified against the content-integrity digest computed from
// the just-fetched bundle, immediately before the caller hands the set to
// platform.ProjectResolvedUnits (which links on identity/path without
// re-hashing CAS bytes). A mismatch — a store tamper landed AFTER materialize
// but BEFORE projection — fails the whole pass closed, so a tampered artifact
// is never linked/invoked. Combined with the read-only store hardening
// (config.MaterializeToStore) this closes the materialize→symlink TOCTOU down
// to a window that also requires a privilege escalation to exploit.
func verifyProjectionInputs(agentsHome string, units []platform.ResolvedUnit, contentByUnit []string) error {
	for i, u := range units {
		present, matches := config.VerifyStoreContentDigest(agentsHome, u.Family, u.Digest, contentByUnit[i])
		if !present {
			return fmt.Errorf("packages %s/%s: CAS entry vanished before projection", u.Family, u.Name)
		}
		if !matches {
			return fmt.Errorf("packages %s/%s: CAS content failed integrity re-check before projection (possible store tamper)", u.Family, u.Name)
		}
	}
	return nil
}

// commitArtifactLock writes ONE combined lock (review #3 atomicity + lost-update
// fix): the units section — every existing NON-artifact unit (the layer/profile
// units pass-1 wrote) plus exactly the supplied artifact units — AND the sibling
// artifact-content integrity section, in a single serialized read-modify-write
// (agentslock.Update). Update holds the advisory lock across the read of the
// current units AND the write, so a concurrent pass-1 that committed fresh
// layer/profile units between our resolve and this write is observed and
// preserved instead of clobbered by a stale snapshot (the previous
// Open→read-outside-lock→Flush shape lost such updates). inputs_digest and every
// other sibling section (adapters, install stamp) are preserved by NOT staging
// them (mergeDiskLocked reapplies only this write's dirty keys). Existing
// artifact units are replaced wholesale, so a removed packages[] ref leaves
// neither an orphan lock unit nor an orphan content anchor (R5). lock_units.go /
// LockedUnit are untouched.
func commitArtifactLock(projectPath string, artifactUnits map[string]config.LockedUnit, contentDigests map[string]string) error {
	return agentslock.Update(config.AgentsLockPath(projectPath), func(lf *agentslock.Lockfile) error {
		existing := map[string]config.LockedUnit{}
		if _, err := lf.Section(config.LockSectionUnits, &existing); err != nil {
			return err
		}
		merged := make(map[string]config.LockedUnit, len(existing)+len(artifactUnits))
		for ref, u := range existing {
			if u.Kind == config.UnitKindArtifact {
				continue
			}
			merged[ref] = u
		}
		for ref, u := range artifactUnits {
			merged[ref] = u
		}
		if err := lf.SetSection(config.LockSectionUnits, merged); err != nil {
			return err
		}
		return lf.SetSection(artifactContentLockSection, contentDigests)
	})
}

// readArtifactContentDigests loads the artifact-content integrity section
// (ref → content digest). A missing section or read error yields an empty
// (non-nil) map so the offline H7 resolver treats an unanchored ref as
// "cannot verify" (skip) rather than crashing.
func readArtifactContentDigests(projectPath string) map[string]string {
	lf, err := agentslock.Open(config.AgentsLockPath(projectPath))
	if err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	if _, err := lf.Section(artifactContentLockSection, &out); err != nil {
		return map[string]string{}
	}
	return out
}
