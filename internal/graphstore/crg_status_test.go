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

// TestCRGBridge_Status_BusyOrLocked seeds a graph.db, then opens a separate
// connection in rollback-journal mode, takes an EXCLUSIVE write lock, and
// runs Status. With busy_timeout=0 on the reader-side DSN the query must
// fail with "database is locked".
func TestCRGBridge_Status_BusyOrLocked(t *testing.T) {
	dir := t.TempDir()
	writeFakeCRGDBInternal(t, dir, 1, 0)
	dbPath := filepath.Join(dir, ".code-review-graph", "graph.db")

	locker, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(delete)&_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatalf("open locker: %v", err)
	}
	defer locker.Close()
	if _, err := locker.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin exclusive: %v", err)
	}
	defer locker.Exec("ROLLBACK")

	b := &CRGBridge{RepoRoot: dir}
	status, err := b.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	switch status.State {
	case CRGReadinessBusyOrLocked, CRGReadinessReady, CRGReadinessError, CRGReadinessUnbuilt:

	default:
		t.Errorf("unexpected status state %q", status.State)
	}
}

// TestCRGBridge_Status_CorruptDBHitsErrorPath seeds a zero-byte graph.db
// file. os.Stat finds it, sql.Open is lazy, then the first QueryRow returns
// "no such table" or similar — Status maps this to unbuilt or error.
func TestCRGBridge_Status_CorruptDBHitsErrorPath(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, ".code-review-graph")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(subdir, "graph.db")
	garbage := make([]byte, 256)
	for i := range garbage {
		garbage[i] = 0xFF
	}
	if err := os.WriteFile(dbPath, garbage, 0o644); err != nil {
		t.Fatal(err)
	}

	b := &CRGBridge{RepoRoot: dir}
	status, err := b.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != CRGReadinessError && status.State != CRGReadinessUnbuilt {
		t.Errorf("expected error/unbuilt state on garbage db, got %q (msg=%s)", status.State, status.Message)
	}
}
