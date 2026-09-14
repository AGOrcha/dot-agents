// Package graphstore — submodule graph merge.
//
// Every statement this file executes is a FIXED string literal. Nothing —
// table name, schema alias, column list, repository scope, source database
// path — is ever interpolated into SQL text; the only variable inputs are bound
// parameters. That is why there is no scanner suppression for this file: there
// is no assembled SQL left to suppress. The cost of the fixed statements is
// that the merge now names the graph columns it carries, which is the same
// column contract CRGBridge.ReadNodes / ReadEdges already pin.
package graphstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// MergeStats reports what one graph merge copied.
type MergeStats struct {
	// Nodes is the number of node rows inserted into the destination.
	Nodes int `json:"nodes"`
	// Edges is the number of edge rows inserted into the destination.
	Edges int `json:"edges"`
}

// The two statements that manage the source attachment. The source path is
// bound, never interpolated.
const (
	attachSourceSQL = `ATTACH DATABASE ? AS src`
	detachSourceSQL = `DETACH DATABASE src`
)

// Table names, for error text only. They never reach a statement: each
// statement below spells its own table.
const (
	tableNodes = "nodes"
	tableEdges = "edges"
)

// nodeColumns / edgeColumns are the graph base-table columns the merge copies,
// and therefore the columns BOTH databases must have. They are exactly the
// CRG base schema CRGBridge.ReadNodes / ReadEdges read, minus the `id`
// autoincrement key — the destination assigns its own ids so two graphs merge
// without primary-key collisions.
//
// A source carrying EXTRA columns (a graph written by a newer CRG) still
// merges: the statements select only the columns named here. A source or
// destination MISSING one of them fails loudly and names the column, because
// copying a partial row would silently drop a repository's symbol metadata.
var (
	nodeColumns = []string{
		"kind", "name", "qualified_name", "file_path", "line_start", "line_end",
		"language", "parent_name", "params", "return_type", "is_test",
		"file_hash", "extra", "updated_at",
	}
	edgeColumns = []string{
		"kind", "source_qualified", "target_qualified", "file_path", "line",
		"extra", "updated_at",
	}
)

// Schema probes. `SELECT *` against an empty result set is how the merge reads
// back each side's ACTUAL column list without a PRAGMA whose output shape
// varies by SQLite build.
const (
	probeSrcNodes = `SELECT * FROM src.nodes LIMIT 0`
	probeDstNodes = `SELECT * FROM main.nodes LIMIT 0`
	probeSrcEdges = `SELECT * FROM src.edges LIMIT 0`
	probeDstEdges = `SELECT * FROM main.edges LIMIT 0`
)

// Scope-clearing deletes. A row belongs to the scope when a qualified name
// STARTS with the scope prefix, which is what `instr(col, ?) = 1` tests.
const (
	clearNodesSQL = `DELETE FROM main.nodes WHERE instr(qualified_name, ?) = 1`
	clearEdgesSQL = `DELETE FROM main.edges
	                 WHERE instr(source_qualified, ?) = 1
	                    OR instr(target_qualified, ?) = 1`
)

// Scoped copies. The two rewrites they apply are both load-bearing:
//
//   - qualified names gain a `<scope>::` prefix. CRG resolves edge endpoints by
//     qualified name, so without a discriminator a merged graph links `Button`
//     in one repository to `Button` in another and impact radius reports edges
//     no build could ever produce. Prefixing the node names and both edge
//     endpoints keeps every intra-repo edge intact while making a cross-repo
//     name match impossible.
//   - RELATIVE file paths gain the submodule's path prefix, so a merged row
//     still points at a file that exists relative to the superproject. The
//     CASE recognises the three absolute shapes CRG can store — POSIX roots
//     (/x), Windows drive letters (C:\x) and UNC paths (\\host\share) — and
//     leaves those untouched.
const (
	mergeNodesSQL = `INSERT OR IGNORE INTO main.nodes
	    (kind, name, qualified_name, file_path, line_start, line_end, language,
	     parent_name, params, return_type, is_test, file_hash, extra, updated_at)
	SELECT kind, name, ? || qualified_name,
	       CASE WHEN substr(file_path, 1, 1) IN ('/', '\') OR substr(file_path, 2, 1) = ':'
	            THEN file_path ELSE ? || file_path END,
	       line_start, line_end, language, parent_name, params, return_type,
	       is_test, file_hash, extra, updated_at
	  FROM src.nodes`

	mergeEdgesSQL = `INSERT OR IGNORE INTO main.edges
	    (kind, source_qualified, target_qualified, file_path, line, extra, updated_at)
	SELECT kind, ? || source_qualified, ? || target_qualified,
	       CASE WHEN substr(file_path, 1, 1) IN ('/', '\') OR substr(file_path, 2, 1) = ':'
	            THEN file_path ELSE ? || file_path END,
	       line, extra, updated_at
	  FROM src.edges`
)

// mergeScope carries the per-repository namespace applied to rows copied out of
// a submodule graph. Both fields are bound values, never statement text.
type mergeScope struct {
	prefix  string // "<scope>::"
	relBase string // "<scope>/"
}

func newMergeScope(scope string) mergeScope {
	return mergeScope{prefix: scope + scopeSeparator, relBase: scope + "/"}
}

// MergeGraphDB folds the submodule graph at srcPath into the already-open
// superproject graph db, namespacing every qualified name under scope.
//
// The merge is AUTHORITATIVE for its scope: it first deletes every row already
// carrying this scope, then copies the source's rows in. That makes it
// genuinely idempotent — `nodes` would survive on its unique qualified_name,
// but `edges` carries no unique constraint, so a re-merge would otherwise
// duplicate every edge — and it is also what lets a re-merge drop symbols the
// submodule deleted instead of leaving them behind forever.
//
// Only base tables are merged. Derived tables (FTS index, flows, communities)
// are deliberately NOT copied: they are rebuilt from the merged base rows by
// the postprocess pass, which is the only way derived state can be correct for
// the combined graph.
//
// Callers MUST run postprocess on the destination afterwards. Merging writes
// base rows only, which leaves the FTS index, flows, and communities stale —
// the exact trap the proposal recorded (a merged graph with populated
// nodes/edges and an EMPTY search index that still looked healthy).
// CRGBridge.BuildReport owns that ordering for the build path.
func MergeGraphDB(db *sql.DB, srcPath, scope string) (MergeStats, error) {
	if scope == "" {
		return MergeStats{}, fmt.Errorf("merge graph: empty scope (a merged repository must be namespaced)")
	}
	ctx := context.Background()
	// The whole merge runs on ONE pooled connection: ATTACH is per-connection
	// state, so a pool that handed the INSERTs a different connection would
	// not see the source database at all.
	conn, err := db.Conn(ctx)
	if err != nil {
		return MergeStats{}, fmt.Errorf("open merge connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, attachSourceSQL, srcPath); err != nil {
		return MergeStats{}, fmt.Errorf("attach source graph %s: %w", srcPath, err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, detachSourceSQL) }()

	// BEGIN IMMEDIATE takes the write lock up front: a merge that cannot write
	// must fail before it has copied half a repository.
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return MergeStats{}, fmt.Errorf("begin merge: %w", err)
	}
	scoped := newMergeScope(scope)
	nodes, err := mergeNodes(ctx, conn, scoped)
	if err != nil {
		rollback(ctx, conn)
		return MergeStats{}, err
	}
	edges, err := mergeEdges(ctx, conn, scoped)
	if err != nil {
		rollback(ctx, conn)
		return MergeStats{}, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		rollback(ctx, conn)
		return MergeStats{}, fmt.Errorf("commit merge: %w", err)
	}
	return MergeStats{Nodes: nodes, Edges: edges}, nil
}

// rollback abandons a failed merge. Its own failure is not actionable — the
// caller is already returning the error that caused it.
func rollback(ctx context.Context, conn *sql.Conn) {
	_, _ = conn.ExecContext(ctx, `ROLLBACK`)
}

// mergeNodes clears the scope's existing node rows and copies the source's in,
// returning the number inserted.
func mergeNodes(ctx context.Context, conn *sql.Conn, scope mergeScope) (int, error) {
	if err := requireColumns(ctx, conn, tableNodes, probeSrcNodes, probeDstNodes, nodeColumns); err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, clearNodesSQL, scope.prefix); err != nil {
		return 0, fmt.Errorf("clear previous %s rows for scope: %w", tableNodes, err)
	}
	return copyRows(ctx, conn, tableNodes, mergeNodesSQL, scope.prefix, scope.relBase)
}

// mergeEdges is mergeNodes for the edge table: both endpoints carry the scope,
// so either one matching clears the row.
func mergeEdges(ctx context.Context, conn *sql.Conn, scope mergeScope) (int, error) {
	if err := requireColumns(ctx, conn, tableEdges, probeSrcEdges, probeDstEdges, edgeColumns); err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, clearEdgesSQL, scope.prefix, scope.prefix); err != nil {
		return 0, fmt.Errorf("clear previous %s rows for scope: %w", tableEdges, err)
	}
	return copyRows(ctx, conn, tableEdges, mergeEdgesSQL, scope.prefix, scope.prefix, scope.relBase)
}

// copyRows runs one merge copy and reports the rows it inserted.
func copyRows(ctx context.Context, conn *sql.Conn, table, stmt string, args ...any) (int, error) {
	res, err := conn.ExecContext(ctx, stmt, args...)
	if err != nil {
		return 0, fmt.Errorf("merge %s rows: %w", table, err)
	}
	// RowsAffected is a report-only count; SQLite always supplies it.
	inserted, _ := res.RowsAffected()
	return int(inserted), nil
}

// requireColumns verifies that both sides of the merge carry every column the
// fixed statements name, and says which column is missing when one does not.
//
// A probe that fails outright (no such table) is reported the same way, so an
// absent `nodes` table and a `nodes` table missing `qualified_name` both point
// the operator at the same side of the merge.
func requireColumns(ctx context.Context, conn *sql.Conn, table, srcProbe, dstProbe string, required []string) error {
	srcCols, err := probeColumns(ctx, conn, srcProbe)
	if err != nil {
		return fmt.Errorf("read source %s schema: %w", table, err)
	}
	if missing := missingColumns(required, srcCols); len(missing) > 0 {
		return fmt.Errorf("source %s schema is missing required column(s): %s",
			table, strings.Join(missing, ", "))
	}
	dstCols, err := probeColumns(ctx, conn, dstProbe)
	if err != nil {
		return fmt.Errorf("read destination %s schema: %w", table, err)
	}
	if missing := missingColumns(required, dstCols); len(missing) > 0 {
		return fmt.Errorf("destination %s schema is missing required column(s): %s",
			table, strings.Join(missing, ", "))
	}
	return nil
}

// probeColumns returns the column names a probe statement reports.
func probeColumns(ctx context.Context, conn *sql.Conn, probe string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, probe)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Columns only fails on an already-closed Rows; this one was opened above.
	names, _ := rows.Columns()
	return names, nil
}

// missingColumns returns the required columns absent from have, in required
// order so the error text is deterministic.
func missingColumns(required, have []string) []string {
	present := make(map[string]bool, len(have))
	for _, c := range have {
		present[c] = true
	}
	var missing []string
	for _, c := range required {
		if !present[c] {
			missing = append(missing, c)
		}
	}
	return missing
}
