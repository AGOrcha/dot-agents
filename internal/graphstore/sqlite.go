package graphstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// SQLiteStore is the SQLite-backed implementation of Store.
type SQLiteStore struct {
	db *sql.DB

	// Shutdown lifecycle. reapers tracks the abandon-and-fail conn-drain
	// goroutines (see queryContextGuarded) so Close blocks until every
	// orphaned connection has actually been returned to the pool rather
	// than racing db.Close() against an in-flight reaper.
	//
	// mu serialises reaper registration against Close: a reaper is
	// spawned lazily (only when a request times out), so reapers.Add
	// must be ordered-before reapers.Wait via mu or the race detector
	// (correctly) flags Add-not-happens-before-Wait. Once closed is set
	// no new tracked reaper is registered — a timeout racing shutdown
	// drains its conn untracked (best effort; Close already committed to
	// Wait) so nothing is stranded.
	mu      sync.Mutex
	closed  bool
	reapers sync.WaitGroup
}

// OpenSQLite opens (or creates) the SQLite database at dbPath and initialises
// the schema. The parent directory is created if it does not exist.
func OpenSQLite(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("graphstore: create db dir: %w", err)
	}

	db, err := sqlOpen("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("graphstore: open db: %w", err)
	}

	// Connection-pool sizing is provider-internal (CONTRACT.md L104-108
	// assigns pool / write-serialization mechanism to the provider's
	// discretion; it is NOT a contract clause). The earlier
	// SetMaxOpenConns(1) made the whole store a single-conn chokepoint:
	// modernc.org/sqlite's _sqlite3Step is a non-preemptible translated-C
	// VM loop, so on Windows a ctx deadline (or an out-of-band Close from
	// another goroutine) CANNOT interrupt an in-progress step. With the
	// cap at 1, any one wedged/slow step held the only connection and the
	// next operation could never acquire it -> whole-store deadlock and
	// the windows-latest "test timed out after 5m" panic.
	//
	// A small bounded pool of cheap, short-lived connections removes the
	// chokepoint: a wedged step can be abandoned (see queryContextGuarded)
	// and the next op acquires a different conn instead of blocking on the
	// one stuck step. This does NOT change the documented cross-process
	// write-serialization story — that is delivered by SQLite WAL +
	// busy_timeout=5000 at the file/OS level (set below), which modernc
	// honors with multiple independent connections (file locking + WAL).
	// Empirically verified: two independent pools writing the same DB
	// concurrently still serialize via busy_timeout with zero corruption.
	// Path A is ephemeral + cheap, so conns are kept short-lived and the
	// idle set small rather than long-pooled.
	//
	// Pool size is a pure throughput knob, not a correctness one: WAL +
	// busy_timeout (set below) own write-serialization at the file/OS
	// level regardless of how many conns the pool hands out, so raising
	// the cap only buys more intra-process read concurrency.
	//
	// Sizing target: agent fleets. A single review/planning stage fans
	// out ~3 subagents that each hit `da kg` (and other `da` commands,
	// sometimes scripted in batches) to gather lens/analysis context; an
	// orchestrator multiplies that across plans and tasks. Rough demand
	// is (n_tasks * r_agents * x_calls) concurrent short reads against
	// the same store. 512 is an initial ceiling meant to absorb a basic
	// squadron/fleet without the pool itself becoming the chokepoint;
	// node fd/memory limits are the real cap and will surface first.
	// This is an untested heuristic from session anecdote + forum
	// discussion, NOT a tuned figure — expect to revise it with real
	// fleet telemetry. Idle is kept at 64 (not 4) so steady-state fleet
	// traffic reuses warm conns instead of paying modernc's per-conn
	// open + WAL/PRAGMA cost on every burst; ConnMaxIdleTime still reaps
	// the long tail so an idle store does not pin 64 fds forever.
	db.SetMaxOpenConns(512)
	db.SetMaxIdleConns(64)
	db.SetConnMaxIdleTime(30 * time.Second)
	db.SetConnMaxLifetime(5 * time.Minute)

	if _, err := dbExec(db, "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("graphstore: set WAL mode: %w", err)
	}
	// synchronous=NORMAL is the SQLite-recommended pairing with WAL. In WAL
	// mode NORMAL is crash-safe across application crashes (a transaction
	// can only be lost on OS crash / power loss, and then only the last
	// one) — appropriate for this rebuildable derived graph cache. It drops
	// the per-auto-commit fsync that modernc.org/sqlite's pure-Go VM pays
	// on every statement. On Windows that fsync is pathologically slow:
	// without this, an un-batched bulk write loop (e.g. the bounds
	// enforcement test's 5k+ UpsertNode/UpsertEdge auto-commit statements)
	// exceeds the 5-minute test budget and the windows-latest job panics
	// "test timed out after 5m" (ubuntu/macos finish in seconds). This
	// changes durability tuning only — it does not weaken the Path-A
	// bounds/timeout contract or any read semantics.
	if _, err := dbExec(db, "PRAGMA synchronous=NORMAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("graphstore: set synchronous mode: %w", err)
	}
	// WAL + busy_timeout are the cross-process write-serialization
	// mechanism (CONTRACT.md guidance): a concurrent `da workflow` and
	// MCP-server both writing this DB serialize at the SQLite file/OS
	// level and, only after 5s of sustained contention, surface a hard
	// SQLITE_BUSY rather than queue. That is a user-visible flake, not
	// data loss; acceptable for the current single-orchestrator usage.
	// This serialization is independent of the Go connection-pool size
	// (it is enforced by SQLite's file lock + WAL, not *sql.DB), which is
	// precisely why the pool cap above could be relaxed without changing
	// the documented concurrency behavior. Revisit (longer timeout or an
	// app-level write lock) if concurrent-writer workflows become common.
	if _, err := dbExec(db, "PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("graphstore: set busy_timeout: %w", err)
	}
	if _, err := dbExec(db, "PRAGMA foreign_keys=ON"); err != nil {
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

// initSchema creates the idempotent base schema, then brings the code-graph
// layer forward to SchemaVersion and records that version. Running it on an
// already-current database executes no DDL, so it is safe on every open.
func (s *SQLiteStore) initSchema() error {
	if _, err := dbExec(s.db, schemaSQL); err != nil {
		return fmt.Errorf("graphstore: init schema: %w", err)
	}
	if err := s.migrateCodeGraphSchema(); err != nil {
		return err
	}
	if err := s.SetMetadata(metadataSchemaVersion, strconv.Itoa(SchemaVersion)); err != nil {
		return fmt.Errorf("graphstore: record schema version: %w", err)
	}
	return nil
}

// OpenSQLiteReadOnly opens an EXISTING SQLite store at dbPath read-only, for
// read paths (e.g. the eval generator) that must never bring a store into
// being as a side effect. Unlike OpenSQLite — which is open-or-CREATE: it
// MkdirAlls the parent and initialises an empty schema — this opens through a
// `mode=ro` file URI, so the open itself is creation-safe BY CONSTRUCTION: a
// missing store fails to open rather than being silently created, no matter the
// caller's timing (there is no create-capable open to race). It also skips the
// schema init and the WAL/journal PRAGMAs, which are writes a read-only handle
// must not perform.
//
// A missing store is reported as a wrapped os.ErrNotExist so callers can
// errors.Is(err, os.ErrNotExist) and render an actionable "not built" message.
func OpenSQLiteReadOnly(dbPath string) (*SQLiteStore, error) {
	db, err := sqlOpen("sqlite", readOnlyDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("graphstore: open db: %w", err)
	}
	// sql.Open is lazy; Ping forces the actual (no-create) open so an absent or
	// unreadable store fails here rather than at the first query.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, classifyReadOnlyOpenErr(dbPath, err)
	}
	return &SQLiteStore{db: db}, nil
}

// classifyReadOnlyOpenErr turns a failed read-only open into a caller-friendly
// error: a wrapped os.ErrNotExist when the store file is genuinely absent,
// otherwise the underlying open error. The stat runs only AFTER the open has
// already failed, so it is purely for classifying the message — it is never
// load-bearing for the no-create guarantee (mode=ro already owns that).
func classifyReadOnlyOpenErr(dbPath string, openErr error) error {
	if _, statErr := os.Stat(dbPath); errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("graphstore: db %s does not exist: %w", dbPath, os.ErrNotExist)
	}
	return fmt.Errorf("graphstore: open db %s: %w", dbPath, openErr)
}

// readOnlyDSN builds a `mode=ro` SQLite file URI for dbPath. The `file:` prefix
// makes modernc treat it as a URI (so mode=ro reaches sqlite3_open_v2 and
// suppresses SQLITE_OPEN_CREATE); url.URL handles percent-encoding of spaces
// and other special characters in the path, and the leading-slash normalisation
// keeps Windows drive paths (C:/… → /C:/…) valid file URIs.
func readOnlyDSN(dbPath string) string {
	p := filepath.ToSlash(dbPath)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}
	return u.String()
}

// Close shuts the store down deterministically: it marks the store
// closed (so no new tracked reaper can register), waits for every
// in-flight abandon-and-fail reaper to finish draining its orphaned
// connection, then closes the pool. Waiting on reapers before
// db.Close() is the correctness point — a timed-out request abandons
// its connection to a background reaper (see queryContextGuarded);
// closing the pool while that reaper still holds the conn would race
// db.Close() against an in-flight step and could leak the goroutine +
// connection past the store's lifetime.
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.reapers.Wait()
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

// RemoveFileData drops one file's nodes and edges.
//
// It runs through derivedTx because deleting a node row is exactly the write
// upstream's inert foreign keys permit and an enforcing one does not:
// risk_index.node_id references nodes(id), so a rebuild over a graph that
// still carries summary rows would fail the constraint instead of replacing
// the file. The transaction also makes the node and edge deletes atomic.
func (s *SQLiteStore) RemoveFileData(filePath string) error {
	return s.derivedTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM nodes WHERE file_path=?", filePath); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM edges WHERE file_path=?", filePath)
		return err
	})
}

// StoreFileNodesEdges atomically replaces all nodes and edges for a file.
//
// Like RemoveFileData it runs through derivedTx: replacing a file deletes its
// node rows, which risk_index references, and upstream's foreign keys are
// inert for exactly that reason.
func (s *SQLiteStore) StoreFileNodesEdges(filePath string, nodes []NodeInfo, edges []EdgeInfo, fileHash string) error {
	return s.derivedTx(func(tx *sql.Tx) error {
		return storeFileNodesEdgesTx(tx, filePath, nodes, edges, fileHash)
	})
}

// storeFileNodesEdgesTx is the replace itself, inside a caller's transaction.
func storeFileNodesEdgesTx(tx *sql.Tx, filePath string, nodes []NodeInfo, edges []EdgeInfo, fileHash string) error {
	if _, err := tx.Exec("DELETE FROM nodes WHERE file_path=?", filePath); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM edges WHERE file_path=?", filePath); err != nil {
		return err
	}

	now := float64(time.Now().UnixNano()) / 1e9

	for _, node := range nodes {
		qualified := makeQualified(node)
		extra, err := encodeExtra(node.Extra)
		if err != nil {
			return fmt.Errorf("graphstore: encode extra for node %q: %w", qualified, err)
		}
		isTest := 0
		if node.IsTest {
			isTest = 1
		}
		_, err = tx.Exec(`
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
		extra, err := encodeExtra(edge.Extra)
		if err != nil {
			return fmt.Errorf("graphstore: encode extra for edge %s->%s: %w", edge.Source, edge.Target, err)
		}
		_, err = tx.Exec(`
			INSERT INTO edges
			  (kind, source_qualified, target_qualified, file_path, line, extra, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			edge.Kind, edge.Source, edge.Target, edge.FilePath, edge.Line, extra, now,
		)
		if err != nil {
			return fmt.Errorf("graphstore: store edge: %w", err)
		}
	}

	return nil
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
		end := min(i+batchSize, len(qualifiedNames))
		edges, err := s.queryEdgesBatch(qualifiedNames[i:end])
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

func (s *SQLiteStore) queryEdgesBatch(batch []string) ([]GraphEdge, error) {
	placeholders := strings.Repeat("?,", len(batch))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(batch))
	for j, q := range batch {
		args[j] = q
	}
	query := "SELECT * FROM edges WHERE source_qualified IN (" + placeholders + ")"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectEdges(rows)
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
	limit = normalizeSearchLimit(limit)
	ctx, cancel := requestContext(nil)
	defer cancel()
	pattern := "%" + query + "%"
	rows, err := s.queryContextGuarded(
		ctx,
		"SELECT * FROM nodes WHERE name LIKE ? OR qualified_name LIKE ? LIMIT ?",
		pattern, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows.Rows)
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

	var err error
	stats.NodesByKind, err = s.queryKindCounts("SELECT kind, COUNT(*) FROM nodes GROUP BY kind")
	if err != nil {
		return stats, err
	}
	stats.EdgesByKind, err = s.queryKindCounts("SELECT kind, COUNT(*) FROM edges GROUP BY kind")
	if err != nil {
		return stats, err
	}
	stats.Languages, err = s.queryDistinctStrings("SELECT DISTINCT language FROM nodes WHERE language IS NOT NULL AND language != ''")
	if err != nil {
		return stats, err
	}

	stats.LastUpdated, _ = s.GetMetadata("last_updated")
	return stats, nil
}

func (s *SQLiteStore) queryKindCounts(query string) (map[string]int, error) {
	m := map[string]int{}
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var c int
		if err := rows.Scan(&k, &c); err != nil {
			return nil, err
		}
		m[k] = c
	}
	return m, nil
}

func (s *SQLiteStore) queryDistinctStrings(query string) ([]string, error) {
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, nil
}

// GetImpactRadius performs a pure-Go BFS from the nodes in changedFiles,
// traversing both outbound and inbound edges up to maxDepth hops. The
// BFS + node-resolution + edge-aggregation body lives in
// computeImpactRadius (impact.go); this method only handles the
// SQLite-specific seed gathering and edge adjacency loading.
func (s *SQLiteStore) GetImpactRadius(changedFiles []string, maxDepth, maxNodes int) (ImpactResult, error) {
	// Provider-owned request timeout (CONTRACT.md guarantee #2): the
	// full-table edge scan + BFS is the long traversal; callers do not
	// wrap their own deadline. Bounds are clamped in computeImpactRadius.
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

	fwd, rev, err := s.loadEdgeAdjacency(ctx)
	if err != nil {
		return ImpactResult{}, err
	}

	// computeImpactRadius re-enters the store (GetEdgesAmong /
	// resolveImpactNodes). loadEdgeAdjacency releases its edge result set
	// via a deferred Close before this call returns, so re-entrant reads
	// reuse a freed conn promptly. (Under the now-relaxed pool a stranded
	// result set no longer deadlocks the whole store, but deterministic
	// release keeps the pool small and conns short-lived per Path A.)
	return computeImpactRadius(seeds, fwd, rev, maxDepth, maxNodes, s)
}

// loadEdgeAdjacency runs the full-table edge scan under the request-timeout
// context and builds the forward/reverse adjacency maps. The result set is
// closed via defer before this function returns, so its connection is
// deterministically released — an early return, scan error, or panic cannot
// strand it. If the request timeout fires mid-scan, queryContextGuarded
// returns a timeout error here (abandoning the wedged modernc conn) rather
// than blocking; see its doc comment for why that is the only correct
// mechanism on the non-preemptible modernc/Windows path.
func (s *SQLiteStore) loadEdgeAdjacency(ctx context.Context) (fwd, rev map[string][]string, err error) {
	rows, err := s.queryContextGuarded(ctx, "SELECT source_qualified, target_qualified FROM edges")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	return buildEdgeAdjacency(rows.Rows)
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
	limit = normalizeSearchLimit(limit)
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

// errRequestTimeout is returned by queryContextGuarded when the
// provider-owned request deadline fires before the SQLite query produced a
// result set. It preserves the CONTRACT.md guarantee #2 (caller sees a
// deadline-bounded error); only the SQLite mechanism for delivering it
// changed (see queryContextGuarded).
var errRequestTimeout = fmt.Errorf("graphstore: sqlite request exceeded provider timeout")

// queryContextGuarded runs a request-timeout-bounded read and, on timeout,
// ABANDONS the wedged connection and fails rather than trying to interrupt
// the in-progress step.
//
// Why "abandon-and-fail", not "cancel the step": gcc2 routes SQLite reads
// through QueryContext with the Path-A request-timeout context. modernc's
// _sqlite3Step is a non-preemptible translated-C VM loop — on Windows
// neither a ctx deadline NOR an out-of-band sql.Rows.Close() from another
// goroutine can interrupt an in-progress step (the prior watchdog fix
// assumed Close could; it cannot, which is why two Windows passes failed).
// QueryContext itself does not return until the wedged step completes, so a
// watchdog that waits for *sql.Rows has nothing to close yet.
//
// Correct mechanism: run QueryContext on its own goroutine. If the
// request-timeout ctx fires first, return errRequestTimeout immediately and
// leave the goroutine (and its conn) to finish out-of-band. The orphaned
// modernc step runs to completion on its now-abandoned conn and is reaped by
// the closeAbandoned helper; it does NOT block the next op because the
// connection pool is no longer capped at 1 (OpenSQLite) — the next
// acquisition simply uses a different conn. The timeout *guarantee* (caller
// sees a deadline-bounded error) is preserved; only its SQLite mechanism
// changed from "cancel the step" (impossible on modernc/Windows) to
// "abandon the conn + fail". The bounded-result enforcement (hard
// node/depth cap) is unaffected and still applied by computeImpactRadius.
func (s *SQLiteStore) queryContextGuarded(ctx context.Context, query string, args ...any) (*guardedRows, error) {
	type queryResult struct {
		rows *sql.Rows
		err  error
	}
	resCh := make(chan queryResult, 1)
	go func() {
		rows, err := s.db.QueryContext(ctx, query, args...)
		resCh <- queryResult{rows: rows, err: err}
	}()

	select {
	case res := <-resCh:
		if res.err != nil {
			return nil, res.err
		}
		return &guardedRows{Rows: res.rows}, nil
	case <-ctx.Done():
		// modernc/Windows cannot interrupt the in-flight step; abandon the
		// goroutine + its conn (safe only because the pool is not capped at
		// 1) and fail with a deadline-bounded error. The reaper drains +
		// closes the orphaned result set when the step eventually finishes
		// so the abandoned conn is returned to the pool rather than leaked.
		// It is registered on s.reapers (under s.mu so the Add is
		// ordered-before Close's Wait) so Close blocks until every such
		// drain has completed (no goroutine/conn outlives the store). If
		// the store is already closing, drain untracked — Close has
		// committed to Wait and must not observe a late Add.
		drain := func() {
			res := <-resCh
			if res.rows != nil {
				_ = res.rows.Close()
			}
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			go drain()
			return nil, errRequestTimeout
		}
		s.reapers.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.reapers.Done()
			drain()
		}()
		return nil, errRequestTimeout
	}
}

// guardedRows wraps *sql.Rows. The deadline mechanism now lives in
// queryContextGuarded (abandon-and-fail before returning rows), so Close is
// just an idempotent passthrough; callers still defer it on the
// normal-completion path to release the conn deterministically.
type guardedRows struct {
	*sql.Rows
	closeOnce sync.Once
}

func (g *guardedRows) Close() error {
	g.closeOnce.Do(func() { _ = g.Rows.Close() })
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// makeQualified builds a node's graph identity, matching upstream
// GraphStore._make_qualified (graph.py) exactly:
//
//	File node        -> "<file_path>"
//	nested in parent -> "<file_path>::<parent_name>.<name>"
//	otherwise        -> "<file_path>::<name>"
//
// The file path is part of every identity, including the nested case: two
// receivers named Session in different files are different types, and a
// bare "Session.Expired" would collide them and silently merge their
// edges, community membership and impact radius. A File node's identity is
// the bare path because that is what every IMPORTS_FROM / CONTAINS edge
// sourced at a file refers to.
func makeQualified(node NodeInfo) string {
	if node.Kind == NodeKindFile {
		return node.FilePath
	}
	if node.ParentName != "" {
		return node.FilePath + "::" + node.ParentName + "." + node.Name
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

// extraDecodeErrorKey tags the map returned by decodeExtra when the stored
// extra JSON failed to parse, so a corrupt row is distinguishable from a
// legitimately empty one without changing decodeExtra's signature (a full
// error-propagating signature is tracked as a follow-up).
const extraDecodeErrorKey = "_graphstore_extra_decode_error"

func decodeExtra(s string) map[string]any {
	if s == "" || s == "{}" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		slog.Warn("graphstore: corrupt extra JSON on read, dropping metadata", "error", err)
		return map[string]any{extraDecodeErrorKey: err.Error()}
	}
	return m
}

type nodeScanner interface {
	Scan(dest ...any) error
}

// scanNode scans a `SELECT *` nodes row positionally. The column order is the
// schema's declaration order, which is why migrations.go appends
// signature/community_id via ALTER TABLE (upstream's own column order) instead
// of inserting them mid-table: those two trailing destinations depend on it.
//
// Every column upstream leaves nullable is scanned through a sql.Null* and
// flattened to the Go zero value. That is not defensive padding — it is what
// lets the native store READ AN UPSTREAM-WRITTEN DATABASE, which the
// schema-parity contract and the Python-bridge rollback path both require.
// Upstream really does store NULL there (see the release fixture's nodes:
// parent_name, params, return_type and modifiers are null for most rows),
// whereas the native writers always pass "" — so scanning straight into a
// string works on our own rows and fails on theirs.
func scanNode(row nodeScanner) (*GraphNode, error) {
	var n GraphNode
	var isTest sql.NullInt64
	var lineStart, lineEnd, communityID sql.NullInt64
	var language, parentName, params, returnType sql.NullString
	var modifiers, fileHash, extraStr, signature sql.NullString
	err := row.Scan(
		&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath,
		&lineStart, &lineEnd, &language, &parentName,
		&params, &returnType, &modifiers, &isTest,
		&fileHash, &extraStr, &n.UpdatedAt,
		&signature, &communityID,
	)
	if err != nil {
		return nil, err
	}
	n.LineStart = int(lineStart.Int64)
	n.LineEnd = int(lineEnd.Int64)
	n.Language = language.String
	n.ParentName = parentName.String
	n.Params = params.String
	n.ReturnType = returnType.String
	n.IsTest = isTest.Int64 != 0
	n.FileHash = fileHash.String
	n.Extra = decodeExtra(extraStr.String)
	n.Signature = signature.String
	n.CommunityID = communityID.Int64
	// modifiers is read and discarded: the column exists for upstream schema
	// parity but GraphNode exposes no field for it, and `SELECT *` still has
	// to supply a destination for every column.
	return &n, nil
}

func collectNodes(rows *sql.Rows) ([]GraphNode, error) {
	var result []GraphNode
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *n)
	}
	return result, rows.Err()
}

// collectEdges scans `SELECT *` edges rows positionally. confidence and
// confidence_tier are the trailing columns migrations.go appends (upstream
// schema v9); like the nullable columns before them they are scanned through
// sql.Null* so a row written before the migration — or by upstream, which
// leaves line/extra nullable — reads back as the Go zero value instead of
// failing the scan. See scanNode for why that matters.
func collectEdges(rows *sql.Rows) ([]GraphEdge, error) {
	var result []GraphEdge
	for rows.Next() {
		var e GraphEdge
		var line sql.NullInt64
		var extraStr, tier sql.NullString
		var confidence sql.NullFloat64
		err := rows.Scan(
			&e.ID, &e.Kind, &e.SourceQualified, &e.TargetQualified,
			&e.FilePath, &line, &extraStr, &e.UpdatedAt,
			&confidence, &tier,
		)
		if err != nil {
			return nil, err
		}
		e.Line = int(line.Int64)
		e.Extra = decodeExtra(extraStr.String)
		e.Confidence = confidence.Float64
		e.ConfidenceTier = tier.String
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

// ---------------------------------------------------------------------------
// Code graph — derived views (CodeGraphDerived)
// ---------------------------------------------------------------------------

// derivedTx runs fn inside one immediate transaction with SQLite's foreign-key
// enforcement disabled for the duration.
//
// Both halves matter, and both callers matter.
//
// The transaction is what makes a Replace* atomic: DELETE + the INSERT loop
// either all land or none do, so an interrupted recompute leaves the previous
// generation of the table intact instead of an empty or half-filled one. This
// mirrors the explicit `BEGIN IMMEDIATE` upstream wraps store_flows,
// store_communities and each _compute_summaries block in.
//
// The foreign-key suspension is upstream behaviour made explicit. Upstream's
// code-graph tables DECLARE foreign keys (community_summaries -> communities,
// flow_snapshots -> flows, risk_index -> nodes) but Python's sqlite3 leaves
// `PRAGMA foreign_keys` OFF, so those declarations never constrain anything —
// and upstream DEPENDS on that. A standalone postprocess replaces
// `communities` while `community_summaries` still references the old ids, and
// upstream carries the stale summary rows forward; that tolerated staleness IS
// the documented lifecycle split (see CodeGraphDerived). OpenSQLite turns
// foreign keys ON for the KG layer, which does want referential integrity, so
// scoping the pragma to one dedicated connection for the duration of the write
// keeps the KG layer intact while reproducing upstream exactly. The pragma is
// a no-op inside a transaction, hence it is set on the conn BEFORE Begin and
// restored after Commit.
//
// The INGESTION path runs through here too, not just the derived writers:
// StoreFileNodesEdges and RemoveFileData both begin with
// `DELETE FROM nodes WHERE file_path=?`, and risk_index references nodes(id),
// so a second build over a graph that already carries summary rows would be
// refused outright. CONTRACT.md states the rule for anyone adding another
// node-referencing table.
func (s *SQLiteStore) derivedTx(fn func(tx *sql.Tx) error) error {
	ctx, cancel := requestContext(nil)
	defer cancel()

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	// Restore on the way out so the conn cannot return to the pool with FK
	// enforcement silently disabled for an unrelated later caller.
	defer func() {
		if _, rerr := conn.ExecContext(ctx, "PRAGMA foreign_keys=ON"); rerr != nil {
			slog.Warn("graphstore: restoring foreign_keys on derived conn failed", "error", rerr)
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ReadFlows() ([]FlowRow, error) {
	rows, err := s.db.Query(
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

func (s *SQLiteStore) ReadFlowMemberships() ([]FlowMembershipRow, error) {
	rows, err := s.db.Query(
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

func (s *SQLiteStore) ReadCommunities() ([]CommunityRow, error) {
	rows, err := s.db.Query(
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
		var parent sql.NullInt64
		var lang, desc sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &c.Level, &parent, &c.Cohesion,
			&c.Size, &lang, &desc); err != nil {
			return nil, err
		}
		if parent.Valid {
			id := parent.Int64
			c.ParentID = &id
		}
		c.DominantLanguage = lang.String
		c.Description = desc.String
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ReadCommunitySummaries() ([]CommunitySummaryRow, error) {
	rows, err := s.db.Query(
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

func (s *SQLiteStore) ReadFlowSnapshots() ([]FlowSnapshotRow, error) {
	rows, err := s.db.Query(
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

func (s *SQLiteStore) ReadRiskIndex() ([]RiskIndexRow, error) {
	rows, err := s.db.Query(
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

// SearchNodesFTS runs upstream's _fts_search query (search.py): the term is
// wrapped in double quotes (with embedded quotes doubled) so an FTS5 operator
// in a user query is matched as a literal instead of changing the query's
// meaning, and results come back ordered by FTS5 `rank`, which is negative
// BM25 — ascending rank is best-first.
func (s *SQLiteStore) SearchNodesFTS(query string, limit int) ([]int64, error) {
	limit = normalizeSearchLimit(limit)
	ctx, cancel := requestContext(nil)
	defer cancel()
	safe := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	rows, err := s.queryContextGuarded(ctx,
		"SELECT rowid FROM nodes_fts WHERE nodes_fts MATCH ? ORDER BY rank LIMIT ?",
		safe, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Rows.Next() {
		var id int64
		if err := rows.Rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Rows.Err()
}

// SearchNodesFTSWords and CountNodesFTSWords share ftsWordsMatch and both
// JOIN nodes: an external-content FTS5 table can still hold rowids for rows
// deleted since the last rebuild, and upstream's JOIN silently drops them
// rather than reporting ids that no longer resolve to a node.
func (s *SQLiteStore) SearchNodesFTSWords(words []string, limit int) ([]int64, error) {
	if len(words) == 0 {
		return nil, nil
	}
	limit = normalizeSearchLimit(limit)
	ctx, cancel := requestContext(nil)
	defer cancel()
	rows, err := s.queryContextGuarded(ctx,
		"SELECT n.id FROM nodes_fts f JOIN nodes n ON f.rowid = n.id "+
			"WHERE nodes_fts MATCH ? LIMIT ?",
		ftsWordsMatch(words), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Rows.Next() {
		var id int64
		if err := rows.Rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Rows.Err()
}

func (s *SQLiteStore) CountNodesFTSWords(words []string) (int, error) {
	if len(words) == 0 {
		return 0, nil
	}
	ctx, cancel := requestContext(nil)
	defer cancel()
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM nodes_fts f JOIN nodes n ON f.rowid = n.id "+
			"WHERE nodes_fts MATCH ?",
		ftsWordsMatch(words)).Scan(&n)
	return n, err
}

// ftsWordsMatch builds upstream's multi-word MATCH expression: each word
// becomes its own double-quoted phrase (embedded quotes doubled, so an FTS5
// operator in user input is matched literally) and the phrases are ANDed.
func ftsWordsMatch(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " AND ")
}

func (s *SQLiteStore) ReplaceFlows(flows []FlowRow, paths [][]int64) (int, error) {
	if len(paths) != len(flows) {
		return 0, fmt.Errorf("graphstore: ReplaceFlows got %d flows but %d paths", len(flows), len(paths))
	}
	count := 0
	err := s.derivedTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM flow_memberships"); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM flows"); err != nil {
			return err
		}
		for i, f := range flows {
			res, err := tx.Exec(
				`INSERT INTO flows
				   (name, entry_point_id, depth, node_count, file_count,
				    criticality, path_json)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`,
				f.Name, f.EntryPointID, f.Depth, f.NodeCount, f.FileCount,
				f.Criticality, encodeIDPath(paths[i]))
			if err != nil {
				return err
			}
			flowID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			for position, nodeID := range paths[i] {
				if _, err := tx.Exec(
					"INSERT OR IGNORE INTO flow_memberships (flow_id, node_id, position) VALUES (?, ?, ?)",
					flowID, nodeID, position); err != nil {
					return err
				}
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

func (s *SQLiteStore) ReplaceCommunities(communities []CommunityRow, members [][]string) (int, error) {
	if len(members) != len(communities) {
		return 0, fmt.Errorf("graphstore: ReplaceCommunities got %d communities but %d member sets",
			len(communities), len(members))
	}
	count := 0
	err := s.derivedTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM communities"); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE nodes SET community_id = NULL"); err != nil {
			return err
		}
		for i, c := range communities {
			res, err := tx.Exec(
				`INSERT INTO communities
				   (name, level, cohesion, size, dominant_language, description)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				c.Name, c.Level, c.Cohesion, c.Size, c.DominantLanguage, c.Description)
			if err != nil {
				return err
			}
			communityID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			if err := assignCommunity(tx, communityID, members[i]); err != nil {
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

// assignCommunity points every node whose qualified name is in memberQNs at
// communityID, batching the IN clause to stay under SQLite's default
// 999-variable statement limit.
func assignCommunity(tx *sql.Tx, communityID int64, memberQNs []string) error {
	const batchSize = 450
	for start := 0; start < len(memberQNs); start += batchSize {
		batch := memberQNs[start:min(start+batchSize, len(memberQNs))]
		args := make([]any, 0, len(batch)+1)
		args = append(args, communityID)
		for _, qn := range batch {
			args = append(args, qn)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		if _, err := tx.Exec(
			"UPDATE nodes SET community_id = ? WHERE qualified_name IN ("+placeholders+")",
			args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) ReplaceCommunitySummaries(rows []CommunitySummaryRow) (int, error) {
	return s.replaceRows("community_summaries", len(rows), func(tx *sql.Tx) error {
		for _, r := range rows {
			if _, err := tx.Exec(
				`INSERT OR REPLACE INTO community_summaries
				   (community_id, name, purpose, key_symbols, size, dominant_language)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				r.CommunityID, r.Name, r.Purpose, r.KeySymbols, r.Size, r.DominantLanguage,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) ReplaceFlowSnapshots(rows []FlowSnapshotRow) (int, error) {
	return s.replaceRows("flow_snapshots", len(rows), func(tx *sql.Tx) error {
		for _, r := range rows {
			if _, err := tx.Exec(
				`INSERT OR REPLACE INTO flow_snapshots
				   (flow_id, name, entry_point, critical_path, criticality,
				    node_count, file_count)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`,
				r.FlowID, r.Name, r.EntryPoint, r.CriticalPath, r.Criticality,
				r.NodeCount, r.FileCount,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) ReplaceRiskIndex(rows []RiskIndexRow) (int, error) {
	return s.replaceRows("risk_index", len(rows), func(tx *sql.Tx) error {
		for _, r := range rows {
			if _, err := tx.Exec(
				`INSERT OR REPLACE INTO risk_index
				   (node_id, qualified_name, risk_score, caller_count,
				    test_coverage, security_relevant, last_computed)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`,
				r.NodeID, r.QualifiedName, r.RiskScore, r.CallerCount,
				r.TestCoverage, boolToInt(r.SecurityRelevant), r.LastComputed,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// replaceRows is the DELETE-then-insert transaction shared by the three
// summary-table writers: one atomic generation swap, returning the row count
// the caller handed in.
func (s *SQLiteStore) replaceRows(table string, n int, insert func(*sql.Tx) error) (int, error) {
	err := s.derivedTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return err
		}
		return insert(tx)
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ApplyEdgeRewrites writes the recomputed endpoints and extra for each edge
// inside one transaction, so a resolution pass computed over the whole edge
// set cannot land half-applied and leave the graph in a state no pass would
// ever produce.
func (s *SQLiteStore) ApplyEdgeRewrites(rewrites []EdgeRewrite) (int, error) {
	if len(rewrites) == 0 {
		return 0, nil
	}
	err := s.derivedTx(func(tx *sql.Tx) error {
		for _, r := range rewrites {
			extra, err := encodeExtra(r.Extra)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(
				`UPDATE edges SET source_qualified = ?, target_qualified = ?, extra = ?
				 WHERE id = ?`,
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

// RebuildFTS reproduces upstream's rebuild_fts_index (search.py): drop the
// virtual table, re-declare it identically, then let FTS5's own 'rebuild'
// command repopulate it from the external content table in one pass. Dropping
// and re-creating rather than deleting rows is what makes the rebuild recover a
// nodes_fts whose declaration has drifted from the nodes columns it projects.
func (s *SQLiteStore) RebuildFTS() (int, error) {
	err := s.derivedTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DROP TABLE IF EXISTS nodes_fts"); err != nil {
			return err
		}
		if _, err := tx.Exec(ftsSchemaSQL); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild')")
		return err
	})
	if err != nil {
		return 0, err
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM nodes_fts").Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *SQLiteStore) SetNodeSignature(id int64, signature string) error {
	_, err := s.db.Exec("UPDATE nodes SET signature = ? WHERE id = ?", signature, id)
	return err
}

func (s *SQLiteStore) SetNodeCommunity(id int64, communityID int64) error {
	_, err := s.db.Exec("UPDATE nodes SET community_id = ? WHERE id = ?", communityID, id)
	return err
}

func (s *SQLiteStore) NodesWithoutSignature() ([]GraphNode, error) {
	rows, err := s.db.Query("SELECT * FROM nodes WHERE signature IS NULL ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows)
}

// ReadNodesByKind orders by (kind, id) because that is the order upstream's
// get_nodes_by_kind observably returns: its `kind IN (...)` predicate is served
// by idx_nodes_kind, so SQLite walks the index key-first and rowid-second. Entry
// point detection iterates this list and flows are sorted by criticality with a
// STABLE sort, so the order decides which of two equally critical flows comes
// first — it is part of the contract, not an incidental detail.
func (s *SQLiteStore) ReadNodesByKind(kinds []string) ([]GraphNode, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	args := make([]any, len(kinds))
	for i, k := range kinds {
		args[i] = k
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
	rows, err := s.db.Query(
		"SELECT * FROM nodes WHERE kind IN ("+placeholders+") ORDER BY kind, id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows)
}

// ReadNodesByID resolves a set of node ids in ONE batched query per 450 ids,
// which is the point of the method: the FTS readers hand back a handful of
// ids, and resolving them by scanning the whole nodes table is the exact
// cost upstream's batched `WHERE id IN (...)` fetch avoids. Rows come back in
// ascending id order (NOT the caller's input order) and ids with no row are
// silently skipped.
func (s *SQLiteStore) ReadNodesByID(ids []int64) ([]GraphNode, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	const batchSize = 450
	var out []GraphNode
	for start := 0; start < len(ids); start += batchSize {
		batch := ids[start:min(start+batchSize, len(ids))]
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		nodes, err := s.queryNodesIn(placeholders, args)
		if err != nil {
			return nil, err
		}
		out = append(out, nodes...)
	}
	if len(out) > 1 {
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	}
	return out, nil
}

// queryNodesIn runs one batch of ReadNodesByID's IN query.
func (s *SQLiteStore) queryNodesIn(placeholders string, args []any) ([]GraphNode, error) {
	rows, err := s.db.Query(
		"SELECT * FROM nodes WHERE id IN ("+placeholders+") ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows)
}

func (s *SQLiteStore) ReadNodesByCommunity(communityID int64) ([]GraphNode, error) {
	rows, err := s.db.Query("SELECT * FROM nodes WHERE community_id = ? ORDER BY id", communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows)
}

func (s *SQLiteStore) ReadAllNodes() ([]GraphNode, error) {
	rows, err := s.db.Query("SELECT * FROM nodes ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows)
}

func (s *SQLiteStore) ReadAllEdges() ([]GraphEdge, error) {
	rows, err := s.db.Query("SELECT * FROM edges ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectEdges(rows)
}

// encodeIDPath renders a flow's node-id path for flows.path_json the way
// Python's json.dumps does: "[8, 9]", with a space after each comma, and "[]"
// for an empty path rather than "null".
//
// encoding/json emits "[8,9]" instead. That matters because path_json is a
// stored TEXT column compared byte for byte against upstream's rows in the
// release-contract parity test — a missing space is a real divergence, not a
// formatting preference.
func encodeIDPath(path []int64) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, id := range path {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.FormatInt(id, 10))
	}
	b.WriteByte(']')
	return b.String()
}

// boolToInt renders a Go bool as SQLite's 0/1 integer boolean.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
