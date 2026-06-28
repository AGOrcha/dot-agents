package config

import (
	"encoding/json"
	"sort"
)

// profile_projection.go renders a RESOLVED profile into its on-disk,
// harness-consumable form (R8). A platform file — AGENT.md frontmatter, the hook
// skill-allowlist data, MCP inclusion, settings — is a PROJECTION of the resolved
// profile, never a second source of truth: it is GENERATED from the effective
// bundle (via da refresh / install) and stamped with the SOURCE digest it was
// produced from, so projection drift (profile_drift.go, R7) is a digest compare
// rather than a deep re-resolve. The engine-level projection is the canonical
// effective bundle plus that provenance; the per-harness file shapes (Q5) layer
// on top of this without re-deriving config.

// ProfileProjection is the materialized rendering of a resolved profile. It
// carries the SOURCE digest (the resolved bundle's Decision-7 digest at
// projection time) and the contributing refs so a later check can tell whether
// the inputs that produced it have since moved (R7) — the projection is downstream
// of the resolution, never its authority (R8).
type ProfileProjection struct {
	// SourceDigest is ResolvedProfile.Digest at projection time — the pin the
	// drift check compares a fresh resolution against.
	SourceDigest string `json:"source_digest"`
	// Refs is the sorted set of absolute refs that contributed to the projected
	// bundle, captured so a provenance change (Decision 7) registers as drift even
	// when values coincide.
	Refs []string `json:"contributing_refs"`
	// Bundle is the canonical (deep-copied) effective fragment that was
	// materialized. It is independent of the source ResolvedProfile so a later
	// mutation of either side does not silently alias the other.
	Bundle map[string]any `json:"bundle"`
}

// ProjectProfile materializes a resolved profile into a projection (R8). The
// bundle is DEEP-COPIED so the projection is a self-contained snapshot — editing
// the source resolution afterward cannot mutate an already-materialized
// projection, which is what lets drift detection compare the two independently.
func ProjectProfile(resolved ResolvedProfile) ProfileProjection {
	return ProfileProjection{
		SourceDigest: resolved.Digest,
		Refs:         sortedCopy(resolved.Contributing),
		Bundle:       deepCopyBundle(resolved.Bundle),
	}
}

// Allowlist extracts the string-set at a dot-path leaf of the projected bundle
// (e.g. "tools.allow", "skills.allow") in sorted order — the concrete shape a
// harness allowlist projection (the orchestrator Skill-scoping dogfood, §5.3)
// consumes. A path that is absent, or whose leaf is not a string array, yields an
// empty slice so a missing projection reads as "grants nothing" rather than
// panicking.
func (p ProfileProjection) Allowlist(path string) []string {
	val, ok := lookupPath(p.Bundle, splitFieldPath(path))
	if !ok {
		return []string{}
	}
	arr, ok := val.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// deepCopyBundle returns an independent deep copy of a bundle via a JSON
// round-trip. The bundle is always plain JSON values (the resolver builds it from
// decoded fragments), so the round-trip cannot fail; a nil bundle copies to a
// non-nil empty map so the projection always carries a usable object.
func deepCopyBundle(bundle map[string]any) map[string]any {
	if len(bundle) == 0 {
		return map[string]any{}
	}
	data, _ := json.Marshal(bundle)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out
}

// sortedCopy returns a sorted copy of a string slice, leaving the input
// untouched (the projection must not reorder the resolution's own ref slice).
func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}
