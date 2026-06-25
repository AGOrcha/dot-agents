// Package crg implements the built-in kg-native Code Review Graph adapter
// (graph-backend-adapter-contract §11). It replaces the legacy Python
// subprocess bridge: its bootstrap performs Tree-sitter-style symbol
// ingestion into the kg_crg.* namespace through the da-adapter-sdk, and the
// parity surfaces the §11.6 corpus tests compare are computed by READING BACK
// the actually-persisted notes/edges from the Store seam (readback.go) — never
// from the input corpus.
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

// driverSourceMutation is the O5 staleness driver this adapter declares. It
// fires per symbol ONLY when the symbol's content-hash actually changed
// relative to the prior persisted state (insert or content-change) — not on
// every bootstrap, and not per any-upsert.
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

// ImpactRadius runs the no-op identity used when no store is bound. The real
// blast-radius computation reads the persisted edge graph back from the store
// (ImpactRadiusFromStore); this method satisfies the registry.Adapter contract.
func (a Adapter) ImpactRadius(req registry.ImpactRequest) (registry.ImpactResult, error) {
	ids := make([]string, len(req.ChangedIDs))
	copy(ids, req.ChangedIDs)
	return registry.ImpactResult{IDs: ids}, nil
}

// Register adds the CRG adapter to reg.
func Register(reg *registry.Registry) error {
	return reg.Register(New())
}

// Bootstrap ingests corpus into the adapter's kg_crg.* namespace through the
// SDK (writes only — §8.2 ModeWrite), fires the O5 source_mutation driver only
// for symbols whose content-hash changed relative to prevNotes (the prior
// commit's persisted notes; nil for the first commit), then returns the build
// snapshot computed by READING BACK the persisted namespace from the store. The
// returned snapshot reflects what storage actually holds, so dropped dangling
// edges do not appear and parity is verified, not guaranteed by construction.
func Bootstrap(s *sdk.SDK, store StoreReader, corpus Corpus, prevNotes []sdk.Note) (graphstore.ParitySnapshot, error) {
	notes, edges := corpus.ToGraph()
	if err := s.WriteNotes(notes); err != nil {
		return graphstore.ParitySnapshot{}, err
	}
	if err := s.WriteEdges(edges); err != nil {
		return graphstore.ParitySnapshot{}, err
	}
	fireSourceMutations(s, prevNotes, notes)
	return SnapshotFromStore(s.Adapter(), store, s.Adapter(), corpus.Commit)
}

// fireSourceMutations fires the O5 source_mutation driver for each newly-written
// note whose content-hash differs from its prior persisted value (insert or
// content change). An unchanged re-bootstrap fires nothing (O5: content-hash
// change, not any-upsert).
func fireSourceMutations(s *sdk.SDK, prevNotes, curNotes []sdk.Note) {
	prevHash := make(map[string]string, len(prevNotes))
	for _, n := range prevNotes {
		prevHash[n.ID] = fieldString(n, fieldContentSum)
	}
	for _, n := range curNotes {
		cur := fieldString(n, fieldContentSum)
		if old, existed := prevHash[n.ID]; existed && old == cur {
			continue // content unchanged — driver must NOT fire (O5)
		}
		s.DeclarePredicateFired(driverSourceMutation, map[string]any{
			"id":            n.ID,
			fieldContentSum: cur,
		})
	}
}
