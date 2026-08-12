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

// srcSchema is the alias the source graph is attached under for the duration
// of a merge.
const srcSchema = "src"

// idColumn is the autoincrement primary key every CRG graph table carries. It
// is never copied: the destination assigns its own ids, so two graphs merge
// without primary-key collisions.
const idColumn = "id"

// mergeTarget binds one base graph table to the stats counter it feeds.
//
// Only base tables are merged. Derived tables (FTS index, flows, communities)
// are deliberately NOT copied: they are rebuilt from the merged base rows by
// the postprocess pass, which is the only way derived state can be correct for
// the combined graph.
type mergeTarget struct {
	table string
	count *int
}

// qualifiedNameColumns are the columns holding qualified names — the values
// CRG resolves edge endpoints against, and therefore the values that must
// carry a repository scope once two graphs share a database.
var qualifiedNameColumns = map[string]bool{
	"qualified_name":   true,
	"source_qualified": true,
	"target_qualified": true,
}

// filePathColumn holds a source file location, rebased for relative values.
const filePathColumn = "file_path"

// mergeScope carries the per-repository namespace applied to rows copied out
// of a submodule graph.
//
// Two rewrites happen, and both are load-bearing:
//
//   - qualified names gain a `<scope>::` prefix. CRG resolves edge endpoints
//     by qualified name, so without a discriminator a merged graph links
//     `Button` in one repository to `Button` in another and impact radius
//     reports edges that no build could ever produce. Prefixing the node names
//     and both edge endpoints keeps every intra-repo edge intact while making
//     a cross-repo name match impossible.
//   - relative file paths gain the submodule's path prefix, so a merged row
//     still points at a file that exists relative to the superproject. CRG
//     writes absolute paths today; absolute values are left untouched.
type mergeScope struct {
	prefix  string // "<scope>::"
	relBase string // "<scope>/"
}

func newMergeScope(scope string) mergeScope {
	return mergeScope{prefix: scope + scopeSeparator, relBase: scope + "/"}
}

// absolutePathTest is the SQL predicate for "this file path is already
// absolute". It covers POSIX roots (/x), Windows drive letters (C:\x) and UNC
// paths (\\host\share) — the three shapes CRG can store.
const absolutePathTest = `substr(%[1]s, 1, 1) IN ('/', '\') OR substr(%[1]s, 2, 1) = ':'`

// selectExpr returns the SELECT expression that reads col out of the source
// graph with this scope applied, plus the bind argument it needs (nil when the
// column is copied verbatim).
func (s mergeScope) selectExpr(col string) (expr string, arg any) {
	quoted := quoteIdent(col)
	switch {
	case qualifiedNameColumns[col]:
		return "? || " + quoted, s.prefix
	case col == filePathColumn:
		test := fmt.Sprintf(absolutePathTest, quoted)
		return "CASE WHEN " + test + " THEN " + quoted + " ELSE ? || " + quoted + " END", s.relBase
	default:
		return quoted, nil
	}
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
// The merge is transactional and schema-drift tolerant: only columns present
// in BOTH databases are copied, so a source produced by a different CRG
// version merges instead of failing outright.
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

	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS `+srcSchema, srcPath); err != nil {
		return MergeStats{}, fmt.Errorf("attach source graph %s: %w", srcPath, err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `DETACH DATABASE `+srcSchema) }()

	// BEGIN IMMEDIATE takes the write lock up front: a merge that cannot write
	// must fail before it has copied half a repository.
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return MergeStats{}, fmt.Errorf("begin merge: %w", err)
	}
	stats := MergeStats{}
	for _, target := range []mergeTarget{{"nodes", &stats.Nodes}, {"edges", &stats.Edges}} {
		n, mergeErr := mergeTable(ctx, conn, target.table, newMergeScope(scope))
		if mergeErr != nil {
			rollback(ctx, conn)
			return MergeStats{}, mergeErr
		}
		*target.count = n
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		rollback(ctx, conn)
		return MergeStats{}, fmt.Errorf("commit merge: %w", err)
	}
	return stats, nil
}

// rollback abandons a failed merge. Its own failure is not actionable — the
// caller is already returning the error that caused it.
func rollback(ctx context.Context, conn *sql.Conn) {
	_, _ = conn.ExecContext(ctx, `ROLLBACK`)
}

// mergeTable copies one table's rows from the attached source into the
// destination, applying scope, and returns the number of rows inserted.
func mergeTable(ctx context.Context, conn *sql.Conn, table string, scope mergeScope) (int, error) {
	srcCols, err := tableColumns(ctx, conn, srcSchema, table)
	if err != nil {
		return 0, fmt.Errorf("read source %s schema: %w", table, err)
	}
	dstCols, err := tableColumns(ctx, conn, "main", table)
	if err != nil {
		return 0, fmt.Errorf("read destination %s schema: %w", table, err)
	}
	// No shared columns means the two schemas have nothing in common. Copying
	// zero columns would look like success while losing a whole repository.
	cols := intersectColumns(srcCols, dstCols)
	if len(cols) == 0 {
		return 0, fmt.Errorf("source and destination %s tables share no columns", table)
	}

	// Clear this scope's previous rows first: the merge owns everything under
	// its namespace, so a re-merge replaces rather than accumulates.
	if err := clearScope(ctx, conn, table, cols, scope); err != nil {
		return 0, err
	}

	exprs := make([]string, len(cols))
	var args []any
	for i, col := range cols {
		expr, arg := scope.selectExpr(col)
		exprs[i] = expr
		if arg != nil {
			args = append(args, arg)
		}
	}
	insert := "INSERT OR IGNORE INTO main." + table + " (" + strings.Join(quoteIdents(cols), ", ") +
		") SELECT " + strings.Join(exprs, ", ") + " FROM " + srcSchema + "." + table
	res, err := conn.ExecContext(ctx, insert, args...)
	if err != nil {
		return 0, fmt.Errorf("merge %s rows: %w", table, err)
	}
	// RowsAffected is a report-only count; SQLite always supplies it.
	inserted, _ := res.RowsAffected()
	return int(inserted), nil
}

// clearScope deletes the destination rows that already belong to this scope,
// matching on whichever qualified-name columns the table actually has. A table
// with no qualified-name column carries no scope and is left alone.
func clearScope(ctx context.Context, conn *sql.Conn, table string, cols []string, scope mergeScope) error {
	var predicates []string
	var args []any
	for _, col := range cols {
		if qualifiedNameColumns[col] {
			predicates = append(predicates, "instr("+quoteIdent(col)+", ?) = 1")
			args = append(args, scope.prefix)
		}
	}
	if len(predicates) == 0 {
		return nil
	}
	del := "DELETE FROM main." + table + " WHERE " + strings.Join(predicates, " OR ")
	if _, err := conn.ExecContext(ctx, del, args...); err != nil {
		return fmt.Errorf("clear previous %s rows for scope: %w", table, err)
	}
	return nil
}

// tableColumns returns the copyable columns of schema.table — everything but
// the primary key, which the destination assigns itself.
func tableColumns(ctx context.Context, conn *sql.Conn, schema, table string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, `SELECT * FROM `+schema+`.`+table+` LIMIT 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Columns only fails on an already-closed Rows; this one was opened above.
	names, _ := rows.Columns()
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name != idColumn {
			out = append(out, name)
		}
	}
	return out, nil
}

// intersectColumns returns the columns present in both schemas, in src order.
func intersectColumns(src, dst []string) []string {
	inDst := make(map[string]bool, len(dst))
	for _, c := range dst {
		inDst[c] = true
	}
	var out []string
	for _, c := range src {
		if inDst[c] {
			out = append(out, c)
		}
	}
	return out
}

// quoteIdent double-quotes a SQLite identifier so a column named after a
// keyword still parses.
func quoteIdent(col string) string {
	return `"` + strings.ReplaceAll(col, `"`, `""`) + `"`
}

// quoteIdents applies quoteIdent to each column.
func quoteIdents(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = quoteIdent(c)
	}
	return out
}
