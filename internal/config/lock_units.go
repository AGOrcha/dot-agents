package config

import (
	"github.com/NikashPrakash/dot-agents/internal/agentslock"
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
)

// LockedUnit is one entry in the lockfile's "units" section (§7A.3). The map key
// is the fully-resolved ref "source:path@resolved-version"; the entry records
// the kind plus content-hash and timestamps. Staleness is content-hash driven
// (digest mismatch), never clock-driven — LastCheckedAt is a review-nudge basis
// only and never auto-invalidates.
type LockedUnit struct {
	// Kind is UnitKindLayer or UnitKindArtifact.
	Kind string `json:"kind"`
	// Digest is the content hash recorded at fetch time ("sha256:…").
	Digest string `json:"digest"`
	// FetchedAt is the RFC3339 timestamp the unit was fetched.
	FetchedAt string `json:"fetched_at"`
	// LastCheckedAt is the RFC3339 timestamp of the last explicit upstream
	// re-check; it powers a doctor/explain review-nudge and never drives
	// auto-invalidation (§7A.3). Empty when never re-checked since fetch.
	LastCheckedAt string `json:"last_checked_at,omitempty"`
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
}

// WriteUnitsLock writes the resolved units state and inputs_digest to
// .agentsrc.lock via the shared agentslock writer, preserving any sibling
// sections (e.g. "adapters") another writer populated (§7A.3). It is the §7A
// successor to WriteConfigLock; a later resolver task wires it into the
// two-pass engine.
func WriteUnitsLock(projectPath string, lock UnitsLock) error {
	lf, err := agentslock.Open(AgentsLockPath(projectPath))
	if err != nil {
		return err
	}
	units := lock.Units
	if units == nil {
		units = map[string]LockedUnit{}
	}
	// SetSection cannot fail here: "units" is not a reserved key and a
	// map[string]LockedUnit always marshals (mirrors agentslock.setVersion's
	// impossible-marshal convention). Errors surface from the atomic Flush.
	_ = lf.SetSection(LockSectionUnits, units)
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
			Kind:          kind,
			Digest:        l.ResolvedSHA,
			FetchedAt:     l.FetchedAt,
			LastCheckedAt: l.FetchedAt,
		}
	}
	return nil
}
