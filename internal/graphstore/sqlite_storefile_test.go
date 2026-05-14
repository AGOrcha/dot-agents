// Package graphstore — coverage for the inner-transaction error branches in
// SQLiteStore.StoreFileNodesEdges. We open a store, drop one of the
// underlying tables, then call StoreFileNodesEdges to drive each
// `if err != nil { return err }` line in turn.
package graphstore

import (
	"testing"
)

// TestSQLiteStore_StoreFileNodesEdges_DeleteNodesError covers the first
// `tx.Exec("DELETE FROM nodes ...")` failure.
func TestSQLiteStore_StoreFileNodesEdges_DeleteNodesError(t *testing.T) {
	s := openInternalStore(t)
	if _, err := s.db.Exec("DROP TABLE nodes"); err != nil {
		t.Fatalf("drop nodes: %v", err)
	}
	err := s.StoreFileNodesEdges("f.go",
		[]NodeInfo{{Kind: "Function", Name: "fn", FilePath: "f.go"}},
		nil, "h",
	)
	if err == nil {
		t.Error("expected error when nodes table is missing")
	}
}

// TestSQLiteStore_StoreFileNodesEdges_DeleteEdgesError covers the second
// `tx.Exec("DELETE FROM edges ...")` failure path — nodes table is present
// (so first delete passes) but edges is missing.
func TestSQLiteStore_StoreFileNodesEdges_DeleteEdgesError(t *testing.T) {
	s := openInternalStore(t)
	if _, err := s.db.Exec("DROP TABLE edges"); err != nil {
		t.Fatalf("drop edges: %v", err)
	}
	err := s.StoreFileNodesEdges("f.go", nil, nil, "h")
	if err == nil {
		t.Error("expected error when edges table is missing")
	}
}

// TestSQLiteStore_StoreFileNodesEdges_InsertNodeError covers the inner
// node-insert error.  We drop a required column from the nodes table so
// the INSERT inside StoreFileNodesEdges fails.
func TestSQLiteStore_StoreFileNodesEdges_InsertNodeError(t *testing.T) {
	s := openInternalStore(t)
	// Drop the qualified_name column — INSERT will then reference a missing
	// column and fail. SQLite supports ALTER TABLE DROP COLUMN since 3.35.
	if _, err := s.db.Exec("ALTER TABLE nodes DROP COLUMN extra"); err != nil {
		t.Skipf("ALTER TABLE DROP COLUMN unsupported: %v", err)
	}
	err := s.StoreFileNodesEdges("f.go",
		[]NodeInfo{{Kind: "Function", Name: "fn", FilePath: "f.go"}},
		nil, "h",
	)
	if err == nil {
		t.Error("expected error from broken nodes schema")
	}
}

// TestSQLiteStore_StoreFileNodesEdges_InsertEdgeError covers the inner
// edge-insert error.  We drop a required column from the edges table so
// the INSERT inside StoreFileNodesEdges fails.
func TestSQLiteStore_StoreFileNodesEdges_InsertEdgeError(t *testing.T) {
	s := openInternalStore(t)
	if _, err := s.db.Exec("ALTER TABLE edges DROP COLUMN extra"); err != nil {
		t.Skipf("ALTER TABLE DROP COLUMN unsupported: %v", err)
	}
	err := s.StoreFileNodesEdges("f.go",
		nil,
		[]EdgeInfo{{Kind: "CALLS", Source: "a", Target: "b", FilePath: "f.go"}},
		"h",
	)
	if err == nil {
		t.Error("expected error from broken edges schema")
	}
}
