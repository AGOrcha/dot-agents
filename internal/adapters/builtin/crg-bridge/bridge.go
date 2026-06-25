// Package crgbridge implements the migration-only crg-bridge adapter
// (graph-backend-adapter-contract §11.2). It ships inside `da` and exposes the
// legacy Python CRG bridge state under a read-only mirror namespace
// (kg_crg-bridge.*) so migration tooling can compare bridge output to the new
// kg-native CRG adapter via the structured parity oracles in
// internal/graphstore. It is marked migration_only: true; long-term adapters
// must not declare reads_from against it (the loader rejects that).
//
// The mirror is READ-ONLY at the adapter layer: this package exposes NO write
// path. The legacy bridge namespace is populated externally by the Python
// subprocess (modeled in tests by a legacy seeder that writes as the external
// process). The adapter only reads that state back through the Store seam to
// produce parity snapshots, so it can never mutate the mirror.
package crgbridge

import (
	// blank import: enables the //go:embed directive on schemaYAML.
	_ "embed"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// Name is the adapter's short name and its kg mirror-namespace stem.
const Name = "crg-bridge"

//go:embed schema.yaml
var schemaYAML []byte

// Adapter is the migration-only crg-bridge mirror adapter. The zero value is
// usable.
type Adapter struct{}

// New returns the crg-bridge adapter.
func New() Adapter { return Adapter{} }

// Schema parses and returns the embedded §11.2 mirror schema. A malformed
// embed is a build-time bug, so Schema panics rather than failing at runtime.
func (Adapter) Schema() registry.Schema {
	s, err := registry.LoadSchema(schemaYAML)
	if err != nil {
		panic("crg-bridge adapter: embedded schema invalid: " + err.Error())
	}
	return s
}

// Name returns the adapter name.
func (a Adapter) Name() string { return a.Schema().Name }

// MigrationOnly reports whether this adapter is migration-only (§11.2). It
// reads the embedded schema's migration_only flag.
func (a Adapter) MigrationOnly() bool { return a.Schema().MigrationOnly }

// ImpactRadius is the §11.2 no-op identity: the mirror is a parity read target,
// not a review-pipeline backend (max_depth 0), so it returns the changed ids
// unchanged.
func (a Adapter) ImpactRadius(req registry.ImpactRequest) (registry.ImpactResult, error) {
	ids := make([]string, len(req.ChangedIDs))
	copy(ids, req.ChangedIDs)
	return registry.ImpactResult{IDs: ids}, nil
}

// Register adds the crg-bridge adapter to reg.
func Register(reg *registry.Registry) error {
	return reg.Register(New())
}

// MirrorSnapshot reads the legacy bridge state back from the mirror namespace
// (kg_crg-bridge.*) through the Store seam and returns its build snapshot. This
// is READ-ONLY: the adapter never writes to the namespace. The namespace is
// populated externally by the legacy Python bridge; the adapter only mirrors
// (reads) it for parity comparison against the kg-native crg snapshot.
func MirrorSnapshot(store crg.StoreReader, commit string) (graphstore.ParitySnapshot, error) {
	return crg.SnapshotFromStore(Name, store, Name, commit)
}
