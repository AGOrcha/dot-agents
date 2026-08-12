package codegraph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"golang.org/x/sys/execabs"
)

// extraContentHash is the `extra` key carrying a symbol's own content hash.
const extraContentHash = "content_hash"

// defaultUpdateBase mirrors the bridge's default incremental diff base.
const defaultUpdateBase = "HEAD~1"

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
	store  graphstore.Store
	// changedFiles is the git-diff seam; tests substitute it.
	changedFiles func(root, base string) ([]string, error)
	// now is the clock seam.
	now func() time.Time
	// status is the readiness-read seam, always bound to (*Engine).Status.
	// The build/update report paths must handle a failing status read, and
	// Status itself reports every failure through State/Message rather than
	// an error, so the seam is the only way to exercise that arm.
	status func() (*graphstore.CRGStatus, error)
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
		changedFiles: gitChangedFiles,
		now:          time.Now,
	}
	e.status = e.Status
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

// ── Build / Update (§11.1 rows 1-2) ──────────────────────────────────────────

// BuildReport performs a full ingestion of the repository and returns the same
// structured report shape the bridge produced.
//
// opts.SkipFlows / opts.SkipPostprocess are accepted and have no effect: the
// kg-native backend derives flows, communities and the risk index on demand
// from persisted storage rather than materializing them during ingestion, so
// there is no post-processing pass for them to skip. Callers passing the flags
// still get a correct graph; the flags are kept in the signature so the CLI
// surface is unchanged.
func (e *Engine) BuildReport(_ graphstore.BuildOptions) (*graphstore.CRGOperationReport, error) {
	files, _, err := Scan(e.root, e.headCommit())
	if err != nil {
		return nil, err
	}
	store, err := e.writeStore()
	if err != nil {
		return nil, err
	}
	if err := e.removeStaleFiles(store, files); err != nil {
		return nil, err
	}
	nodes, edges, err := persistFiles(store, files)
	if err != nil {
		return nil, err
	}
	if err := e.stampUpdated(store); err != nil {
		return nil, err
	}
	status, err := e.status()
	if err != nil {
		return nil, err
	}
	return buildOutcomeReport("build", status, fmt.Sprintf("Build complete: %d nodes, %d edges, %d files", nodes, edges, len(files))), nil
}

// Build performs a full ingestion, discarding the report.
func (e *Engine) Build(opts graphstore.BuildOptions) error {
	_, err := e.BuildReport(opts)
	return err
}

// UpdateReport re-ingests only the files that changed since opts.Base. It
// mirrors the bridge's outcomes exactly: `no_diff` when git reports no changed
// files, `no_mutation` when the changed files produced no graph rows, and
// `updated` otherwise.
func (e *Engine) UpdateReport(opts graphstore.UpdateOptions) (*graphstore.CRGOperationReport, error) {
	changed, err := e.changedFiles(e.root, opts.Base)
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return e.noDiffReport()
	}
	files, _, err := Scan(e.root, e.headCommit())
	if err != nil {
		return nil, err
	}
	store, err := e.writeStore()
	if err != nil {
		return nil, err
	}
	nodes, edges, err := e.applyChangedFiles(store, files, changed)
	if err != nil {
		return nil, err
	}
	if err := e.stampUpdated(store); err != nil {
		return nil, err
	}
	status, err := e.status()
	if err != nil {
		return nil, err
	}
	return updateOutcomeReport(status, changed, nodes, edges), nil
}

// Update performs an incremental update, discarding the report.
func (e *Engine) Update(opts graphstore.UpdateOptions) error {
	_, err := e.UpdateReport(opts)
	return err
}

// noDiffReport is the empty-diff outcome: the graph is left untouched.
func (e *Engine) noDiffReport() (*graphstore.CRGOperationReport, error) {
	status, err := e.status()
	if err != nil {
		return nil, err
	}
	return &graphstore.CRGOperationReport{
		Operation: "update",
		Outcome:   "no_diff",
		Summary:   "No diff to apply; code graph left unchanged.",
		Status:    status,
	}, nil
}

// applyChangedFiles rewrites exactly the changed files' rows: a file that still
// parses is re-persisted, and one that no longer exists (deleted or renamed
// away) has its rows removed.
func (e *Engine) applyChangedFiles(store graphstore.Store, files []SourceFile, changed []string) (nodes, edges int, err error) {
	byPath := make(map[string]SourceFile, len(files))
	for _, f := range files {
		byPath[f.Path] = f
	}
	for _, path := range changed {
		sf, ok := byPath[path]
		if !ok {
			if rerr := store.RemoveFileData(path); rerr != nil {
				return nodes, edges, rerr
			}
			continue
		}
		n, ed, perr := persistFile(store, sf)
		if perr != nil {
			return nodes, edges, perr
		}
		nodes += n
		edges += ed
	}
	return nodes, edges, nil
}

// removeStaleFiles drops rows for files that a full rebuild no longer sees.
func (e *Engine) removeStaleFiles(store graphstore.Store, files []SourceFile) error {
	existing, err := store.GetAllFiles()
	if err != nil {
		return err
	}
	fresh := make(map[string]bool, len(files))
	for _, f := range files {
		fresh[f.Path] = true
	}
	for _, path := range existing {
		if !fresh[path] {
			if rerr := store.RemoveFileData(path); rerr != nil {
				return rerr
			}
		}
	}
	return nil
}

// stampUpdated records the build timestamp the status readiness check uses.
func (e *Engine) stampUpdated(store graphstore.Store) error {
	return store.SetMetadata("last_updated", e.now().UTC().Format(time.RFC3339))
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

// persistFile atomically replaces one file's nodes and edges. The File node is
// written alongside the symbols so file enumeration and the status file count
// behave exactly as they did on the bridge.
func persistFile(store graphstore.CodeGraphWriter, f SourceFile) (int, int, error) {
	nodes := []graphstore.NodeInfo{{
		Kind:     nodeKindFile,
		Name:     filepath.Base(f.Path),
		FilePath: f.Path,
		Language: languageGo,
		IsTest:   f.IsTest,
	}}
	for _, d := range f.Decls {
		nodes = append(nodes, nodeInfoFor(f, d))
	}
	edges := make([]graphstore.EdgeInfo, 0, len(f.References))
	for _, ref := range f.References {
		edges = append(edges, graphstore.EdgeInfo{
			Kind: ref.Kind, Source: ref.From, Target: ref.To, FilePath: f.Path,
		})
	}
	if err := store.StoreFileNodesEdges(f.Path, nodes, edges, f.FileHash); err != nil {
		return 0, 0, err
	}
	return len(nodes), len(edges), nil
}

// nodeInfoFor lowers a scanned declaration to the store's node shape. ParentName
// carries the package qualifier because the store derives `qualified_name` as
// `parent_name + "." + name`, which reproduces the scanner's qualified name
// exactly — the invariant every edge endpoint depends on.
func nodeInfoFor(f SourceFile, d Decl) graphstore.NodeInfo {
	return graphstore.NodeInfo{
		Kind:       d.Symbol.Kind,
		Name:       d.Local,
		FilePath:   f.Path,
		LineStart:  d.Symbol.LineStart,
		LineEnd:    d.LineEnd,
		Language:   languageGo,
		ParentName: f.PkgPath,
		IsTest:     f.IsTest,
		Extra:      map[string]any{extraContentHash: d.Symbol.ContentHash},
	}
}

// buildOutcomeReport classifies a build result against the graph's readiness,
// mirroring the bridge's outcome vocabulary one-for-one.
func buildOutcomeReport(op string, status *graphstore.CRGStatus, readySummary string) *graphstore.CRGOperationReport {
	report := &graphstore.CRGOperationReport{Operation: op, Status: status}
	switch {
	case status.Ready:
		report.Outcome = graphstore.CRGReadinessReady
		report.Summary = readySummary
	case status.State == graphstore.CRGReadinessBusyOrLocked:
		report.Outcome = graphstore.CRGReadinessBusyOrLocked
		report.Summary = "Build completed, but the code graph is busy or locked."
	case status.State == graphstore.CRGReadinessUnbuilt:
		report.Outcome = graphstore.CRGReadinessUnbuilt
		report.Summary = "Build completed but the code graph is still unbuilt."
	default:
		report.Outcome = graphstore.CRGReadinessError
		report.Summary = status.Message
	}
	return report
}

// updateOutcomeReport classifies an incremental update result.
func updateOutcomeReport(status *graphstore.CRGStatus, changed []string, nodes, edges int) *graphstore.CRGOperationReport {
	report := &graphstore.CRGOperationReport{
		Operation:    "update",
		Outcome:      "updated",
		ChangedFiles: changed,
		Status:       status,
		Summary:      fmt.Sprintf("Update complete: %d nodes, %d edges, %d files", status.Nodes, status.Edges, status.Files),
	}
	if nodes == 0 && edges == 0 {
		report.Outcome = "no_mutation"
		report.Summary = fmt.Sprintf("Changed %d files with no graph mutations.", len(changed))
	}
	return report
}

// ── Status (§11.1 row 3) ─────────────────────────────────────────────────────

// Status reports the persisted graph's counts and readiness. Like the bridge's
// Status it never returns an error for an absent or unreadable graph: the
// condition is surfaced through State/Message so `code-status` stays a
// diagnostic command rather than a failure.
func (e *Engine) Status() (*graphstore.CRGStatus, error) {
	status := &graphstore.CRGStatus{
		LastUpdated: "never",
		State:       graphstore.CRGReadinessUnbuilt,
	}
	store, err := e.readStore()
	if err != nil {
		status.State = graphstore.CRGReadinessError
		status.Message = err.Error()
		return status, nil
	}
	if store == nil {
		status.Message = "code graph database missing"
		return status, nil
	}
	stats, err := store.GetStats()
	if err != nil {
		status.State = graphstore.CRGReadinessError
		status.Message = err.Error()
		return status, nil
	}
	applyStats(status, stats)
	return status, nil
}

// applyStats projects store statistics onto the status shape and derives the
// readiness state.
func applyStats(status *graphstore.CRGStatus, stats graphstore.GraphStats) {
	status.Nodes = stats.TotalNodes
	status.Edges = stats.TotalEdges
	status.Files = stats.FilesCount
	status.Languages = strings.Join(stats.Languages, ", ")
	if strings.TrimSpace(stats.LastUpdated) != "" {
		status.LastUpdated = stats.LastUpdated
	}
	if status.Nodes > 0 && status.Files > 0 && status.LastUpdated != "never" {
		status.State = graphstore.CRGReadinessReady
		status.Ready = true
		return
	}
	status.State = graphstore.CRGReadinessUnbuilt
	status.Message = "code graph has not been built yet"
}

// ── Bulk export ──────────────────────────────────────────────────────────────

// ReadNodes exports persisted nodes (limit <= 0 exports all), matching the
// bridge's bulk-export semantics the warm-link sync depends on.
func (e *Engine) ReadNodes(limit int) ([]graphstore.GraphNode, error) {
	store, err := e.readStore()
	if err != nil || store == nil {
		return nil, err
	}
	nodes, err := readAllNodes(store)
	if err != nil {
		return nil, err
	}
	return truncate(nodes, limit), nil
}

// ReadEdges exports persisted edges (limit <= 0 exports all).
func (e *Engine) ReadEdges(limit int) ([]graphstore.GraphEdge, error) {
	store, err := e.readStore()
	if err != nil || store == nil {
		return nil, err
	}
	nodes, err := readAllNodes(store)
	if err != nil {
		return nil, err
	}
	edges, err := readAllEdges(store, nodes)
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

// gitChangedFiles returns the repo-relative paths changed between base and
// HEAD. It is the same `git diff --name-only base...HEAD` the bridge ran, with
// one deliberate improvement: when base does not resolve — the common case of a
// repository whose only commit is the root commit, where `HEAD~1` does not
// exist — it falls back to every tracked file instead of failing. The bridge
// hard-errored there, which made `build_or_update_graph_tool` unusable on a
// fresh repo; re-ingesting everything is the correct incremental answer when
// there is no earlier state to diff against.
func gitChangedFiles(root, base string) ([]string, error) {
	if base == "" {
		base = defaultUpdateBase
	}
	if !gitRevExists(root, base) {
		return gitTrackedFiles(root)
	}
	cmd := execabs.Command("git", "-C", root, "diff", "--name-only", "--diff-filter=ACMRTUXB", base+"...HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return nil, fmt.Errorf("git diff %s...HEAD: %s", base, msg)
		}
		return nil, fmt.Errorf("git diff %s...HEAD: %w", base, err)
	}
	return nonEmptyLines(string(out)), nil
}

// gitRevExists reports whether a revision resolves in root's repository.
func gitRevExists(root, rev string) bool {
	return execabs.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", rev+"^{commit}").Run() == nil
}

// gitTrackedFiles lists every tracked path, the fallback "everything changed"
// answer for a repository with no resolvable diff base.
func gitTrackedFiles(root string) ([]string, error) {
	out, err := execabs.Command("git", "-C", root, "ls-files").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	return nonEmptyLines(string(out)), nil
}

// nonEmptyLines splits output into trimmed, non-empty lines.
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

// headCommit returns the current HEAD sha, or "" outside a git repository. It
// only labels the ingested corpus, so it is never fatal.
func headCommit(root string) string {
	out, err := execabs.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// headCommit resolves this engine's repository HEAD.
func (e *Engine) headCommit() string { return headCommit(e.root) }
