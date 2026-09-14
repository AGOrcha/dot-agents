package crgbehavior

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// releaseSchemaSQL is the pinned release's OWN schema, transcribed from
// code-review-graph v2.3.8 (graph.py `_SCHEMA_SQL` plus migrations v2..v9).
// The fixtures build a REAL SQLite store — including the FTS5 external-content
// virtual table — rather than stubbing the reader, so the reader stays honest
// about the schema it decodes and an FTS5 `MATCH` is a real search.
const releaseSchemaSQL = `
CREATE TABLE nodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL, name TEXT NOT NULL, qualified_name TEXT NOT NULL UNIQUE,
  file_path TEXT NOT NULL, line_start INTEGER, line_end INTEGER, language TEXT,
  parent_name TEXT, params TEXT, return_type TEXT, modifiers TEXT,
  is_test INTEGER DEFAULT 0, file_hash TEXT, extra TEXT DEFAULT '{}',
  updated_at REAL NOT NULL DEFAULT 0, signature TEXT, community_id INTEGER);
CREATE TABLE edges (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL, source_qualified TEXT NOT NULL, target_qualified TEXT NOT NULL,
  file_path TEXT NOT NULL, line INTEGER DEFAULT 0, extra TEXT DEFAULT '{}',
  confidence REAL DEFAULT 1.0, confidence_tier TEXT DEFAULT 'EXTRACTED',
  updated_at REAL NOT NULL DEFAULT 0);
CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE flows (
  id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL,
  entry_point_id INTEGER NOT NULL, depth INTEGER NOT NULL, node_count INTEGER NOT NULL,
  file_count INTEGER NOT NULL, criticality REAL NOT NULL DEFAULT 0.0,
  path_json TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')));
CREATE TABLE flow_memberships (
  flow_id INTEGER NOT NULL, node_id INTEGER NOT NULL, position INTEGER NOT NULL,
  PRIMARY KEY (flow_id, node_id));
CREATE TABLE communities (
  id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, level INTEGER NOT NULL DEFAULT 0,
  parent_id INTEGER, cohesion REAL NOT NULL DEFAULT 0.0, size INTEGER NOT NULL DEFAULT 0,
  dominant_language TEXT, description TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')));
CREATE TABLE community_summaries (
  community_id INTEGER PRIMARY KEY, name TEXT NOT NULL, purpose TEXT DEFAULT '',
  key_symbols TEXT DEFAULT '[]', risk TEXT DEFAULT 'unknown', size INTEGER DEFAULT 0,
  dominant_language TEXT DEFAULT '');
CREATE TABLE flow_snapshots (
  flow_id INTEGER PRIMARY KEY, name TEXT NOT NULL, entry_point TEXT NOT NULL,
  critical_path TEXT DEFAULT '[]', criticality REAL DEFAULT 0.0,
  node_count INTEGER DEFAULT 0, file_count INTEGER DEFAULT 0);
CREATE TABLE risk_index (
  node_id INTEGER PRIMARY KEY, qualified_name TEXT NOT NULL, risk_score REAL DEFAULT 0.0,
  caller_count INTEGER DEFAULT 0, test_coverage TEXT DEFAULT 'unknown',
  security_relevant INTEGER DEFAULT 0, last_computed TEXT DEFAULT '');
CREATE VIRTUAL TABLE nodes_fts USING fts5(
  name, qualified_name, file_path, signature,
  content='nodes', content_rowid='rowid', tokenize='porter unicode61');
INSERT INTO metadata (key, value) VALUES ('schema_version', '9');
`

// graphRoot is the absolute root the fixture graph was built under; the reader
// must trim it away so both sides of the gate share one id space.
const graphRoot = "/abs/repo"

// twoFlowSeed is a small release graph: three symbols across two files, one
// traced flow with its snapshot, one community with its summary, two scored
// nodes, and one unresolved bare call target.
const twoFlowSeed = `
INSERT INTO nodes (id,kind,name,qualified_name,file_path,line_start,language,file_hash,signature,community_id) VALUES
 (1,'Function','Entry','/abs/repo/pkg/a.go::Entry','/abs/repo/pkg/a.go',3,'go','h1','def Entry()',7),
 (2,'Function','Step','/abs/repo/pkg/a.go::Step','/abs/repo/pkg/a.go',9,'go','h1','def Step()',7),
 (3,'Class','Widget','/abs/repo/pkg/b.go::Widget','/abs/repo/pkg/b.go',1,'go','h2','class Widget',NULL);
INSERT INTO edges (kind,source_qualified,target_qualified,file_path,line,confidence,confidence_tier) VALUES
 ('CALLS','/abs/repo/pkg/a.go::Entry','/abs/repo/pkg/a.go::Step','/abs/repo/pkg/a.go',4,1.0,'EXTRACTED'),
 ('IMPORTS_FROM','/abs/repo/pkg/a.go::Entry','/abs/repo/pkg/b.go::Widget','/abs/repo/pkg/a.go',1,0.75,'INFERRED'),
 ('CALLS','/abs/repo/pkg/a.go::Step','append','/abs/repo/pkg/a.go',11,0.5,'HEURISTIC');
INSERT INTO flows (id,name,entry_point_id,depth,node_count,file_count,criticality,path_json) VALUES
 (41,'Entry',1,1,2,1,0.1875,'[1,2]');
INSERT INTO flow_memberships (flow_id,node_id,position) VALUES (41,1,0),(41,2,1);
INSERT INTO communities (id,name,size,dominant_language) VALUES (7,'pkg',2,'go');
INSERT INTO community_summaries (community_id,name,purpose,key_symbols,risk,size,dominant_language) VALUES
 (7,'pkg','pkg','["Entry","Step"]','unknown',2,'go');
INSERT INTO flow_snapshots (flow_id,name,entry_point,critical_path,criticality,node_count,file_count) VALUES
 (41,'Entry','/abs/repo/pkg/a.go::Entry','["/abs/repo/pkg/a.go::Entry","/abs/repo/pkg/a.go::Step"]',0.1875,2,1);
INSERT INTO risk_index (node_id,qualified_name,risk_score,caller_count,test_coverage,security_relevant,last_computed) VALUES
 (1,'/abs/repo/pkg/a.go::Entry',0.3,0,'untested',0,'2026-01-01 00:00:00'),
 (2,'/abs/repo/pkg/a.go::Step',0.7,4,'untested',1,'2026-01-01 00:00:00');
INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild');
`

// newGraphDB creates a pinned-release-shaped SQLite store and returns its path.
func newGraphDB(t *testing.T, seed string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph.db")
	db, err := sql.Open(sqliteDriver, path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(releaseSchemaSQL + seed); err != nil {
		t.Fatalf("seed fixture db: %v", err)
	}
	return path
}

// openGraph opens a seeded fixture store through the production reader.
func openGraph(t *testing.T, seed string) *BridgeStore {
	t.Helper()
	store, err := OpenBridgeStore(graphRoot, newGraphDB(t, seed), PinnedVersion, testRelease())
	if err != nil {
		t.Fatalf("open bridge store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// testRelease is the pinned release fixture the unit tests probe against. It
// mirrors the checked-in release-2.3.8.json; TestReleaseFixtureIsTheCheckedInOne
// proves the two do not drift.
func testRelease() Release {
	return Release{
		Package:       PackageName,
		Version:       PinnedVersion,
		Tag:           PinnedTag,
		Commit:        PinnedCommit,
		SchemaVersion: PinnedSchemaVersion,
		Languages:     map[string]string{".go": "go", ".py": "python", ".ts": "typescript", ".rs": "rust"},
		Tables: []TableSpec{
			{Name: "metadata", Required: true, Columns: []string{"key", "value"}},
			{Name: "nodes", Required: true, Columns: []string{
				"id", "name", "qualified_name", "file_path", "kind", "language",
				"line_start", "file_hash", "signature", "community_id"}},
			{Name: "edges", Required: true, Columns: []string{
				"kind", "source_qualified", "target_qualified", "file_path", "line",
				"confidence", "confidence_tier"}, Surfaces: []string{SurfaceEdgeConfidence}},
			{Name: tableFlows, Columns: []string{
				"id", "name", "entry_point_id", "depth", "node_count", "file_count",
				"criticality", "path_json"}, Surfaces: []string{SurfaceFlows, SurfaceFlowMetrics}},
			{Name: tableFlowMemberships, Columns: []string{"flow_id", "node_id", "position"}},
			{Name: tableCommunities, Columns: []string{"id", "name", "size", "dominant_language"},
				Surfaces: []string{SurfaceCommunities}},
			{Name: tableCommunitySummaries, Columns: []string{
				"community_id", "name", "purpose", "key_symbols", "risk", "size", "dominant_language"},
				Surfaces: []string{SurfaceCommunitySummaries}},
			{Name: tableFlowSnapshots, Columns: []string{
				"flow_id", "name", "entry_point", "critical_path", "criticality",
				"node_count", "file_count"}, Surfaces: []string{SurfaceFlowSnapshots}},
			{Name: tableRiskIndex, Columns: []string{
				"node_id", "qualified_name", "risk_score", "caller_count",
				"test_coverage", "security_relevant"},
				Surfaces: []string{SurfaceRiskIndex, SurfaceRiskDetail}},
			{Name: tableNodesFTS, Columns: []string{"name", "qualified_name", "file_path", "signature"},
				Surfaces: []string{SurfaceFTSIndex, SurfaceFTSSearch}},
		},
	}
}

// repoFile builds a comparison id for a fixture symbol.
func repoFile(file, symbol string) string {
	return symbolIDOf(file+qualifiedSep+symbol, file)
}
