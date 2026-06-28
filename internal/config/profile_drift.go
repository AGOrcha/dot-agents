package config

import "sort"

// profile_drift.go detects PROJECTION drift (R7): a materialized projection
// (profile_projection.go) that no longer matches the profile the engine now
// resolves. Because platform files are projections of a resolved profile, the
// inputs can move (a fragment edit, a policy change, an authority/grant shift)
// and leave the on-disk projection stale — "AGENT.md grants Skill but the
// skill-allowlist gate is missing," or "the projected allowlist does not match
// the effective profile digest." This is the drift-verification surface
// `da config verify` / `da doctor` consumes (done-criterion 9). It is read-only:
// it reports what changed, never repairs the projection.

// ProfileDriftKind classifies one field's drift between a projection and the
// freshly resolved bundle.
type ProfileDriftKind string

const (
	// DriftChanged means the field is present on both sides with different values
	// (the resolution moved a value the projection still carries the old form of).
	DriftChanged ProfileDriftKind = "changed"
	// DriftAdded means the field is present in the resolved bundle but ABSENT from
	// the projection (the resolution grew a field the stale projection is missing).
	DriftAdded ProfileDriftKind = "added"
	// DriftRemoved means the field is present in the projection but ABSENT from the
	// resolved bundle (the projection carries a field the resolution dropped).
	DriftRemoved ProfileDriftKind = "removed"
)

// ProfileFieldDrift is one drifted leaf: its dot-path, the kind of drift, and the
// two sides' values so a caller renders a precise "what changed" diagnostic.
type ProfileFieldDrift struct {
	// Path is the dot-path of the drifted leaf.
	Path string `json:"path"`
	// Kind classifies the drift (changed / added / removed).
	Kind ProfileDriftKind `json:"kind"`
	// Projected is the value the materialized projection carries (nil for added).
	Projected any `json:"projected,omitempty"`
	// Resolved is the value the engine now resolves (nil for removed).
	Resolved any `json:"resolved,omitempty"`
}

// ProfileDriftResult is the outcome of comparing a projection against the freshly
// resolved profile. HasDrift is the single boolean `da config verify` branches on;
// Changes enumerates every drifted leaf in deterministic (sorted-path) order.
type ProfileDriftResult struct {
	// HasDrift is true when the projection is stale: the source digest moved OR a
	// field-level difference exists between the projection and the resolved bundle.
	HasDrift bool `json:"has_drift"`
	// DigestMatch reports whether the projection's recorded source digest still
	// equals the freshly resolved digest (Decision 7) — the cheap primary signal.
	DigestMatch bool `json:"digest_match"`
	// ProjectedDigest is the projection's recorded source digest (stale when drift).
	ProjectedDigest string `json:"projected_digest"`
	// ResolvedDigest is the freshly resolved bundle digest.
	ResolvedDigest string `json:"resolved_digest"`
	// Changes is every drifted leaf, sorted by path. Empty when the projection is
	// byte-current with the resolution.
	Changes []ProfileFieldDrift `json:"changes,omitempty"`
}

// DetectProfileDrift compares a materialized projection against the profile the
// engine now resolves and reports the drift (R7). Drift is signalled two ways,
// both surfaced: the recorded source digest no longer matches the fresh
// resolution digest (the cheap Decision-7 check), AND/OR a field-level difference
// exists between the projection's bundle and the resolved bundle. HasDrift is the
// disjunction so a tampered projection whose digest was left stale-equal is still
// caught by the value diff.
func DetectProfileDrift(projection ProfileProjection, resolved ResolvedProfile) ProfileDriftResult {
	digestMatch := projection.SourceDigest == resolved.Digest
	changes := diffBundles(projection.Bundle, resolved.Bundle)
	return ProfileDriftResult{
		HasDrift:        !digestMatch || len(changes) > 0,
		DigestMatch:     digestMatch,
		ProjectedDigest: projection.SourceDigest,
		ResolvedDigest:  resolved.Digest,
		Changes:         changes,
	}
}

// DetectProfileDriftFromSnapshot resolves the given dispatch context on the live
// §15 + L1 substrate and compares a stored projection against it — the single
// readback path `da config verify` calls to flag a stale projection without the
// caller re-implementing resolution. A resolve error (a malformed policy or a
// fatal authority violation) propagates fail-closed, exactly as the engine raises
// it.
func DetectProfileDriftFromSnapshot(projection ProfileProjection, snap *Snapshot, role, appType, stage, harness string) (ProfileDriftResult, error) {
	resolved, err := ResolveProfileContext(snap, role, appType, stage, harness)
	if err != nil {
		return ProfileDriftResult{}, err
	}
	return DetectProfileDrift(projection, resolved), nil
}

// diffBundles computes the per-leaf drift between a projected bundle and the
// resolved bundle. Both are flattened to dot-path leaves; a path on only one side
// is added/removed, a path on both with unequal values is changed. The result is
// sorted by path for deterministic rendering.
func diffBundles(projected, resolved map[string]any) []ProfileFieldDrift {
	projLeaves := leafMap(projected)
	resLeaves := leafMap(resolved)

	seen := map[string]bool{}
	var out []ProfileFieldDrift
	for path, projVal := range projLeaves {
		seen[path] = true
		resVal, ok := resLeaves[path]
		if !ok {
			out = append(out, ProfileFieldDrift{Path: path, Kind: DriftRemoved, Projected: projVal})
			continue
		}
		if !valuesEqual(projVal, resVal) {
			out = append(out, ProfileFieldDrift{Path: path, Kind: DriftChanged, Projected: projVal, Resolved: resVal})
		}
	}
	for path, resVal := range resLeaves {
		if !seen[path] {
			out = append(out, ProfileFieldDrift{Path: path, Kind: DriftAdded, Resolved: resVal})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// leafMap flattens a bundle into a path→value map of its leaves, reusing the
// resolver's flattenLeaves so the drift diff sees exactly the leaves the merge
// produced.
func leafMap(bundle map[string]any) map[string]any {
	out := map[string]any{}
	for _, lf := range flattenLeaves(bundle) {
		out[lf.path] = lf.value
	}
	return out
}
