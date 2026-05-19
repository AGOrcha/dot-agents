package graphstore

import (
	"fmt"
	"path/filepath"
	"testing"
)

// openTestSQLiteInternal opens a throwaway SQLiteStore for in-package
// enforcement tests (the external sqlite_test.go helper is not visible
// here). Mirrors that helper's tempdir+Close pattern.
func openTestSQLiteInternal(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "bounds.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestSQLiteSearchNodesEnforcesHardLimit proves the row ceiling is HARD on
// the native path: with more rows than hardSearchLimit and a caller asking
// for far more, the result is capped at hardSearchLimit (the caller value
// is a request, the provider owns the cap).
func TestSQLiteSearchNodesEnforcesHardLimit(t *testing.T) {
	s := openTestSQLiteInternal(t)
	total := hardSearchLimit + 50
	// One transaction instead of `total` implicit per-UpsertNode commits.
	// modernc/Windows fsyncs on every commit, so a per-row seed of this
	// size dominated the suite wall-clock; StoreFileNodesEdges wraps the
	// whole seed in a single Tx (same rows, same upsert semantics).
	nodes := make([]NodeInfo, 0, total)
	for i := 0; i < total; i++ {
		nodes = append(nodes, NodeInfo{
			Kind:     NodeKindFunction,
			Name:     fmt.Sprintf("fn%d", i),
			FilePath: "f.go",
		})
	}
	if err := s.StoreFileNodesEdges("f.go", nodes, nil, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := s.SearchNodes("fn", 10_000_000) // caller asks for "everything"
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(got) > hardSearchLimit {
		t.Fatalf("SearchNodes returned %d rows, hard cap is %d", len(got), hardSearchLimit)
	}
	if len(got) != hardSearchLimit {
		t.Fatalf("expected exactly the hard cap %d rows, got %d", hardSearchLimit, len(got))
	}
}

// TestSQLiteSearchNodesZeroLimitUsesDefault proves an unset (0) limit is
// the provider default, not "unbounded".
func TestSQLiteSearchNodesZeroLimitUsesDefault(t *testing.T) {
	s := openTestSQLiteInternal(t)
	// Single-Tx seed (see note in TestSQLiteSearchNodesEnforcesHardLimit).
	total := defaultSearchLimit + 25
	nodes := make([]NodeInfo, 0, total)
	for i := 0; i < total; i++ {
		nodes = append(nodes, NodeInfo{
			Kind: NodeKindFunction, Name: fmt.Sprintf("fn%d", i), FilePath: "f.go",
		})
	}
	if err := s.StoreFileNodesEdges("f.go", nodes, nil, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := s.SearchNodes("fn", 0)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(got) != defaultSearchLimit {
		t.Fatalf("0 limit should yield default %d rows, got %d", defaultSearchLimit, len(got))
	}
}

// TestSQLiteImpactRadiusBoundsAreClampedAndUniform proves GetImpactRadius
// routes through the same normalizeTraversalBounds chokepoint the CRG path
// uses: an over-cap maxNodes request cannot produce more than hardMaxNodes
// impacted nodes. Builds a star wider than the hard cap.
func TestSQLiteImpactRadiusBoundsAreClampedAndUniform(t *testing.T) {
	s := openTestSQLiteInternal(t)

	// Qualified name = FilePath + "::" + Name when there is no parent
	// (see makeQualified): the edge source must match the seed node's
	// qualified name or the BFS finds no neighbours.
	const center = "seed.go::center"
	leaves := hardMaxNodes + 100
	// Single-Tx seed (see note in TestSQLiteSearchNodesEnforcesHardLimit):
	// ~5.1k nodes + ~5.1k edges was the dominant per-row commit cost. The
	// filePath param only scopes the (no-op, fresh-DB) pre-delete; each
	// node/edge keeps its own FilePath, so the seeded shape is unchanged.
	nodes := make([]NodeInfo, 0, leaves+1)
	nodes = append(nodes, NodeInfo{Kind: NodeKindFunction, Name: "center", FilePath: "seed.go"})
	edges := make([]EdgeInfo, 0, leaves)
	for i := 0; i < leaves; i++ {
		ln := fmt.Sprintf("leaf%d", i)
		nodes = append(nodes, NodeInfo{Kind: NodeKindFunction, Name: ln, FilePath: "leaf.go"})
		edges = append(edges, EdgeInfo{
			Kind: EdgeKindCalls, Source: center, Target: "leaf.go::" + ln, FilePath: "seed.go",
		})
	}
	if err := s.StoreFileNodesEdges("seed.go", nodes, edges, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Ask for far more than the hard cap; provider must clamp.
	res, err := s.GetImpactRadius([]string{"seed.go"}, 1<<20, 1<<20)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	if len(res.ImpactedNodes) > hardMaxNodes {
		t.Fatalf("impacted nodes %d exceeded hard cap %d (bounds not enforced)",
			len(res.ImpactedNodes), hardMaxNodes)
	}
}
