package crgbehavior

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// bridgeSchema mirrors the legacy code-review-graph store's columns the gate
// reads. Building a real SQLite store (rather than stubbing the reader) keeps
// the reader honest about the schema it decodes.
const bridgeSchema = `
CREATE TABLE nodes (
  id INTEGER PRIMARY KEY, kind TEXT, name TEXT, qualified_name TEXT,
  file_path TEXT, line_start INTEGER, language TEXT, file_hash TEXT,
  community_id INTEGER);
CREATE TABLE edges (
  id INTEGER PRIMARY KEY, kind TEXT, source_qualified TEXT, target_qualified TEXT);
CREATE TABLE flow_memberships (flow_id INTEGER, node_id INTEGER, position INTEGER);
CREATE TABLE risk_index (node_id INTEGER, risk_score REAL);
CREATE TABLE nodes_fts (qualified_name TEXT);
`

// repoPrefix is the absolute root the fixture graph was built under; the reader
// must normalize it away so both sides of the gate share one id space.
const repoPrefix = "/abs/repo"

// newBridgeDB creates a legacy-shaped SQLite store and returns its path.
func newBridgeDB(t *testing.T, seed string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(bridgeSchema + seed); err != nil {
		t.Fatalf("seed fixture db: %v", err)
	}
	return path
}

// twoFlowSeed is a small legacy graph: three symbols in two files, one flow,
// one unassigned community, one scored node, and one unresolved call target.
const twoFlowSeed = `
INSERT INTO nodes VALUES
 (1,'Function','Entry','/abs/repo/pkg/a.go::Entry','/abs/repo/pkg/a.go',3,'go','h1',7),
 (2,'Function','Step','/abs/repo/pkg/a.go::Step','/abs/repo/pkg/a.go',9,'go','h1',7),
 (3,'Type','Widget','/abs/repo/pkg/b.go::Widget','/abs/repo/pkg/b.go',1,'go','h2',NULL);
INSERT INTO edges VALUES
 (1,'CALLS','/abs/repo/pkg/a.go::Entry','/abs/repo/pkg/a.go::Step'),
 (2,'IMPORTS_FROM','/abs/repo/pkg/a.go::Entry','/abs/repo/pkg/b.go::Widget'),
 (3,'CALLS','/abs/repo/pkg/a.go::Step','append');
INSERT INTO flow_memberships VALUES (41,1,0),(41,2,1),(42,999,0);
INSERT INTO risk_index VALUES (1,0.7),(2,0.3),(999,0.9);
INSERT INTO nodes_fts VALUES
 ('/abs/repo/pkg/a.go::Entry'),('/abs/repo/pkg/a.go::Step'),
 ('/abs/repo/pkg/b.go::Widget'),('/abs/repo/pkg/a.go::Entry');
`

func TestReadBridgeViewsNormalizesLegacyState(t *testing.T) {
	views, err := ReadBridgeViews(repoPrefix, newBridgeDB(t, twoFlowSeed))
	if err != nil {
		t.Fatalf("read views: %v", err)
	}
	assertSymbolsNormalized(t, views)
	assertImportKindMapped(t, views)
	assertFlowsKeyedByEntryPoint(t, views)
	assertCommunitiesAndRisk(t, views)
	if strings.Join(views.FTS, ",") != "pkg/a.go::Entry,pkg/a.go::Step,pkg/b.go::Widget" {
		t.Fatalf("FTS token set = %v, want sorted distinct repo-relative tokens", views.FTS)
	}
	if views.FilesIndexed != 2 {
		t.Fatalf("FilesIndexed = %d, want 2", views.FilesIndexed)
	}
}

func assertSymbolsNormalized(t *testing.T, views BridgeViews) {
	t.Helper()
	if len(views.Symbols) != 3 {
		t.Fatalf("symbols = %d, want 3", len(views.Symbols))
	}
	first := views.Symbols[0]
	if first.QualifiedName != "pkg/a.go::Entry" || first.FilePath != "pkg/a.go" {
		t.Fatalf("absolute paths must be relativized, got %+v", first)
	}
	if first.Kind != "Function" || first.Language != "go" || first.LineStart != 3 || first.ContentHash != "h1" {
		t.Fatalf("symbol fields lost in normalization: %+v", first)
	}
}

func assertImportKindMapped(t *testing.T, views BridgeViews) {
	t.Helper()
	kinds := map[string]int{}
	for _, r := range views.References {
		kinds[r.Kind]++
	}
	if kinds["IMPORTS"] != 1 || kinds["IMPORTS_FROM"] != 0 {
		t.Fatalf("legacy IMPORTS_FROM must map onto the kg-native IMPORTS spelling: %v", kinds)
	}
	if kinds["CALLS"] != 2 {
		t.Fatalf("CALLS edges = %d, want 2 (including the unresolved target, dropped later at ingestion)", kinds["CALLS"])
	}
}

func assertFlowsKeyedByEntryPoint(t *testing.T, views BridgeViews) {
	t.Helper()
	if len(views.FlowMemberships) != 2 {
		t.Fatalf("flow rows = %d, want 2 (the flow whose node is absent is dropped)", len(views.FlowMemberships))
	}
	want := "pkg/a.go::Entry@pkg/a.go"
	for _, row := range views.FlowMemberships {
		if row.FlowID != want {
			t.Fatalf("flow rows must be re-keyed by their entry-point symbol id, got %q", row.FlowID)
		}
	}
}

func assertCommunitiesAndRisk(t *testing.T, views BridgeViews) {
	t.Helper()
	entry := "pkg/a.go::Entry@pkg/a.go"
	widget := "pkg/b.go::Widget@pkg/b.go"
	if views.Communities[entry] != "c7" {
		t.Fatalf("community id = %q, want c7", views.Communities[entry])
	}
	if !strings.HasPrefix(views.Communities[widget], "unassigned:") {
		t.Fatalf("an unassigned node must become its own singleton cluster, got %q", views.Communities[widget])
	}
	if views.RiskIndex[entry] != 0.7 || len(views.RiskIndex) != 2 {
		t.Fatalf("risk index = %v, want the two scored nodes keyed natively", views.RiskIndex)
	}
}

func TestReadBridgeViewsUnavailable(t *testing.T) {
	if _, err := ReadBridgeViews(repoPrefix, filepath.Join(t.TempDir(), "absent.db")); !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatal("a missing graph must report ErrBridgeUnavailable so the gate SKIPS")
	}
	empty := newBridgeDB(t, "")
	if _, err := ReadBridgeViews(repoPrefix, empty); !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatal("an empty graph must report ErrBridgeUnavailable, not a false-clean comparison")
	}
}

func TestReadBridgeViewsReportsMissingTables(t *testing.T) {
	cases := []struct {
		drop string
		want string
	}{
		{"nodes", "query bridge nodes"},
		{"edges", "query bridge edges"},
		{"flow_memberships", "query bridge flow_memberships"},
		{"risk_index", "query bridge risk_index"},
		{"nodes_fts", "query bridge nodes_fts"},
	}
	for _, tc := range cases {
		path := newBridgeDB(t, twoFlowSeed+"\nDROP TABLE "+tc.drop+";")
		_, err := ReadBridgeViews(repoPrefix, path)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("dropping %s: err = %v, want one mentioning %q", tc.drop, err, tc.want)
		}
	}
}

func TestReadBridgeViewsReportsScanFailures(t *testing.T) {
	cases := []struct {
		name string
		seed string
		want string
	}{
		{
			"node with null qualified name",
			`INSERT INTO nodes VALUES (1,'Function','n',NULL,'/abs/repo/a.go',1,'go','h',NULL);`,
			"scan bridge node",
		},
		{
			"edge with null endpoint",
			twoFlowSeed + `INSERT INTO edges VALUES (9,'CALLS',NULL,'x');`,
			"scan bridge edge",
		},
		{
			"non-numeric flow id",
			twoFlowSeed + `INSERT INTO flow_memberships VALUES ('not-a-number',1,0);`,
			"scan bridge flow_membership",
		},
		{
			"non-numeric risk node id",
			twoFlowSeed + `INSERT INTO risk_index VALUES ('not-a-number',0.1);`,
			"scan bridge risk_index",
		},
		{
			"null fts token",
			twoFlowSeed + `INSERT INTO nodes_fts VALUES (NULL);`,
			"scan bridge fts token",
		},
	}
	for _, tc := range cases {
		_, err := ReadBridgeViews(repoPrefix, newBridgeDB(t, tc.seed))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want one mentioning %q", tc.name, err, tc.want)
		}
	}
}

func TestReadBridgeViewsRejectsUnopenableStore(t *testing.T) {
	// A directory exists but is not a SQLite database: open succeeds lazily,
	// the first query fails.
	dir := t.TempDir()
	if _, err := ReadBridgeViews(repoPrefix, dir); err == nil {
		t.Fatal("a non-database path must fail")
	}
}

func TestWrapIterErrNamesTheTable(t *testing.T) {
	if err := wrapIterErr(nil, "nodes"); err != nil {
		t.Fatalf("a clean iteration must not synthesize an error: %v", err)
	}
	err := wrapIterErr(errors.New("connection lost"), "edges")
	if err == nil || !strings.Contains(err.Error(), "iterate bridge edges") {
		t.Fatalf("err = %v, want one naming the table", err)
	}
}

func TestRelativizeLeavesRelativeValuesAlone(t *testing.T) {
	if got := relativize("pkg/a.go::X", ""); got != "pkg/a.go::X" {
		t.Fatalf("an empty root must be a no-op, got %q", got)
	}
	if got := relativize("/abs/repo/pkg/a.go::X", "/abs/repo/"); got != "pkg/a.go::X" {
		t.Fatalf("a trailing-slash root must still strip, got %q", got)
	}
}

func TestCorpusCarriesCommitAndGraph(t *testing.T) {
	views, err := ReadBridgeViews(repoPrefix, newBridgeDB(t, twoFlowSeed))
	if err != nil {
		t.Fatalf("read views: %v", err)
	}
	c := views.Corpus("head-sha")
	if c.Commit != "head-sha" || len(c.Symbols) != 3 || len(c.References) != 3 {
		t.Fatalf("corpus = %+v, want the bridge graph pinned at the commit", c)
	}
	notes, edges := c.ToGraph()
	if len(notes) != 3 {
		t.Fatalf("notes = %d, want 3", len(notes))
	}
	if len(edges) != 2 {
		t.Fatalf("edges = %d, want 2 — the unresolved bare call target must be dropped at ingestion", len(edges))
	}
}
