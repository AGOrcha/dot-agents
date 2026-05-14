package graphstore

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// withSQLOpen swaps the sqlOpen seam for the duration of the test.
func withSQLOpen(t *testing.T, fn func(driver, dsn string) (*sql.DB, error)) {
	t.Helper()
	prev := sqlOpen
	sqlOpen = fn
	t.Cleanup(func() { sqlOpen = prev })
}

// withDBExec swaps the dbExec seam for the duration of the test.
func withDBExec(t *testing.T, fn func(*sql.DB, string, ...any) (sql.Result, error)) {
	t.Helper()
	prev := dbExec
	dbExec = fn
	t.Cleanup(func() { dbExec = prev })
}

// TestOpenSQLite_SQLOpenError covers the early sql.Open failure branch in
// OpenSQLite. We inject a stub via the sqlOpen seam.
func TestOpenSQLite_SQLOpenError(t *testing.T) {
	withSQLOpen(t, func(driver, dsn string) (*sql.DB, error) {
		return nil, errors.New("synthetic open failure")
	})
	dir := t.TempDir()
	_, err := OpenSQLite(filepath.Join(dir, "x.db"))
	if err == nil {
		t.Fatal("expected error when sql.Open fails")
	}
}

// TestOpenSQLite_WALPragmaError covers the journal_mode PRAGMA failure path.
func TestOpenSQLite_WALPragmaError(t *testing.T) {
	withDBExec(t, func(db *sql.DB, q string, args ...any) (sql.Result, error) {
		if q == "PRAGMA journal_mode=WAL" {
			return nil, errors.New("synthetic WAL failure")
		}
		return db.Exec(q, args...)
	})
	dir := t.TempDir()
	_, err := OpenSQLite(filepath.Join(dir, "x.db"))
	if err == nil {
		t.Fatal("expected WAL pragma error")
	}
}

// TestOpenSQLite_BusyTimeoutPragmaError covers the busy_timeout failure.
func TestOpenSQLite_BusyTimeoutPragmaError(t *testing.T) {
	withDBExec(t, func(db *sql.DB, q string, args ...any) (sql.Result, error) {
		if q == "PRAGMA busy_timeout=5000" {
			return nil, errors.New("synthetic busy_timeout failure")
		}
		return db.Exec(q, args...)
	})
	dir := t.TempDir()
	_, err := OpenSQLite(filepath.Join(dir, "x.db"))
	if err == nil {
		t.Fatal("expected busy_timeout pragma error")
	}
}

// TestOpenSQLite_ForeignKeysPragmaError covers the foreign_keys failure path.
func TestOpenSQLite_ForeignKeysPragmaError(t *testing.T) {
	withDBExec(t, func(db *sql.DB, q string, args ...any) (sql.Result, error) {
		if q == "PRAGMA foreign_keys=ON" {
			return nil, errors.New("synthetic foreign_keys failure")
		}
		return db.Exec(q, args...)
	})
	dir := t.TempDir()
	_, err := OpenSQLite(filepath.Join(dir, "x.db"))
	if err == nil {
		t.Fatal("expected foreign_keys pragma error")
	}
}

// TestOpenSQLite_InitSchemaError covers the initSchema failure branch by
// injecting a failure on the (very long) schemaSQL exec.
func TestOpenSQLite_InitSchemaError(t *testing.T) {
	withDBExec(t, func(db *sql.DB, q string, args ...any) (sql.Result, error) {
		// schemaSQL is multi-statement DDL beginning with CREATE TABLE.
		// PRAGMAs are short, single statements, so length+content disambiguates.
		if strings.Contains(q, "CREATE TABLE") {
			return nil, errors.New("synthetic schema failure")
		}
		return db.Exec(q, args...)
	})
	dir := t.TempDir()
	_, err := OpenSQLite(filepath.Join(dir, "x.db"))
	if err == nil {
		t.Fatal("expected init schema error")
	}
}
