// Package none implements the built-in `none` graph-backend adapter
// (graph-backend-adapter-contract §13.1).
//
// The `none` adapter is the null backend: it declares no note types and no
// edge types, and its impact radius returns the changed nodes themselves
// with no expansion (max_depth 0). It is the smallest adapter that proves
// the contract end-to-end — schema validation, registry resolution,
// lockfile init, and impact-radius routing all run against it without any
// real domain logic.
package none

import (
	_ "embed"

	"github.com/NikashPrakash/dot-agents/internal/kg/registry"
)

// Name is the adapter's short name.
const Name = "none"

// schemaYAML is the canonical §13.1 schema, embedded so the adapter and its
// loader share one source of truth. It is a package var (not a const) so a
// test can substitute a malformed schema to exercise the Schema panic guard.
//
//go:embed schema.yaml
var schemaYAML []byte

// Adapter is the built-in `none` adapter. The zero value is usable.
type Adapter struct{}

// New returns the `none` adapter.
func New() Adapter { return Adapter{} }

// Schema parses and returns the embedded §13.1 schema. The embedded YAML is
// validated at load time; a malformed embed is a build-time-fixable bug, so
// Schema panics rather than returning an error (it cannot fail at runtime
// for a shipped binary).
func (Adapter) Schema() registry.Schema {
	s, err := registry.LoadSchema(schemaYAML)
	if err != nil {
		panic("none adapter: embedded schema invalid: " + err.Error())
	}
	return s
}

// Name returns the adapter name.
func (a Adapter) Name() string { return a.Schema().Name }

// ImpactRadius returns the changed ids unchanged — the null impact radius
// (max_depth 0, no neighborhood expansion) of §13.1. nil input yields an
// empty (non-nil) result slice so callers can range without a nil guard.
func (Adapter) ImpactRadius(req registry.ImpactRequest) (registry.ImpactResult, error) {
	ids := make([]string, len(req.ChangedIDs))
	copy(ids, req.ChangedIDs)
	return registry.ImpactResult{IDs: ids}, nil
}

// Register adds the `none` adapter to reg. It is the single registration
// entry point built-in wiring calls.
func Register(reg *registry.Registry) error {
	return reg.Register(New())
}
