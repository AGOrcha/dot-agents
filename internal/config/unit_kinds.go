package config

import "fmt"

// unit_kinds.go owns the §15 D3/D13 unit-kind guards (config-distribution-model
// §15.9 items 7-8). It keeps the descriptor kind internal (non-projected until
// source-shipped) and the synced project-set kind distinct from the
// machine-local binding table, so the resolver/lock recognize the kinds today
// without enabling the deferred behaviors.

// descriptorsSourceShipped gates the §15 D3/A3 CONDITIONAL fourth resolver
// behavior. It is false until the multi-harness F4 probe ratifies the descriptor
// schema and a descriptor becomes source-shipped. While false, a descriptor is
// Go-internal data, NOT a §15 unit — IsProjectableKind/ValidateUnitKind reject a
// descriptor unit so the resolver fails closed rather than mis-resolving it.
const descriptorsSourceShipped = false

// KnownUnitKind reports whether kind is a recognized §15 unit kind. descriptor is
// recognized as a reserved kind even though it is not yet a projectable/lockable
// unit (see IsProjectableKind).
func KnownUnitKind(kind string) bool {
	switch kind {
	case UnitKindLayer, UnitKindArtifact, UnitKindProjectSet, UnitKindDescriptor, UnitKindProfile, UnitKindManifest:
		return true
	default:
		return false
	}
}

// IsProjectableKind reports whether a unit of this kind participates in
// resolution/projection today. layer (merges), artifact (installs), project-set
// (synced identity registry), and manifest (distributable §15+L1 bundle, L2) are
// projectable; descriptor is NOT until descriptorsSourceShipped flips (§15 D3/A3).
func IsProjectableKind(kind string) bool {
	switch kind {
	case UnitKindLayer, UnitKindArtifact, UnitKindProjectSet, UnitKindProfile, UnitKindManifest:
		return true
	case UnitKindDescriptor:
		return descriptorsSourceShipped
	default:
		return false
	}
}

// IsSyncedUnitKind reports whether a unit of this kind is SYNCED config (rides a
// scope, participates in the lock + inputs_digest). project-set is synced
// portable identity (§15 D13/A2); manifest is the synced, scope-attachable L2
// bundle (distributable-config-manifest D1); the machine-local binding table
// (id → absolute-path) is never a unit, so it never reaches this guard. A
// descriptor is not synced until source-shipped.
func IsSyncedUnitKind(kind string) bool {
	switch kind {
	case UnitKindLayer, UnitKindArtifact, UnitKindProjectSet, UnitKindProfile, UnitKindManifest:
		return true
	default:
		return false
	}
}

// ValidateUnitKind rejects a unit kind that must not appear as a resolvable/
// lockable unit yet. An unknown kind and a not-yet-source-shipped descriptor are
// both fail-closed errors, so a lockfile or manifest can never smuggle a
// mis-resolved unit past the resolver (§15.9 items 7-8).
func ValidateUnitKind(kind string) error {
	if !KnownUnitKind(kind) {
		return fmt.Errorf("unknown unit kind %q", kind)
	}
	if kind == UnitKindDescriptor && !descriptorsSourceShipped {
		return fmt.Errorf("unit kind %q is internal-only until source-shipped (multi-harness F4 probe); not a resolvable unit", kind)
	}
	return nil
}
