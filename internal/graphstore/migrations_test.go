package graphstore_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	_ "modernc.org/sqlite"
)

// TestEnsureSchema_CreatesTablesOnFreshDB opens a new SQLite database via
// OpenSQLite (which runs initSchema internally) and verifies that all
// expected tables exist.
func TestEnsureSchema_CreatesTablesOnFreshDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fresh.db")

	s, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer s.Close()

	// Open a raw connection to query sqlite_master directly.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()

	expectedTables := []string{"nodes", "edges", "metadata", "kg_notes", "note_symbol_links"}
	for _, table := range expectedTables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q should exist but query failed: %v", table, err)
			continue
		}
		if name != table {
			t.Errorf("expected table name %q, got %q", table, name)
		}
	}
}

// TestEnsureSchema_CreatesIndexes verifies that the expected indexes are
// created by the schema DDL.
func TestEnsureSchema_CreatesIndexes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "indexes.db")

	s, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer s.Close()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()

	expectedIndexes := []string{
		"idx_nodes_file",
		"idx_nodes_kind",
		"idx_nodes_qualified",
		"idx_edges_source",
		"idx_edges_target",
		"idx_edges_kind",
		"idx_edges_file",
		"idx_kg_notes_type",
		"idx_kg_notes_status",
		"idx_kg_notes_archived",
		"idx_nsl_note_id",
		"idx_nsl_qualified",
	}
	for _, idx := range expectedIndexes {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q should exist but query failed: %v", idx, err)
			continue
		}
	}
}

// TestEnsureSchema_NodesTableColumns verifies the nodes table has the full
// set of columns expected by the Store interface contract.
func TestEnsureSchema_NodesTableColumns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cols.db")

	s, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer s.Close()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("PRAGMA table_info(nodes)")
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()

	wantCols := map[string]bool{
		"id": false, "kind": false, "name": false, "qualified_name": false,
		"file_path": false, "line_start": false, "line_end": false,
		"language": false, "parent_name": false, "params": false,
		"return_type": false, "modifiers": false, "is_test": false,
		"file_hash": false, "extra": false, "updated_at": false,
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, ok := wantCols[name]; ok {
			wantCols[name] = true
		}
	}
	for col, found := range wantCols {
		if !found {
			t.Errorf("expected nodes column %q", col)
		}
	}
}

// TestEnsureSchema_KGNotesUniqueByID verifies the kg_notes table enforces
// PRIMARY KEY on id (duplicate insert should fail via raw SQL).
func TestEnsureSchema_KGNotesUniqueByID(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "unique.db")
	s, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer s.Close()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(
		`INSERT INTO kg_notes (id,title,note_type,status,summary,file_path,version,archived_at,indexed_at)
		 VALUES ('dup','t1','concept','active','','a.md',0,'',1.0)`,
	)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Second insert with the same id must fail (PRIMARY KEY).
	_, err = db.Exec(
		`INSERT INTO kg_notes (id,title,note_type,status,summary,file_path,version,archived_at,indexed_at)
		 VALUES ('dup','t2','concept','active','','a.md',0,'',1.0)`,
	)
	if err == nil {
		t.Error("expected PRIMARY KEY violation on duplicate id")
	}
}

// TestEnsureSchema_NoteSymbolLinkUniqueConstraint verifies the UNIQUE
// constraint across (note_id, qualified_name, link_kind).
func TestEnsureSchema_NoteSymbolLinkUniqueConstraint(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nsl.db")
	s, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer s.Close()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(
		`INSERT INTO note_symbol_links (note_id,qualified_name,link_kind,created_at)
		 VALUES ('n1','q1','mentions',1.0)`,
	)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO note_symbol_links (note_id,qualified_name,link_kind,created_at)
		 VALUES ('n1','q1','mentions',2.0)`,
	)
	if err == nil {
		t.Error("expected UNIQUE constraint violation")
	}
}

// TestEnsureSchema_IdempotentOnExistingDB verifies that running schema init
// twice on the same database does not error (CREATE TABLE IF NOT EXISTS).
func TestEnsureSchema_IdempotentOnExistingDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "idempotent.db")

	s1, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// Insert a node to prove data survives re-init
	_, err = s1.UpsertNode(graphstore.NodeInfo{
		Kind:     "Function",
		Name:     "TestFunc",
		FilePath: "test.go",
	}, "abc123")
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	s1.Close()

	s2, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer s2.Close()

	stats, err := s2.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalNodes != 1 {
		t.Errorf("expected 1 node after re-open, got %d", stats.TotalNodes)
	}
}
