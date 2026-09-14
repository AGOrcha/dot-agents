package graphstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the Postgres-backed implementation of Store.
// It uses pgxpool for connection pooling and is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// OpenPostgres connects to a Postgres database at dsn (a libpq-style connection
// string or URL, e.g. "postgres://user:pass@host:5432/dbname") and initialises
// the schema.
func OpenPostgres(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("graphstore: open postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("graphstore: ping postgres: %w", err)
	}

	s := &PostgresStore{pool: pool}
	if err := s.initSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// pgSchemaSQL is the Postgres DDL for the graphstore schema.
// Differences from SQLite:
//   - BIGSERIAL instead of INTEGER PRIMARY KEY AUTOINCREMENT
//   - BOOLEAN instead of INTEGER for is_test
//   - DOUBLE PRECISION instead of REAL for float columns
//   - tsvector/GIN index for full-text search on nodes
//   - ON CONFLICT syntax is identical (pgx supports standard SQL upserts)
const pgSchemaSQL = `
CREATE TABLE IF NOT EXISTS nodes (
    id              BIGSERIAL PRIMARY KEY,
    kind            TEXT          NOT NULL,
    name            TEXT          NOT NULL,
    qualified_name  TEXT          NOT NULL UNIQUE,
    file_path       TEXT          NOT NULL,
    line_start      INTEGER,
    line_end        INTEGER,
    language        TEXT,
    parent_name     TEXT,
    params          TEXT,
    return_type     TEXT,
    modifiers       TEXT,
    is_test         BOOLEAN       NOT NULL DEFAULT FALSE,
    file_hash       TEXT,
    extra           TEXT          NOT NULL DEFAULT '{}',
    updated_at      DOUBLE PRECISION NOT NULL,
    signature       TEXT,
    community_id    BIGINT
);

CREATE TABLE IF NOT EXISTS edges (
    id               BIGSERIAL PRIMARY KEY,
    kind             TEXT          NOT NULL,
    source_qualified TEXT          NOT NULL,
    target_qualified TEXT          NOT NULL,
    file_path        TEXT          NOT NULL,
    line             INTEGER       NOT NULL DEFAULT 0,
    extra            TEXT          NOT NULL DEFAULT '{}',
    updated_at       DOUBLE PRECISION NOT NULL,
    confidence       DOUBLE PRECISION DEFAULT 1.0,
    confidence_tier  TEXT          DEFAULT 'EXTRACTED'
);

CREATE TABLE IF NOT EXISTS metadata (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS kg_notes (
    id          TEXT PRIMARY KEY,
    title       TEXT             NOT NULL,
    note_type   TEXT             NOT NULL,
    status      TEXT             NOT NULL,
    summary     TEXT             NOT NULL DEFAULT '',
    file_path   TEXT             NOT NULL,
    version     INTEGER          NOT NULL DEFAULT 0,
    archived_at TEXT             NOT NULL DEFAULT '',
    indexed_at  DOUBLE PRECISION NOT NULL
);

CREATE TABLE IF NOT EXISTS note_symbol_links (
    id             BIGSERIAL PRIMARY KEY,
    note_id        TEXT             NOT NULL,
    qualified_name TEXT             NOT NULL,
    link_kind      TEXT             NOT NULL DEFAULT 'mentions',
    created_at     DOUBLE PRECISION NOT NULL,
    UNIQUE(note_id, qualified_name, link_kind)
);

-- Derived views (upstream schema v3-v6). Same columns, constraints and
-- defaults as the SQLite DDL in migrations.go, spelled in Postgres types:
-- BIGSERIAL for AUTOINCREMENT, DOUBLE PRECISION for REAL, now() for
-- datetime('now'). There is deliberately NO nodes_fts equivalent -- see
-- the FTS methods below.
CREATE TABLE IF NOT EXISTS flows (
    id             BIGSERIAL PRIMARY KEY,
    name           TEXT    NOT NULL,
    entry_point_id BIGINT  NOT NULL,
    depth          INTEGER NOT NULL,
    node_count     INTEGER NOT NULL,
    file_count     INTEGER NOT NULL,
    criticality    DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    path_json      TEXT    NOT NULL,
    created_at     TEXT    NOT NULL DEFAULT (now()::text),
    updated_at     TEXT    NOT NULL DEFAULT (now()::text)
);

CREATE TABLE IF NOT EXISTS flow_memberships (
    flow_id  BIGINT  NOT NULL,
    node_id  BIGINT  NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (flow_id, node_id)
);

CREATE TABLE IF NOT EXISTS communities (
    id                BIGSERIAL PRIMARY KEY,
    name              TEXT    NOT NULL,
    level             INTEGER NOT NULL DEFAULT 0,
    parent_id         BIGINT,
    cohesion          DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    size              INTEGER NOT NULL DEFAULT 0,
    dominant_language TEXT,
    description       TEXT,
    created_at        TEXT    NOT NULL DEFAULT (now()::text)
);

CREATE TABLE IF NOT EXISTS community_summaries (
    community_id      BIGINT PRIMARY KEY,
    name              TEXT   NOT NULL,
    purpose           TEXT    DEFAULT '',
    key_symbols       TEXT    DEFAULT '[]',
    risk              TEXT    DEFAULT 'unknown',
    size              INTEGER DEFAULT 0,
    dominant_language TEXT    DEFAULT ''
);

CREATE TABLE IF NOT EXISTS flow_snapshots (
    flow_id       BIGINT PRIMARY KEY,
    name          TEXT   NOT NULL,
    entry_point   TEXT   NOT NULL,
    critical_path TEXT    DEFAULT '[]',
    criticality   DOUBLE PRECISION DEFAULT 0.0,
    node_count    INTEGER DEFAULT 0,
    file_count    INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS risk_index (
    node_id           BIGINT PRIMARY KEY,
    qualified_name    TEXT   NOT NULL,
    risk_score        DOUBLE PRECISION DEFAULT 0.0,
    caller_count      INTEGER DEFAULT 0,
    test_coverage     TEXT    DEFAULT 'unknown',
    security_relevant INTEGER DEFAULT 0,
    last_computed     TEXT    DEFAULT ''
);

-- Code graph indexes
CREATE INDEX IF NOT EXISTS idx_nodes_file      ON nodes(file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_kind      ON nodes(kind);
CREATE INDEX IF NOT EXISTS idx_nodes_qualified ON nodes(qualified_name);
CREATE INDEX IF NOT EXISTS idx_edges_source    ON edges(source_qualified);
CREATE INDEX IF NOT EXISTS idx_edges_target    ON edges(target_qualified);
CREATE INDEX IF NOT EXISTS idx_edges_kind      ON edges(kind);
CREATE INDEX IF NOT EXISTS idx_edges_file      ON edges(file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_community ON nodes(community_id);
CREATE INDEX IF NOT EXISTS idx_edges_target_kind ON edges(target_qualified, kind);
CREATE INDEX IF NOT EXISTS idx_edges_source_kind ON edges(source_qualified, kind);
CREATE INDEX IF NOT EXISTS idx_edges_composite  ON edges(kind, source_qualified, target_qualified, file_path, line);
CREATE INDEX IF NOT EXISTS idx_flows_criticality ON flows(criticality DESC);
CREATE INDEX IF NOT EXISTS idx_flows_entry      ON flows(entry_point_id);
CREATE INDEX IF NOT EXISTS idx_flow_memberships_node ON flow_memberships(node_id);
CREATE INDEX IF NOT EXISTS idx_communities_parent ON communities(parent_id);
CREATE INDEX IF NOT EXISTS idx_communities_cohesion ON communities(cohesion DESC);
CREATE INDEX IF NOT EXISTS idx_risk_index_score ON risk_index(risk_score DESC);

-- KG indexes
CREATE INDEX IF NOT EXISTS idx_kg_notes_type     ON kg_notes(note_type);
CREATE INDEX IF NOT EXISTS idx_kg_notes_status   ON kg_notes(status);
CREATE INDEX IF NOT EXISTS idx_kg_notes_archived ON kg_notes(archived_at);
CREATE INDEX IF NOT EXISTS idx_nsl_note_id       ON note_symbol_links(note_id);
CREATE INDEX IF NOT EXISTS idx_nsl_qualified     ON note_symbol_links(qualified_name);
`

func (s *PostgresStore) initSchema(ctx context.Context) error {
	// Execute each statement individually; pgx doesn't support multi-statement
	// Exec in a single call unless using simple protocol.
	stmts := splitPGStatements(pgSchemaSQL)
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("graphstore: pg init schema: %w (stmt: %.80s)", err, stmt)
		}
	}
	return nil
}

// splitPGStatements splits a multi-statement SQL string on semicolons,
// returning non-empty trimmed statements.
func splitPGStatements(sql string) []string {
	parts := strings.Split(sql, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Close closes the connection pool.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

// Commit is a no-op for PostgresStore — writes auto-commit individually.
func (s *PostgresStore) Commit() error { return nil }

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func (s *PostgresStore) SetMetadata(key, value string) error {
	// gcc4: route through provider-owned request timeout for cross-provider
	// uniformity (CONTRACT.md guarantee #2). Path-A deadline applies to every
	// pool operation, not only the long traversals.
	ctx, cancel := requestContext(nil)
	defer cancel()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO metadata (key, value) VALUES ($1, $2)
		 ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`,
		key, value,
	)
	return err
}

func (s *PostgresStore) GetMetadata(key string) (string, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	var val string
	err := s.pool.QueryRow(ctx,
		"SELECT value FROM metadata WHERE key=$1", key,
	).Scan(&val)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return val, err
}

// ---------------------------------------------------------------------------
// Code graph — write
// ---------------------------------------------------------------------------

func (s *PostgresStore) UpsertNode(node NodeInfo, fileHash string) (int64, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	now := float64(time.Now().UnixNano()) / 1e9
	qualified := makeQualified(node)
	extra, err := encodeExtra(node.Extra)
	if err != nil {
		return 0, err
	}

	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO nodes
		  (kind, name, qualified_name, file_path, line_start, line_end,
		   language, parent_name, params, return_type, modifiers, is_test,
		   file_hash, extra, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT(qualified_name) DO UPDATE SET
		  kind=EXCLUDED.kind, name=EXCLUDED.name,
		  file_path=EXCLUDED.file_path,
		  line_start=EXCLUDED.line_start, line_end=EXCLUDED.line_end,
		  language=EXCLUDED.language, parent_name=EXCLUDED.parent_name,
		  params=EXCLUDED.params, return_type=EXCLUDED.return_type,
		  modifiers=EXCLUDED.modifiers, is_test=EXCLUDED.is_test,
		  file_hash=EXCLUDED.file_hash, extra=EXCLUDED.extra,
		  updated_at=EXCLUDED.updated_at
		RETURNING id`,
		node.Kind, node.Name, qualified, node.FilePath,
		node.LineStart, node.LineEnd, node.Language,
		node.ParentName, node.Params, node.ReturnType, node.Modifiers,
		node.IsTest, fileHash, extra, now,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("graphstore: upsert node %q: %w", qualified, err)
	}
	return id, nil
}

func (s *PostgresStore) UpsertEdge(edge EdgeInfo) (int64, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	now := float64(time.Now().UnixNano()) / 1e9
	extra, err := encodeExtra(edge.Extra)
	if err != nil {
		return 0, err
	}

	// Check for existing edge
	var existingID int64
	err = s.pool.QueryRow(ctx,
		`SELECT id FROM edges
		 WHERE kind=$1 AND source_qualified=$2 AND target_qualified=$3 AND file_path=$4`,
		edge.Kind, edge.Source, edge.Target, edge.FilePath,
	).Scan(&existingID)

	if err == nil {
		// update existing
		_, err = s.pool.Exec(ctx,
			"UPDATE edges SET line=$1, extra=$2, updated_at=$3 WHERE id=$4",
			edge.Line, extra, now, existingID,
		)
		return existingID, err
	}
	if err != pgx.ErrNoRows {
		return 0, fmt.Errorf("graphstore: lookup edge: %w", err)
	}

	// Insert new
	var id int64
	err = s.pool.QueryRow(ctx,
		`INSERT INTO edges
		 (kind, source_qualified, target_qualified, file_path, line, extra, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING id`,
		edge.Kind, edge.Source, edge.Target, edge.FilePath, edge.Line, extra, now,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("graphstore: insert edge: %w", err)
	}
	return id, nil
}

func (s *PostgresStore) RemoveFileData(filePath string) error {
	ctx, cancel := requestContext(nil)
	defer cancel()
	if _, err := s.pool.Exec(ctx, "DELETE FROM nodes WHERE file_path=$1", filePath); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, "DELETE FROM edges WHERE file_path=$1", filePath)
	return err
}

// StoreFileNodesEdges atomically replaces all nodes and edges for a file.
func (s *PostgresStore) StoreFileNodesEdges(filePath string, nodes []NodeInfo, edges []EdgeInfo, fileHash string) error {
	ctx, cancel := requestContext(nil)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("graphstore: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, "DELETE FROM nodes WHERE file_path=$1", filePath); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM edges WHERE file_path=$1", filePath); err != nil {
		return err
	}

	now := float64(time.Now().UnixNano()) / 1e9

	for _, node := range nodes {
		qualified := makeQualified(node)
		extra, err := encodeExtra(node.Extra)
		if err != nil {
			return fmt.Errorf("graphstore: encode extra for node %q: %w", qualified, err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO nodes
			  (kind, name, qualified_name, file_path, line_start, line_end,
			   language, parent_name, params, return_type, modifiers, is_test,
			   file_hash, extra, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT(qualified_name) DO UPDATE SET
			  kind=EXCLUDED.kind, name=EXCLUDED.name,
			  file_path=EXCLUDED.file_path,
			  line_start=EXCLUDED.line_start, line_end=EXCLUDED.line_end,
			  language=EXCLUDED.language, parent_name=EXCLUDED.parent_name,
			  params=EXCLUDED.params, return_type=EXCLUDED.return_type,
			  modifiers=EXCLUDED.modifiers, is_test=EXCLUDED.is_test,
			  file_hash=EXCLUDED.file_hash, extra=EXCLUDED.extra,
			  updated_at=EXCLUDED.updated_at`,
			node.Kind, node.Name, qualified, node.FilePath,
			node.LineStart, node.LineEnd, node.Language,
			node.ParentName, node.Params, node.ReturnType, node.Modifiers,
			node.IsTest, fileHash, extra, now,
		)
		if err != nil {
			return fmt.Errorf("graphstore: store node %q: %w", qualified, err)
		}
	}

	for _, edge := range edges {
		extra, err := encodeExtra(edge.Extra)
		if err != nil {
			return fmt.Errorf("graphstore: encode extra for edge %s->%s: %w", edge.Source, edge.Target, err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO edges
			  (kind, source_qualified, target_qualified, file_path, line, extra, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			edge.Kind, edge.Source, edge.Target, edge.FilePath, edge.Line, extra, now,
		)
		if err != nil {
			return fmt.Errorf("graphstore: store edge: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// Code graph — read
// ---------------------------------------------------------------------------

func (s *PostgresStore) GetNode(qualifiedName string) (*GraphNode, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	row := s.pool.QueryRow(ctx, `
		SELECT id, kind, name, qualified_name, file_path, line_start, line_end,
		       language, parent_name, params, return_type, modifiers, is_test,
		       file_hash, extra, updated_at, signature, community_id
		FROM nodes WHERE qualified_name=$1`, qualifiedName)
	n, err := pgScanNode(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return n, err
}

func (s *PostgresStore) GetNodesByFile(filePath string) ([]GraphNode, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, name, qualified_name, file_path, line_start, line_end,
		       language, parent_name, params, return_type, modifiers, is_test,
		       file_hash, extra, updated_at, signature, community_id
		FROM nodes WHERE file_path=$1`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectNodes(rows)
}

func (s *PostgresStore) GetEdgesBySource(qualifiedName string) ([]GraphEdge, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, source_qualified, target_qualified, file_path, line, extra, updated_at,
		       confidence, confidence_tier
		FROM edges WHERE source_qualified=$1`, qualifiedName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectEdges(rows)
}

func (s *PostgresStore) GetEdgesByTarget(qualifiedName string) ([]GraphEdge, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, source_qualified, target_qualified, file_path, line, extra, updated_at,
		       confidence, confidence_tier
		FROM edges WHERE target_qualified=$1`, qualifiedName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectEdges(rows)
}

func (s *PostgresStore) GetEdgesAmong(qualifiedNames []string) ([]GraphEdge, error) {
	if len(qualifiedNames) == 0 {
		return nil, nil
	}
	qnSet := make(map[string]bool, len(qualifiedNames))
	for _, q := range qualifiedNames {
		qnSet[q] = true
	}

	ctx, cancel := requestContext(nil)
	defer cancel()

	// Postgres supports $1 = ANY($2) for array membership — no batching needed.
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, source_qualified, target_qualified, file_path, line, extra, updated_at,
		       confidence, confidence_tier
		FROM edges WHERE source_qualified = ANY($1)`,
		qualifiedNames,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all, err := pgCollectEdges(rows)
	if err != nil {
		return nil, err
	}

	// Filter to only edges where target is also in the set.
	var result []GraphEdge
	for _, e := range all {
		if qnSet[e.TargetQualified] {
			result = append(result, e)
		}
	}
	return result, nil
}

func (s *PostgresStore) GetAllFiles() ([]string, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx, "SELECT DISTINCT file_path FROM nodes WHERE kind='File'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// SearchNodes performs a case-insensitive LIKE search on name and qualified_name.
// For production workloads with large graphs, consider adding a tsvector GIN
// index and using to_tsquery instead.
func (s *PostgresStore) SearchNodes(query string, limit int) ([]GraphNode, error) {
	limit = normalizeSearchLimit(limit)
	ctx, cancel := requestContext(nil)
	defer cancel()
	pattern := "%" + query + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, name, qualified_name, file_path, line_start, line_end,
		       language, parent_name, params, return_type, modifiers, is_test,
		       file_hash, extra, updated_at, signature, community_id
		FROM nodes WHERE name ILIKE $1 OR qualified_name ILIKE $2
		LIMIT $3`,
		pattern, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectNodes(rows)
}

func (s *PostgresStore) GetStats() (GraphStats, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	var stats GraphStats

	if err := s.scanStatsCounts(ctx, &stats); err != nil {
		return stats, err
	}

	stats.NodesByKind = map[string]int{}
	if err := s.collectKindCounts(ctx, "SELECT kind, COUNT(*) FROM nodes GROUP BY kind", stats.NodesByKind); err != nil {
		return stats, err
	}

	stats.EdgesByKind = map[string]int{}
	if err := s.collectKindCounts(ctx, "SELECT kind, COUNT(*) FROM edges GROUP BY kind", stats.EdgesByKind); err != nil {
		return stats, err
	}

	languages, err := s.collectLanguages(ctx)
	if err != nil {
		return stats, err
	}
	stats.Languages = languages

	stats.LastUpdated, _ = s.GetMetadata("last_updated")
	return stats, nil
}

// scanStatsCounts populates the scalar count fields on stats by running each
// COUNT(*) query in turn. Returns the first error encountered.
func (s *PostgresStore) scanStatsCounts(ctx context.Context, stats *GraphStats) error {
	queries := []struct {
		query string
		dst   *int
	}{
		{"SELECT COUNT(*) FROM nodes", &stats.TotalNodes},
		{"SELECT COUNT(*) FROM edges", &stats.TotalEdges},
		{"SELECT COUNT(*) FROM nodes WHERE kind='File'", &stats.FilesCount},
		{"SELECT COUNT(*) FROM kg_notes", &stats.NotesCount},
		{"SELECT COUNT(*) FROM note_symbol_links", &stats.LinksCount},
	}
	for _, q := range queries {
		if err := s.pool.QueryRow(ctx, q.query).Scan(q.dst); err != nil {
			return err
		}
	}
	return nil
}

// collectKindCounts executes a "SELECT kind, COUNT(*) ..." style query and
// fills dst with the resulting kind→count pairs.
func (s *PostgresStore) collectKindCounts(ctx context.Context, query string, dst map[string]int) error {
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var c int
		if err := rows.Scan(&k, &c); err != nil {
			return err
		}
		dst[k] = c
	}
	return rows.Err()
}

// collectLanguages returns the distinct non-empty language values stored on
// nodes.
func (s *PostgresStore) collectLanguages(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT DISTINCT language FROM nodes WHERE language IS NOT NULL AND language != ''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var languages []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		languages = append(languages, l)
	}
	return languages, rows.Err()
}

// GetImpactRadius performs a pure-Go BFS from the nodes in changedFiles,
// traversing both outbound and inbound edges up to maxDepth hops.
// Postgres-specific: uses pgx for the seed/edge queries; the BFS body
// is shared with the SQLite backend via computeImpactRadius (impact.go).
func (s *PostgresStore) GetImpactRadius(changedFiles []string, maxDepth, maxNodes int) (ImpactResult, error) {
	// Provider-owned request timeout (CONTRACT.md guarantee #2). Bounds
	// are clamped uniformly in computeImpactRadius.
	ctx, cancel := requestContext(nil)
	defer cancel()

	seeds := map[string]bool{}
	for _, f := range changedFiles {
		nodes, err := s.GetNodesByFile(f)
		if err != nil {
			return ImpactResult{}, err
		}
		for _, n := range nodes {
			seeds[n.QualifiedName] = true
		}
	}

	rows, err := s.pool.Query(ctx, "SELECT source_qualified, target_qualified FROM edges")
	if err != nil {
		return ImpactResult{}, err
	}
	fwd, rev, err := buildEdgeAdjacency(rows)
	rows.Close()
	if err != nil {
		return ImpactResult{}, err
	}

	return computeImpactRadius(seeds, fwd, rev, maxDepth, maxNodes, s)
}

// ---------------------------------------------------------------------------
// KG notes
// ---------------------------------------------------------------------------

func (s *PostgresStore) UpsertKGNote(note KGNote) error {
	ctx, cancel := requestContext(nil)
	defer cancel()
	now := float64(time.Now().UnixNano()) / 1e9
	if note.IndexedAt == 0 {
		note.IndexedAt = now
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO kg_notes
		  (id, title, note_type, status, summary, file_path, version, archived_at, indexed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT(id) DO UPDATE SET
		  title=EXCLUDED.title, note_type=EXCLUDED.note_type,
		  status=EXCLUDED.status, summary=EXCLUDED.summary,
		  file_path=EXCLUDED.file_path, version=EXCLUDED.version,
		  archived_at=EXCLUDED.archived_at, indexed_at=EXCLUDED.indexed_at`,
		note.ID, note.Title, note.NoteType, note.Status, note.Summary,
		note.FilePath, note.Version, note.ArchivedAt, note.IndexedAt,
	)
	return err
}

func (s *PostgresStore) GetKGNote(id string) (*KGNote, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	note := &KGNote{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, title, note_type, status, summary, file_path, version, archived_at, indexed_at
		 FROM kg_notes WHERE id=$1`, id,
	).Scan(&note.ID, &note.Title, &note.NoteType, &note.Status,
		&note.Summary, &note.FilePath, &note.Version, &note.ArchivedAt, &note.IndexedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return note, err
}

func (s *PostgresStore) SearchKGNotes(query string, limit int) ([]KGNote, error) {
	limit = normalizeSearchLimit(limit)
	ctx, cancel := requestContext(nil)
	defer cancel()
	pattern := "%" + query + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, note_type, status, summary, file_path, version, archived_at, indexed_at
		FROM kg_notes
		WHERE (title ILIKE $1 OR summary ILIKE $2) AND archived_at=''
		LIMIT $3`,
		pattern, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectNotes(rows)
}

func (s *PostgresStore) ListArchivedKGNotes() ([]KGNote, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, note_type, status, summary, file_path, version, archived_at, indexed_at
		FROM kg_notes WHERE archived_at != '' ORDER BY archived_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectNotes(rows)
}

// ---------------------------------------------------------------------------
// Note→symbol links
// ---------------------------------------------------------------------------

func (s *PostgresStore) UpsertNoteSymbolLink(link NoteSymbolLink) (int64, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	now := float64(time.Now().UnixNano()) / 1e9
	if link.CreatedAt == 0 {
		link.CreatedAt = now
	}

	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO note_symbol_links (note_id, qualified_name, link_kind, created_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT(note_id, qualified_name, link_kind) DO NOTHING
		RETURNING id`,
		link.NoteID, link.QualifiedName, link.LinkKind, link.CreatedAt,
	).Scan(&id)

	if err == pgx.ErrNoRows {
		// Conflict — already exists; look up the existing id.
		err = s.pool.QueryRow(ctx,
			"SELECT id FROM note_symbol_links WHERE note_id=$1 AND qualified_name=$2 AND link_kind=$3",
			link.NoteID, link.QualifiedName, link.LinkKind,
		).Scan(&id)
	}
	return id, err
}

func (s *PostgresStore) GetLinksForNote(noteID string) ([]NoteSymbolLink, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		"SELECT id, note_id, qualified_name, link_kind, created_at FROM note_symbol_links WHERE note_id=$1",
		noteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectLinks(rows)
}

func (s *PostgresStore) GetLinksForSymbol(qualifiedName string) ([]NoteSymbolLink, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		"SELECT id, note_id, qualified_name, link_kind, created_at FROM note_symbol_links WHERE qualified_name=$1",
		qualifiedName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectLinks(rows)
}

func (s *PostgresStore) DeleteNoteSymbolLink(id int64) error {
	ctx, cancel := requestContext(nil)
	defer cancel()
	_, err := s.pool.Exec(ctx,
		"DELETE FROM note_symbol_links WHERE id=$1", id,
	)
	return err
}

// ---------------------------------------------------------------------------
// Internal scan helpers
// ---------------------------------------------------------------------------

type pgRowScanner interface {
	Scan(dest ...any) error
}

func pgScanNode(row pgRowScanner) (*GraphNode, error) {
	var n GraphNode
	var extraStr string
	var modifiers *string
	var signature *string
	var communityID *int64
	err := row.Scan(
		&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath,
		&n.LineStart, &n.LineEnd, &n.Language, &n.ParentName,
		&n.Params, &n.ReturnType, &modifiers, &n.IsTest,
		&n.FileHash, &extraStr, &n.UpdatedAt, &signature, &communityID,
	)
	if err != nil {
		return nil, err
	}
	n.Extra = decodeExtra(extraStr)
	n.Signature = derefString(signature)
	n.CommunityID = derefInt64(communityID)
	return &n, nil
}

// derefString / derefInt64 read a nullable Postgres column into the
// GraphNode zero value ("" / 0), matching the SQLite backend's sql.Null*
// handling of the same two nullable columns.
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func pgCollectNodes(rows pgx.Rows) ([]GraphNode, error) {
	var result []GraphNode
	for rows.Next() {
		n, err := pgScanNode(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *n)
	}
	return result, rows.Err()
}

func pgCollectEdges(rows pgx.Rows) ([]GraphEdge, error) {
	var result []GraphEdge
	for rows.Next() {
		var e GraphEdge
		var extraStr string
		var confidence *float64
		var tier *string
		err := rows.Scan(
			&e.ID, &e.Kind, &e.SourceQualified, &e.TargetQualified,
			&e.FilePath, &e.Line, &extraStr, &e.UpdatedAt,
			&confidence, &tier,
		)
		if err != nil {
			return nil, err
		}
		e.Extra = decodeExtra(extraStr)
		if confidence != nil {
			e.Confidence = *confidence
		}
		e.ConfidenceTier = derefString(tier)
		result = append(result, e)
	}
	return result, rows.Err()
}

func pgCollectNotes(rows pgx.Rows) ([]KGNote, error) {
	var result []KGNote
	for rows.Next() {
		var n KGNote
		if err := rows.Scan(&n.ID, &n.Title, &n.NoteType, &n.Status,
			&n.Summary, &n.FilePath, &n.Version, &n.ArchivedAt, &n.IndexedAt); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

func pgCollectLinks(rows pgx.Rows) ([]NoteSymbolLink, error) {
	var result []NoteSymbolLink
	for rows.Next() {
		var l NoteSymbolLink
		if err := rows.Scan(&l.ID, &l.NoteID, &l.QualifiedName, &l.LinkKind, &l.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, rows.Err()
}

// pgEncodeExtra encodes a map to JSON. Kept for clarity (uses the shared helper).
func pgEncodeExtra(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

// ---------------------------------------------------------------------------
// Code graph — derived views (CodeGraphDerived)
// ---------------------------------------------------------------------------
//
// Postgres hosts the six derived TABLES natively: they are plain relational
// tables and the replace-writers below are the same DELETE + INSERT inside one
// transaction the SQLite backend runs, so the atomic-generation-swap guarantee
// holds identically.
//
// It cannot host the nodes_fts index. That index is a SQLite FTS5
// EXTERNAL-CONTENT virtual table: it stores no rows of its own, projects
// nodes(name, qualified_name, file_path, signature) by rowid, ranks with FTS5's
// BM25 `rank` column, and is repopulated by the FTS5-specific
// `INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild')` command. Postgres has
// none of that: its full-text story is tsvector + GIN with different
// tokenizing and a different (ts_rank) score, so an emulation would silently
// return a DIFFERENT result set and rank order for the same query — exactly
// the divergence the parity contract exists to prevent. RebuildFTS and
// SearchNodesFTS therefore fail with ErrFTSUnsupported, which callers can
// detect with errors.Is and fall back from, rather than pretending to index
// zero rows or returning an empty match list.
//
// SearchNodes (CodeGraphReader) remains available on both backends and is the
// portable substring search; it is what a caller should degrade to.

// pgDerivedTx runs fn in one transaction, rolling back on error. It is the
// Postgres equivalent of SQLiteStore.derivedTx; there is no foreign-key
// suspension because the Postgres derived DDL deliberately declares no FKs
// between the derived tables — upstream's declarations are inert (Python's
// sqlite3 leaves enforcement off) and reproducing an inert constraint as an
// ENFORCED one would break the documented generation-swap semantics.
func (s *PostgresStore) pgDerivedTx(fn func(ctx context.Context, tx pgx.Tx) error) error {
	ctx, cancel := requestContext(nil)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) ReadFlows() ([]FlowRow, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, entry_point_id, depth, node_count, file_count,
		        criticality, path_json, created_at, updated_at
		 FROM flows ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FlowRow
	for rows.Next() {
		var f FlowRow
		if err := rows.Scan(&f.ID, &f.Name, &f.EntryPointID, &f.Depth,
			&f.NodeCount, &f.FileCount, &f.Criticality, &f.PathJSON,
			&f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ReadFlowMemberships() ([]FlowMembershipRow, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		"SELECT flow_id, node_id, position FROM flow_memberships ORDER BY flow_id, node_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FlowMembershipRow
	for rows.Next() {
		var m FlowMembershipRow
		if err := rows.Scan(&m.FlowID, &m.NodeID, &m.Position); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ReadCommunities() ([]CommunityRow, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, level, parent_id, cohesion, size,
		        dominant_language, description
		 FROM communities ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunityRow
	for rows.Next() {
		var c CommunityRow
		var parent *int64
		var lang, desc *string
		if err := rows.Scan(&c.ID, &c.Name, &c.Level, &parent, &c.Cohesion,
			&c.Size, &lang, &desc); err != nil {
			return nil, err
		}
		c.ParentID = parent
		c.DominantLanguage = derefString(lang)
		c.Description = derefString(desc)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ReadCommunitySummaries() ([]CommunitySummaryRow, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		`SELECT community_id, name, purpose, key_symbols, risk, size,
		        dominant_language
		 FROM community_summaries ORDER BY community_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunitySummaryRow
	for rows.Next() {
		var c CommunitySummaryRow
		if err := rows.Scan(&c.CommunityID, &c.Name, &c.Purpose, &c.KeySymbols,
			&c.Risk, &c.Size, &c.DominantLanguage); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ReadFlowSnapshots() ([]FlowSnapshotRow, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		`SELECT flow_id, name, entry_point, critical_path, criticality,
		        node_count, file_count
		 FROM flow_snapshots ORDER BY flow_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FlowSnapshotRow
	for rows.Next() {
		var f FlowSnapshotRow
		if err := rows.Scan(&f.FlowID, &f.Name, &f.EntryPoint, &f.CriticalPath,
			&f.Criticality, &f.NodeCount, &f.FileCount); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ReadRiskIndex() ([]RiskIndexRow, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		`SELECT node_id, qualified_name, risk_score, caller_count,
		        test_coverage, security_relevant, last_computed
		 FROM risk_index ORDER BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RiskIndexRow
	for rows.Next() {
		var r RiskIndexRow
		var sec int
		if err := rows.Scan(&r.NodeID, &r.QualifiedName, &r.RiskScore,
			&r.CallerCount, &r.TestCoverage, &sec, &r.LastComputed); err != nil {
			return nil, err
		}
		r.SecurityRelevant = sec != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// SearchNodesFTS is not available on Postgres. See the file-section comment:
// emulating an FTS5 BM25 external-content index with tsvector would return a
// different result set and rank order, so this reports the capability gap
// instead. Degrade to SearchNodes.
func (s *PostgresStore) SearchNodesFTS(string, int) ([]int64, error) {
	return nil, ErrFTSUnsupported
}

// SearchNodesFTSWords is not available on Postgres; see SearchNodesFTS.
func (s *PostgresStore) SearchNodesFTSWords([]string, int) ([]int64, error) {
	return nil, ErrFTSUnsupported
}

// CountNodesFTSWords is not available on Postgres; see SearchNodesFTS.
func (s *PostgresStore) CountNodesFTSWords([]string) (int, error) {
	return 0, ErrFTSUnsupported
}

// ApplyEdgeRewrites writes the recomputed endpoints and extra for each edge
// inside one transaction; see the SQLite implementation for why it is atomic.
func (s *PostgresStore) ApplyEdgeRewrites(rewrites []EdgeRewrite) (int, error) {
	if len(rewrites) == 0 {
		return 0, nil
	}
	err := s.pgDerivedTx(func(ctx context.Context, tx pgx.Tx) error {
		for _, r := range rewrites {
			extra, err := encodeExtra(r.Extra)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`UPDATE edges SET source_qualified = $1, target_qualified = $2, extra = $3
				 WHERE id = $4`,
				r.SourceQualified, r.TargetQualified, extra, r.EdgeID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(rewrites), nil
}

// RebuildFTS is not available on Postgres; there is no nodes_fts index to
// rebuild. Returning ErrFTSUnsupported rather than (0, nil) keeps "this
// backend has no full-text index" distinguishable from "the graph is empty".
func (s *PostgresStore) RebuildFTS() (int, error) {
	return 0, ErrFTSUnsupported
}

// pgTruncateFlowGeneration clears the previous flow generation. Memberships go
// first because they reference flows.
func pgTruncateFlowGeneration(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "DELETE FROM flow_memberships"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "DELETE FROM flows")
	return err
}

// pgInsertFlow writes one flow and the membership row for every node on its
// path, in path order so `position` matches the caller's ordering.
func pgInsertFlow(ctx context.Context, tx pgx.Tx, f FlowRow, path []int64) error {
	var flowID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO flows
		   (name, entry_point_id, depth, node_count, file_count,
		    criticality, path_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		f.Name, f.EntryPointID, f.Depth, f.NodeCount, f.FileCount,
		f.Criticality, encodeIDPath(path),
	).Scan(&flowID); err != nil {
		return err
	}
	for position, nodeID := range path {
		if _, err := tx.Exec(ctx,
			`INSERT INTO flow_memberships (flow_id, node_id, position)
			 VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			flowID, nodeID, position); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) ReplaceFlows(flows []FlowRow, paths [][]int64) (int, error) {
	if len(paths) != len(flows) {
		return 0, fmt.Errorf("graphstore: ReplaceFlows got %d flows but %d paths", len(flows), len(paths))
	}
	count := 0
	err := s.pgDerivedTx(func(ctx context.Context, tx pgx.Tx) error {
		if err := pgTruncateFlowGeneration(ctx, tx); err != nil {
			return err
		}
		for i, f := range flows {
			if err := pgInsertFlow(ctx, tx, f, paths[i]); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *PostgresStore) ReplaceCommunities(communities []CommunityRow, members [][]string) (int, error) {
	if len(members) != len(communities) {
		return 0, fmt.Errorf("graphstore: ReplaceCommunities got %d communities but %d member sets",
			len(communities), len(members))
	}
	count := 0
	err := s.pgDerivedTx(func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM communities"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE nodes SET community_id = NULL"); err != nil {
			return err
		}
		for i, c := range communities {
			var communityID int64
			if err := tx.QueryRow(ctx,
				`INSERT INTO communities
				   (name, level, cohesion, size, dominant_language, description)
				 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
				c.Name, c.Level, c.Cohesion, c.Size, c.DominantLanguage, c.Description,
			).Scan(&communityID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				"UPDATE nodes SET community_id = $1 WHERE qualified_name = ANY($2)",
				communityID, members[i]); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *PostgresStore) ReplaceCommunitySummaries(rows []CommunitySummaryRow) (int, error) {
	return s.pgReplaceRows("community_summaries", len(rows), func(ctx context.Context, tx pgx.Tx) error {
		for _, r := range rows {
			if _, err := tx.Exec(ctx,
				`INSERT INTO community_summaries
				   (community_id, name, purpose, key_symbols, size, dominant_language)
				 VALUES ($1, $2, $3, $4, $5, $6)
				 ON CONFLICT (community_id) DO UPDATE SET
				   name=EXCLUDED.name, purpose=EXCLUDED.purpose,
				   key_symbols=EXCLUDED.key_symbols, size=EXCLUDED.size,
				   dominant_language=EXCLUDED.dominant_language`,
				r.CommunityID, r.Name, r.Purpose, r.KeySymbols, r.Size, r.DominantLanguage,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PostgresStore) ReplaceFlowSnapshots(rows []FlowSnapshotRow) (int, error) {
	return s.pgReplaceRows("flow_snapshots", len(rows), func(ctx context.Context, tx pgx.Tx) error {
		for _, r := range rows {
			if _, err := tx.Exec(ctx,
				`INSERT INTO flow_snapshots
				   (flow_id, name, entry_point, critical_path, criticality,
				    node_count, file_count)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)
				 ON CONFLICT (flow_id) DO UPDATE SET
				   name=EXCLUDED.name, entry_point=EXCLUDED.entry_point,
				   critical_path=EXCLUDED.critical_path,
				   criticality=EXCLUDED.criticality,
				   node_count=EXCLUDED.node_count, file_count=EXCLUDED.file_count`,
				r.FlowID, r.Name, r.EntryPoint, r.CriticalPath, r.Criticality,
				r.NodeCount, r.FileCount,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PostgresStore) ReplaceRiskIndex(rows []RiskIndexRow) (int, error) {
	return s.pgReplaceRows("risk_index", len(rows), func(ctx context.Context, tx pgx.Tx) error {
		for _, r := range rows {
			if _, err := tx.Exec(ctx,
				`INSERT INTO risk_index
				   (node_id, qualified_name, risk_score, caller_count,
				    test_coverage, security_relevant, last_computed)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)
				 ON CONFLICT (node_id) DO UPDATE SET
				   qualified_name=EXCLUDED.qualified_name,
				   risk_score=EXCLUDED.risk_score,
				   caller_count=EXCLUDED.caller_count,
				   test_coverage=EXCLUDED.test_coverage,
				   security_relevant=EXCLUDED.security_relevant,
				   last_computed=EXCLUDED.last_computed`,
				r.NodeID, r.QualifiedName, r.RiskScore, r.CallerCount,
				r.TestCoverage, boolToInt(r.SecurityRelevant), r.LastComputed,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// pgReplaceRows is the DELETE-then-insert transaction shared by the three
// summary-table writers.
func (s *PostgresStore) pgReplaceRows(table string, n int, insert func(context.Context, pgx.Tx) error) (int, error) {
	err := s.pgDerivedTx(func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
		return insert(ctx, tx)
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (s *PostgresStore) SetNodeSignature(id int64, signature string) error {
	ctx, cancel := requestContext(nil)
	defer cancel()
	_, err := s.pool.Exec(ctx, "UPDATE nodes SET signature = $1 WHERE id = $2", signature, id)
	return err
}

func (s *PostgresStore) SetNodeCommunity(id, communityID int64) error {
	ctx, cancel := requestContext(nil)
	defer cancel()
	_, err := s.pool.Exec(ctx, "UPDATE nodes SET community_id = $1 WHERE id = $2", communityID, id)
	return err
}

func (s *PostgresStore) NodesWithoutSignature() ([]GraphNode, error) {
	return s.queryNodes("WHERE signature IS NULL ORDER BY id")
}

func (s *PostgresStore) ReadNodesByKind(kinds []string) ([]GraphNode, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	return s.queryNodes("WHERE kind = ANY($1) ORDER BY kind, id", kinds)
}

func (s *PostgresStore) ReadNodesByID(ids []int64) ([]GraphNode, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return s.queryNodes("WHERE id = ANY($1) ORDER BY id", ids)
}

func (s *PostgresStore) ReadNodesByCommunity(communityID int64) ([]GraphNode, error) {
	return s.queryNodes("WHERE community_id = $1 ORDER BY id", communityID)
}

func (s *PostgresStore) ReadAllNodes() ([]GraphNode, error) {
	return s.queryNodes("ORDER BY id")
}

func (s *PostgresStore) ReadAllEdges() ([]GraphEdge, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		`SELECT id, kind, source_qualified, target_qualified, file_path, line,
		        extra, updated_at, confidence, confidence_tier
		 FROM edges ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectEdges(rows)
}

// queryNodes runs a full-column nodes SELECT with the caller's trailing
// clause, so the column list (and therefore the pgScanNode destination
// order) is written exactly once.
func (s *PostgresStore) queryNodes(clause string, args ...any) ([]GraphNode, error) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.pool.Query(ctx,
		`SELECT id, kind, name, qualified_name, file_path, line_start, line_end,
		        language, parent_name, params, return_type, modifiers, is_test,
		        file_hash, extra, updated_at, signature, community_id
		 FROM nodes `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectNodes(rows)
}
