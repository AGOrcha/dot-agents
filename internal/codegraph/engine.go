package codegraph

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// extraContentHash is the `extra` key carrying a symbol's own content hash.
const extraContentHash = "content_hash"

// nodeKindFile is the structural node kind used for file-level rows. A File
// node per source file is what makes `GetAllFiles`, the status file count and
// upstream's File-node language inventory work.
const nodeKindFile = graphstore.NodeKindFile

// Engine is the kg-native code-graph backend: it satisfies
// graphstore.CodeGraphProvider entirely in-process, with no Python subprocess.
//
// Ingestion writes through the published graphstore contract
// (graphstore.CodeGraphWriter) into a repo-local database, and every derived
// view — flows, communities, risk index, FTS, impact radius — is computed by
// reading that storage back through the crg adapter's parity-verified
// derivations. Nothing is served from the in-memory scan, so a divergent write
// is visible rather than papered over (the same readback discipline the §11.6
// parity oracles use).
type Engine struct {
	root   string
	dbPath string
	// store is the lazily opened handle every operation shares. It is
	// created by the first write and never by a read.
	store graphstore.Store
	// changedFiles is the changed-file discovery seam, bound to the shared
	// ReleaseChangedFiles port of upstream's get_changed_files. Tests
	// substitute it to drive a diff without a real repository.
	changedFiles func(root, base string) ([]string, error)
	// now is the clock seam. The lifecycle stamps build metadata with it,
	// so a test can pin every timestamp the report and `status` expose.
	now func() time.Time
	// session is this connection's MCP hint state. The release's `_hints`
	// block suppresses next-step suggestions for tools already called, so
	// the state is per-connection: two concurrent clients must not see each
	// other's suppressions. See tools_hints.go.
	session     *SessionState
	sessionOnce sync.Once
}

// Compile-time proof that the kg-native engine is a drop-in for the bridge.
var _ graphstore.CodeGraphProvider = (*Engine)(nil)

// Open returns an engine rooted at repoRoot. It performs no I/O: the database
// is created on the first write and is never created by a read, so running
// `da kg code-status` in a repo that has no graph reports "unbuilt" without
// leaving a directory behind (the bridge behaved the same way).
func Open(repoRoot string) *Engine {
	if repoRoot == "" {
		repoRoot = "."
	}
	e := &Engine{
		root:         repoRoot,
		dbPath:       graphstore.NativeGraphDBPath(repoRoot),
		changedFiles: ReleaseChangedFiles,
		now:          time.Now,
	}
	return e
}

// Close releases the underlying store handle if one was opened.
func (e *Engine) Close() error {
	if e.store == nil {
		return nil
	}
	store := e.store
	e.store = nil
	return store.Close()
}

// DBPath is the on-disk location of this engine's graph.
func (e *Engine) DBPath() string { return e.dbPath }

// built reports whether a graph database exists for this repo.
func (e *Engine) built() bool {
	_, err := os.Stat(e.dbPath)
	return err == nil
}

// writeStore opens (creating if needed) the store for a mutating operation.
func (e *Engine) writeStore() (graphstore.Store, error) {
	if e.store != nil {
		return e.store, nil
	}
	store, err := graphstore.OpenSQLite(e.dbPath)
	if err != nil {
		return nil, fmt.Errorf("codegraph: open %s: %w", e.dbPath, err)
	}
	e.store = store
	return store, nil
}

// readStore opens the store for a read-only operation, returning (nil, nil)
// when the graph has never been built. Every read path treats a nil store as
// "empty graph" so an unbuilt repo degrades to empty results instead of an
// error — the graceful behaviour the bridge's missing-database branch had.
func (e *Engine) readStore() (graphstore.Store, error) {
	if e.store != nil {
		return e.store, nil
	}
	if !e.built() {
		return nil, nil
	}
	return e.writeStore()
}

// persistFiles writes every scanned file and returns the total row counts.
func persistFiles(store graphstore.CodeGraphWriter, files []SourceFile) (nodes, edges int, err error) {
	for _, f := range files {
		n, ed, perr := persistFile(store, f)
		if perr != nil {
			return nodes, edges, perr
		}
		nodes += n
		edges += ed
	}
	return nodes, edges, nil
}

// persistFile atomically replaces one file's nodes and edges, keyed on the
// file's ABSOLUTE path — the identity every row in the graph carries. The File
// node is written first and the symbols follow in source order, so the rows'
// autoincrement ids run in upstream's emission order.
//
// The returned counts are the EXTRACTED counts, not the row counts: upstream's
// build totals sum the parser's per-file node and edge lists before its own
// upsert collapses any duplicate, so reporting row counts here would
// understate what upstream reports for the same tree.
func persistFile(store graphstore.CodeGraphWriter, f SourceFile) (int, int, error) {
	nodes := make([]graphstore.NodeInfo, 0, len(f.Decls)+1)
	nodes = append(nodes, graphstore.NodeInfo{
		// A File node's name IS its path: upstream names it by the path, so
		// the name, the derived qualified name and file_path are one string —
		// the string every file-sourced CONTAINS and IMPORTS_FROM edge names.
		Kind:      nodeKindFile,
		Name:      f.Path,
		FilePath:  f.Path,
		LineStart: 1,
		LineEnd:   f.LineCount,
		Language:  languageGo,
		IsTest:    f.IsTest,
	})
	for _, d := range f.Decls {
		nodes = append(nodes, nodeInfoFor(f, d))
	}
	if err := store.StoreFileNodesEdges(f.Path, nodes, edgeInfosFor(f), f.FileHash); err != nil {
		return 0, 0, err
	}
	return len(nodes), len(f.Edges), nil
}

// nodeInfoFor lowers a scanned declaration to the store's node shape. Name and
// ParentName stay separate — `Expired` and `Session`, never `Session.Expired` —
// because the store derives `qualified_name` from them as
// `<file_path>::<parent_name>.<name>`, which is exactly the identity the
// scanner recorded in Symbol.QualifiedName and named in every edge endpoint.
func nodeInfoFor(f SourceFile, d Decl) graphstore.NodeInfo {
	return graphstore.NodeInfo{
		Kind:       d.Symbol.Kind,
		Name:       d.Name,
		FilePath:   f.Path,
		LineStart:  d.Symbol.LineStart,
		LineEnd:    d.LineEnd,
		Language:   languageGo,
		ParentName: d.ParentName,
		Params:     d.Params,
		IsTest:     d.IsTest,
		Extra:      map[string]any{extraContentHash: d.Symbol.ContentHash},
	}
}

// edgeInfosFor lowers a file's edges to the store's edge shape, collapsing
// edges identical in (kind, source, target, file, line) to a single row. That
// collapse is upstream's: its edge write upserts on exactly that tuple, so two
// calls to the same unresolved name on one line are one row. The first
// occurrence wins, keeping insertion order — and therefore row ids — stable.
func edgeInfosFor(f SourceFile) []graphstore.EdgeInfo {
	out := make([]graphstore.EdgeInfo, 0, len(f.Edges))
	seen := make(map[Edge]bool, len(f.Edges))
	for _, e := range f.Edges {
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, graphstore.EdgeInfo{
			Kind: e.Kind, Source: e.Source, Target: e.Target,
			FilePath: f.Path, Line: e.Line,
		})
	}
	return out
}

// ── Bulk export ──────────────────────────────────────────────────────────────

// ReadNodes exports persisted nodes in primary-key order (limit <= 0 exports
// all), matching the bulk-export semantics the warm-link sync depends on.
func (e *Engine) ReadNodes(limit int) ([]graphstore.GraphNode, error) {
	store, err := e.readStore()
	if err != nil || store == nil {
		return nil, err
	}
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, err
	}
	return truncate(nodes, limit), nil
}

// ReadEdges exports persisted edges in primary-key order (limit <= 0 exports
// all).
func (e *Engine) ReadEdges(limit int) ([]graphstore.GraphEdge, error) {
	store, err := e.readStore()
	if err != nil || store == nil {
		return nil, err
	}
	edges, err := store.ReadAllEdges()
	if err != nil {
		return nil, err
	}
	return truncate(edges, limit), nil
}

// truncate applies a bulk-export limit; a non-positive limit exports everything.
func truncate[T any](items []T, limit int) []T {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

// ── git helpers ──────────────────────────────────────────────────────────────

// headCommit returns the current HEAD sha, or "" outside a git repository. It
// only labels the ingested corpus, so it is never fatal.
func headCommit(root string) string {
	_, sha := graphstore.GitBranchInfo(root)
	return sha
}

// headCommit resolves this engine's repository HEAD.
func (e *Engine) headCommit() string { return headCommit(e.root) }
