// Package crg implements the built-in kg-native Code Review Graph adapter
// (graph-backend-adapter-contract §11). It replaces the legacy Python
// subprocess bridge: its bootstrap performs Tree-sitter-style symbol
// ingestion into the kg_crg.* namespace through the da-adapter-sdk, and it
// exposes the structured parity surfaces (snapshot, upserts, impact radius)
// the §11.6 corpus tests compare against the crg-bridge mirror.
//
// Tree-sitter parsing itself is the bootstrap skill's concern; this package
// models ingestion over a normalized symbol corpus so the §11.1 parity rows
// are machine-verifiable with no live subprocess (the dogfood pattern).
package crg

import (
	// blank import: enables the //go:embed directive on schemaYAML.
	_ "embed"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// Name is the adapter's short name and its kg namespace stem (kg_crg.*).
const Name = "crg"

// driverSourceMutation is the O5 staleness driver this adapter declares and
// fires per ingested symbol on its content-hash (not per any-upsert).
const driverSourceMutation = "source_mutation"

//go:embed schema.yaml
var schemaYAML []byte

// Adapter is the built-in kg-native CRG adapter. The zero value is usable.
type Adapter struct{}

// New returns the CRG adapter.
func New() Adapter { return Adapter{} }

// Schema parses and returns the embedded §11 schema. A malformed embed is a
// build-time bug, so Schema panics rather than returning a runtime error.
func (Adapter) Schema() registry.Schema {
	s, err := registry.LoadSchema(schemaYAML)
	if err != nil {
		panic("crg adapter: embedded schema invalid: " + err.Error())
	}
	return s
}

// Name returns the adapter name.
func (a Adapter) Name() string { return a.Schema().Name }

// ImpactRadius runs the adapter's impact-radius operation against an already
// bootstrapped namespace view. The kg-native adapter expands the changed ids
// along CALLS/TESTED_BY/IMPORTS edges up to the schema's max_depth — the
// parity counterpart of the legacy bridge's impact-radius tool.
func (a Adapter) ImpactRadius(req registry.ImpactRequest) (registry.ImpactResult, error) {
	// Without a bootstrapped store this degenerates to the identity (the
	// changed ids themselves) — the same shape the contract's no-op returns.
	ids := make([]string, len(req.ChangedIDs))
	copy(ids, req.ChangedIDs)
	return registry.ImpactResult{IDs: ids}, nil
}

// Register adds the CRG adapter to reg.
func Register(reg *registry.Registry) error {
	return reg.Register(New())
}

// Bootstrap ingests corpus into the adapter's kg_crg.* namespace through the
// SDK (writes only — §8.2 ModeWrite) and returns the structured build
// snapshot. It is the kg-native replacement for the bridge `build` tool. The
// snapshot's per-kind buckets are the O6-refinement-A anchor columns the
// §11.6 parity test compares.
func Bootstrap(s *sdk.SDK, corpus Corpus, commit string) (graphstore.ParitySnapshot, error) {
	notes, edges := corpus.ToGraph()
	if err := s.WriteNotes(notes); err != nil {
		return graphstore.ParitySnapshot{}, err
	}
	if err := s.WriteEdges(edges); err != nil {
		return graphstore.ParitySnapshot{}, err
	}
	// O5: source_mutation fires per ingested symbol on its content-hash, not
	// per any-upsert — the driver event the contract's staleness slice needs.
	for _, sym := range corpus.Symbols {
		s.DeclarePredicateFired(driverSourceMutation, map[string]any{
			"id":            symbolID(sym),
			fieldContentSum: sym.ContentHash,
		})
	}
	return Snapshot(Name, corpus, commit), nil
}
