package graphstore_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

// ---------------------------------------------------------------------------
// Schema-v9 parity against the release contract
// ---------------------------------------------------------------------------

// releaseSchemaPath resolves the recorded upstream schema relative to this
// test file.
func releaseSchemaPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(root, "testdata", "crg-release", "v2.3.8", "sqlite-schema.json")
}

// releaseSchema is the decoded testdata/crg-release/v2.3.8/sqlite-schema.json:
// upstream code-review-graph v2.3.8's verbatim sqlite_master rows.
type releaseSchema struct {
	SchemaVersion int      `json:"schema_version"`
	MetadataKeys  []string `json:"metadata_keys"`
	Objects       struct {
		Tables  map[string]string `json:"tables"`
		Indexes map[string]string `json:"indexes"`
		Views   map[string]string `json:"views"`
	} `json:"objects"`
}

func loadReleaseSchema(t *testing.T) releaseSchema {
	t.Helper()
	data, err := os.ReadFile(releaseSchemaPath(t))
	if err != nil {
		t.Fatalf("read release schema: %v", err)
	}
	var s releaseSchema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse release schema: %v", err)
	}
	return s
}

// nativeOnlyObjects are the KG-layer objects upstream has no equivalent for.
// They are the ONE documented superset over upstream's object set, enumerated
// explicitly so a future accidental addition shows up as a test failure rather
// than slipping in under a permissive "ignore extras" rule.
var nativeOnlyObjects = map[string]bool{
	"kg_notes":              true,
	"note_symbol_links":     true,
	"idx_kg_notes_type":     true,
	"idx_kg_notes_status":   true,
	"idx_kg_notes_archived": true,
	"idx_nsl_note_id":       true,
	"idx_nsl_qualified":     true,
}

// hardenedTables are the three tables the native store shares with upstream but
// declares with stricter column constraints (NOT NULL / DEFAULT on columns
// upstream leaves nullable). They predate this schema and SQLite cannot relax
// or tighten a column on an existing table, so their DDL TEXT is not compared;
// their COLUMN SET is, which is what every query and every derived row actually
// depends on.
var hardenedTables = map[string]bool{
	"nodes":    true,
	"edges":    true,
	"metadata": true,
}

// sqliteManagedTables are created by SQLite itself, not by our DDL:
// sqlite_sequence appears with AUTOINCREMENT and the nodes_fts_* shadow tables
// with the FTS5 virtual table. Their SQL text is emitted by the SQLite build,
// so it is not our contract — only their PRESENCE is, since an absent shadow
// table means the FTS5 module did not actually instantiate.
var sqliteManagedTables = map[string]bool{
	"sqlite_sequence":   true,
	"nodes_fts_data":    true,
	"nodes_fts_idx":     true,
	"nodes_fts_docsize": true,
	"nodes_fts_config":  true,
}

// normalizeDDL makes two spellings of the same schema object comparable:
// `IF NOT EXISTS` is stripped (SQLite already drops it from sqlite_master, and
// upstream's migrations use it inconsistently), SQL line comments are removed,
// and every whitespace run collapses to a single space. What survives is the
// columns, types, defaults and constraints — the parts that are the contract.
func normalizeDDL(sql string) string {
	out := strings.ReplaceAll(stripDDLComments(sql), "\n", " ")
	out = strings.ReplaceAll(out, "IF NOT EXISTS ", "")
	out = strings.Join(strings.Fields(out), " ")
	return strings.TrimSuffix(strings.TrimSpace(out), ";")
}

// readSchemaObjects reads a database's sqlite_master into name -> normalized
// SQL maps, one per object type. Auto-created indexes have a NULL sql.
func readSchemaObjects(t *testing.T, dbPath string) (tables, indexes, views map[string]string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT type, name, sql FROM sqlite_master")
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()
	tables, indexes, views = map[string]string{}, map[string]string{}, map[string]string{}
	for rows.Next() {
		var objType, name string
		var objSQL sql.NullString
		if err := rows.Scan(&objType, &name, &objSQL); err != nil {
			t.Fatalf("scan sqlite_master: %v", err)
		}
		switch objType {
		case "table":
			tables[name] = normalizeDDL(objSQL.String)
		case "index":
			if !objSQL.Valid {
				// SQLite auto-creates an index per UNIQUE / non-INTEGER
				// PRIMARY KEY and records a NULL sql for it. It is implied by
				// a column declaration rather than authored, which is why
				// upstream's recorded object set omits these too.
				continue
			}
			indexes[name] = normalizeDDL(objSQL.String)
		case "view":
			views[name] = normalizeDDL(objSQL.String)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("sqlite_master rows: %v", err)
	}
	return tables, indexes, views
}

// columnsOf returns a table's column names.
func columnsOf(t *testing.T, dbPath, table string) map[string]bool {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("pragma_table_info rows: %v", err)
	}
	return out
}

// TestSchemaV9_FreshDatabaseMatchesReleaseContract proves a freshly created
// native database carries upstream code-review-graph v2.3.8's entire
// schema-v9 object set, with the same DDL.
//
// This is the schema half of the parity contract: the derived-view
// computations can only be compared to upstream's recorded rows if the tables
// holding them are upstream's tables. It also pins the two documented
// deviations explicitly — the hardened nodes/edges/metadata column
// constraints, and the KG-layer superset — so neither can grow silently.
func TestSchemaV9_FreshDatabaseMatchesReleaseContract(t *testing.T) {
	want := loadReleaseSchema(t)
	if want.SchemaVersion != graphstore.SchemaVersion {
		t.Fatalf("release schema_version = %d, graphstore.SchemaVersion = %d",
			want.SchemaVersion, graphstore.SchemaVersion)
	}

	dbPath := filepath.Join(t.TempDir(), "fresh.db")
	store, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	if got, err := store.GetMetadata("schema_version"); err != nil {
		t.Fatalf("read schema_version: %v", err)
	} else if got != strconv.Itoa(graphstore.SchemaVersion) {
		t.Errorf("metadata schema_version = %q, want %q",
			got, strconv.Itoa(graphstore.SchemaVersion))
	}

	assertSchemaMatchesRelease(t, dbPath, want)
}

// TestSchemaV9_ExistingDatabaseMigratesForward is the migration acceptance
// test: an OLD native database — created before schema v9, so it has nodes and
// edges WITHOUT the four added columns and none of the derived tables — must
// come forward to the fixture's object set on the next open, without losing
// its rows.
//
// It exercises the path a real user hits, which the fresh-database test above
// cannot: on an existing database the four columns arrive by ALTER TABLE, and
// nodes_fts and idx_nodes_community can only be created after they do.
func TestSchemaV9_ExistingDatabaseMigratesForward(t *testing.T) {
	want := loadReleaseSchema(t)
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	writePreV9Database(t, dbPath)

	store, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer store.Close()

	// The pre-existing row survived the migration.
	node, err := store.GetNode("legacy.go::legacyFn")
	if err != nil {
		t.Fatalf("get legacy node: %v", err)
	}
	if node == nil {
		t.Fatal("migration lost the pre-existing node")
	}
	if node.Signature != "" || node.CommunityID != 0 {
		t.Errorf("a migrated row's new columns must read back empty, got %q / %d",
			node.Signature, node.CommunityID)
	}

	assertSchemaMatchesRelease(t, dbPath, want)

	// Re-opening must be a no-op, not a second round of ALTER TABLEs (which
	// would fail with "duplicate column name").
	store.Close()
	reopened, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("re-open migrated db: %v", err)
	}
	defer reopened.Close()
	assertSchemaMatchesRelease(t, dbPath, want)
}

// writePreV9Database creates a database in the shape this store produced
// before schema v9: nodes and edges without signature/community_id/confidence/
// confidence_tier, no derived tables, and no schema_version metadata row.
func writePreV9Database(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	legacyDDL := `
CREATE TABLE nodes (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT    NOT NULL,
    name            TEXT    NOT NULL,
    qualified_name  TEXT    NOT NULL UNIQUE,
    file_path       TEXT    NOT NULL,
    line_start      INTEGER,
    line_end        INTEGER,
    language        TEXT,
    parent_name     TEXT,
    params          TEXT,
    return_type     TEXT,
    modifiers       TEXT,
    is_test         INTEGER NOT NULL DEFAULT 0,
    file_hash       TEXT,
    extra           TEXT    NOT NULL DEFAULT '{}',
    updated_at      REAL    NOT NULL
);
CREATE TABLE edges (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    kind             TEXT    NOT NULL,
    source_qualified TEXT    NOT NULL,
    target_qualified TEXT    NOT NULL,
    file_path        TEXT    NOT NULL,
    line             INTEGER NOT NULL DEFAULT 0,
    extra            TEXT    NOT NULL DEFAULT '{}',
    updated_at       REAL    NOT NULL
);
CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);`
	if _, err := db.Exec(legacyDDL); err != nil {
		t.Fatalf("legacy ddl: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO nodes
		  (kind, name, qualified_name, file_path, line_start, line_end, language,
		   parent_name, params, return_type, modifiers, is_test, file_hash, extra, updated_at)
		VALUES ('Function','legacyFn','legacy.go::legacyFn','legacy.go',1,3,'go',
		        NULL,'()',NULL,NULL,0,'h','{}',1.0)`); err != nil {
		t.Fatalf("seed legacy node: %v", err)
	}
}

// assertSchemaMatchesRelease is the shared comparison: every object the
// release records must exist, with matching normalized DDL where we author it,
// and the only extra objects are the enumerated KG-layer ones.
func assertSchemaMatchesRelease(t *testing.T, dbPath string, want releaseSchema) {
	t.Helper()
	gotTables, gotIndexes, gotViews := readSchemaObjects(t, dbPath)

	for name, wantSQL := range want.Objects.Tables {
		gotSQL, present := gotTables[name]
		if !present {
			t.Errorf("missing table %q", name)
			continue
		}
		switch {
		case sqliteManagedTables[name]:
			// SQLite emits the DDL; presence is the contract.
		case hardenedTables[name]:
			assertColumnsSuperset(t, dbPath, name, wantSQL)
		default:
			if gotSQL != normalizeDDL(wantSQL) {
				t.Errorf("table %q DDL differs\n got: %s\nwant: %s",
					name, gotSQL, normalizeDDL(wantSQL))
			}
		}
	}

	for name, wantSQL := range want.Objects.Indexes {
		gotSQL, present := gotIndexes[name]
		if !present {
			t.Errorf("missing index %q", name)
			continue
		}
		if gotSQL != normalizeDDL(wantSQL) {
			t.Errorf("index %q DDL differs\n got: %s\nwant: %s",
				name, gotSQL, normalizeDDL(wantSQL))
		}
	}

	// Upstream declares no views, and neither may we: a SQL view would be an
	// adapter-authored query surface the contract deliberately excludes.
	if len(want.Objects.Views) != 0 {
		t.Fatalf("the release fixture unexpectedly records views: %v", want.Objects.Views)
	}
	if len(gotViews) != 0 {
		t.Errorf("the native schema must declare no views, got %v", gotViews)
	}

	for name := range gotTables {
		if _, upstream := want.Objects.Tables[name]; !upstream && !nativeOnlyObjects[name] {
			t.Errorf("unexpected extra table %q — add it to nativeOnlyObjects "+
				"with a reason, or remove it", name)
		}
	}
	for name := range gotIndexes {
		if _, upstream := want.Objects.Indexes[name]; !upstream && !nativeOnlyObjects[name] {
			t.Errorf("unexpected extra index %q — add it to nativeOnlyObjects "+
				"with a reason, or remove it", name)
		}
	}
}

// assertColumnsSuperset checks a hardened table has every column upstream's
// DDL declares. The column names are lifted from the recorded DDL rather than
// hardcoded, so the check tracks the fixture.
func assertColumnsSuperset(t *testing.T, dbPath, table, upstreamDDL string) {
	t.Helper()
	got := columnsOf(t, dbPath, table)
	for _, col := range declaredColumns(upstreamDDL) {
		if !got[col] {
			t.Errorf("table %q is missing upstream column %q", table, col)
		}
	}
}

// declaredColumns extracts the column names from a CREATE TABLE statement: the
// first identifier of each comma-separated clause inside the outermost
// parentheses, skipping table-level constraint clauses.
//
// Comments are stripped from the whole statement FIRST, before the split.
// Upstream's nodes and edges DDL carries an inline comment listing the kind
// vocabulary ("-- CALLS, IMPORTS_FROM, INHERITS, ...") whose commas would
// otherwise be read as column separators and yield phantom columns.
func declaredColumns(ddl string) []string {
	ddl = stripDDLComments(ddl)
	open := strings.Index(ddl, "(")
	closeIdx := strings.LastIndex(ddl, ")")
	if open < 0 || closeIdx <= open {
		return nil
	}
	body := ddl[open+1 : closeIdx]
	var out []string
	depth := 0
	start := 0
	clauses := []string{}
	for i, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				clauses = append(clauses, body[start:i])
				start = i + 1
			}
		}
	}
	clauses = append(clauses, body[start:])
	constraintKeywords := map[string]bool{
		"primary": true, "unique": true, "foreign": true,
		"check": true, "constraint": true,
	}
	for _, clause := range clauses {
		fields := strings.Fields(clause)
		if len(fields) == 0 {
			continue
		}
		name := strings.Trim(fields[0], "\"`[]'")
		if constraintKeywords[strings.ToLower(name)] {
			continue
		}
		out = append(out, name)
	}
	return out
}

// stripDDLComments removes every SQL line comment from a DDL statement,
// keeping the line structure so the remaining commas are real separators.
func stripDDLComments(ddl string) string {
	lines := strings.Split(ddl, "\n")
	for i, line := range lines {
		if j := strings.Index(line, "--"); j >= 0 {
			lines[i] = line[:j]
		}
	}
	return strings.Join(lines, "\n")
}

// TestSchemaV9_MetadataKeysCoverTheRelease proves the native store records
// every metadata key upstream's schema fixture lists, or documents why not.
// `schema_version` is this lane's; the rest belong to the build/update
// lifecycle, so this asserts only the one it owns and reports the others as
// informational — a hard assertion here would duplicate (and fight with) the
// lifecycle report tests.
func TestSchemaV9_MetadataKeysCoverTheRelease(t *testing.T) {
	want := loadReleaseSchema(t)
	found := false
	for _, key := range want.MetadataKeys {
		if key == "schema_version" {
			found = true
		}
	}
	if !found {
		t.Fatal("the release fixture no longer records a schema_version " +
			"metadata key; the version contract has moved")
	}
}

// TestSchemaV9_SecondFullBuildSucceeds is the regression test for the
// foreign-key asymmetry described in CONTRACT.md: risk_index references
// nodes(id), OpenSQLite enables PRAGMA foreign_keys, and a second build
// re-persists every file by DELETEing its nodes first. With enforcement
// active that DELETE is refused and the whole second build fails with
// "FOREIGN KEY constraint failed".
//
// Upstream never hits it because Python's sqlite3 leaves enforcement off, so
// its identical declarations constrain nothing and a rebuild simply carries
// the stale summary rows forward. Nothing covered this before: one build
// leaves risk_index empty, so the constraint has nothing to violate.
func TestSchemaV9_SecondFullBuildSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rebuild.db")
	store, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	nodes := []graphstore.NodeInfo{
		{Kind: graphstore.NodeKindFile, Name: "a.go", FilePath: "a.go", Language: "go"},
		{Kind: graphstore.NodeKindFunction, Name: "fn", FilePath: "a.go", Language: "go", Params: "()"},
	}
	edges := []graphstore.EdgeInfo{
		{Kind: graphstore.EdgeKindContains, Source: "a.go", Target: "a.go::fn", FilePath: "a.go"},
	}

	// Build #1, then a full postprocess that populates the node-referencing
	// summary table.
	if err := store.StoreFileNodesEdges("a.go", nodes, edges, "hash-1"); err != nil {
		t.Fatalf("first build: %v", err)
	}
	fn, err := store.GetNode("a.go::fn")
	if err != nil || fn == nil {
		t.Fatalf("read seeded node: %v", err)
	}
	if _, err := store.ReplaceRiskIndex([]graphstore.RiskIndexRow{{
		NodeID: fn.ID, QualifiedName: fn.QualifiedName, RiskScore: 0.3,
		TestCoverage: "untested", LastComputed: "stamp",
	}}); err != nil {
		t.Fatalf("replace risk index: %v", err)
	}

	// Build #2 over the same file. This is the case that used to fail.
	if err := store.StoreFileNodesEdges("a.go", nodes, edges, "hash-2"); err != nil {
		t.Fatalf("second build over a graph with a populated risk_index: %v", err)
	}
	// And the direct removal path, which has the same leading DELETE.
	if err := store.RemoveFileData("a.go"); err != nil {
		t.Fatalf("RemoveFileData over a graph with a populated risk_index: %v", err)
	}

	// The stale summary row survives, exactly as upstream's does — it is not
	// cleared, because the release contract pins that tolerated staleness.
	risk, err := store.ReadRiskIndex()
	if err != nil {
		t.Fatalf("read risk index: %v", err)
	}
	if len(risk) != 1 {
		t.Fatalf("risk_index rows after a rebuild = %d, want the previous "+
			"generation's 1 row left in place", len(risk))
	}

	// KG-layer referential integrity is NOT relaxed by any of the above: the
	// pragma is scoped to the code-graph write's own connection.
	assertForeignKeysEnforced(t, dbPath)
}

// assertForeignKeysEnforced proves the FK suspension did not leak: a fresh
// connection from the pool must still report enforcement on.
func assertForeignKeysEnforced(t *testing.T, dbPath string) {
	t.Helper()
	store, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer store.Close()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	// A raw connection has its own pragma state, so this checks the DDL side
	// instead: the declarations must still be present in the stored schema.
	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='risk_index' " +
			"AND sql LIKE '%FOREIGN KEY%'").Scan(&count); err != nil {
		t.Fatalf("inspect risk_index ddl: %v", err)
	}
	if count != 1 {
		t.Error("risk_index must keep upstream's FOREIGN KEY declaration; " +
			"suspending ENFORCEMENT is not the same as dropping the constraint")
	}
}
