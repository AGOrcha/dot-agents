package graphstore

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestCRGBridge_Status_NoNodesTable triggers the line 352-356 branch in
// crg.go's Status: nodes table is missing, applyCRGStatusError is invoked
// with an "no such table: nodes" error, classified as unbuilt.
func TestCRGBridge_Status_NoNodesTable(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, ".code-review-graph")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "graph.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Create a different table so the DB file exists but `nodes` does not.
	if _, err := db.Exec("CREATE TABLE placeholder (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	b := &CRGBridge{RepoRoot: dir}
	status, err := b.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != string(CRGReadinessUnbuilt) {
		t.Errorf("expected state=unbuilt for missing nodes table, got %q (msg=%q)", status.State, status.Message)
	}
	if status.Message == "" {
		t.Error("expected non-empty message when nodes table is missing")
	}
}

// TestCRGBridge_Status_NoEdgesTable triggers the line 358-361 branch: nodes
// table exists but edges does not. Status classifies via applyCRGStatusError.
func TestCRGBridge_Status_NoEdgesTable(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, ".code-review-graph")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "graph.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE nodes (
		id INTEGER PRIMARY KEY, file_path TEXT, updated_at TEXT, language TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	b := &CRGBridge{RepoRoot: dir}
	status, err := b.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	// "no such table: edges" classifies as unbuilt.
	if status.State != string(CRGReadinessUnbuilt) {
		t.Errorf("expected state=unbuilt for missing edges table, got %q (msg=%q)", status.State, status.Message)
	}
}

// TestCRGBridge_Status_NoLanguagesViaCorrupt triggers the readCRGLanguages
// error path by closing the db before Status reads from it. We can't easily
// do that from outside, so instead we exercise the "all tables exist but
// nodes has no `language` column" branch.
//
// Note: schema without `language` would already fail the earlier query that
// reads file_path, so we need to construct one where COUNT/MAX works but
// SELECT DISTINCT language fails. Instead, omit language column from nodes
// and rely on the unbuilt classification of the failing query.
func TestCRGBridge_Status_LanguagesQueryFails(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, ".code-review-graph")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "graph.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// nodes without `language` column will fail the readCRGLanguages query.
	if _, err := db.Exec(`CREATE TABLE nodes (
		id INTEGER PRIMARY KEY, file_path TEXT, updated_at TEXT
	); CREATE TABLE edges (id INTEGER PRIMARY KEY);`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	b := &CRGBridge{RepoRoot: dir}
	status, err := b.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	// "no such column: language" matches `not found` in isCRGUnbuiltError -> unbuilt.
	if status.State == string(CRGReadinessReady) {
		t.Errorf("did not expect ready status; got state=%q msg=%q", status.State, status.Message)
	}
}
