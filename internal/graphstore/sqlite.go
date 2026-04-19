package graphstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// SQLiteStore is the SQLite-backed implementation of Store.
type SQLiteStore struct {
	db *sql.DB
}

// OpenSQLite opens (or creates) the SQLite database at dbPath and initialises
// the schema. The parent directory is created if it does not exist.
func OpenSQLite(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("graphstore: create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("graphstore: open db: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite doesn't benefit from a pool

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("graphstore: set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("graphstore: set busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("graphstore: enable foreign_keys: %w", err)
	}

	s := &SQLiteStore{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) initSchema() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("graphstore: init schema: %w", err)
	}
	return nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Commit is a no-op for SQLiteStore — writes auto-commit via individual
// transactions. Exposed on the interface for backends that need explicit flush.
func (s *SQLiteStore) Commit() error { return nil }

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func (s *SQLiteStore) SetMetadata(key, value string) error {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)", key, value,
	)
	return err
}

func (s *SQLiteStore) GetMetadata(key string) (string, error) {
	var val string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key=?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// ---------------------------------------------------------------------------
// Code graph — write
// ---------------------------------------------------------------------------

func (s *SQLiteStore) UpsertNode(node NodeInfo, fileHash string) (int64, error) {
	now := float64(time.Now().UnixNano()) / 1e9
	qualified := makeQualified(node)
	extra, err := encodeExtra(node.Extra)
	if err != nil {
		return 0, err
	}

	isTest := 0
	if node.IsTest {
		isTest = 1
	}

	_, err = s.db.Exec(`
		INSERT INTO nodes
		  (kind, name, qualified_name, file_path, line_start, line_end,
		   language, parent_name, params, return_type, modifiers, is_test,
		   file_hash, extra, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(qualified_name) DO UPDATE SET
		  kind=excluded.kind, name=excluded.name,
		  file_path=excluded.file_path,
		  line_start=excluded.line_start, line_end=excluded.line_end,
		  language=excluded.language, parent_name=excluded.parent_name,
		  params=excluded.params, return_type=excluded.return_type,
		  modifiers=excluded.modifiers, is_test=excluded.is_test,
		  file_hash=excluded.file_hash, extra=excluded.extra,
		  updated_at=excluded.updated_at`,
		node.Kind, node.Name, qualified, node.FilePath,
		node.LineStart, node.LineEnd, node.Language,
		node.ParentName, node.Params, node.ReturnType, node.Modifiers,
		isTest, fileHash, extra, now,
	)
	if err != nil {
		return 0, fmt.Errorf("graphstore: upsert node %q: %w", qualified, err)
	}

	var id int64
	err = s.db.QueryRow("SELECT id FROM nodes WHERE qualified_name=?", qualified).Scan(&id)
	return id, err
}

func (s *SQLiteStore) UpsertEdge(edge EdgeInfo) (int64, error) {
	now := float64(time.Now().UnixNano()) / 1e9
	extra, err := encodeExtra(edge.Extra)
	if err != nil {
		return 0, err
	}

	var existingID int64
	err = s.db.QueryRow(
		`SELECT id FROM edges
		 WHERE kind=? AND source_qualified=? AND target_qualified=? AND file_path=?`,
		edge.Kind, edge.Source, edge.Target, edge.FilePath,
	).Scan(&existingID)

	if err == nil {
		// update existing
		_, err = s.db.Exec(
			"UPDATE edges SET line=?, extra=?, updated_at=? WHERE id=?",
			edge.Line, extra, now, existingID,
		)
		return existingID, err
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("graphstore: lookup edge: %w", err)
	}

	res, err := s.db.Exec(
		`INSERT INTO edges
		 (kind, source_qualified, target_qualified, file_path, line, extra, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		edge.Kind, edge.Source, edge.Target, edge.FilePath, edge.Line, extra, now,
	)
	if err != nil {
		return 0, fmt.Errorf("graphstore: insert edge: %w", err)
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) RemoveFileData(filePath string) error {
	if _, err := s.db.Exec("DELETE FROM nodes WHERE file_path=?", filePath); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM edges WHERE file_path=?", filePath)
	return err
}

// StoreFileNodesEdges atomically replaces all nodes and edges for a file.
func (s *SQLiteStore) StoreFileNodesEdges(filePath string, nodes []NodeInfo, edges []EdgeInfo, fileHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("graphstore: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec("DELETE FROM nodes WHERE file_path=?", filePath); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM edges WHERE file_path=?", filePath); err != nil {
		return err
	}

	now := float64(time.Now().UnixNano()) / 1e9

	for _, node := range nodes {
		qualified := makeQualified(node)
		extra, _ := encodeExtra(node.Extra)
		isTest := 0
		if node.IsTest {
			isTest = 1
		}
		_, err := tx.Exec(`
			INSERT INTO nodes
			  (kind, name, qualified_name, file_path, line_start, line_end,
			   language, parent_name, params, return_type, modifiers, is_test,
			   file_hash, extra, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(qualified_name) DO UPDATE SET
			  kind=excluded.kind, name=excluded.name,
			  file_path=excluded.file_path,
			  line_start=excluded.line_start, line_end=excluded.line_end,
			  language=excluded.language, parent_name=excluded.parent_name,
			  params=excluded.params, return_type=excluded.return_type,
			  modifiers=excluded.modifiers, is_test=excluded.is_test,
			  file_hash=excluded.file_hash, extra=excluded.extra,
			  updated_at=excluded.updated_at`,
			node.Kind, node.Name, qualified, node.FilePath,
			node.LineStart, node.LineEnd, node.Language,
			node.ParentName, node.Params, node.ReturnType, node.Modifiers,
			isTest, fileHash, extra, now,
		)
		if err != nil {
			return fmt.Errorf("graphstore: store node %q: %w", qualified, err)
		}
	}

	for _, edge := range edges {
		extra, _ := encodeExtra(edge.Extra)
		_, err := tx.Exec(`
			INSERT INTO edges
			  (kind, source_qualified, target_qualified, file_path, line, extra, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			edge.Kind, edge.Source, edge.Target, edge.FilePath, edge.Line, extra, now,
		)
		if err != nil {
			return fmt.Errorf("graphstore: store edge: %w", err)
		}
	}

	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Code graph — read
// ---------------------------------------------------------------------------

func (s *SQLiteStore) GetNode(qualifiedName string) (*GraphNode, error) {
	row := s.db.QueryRow("SELECT * FROM nodes WHERE qualified_name=?", qualifiedName)
	n, err := scanNode(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

func (s *SQLiteStore) GetNodesByFile(filePath string) ([]GraphNode, error) {
	rows, err := s.db.Query("SELECT * FROM nodes WHERE file_path=?", filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows)
}

func (s *SQLiteStore) GetEdgesBySource(qualifiedName string) ([]GraphEdge, error) {
	rows, err := s.db.Query("SELECT * FROM edges WHERE source_qualified=?", qualifiedName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectEdges(rows)
}

func (s *SQLiteStore) GetEdgesByTarget(qualifiedName string) ([]GraphEdge, error) {
	rows, err := s.db.Query("SELECT * FROM edges WHERE target_qualified=?", qualifiedName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectEdges(rows)
}

func (s *SQLiteStore) GetEdgesAmong(qualifiedNames []string) ([]GraphEdge, error) {
	if len(qualifiedNames) == 0 {
		return nil, nil
	}
	qnSet := make(map[string]bool, len(qualifiedNames))
	for _, q := range qualifiedNames {
		qnSet[q] = true
	}

	const batchSize = 450
	var result []GraphEdge

	for i := 0; i < len(qualifiedNames); i += batchSize {
		end := i + batchSize
		if end > len(qualifiedNames) {
			end = len(qualifiedNames)
		}
		batch := qualifiedNames[i:end]
		placeholders := strings.Repeat("?,", len(batch))
		placeholders = placeholders[:len(placeholders)-1]

		args := make([]any, len(batch))
		for j, q := range batch {
			args[j] = q
		}

		rows, err := s.db.Query(
			fmt.Sprintf("SELECT * FROM edges WHERE source_qualified IN (%s)", placeholders),
			args...,
		)
		if err != nil {
			return nil, err
		}
		edges, err := collectEdges(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if qnSet[e.TargetQualified] {
				result = append(result, e)
			}
		}
	}
	return result, nil
}

func (s *SQLiteStore) GetAllFiles() ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT file_path FROM nodes WHERE kind='File'")
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

func (s *SQLiteStore) SearchNodes(query string, limit int) ([]GraphNode, error) {
	pattern := "%" + query + "%"
	rows, err := s.db.Query(
		"SELECT * FROM nodes WHERE name LIKE ? OR qualified_name LIKE ? LIMIT ?",
		pattern, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows)
}

func (s *SQLiteStore) GetStats() (GraphStats, error) {
	var stats GraphStats

	if err := s.db.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&stats.TotalNodes); err != nil {
		return stats, err
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM edges").Scan(&stats.TotalEdges); err != nil {
		return stats, err
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE kind='File'").Scan(&stats.FilesCount); err != nil {
		return stats, err
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM kg_notes").Scan(&stats.NotesCount); err != nil {
		return stats, err
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM note_symbol_links").Scan(&stats.LinksCount); err != nil {
		return stats, err
	}

	stats.NodesByKind = map[string]int{}
	rows, err := s.db.Query("SELECT kind, COUNT(*) FROM nodes GROUP BY kind")
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

	stats.EdgesByKind = map[string]int{}
	rows, err = s.db.Query("SELECT kind, COUNT(*) FROM edges GROUP BY kind")
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

	rows, err = s.db.Query("SELECT DISTINCT language FROM nodes WHERE language IS NOT NULL AND language != ''")
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

	stats.LastUpdated, _ = s.GetMetadata("last_updated")
	return stats, nil
}

// GetImpactRadius performs a pure-Go BFS from the nodes in changedFiles,
// traversing both outbound and inbound edges up to maxDepth hops.
func (s *SQLiteStore) GetImpactRadius(changedFiles []string, maxDepth, maxNodes int) (ImpactResult, error) {
	// Build adjacency map from all edges for the relevant subgraph.
	// For large graphs this is an in-memory BFS — adequate for maxNodes=500.
	type adj struct {
		outbound []string // source → targets
		inbound  []string // target ← sources
	}

	// Seed: gather all qualified names in the changed files.
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

	// Build edge adjacency for BFS.
	// We load all edges and index them — efficient enough for typical repo sizes.
	rows, err := s.db.Query("SELECT source_qualified, target_qualified FROM edges")
	if err != nil {
		return ImpactResult{}, err
	}
	fwd := map[string][]string{} // source → targets
	rev := map[string][]string{} // target → sources
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

	// Resolve to full node records.
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

	// Collect edges among all relevant nodes.
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

func (s *SQLiteStore) UpsertKGNote(note KGNote) error {
	now := float64(time.Now().UnixNano()) / 1e9
	if note.IndexedAt == 0 {
		note.IndexedAt = now
	}
	_, err := s.db.Exec(`
		INSERT INTO kg_notes
		  (id, title, note_type, status, summary, file_path, version, archived_at, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  title=excluded.title, note_type=excluded.note_type,
		  status=excluded.status, summary=excluded.summary,
		  file_path=excluded.file_path, version=excluded.version,
		  archived_at=excluded.archived_at, indexed_at=excluded.indexed_at`,
		note.ID, note.Title, note.NoteType, note.Status, note.Summary,
		note.FilePath, note.Version, note.ArchivedAt, note.IndexedAt,
	)
	return err
}

func (s *SQLiteStore) GetKGNote(id string) (*KGNote, error) {
	row := s.db.QueryRow(
		"SELECT id, title, note_type, status, summary, file_path, version, archived_at, indexed_at FROM kg_notes WHERE id=?",
		id,
	)
	note := &KGNote{}
	err := row.Scan(&note.ID, &note.Title, &note.NoteType, &note.Status,
		&note.Summary, &note.FilePath, &note.Version, &note.ArchivedAt, &note.IndexedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return note, err
}

func (s *SQLiteStore) SearchKGNotes(query string, limit int) ([]KGNote, error) {
	pattern := "%" + query + "%"
	rows, err := s.db.Query(
		`SELECT id, title, note_type, status, summary, file_path, version, archived_at, indexed_at
		 FROM kg_notes
		 WHERE (title LIKE ? OR summary LIKE ?) AND archived_at=''
		 LIMIT ?`,
		pattern, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNotes(rows)
}

func (s *SQLiteStore) ListArchivedKGNotes() ([]KGNote, error) {
	rows, err := s.db.Query(
		`SELECT id, title, note_type, status, summary, file_path, version, archived_at, indexed_at
		 FROM kg_notes WHERE archived_at != '' ORDER BY archived_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNotes(rows)
}

// ---------------------------------------------------------------------------
// Note→symbol links
// ---------------------------------------------------------------------------

func (s *SQLiteStore) UpsertNoteSymbolLink(link NoteSymbolLink) (int64, error) {
	now := float64(time.Now().UnixNano()) / 1e9
	if link.CreatedAt == 0 {
		link.CreatedAt = now
	}
	res, err := s.db.Exec(`
		INSERT INTO note_symbol_links (note_id, qualified_name, link_kind, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(note_id, qualified_name, link_kind) DO NOTHING`,
		link.NoteID, link.QualifiedName, link.LinkKind, link.CreatedAt,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		// Already exists — return existing id
		err = s.db.QueryRow(
			"SELECT id FROM note_symbol_links WHERE note_id=? AND qualified_name=? AND link_kind=?",
			link.NoteID, link.QualifiedName, link.LinkKind,
		).Scan(&id)
	}
	return id, err
}

func (s *SQLiteStore) GetLinksForNote(noteID string) ([]NoteSymbolLink, error) {
	rows, err := s.db.Query(
		"SELECT id, note_id, qualified_name, link_kind, created_at FROM note_symbol_links WHERE note_id=?",
		noteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectLinks(rows)
}

func (s *SQLiteStore) GetLinksForSymbol(qualifiedName string) ([]NoteSymbolLink, error) {
	rows, err := s.db.Query(
		"SELECT id, note_id, qualified_name, link_kind, created_at FROM note_symbol_links WHERE qualified_name=?",
		qualifiedName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectLinks(rows)
}

func (s *SQLiteStore) DeleteNoteSymbolLink(id int64) error {
	_, err := s.db.Exec("DELETE FROM note_symbol_links WHERE id=?", id)
	return err
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func makeQualified(node NodeInfo) string {
	if node.ParentName != "" {
		return node.ParentName + "." + node.Name
	}
	return node.FilePath + "::" + node.Name
}

func encodeExtra(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

func decodeExtra(s string) map[string]any {
	if s == "" || s == "{}" {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

type nodeScanner interface {
	Scan(dest ...any) error
}

func scanNode(row nodeScanner) (*GraphNode, error) {
	var n GraphNode
	var isTest int
	var extraStr, modifiers string
	err := row.Scan(
		&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath,
		&n.LineStart, &n.LineEnd, &n.Language, &n.ParentName,
		&n.Params, &n.ReturnType, &modifiers, &isTest,
		&n.FileHash, &extraStr, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	n.IsTest = isTest != 0
	n.Extra = decodeExtra(extraStr)
	return &n, nil
}

func collectNodes(rows *sql.Rows) ([]GraphNode, error) {
	var result []GraphNode
	for rows.Next() {
		var n GraphNode
		var isTest int
		var extraStr, modifiers string
		err := rows.Scan(
			&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.LineStart, &n.LineEnd, &n.Language, &n.ParentName,
			&n.Params, &n.ReturnType, &modifiers, &isTest,
			&n.FileHash, &extraStr, &n.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		n.IsTest = isTest != 0
		n.Extra = decodeExtra(extraStr)
		result = append(result, n)
	}
	return result, rows.Err()
}

func collectEdges(rows *sql.Rows) ([]GraphEdge, error) {
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

func collectNotes(rows *sql.Rows) ([]KGNote, error) {
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

func collectLinks(rows *sql.Rows) ([]NoteSymbolLink, error) {
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

// CountNodes returns the number of nodes in the code graph.
func (s *SQLiteStore) CountNodes() int {
	var n int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&n)
	return n
}

// CountKGNotes returns the number of KG notes in the warm store.
func (s *SQLiteStore) CountKGNotes() int {
	var n int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM kg_notes").Scan(&n)
	return n
}
