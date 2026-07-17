package config

import (
	"github.com/AGOrcha/dot-agents/internal/agentslock"
)

// LockSectionUnits is the agentslock section name for the unified units model
// (config-distribution-model §7A.3). It replaces the legacy per-tier "config"
// and "packages" sections: one map keyed by "source:path@version" carrying a
// `kind`. The "adapters" section stays separate (graph lifecycle owns it).
const LockSectionUnits = "units"

// Unit kinds (§7A.1): a resolvable unit is either a config `layer` (merges into
// effective config, may declare more units) or an executable `artifact`
// (installed discretely, invoked under trust/signing). The kind governs
// merge/trust only, not sourcing.
const (
	UnitKindLayer    = "layer"
	UnitKindArtifact = "artifact"
	// UnitKindProjectSet is the §15 D13/A2 synced identity-registry unit: the
	// portable project IDENTITY (id + portable key, NO path). It is a first-class
	// unit — scope- and manifest-referenceable, locked, and a member of
	// inputs_digest when locally authored — under the same selector-merge law as
	// every other unit. The machine-local BINDING table (id → absolute-path) is
	// NOT this unit and is never synced/scoped/projected (see IsSyncedUnitKind).
	UnitKindProjectSet = "project-set"
	// UnitKindDescriptor is the §15 D3/A3 CONDITIONAL fourth behavior: declarative,
	// non-merging, non-installing projection data. It stays Go-internal / NOT a
	// §15 unit (no lock entry, not projected) until a descriptor becomes
	// source-shipped (gated by the multi-harness F4 probe). The constant reserves
	// the kind so the resolver/lock recognize and fail-closed on a descriptor unit
	// today rather than mis-resolving it (see IsProjectableKind).
	UnitKindDescriptor = "descriptor"
)

// LockedUnit is one entry in the lockfile's "units" section (§7A.3). The map key
// is the fully-resolved ref "source:path@resolved-version"; the entry records
// the kind plus the content hash. Staleness is content-hash driven (digest
// mismatch), never clock-driven.
//
// The lock is content-addressed (uv-style): it carries NO wall-clock timestamps.
// A frozen no-op re-install must therefore produce a byte-identical units section
// — re-stamping a per-unit fetched_at/last_checked_at on every run churned the
// lock and, on a slow filesystem (windows CI), opened a transient inconsistent-read
// window between install and the very next read. The FetchedAt/LastCheckedAt
// fields are retained ONLY to decode legacy locks that still carry them; they are
// never written. Any "last re-checked N ago" refresh timestamping belongs in the
// machine-local config (the id→absolute-path binding table), NOT in the portable,
// content-addressed lock.
type LockedUnit struct {
	// Kind is UnitKindLayer or UnitKindArtifact.
	Kind string `json:"kind"`
	// Digest is the content hash recorded at fetch time ("sha256:…").
	Digest string `json:"digest"`
	// FetchedAt is legacy-decode only (see the type comment): read from a v1 lock
	// that carried it, never written by the current resolver. omitempty so a
	// re-serialized unit drops it.
	FetchedAt string `json:"fetched_at,omitempty"`
	// LastCheckedAt is legacy-decode only (see the type comment): the review-nudge
	// basis a v1 lock carried. Never written now; the refresh-timestamp home is the
	// machine-local config. omitempty so a re-serialized unit drops it.
	LastCheckedAt string `json:"last_checked_at,omitempty"`
	// CacheKey is the effective content cache key the unit resolved at
	// (config-distribution-model §7A.4). It is the cache-key staleness axis —
	// orthogonal to the content-hash driver events — that CacheKeyStaleForLayer
	// compares on a later resolve. Carried on the units model (not just the
	// retired legacy "config" section) so the §7A units-lock cutover does NOT
	// drop the §7A.4 cache-key gate: a `--refresh`/always_revalidate force escape
	// and a cache_keys override edit both still register. Omitted for a unit
	// resolved without a cache key (e.g. a legacy lock migrated on read).
	CacheKey string `json:"cache_key,omitempty"`
}

// UnitsLock is the config-owned view of the lockfile under the §7A model: the
// resolved units map plus the top-level inputs_digest (the whole-normalized hash
// of all local config scopes). Staleness is an inputs_digest mismatch OR a
// changed declared set OR a per-unit digest mismatch — all cheap, local, and
// clock-free.
type UnitsLock struct {
	// Units is keyed by "source:path@resolved-version".
	Units map[string]LockedUnit
	// InputsDigest is the top-level whole-normalized local-scope hash. Empty
	// when no local scope has been hashed yet.
	InputsDigest string
	// ProfileUnits is the kind:profile contribution (R2): the resolved profile
	// fragments projected to LockedUnit entries (ProfileLockUnits), keyed by the
	// profile's namespaced key. It is folded INTO the persisted "units" section by
	// WriteUnitsLock so a kind:profile unit is a first-class lock entry alongside
	// layer/artifact units — making a profile resolution reproducible from the lock
	// without re-resolving. Nil for a caller that records no profiles (the section
	// then carries only Units, exactly as before).
	ProfileUnits map[string]LockedUnit
}

// allUnits returns the full unit set to persist: the base Units plus the
// ProfileUnits contribution folded in. A profile key never collides with a layer/
// artifact ref (profile keys are <source>:profile:… or authored:…), so the fold is
// purely additive; on the impossible collision the profile entry wins, keeping the
// kind:profile lock authoritative.
func (l UnitsLock) allUnits() map[string]LockedUnit {
	out := make(map[string]LockedUnit, len(l.Units)+len(l.ProfileUnits))
	for ref, u := range l.Units {
		out[ref] = u
	}
	for key, u := range l.ProfileUnits {
		out[key] = u
	}
	return out
}

// WriteUnitsLock writes the resolved units state and inputs_digest to
// .agentsrc.lock via the shared agentslock writer, preserving any sibling
// sections (e.g. "adapters") another writer populated (§7A.3). The persisted
// "units" section is the base Units folded with the ProfileUnits contribution
// (R2), so profile units land in the lock through this one funnel — no parallel
// profile-lock machinery. It is the §7A successor to WriteConfigLock.
func WriteUnitsLock(projectPath string, lock UnitsLock) error {
	lf, err := agentslock.Open(AgentsLockPath(projectPath))
	if err != nil {
		return err
	}
	// SetSection cannot fail here: "units" is not a reserved key and a
	// map[string]LockedUnit always marshals (mirrors agentslock.setVersion's
	// impossible-marshal convention). Errors surface from the atomic Flush.
	_ = lf.SetSection(LockSectionUnits, lock.allUnits())
	lf.SetInputsDigest(lock.InputsDigest)
	return lf.Flush()
}

// ReadUnits loads the §7A units view of an existing lockfile. When the file
// already carries a "units" section it is read directly; when it does not but a
// legacy "config"/"packages" pair is present, the legacy shape is migrated in
// memory (v1 → v2) so callers always see the unified model. A wholly absent or
// empty lockfile yields an empty (non-nil) units map (§7A.3).
func ReadUnits(projectPath string) (UnitsLock, error) {
	lf, err := agentslock.Open(AgentsLockPath(projectPath))
	if err != nil {
		return UnitsLock{}, err
	}
	digest, _ := lf.InputsDigest()
	units := map[string]LockedUnit{}
	present, err := lf.Section(LockSectionUnits, &units)
	if err != nil {
		return UnitsLock{}, err
	}
	if present {
		return UnitsLock{Units: units, InputsDigest: digest}, nil
	}
	migrated, err := migrateLegacyUnits(lf)
	if err != nil {
		return UnitsLock{}, err
	}
	return UnitsLock{Units: migrated, InputsDigest: digest}, nil
}

// migrateLegacyUnits builds the §7A units map from a v1 lockfile's legacy
// "config" (layers) and "packages" (artifacts) sections. resolved_sha → digest,
// ttl_expires_at is dropped (the clock-based TTL is replaced by content-hash
// staleness; last_checked_at carries the review-nudge basis instead, §7A.3).
// Missing sections contribute nothing; an all-empty legacy file yields an empty
// map.
func migrateLegacyUnits(lf *agentslock.Lockfile) (map[string]LockedUnit, error) {
	units := map[string]LockedUnit{}
	if err := mergeLegacySection(lf, LockSectionConfig, UnitKindLayer, units); err != nil {
		return nil, err
	}
	if err := mergeLegacySection(lf, LockSectionPackages, UnitKindArtifact, units); err != nil {
		return nil, err
	}
	return units, nil
}

// mergeLegacySection decodes one legacy LockedLayer-shaped section and folds its
// entries into units under the given kind. An absent section is a no-op.
func mergeLegacySection(lf *agentslock.Lockfile, section, kind string, units map[string]LockedUnit) error {
	legacy := map[string]LockedLayer{}
	present, err := lf.Section(section, &legacy)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	for ref, l := range legacy {
		units[ref] = LockedUnit{
			Kind:     kind,
			Digest:   l.ResolvedSHA,
			CacheKey: l.CacheKey,
		}
	}
	return nil
}
