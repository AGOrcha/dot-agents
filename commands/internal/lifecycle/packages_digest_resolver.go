package lifecycle

import (
	"github.com/AGOrcha/dot-agents/internal/config"
)

// packages_digest_resolver.go builds H7's production artifact-store integrity
// resolver: a config.UnitDigestFunc — the same seam config.Staleness/
// EnsureResolved already define — that detects a `kind:artifact` unit whose
// installed CAS content no longer matches what was locked, so a post-install
// store tamper registers as unit-digest staleness instead of being silently
// trusted. It is threaded into BOTH config.EnsureResolved (install/refresh,
// see ensureInstallResolved/ensureLockFreshForRefresh) and `da config verify`
// (commands/config/verify.go), closing the gap H7 names: both previously
// passed nil.
//
// Review #1 correction: integrity is verified from the git-tracked
// content-integrity anchor (the artifact-content lock section written by
// pass-2), NOT from the declared version-spec. So it covers ALL refs
// uniformly — an ordinary `@main`/`@1` ref is checked exactly like a
// `@pinned:sha256:…` one — and it is fully OFFLINE and source-type agnostic:
// it re-hashes the on-disk CAS tree (config.StoreContentDigest, via
// VerifyStoreContentDigest) and compares to the committed anchor, never
// re-fetching (honoring Staleness's network-free contract). The
// store-addressing digest in the units section (BundleDigest) embeds modes +
// explicit dir entries and does not round-trip from disk, which is exactly
// why the separate content anchor exists.
//
// It never self-heals (review #2a): a verify-only caller reports the tamper
// (forces a unit-digest-mismatch driver event so `config verify` FAILs) and
// leaves the tampered bytes in place for the operator to see — it does not
// quarantine or re-fetch, which would erase the evidence.

// PackagesArtifactDigestResolver returns the H7 resolver for projectPath.
func PackagesArtifactDigestResolver(projectPath string) config.UnitDigestFunc {
	return func(ref string) (string, bool) {
		units, err := config.ReadUnits(projectPath)
		if err != nil {
			return "", false
		}
		locked, ok := units.Units[ref]
		if !ok || locked.Kind != config.UnitKindArtifact {
			return "", false
		}
		expectedContent, ok := readArtifactContentDigests(projectPath)[ref]
		if !ok || expectedContent == "" {
			// No committed integrity anchor for this ref (e.g. a lock written
			// before pass-2 recorded content digests): cannot verify offline,
			// so skip rather than false-alarm.
			return "", false
		}
		parts, err := config.ParsePackageRef(ref)
		if err != nil {
			return "", false
		}
		bucket, _, err := splitPackageArtifactFamily(parts.ArtifactPath)
		if err != nil {
			return "", false
		}

		present, matches := config.VerifyStoreContentDigest(config.AgentsHome(), bucket, locked.Digest, expectedContent)
		if !present {
			// Not hydrated on this machine yet — HydratePackagesUnits' job, not
			// a tamper signal.
			return "", false
		}
		if !matches {
			// The CAS entry exists but its content no longer matches the
			// committed integrity anchor. Return a value guaranteed to differ
			// from locked.Digest so the caller registers unit-digest-mismatch
			// staleness (surfacing the tamper); never quarantine/re-fetch here.
			return locked.Digest + ":tampered", true
		}
		return locked.Digest, true
	}
}
