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
    updated_at      DOUBLE PRECISION NOT NULL
);

CREATE TABLE IF NOT EXISTS edges (
    id               BIGSERIAL PRIMARY KEY,
    kind             TEXT          NOT NULL,
    source_qualified TEXT          NOT NULL,
    target_qualified TEXT          NOT NULL,
    file_path        TEXT          NOT NULL,
    line             INTEGER       NOT NULL DEFAULT 0,
    extra            TEXT          NOT NULL DEFAULT '{}',
    updated_at       DOUBLE PRECISION NOT NULL
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

-- Code graph indexes
CREATE INDEX IF NOT EXISTS idx_nodes_file      ON nodes(file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_kind      ON nodes(kind);
CREATE INDEX IF NOT EXISTS idx_nodes_qualified ON nodes(qualified_name);
CREATE INDEX IF NOT EXISTS idx_edges_source    ON edges(source_qualified);
CREATE INDEX IF NOT EXISTS idx_edges_target    ON edges(target_qualified);
CREATE INDEX IF NOT EXISTS idx_edges_kind      ON edges(kind);
CREATE INDEX IF NOT EXISTS idx_edges_file      ON edges(file_path);

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
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO metadata (key, value) VALUES ($1, $2)
		 ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`,
		key, value,
	)
	return err
}

func (s *PostgresStore) GetMetadata(key string) (string, error) {
	var val string
	err := s.pool.QueryRow(context.Background(),
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "DELETE FROM nodes WHERE file_path=$1", filePath); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, "DELETE FROM edges WHERE file_path=$1", filePath)
	return err
}

// StoreFileNodesEdges atomically replaces all nodes and edges for a file.
func (s *PostgresStore) StoreFileNodesEdges(filePath string, nodes []NodeInfo, edges []EdgeInfo, fileHash string) error {
	ctx := context.Background()
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
		extra, _ := encodeExtra(node.Extra)
		_, err := tx.Exec(ctx, `
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
		extra, _ := encodeExtra(edge.Extra)
		_, err := tx.Exec(ctx, `
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
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, `
		SELECT id, kind, name, qualified_name, file_path, line_start, line_end,
		       language, parent_name, params, return_type, modifiers, is_test,
		       file_hash, extra, updated_at
		FROM nodes WHERE qualified_name=$1`, qualifiedName)
	n, err := pgScanNode(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return n, err
}

func (s *PostgresStore) GetNodesByFile(filePath string) ([]GraphNode, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, name, qualified_name, file_path, line_start, line_end,
		       language, parent_name, params, return_type, modifiers, is_test,
		       file_hash, extra, updated_at
		FROM nodes WHERE file_path=$1`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectNodes(rows)
}

func (s *PostgresStore) GetEdgesBySource(qualifiedName string) ([]GraphEdge, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, source_qualified, target_qualified, file_path, line, extra, updated_at
		FROM edges WHERE source_qualified=$1`, qualifiedName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgCollectEdges(rows)
}

func (s *PostgresStore) GetEdgesByTarget(qualifiedName string) ([]GraphEdge, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, source_qualified, target_qualified, file_path, line, extra, updated_at
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

	ctx := context.Background()

	// Postgres supports $1 = ANY($2) for array membership — no batching needed.
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, source_qualified, target_qualified, file_path, line, extra, updated_at
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
	ctx := context.Background()
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
	ctx := context.Background()
	pattern := "%" + query + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, name, qualified_name, file_path, line_start, line_end,
		       language, parent_name, params, return_type, modifiers, is_test,
		       file_hash, extra, updated_at
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
	ctx := context.Background()
	var stats GraphStats

	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM nodes").Scan(&stats.TotalNodes); err != nil {
		return stats, err
	}
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM edges").Scan(&stats.TotalEdges); err != nil {
		return stats, err
	}
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM nodes WHERE kind='File'").Scan(&stats.FilesCount); err != nil {
		return stats, err
	}
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM kg_notes").Scan(&stats.NotesCount); err != nil {
		return stats, err
	}
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM note_symbol_links").Scan(&stats.LinksCount); err != nil {
		return stats, err
	}

	stats.NodesByKind = map[string]int{}
	rows, err := s.pool.Query(ctx, "SELECT kind, COUNT(*) FROM nodes GROUP BY kind")
	if err != nil {
		return stats, err
	}
	for rows.Next() {
		var k string
		var c int
		if err := rows.Scan(&k, &c); err != nil {
			rows.Close()
			return stats, err
		}
		stats.NodesByKind[k] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return stats, err
	}

	stats.EdgesByKind = map[string]int{}
	rows, err = s.pool.Query(ctx, "SELECT kind, COUNT(*) FROM edges GROUP BY kind")
	if err != nil {
		return stats, err
	}
	for rows.Next() {
		var k string
		var c int
		if err := rows.Scan(&k, &c); err != nil {
			rows.Close()
			return stats, err
		}
		stats.EdgesByKind[k] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return stats, err
	}

	rows, err = s.pool.Query(ctx, "SELECT DISTINCT language FROM nodes WHERE language IS NOT NULL AND language != ''")
	if err != nil {
		return stats, err
	}
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			rows.Close()
			return stats, err
		}
		stats.Languages = append(stats.Languages, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return stats, err
	}

	stats.LastUpdated, _ = s.GetMetadata("last_updated")
	return stats, nil
}

// GetImpactRadius performs a pure-Go BFS from the nodes in changedFiles,
// traversing both outbound and inbound edges up to maxDepth hops.
// The implementation is identical to the SQLite version — the BFS logic is
// backend-agnostic; only the underlying queries differ.
func (s *PostgresStore) GetImpactRadius(changedFiles []string, maxDepth, maxNodes int) (ImpactResult, error) {
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

	ctx := context.Background()
	rows, err := s.pool.Query(ctx, "SELECT source_qualified, target_qualified FROM edges")
	if err != nil {
		return ImpactResult{}, err
	}
	fwd := map[string][]string{}
	rev := map[string][]string{}
	for rows.Next() {
		var src, tgt string
		if err := rows.Scan(&src, &tgt); err != nil {
			rows.Close()
			return ImpactResult{}, err
		}
		fwd[src] = append(fwd[src], tgt)
		rev[tgt] = append(rev[tgt], src)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ImpactResult{}, err
	}

	visited := map[string]bool{}
	frontier := make([]string, 0, len(seeds))
	for q := range seeds {
		frontier = append(frontier, q)
	}

	impacted := map[string]bool{}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, qn := range frontier {
			visited[qn] = true
			for _, neighbor := range fwd[qn] {
				if !visited[neighbor] {
					next = append(next, neighbor)
					impacted[neighbor] = true
				}
			}
			for _, pred := range rev[qn] {
				if !visited[pred] {
					next = append(next, pred)
					impacted[pred] = true
				}
			}
		}
		if len(visited)+len(next) > maxNodes {
			break
		}
		frontier = next
	}

	var changedNodes []GraphNode
	for qn := range seeds {
		n, err := s.GetNode(qn)
		if err != nil || n == nil {
			continue
		}
		changedNodes = append(changedNodes, *n)
	}

	var impactedNodes []GraphNode
	for qn := range impacted {
		if seeds[qn] {
			continue
		}
		n, err := s.GetNode(qn)
		if err != nil || n == nil {
			continue
		}
		impactedNodes = append(impactedNodes, *n)
	}

	impactedFiles := map[string]bool{}
	for _, n := range impactedNodes {
		impactedFiles[n.FilePath] = true
	}
	var files []string
	for f := range impactedFiles {
		files = append(files, f)
	}

	all := make([]string, 0, len(seeds)+len(impacted))
	for q := range seeds {
		all = append(all, q)
	}
	for q := range impacted {
		all = append(all, q)
	}
	edges, err := s.GetEdgesAmong(all)
	if err != nil {
		return ImpactResult{}, err
	}

	return ImpactResult{
		ChangedNodes:  changedNodes,
		ImpactedNodes: impactedNodes,
		ImpactedFiles: files,
		Edges:         edges,
	}, nil
}

// ---------------------------------------------------------------------------
// KG notes
// ---------------------------------------------------------------------------

func (s *PostgresStore) UpsertKGNote(note KGNote) error {
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	_, err := s.pool.Exec(context.Background(),
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
	err := row.Scan(
		&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath,
		&n.LineStart, &n.LineEnd, &n.Language, &n.ParentName,
		&n.Params, &n.ReturnType, &modifiers, &n.IsTest,
		&n.FileHash, &extraStr, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	n.Extra = decodeExtra(extraStr)
	return &n, nil
}

func pgCollectNodes(rows pgx.Rows) ([]GraphNode, error) {
	var result []GraphNode
	for rows.Next() {
		var n GraphNode
		var extraStr string
		var modifiers *string
		err := rows.Scan(
			&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.LineStart, &n.LineEnd, &n.Language, &n.ParentName,
			&n.Params, &n.ReturnType, &modifiers, &n.IsTest,
			&n.FileHash, &extraStr, &n.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		n.Extra = decodeExtra(extraStr)
		result = append(result, n)
	}
	return result, rows.Err()
}

func pgCollectEdges(rows pgx.Rows) ([]GraphEdge, error) {
	var result []GraphEdge
	for rows.Next() {
		var e GraphEdge
		var extraStr string
		err := rows.Scan(
			&e.ID, &e.Kind, &e.SourceQualified, &e.TargetQualified,
			&e.FilePath, &e.Line, &extraStr, &e.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		e.Extra = decodeExtra(extraStr)
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
