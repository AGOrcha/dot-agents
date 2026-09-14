package codegraph

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// This file is the kg-native port of upstream code-review-graph v2.3.8's
// build / update / post-process lifecycle: `tools/build.py`'s
// build_or_update_graph and run_postprocess, driving `incremental.py`'s
// full_build, incremental_update and resolve_incremental_base.
//
// The result dicts those functions return are a contract, not a convenience:
// which keys are PRESENT distinguishes a full build from an incremental one,
// an incremental early return from a real update, and a standalone
// post-process from a post-build pass. testdata/crg-release/v2.3.8/
// lifecycle.json pins every case, and lifecycle_test.go drives this file
// against it.

// upstreamTimeLayout is the timestamp every metadata key upstream writes
// uses: `time.strftime("%Y-%m-%dT%H:%M:%S")` — local time, no offset.
const upstreamTimeLayout = "2006-01-02T15:04:05"

// sqliteNowLayout is SQLite's `datetime('now')` rendering, which is what
// upstream stamps risk_index rows with.
const sqliteNowLayout = "2006-01-02 15:04:05"

// Metadata keys the lifecycle writes. They are upstream's exactly, so a
// graph built by either backend reads the same way.
const (
	metaSchemaVersion     = "schema_version"
	metaLastUpdated       = "last_updated"
	metaLastBuildType     = "last_build_type"
	metaGitBranch         = "git_branch"
	metaGitHeadSHA        = "git_head_sha"
	metaSVNBranch         = "svn_branch"
	metaSVNRevision       = "svn_revision"
	metaLastPostprocessed = "last_postprocessed_at"
	metaPostprocessLevel  = "postprocess_level"
)

// Build types the report's `build_type` field carries.
const (
	buildTypeFull        = "full"
	buildTypeIncremental = "incremental"
)

// nonGitAutoBase is the base upstream's resolve_incremental_base returns for
// a working copy that is not git: SVN change discovery ignores the base and
// a plain directory has no history, so the value only has to be harmless.
const nonGitAutoBase = "HEAD~1"

// defaultDependentHops / maxDependentFiles are upstream's dependent-expansion
// bounds: CRG_DEPENDENT_HOPS iterations (2 by default) and a hard 500-file
// cap, so one churned hub file cannot turn an incremental update into a
// whole-repository re-parse.
const (
	defaultDependentHops = 2
	maxDependentFiles    = 500
)

// dependentHops reads upstream's CRG_DEPENDENT_HOPS override.
func dependentHops() int {
	if raw := os.Getenv("CRG_DEPENDENT_HOPS"); raw != "" {
		if hops, err := strconv.Atoi(raw); err == nil && hops > 0 {
			return hops
		}
	}
	return defaultDependentHops
}

// dependentEdgeKinds are the edge kinds that make one file depend on
// another, per upstream's _single_hop_dependents.
var dependentEdgeKinds = map[string]bool{
	graphstore.EdgeKindCalls:       true,
	graphstore.EdgeKindImportsFrom: true,
	graphstore.EdgeKindInherits:    true,
	graphstore.EdgeKindImplements:  true,
}

// ── Build (§11.1 row 1) ──────────────────────────────────────────────────────

// BuildReport performs a full ingestion and returns upstream's
// build_or_update_graph(full_rebuild=True) result.
func (e *Engine) BuildReport(opts graphstore.BuildOptions) (*graphstore.CRGOperationReport, error) {
	target, release, err := e.rooted(opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	defer release()
	return target.fullBuildReport(nil, graphstore.NormalizePostprocessLevel(opts.Postprocess),
		opts.RecurseSubmodules, opts.EmbeddingProvider, opts.EmbeddingModel)
}

// Build performs a full ingestion, discarding the report.
func (e *Engine) Build(opts graphstore.BuildOptions) error {
	_, err := e.BuildReport(opts)
	return err
}

// fullBuildReport is the full-rebuild arm shared by Build and by an Update
// that had to fall back to one. A nil store is opened by the build itself,
// which is what lets a scan failure surface before a database exists.
func (e *Engine) fullBuildReport(store graphstore.Store, level string, recurseSubmodules *bool, provider, model string) (*graphstore.CRGOperationReport, error) {
	store, result, err := e.fullBuild(store, recurseSubmodules)
	if err != nil {
		return nil, err
	}
	report := &graphstore.CRGOperationReport{
		Status:            statusOK,
		BuildType:         buildTypeFull,
		BaseResolved:      graphstore.NullString(),
		Summary:           graphstore.FullBuildSummary(result.filesParsed, result.nodes, result.edges),
		FilesParsed:       new(result.filesParsed),
		TotalNodes:        new(result.nodes),
		TotalEdges:        new(result.edges),
		StaleFilesRemoved: new(result.staleRemoved),
		Errors:            new(result.errors),
		// A full build runs every language resolver unconditionally, and
		// each reports zeroes when the graph holds no file of its
		// language. The kg-native scanner parses Go only, so that is
		// always the answer here.
		ResolverStats: graphstore.ZeroResolverStats(),
	}
	if err := e.runPostBuild(store, report, level, true, nil, provider, model); err != nil {
		return nil, err
	}
	return report, nil
}

// fullBuild is upstream's full_build: re-parse the whole repository, purge
// the files the fresh inventory no longer sees, and stamp the build.
//
// The scan runs BEFORE the store is opened so an unreadable repository root
// fails without leaving a graph directory behind — the same "a read never
// creates the database" discipline Open documents.
func (e *Engine) fullBuild(store graphstore.Store, recurseSubmodules *bool) (graphstore.Store, ingestResult, error) {
	scanned, _, err := Scan(e.root, e.headCommit())
	if err != nil {
		return nil, ingestResult{}, err
	}
	scanned = excludeSubmodules(e.root, scanned, recurseSubmodules)
	if store == nil {
		if store, err = e.writeStore(); err != nil {
			return nil, ingestResult{}, err
		}
	}
	stale, err := e.reconcileScannedFiles(store, scanned)
	if err != nil {
		return nil, ingestResult{}, err
	}
	nodes, edges, err := persistFiles(store, scanned)
	if err != nil {
		return nil, ingestResult{}, err
	}
	if err := e.stampBuild(store, buildTypeFull); err != nil {
		return nil, ingestResult{}, err
	}
	return store, ingestResult{
		filesParsed:  len(scanned),
		nodes:        nodes,
		edges:        edges,
		staleRemoved: stale,
		errors:       []graphstore.BuildErrorRow{},
	}, nil
}

// ── Update (§11.1 row 2) ─────────────────────────────────────────────────────

// UpdateReport re-ingests only what changed since the resolved base and
// returns upstream's build_or_update_graph(full_rebuild=False) result.
//
// It owns both of upstream's implicit escalations to a full rebuild, so no
// caller has to reproduce them: a graph with no nodes cannot be updated
// incrementally, and an automatic base that resolves to nothing must not be
// silently replaced by HEAD~1 — that would report an out-of-date graph as up
// to date. Callers therefore never pre-empt this method.
func (e *Engine) UpdateReport(opts graphstore.UpdateOptions) (*graphstore.CRGOperationReport, error) {
	target, release, err := e.rooted(opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	defer release()
	level := graphstore.NormalizePostprocessLevel(opts.Postprocess)
	store, err := target.writeStore()
	if err != nil {
		return nil, err
	}

	fullRebuild := opts.FullRebuild
	if !fullRebuild {
		populated, err := hasNodes(store)
		if err != nil {
			return nil, err
		}
		fullRebuild = !populated
	}

	base := opts.Base
	if !fullRebuild && base == "" && !opts.BaseSet {
		resolved, ok, err := target.resolveIncrementalBase(store)
		if err != nil {
			return nil, err
		}
		if !ok {
			fullRebuild = true
		}
		base = resolved
	}

	if fullRebuild {
		return target.fullBuildReport(store, level, opts.RecurseSubmodules, opts.EmbeddingProvider, opts.EmbeddingModel)
	}

	result, err := target.incrementalUpdate(store, base)
	if err != nil {
		return nil, err
	}
	report := &graphstore.CRGOperationReport{
		Status:            statusOK,
		BuildType:         buildTypeIncremental,
		BaseResolved:      graphstore.SomeString(base),
		FilesUpdated:      new(result.filesUpdated),
		TotalNodes:        new(result.nodes),
		TotalEdges:        new(result.edges),
		ChangedFiles:      new(result.changedFiles),
		DependentFiles:    new(result.dependentFiles),
		StaleFilesRemoved: new(result.staleRemoved),
		Errors:            new(result.errors),
	}
	if !result.early {
		// Upstream runs a language resolver only when a file of that
		// language changed and records None for every one it skipped.
		// The early return never reaches them at all, which is why the
		// keys are absent rather than null there.
		report.ResolverStats = graphstore.NullResolverStats()
	}
	if result.filesUpdated == 0 {
		report.Summary = graphstore.NoChangesSummary()
		report.PostprocessLevel = level
		return report, nil
	}
	report.Summary = graphstore.IncrementalSummary(
		result.filesUpdated, result.nodes, result.edges, result.changedFiles, result.dependentFiles)
	if err := e.runPostBuild(store, report, level, false, result.changedFiles, opts.EmbeddingProvider, opts.EmbeddingModel); err != nil {
		return nil, err
	}
	return report, nil
}

// Update performs an incremental update, discarding the report.
func (e *Engine) Update(opts graphstore.UpdateOptions) error {
	_, err := e.UpdateReport(opts)
	return err
}

// resolveIncrementalBase ports upstream's resolve_incremental_base.
//
// The graph records the commit it was last built at, and diffing against
// THAT — rather than a fixed HEAD~1 — is what makes one update reconcile
// everything since the last sync instead of only the most recent commit. The
// second return reports whether a usable base exists at all; false means the
// caller must fall back to a full rebuild rather than diff against a base
// that would under-report.
func (e *Engine) resolveIncrementalBase(store graphstore.CodeGraphReader) (string, bool, error) {
	if graphstore.DetectVCS(e.root) != graphstore.VCSGit {
		return nonGitAutoBase, true, nil
	}
	stored, err := store.GetMetadata(metaGitHeadSHA)
	if err != nil {
		return "", false, err
	}
	if stored != "" && graphstore.GitCommitExists(e.root, stored) {
		return stored, true, nil
	}
	return "", false, nil
}

// ingestResult is the shared outcome of upstream's full_build and
// incremental_update: the counts a run produced plus the bookkeeping the
// report reproduces verbatim.
type ingestResult struct {
	filesParsed    int
	filesUpdated   int
	nodes          int
	edges          int
	changedFiles   []string
	dependentFiles []string
	staleRemoved   int
	errors         []graphstore.BuildErrorRow
	// early marks upstream's short return from incremental_update — the
	// diff was empty and nothing was reconciled, so the result dict never
	// grew the per-language resolver keys.
	early bool
}

// incrementalUpdate is upstream's incremental_update: re-parse the changed
// files and the files that depend on them, purge what disappeared, and stamp
// the update only when something actually moved.
func (e *Engine) incrementalUpdate(store graphstore.Store, base string) (ingestResult, error) {
	if err := assertGraphMatchesRoot(e.absRoot(), store); err != nil {
		return ingestResult{}, err
	}
	changed, err := e.changedFiles(e.root, base)
	if err != nil {
		return ingestResult{}, err
	}
	stale, err := e.reconcileStoredFiles(store)
	if err != nil {
		return ingestResult{}, err
	}
	if len(changed) == 0 && stale == 0 {
		return ingestResult{
			changedFiles:   []string{},
			dependentFiles: []string{},
			errors:         []graphstore.BuildErrorRow{},
			early:          true,
		}, nil
	}

	dependents, err := e.dependentsOf(store, changed)
	if err != nil {
		return ingestResult{}, err
	}
	candidates := append(append([]string{}, changed...), dependents...)

	ingest, err := e.ingestChangedFiles(store, candidates)
	if err != nil {
		return ingestResult{}, err
	}
	result := ingestResult{
		filesUpdated:   ingest.parsed + stale + ingest.purged,
		nodes:          ingest.nodes,
		edges:          ingest.edges,
		changedFiles:   changed,
		dependentFiles: dependents,
		staleRemoved:   stale,
		errors:         ingest.errors,
	}
	if result.filesUpdated > 0 {
		if err := e.stampBuild(store, buildTypeIncremental); err != nil {
			return ingestResult{}, err
		}
	}
	return result, nil
}

// changedFileIngest is the per-file outcome of one incremental re-ingest.
type changedFileIngest struct {
	parsed int
	nodes  int
	edges  int
	purged int
	errors []graphstore.BuildErrorRow
}

// ingestChangedFiles re-parses the candidate paths that still exist and whose
// content actually moved, and purges the ones that no longer do.
//
// The hash check matters: a dependent file is pulled in because a file it
// uses changed, not because it changed itself, so without it every update
// would rewrite the whole dependency cone's rows and churn their timestamps.
func (e *Engine) ingestChangedFiles(store graphstore.Store, rels []string) (changedFileIngest, error) {
	ingest := changedFileIngest{errors: []graphstore.BuildErrorRow{}}
	scanned, err := ScanPaths(e.root, rels)
	if err != nil {
		return ingest, err
	}
	parseable := make(map[string]SourceFile, len(scanned))
	for _, sf := range scanned {
		parseable[sf.Path] = sf
	}
	stored, err := storedFileSet(store)
	if err != nil {
		return ingest, err
	}

	var missing []string
	for _, rel := range rels {
		abs := e.absPath(rel)
		sf, ok := parseable[abs]
		if !ok {
			// A path the scanner did not return is either gone (purge
			// its rows) or simply not source we index (leave it be).
			if !isRegularFile(abs) && stored[abs] {
				missing = append(missing, abs)
			}
			continue
		}
		unchanged, err := fileHashMatches(store, abs, sf.FileHash)
		if err != nil {
			return ingest, err
		}
		if unchanged {
			continue
		}
		nodes, edges, err := persistFile(store, sf)
		if err != nil {
			return ingest, err
		}
		ingest.parsed++
		ingest.nodes += nodes
		ingest.edges += edges
	}

	sort.Strings(missing)
	for _, abs := range missing {
		if err := store.RemoveFileData(abs); err != nil {
			return ingest, err
		}
		ingest.purged++
	}
	return ingest, nil
}

// fileHashMatches reports whether the graph already holds this exact content.
func fileHashMatches(store graphstore.CodeGraphReader, absPath, hash string) (bool, error) {
	existing, err := store.GetNodesByFile(absPath)
	if err != nil {
		return false, err
	}
	return len(existing) > 0 && existing[0].FileHash == hash, nil
}

// dependentsOf returns the repo-relative paths of every file that depends on
// one of the changed files, sorted, with upstream's hop and size bounds.
func (e *Engine) dependentsOf(store graphstore.CodeGraphReader, changed []string) ([]string, error) {
	found := map[string]bool{}
	for _, rel := range changed {
		deps, err := findDependents(store, e.absPath(rel))
		if err != nil {
			return nil, err
		}
		for _, dep := range deps {
			found[e.relPath(dep)] = true
		}
	}
	dependents := make([]string, 0, len(found))
	for dep := range found {
		dependents = append(dependents, dep)
	}
	sort.Strings(dependents)
	return dependents, nil
}

// findDependents is upstream's find_dependents: breadth-first expansion of
// the reverse dependency graph, bounded to dependentHops() iterations and
// maxDependentFiles results.
func findDependents(store graphstore.CodeGraphReader, absPath string) ([]string, error) {
	all := map[string]bool{}
	visited := map[string]bool{absPath: true}
	frontier := []string{absPath}
	for hop := dependentHops(); hop > 0 && len(frontier) > 0; hop-- {
		var next []string
		for _, path := range frontier {
			deps, err := singleHopDependents(store, path)
			if err != nil {
				return nil, err
			}
			for _, dep := range deps {
				if visited[dep] {
					continue
				}
				visited[dep] = true
				all[dep] = true
				next = append(next, dep)
			}
		}
		frontier = next
		if len(all) > maxDependentFiles {
			break
		}
	}
	out := make([]string, 0, len(all))
	for dep := range all {
		out = append(out, dep)
	}
	sort.Strings(out)
	if len(out) > maxDependentFiles {
		out = out[:maxDependentFiles]
	}
	return out, nil
}

// singleHopDependents returns the files that directly depend on absPath:
// those importing the file itself, and those whose edges target one of the
// symbols the file declares.
func singleHopDependents(store graphstore.CodeGraphReader, absPath string) ([]string, error) {
	deps := map[string]bool{}
	fileEdges, err := store.GetEdgesByTarget(absPath)
	if err != nil {
		return nil, err
	}
	for _, edge := range fileEdges {
		if edge.Kind == graphstore.EdgeKindImportsFrom {
			deps[edge.FilePath] = true
		}
	}
	nodes, err := store.GetNodesByFile(absPath)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		edges, err := store.GetEdgesByTarget(node.QualifiedName)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if dependentEdgeKinds[edge.Kind] {
				deps[edge.FilePath] = true
			}
		}
	}
	delete(deps, absPath)
	out := make([]string, 0, len(deps))
	for dep := range deps {
		out = append(out, dep)
	}
	sort.Strings(out)
	return out, nil
}

// ── Stale reconciliation ─────────────────────────────────────────────────────

// reconcileScannedFiles purges graph rows for every stored file the fresh
// inventory no longer contains, and returns how many it removed.
func (e *Engine) reconcileScannedFiles(store graphstore.Store, scanned []SourceFile) (int, error) {
	current := make(map[string]bool, len(scanned))
	for _, sf := range scanned {
		current[sf.Path] = true
	}
	return purgeStale(store, func(path string) bool { return current[path] })
}

// reconcileStoredFiles is the incremental arm: there is no fresh inventory,
// so each stored file is re-checked on disk instead of re-walking the tree.
func (e *Engine) reconcileStoredFiles(store graphstore.Store) (int, error) {
	return purgeStale(store, func(path string) bool {
		return isRegularFile(path) && strings.HasSuffix(path, ".go")
	})
}

// purgeStale removes every stored file that keep rejects, in sorted order so
// a partial failure is reproducible.
func purgeStale(store graphstore.Store, keep func(path string) bool) (int, error) {
	stored, err := store.GetAllFiles()
	if err != nil {
		return 0, err
	}
	var stale []string
	for _, path := range stored {
		if !keep(path) {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	for _, path := range stale {
		if err := store.RemoveFileData(path); err != nil {
			return 0, err
		}
	}
	return len(stale), nil
}

// assertGraphMatchesRoot refuses an incremental reconciliation against a
// graph that was built somewhere else. Without it, every stored file would
// look stale and the update would delete a perfectly good graph instead of
// reporting the mismatch.
func assertGraphMatchesRoot(root string, store graphstore.CodeGraphReader) error {
	stored, err := store.GetAllFiles()
	if err != nil {
		return err
	}
	if len(stored) == 0 {
		return nil
	}
	prefix := NormalizeFilePath(root)
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for _, path := range stored {
		if strings.HasPrefix(filepath.ToSlash(path), prefix) {
			return nil
		}
	}
	sorted := append([]string{}, stored...)
	sort.Strings(sorted)
	return fmt.Errorf(
		"codegraph: the graph holds %d file(s) such as %q, none of them under %q; "+
			"it was built with a different repository root. Rebuild it, or retry with "+
			"the root it was built with, instead of reconciling every file away",
		len(stored), sorted[0], root)
}

// ── Post-processing (§11.1 row 7) ────────────────────────────────────────────

// PostprocessReport runs a STANDALONE post-processing pass and returns
// upstream's run_postprocess result.
//
// It is deliberately not the same pass a build runs. Upstream's standalone
// form recomputes call-target resolution, signatures, the FTS index, flows
// and communities, but never the three pre-computed summary tables — so
// community_summaries, flow_snapshots and risk_index keep whatever the last
// full build wrote. Recomputing them here would be an eager refresh upstream
// does not do, and the `summary_tables_after_standalone_postprocess` fixture
// is what pins the difference.
func (e *Engine) PostprocessReport(opts graphstore.PostprocessOptions) (*graphstore.CRGOperationReport, error) {
	report := &graphstore.CRGOperationReport{
		Status:   statusOK,
		Summary:  graphstore.PostprocessSummary(),
		Warnings: graphstore.EmbeddingRefreshWarnings(opts.EmbeddingProvider, opts.EmbeddingModel),
	}
	target, release, err := e.rooted(opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	defer release()
	store, err := target.readStore()
	if err != nil {
		return nil, err
	}
	if store == nil {
		// Nothing built yet: a post-process is a no-op, not a failure.
		return report, nil
	}

	resolveCallTargets(report)
	if err := updateSignatures(store, report); err != nil {
		return nil, err
	}
	if opts.FTSEnabled() {
		indexed, err := store.RebuildFTS()
		if err != nil {
			return nil, err
		}
		report.FTSIndexed = new(indexed)
	}
	nodes, edges, err := readGraph(store)
	if err != nil {
		return nil, err
	}
	if opts.FlowsEnabled() {
		detected, err := traceAllFlows(store, nodes, edges)
		if err != nil {
			return nil, err
		}
		report.FlowsDetected = new(detected)
	}
	if opts.CommunitiesEnabled() {
		detected, err := detectAllCommunities(store, nodes, edges)
		if err != nil {
			return nil, err
		}
		report.CommunitiesDetected = new(detected)
	}
	if err := store.SetMetadata(metaLastPostprocessed, target.stamp()); err != nil {
		return nil, err
	}
	return report, store.Commit()
}

// Postprocess runs a standalone post-processing pass, discarding the report.
func (e *Engine) Postprocess(opts graphstore.PostprocessOptions) error {
	_, err := e.PostprocessReport(opts)
	return err
}

// runPostBuild is upstream's _run_postprocess: the pass a build or update
// runs, which — unlike the standalone form — DOES refresh the three summary
// tables, and does so only at the "full" level.
func (e *Engine) runPostBuild(store graphstore.Store, report *graphstore.CRGOperationReport, level string, fullRebuild bool, changed []string, provider, model string) error {
	report.PostprocessLevel = level
	report.Warnings = append(report.Warnings, graphstore.EmbeddingRefreshWarnings(provider, model)...)
	if level == graphstore.PostprocessNone {
		return nil
	}

	resolveCallTargets(report)

	timing := &graphstore.PostprocessTiming{}
	started := time.Now()
	if err := updateSignatures(store, report); err != nil {
		return err
	}
	timing.SignaturesS = secondsSince(started)

	started = time.Now()
	indexed, err := store.RebuildFTS()
	if err != nil {
		return err
	}
	report.FTSIndexed, report.FTSRebuilt = new(indexed), new(true)
	timing.FTSS = secondsSince(started)

	if level == graphstore.PostprocessMinimal {
		// Upstream returns here, BEFORE stamping last_postprocessed_at
		// and postprocess_level: only a full pass records them.
		report.PostprocessTiming = timing
		return nil
	}

	nodes, edges, err := readGraph(store)
	if err != nil {
		return err
	}
	// An incremental update re-traces only the flows and communities the
	// changed files touch; a full rebuild recomputes both outright.
	incremental := !fullRebuild && len(changed) > 0

	started = time.Now()
	flows, err := e.detectFlows(store, nodes, edges, incremental, changed)
	if err != nil {
		return err
	}
	report.FlowsDetected = new(flows)
	timing.FlowsS = new(secondsSince(started))

	started = time.Now()
	communities, err := e.detectCommunities(store, nodes, edges, incremental, changed)
	if err != nil {
		return err
	}
	report.CommunitiesDetected = new(communities)
	timing.CommunitiesS = new(secondsSince(started))

	started = time.Now()
	if err := e.refreshSummaries(store, edges); err != nil {
		return err
	}
	report.SummariesComputed = new(true)
	timing.SummariesS = new(secondsSince(started))

	report.PostprocessTiming = timing
	if err := store.SetMetadata(metaLastPostprocessed, e.stamp()); err != nil {
		return err
	}
	if err := store.SetMetadata(metaPostprocessLevel, level); err != nil {
		return err
	}
	return store.Commit()
}

// resolveCallTargets records the bare and C++-scoped call-target resolution
// counters.
//
// Upstream resolves call targets that the parser left as bare names, which
// for its corpus means Python module-qualified calls and C++ `Class::method`
// selectors. The kg-native scanner parses Go, where a cross-package or
// method-selector target is left bare BY DESIGN (graph.json pins that), and
// there is no second pass that could resolve one. Reporting upstream's zero
// is therefore the honest answer, not a placeholder — but it is also the
// reason a polyglot repository must route through the retained bridge.
func resolveCallTargets(report *graphstore.CRGOperationReport) {
	report.BareEdgesResolved, report.CPPScopedEdgesResolved = new(0), new(0)
}

// updateSignatures backfills the `signature` column for every node that
// lacks one, using upstream's rendering.
func updateSignatures(store graphstore.Store, report *graphstore.CRGOperationReport) error {
	pending, err := store.NodesWithoutSignature()
	if err != nil {
		return err
	}
	for _, node := range pending {
		if err := store.SetNodeSignature(node.ID, crg.NodeSignature(node)); err != nil {
			return err
		}
	}
	if err := store.Commit(); err != nil {
		return err
	}
	report.SignaturesUpdated = new(true)
	return nil
}

// readGraph loads the node and edge slices every derived-view computation
// reads. They are loaded once and shared, which is also how the parity
// oracle drives them.
func readGraph(store graphstore.Store) ([]graphstore.GraphNode, []graphstore.GraphEdge, error) {
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, nil, err
	}
	edges, err := store.ReadAllEdges()
	if err != nil {
		return nil, nil, err
	}
	return nodes, edges, nil
}

// detectFlows recomputes execution flows, incrementally when only a known
// set of files moved.
func (e *Engine) detectFlows(store graphstore.Store, nodes []graphstore.GraphNode, edges []graphstore.GraphEdge, incremental bool, changed []string) (int, error) {
	if !incremental {
		return traceAllFlows(store, nodes, edges)
	}
	existing, err := store.ReadFlows()
	if err != nil {
		return 0, err
	}
	retraced, retracedPaths, keep, keepPaths := crg.IncrementalTraceFlows(
		nodes, edges, existing, changed, crg.DefaultFlowMaxDepth)
	if _, err := store.ReplaceFlows(append(keep, retraced...), append(keepPaths, retracedPaths...)); err != nil {
		return 0, err
	}
	// Upstream reports the number of flows it RE-TRACED, not the total it
	// kept, so an update that touched nothing reports zero.
	return len(retraced), nil
}

// traceAllFlows recomputes every flow from scratch.
func traceAllFlows(store graphstore.Store, nodes []graphstore.GraphNode, edges []graphstore.GraphEdge) (int, error) {
	flows, paths := crg.TraceFlows(nodes, edges, crg.DefaultFlowMaxDepth, false)
	return store.ReplaceFlows(flows, paths)
}

// detectCommunities recomputes communities, skipping the work entirely when
// an incremental update touched no file that belongs to one.
func (e *Engine) detectCommunities(store graphstore.Store, nodes []graphstore.GraphNode, edges []graphstore.GraphEdge, incremental bool, changed []string) (int, error) {
	if incremental && !crg.CommunitiesAffected(nodes, changed) {
		return 0, nil
	}
	return detectAllCommunities(store, nodes, edges)
}

// detectAllCommunities runs full community detection and persists it.
func detectAllCommunities(store graphstore.Store, nodes []graphstore.GraphNode, edges []graphstore.GraphEdge) (int, error) {
	communities, members := crg.DetectCommunities(nodes, edges, crg.DefaultCommunityMinSize)
	return store.ReplaceCommunities(communities, members)
}

// refreshSummaries recomputes the three pre-computed summary tables. Nodes
// are re-read first because community detection reassigns nodes.community_id
// and the summaries are grouped by it.
func (e *Engine) refreshSummaries(store graphstore.Store, edges []graphstore.GraphEdge) error {
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return err
	}
	flows, err := store.ReadFlows()
	if err != nil {
		return err
	}
	communities, err := store.ReadCommunities()
	if err != nil {
		return err
	}
	summaries := crg.ComputeSummaries(nodes, edges, flows, communities, e.now().UTC().Format(sqliteNowLayout))
	if _, err := store.ReplaceCommunitySummaries(summaries.CommunitySummaries); err != nil {
		return err
	}
	if _, err := store.ReplaceFlowSnapshots(summaries.FlowSnapshots); err != nil {
		return err
	}
	_, err = store.ReplaceRiskIndex(summaries.RiskIndex)
	return err
}

// secondsSince renders a stage duration the way upstream does: seconds,
// rounded to six places, never negative.
func secondsSince(started time.Time) float64 {
	return math.Max(0, math.Round(time.Since(started).Seconds()*1e6)/1e6)
}

// ── Status (§11.1 row 3) ─────────────────────────────────────────────────────

// Status reports the persisted graph in upstream's `status --json` shape,
// plus the readiness overlay `da kg code-status` prints.
//
// Like the bridge's Status it never returns an error for an absent or
// unreadable graph: the condition is surfaced through State/Message so
// code-status stays a diagnostic rather than a failure.
func (e *Engine) Status() (*graphstore.CRGStatus, error) {
	status := &graphstore.CRGStatus{State: graphstore.CRGReadinessUnbuilt, Languages: []string{}}
	store, err := e.readStore()
	if err != nil {
		status.State = graphstore.CRGReadinessError
		status.Message = err.Error()
		return status, nil
	}
	if store == nil {
		status.Message = "code graph database missing"
		applyEngineVCSStatus(status, e.root, func(string) string { return "" })
		return status, nil
	}
	stats, err := store.GetStats()
	if err != nil {
		status.State = graphstore.CRGReadinessError
		status.Message = err.Error()
		return status, nil
	}
	applyStats(status, stats)
	applyEngineVCSStatus(status, e.root, func(key string) string {
		value, err := store.GetMetadata(key)
		if err != nil {
			return ""
		}
		return value
	})
	return status, nil
}

// applyStats projects store statistics onto the status shape and derives the
// readiness state.
func applyStats(status *graphstore.CRGStatus, stats graphstore.GraphStats) {
	status.Nodes = stats.TotalNodes
	status.Edges = stats.TotalEdges
	status.Files = stats.FilesCount
	status.Languages = append([]string{}, stats.Languages...)
	sort.Strings(status.Languages)
	if updated := strings.TrimSpace(stats.LastUpdated); updated != "" {
		status.LastUpdated = new(updated)
	}
	if status.Nodes > 0 && status.Files > 0 && status.LastUpdated != nil {
		status.State = graphstore.CRGReadinessReady
		status.Ready = true
		return
	}
	status.State = graphstore.CRGReadinessUnbuilt
	status.Message = "code graph has not been built yet"
}

// applyEngineVCSStatus fills the VCS half of `status --json`. It is the same
// rule the bridge applies, reached through the shared probes so the two
// backends cannot report a repository differently.
func applyEngineVCSStatus(status *graphstore.CRGStatus, root string, metadata func(key string) string) {
	status.VCS = graphstore.DetectVCS(root)
	status.BuiltOnBranch = optionalString(metadata(metaGitBranch))
	status.BuiltAtCommit = optionalString(metadata(metaGitHeadSHA))
	status.SVNBranch = optionalString(metadata(metaSVNBranch))
	status.SVNRevision = optionalString(metadata(metaSVNRevision))
	if status.VCS == graphstore.VCSGit {
		branch, sha := graphstore.GitBranchInfo(root)
		status.CurrentBranch, status.CurrentSHA = new(branch), new(sha)
	}
}

// optionalString maps "" onto a JSON null, matching how upstream reports a
// metadata key that was never written.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// ── Metadata ─────────────────────────────────────────────────────────────────

// stampBuild writes the metadata keys upstream records after a successful
// ingestion, so `status` and the next automatic base resolution agree with
// the graph that was just written.
func (e *Engine) stampBuild(store graphstore.Store, buildType string) error {
	writes := [][2]string{
		{metaSchemaVersion, strconv.Itoa(graphstore.SchemaVersion)},
		{metaLastUpdated, e.stamp()},
		{metaLastBuildType, buildType},
	}
	switch graphstore.DetectVCS(e.root) {
	case graphstore.VCSGit:
		branch, sha := graphstore.GitBranchInfo(e.root)
		writes = appendIfSet(writes, metaGitBranch, branch)
		writes = appendIfSet(writes, metaGitHeadSHA, sha)
	case graphstore.VCSSVN:
		branch, revision := graphstore.SVNInfo(e.root)
		writes = appendIfSet(writes, metaSVNBranch, branch)
		writes = appendIfSet(writes, metaSVNRevision, revision)
	}
	for _, kv := range writes {
		if err := store.SetMetadata(kv[0], kv[1]); err != nil {
			return err
		}
	}
	return store.Commit()
}

// appendIfSet records a metadata write only for a value the probe produced;
// upstream never overwrites a stored branch or sha with an empty string.
func appendIfSet(writes [][2]string, key, value string) [][2]string {
	if value == "" {
		return writes
	}
	return append(writes, [2]string{key, value})
}

// stamp renders "now" the way every upstream metadata timestamp is written.
func (e *Engine) stamp() string { return e.now().Format(upstreamTimeLayout) }

// ── Path and filesystem helpers ──────────────────────────────────────────────

// rooted returns the engine an operation must run against, honouring an
// explicit RepoRoot option.
//
// Upstream's _resolve_repo_root puts an explicit repo_root above every other
// source, and the graph it builds lives under THAT root. Re-pointing this
// engine's root would not be enough: Open derives the database path from the
// root, so the other repository's sources would be written into THIS
// repository's database. A different root therefore gets its own engine,
// closed by the returned release, and inherits this engine's seams so a
// pinned clock or stubbed diff still applies.
func (e *Engine) rooted(repoRoot string) (*Engine, func(), error) {
	if repoRoot == "" {
		return e, func() {}, nil
	}
	requested, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("codegraph: resolve repo root %q: %w", repoRoot, err)
	}
	if NormalizeFilePath(requested) == NormalizeFilePath(e.absRoot()) {
		return e, func() {}, nil
	}
	scoped := Open(repoRoot)
	scoped.changedFiles = e.changedFiles
	scoped.now = e.now
	return scoped, func() { _ = scoped.Close() }, nil
}

// absPath maps a repo-relative path onto the absolute, forward-slash
// spelling the graph keys nodes and files by.
func (e *Engine) absPath(rel string) string {
	if filepath.IsAbs(rel) {
		return NormalizeFilePath(filepath.Clean(rel))
	}
	return NormalizeFilePath(filepath.Join(e.absRoot(), rel))
}

// absRoot is the repository root in the spelling the graph was written with.
func (e *Engine) absRoot() string {
	root, err := filepath.Abs(e.root)
	if err != nil {
		return e.root
	}
	return root
}

// relPath maps a graph path back to a repo-relative one, keeping the
// absolute spelling when the file lives outside the repository.
func (e *Engine) relPath(abs string) string {
	rel, err := filepath.Rel(NormalizeFilePath(e.absRoot()), abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return filepath.ToSlash(rel)
}

// isRegularFile reports whether path is a real file rather than a directory,
// a symlink or nothing at all.
func isRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// storedFileSet is the set of file paths the graph currently holds rows for.
func storedFileSet(store graphstore.CodeGraphReader) (map[string]bool, error) {
	files, err := store.GetAllFiles()
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(files))
	for _, path := range files {
		set[path] = true
	}
	return set, nil
}

// hasNodes reports whether the graph holds anything at all. An empty graph
// cannot be updated incrementally, so upstream escalates to a full rebuild.
func hasNodes(store graphstore.CodeGraphReader) (bool, error) {
	stats, err := store.GetStats()
	if err != nil {
		return false, err
	}
	return stats.TotalNodes > 0, nil
}

// excludeSubmodules drops files that live inside a nested git repository
// unless the caller opted into submodule traversal.
//
// Upstream gets this from `git ls-files`, which stops at a submodule
// boundary unless --recurse-submodules is passed. The kg-native scanner
// walks the tree and therefore sees submodule sources unconditionally, so
// the opt-in is applied here rather than leaving the flag inert.
func excludeSubmodules(root string, files []SourceFile, recurseSubmodules *bool) []SourceFile {
	if graphstore.RecurseSubmodules(recurseSubmodules) {
		return files
	}
	rootDir := filepath.ToSlash(filepath.Clean(root))
	nested := map[string]bool{}
	kept := files[:0]
	for _, sf := range files {
		if !insideNestedRepo(rootDir, filepath.ToSlash(filepath.Dir(sf.Path)), nested) {
			kept = append(kept, sf)
		}
	}
	return kept
}

// insideNestedRepo walks dir's ancestry up to rootDir looking for a .git
// entry, memoising each directory it decides.
func insideNestedRepo(rootDir, dir string, nested map[string]bool) bool {
	if dir == rootDir || !strings.HasPrefix(dir, rootDir) {
		return false
	}
	if answer, ok := nested[dir]; ok {
		return answer
	}
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	answer := err == nil || insideNestedRepo(rootDir, filepath.ToSlash(filepath.Dir(dir)), nested)
	nested[dir] = answer
	return answer
}
