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
// edges.
//
// The DDL is the FULL CRG base schema the merge names — the same column set
// CRGBridge.ReadNodes / ReadEdges read — because the merge executes fixed
// statements over those columns rather than over whatever the two schemas
// happen to share. qualified_name is unique and the ids autoincrement because
// both properties are what make a naive merge misbehave.
func seedGraphDB(t *testing.T, path string, nodes []graphNodeRow, edges []graphEdgeRow) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db := openTestDB(t, path)
	defer db.Close()
	if _, err := db.Exec(graphBaseDDL); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	for _, n := range nodes {
		if _, err := db.Exec(
			`INSERT INTO nodes (kind,name,qualified_name,file_path,line_start,line_end,
			   language,parent_name,params,return_type,is_test,file_hash,extra,updated_at)
			 VALUES ('Function',?,?,?,1,9,'go','','','',0,'','{}',1.0)`,
			n.name, n.qualified, n.filePath); err != nil {
			t.Fatalf("insert node %s: %v", n.qualified, err)
		}
	}
	for _, e := range edges {
		if _, err := db.Exec(
			`INSERT INTO edges (kind,source_qualified,target_qualified,file_path,line,extra,updated_at)
			 VALUES ('CALLS',?,?,?,1,'{}',1.0)`, e.source, e.target, e.filePath); err != nil {
			t.Fatalf("insert edge %s->%s: %v", e.source, e.target, err)
		}
	}
}

// graphBaseDDL is the CRG base schema the merge's fixed statements require on
// BOTH sides. It is spelled out once here so every fixture in this file starts
// from the real column contract.
const graphBaseDDL = `
	CREATE TABLE nodes (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
	  file_path TEXT, line_start INTEGER, line_end INTEGER, language TEXT,
	  parent_name TEXT, params TEXT, return_type TEXT, is_test INTEGER,
	  file_hash TEXT, extra TEXT, updated_at REAL
	);
	CREATE TABLE edges (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  kind TEXT, source_qualified TEXT, target_qualified TEXT,
	  file_path TEXT, line INTEGER, extra TEXT, updated_at REAL
	);`

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

// TestMergeGraphDB_Idempotent: re-merging the same submodule replaces its
// rows rather than adding to them. `edges` carries no unique constraint, so a
// merge that only inserted would silently double every submodule edge on the
// second run and inflate impact radius and flow detection.
func TestMergeGraphDB_Idempotent(t *testing.T) {
	dst, src := twoRepoFixture(t)

	if _, err := mergeInto(t, dst, src, "vendor/lib"); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	nodesAfterFirst, edgesAfterFirst := countRows(t, dst, "nodes"), countRows(t, dst, "edges")

	if _, err := mergeInto(t, dst, src, "vendor/lib"); err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if n := countRows(t, dst, "nodes"); n != nodesAfterFirst {
		t.Errorf("node count after re-merge = %d, want %d", n, nodesAfterFirst)
	}
	if n := countRows(t, dst, "edges"); n != edgesAfterFirst {
		t.Errorf("edge count after re-merge = %d, want %d", n, edgesAfterFirst)
	}
	if nodesAfterFirst != 4 || edgesAfterFirst != 2 {
		t.Errorf("fixture drift: %d nodes / %d edges after the first merge", nodesAfterFirst, edgesAfterFirst)
	}
}

// TestMergeGraphDB_ReplacesStaleScopeRows: the merge owns its namespace, so a
// symbol the submodule deleted disappears from the superproject graph instead
// of lingering forever.
func TestMergeGraphDB_ReplacesStaleScopeRows(t *testing.T) {
	dst, src := twoRepoFixture(t)
	if _, err := mergeInto(t, dst, src, "vendor/lib"); err != nil {
		t.Fatalf("first merge: %v", err)
	}

	// The submodule is rebuilt with FullCheck removed.
	dir := t.TempDir()
	rebuilt := filepath.Join(dir, "rebuilt.db")
	seedGraphDB(t, rebuilt,
		[]graphNodeRow{{qualified: "Button", name: "Button", filePath: "/repo/sub/widgets/Button.tsx"}}, nil)

	if _, err := mergeInto(t, dst, rebuilt, "vendor/lib"); err != nil {
		t.Fatalf("second merge: %v", err)
	}
	db := openTestDB(t, dst)
	defer db.Close()
	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE qualified_name = 'vendor/lib::FullCheck'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Error("a symbol removed from the submodule must not survive the re-merge")
	}
	if n := countRows(t, dst, "nodes"); n != 3 {
		t.Errorf("node count = %d, want 3 (2 superproject + 1 submodule)", n)
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

// TestMergeGraphDB_MissingRequiredColumnNamesIt: the merge's statements are
// fixed, so a side that does not carry a named column cannot be merged. Saying
// "0 rows copied" would hide a lost repository, and saying "schema mismatch"
// would leave the operator guessing — the error names the column.
func TestMergeGraphDB_MissingRequiredColumnNamesIt(t *testing.T) {
	cases := []struct {
		name, ddl, wantSide, wantCols string
	}{
		{
			name:     "destination nodes is missing everything",
			ddl:      `CREATE TABLE nodes (id INTEGER PRIMARY KEY); CREATE TABLE edges (id INTEGER PRIMARY KEY)`,
			wantSide: "destination nodes schema is missing required column(s)",
			wantCols: "kind, name, qualified_name",
		},
		{
			name: "destination nodes is missing one column",
			ddl: `CREATE TABLE nodes (
				  id INTEGER PRIMARY KEY AUTOINCREMENT,
				  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
				  file_path TEXT, line_start INTEGER, line_end INTEGER, language TEXT,
				  parent_name TEXT, params TEXT, return_type TEXT, is_test INTEGER,
				  file_hash TEXT, updated_at REAL
				);
				CREATE TABLE edges (
				  id INTEGER PRIMARY KEY AUTOINCREMENT,
				  kind TEXT, source_qualified TEXT, target_qualified TEXT,
				  file_path TEXT, line INTEGER, extra TEXT, updated_at REAL
				)`,
			wantSide: "destination nodes schema is missing required column(s)",
			wantCols: "extra",
		},
		{
			name: "destination edges is missing an endpoint",
			ddl: `CREATE TABLE nodes (
				  id INTEGER PRIMARY KEY AUTOINCREMENT,
				  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
				  file_path TEXT, line_start INTEGER, line_end INTEGER, language TEXT,
				  parent_name TEXT, params TEXT, return_type TEXT, is_test INTEGER,
				  file_hash TEXT, extra TEXT, updated_at REAL
				);
				CREATE TABLE edges (
				  id INTEGER PRIMARY KEY AUTOINCREMENT,
				  kind TEXT, source_qualified TEXT,
				  file_path TEXT, line INTEGER, extra TEXT, updated_at REAL
				)`,
			wantSide: "destination edges schema is missing required column(s)",
			wantCols: "target_qualified",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dst := filepath.Join(dir, "dst.db")
			src := filepath.Join(dir, "src.db")
			db := openTestDB(t, dst)
			if _, err := db.Exec(tc.ddl); err != nil {
				t.Fatal(err)
			}
			db.Close()
			seedGraphDB(t, src, []graphNodeRow{{qualified: "W", name: "W", filePath: "/y/w.go"}}, nil)

			_, err := mergeInto(t, dst, src, "vendor/lib")
			if err == nil || !strings.Contains(err.Error(), tc.wantSide) {
				t.Fatalf("expected %q, got %v", tc.wantSide, err)
			}
			if !strings.Contains(err.Error(), tc.wantCols) {
				t.Errorf("error must name the missing column(s) %q, got %v", tc.wantCols, err)
			}
		})
	}
}

// TestMergeGraphDB_MissingRequiredSourceColumn: the same contract on the other
// side. A source graph that cannot supply a named column is a merge that would
// quietly lose that field for a whole repository.
func TestMergeGraphDB_MissingRequiredSourceColumn(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.db")
	src := filepath.Join(dir, "src.db")
	seedGraphDB(t, dst, []graphNodeRow{{qualified: "main", name: "main", filePath: "/x/main.go"}}, nil)
	db := openTestDB(t, src)
	if _, err := db.Exec(`CREATE TABLE nodes (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
		  file_path TEXT, line_start INTEGER, line_end INTEGER, language TEXT,
		  parent_name TEXT, params TEXT, return_type TEXT, is_test INTEGER,
		  file_hash TEXT, extra TEXT
		);
		CREATE TABLE edges (id INTEGER PRIMARY KEY AUTOINCREMENT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err := mergeInto(t, dst, src, "vendor/lib")
	if err == nil || !strings.Contains(err.Error(), "source nodes schema is missing required column(s): updated_at") {
		t.Fatalf("expected a source missing-column error naming updated_at, got %v", err)
	}
}

// TestMergeGraphDB_ScopeClearFailureRollsBack: the scope-clearing DELETE runs
// before any row is copied, so a destination that cannot accept it aborts the
// whole merge instead of leaving a half-merged repository behind. (The INSERT
// branch is covered by TestMergeGraphDB_InsertFailureRollsBack.)
func TestMergeGraphDB_ScopeClearFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.db")
	src := filepath.Join(dir, "src.db")
	seedGraphDB(t, src, []graphNodeRow{{qualified: "W", name: "W", filePath: "/y/w.go"}}, nil)
	db := openTestDB(t, dst)
	// `nodes` is a VIEW carrying every required column, so the schema probe
	// passes and the first WRITE is what fails. A view with missing columns
	// would fail at the probe instead and prove nothing about the rollback.
	if _, err := db.Exec(`CREATE TABLE stored (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
		  file_path TEXT, line_start INTEGER, line_end INTEGER, language TEXT,
		  parent_name TEXT, params TEXT, return_type TEXT, is_test INTEGER,
		  file_hash TEXT, extra TEXT, updated_at REAL
		);
		CREATE VIEW nodes AS SELECT * FROM stored;
		CREATE TABLE edges (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, source_qualified TEXT, target_qualified TEXT,
		  file_path TEXT, line INTEGER, extra TEXT, updated_at REAL
		)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err := mergeInto(t, dst, src, "vendor/lib")
	if err == nil || !strings.Contains(err.Error(), "clear previous nodes rows for scope") {
		t.Fatalf("expected the scope-clearing delete to fail, got %v", err)
	}
	if n := countRows(t, dst, "stored"); n != 0 {
		t.Errorf("failed merge left %d rows behind", n)
	}
}

// TestMergeGraphDB_EdgeScopeClearFailureRollsBack: the edge table is cleared
// on BOTH endpoints and after the nodes have already been copied, so its own
// failure has to roll the node copy back too — a graph with the submodule's
// nodes and none of its edges would report zero dependencies for a whole
// repository.
func TestMergeGraphDB_EdgeScopeClearFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.db")
	src := filepath.Join(dir, "src.db")
	seedGraphDB(t, src,
		[]graphNodeRow{{qualified: "W", name: "W", filePath: "/y/w.go"}},
		[]graphEdgeRow{{source: "W", target: "X", filePath: "/y/w.go"}})
	db := openTestDB(t, dst)
	// `nodes` is a real table so its copy succeeds; `edges` is a view, so the
	// first EDGE write — the scope-clearing delete — is what fails.
	if _, err := db.Exec(`CREATE TABLE nodes (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
		  file_path TEXT, line_start INTEGER, line_end INTEGER, language TEXT,
		  parent_name TEXT, params TEXT, return_type TEXT, is_test INTEGER,
		  file_hash TEXT, extra TEXT, updated_at REAL
		);
		CREATE TABLE stored_edges (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, source_qualified TEXT, target_qualified TEXT,
		  file_path TEXT, line INTEGER, extra TEXT, updated_at REAL
		);
		CREATE VIEW edges AS SELECT * FROM stored_edges`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err := mergeInto(t, dst, src, "vendor/lib")
	if err == nil || !strings.Contains(err.Error(), "clear previous edges rows for scope") {
		t.Fatalf("expected the edge scope-clearing delete to fail, got %v", err)
	}
	if n := countRows(t, dst, "nodes"); n != 0 {
		t.Errorf("the edge failure must roll the node copy back, found %d nodes", n)
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
	// at INSERT — the portable way to make the commit itself fail. Every
	// required column is present so the failure is the commit, not the probe.
	if _, err := db.Exec(`CREATE TABLE owners (id INTEGER PRIMARY KEY);
		CREATE TABLE nodes (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
		  file_path TEXT, line_end INTEGER, language TEXT,
		  parent_name TEXT, params TEXT, return_type TEXT, is_test INTEGER,
		  file_hash TEXT, extra TEXT, updated_at REAL,
		  line_start INTEGER REFERENCES owners(id) DEFERRABLE INITIALLY DEFERRED
		);
		CREATE TABLE edges (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, source_qualified TEXT, target_qualified TEXT,
		  file_path TEXT, line INTEGER, extra TEXT, updated_at REAL
		)`); err != nil {
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

// TestMergeGraphDB_InsertFailureRollsBack: a destination that accepts the
// scope-clearing delete but rejects the rows themselves still aborts the whole
// merge rather than leaving the scope emptied and unrefilled.
func TestMergeGraphDB_InsertFailureRollsBack(t *testing.T) {
	dst, src := twoRepoFixture(t)
	if _, err := mergeInto(t, dst, src, "vendor/lib"); err != nil {
		t.Fatalf("seed merge: %v", err)
	}
	db := openTestDB(t, dst)
	if _, err := db.Exec(`CREATE TRIGGER reject_nodes BEFORE INSERT ON nodes
		BEGIN SELECT RAISE(ABORT, 'nodes are frozen'); END`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err := mergeInto(t, dst, src, "vendor/lib")
	if err == nil || !strings.Contains(err.Error(), "merge nodes rows") {
		t.Fatalf("expected a row-insert error, got %v", err)
	}
	// The rollback must restore the scope the merge had already cleared.
	if n := countRows(t, dst, "nodes"); n != 4 {
		t.Errorf("node count after the failed merge = %d, want the pre-merge 4", n)
	}
}

// TestMergeGraphDB_AdditiveDestinationColumnTolerated: a destination written by
// a DIFFERENT graph version can carry columns the merge does not name. The
// fixed statements list their columns explicitly, so the extra one keeps its
// default instead of blocking the merge.
func TestMergeGraphDB_AdditiveDestinationColumnTolerated(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "super.db")
	src := filepath.Join(dir, "sub.db")
	seedGraphDB(t, dst, []graphNodeRow{{qualified: "main", name: "main", filePath: "/x/main.go"}}, nil)
	seedGraphDB(t, src, []graphNodeRow{{qualified: "Widget", name: "Widget", filePath: "/y/widget.go"}}, nil)
	dstDB := openTestDB(t, dst)
	if _, err := dstDB.Exec(`ALTER TABLE nodes ADD COLUMN modifiers TEXT`); err != nil {
		t.Fatalf("alter: %v", err)
	}
	dstDB.Close()

	stats, err := mergeInto(t, dst, src, "vendor/lib")
	if err != nil {
		t.Fatalf("MergeGraphDB into a wider destination: %v", err)
	}
	if stats.Nodes != 1 {
		t.Errorf("stats = %+v, want 1 node merged", stats)
	}
	if got := nodeFilePath(t, dst, "vendor/lib::Widget"); got != "/y/widget.go" {
		t.Errorf("merge into a wider destination lost the row: %q", got)
	}
}

// TestMergeGraphDB_CarriesEveryNamedColumn: the merge's whole reason to name
// columns instead of intersecting them is that a submodule's symbol metadata
// must arrive intact. A copy that silently dropped line numbers, language, or
// the extra blob would leave the merged rows visibly degraded.
func TestMergeGraphDB_CarriesEveryNamedColumn(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "super.db")
	src := filepath.Join(dir, "sub.db")
	seedGraphDB(t, dst, []graphNodeRow{{qualified: "main", name: "main", filePath: "/x/main.go"}}, nil)
	seedGraphDB(t, src, nil, nil)
	srcDB := openTestDB(t, src)
	if _, err := srcDB.Exec(`INSERT INTO nodes
		(kind,name,qualified_name,file_path,line_start,line_end,language,parent_name,
		 params,return_type,is_test,file_hash,extra,updated_at)
		VALUES ('Class','Widget','Widget','/y/widget.go',12,40,'ts','ui','(a,b)','void',
		        1,'deadbeef','{"k":1}',1234.5)`); err != nil {
		t.Fatal(err)
	}
	if _, err := srcDB.Exec(`INSERT INTO edges
		(kind,source_qualified,target_qualified,file_path,line,extra,updated_at)
		VALUES ('CALLS','Widget','Render','/y/widget.go',20,'{"c":0.5}',678.9)`); err != nil {
		t.Fatal(err)
	}
	srcDB.Close()

	if _, err := mergeInto(t, dst, src, "vendor/lib"); err != nil {
		t.Fatalf("MergeGraphDB: %v", err)
	}

	db := openTestDB(t, dst)
	defer db.Close()
	var (
		kind, name, qualified, filePath, language, parent, params, ret, hash, extra string
		lineStart, lineEnd, isTest                                                  int
		updated                                                                     float64
	)
	if err := db.QueryRow(`SELECT kind,name,qualified_name,file_path,line_start,line_end,
		language,parent_name,params,return_type,is_test,file_hash,extra,updated_at
		FROM nodes WHERE qualified_name = 'vendor/lib::Widget'`).Scan(
		&kind, &name, &qualified, &filePath, &lineStart, &lineEnd, &language, &parent,
		&params, &ret, &isTest, &hash, &extra, &updated); err != nil {
		t.Fatalf("read merged node: %v", err)
	}
	got := []any{kind, name, filePath, lineStart, lineEnd, language, parent, params, ret,
		isTest, hash, extra, updated}
	want := []any{"Class", "Widget", "/y/widget.go", 12, 40, "ts", "ui", "(a,b)", "void",
		1, "deadbeef", `{"k":1}`, 1234.5}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("merged node column %d = %v, want %v", i, got[i], want[i])
		}
	}

	var srcQ, dstQ, edgeFile, edgeExtra string
	var edgeLine int
	var edgeUpdated float64
	if err := db.QueryRow(`SELECT source_qualified,target_qualified,file_path,line,extra,updated_at
		FROM edges WHERE source_qualified = 'vendor/lib::Widget'`).Scan(
		&srcQ, &dstQ, &edgeFile, &edgeLine, &edgeExtra, &edgeUpdated); err != nil {
		t.Fatalf("read merged edge: %v", err)
	}
	// BOTH endpoints carry the scope: a prefix on only one side would resolve
	// the other against the superproject and invent a cross-repo edge.
	if srcQ != "vendor/lib::Widget" || dstQ != "vendor/lib::Render" {
		t.Errorf("merged edge endpoints = %q -> %q, want both scoped", srcQ, dstQ)
	}
	if edgeFile != "/y/widget.go" || edgeLine != 20 || edgeExtra != `{"c":0.5}` || edgeUpdated != 678.9 {
		t.Errorf("merged edge payload = %q/%d/%q/%v", edgeFile, edgeLine, edgeExtra, edgeUpdated)
	}
}

// TestMergeSQLIsFullyLiteral is the reason the go:S2077 suppression is gone:
// nothing variable reaches a statement. If a future change reintroduces an
// interpolated identifier, the statement stops being a plain literal and this
// test is where that shows up.
func TestMergeSQLIsFullyLiteral(t *testing.T) {
	statements := map[string]string{
		"attach":          attachSourceSQL,
		"detach":          detachSourceSQL,
		"probe src nodes": probeSrcNodes,
		"probe dst nodes": probeDstNodes,
		"probe src edges": probeSrcEdges,
		"probe dst edges": probeDstEdges,
		"clear nodes":     clearNodesSQL,
		"clear edges":     clearEdgesSQL,
		"merge nodes":     mergeNodesSQL,
		"merge edges":     mergeEdgesSQL,
	}
	for name, stmt := range statements {
		if strings.ContainsAny(stmt, "%") {
			t.Errorf("%s statement carries a format verb: %q", name, stmt)
		}
	}
	// The scope reaches SQL only as bound values, never as text.
	scope := newMergeScope("vendor/lib")
	if scope.prefix != "vendor/lib::" || scope.relBase != "vendor/lib/" {
		t.Errorf("scope = %+v, want the :: prefix and the / rebase", scope)
	}
	for _, stmt := range []string{mergeNodesSQL, mergeEdgesSQL, clearNodesSQL, clearEdgesSQL} {
		if strings.Contains(stmt, scope.prefix) || strings.Contains(stmt, "vendor") {
			t.Errorf("statement embeds a scope value: %q", stmt)
		}
	}
	// Bind counts must match what mergeNodes / mergeEdges pass.
	binds := map[string]int{
		clearNodesSQL: 1, clearEdgesSQL: 2, mergeNodesSQL: 2, mergeEdgesSQL: 3,
	}
	for stmt, want := range binds {
		if got := strings.Count(stmt, "?"); got != want {
			t.Errorf("statement %q has %d binds, want %d", stmt, got, want)
		}
	}
}

// TestMissingColumns pins the deterministic, required-order missing-column
// report the error text depends on.
func TestMissingColumns(t *testing.T) {
	got := missingColumns([]string{"kind", "name", "extra"}, []string{"extra", "name"})
	if strings.Join(got, ",") != "kind" {
		t.Errorf("missingColumns = %v, want [kind]", got)
	}
	if missingColumns(nodeColumns, nodeColumns) != nil {
		t.Error("a complete schema must report nothing missing")
	}
	got = missingColumns([]string{"a", "b"}, nil)
	if strings.Join(got, ",") != "a,b" {
		t.Errorf("missingColumns = %v, want required order [a b]", got)
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
