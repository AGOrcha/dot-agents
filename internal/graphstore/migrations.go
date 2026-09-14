package graphstore

import (
	"database/sql"
	"fmt"
)

// SchemaVersion is the code-graph schema version this store creates and
// migrates to. It is upstream code-review-graph v2.3.8's LATEST_VERSION
// (code_review_graph/migrations.py) and is recorded verbatim under the
// `schema_version` metadata key, so a native database and an upstream
// database report the same version to every consumer.
const SchemaVersion = 9

// metadataSchemaVersion is the metadata key holding SchemaVersion.
const metadataSchemaVersion = "schema_version"

// schemaSQL is the canonical DDL for the graphstore SQLite database.
//
// Two layers live here:
//
//   - The CODE-GRAPH layer is upstream code-review-graph's schema v9. The
//     derived-view tables (communities, flows, flow_memberships,
//     community_summaries, flow_snapshots, risk_index), the embeddings
//     table and every index below are copied VERBATIM from upstream
//     (code_review_graph/migrations.py v3–v9, embeddings.py
//     _EMBEDDINGS_SCHEMA, graph.py's base schema) so a native database and
//     an upstream database are the same database. Do not "tidy" them: the
//     migration parity test in migrations_test.go compares the resulting
//     sqlite_master rows against testdata/crg-release/v2.3.8/
//     sqlite-schema.json.
//   - The KG layer (kg_notes, note_symbol_links and their indexes) is
//     native-only; upstream has no knowledge-note tables. It is the one
//     documented superset over upstream's object set.
//
// `nodes`, `edges` and `metadata` keep the native column constraints
// (NOT NULL / DEFAULT hardening upstream omits) because they predate this
// schema and SQLite cannot relax or tighten a column on an existing table.
// Their COLUMN SETS match upstream exactly once migrateCodeGraphSchema has
// appended nodes.signature / nodes.community_id / edges.confidence /
// edges.confidence_tier.
//
// Every statement here is `CREATE ... IF NOT EXISTS` and order-independent,
// so running it is idempotent and a process killed mid-Exec cannot wedge
// the DB. Column additions are NOT expressible that way and live in
// migrateCodeGraphSchema, which guards each one on pragma_table_info.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS nodes (
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

CREATE TABLE IF NOT EXISTS edges (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    kind             TEXT    NOT NULL,
    source_qualified TEXT    NOT NULL,
    target_qualified TEXT    NOT NULL,
    file_path        TEXT    NOT NULL,
    line             INTEGER NOT NULL DEFAULT 0,
    extra            TEXT    NOT NULL DEFAULT '{}',
    updated_at       REAL    NOT NULL
);

CREATE TABLE IF NOT EXISTS metadata (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Upstream schema v3: execution flows.
CREATE TABLE IF NOT EXISTS flows (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            entry_point_id INTEGER NOT NULL,
            depth INTEGER NOT NULL,
            node_count INTEGER NOT NULL,
            file_count INTEGER NOT NULL,
            criticality REAL NOT NULL DEFAULT 0.0,
            path_json TEXT NOT NULL,
            created_at TEXT NOT NULL DEFAULT (datetime('now')),
            updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

CREATE TABLE IF NOT EXISTS flow_memberships (
            flow_id INTEGER NOT NULL,
            node_id INTEGER NOT NULL,
            position INTEGER NOT NULL,
            PRIMARY KEY (flow_id, node_id)
        );

-- Upstream schema v4: communities.
CREATE TABLE IF NOT EXISTS communities (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            level INTEGER NOT NULL DEFAULT 0,
            parent_id INTEGER,
            cohesion REAL NOT NULL DEFAULT 0.0,
            size INTEGER NOT NULL DEFAULT 0,
            dominant_language TEXT,
            description TEXT,
            created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

-- Upstream schema v6: pre-computed summary tables.
CREATE TABLE IF NOT EXISTS community_summaries (
            community_id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            purpose TEXT DEFAULT '',
            key_symbols TEXT DEFAULT '[]',
            risk TEXT DEFAULT 'unknown',
            size INTEGER DEFAULT 0,
            dominant_language TEXT DEFAULT '',
            FOREIGN KEY (community_id) REFERENCES communities(id)
        );

CREATE TABLE IF NOT EXISTS flow_snapshots (
            flow_id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            entry_point TEXT NOT NULL,
            critical_path TEXT DEFAULT '[]',
            criticality REAL DEFAULT 0.0,
            node_count INTEGER DEFAULT 0,
            file_count INTEGER DEFAULT 0,
            FOREIGN KEY (flow_id) REFERENCES flows(id)
        );

CREATE TABLE IF NOT EXISTS risk_index (
            node_id INTEGER PRIMARY KEY,
            qualified_name TEXT NOT NULL,
            risk_score REAL DEFAULT 0.0,
            caller_count INTEGER DEFAULT 0,
            test_coverage TEXT DEFAULT 'unknown',
            security_relevant INTEGER DEFAULT 0,
            last_computed TEXT DEFAULT '',
            FOREIGN KEY (node_id) REFERENCES nodes(id)
        );

-- Upstream embedding store (code_review_graph/embeddings.py). The native
-- backend does not yet compute vectors; the table is part of upstream's
-- schema-v9 object set and is what the semantic-search / embed tools bind
-- to, so it is created with upstream's DDL rather than diverging later.
CREATE TABLE IF NOT EXISTS embeddings (
    qualified_name TEXT PRIMARY KEY,
    vector BLOB NOT NULL,
    text_hash TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'unknown'
);

-- KG knowledge notes (warm layer — archives and indexed copies of hot notes)
CREATE TABLE IF NOT EXISTS kg_notes (
    id          TEXT PRIMARY KEY,
    title       TEXT    NOT NULL,
    note_type   TEXT    NOT NULL,
    status      TEXT    NOT NULL,
    summary     TEXT    NOT NULL DEFAULT '',
    file_path   TEXT    NOT NULL,
    version     INTEGER NOT NULL DEFAULT 0,
    archived_at TEXT    NOT NULL DEFAULT '',
    indexed_at  REAL    NOT NULL
);

-- Cross-references between KG notes and code symbols
CREATE TABLE IF NOT EXISTS note_symbol_links (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    note_id        TEXT    NOT NULL,
    qualified_name TEXT    NOT NULL,
    link_kind      TEXT    NOT NULL DEFAULT 'mentions',
    created_at     REAL    NOT NULL,
    UNIQUE(note_id, qualified_name, link_kind)
);

-- Code graph indexes (upstream schema v1-v8, verbatim)
CREATE INDEX IF NOT EXISTS idx_nodes_file ON nodes(file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_kind ON nodes(kind);
CREATE INDEX IF NOT EXISTS idx_nodes_qualified ON nodes(qualified_name);
CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_qualified);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_qualified);
CREATE INDEX IF NOT EXISTS idx_edges_kind ON edges(kind);
CREATE INDEX IF NOT EXISTS idx_edges_file ON edges(file_path);
CREATE INDEX IF NOT EXISTS idx_edges_target_kind ON edges(target_qualified, kind);
CREATE INDEX IF NOT EXISTS idx_edges_source_kind ON edges(source_qualified, kind);
CREATE INDEX IF NOT EXISTS idx_edges_composite ON edges(kind, source_qualified, target_qualified, file_path, line);
CREATE INDEX IF NOT EXISTS idx_flows_criticality ON flows(criticality DESC);
CREATE INDEX IF NOT EXISTS idx_flows_entry ON flows(entry_point_id);
CREATE INDEX IF NOT EXISTS idx_flow_memberships_node ON flow_memberships(node_id);
CREATE INDEX IF NOT EXISTS idx_communities_parent ON communities(parent_id);
CREATE INDEX IF NOT EXISTS idx_communities_cohesion ON communities(cohesion DESC);
CREATE INDEX IF NOT EXISTS idx_risk_index_score ON risk_index(risk_score DESC);

-- KG indexes
CREATE INDEX IF NOT EXISTS idx_kg_notes_type       ON kg_notes(note_type);
CREATE INDEX IF NOT EXISTS idx_kg_notes_status     ON kg_notes(status);
CREATE INDEX IF NOT EXISTS idx_kg_notes_archived   ON kg_notes(archived_at);
CREATE INDEX IF NOT EXISTS idx_nsl_note_id         ON note_symbol_links(note_id);
CREATE INDEX IF NOT EXISTS idx_nsl_qualified       ON note_symbol_links(qualified_name);
`

// ftsSchemaSQL is upstream's nodes_fts FTS5 virtual table (migrations.py
// v5, re-created identically by search.py's rebuild_fts_index). It is an
// EXTERNAL-CONTENT table: it stores no copy of the rows, it projects
// nodes(name, qualified_name, file_path, signature) by rowid. That is why
// it must be created only AFTER nodes.signature exists, and why
// `SELECT count(*) FROM nodes_fts` returns the node count.
const ftsSchemaSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(
                name, qualified_name, file_path, signature,
                content='nodes', content_rowid='rowid',
                tokenize='porter unicode61'
            )`

// postMigrationSchemaSQL is the DDL that can only run once
// migrateCodeGraphSchema has appended the column it references. Upstream
// creates idx_nodes_community inside migration v4, immediately after the
// `ALTER TABLE nodes ADD COLUMN community_id`, for exactly this reason.
const postMigrationSchemaSQL = `
CREATE INDEX IF NOT EXISTS idx_nodes_community ON nodes(community_id);
`

// codeGraphAddedColumns are the columns upstream appends to the base
// nodes/edges tables through migrations v2 (nodes.signature), v4
// (nodes.community_id) and v9 (edges.confidence, edges.confidence_tier).
//
// They are applied as ALTER TABLE rather than folded into the CREATE above
// for two reasons: an existing native database cannot be re-created, and
// appending keeps the column ORDER identical to upstream's (`SELECT *`
// positional scans in sqlite.go depend on it).
var codeGraphAddedColumns = []struct {
	table  string
	column string
	ddl    string
}{
	{"nodes", "signature", "ALTER TABLE nodes ADD COLUMN signature TEXT"},
	{"nodes", "community_id", "ALTER TABLE nodes ADD COLUMN community_id INTEGER"},
	{"edges", "confidence", "ALTER TABLE edges ADD COLUMN confidence REAL DEFAULT 1.0"},
	{"edges", "confidence_tier", "ALTER TABLE edges ADD COLUMN confidence_tier TEXT DEFAULT 'EXTRACTED'"},
}

// migrateCodeGraphSchema brings an existing native database forward to
// schema v9. It is idempotent: each column addition is guarded on
// pragma_table_info and the FTS table on IF NOT EXISTS, so re-running it on
// an already-v9 database executes no DDL.
//
// Unlike upstream it is not gated on the recorded schema_version. Upstream
// short-circuits when `schema_version >= 9`; a native database predating
// this change has NO schema_version row at all, and a database written by
// an older `da` could have the tables but not the columns. Probing the
// actual schema is both cheaper to reason about and strictly safer than
// trusting a version stamp that has never been written before now.
func (s *SQLiteStore) migrateCodeGraphSchema() error {
	for _, c := range codeGraphAddedColumns {
		has, err := hasColumn(s.db, c.table, c.column)
		if err != nil {
			return fmt.Errorf("graphstore: probe %s.%s: %w", c.table, c.column, err)
		}
		if has {
			continue
		}
		if _, err := dbExec(s.db, c.ddl); err != nil {
			return fmt.Errorf("graphstore: add %s.%s: %w", c.table, c.column, err)
		}
	}
	// nodes_fts projects nodes.signature and idx_nodes_community indexes
	// nodes.community_id, so both can only be declared once the columns
	// above exist.
	if _, err := dbExec(s.db, ftsSchemaSQL); err != nil {
		return fmt.Errorf("graphstore: create nodes_fts: %w", err)
	}
	if _, err := dbExec(s.db, postMigrationSchemaSQL); err != nil {
		return fmt.Errorf("graphstore: create post-migration indexes: %w", err)
	}
	return nil
}

// hasColumn reports whether table has a column named column. It uses the
// pragma_table_info table-valued function so the table name is a bound
// parameter rather than interpolated SQL.
func hasColumn(db *sql.DB, table, column string) (bool, error) {
	var one int
	err := db.QueryRow(
		"SELECT 1 FROM pragma_table_info(?) WHERE name = ?", table, column,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
