package graphstore

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFakeCRGDBInternal mirrors writeFakeCRGDB (in crg_test.go) for use
// inside package graphstore. Kept here to avoid cross-package imports.
func writeFakeCRGDBInternal(t *testing.T, repoRoot string, nodeCount, edgeCount int) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".code-review-graph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "graph.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	defer db.Close()
	ddl := `
		CREATE TABLE nodes (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
		  file_path TEXT, line_start INTEGER, line_end INTEGER,
		  language TEXT, parent_name TEXT, params TEXT, return_type TEXT,
		  is_test INTEGER, file_hash TEXT, extra TEXT, updated_at TEXT
		);
		CREATE TABLE edges (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, source_qualified TEXT, target_qualified TEXT,
		  file_path TEXT, line INTEGER, extra TEXT, updated_at REAL
		);`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	for i := 0; i < nodeCount; i++ {
		name := "fn" + string(rune('a'+i))
		_, _ = db.Exec(
			`INSERT INTO nodes (kind,name,qualified_name,file_path,line_start,line_end,language,parent_name,params,return_type,is_test,file_hash,extra,updated_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			"Function", name, "pkg::"+name, "f.go", 1, 5, "go", "pkg", "", "", 0, "", "{}", "2026-01-01T00:00:00Z",
		)
	}
	for i := 0; i < edgeCount; i++ {
		_, _ = db.Exec(
			`INSERT INTO edges (kind,source_qualified,target_qualified,file_path,line,extra,updated_at)
			 VALUES (?,?,?,?,?,?,?)`,
			"CALLS", "pkg::A", "pkg::B", "f.go", 1, "{}", 1.0,
		)
	}
}

// erroringImpactStore satisfies impactStoreView but errors on GetEdgesAmong.
type erroringImpactStore struct {
	nodes map[string]*GraphNode
}

func (e *erroringImpactStore) GetNode(qn string) (*GraphNode, error) {
	return e.nodes[qn], nil
}

func (e *erroringImpactStore) GetEdgesAmong(qns []string) ([]GraphEdge, error) {
	return nil, errors.New("edges-among failed")
}
