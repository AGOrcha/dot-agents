package lifecycle

import (
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// packages_digest_resolver.go builds H7's production artifact-store integrity
// resolver: a config.UnitDigestFunc — the same seam config.Staleness/
// EnsureResolved already define — that detects a `kind:artifact` unit whose
// CAS content no longer matches what it should, so a post-install store
// tamper registers as unit-digest staleness instead of being silently
// trusted. It is threaded into BOTH config.EnsureResolved (install/refresh,
// see ensureInstallResolved) and `da config verify` (commands/config/
// verify.go), closing the gap H7 names: both previously passed nil.
//
// config.Staleness is documented as network-free and clock-free
// (staleness.go), so this resolver only ever participates when a ref can be
// verified WITHOUT contacting the network: a `local`-type source (a
// filesystem read is not network), or a non-local source whose fetch resolves
// as a pure cache hit for a digest-pinned version-spec (FetchedArtifact.
// CacheHit). Every other case — a non-artifact unit, an unpinned/tag/branch
// version-spec, or a non-local fetch that would need to hit the network to
// answer — returns ok=false (skip, not a driver event), matching
// UnitDigestFunc's documented "content not locally available" contract.
//
// It reuses config.VerifyArtifactStoreDigest (t2's H16 verify-on-hit,
// exposed read-only) rather than re-verifying by writing through
// MaterializeToStore — a verify-only caller must never silently self-heal a
// tamper it is trying to report.

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
		parts, err := config.ParsePackageRef(ref)
		if err != nil {
			return "", false
		}
		pinnedDigest, isPinned := digestFromPinnedVersionSpec(parts.VersionSpec)
		if !isPinned || pinnedDigest != locked.Digest {
			// Only a ref pinned to the EXACT locked digest is something this
			// resolver can verify without a network round-trip to re-resolve
			// a tag/branch to "what upstream has now".
			return "", false
		}
		bucket, _, err := splitPackageArtifactFamily(parts.ArtifactPath)
		if err != nil {
			return "", false
		}
		rc, err := config.LoadAgentsRC(projectPath)
		if err != nil {
			return "", false
		}
		src, ok := findPackageSource(rc.Sources, parts.SourceID)
		if !ok {
			return "", false
		}
		fetcher, err := config.SelectPackageFetcher(src.Type)
		if err != nil {
			return "", false
		}
		fetched, err := fetcher.FetchArtifact(src, parts)
		if err != nil || fetched.Bundle == nil {
			return "", false
		}
		if src.Type != "local" && !fetched.CacheHit {
			// A non-local, non-cache-hit fetch touched the network to answer —
			// Staleness must stay network-free, so this result is unusable here
			// even though it succeeded.
			return "", false
		}

		agentsHome := config.AgentsHome()
		present, matches := config.VerifyArtifactStoreDigest(agentsHome, bucket, locked.Digest, *fetched.Bundle)
		if !present {
			// Not hydrated yet on this machine — that is HydratePackagesUnits'
			// job, not a tamper signal.
			return "", false
		}
		if !matches {
			// The CAS entry exists but its content no longer matches its own
			// digest: return a value guaranteed to differ from locked.Digest
			// (never empty for a real digest) so the caller registers this as
			// unit-digest-mismatch staleness.
			return "", true
		}
		return locked.Digest, true
	}
}

// digestFromPinnedVersionSpec extracts the "sha256:..." digest from a
// "pinned:sha256:..." version-spec, mirroring fetcher_oci.go's unexported
// digestFromVersionSpec (kept package-local there; this is the lifecycle
// package's own copy of the same, tiny, dependency-free parse).
func digestFromPinnedVersionSpec(spec string) (string, bool) {
	const pin = "pinned:"
	if strings.HasPrefix(spec, pin) {
		d := spec[len(pin):]
		if strings.HasPrefix(d, "sha256:") {
			return d, true
		}
	}
	return "", false
}
