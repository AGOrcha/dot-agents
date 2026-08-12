package graphstore

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// graphNodeRow / graphEdgeRow are the minimal row shapes the merge moves.
type graphNodeRow struct {
	qualified string
	name      string
	filePath  string
}

type graphEdgeRow struct {
	source   string
	target   string
	filePath string
}

// seedGraphDB creates a CRG-shaped graph database at path holding nodes and
// edges. The schema mirrors the real one (qualified_name unique, autoincrement
// ids) because both properties are what make a naive merge misbehave.
func seedGraphDB(t *testing.T, path string, nodes []graphNodeRow, edges []graphEdgeRow) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db := openTestDB(t, path)
	defer db.Close()
	ddl := `
		CREATE TABLE nodes (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
		  file_path TEXT, line_start INTEGER, language TEXT, updated_at REAL
		);
		CREATE TABLE edges (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, source_qualified TEXT, target_qualified TEXT,
		  file_path TEXT, line INTEGER, updated_at REAL
		);`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	for _, n := range nodes {
		if _, err := db.Exec(
			`INSERT INTO nodes (kind,name,qualified_name,file_path,line_start,language,updated_at)
			 VALUES ('Function',?,?,?,1,'go',1.0)`, n.name, n.qualified, n.filePath); err != nil {
			t.Fatalf("insert node %s: %v", n.qualified, err)
		}
	}
	for _, e := range edges {
		if _, err := db.Exec(
			`INSERT INTO edges (kind,source_qualified,target_qualified,file_path,line,updated_at)
			 VALUES ('CALLS',?,?,?,1,1.0)`, e.source, e.target, e.filePath); err != nil {
			t.Fatalf("insert edge %s->%s: %v", e.source, e.target, err)
		}
	}
}

// openTestDB opens a SQLite database for a test.
func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	return db
}

// mergeInto runs a merge against dstPath the way production does: open the
// destination graph, merge, close.
func mergeInto(t *testing.T, dstPath, srcPath, scope string) (MergeStats, error) {
	t.Helper()
	db := openTestDB(t, dstPath)
	defer db.Close()
	return MergeGraphDB(db, srcPath, scope)
}

// twoRepoFixture seeds a superproject graph and a submodule graph that both
// define a symbol called Button — the collision that produced the reported
// cross-repo false edges (a manager-ui file's impact radius surfacing
// client-ui symbols).
func twoRepoFixture(t *testing.T) (dst, src string) {
	t.Helper()
	dir := t.TempDir()
	dst = filepath.Join(dir, "super", "graph.db")
	src = filepath.Join(dir, "sub", "graph.db")
	seedGraphDB(t, dst,
		[]graphNodeRow{
			{qualified: "Button", name: "Button", filePath: "/repo/super/ui/Button.tsx"},
			{qualified: "renderNav", name: "renderNav", filePath: "/repo/super/ui/Nav.tsx"},
		},
		[]graphEdgeRow{{source: "renderNav", target: "Button", filePath: "/repo/super/ui/Nav.tsx"}},
	)
	seedGraphDB(t, src,
		[]graphNodeRow{
			{qualified: "Button", name: "Button", filePath: "/repo/sub/widgets/Button.tsx"},
			{qualified: "FullCheck", name: "FullCheck", filePath: "/repo/sub/checks/Full.tsx"},
		},
		[]graphEdgeRow{{source: "FullCheck", target: "Button", filePath: "/repo/sub/checks/Full.tsx"}},
	)
	return dst, src
}

// resolvedEdges returns "source@sourceFile -> target@targetFile" for every
// edge, resolving each endpoint to the node its qualified name matches. This
// is how CRG resolves edges, so it is how a false edge becomes visible.
func resolvedEdges(t *testing.T, dbPath string) []string {
	t.Helper()
	db := openTestDB(t, dbPath)
	defer db.Close()
	rows, err := db.Query(`SELECT e.source_qualified, COALESCE(s.file_path,'?'),
		e.target_qualified, COALESCE(d.file_path,'?')
		FROM edges e
		LEFT JOIN nodes s ON s.qualified_name = e.source_qualified
		LEFT JOIN nodes d ON d.qualified_name = e.target_qualified
		ORDER BY e.source_qualified, e.target_qualified`)
	if err != nil {
		t.Fatalf("query edges: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sq, sf, tq, tf string
		if err := rows.Scan(&sq, &sf, &tq, &tf); err != nil {
			t.Fatal(err)
		}
		out = append(out, sq+"@"+sf+" -> "+tq+"@"+tf)
	}
	return out
}

// crossRepoEdges returns the resolved edges whose two endpoints live in
// different repositories — edges no single-repo build could ever produce.
func crossRepoEdges(t *testing.T, dbPath string) []string {
	t.Helper()
	var bad []string
	for _, e := range resolvedEdges(t, dbPath) {
		parts := strings.Split(e, " -> ")
		if inSubRepo(parts[0]) != inSubRepo(parts[1]) {
			bad = append(bad, e)
		}
	}
	return bad
}

func inSubRepo(endpoint string) bool { return strings.Contains(endpoint, "/repo/sub/") }

// naiveMerge is the hand-rolled aggregation the proposal recorded: copy rows
// straight across with no repository discriminator. It exists in this test
// only to demonstrate the failure the scoped merge prevents.
func naiveMerge(t *testing.T, dst, src string) {
	t.Helper()
	db := openTestDB(t, dst)
	defer db.Close()
	if _, err := db.Exec(`ATTACH DATABASE ? AS src`, src); err != nil {
		t.Fatalf("attach: %v", err)
	}
	stmts := []string{
		`INSERT OR IGNORE INTO nodes (kind,name,qualified_name,file_path,line_start,language,updated_at)
		 SELECT kind,name,qualified_name,file_path,line_start,language,updated_at FROM src.nodes`,
		`INSERT OR IGNORE INTO edges (kind,source_qualified,target_qualified,file_path,line,updated_at)
		 SELECT kind,source_qualified,target_qualified,file_path,line,updated_at FROM src.edges`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("naive merge: %v", err)
		}
	}
}

// TestNaiveMergeProducesCrossRepoFalseEdge pins the defect: an unscoped merge
// drops the submodule's Button on the unique index and re-points the
// submodule's edge at the SUPERPROJECT's Button, inventing a dependency
// between two repositories that never reference each other.
func TestNaiveMergeProducesCrossRepoFalseEdge(t *testing.T) {
	dst, src := twoRepoFixture(t)

	naiveMerge(t, dst, src)

	bad := crossRepoEdges(t, dst)
	if len(bad) == 0 {
		t.Fatalf("expected the unscoped merge to fabricate a cross-repo edge, got %v", resolvedEdges(t, dst))
	}
	if !strings.Contains(bad[0], "FullCheck") {
		t.Errorf("expected the false edge to originate at FullCheck, got %v", bad)
	}
}

// TestMergeGraphDB_ScopingPreventsCrossRepoEdges is the fix: the same two
// graphs merged under a repository scope keep both Buttons distinct and every
// edge inside its own repository.
func TestMergeGraphDB_ScopingPreventsCrossRepoEdges(t *testing.T) {
	dst, src := twoRepoFixture(t)

	stats, err := mergeInto(t, dst, src, "vendor/client-ui")
	if err != nil {
		t.Fatalf("MergeGraphDB: %v", err)
	}
	if stats.Nodes != 2 || stats.Edges != 1 {
		t.Errorf("merge stats = %+v, want 2 nodes / 1 edge", stats)
	}
	if bad := crossRepoEdges(t, dst); len(bad) != 0 {
		t.Errorf("scoped merge produced cross-repo edges: %v", bad)
	}
	edges := resolvedEdges(t, dst)
	want := []string{
		"renderNav@/repo/super/ui/Nav.tsx -> Button@/repo/super/ui/Button.tsx",
		"vendor/client-ui::FullCheck@/repo/sub/checks/Full.tsx -> vendor/client-ui::Button@/repo/sub/widgets/Button.tsx",
	}
	if strings.Join(edges, "\n") != strings.Join(want, "\n") {
		t.Errorf("resolved edges =\n%s\nwant\n%s", strings.Join(edges, "\n"), strings.Join(want, "\n"))
	}
	if n := countRows(t, dst, "nodes"); n != 4 {
		t.Errorf("expected 4 nodes after the merge (both Buttons kept), got %d", n)
	}
}

// TestMergeGraphDB_Idempotent: re-merging the same submodule inserts nothing
// new, so a rebuilt workspace does not accumulate duplicates.
func TestMergeGraphDB_Idempotent(t *testing.T) {
	dst, src := twoRepoFixture(t)

	if _, err := mergeInto(t, dst, src, "vendor/lib"); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	second, err := mergeInto(t, dst, src, "vendor/lib")
	if err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if second.Nodes != 0 {
		t.Errorf("second merge inserted %d nodes, want 0", second.Nodes)
	}
	if n := countRows(t, dst, "nodes"); n != 4 {
		t.Errorf("node count after re-merge = %d, want 4", n)
	}
}

// TestMergeGraphDB_RelativeFilePathsAreRebased: CRG writes absolute paths
// today, but a relative path from a submodule graph would point at a file that
// does not exist relative to the superproject unless it is rebased.
func TestMergeGraphDB_RelativeFilePathsAreRebased(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "super.db")
	src := filepath.Join(dir, "sub.db")
	seedGraphDB(t, dst, []graphNodeRow{{qualified: "main", name: "main", filePath: "main.go"}}, nil)
	seedGraphDB(t, src, []graphNodeRow{
		{qualified: "Widget", name: "Widget", filePath: "widget.go"},
		{qualified: "PosixAbs", name: "PosixAbs", filePath: "/abs/widget.go"},
		{qualified: "WindowsAbs", name: "WindowsAbs", filePath: `C:\abs\widget.go`},
		{qualified: "UNCAbs", name: "UNCAbs", filePath: `\\host\share\widget.go`},
	}, nil)

	if _, err := mergeInto(t, dst, src, "vendor/lib"); err != nil {
		t.Fatalf("MergeGraphDB: %v", err)
	}
	paths := map[string]string{
		"vendor/lib::Widget":     "vendor/lib/widget.go",
		"vendor/lib::PosixAbs":   "/abs/widget.go",
		"vendor/lib::WindowsAbs": `C:\abs\widget.go`,
		"vendor/lib::UNCAbs":     `\\host\share\widget.go`,
		"main":                   "main.go",
	}
	for qualified, want := range paths {
		if got := nodeFilePath(t, dst, qualified); got != want {
			t.Errorf("%s file_path = %q, want %q", qualified, got, want)
		}
	}
}

// TestMergeGraphDB_SchemaDrift: a submodule graph written by a different CRG
// version (an extra column here) merges on the columns the two schemas share
// rather than failing outright.
func TestMergeGraphDB_SchemaDrift(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "super.db")
	src := filepath.Join(dir, "sub.db")
	seedGraphDB(t, dst, []graphNodeRow{{qualified: "main", name: "main", filePath: "/x/main.go"}}, nil)
	seedGraphDB(t, src, []graphNodeRow{{qualified: "Widget", name: "Widget", filePath: "/y/widget.go"}}, nil)
	srcDB := openTestDB(t, src)
	if _, err := srcDB.Exec(`ALTER TABLE nodes ADD COLUMN experimental_score REAL`); err != nil {
		t.Fatalf("alter: %v", err)
	}
	srcDB.Close()

	stats, err := mergeInto(t, dst, src, "vendor/lib")
	if err != nil {
		t.Fatalf("MergeGraphDB across drifted schemas: %v", err)
	}
	if stats.Nodes != 1 {
		t.Errorf("stats = %+v, want 1 node merged", stats)
	}
	if got := nodeFilePath(t, dst, "vendor/lib::Widget"); got != "/y/widget.go" {
		t.Errorf("drifted merge lost the row: %q", got)
	}
}

// TestMergeGraphDB_EmptyScopeRejected: an unscoped merge is the defect, so the
// API refuses it rather than silently reproducing it.
func TestMergeGraphDB_EmptyScopeRejected(t *testing.T) {
	dst, src := twoRepoFixture(t)
	if _, err := mergeInto(t, dst, src, ""); err == nil {
		t.Fatal("expected an empty scope to be rejected")
	}
}

// TestMergeGraphDB_ClosedDestination surfaces an unusable destination handle
// instead of reporting a merge that never happened.
func TestMergeGraphDB_ClosedDestination(t *testing.T) {
	dst, src := twoRepoFixture(t)
	db := openTestDB(t, dst)
	db.Close()

	if _, err := MergeGraphDB(db, src, "vendor/lib"); err == nil {
		t.Fatal("expected an error against a closed destination")
	}
}

// TestMergeGraphDB_UnattachableSource: a source that is not a readable
// database (here a directory) fails the merge loudly.
func TestMergeGraphDB_UnattachableSource(t *testing.T) {
	dst, _ := twoRepoFixture(t)
	if _, err := mergeInto(t, dst, t.TempDir(), "vendor/lib"); err == nil {
		t.Fatal("expected an error for an unattachable source graph")
	}
}

// TestMergeGraphDB_ReadOnlyDestination: the write lock is taken up front, so a
// destination that cannot be written fails before any rows move.
func TestMergeGraphDB_ReadOnlyDestination(t *testing.T) {
	dst, src := twoRepoFixture(t)
	// query_only makes every write on this handle fail — the portable stand-in
	// for a read-only database file.
	db := openTestDB(t, dst+"?_pragma=query_only(true)")
	defer db.Close()

	if _, err := MergeGraphDB(db, src, "vendor/lib"); err == nil {
		t.Fatal("expected an error against a read-only destination")
	}
	if n := countRows(t, dst, "nodes"); n != 2 {
		t.Errorf("a failed merge must not leave rows behind, got %d nodes", n)
	}
}

// TestMergeGraphDB_MissingSourceTable: a source graph with no nodes table is a
// merge that would silently move nothing, so it is an error.
func TestMergeGraphDB_MissingSourceTable(t *testing.T) {
	dst, _ := twoRepoFixture(t)
	src := filepath.Join(t.TempDir(), "empty.db")
	db := openTestDB(t, src)
	if _, err := db.Exec(`CREATE TABLE placeholder (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err := mergeInto(t, dst, src, "vendor/lib")
	if err == nil || !strings.Contains(err.Error(), "source nodes schema") {
		t.Fatalf("expected a source-schema error, got %v", err)
	}
}

// TestMergeGraphDB_MissingDestinationTable errors rather than half-merging.
func TestMergeGraphDB_MissingDestinationTable(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "empty.db")
	src := filepath.Join(dir, "sub.db")
	db := openTestDB(t, dst)
	if _, err := db.Exec(`CREATE TABLE placeholder (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	seedGraphDB(t, src, []graphNodeRow{{qualified: "W", name: "W", filePath: "/y/w.go"}}, nil)

	_, err := mergeInto(t, dst, src, "vendor/lib")
	if err == nil || !strings.Contains(err.Error(), "destination nodes schema") {
		t.Fatalf("expected a destination-schema error, got %v", err)
	}
}

// TestMergeGraphDB_NoSharedColumns: two schemas with nothing in common cannot
// be merged, and saying "0 rows copied" would hide a lost repository.
func TestMergeGraphDB_NoSharedColumns(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.db")
	src := filepath.Join(dir, "src.db")
	db := openTestDB(t, dst)
	if _, err := db.Exec(`CREATE TABLE nodes (id INTEGER PRIMARY KEY); CREATE TABLE edges (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	seedGraphDB(t, src, []graphNodeRow{{qualified: "W", name: "W", filePath: "/y/w.go"}}, nil)

	_, err := mergeInto(t, dst, src, "vendor/lib")
	if err == nil || !strings.Contains(err.Error(), "share no columns") {
		t.Fatalf("expected a shared-column error, got %v", err)
	}
}

// TestMergeGraphDB_RowInsertFailureRollsBack: a destination that cannot accept
// the rows (here `nodes` is a view) aborts the whole merge rather than leaving
// a half-merged repository behind.
func TestMergeGraphDB_RowInsertFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.db")
	src := filepath.Join(dir, "src.db")
	seedGraphDB(t, src, []graphNodeRow{{qualified: "W", name: "W", filePath: "/y/w.go"}}, nil)
	db := openTestDB(t, dst)
	if _, err := db.Exec(`CREATE TABLE stored (id INTEGER PRIMARY KEY, qualified_name TEXT, file_path TEXT);
		CREATE VIEW nodes AS SELECT id, qualified_name, file_path FROM stored;
		CREATE TABLE edges (id INTEGER PRIMARY KEY AUTOINCREMENT, source_qualified TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err := mergeInto(t, dst, src, "vendor/lib")
	if err == nil || !strings.Contains(err.Error(), "merge nodes rows") {
		t.Fatalf("expected a row-insert error, got %v", err)
	}
	if n := countRows(t, dst, "stored"); n != 0 {
		t.Errorf("failed merge left %d rows behind", n)
	}
}

// TestMergeGraphDB_CommitFailureRollsBack: a destination whose constraints are
// only checked at COMMIT still fails the merge and leaves nothing behind.
func TestMergeGraphDB_CommitFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.db")
	src := filepath.Join(dir, "src.db")
	seedGraphDB(t, src, []graphNodeRow{{qualified: "W", name: "W", filePath: "/y/w.go"}}, nil)
	db := openTestDB(t, dst)
	// A DEFERRABLE INITIALLY DEFERRED foreign key is enforced at COMMIT, not
	// at INSERT — the portable way to make the commit itself fail.
	if _, err := db.Exec(`CREATE TABLE owners (id INTEGER PRIMARY KEY);
		CREATE TABLE nodes (
		  id INTEGER PRIMARY KEY AUTOINCREMENT, qualified_name TEXT UNIQUE, file_path TEXT,
		  line_start INTEGER REFERENCES owners(id) DEFERRABLE INITIALLY DEFERRED);
		CREATE TABLE edges (id INTEGER PRIMARY KEY AUTOINCREMENT, source_qualified TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	// foreign_keys must be ON for the deferred check to fire.
	dstDB := openTestDB(t, dst+"?_pragma=foreign_keys(1)")
	defer dstDB.Close()

	_, err := MergeGraphDB(dstDB, src, "vendor/lib")
	if err == nil || !strings.Contains(err.Error(), "commit merge") {
		t.Fatalf("expected a commit error, got %v", err)
	}
	if n := countRows(t, dst, "nodes"); n != 0 {
		t.Errorf("failed commit left %d rows behind", n)
	}
}

// TestMergeScopeSelectExpr pins the per-column SQL the scope generates.
func TestMergeScopeSelectExpr(t *testing.T) {
	scope := newMergeScope("vendor/lib")
	cases := []struct {
		name     string
		col      string
		wantExpr string
		wantArg  any
	}{
		{"qualified name", "qualified_name", `? || "qualified_name"`, "vendor/lib::"},
		{"edge source", "source_qualified", `? || "source_qualified"`, "vendor/lib::"},
		{"edge target", "target_qualified", `? || "target_qualified"`, "vendor/lib::"},
		{"plain column", "language", `"language"`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, arg := scope.selectExpr(tc.col)
			if expr != tc.wantExpr || arg != tc.wantArg {
				t.Errorf("selectExpr(%q) = (%q, %v), want (%q, %v)", tc.col, expr, arg, tc.wantExpr, tc.wantArg)
			}
		})
	}
	expr, arg := scope.selectExpr("file_path")
	if arg != "vendor/lib/" || !strings.Contains(expr, "CASE WHEN") || !strings.Contains(expr, `substr("file_path", 2, 1) = ':'`) {
		t.Errorf("file_path expr = %q arg = %v", expr, arg)
	}
}

// TestIntersectColumns pins the schema-drift column selection.
func TestIntersectColumns(t *testing.T) {
	got := intersectColumns([]string{"a", "b", "c"}, []string{"c", "a", "d"})
	if strings.Join(got, ",") != "a,c" {
		t.Errorf("intersectColumns = %v, want [a c]", got)
	}
	if intersectColumns(nil, []string{"a"}) != nil {
		t.Error("empty source must intersect to nothing")
	}
}

// TestQuoteIdents pins identifier quoting, including an embedded quote.
func TestQuoteIdents(t *testing.T) {
	got := quoteIdents([]string{"file_path", `we"ird`})
	if got[0] != `"file_path"` || got[1] != `"we""ird"` {
		t.Errorf("quoteIdents = %v", got)
	}
}

// countRows returns the row count of a table.
func countRows(t *testing.T, dbPath, table string) int {
	t.Helper()
	db := openTestDB(t, dbPath)
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// nodeFilePath returns the file_path stored for a qualified name.
func nodeFilePath(t *testing.T, dbPath, qualified string) string {
	t.Helper()
	db := openTestDB(t, dbPath)
	defer db.Close()
	var path string
	if err := db.QueryRow(`SELECT file_path FROM nodes WHERE qualified_name = ?`, qualified).Scan(&path); err != nil {
		t.Fatalf("lookup %s: %v", qualified, err)
	}
	return path
}
