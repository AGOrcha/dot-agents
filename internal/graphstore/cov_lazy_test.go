package graphstore

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// lazyStore: argument/result forwarding
// ---------------------------------------------------------------------------

// covLazyIgnoreFTSUnsupported maps the in-test fake's "this backend has no FTS
// index" answer to nil.
//
// The shared lazyMethods table is consumed by a happy-path delegation test that
// reads any error as a delegation failure. For the three FTS delegators
// ErrFTSUnsupported IS the backend's successful answer (fakeStore reports the
// capability gap rather than lying with an empty result), so it is the one
// error that does not mean the delegator misbehaved. Every other error —
// including the sticky open error the second consumer asserts on — passes
// through untouched, and TestCovLazyStoreForwardsArgumentsAndBackendResults
// separately proves the FTS delegator reaches the right backend method.
func covLazyIgnoreFTSUnsupported(err error) error {
	if errors.Is(err, ErrFTSUnsupported) {
		return nil
	}
	return err
}

// covLazyRecorder is a Store that records what the derived methods under test
// were handed and answers with distinctive values. It exists because a table
// that only checks `err == nil` would still pass if a delegator dropped its
// arguments or dispatched to a neighbouring backend method.
type covLazyRecorder struct {
	fakeStore

	gotCommunityID int64
	gotSnapshots   []FlowSnapshotRow
	gotFTSWords    []string
	gotFTSLimit    int
}

// covLazyBackendNodes is the payload the recorder's read method answers with.
var covLazyBackendNodes = []GraphNode{
	{ID: 42, Kind: "Function", Name: "Login", QualifiedName: "auth::Login", CommunityID: 7},
}

// covLazyBackendIDs is the payload the recorder's search method answers with.
var covLazyBackendIDs = []int64{11, 22, 33}

// covLazyBackendReplaced is the row count the recorder's write method reports.
const covLazyBackendReplaced = 1234

func (r *covLazyRecorder) ReadNodesByCommunity(communityID int64) ([]GraphNode, error) {
	r.gotCommunityID = communityID
	return covLazyBackendNodes, nil
}

func (r *covLazyRecorder) ReplaceFlowSnapshots(rows []FlowSnapshotRow) (int, error) {
	r.gotSnapshots = rows
	return covLazyBackendReplaced, nil
}

func (r *covLazyRecorder) SearchNodesFTSWords(words []string, limit int) ([]int64, error) {
	r.gotFTSWords = words
	r.gotFTSLimit = limit
	return covLazyBackendIDs, nil
}

var _ Store = (*covLazyRecorder)(nil)

// TestCovLazyStoreForwardsArgumentsAndBackendResults proves the derived
// delegators are real pass-throughs: arguments arrive at the backend unchanged
// and the backend's value comes back unchanged, for a read, a write and a
// search method.
func TestCovLazyStoreForwardsArgumentsAndBackendResults(t *testing.T) {
	t.Parallel()
	rec := &covLazyRecorder{}
	ls := NewLazyStore(func() (Store, error) { return rec, nil })

	t.Run("read/ReadNodesByCommunity", func(t *testing.T) {
		covLazyCheckReadForwarding(t, ls, rec)
	})
	t.Run("write/ReplaceFlowSnapshots", func(t *testing.T) {
		covLazyCheckWriteForwarding(t, ls, rec)
	})
	t.Run("search/SearchNodesFTSWords", func(t *testing.T) {
		covLazyCheckSearchForwarding(t, ls, rec)
	})
}

func covLazyCheckReadForwarding(t *testing.T, ls Store, rec *covLazyRecorder) {
	t.Helper()
	got, err := ls.ReadNodesByCommunity(7)
	if err != nil {
		t.Fatalf("ReadNodesByCommunity through lazy store: %v", err)
	}
	if rec.gotCommunityID != 7 {
		t.Fatalf("backend received communityID=%d, want 7", rec.gotCommunityID)
	}
	if !reflect.DeepEqual(got, covLazyBackendNodes) {
		t.Fatalf("got %+v, want the backend's rows %+v", got, covLazyBackendNodes)
	}
}

func covLazyCheckWriteForwarding(t *testing.T, ls Store, rec *covLazyRecorder) {
	t.Helper()
	rows := []FlowSnapshotRow{
		{FlowID: 3, Name: "login", EntryPoint: "auth::Login", Criticality: 0.5, NodeCount: 4},
	}
	n, err := ls.ReplaceFlowSnapshots(rows)
	if err != nil {
		t.Fatalf("ReplaceFlowSnapshots through lazy store: %v", err)
	}
	if !reflect.DeepEqual(rec.gotSnapshots, rows) {
		t.Fatalf("backend received %+v, want %+v", rec.gotSnapshots, rows)
	}
	if n != covLazyBackendReplaced {
		t.Fatalf("got count %d, want the backend's %d", n, covLazyBackendReplaced)
	}
}

func covLazyCheckSearchForwarding(t *testing.T, ls Store, rec *covLazyRecorder) {
	t.Helper()
	words := []string{"login", "token"}
	ids, err := ls.SearchNodesFTSWords(words, 5)
	if err != nil {
		t.Fatalf("SearchNodesFTSWords through lazy store: %v", err)
	}
	if !reflect.DeepEqual(rec.gotFTSWords, words) {
		t.Fatalf("backend received words %q, want %q", rec.gotFTSWords, words)
	}
	if rec.gotFTSLimit != 5 {
		t.Fatalf("backend received limit=%d, want 5", rec.gotFTSLimit)
	}
	if !reflect.DeepEqual(ids, covLazyBackendIDs) {
		t.Fatalf("got ids %v, want the backend's %v", ids, covLazyBackendIDs)
	}
}

// ---------------------------------------------------------------------------
// migrateCodeGraphSchema / hasColumn against real SQLite handles
// ---------------------------------------------------------------------------

// covLazySeedNodeSQL and covLazySeedEdgeSQL insert one row using only the
// pre-v9 column set, so they work on a legacy database and their rows must
// survive the ALTER TABLE migration.
const covLazySeedNodeSQL = `INSERT INTO nodes (kind,name,qualified_name,file_path,updated_at)
	VALUES ('Function','Login','auth::Login','auth.go',1)`

const covLazySeedEdgeSQL = `INSERT INTO edges (kind,source_qualified,target_qualified,file_path,updated_at)
	VALUES ('CALLS','auth::Login','auth::hash','auth.go',1)`

// covLazyStoreOnDDL opens a fresh on-disk SQLite database, runs each script in
// order and returns a store bound to it. Using a real handle (not the dbExec
// seam) is the point: the migration's contract is the resulting schema, which
// only a real SQLite can report.
func covLazyStoreOnDDL(t *testing.T, scripts ...string) (*SQLiteStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, script := range scripts {
		if _, err := db.Exec(script); err != nil {
			t.Fatalf("exec %q: %v", script, err)
		}
	}
	return &SQLiteStore{db: db}, db
}

// covLazyMigratedDDL is schemaSQL plus every added column, i.e. a database
// already carrying the v9 nodes/edges column set but neither nodes_fts nor
// idx_nodes_community.
func covLazyMigratedDDL() []string {
	return append([]string{schemaSQL}, covLazyAddedColumnDDL("")...)
}

// covLazyAddedColumnDDL returns the ALTER statements for every added column
// except the named one, so a caller can build a legacy database missing
// exactly that column.
func covLazyAddedColumnDDL(except string) []string {
	var out []string
	for _, c := range codeGraphAddedColumns {
		if c.column == except {
			continue
		}
		out = append(out, c.ddl)
	}
	return out
}

// covLazyColumnCounts reports how many times each column name appears in a
// table, so a duplicate ALTER shows up as a count of 2 rather than passing a
// presence check.
func covLazyColumnCounts(t *testing.T, db *sql.DB, table string) map[string]int {
	t.Helper()
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column name: %v", err)
		}
		counts[name]++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns of %s: %v", table, err)
	}
	return counts
}

// covLazyObjectType returns the sqlite_master type of a schema object, or ""
// when no object of that name exists.
func covLazyObjectType(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var typ string
	err := db.QueryRow("SELECT type FROM sqlite_master WHERE name = ?", name).Scan(&typ)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("sqlite_master lookup %s: %v", name, err)
	}
	return typ
}

// covLazyAssertV9Shape asserts the whole post-migration contract: every added
// column present exactly once, the FTS virtual table instantiated as a table,
// and the community index created as an index.
func covLazyAssertV9Shape(t *testing.T, db *sql.DB) {
	t.Helper()
	counts := map[string]map[string]int{
		"nodes": covLazyColumnCounts(t, db, "nodes"),
		"edges": covLazyColumnCounts(t, db, "edges"),
	}
	for _, c := range codeGraphAddedColumns {
		if got := counts[c.table][c.column]; got != 1 {
			t.Fatalf("%s.%s appears %d times after migration, want exactly 1", c.table, c.column, got)
		}
	}
	if got := covLazyObjectType(t, db, "nodes_fts"); got != "table" {
		t.Fatalf("nodes_fts is %q after migration, want a table", got)
	}
	if got := covLazyObjectType(t, db, "idx_nodes_community"); got != "index" {
		t.Fatalf("idx_nodes_community is %q after migration, want an index", got)
	}
}

// TestCovLazyMigrateFreshDatabaseReachesV9 proves the migration brings a
// just-created base schema to the v9 shape, and that the appended columns
// carry upstream's defaults (an edge inserted without them reads back 1.0 /
// EXTRACTED, which is what confidence-aware consumers rely on).
func TestCovLazyMigrateFreshDatabaseReachesV9(t *testing.T) {
	t.Parallel()
	s, db := covLazyStoreOnDDL(t, schemaSQL)

	if err := s.migrateCodeGraphSchema(); err != nil {
		t.Fatalf("migrate fresh database: %v", err)
	}
	covLazyAssertV9Shape(t, db)

	if _, err := db.Exec(covLazySeedEdgeSQL); err != nil {
		t.Fatalf("insert edge after migration: %v", err)
	}
	var confidence float64
	var tier string
	err := db.QueryRow("SELECT confidence, confidence_tier FROM edges").Scan(&confidence, &tier)
	if err != nil {
		t.Fatalf("read appended edge columns: %v", err)
	}
	if confidence != 1.0 || tier != "EXTRACTED" {
		t.Fatalf("appended edge defaults = (%v, %q), want (1, %q)", confidence, tier, "EXTRACTED")
	}
}

// TestCovLazyMigrateIsIdempotent proves a second run on an already-migrated
// database executes no DDL and disturbs nothing: no duplicate columns, no
// second FTS table, and rows written between the runs survive.
func TestCovLazyMigrateIsIdempotent(t *testing.T) {
	t.Parallel()
	s, db := covLazyStoreOnDDL(t, schemaSQL)

	if err := s.migrateCodeGraphSchema(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if _, err := db.Exec(covLazySeedNodeSQL); err != nil {
		t.Fatalf("seed node between migrations: %v", err)
	}
	if err := s.migrateCodeGraphSchema(); err != nil {
		t.Fatalf("re-running migrate on a migrated database: %v", err)
	}

	covLazyAssertV9Shape(t, db)
	var name string
	var signature sql.NullString
	err := db.QueryRow("SELECT name, signature FROM nodes").Scan(&name, &signature)
	if err != nil {
		t.Fatalf("read node after re-migration: %v", err)
	}
	if name != "Login" || signature.Valid {
		t.Fatalf("node after re-migration = (%q, %v), want (Login, NULL signature)", name, signature)
	}
}

// TestCovLazyMigrateAddsEachMissingColumn walks one legacy database per added
// column: the other three columns are already there (so the probe's
// already-present path is taken for them) and the fourth must be appended
// without duplicating anything or losing the pre-existing rows.
func TestCovLazyMigrateAddsEachMissingColumn(t *testing.T) {
	t.Parallel()
	for _, c := range codeGraphAddedColumns {
		column := c
		t.Run(column.table+"."+column.column, func(t *testing.T) {
			t.Parallel()
			covLazyCheckLegacyColumnAdded(t, column.table, column.column)
		})
	}
}

func covLazyCheckLegacyColumnAdded(t *testing.T, table, column string) {
	t.Helper()
	scripts := append([]string{schemaSQL}, covLazyAddedColumnDDL(column)...)
	scripts = append(scripts, covLazySeedNodeSQL, covLazySeedEdgeSQL)
	s, db := covLazyStoreOnDDL(t, scripts...)

	if got := covLazyColumnCounts(t, db, table)[column]; got != 0 {
		t.Fatalf("legacy fixture already has %s.%s (%d occurrences)", table, column, got)
	}
	if err := s.migrateCodeGraphSchema(); err != nil {
		t.Fatalf("migrate legacy database missing %s.%s: %v", table, column, err)
	}
	covLazyAssertV9Shape(t, db)

	var qualified string
	if err := db.QueryRow("SELECT qualified_name FROM nodes").Scan(&qualified); err != nil {
		t.Fatalf("pre-migration node lost: %v", err)
	}
	if qualified != "auth::Login" {
		t.Fatalf("pre-migration node = %q, want auth::Login", qualified)
	}
}

// TestCovLazyMigrateProbeFailureIsReported proves a failing schema probe aborts
// the migration with the table and column that could not be probed, rather
// than treating the failure as "column absent" and issuing a blind ALTER.
func TestCovLazyMigrateProbeFailureIsReported(t *testing.T) {
	t.Parallel()
	s, db := covLazyStoreOnDDL(t, schemaSQL)
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	err := s.migrateCodeGraphSchema()
	covLazyAssertErrorContains(t, err, "probe nodes.signature", "database is closed")
}

// TestCovLazyMigrateAddColumnFailureIsReported proves a rejected ALTER surfaces
// as an add failure naming the column: on a database with no nodes table the
// probe truthfully reports the column absent and the ALTER cannot succeed.
func TestCovLazyMigrateAddColumnFailureIsReported(t *testing.T) {
	t.Parallel()
	s, _ := covLazyStoreOnDDL(t, `CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)

	err := s.migrateCodeGraphSchema()
	covLazyAssertErrorContains(t, err, "add nodes.signature", "no such table: nodes")
}

// TestCovLazyMigrateFTSFailureIsReported proves a failure creating nodes_fts is
// reported instead of being swallowed by the IF NOT EXISTS: the name is taken
// by an index, which SQLite refuses to shadow with a virtual table.
func TestCovLazyMigrateFTSFailureIsReported(t *testing.T) {
	t.Parallel()
	scripts := append(covLazyMigratedDDL(), `CREATE INDEX nodes_fts ON nodes(name)`)
	s, _ := covLazyStoreOnDDL(t, scripts...)

	err := s.migrateCodeGraphSchema()
	covLazyAssertErrorContains(t, err, "create nodes_fts", "already an index named nodes_fts")
}

// TestCovLazyMigratePostIndexFailureIsReported proves the post-migration index
// step reports its own failure: nodes_fts is created fine, then
// idx_nodes_community collides with an existing table of that name.
func TestCovLazyMigratePostIndexFailureIsReported(t *testing.T) {
	t.Parallel()
	scripts := append(covLazyMigratedDDL(), `CREATE TABLE idx_nodes_community (x INTEGER)`)
	s, db := covLazyStoreOnDDL(t, scripts...)

	err := s.migrateCodeGraphSchema()
	covLazyAssertErrorContains(t, err, "create post-migration indexes", "already a table named idx_nodes_community")
	if got := covLazyObjectType(t, db, "nodes_fts"); got != "table" {
		t.Fatalf("nodes_fts is %q, want the FTS table created before the failing index step", got)
	}
}

// covLazyAssertErrorContains asserts err is non-nil and its message names both
// the migration step that failed and the underlying SQLite cause.
func covLazyAssertErrorContains(t *testing.T, err error, step, cause string) {
	t.Helper()
	if err == nil {
		t.Fatalf("migrate succeeded, want failure at %q", step)
	}
	for _, want := range []string{step, cause} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("migrate error %q does not mention %q", err, want)
		}
	}
}

// TestCovLazyHasColumn pins the probe's three answers: present, absent, and
// unprobeable. A table that does not exist is "absent", not an error — that is
// what lets the migration run on a database it did not create.
func TestCovLazyHasColumn(t *testing.T) {
	t.Parallel()
	_, db := covLazyStoreOnDDL(t, schemaSQL, codeGraphAddedColumns[0].ddl)

	cases := []struct {
		name   string
		table  string
		column string
		want   bool
	}{
		{"present", "nodes", "signature", true},
		{"base column", "nodes", "qualified_name", true},
		{"absent", "nodes", "community_id", false},
		{"table does not exist", "no_such_table", "signature", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := hasColumn(db, tc.table, tc.column)
			if err != nil {
				t.Fatalf("hasColumn(%s.%s): %v", tc.table, tc.column, err)
			}
			if got != tc.want {
				t.Fatalf("hasColumn(%s.%s) = %v, want %v", tc.table, tc.column, got, tc.want)
			}
		})
	}
}

// TestCovLazyHasColumnQueryFailure proves an unprobeable handle yields an error
// rather than a false "column absent", which would send the caller into a
// doomed ALTER.
func TestCovLazyHasColumnQueryFailure(t *testing.T) {
	t.Parallel()
	_, db := covLazyStoreOnDDL(t, schemaSQL)
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	got, err := hasColumn(db, "nodes", "signature")
	if err == nil {
		t.Fatal("hasColumn on a closed handle returned no error")
	}
	if got {
		t.Fatal("hasColumn reported the column present despite the query failing")
	}
	if !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("hasColumn error %q does not name the closed handle", err)
	}
}
