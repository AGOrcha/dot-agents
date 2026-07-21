package graphstore

import (
	"fmt"
	"path/filepath"
	"testing"
)

// graphstore_bench_test.go — Phase 0 measure-first baselines for the KG
// query hot paths at realistic scale (~5k nodes/edges), SQLite backend.
// Regression guard:
//
//	go test -bench=. -benchtime=10x -run='^$' ./internal/graphstore/
//
// The fixture reuses the single-Tx StoreFileNodesEdges seed shape from
// bounds_enforcement_test.go (per-row UpsertNode commits dominate wall-clock
// on modernc, so the whole seed goes through one transaction), scaled to N
// function nodes across several files with a 2-edge-per-node call graph so
// the impact BFS, edge-batch and search read paths all do real work.

const benchFileCount = 50

// benchScales sweeps a small graph and a ~5k node/edge graph (the CONTRACT
// hardMaxNodes ceiling) so a regression shows up as a change in the per-op
// slope, not just an absolute number on one size.
var benchScales = []int{1000, 5000}

// openBenchSQLite opens a throwaway SQLiteStore for a benchmark, mirroring
// the openTestSQLiteInternal tempdir+Close pattern (b.TempDir is auto-cleaned).
func openBenchSQLite(b *testing.B) *SQLiteStore {
	b.Helper()
	s, err := OpenSQLite(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	b.Cleanup(func() { _ = s.Close() })
	return s
}

// seedBenchGraph seeds n function nodes across benchFileCount files plus a
// 2-edge-per-node call graph (i -> i+1 and i -> i+13, wrapping), all in one
// transaction. Every edge's source and target is a seeded node, so the whole
// returned qualified-name slice is a valid GetEdgesAmong batch. Returns each
// node's qualified name in source order.
func seedBenchGraph(b *testing.B, s *SQLiteStore, n int) []string {
	b.Helper()
	nodes := make([]NodeInfo, n)
	quals := make([]string, n)
	for i := range n {
		file := fmt.Sprintf("pkg%d.go", i%benchFileCount)
		name := fmt.Sprintf("fn%d", i)
		nodes[i] = NodeInfo{Kind: NodeKindFunction, Name: name, FilePath: file, Language: "go"}
		quals[i] = file + "::" + name
	}
	edges := make([]EdgeInfo, 0, 2*n)
	for i := range n {
		edges = append(edges,
			EdgeInfo{Kind: EdgeKindCalls, Source: quals[i], Target: quals[(i+1)%n], FilePath: nodes[i].FilePath},
			EdgeInfo{Kind: EdgeKindCalls, Source: quals[i], Target: quals[(i+13)%n], FilePath: nodes[i].FilePath},
		)
	}
	if err := s.StoreFileNodesEdges("seed.go", nodes, edges, ""); err != nil {
		b.Fatalf("seed: %v", err)
	}
	return quals
}

// scaleName labels a sub-benchmark by its node/edge scale.
func scaleName(n int) string { return fmt.Sprintf("N=%d", n) }

// BenchmarkGetImpactRadius measures the blast-radius hot path: seed lookup by
// changed file, full-table edge-adjacency scan, and the bounded BFS. Bounds
// are requested at the CONTRACT hard caps so the traversal runs to the
// provider ceiling.
func BenchmarkGetImpactRadius(b *testing.B) {
	changed := []string{"pkg0.go", "pkg1.go", "pkg2.go"}
	for _, n := range benchScales {
		b.Run(scaleName(n), func(b *testing.B) {
			s := openBenchSQLite(b)
			seedBenchGraph(b, s, n)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := s.GetImpactRadius(changed, hardMaxDepth, hardMaxNodes); err != nil {
					b.Fatalf("GetImpactRadius: %v", err)
				}
			}
		})
	}
}

// BenchmarkGetStats measures the aggregate-stats hot path (the per-kind count
// scans plus language/file enumeration) over the seeded graph.
func BenchmarkGetStats(b *testing.B) {
	for _, n := range benchScales {
		b.Run(scaleName(n), func(b *testing.B) {
			s := openBenchSQLite(b)
			seedBenchGraph(b, s, n)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := s.GetStats(); err != nil {
					b.Fatalf("GetStats: %v", err)
				}
			}
		})
	}
}

// BenchmarkSearchNodes measures the LIKE-based node search. The query matches
// every seeded function name, so each iteration collects up to the search
// hard limit and exercises the row-scan/decode path at its ceiling.
func BenchmarkSearchNodes(b *testing.B) {
	for _, n := range benchScales {
		b.Run(scaleName(n), func(b *testing.B) {
			s := openBenchSQLite(b)
			seedBenchGraph(b, s, n)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := s.SearchNodes("fn", hardSearchLimit); err != nil {
					b.Fatalf("SearchNodes: %v", err)
				}
			}
		})
	}
}

// BenchmarkGetEdgesAmong measures the edge-batch hot path: the whole
// qualified-name set is passed, exercising the 450-per-query IN batching and
// the target-membership filter across n/450 batches.
func BenchmarkGetEdgesAmong(b *testing.B) {
	for _, n := range benchScales {
		b.Run(scaleName(n), func(b *testing.B) {
			s := openBenchSQLite(b)
			quals := seedBenchGraph(b, s, n)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := s.GetEdgesAmong(quals); err != nil {
					b.Fatalf("GetEdgesAmong: %v", err)
				}
			}
		})
	}
}
