package graphstore

// schemaSQL is the canonical DDL for the graphstore SQLite database.
// It mirrors the Python code-review-graph schema and adds KG-specific tables.
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

-- Code graph indexes
CREATE INDEX IF NOT EXISTS idx_nodes_file      ON nodes(file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_kind      ON nodes(kind);
CREATE INDEX IF NOT EXISTS idx_nodes_qualified ON nodes(qualified_name);
CREATE INDEX IF NOT EXISTS idx_edges_source    ON edges(source_qualified);
CREATE INDEX IF NOT EXISTS idx_edges_target    ON edges(target_qualified);
CREATE INDEX IF NOT EXISTS idx_edges_kind      ON edges(kind);
CREATE INDEX IF NOT EXISTS idx_edges_file      ON edges(file_path);

-- KG indexes
CREATE INDEX IF NOT EXISTS idx_kg_notes_type       ON kg_notes(note_type);
CREATE INDEX IF NOT EXISTS idx_kg_notes_status     ON kg_notes(status);
CREATE INDEX IF NOT EXISTS idx_kg_notes_archived   ON kg_notes(archived_at);
CREATE INDEX IF NOT EXISTS idx_nsl_note_id         ON note_symbol_links(note_id);
CREATE INDEX IF NOT EXISTS idx_nsl_qualified       ON note_symbol_links(qualified_name);
`
