package crg

import (
	"fmt"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// postprocess_bench_test.go — measure-first baselines for the CRG derived-view
// readback hot paths (Phase 0 of the perf-optimization plan). Each benchmark
// seeds a synthetic graph through the kg-native adapter's Bootstrap (writing
// kg_crg.*), then times the derivation reading the persisted namespace back
// through the Store seam — the same discipline the parity tests use. The graph
// is a forest of small CALLS binary trees (each block root is a flow entry
// point) with cross-block IMPORTS edges (community-merging) and a few TESTED_BY
// edges, so flows, communities, and risk_index all do real work.

const benchBlockSize = 16

// benchCorpus builds a synthetic corpus of n symbols wired into per-block CALLS
// trees plus cross-block IMPORTS/TESTED_BY edges.
func benchCorpus(n int) Corpus {
	c := Corpus{Commit: "bench"}
	c.Symbols = make([]Symbol, n)
	for i := range n {
		c.Symbols[i] = Symbol{
			QualifiedName: fmt.Sprintf("sym%d", i),
			Kind:          kindFn,
			Language:      "go",
			FilePath:      fmt.Sprintf("f%d.go", i),
			ContentHash:   fmt.Sprintf("h%d", i),
		}
	}
	c.References = benchReferences(n)
	return c
}

// benchReferences wires the CALLS trees and the cross-block edges for an
// n-symbol corpus.
func benchReferences(n int) []Reference {
	refs := make([]Reference, 0, n*2)
	for i := range n {
		local := i % benchBlockSize
		if local == 0 {
			continue // block root — a flow entry point (no incoming CALLS)
		}
		parent := i - local + (local-1)/2
		refs = append(refs, callRef(parent, i))
	}
	for i := 0; i+benchBlockSize < n; i += benchBlockSize {
		refs = append(refs, Reference{Kind: edgeImports, From: symName(i), To: symName(i + benchBlockSize)})
		if i+2 < n {
			refs = append(refs, Reference{Kind: edgeTestedBy, From: symName(i + 1), To: symName(i + 2)})
		}
	}
	return refs
}

func symName(i int) string { return fmt.Sprintf("sym%d", i) }

func callRef(from, to int) Reference {
	return Reference{Kind: edgeCalls, From: symName(from), To: symName(to)}
}

// benchSeededStore bootstraps c into the crg namespace and returns the store.
func benchSeededStore(b *testing.B, c Corpus) *sdk.MemStore {
	b.Helper()
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	if _, err := Bootstrap(s, store, c, nil); err != nil {
		b.Fatalf("bootstrap: %v", err)
	}
	return store
}

// crgBenchScales is the symbol-count sweep the CRG derivations parametrize over.
var crgBenchScales = []struct {
	name string
	n    int
}{
	{"N=100", 100},
	{"N=500", 500},
	{"N=2000", 2000},
}

func BenchmarkFlowsFromStore(b *testing.B) {
	for _, sc := range crgBenchScales {
		store := benchSeededStore(b, benchCorpus(sc.n))
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := FlowsFromStore(store, Name); err != nil {
					b.Fatalf("flows: %v", err)
				}
			}
		})
	}
}

func BenchmarkCommunitiesFromStore(b *testing.B) {
	for _, sc := range crgBenchScales {
		store := benchSeededStore(b, benchCorpus(sc.n))
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := CommunitiesFromStore(store, Name); err != nil {
					b.Fatalf("communities: %v", err)
				}
			}
		})
	}
}

func BenchmarkRiskIndexFromStore(b *testing.B) {
	for _, sc := range crgBenchScales {
		store := benchSeededStore(b, benchCorpus(sc.n))
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := RiskIndexFromStore(store, Name); err != nil {
					b.Fatalf("risk index: %v", err)
				}
			}
		})
	}
}
