// Package crgbridge implements the migration-only crg-bridge adapter
// (graph-backend-adapter-contract §11.2). It ships inside `da` and exposes the
// legacy Python CRG bridge state under a read-only mirror namespace
// (kg_crg-bridge.*) so migration tooling can compare bridge output to the new
// kg-native CRG adapter via the structured parity oracles in
// internal/graphstore. It is marked migration_only: true; long-term adapters
// must not declare reads_from against it (the loader rejects that).
//
// Like the kg-native crg adapter, the mirror models ingestion over a
// normalized corpus (the bridge's legacy SQLite rows, exported to the same
// Symbol/Reference shape) so the §11.6 parity rows are machine-verifiable with
// no live Python subprocess.
package crgbridge

import (
	// blank import: enables the //go:embed directive on schemaYAML.
	_ "embed"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
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

// Bootstrap mirrors the legacy bridge corpus into the kg_crg-bridge.* mirror
// namespace through the SDK and returns the structured build snapshot. The
// mirror reuses the kg-native crg ingestion (corpus.toGraph / Snapshot) so the
// two surfaces are directly comparable; the only difference is the namespace
// the SDK writes to (Name vs crg.Name). No staleness drivers fire — bridge
// mutation is observed externally (§11.2).
func Bootstrap(s *sdk.SDK, corpus crg.Corpus, commit string) (graphstore.ParitySnapshot, error) {
	notes, edges := corpus.ToGraph()
	if err := s.WriteNotes(notes); err != nil {
		return graphstore.ParitySnapshot{}, err
	}
	if err := s.WriteEdges(edges); err != nil {
		return graphstore.ParitySnapshot{}, err
	}
	return crg.Snapshot(Name, corpus, commit), nil
}
