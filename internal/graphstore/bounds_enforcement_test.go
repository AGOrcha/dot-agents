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
	for i := 0; i < total; i++ {
		if _, err := s.UpsertNode(NodeInfo{
			Kind:     NodeKindFunction,
			Name:     fmt.Sprintf("fn%d", i),
			FilePath: "f.go",
		}, ""); err != nil {
			t.Fatalf("seed node %d: %v", i, err)
		}
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
	for i := 0; i < defaultSearchLimit+25; i++ {
		if _, err := s.UpsertNode(NodeInfo{
			Kind: NodeKindFunction, Name: fmt.Sprintf("fn%d", i), FilePath: "f.go",
		}, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
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
	if _, err := s.UpsertNode(NodeInfo{Kind: NodeKindFunction, Name: "center", FilePath: "seed.go"}, ""); err != nil {
		t.Fatalf("seed center: %v", err)
	}
	leaves := hardMaxNodes + 100
	for i := 0; i < leaves; i++ {
		ln := fmt.Sprintf("leaf%d", i)
		if _, err := s.UpsertNode(NodeInfo{Kind: NodeKindFunction, Name: ln, FilePath: "leaf.go"}, ""); err != nil {
			t.Fatalf("seed leaf %d: %v", i, err)
		}
		if _, err := s.UpsertEdge(EdgeInfo{
			Kind: EdgeKindCalls, Source: center, Target: "leaf.go::" + ln, FilePath: "seed.go",
		}); err != nil {
			t.Fatalf("seed edge %d: %v", i, err)
		}
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
