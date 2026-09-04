package graphstore

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writeArgvRecorder writes a stand-in CRG executable that echoes the arguments
// it was called with, so the default runner's flag translation is observable
// without a real code-review-graph install.
func writeArgvRecorder(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stand-in binary is POSIX-only")
	}
	bin := filepath.Join(dir, "argv-recorder")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// fakeCRGRunner stands in for the code-review-graph subprocess. buildRoot
// seeds a graph database for the root it is handed — one symbol per tracked
// file, plus a `Button` symbol every repository defines — so the multi-root
// orchestration (build each root, merge, postprocess once) is exercised
// without the Python CRG installed.
type fakeCRGRunner struct {
	t *testing.T
	// built and postprocessed record the roots each phase was invoked for.
	built         []string
	postprocessed []string
	// buildOpts records the options each build received, so the test can
	// prove submodule builds defer their postprocess.
	buildOpts []BuildOptions
	// nodesAtPostprocess is the destination graph's node count observed when
	// postprocess ran — the evidence that postprocess sees MERGED rows.
	nodesAtPostprocess int
	// failRoot makes buildRoot fail for the root whose path contains it.
	failRoot string
	// skipSeed suppresses graph seeding for roots the test seeded itself
	// (seeding enumerates via git, which a non-repository directory cannot).
	skipSeed bool
	// emptySeedRoot writes an unusable (table-less) graph for the root whose
	// path contains it, standing in for a build that produced nothing mergeable.
	emptySeedRoot string
	// emptyGraphRoot writes a well-formed but EMPTY graph for the root whose
	// path contains it — a submodule CRG indexed and found no symbols in.
	emptyGraphRoot string
	// postprocessErr is returned by every postprocess pass when set.
	postprocessErr error
}

func (f *fakeCRGRunner) buildRoot(root string, opts BuildOptions) ([]byte, error) {
	f.built = append(f.built, root)
	f.buildOpts = append(f.buildOpts, opts)
	if f.failRoot != "" && strings.Contains(filepath.ToSlash(root), f.failRoot) {
		return []byte("parse failed"), fmt.Errorf("fake build failure")
	}
	switch {
	case f.emptySeedRoot != "" && strings.Contains(filepath.ToSlash(root), f.emptySeedRoot):
		seedGraphDB(f.t, CRGDBPath(root), nil, nil)
		dropGraphTables(f.t, CRGDBPath(root))
	case f.emptyGraphRoot != "" && strings.Contains(filepath.ToSlash(root), f.emptyGraphRoot):
		seedGraphDB(f.t, CRGDBPath(root), nil, nil)
	case !f.skipSeed:
		seedRootGraph(f.t, root)
	}
	return []byte("built " + root), nil
}

func (f *fakeCRGRunner) postprocessRoot(root string, _ PostprocessOptions) error {
	f.postprocessed = append(f.postprocessed, root)
	if f.postprocessErr != nil {
		return f.postprocessErr
	}
	f.nodesAtPostprocess = countRows(f.t, CRGDBPath(root), "nodes")
	return nil
}

// dropGraphTables leaves a graph database file in place with no graph tables —
// the shape a build that produced nothing usable would leave behind.
func dropGraphTables(t *testing.T, path string) {
	t.Helper()
	db := openTestDB(t, path)
	defer db.Close()
	if _, err := db.Exec(`DROP TABLE nodes; DROP TABLE edges`); err != nil {
		t.Fatalf("drop graph tables: %v", err)
	}
}

// seedRootGraph writes a graph database for root holding one node per tracked
// file plus a `Button` node and one edge into it. Every repository defining
// `Button` is the point: it is the collision that used to fabricate
// cross-repository edges once two graphs shared a database.
func seedRootGraph(t *testing.T, root string) {
	t.Helper()
	files, err := EnumerateTrackedFiles(root, false)
	if err != nil {
		t.Fatalf("enumerate %s: %v", root, err)
	}
	nodes := []graphNodeRow{{qualified: "Button", name: "Button", filePath: filepath.Join(root, "ui", "Button.tsx")}}
	for _, f := range files {
		nodes = append(nodes, graphNodeRow{
			qualified: "sym::" + f,
			name:      filepath.Base(f),
			filePath:  filepath.Join(root, filepath.FromSlash(f)),
		})
	}
	edges := []graphEdgeRow{{source: nodes[1].qualified, target: "Button", filePath: nodes[1].filePath}}
	seedGraphDB(t, CRGDBPath(root), nodes, edges)
}

// workspaceBridge returns a bridge over root wired to a fake runner.
func workspaceBridge(t *testing.T, root string) (*CRGBridge, *fakeCRGRunner) {
	t.Helper()
	runner := &fakeCRGRunner{t: t}
	return &CRGBridge{RepoRoot: root, Bin: "fake", runner: runner}, runner
}

// TestBuildReport_IndexesSubmodulesAndReportsEveryRoot is the end-to-end fix:
// a superproject build indexes the submodule too, the counts cover both roots,
// and the readiness report names each one.
func TestBuildReport_IndexesSubmodulesAndReportsEveryRoot(t *testing.T) {
	super := superprojectFixture(t)
	bridge, runner := workspaceBridge(t, super)

	report, err := bridge.BuildReport(BuildOptions{})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	if len(runner.built) != 2 {
		t.Fatalf("expected a build per root, got %v", runner.built)
	}
	if !strings.HasSuffix(filepath.ToSlash(runner.built[1]), "vendor/lib") {
		t.Errorf("second build root = %q, want the submodule", runner.built[1])
	}
	if report.Outcome != CRGReadinessReady {
		t.Fatalf("outcome = %q, summary = %q", report.Outcome, report.Summary)
	}
	// 5 superproject nodes (4 tracked entries + Button) and 4 submodule nodes.
	if report.Status.Nodes != 9 {
		t.Errorf("merged node count = %d, want 9", report.Status.Nodes)
	}
	if report.Merged == nil || report.Merged.Nodes != 4 || report.Merged.Edges != 1 {
		t.Errorf("merge stats = %+v, want 4 nodes / 1 edge", report.Merged)
	}
	if report.Workspace == nil || len(report.Workspace.Roots) != 2 {
		t.Fatalf("workspace plan = %+v", report.Workspace)
	}
	if report.Workspace.RootOnlyFiles != 4 || report.Workspace.Files() != 7 {
		t.Errorf("plan file counts = root-only %d / total %d, want 4 / 7",
			report.Workspace.RootOnlyFiles, report.Workspace.Files())
	}
	for _, want := range []string{".: 4 files", "vendor/lib: 3 files"} {
		if !strings.Contains(report.Summary, want) {
			t.Errorf("summary %q must name every root (missing %q)", report.Summary, want)
		}
	}
}

// TestBuildReport_StatusAttributesRowsPerRoot: code-status breaks the counts
// down per repository, so a 2-file graph in an 885-file workspace can never
// again look like a healthy one.
func TestBuildReport_StatusAttributesRowsPerRoot(t *testing.T) {
	super := superprojectFixture(t)
	bridge, _ := workspaceBridge(t, super)
	if _, err := bridge.BuildReport(BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	status, err := bridge.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Ready || status.State != CRGReadinessReady {
		t.Fatalf("status = %+v, want ready", status)
	}
	if len(status.Roots) != 2 {
		t.Fatalf("roots = %+v, want the superproject and the submodule", status.Roots)
	}
	root, sub := status.Roots[0], status.Roots[1]
	if root.Path != "." || root.Nodes != 5 || root.Files != 5 || !root.Indexed {
		t.Errorf("superproject root = %+v, want 5 nodes / 5 files", root)
	}
	if sub.Path != "vendor/lib" || sub.Nodes != 4 || sub.Files != 4 || !sub.Indexed {
		t.Errorf("submodule root = %+v, want 4 nodes / 4 files", sub)
	}
}

// TestBuildReport_PostprocessRunsOverMergedRows: postprocess is what rebuilds
// the FTS index, flows, and communities. Running it before the merge (or per
// submodule only) is what left a merged graph with populated tables and an
// empty search index, so it must run once, at the end, over every row.
func TestBuildReport_PostprocessRunsOverMergedRows(t *testing.T) {
	super := superprojectFixture(t)
	bridge, runner := workspaceBridge(t, super)

	if _, err := bridge.BuildReport(BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	if len(runner.postprocessed) != 1 || runner.postprocessed[0] != super {
		t.Fatalf("postprocess = %v, want exactly one pass over the superproject", runner.postprocessed)
	}
	if runner.nodesAtPostprocess != 9 {
		t.Errorf("postprocess saw %d nodes, want all 9 (it must run AFTER the merge)", runner.nodesAtPostprocess)
	}
	for i, opts := range runner.buildOpts {
		if !opts.SkipPostprocess {
			t.Errorf("per-root build %d did not defer its postprocess: %+v", i, opts)
		}
	}
}

// TestBuildReport_NoCrossRepoEdges: both repositories define `Button`, and the
// merged graph keeps them apart — the submodule's edge resolves inside the
// submodule, not into the superproject.
func TestBuildReport_NoCrossRepoEdges(t *testing.T) {
	super := superprojectFixture(t)
	bridge, _ := workspaceBridge(t, super)
	if _, err := bridge.BuildReport(BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	db := openTestDB(t, CRGDBPath(super))
	defer db.Close()
	rows, err := db.Query(`SELECT source_qualified, target_qualified FROM edges ORDER BY source_qualified`)
	if err != nil {
		t.Fatalf("query edges: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var source, target string
		if err := rows.Scan(&source, &target); err != nil {
			t.Fatal(err)
		}
		seen++
		if strings.HasPrefix(source, "vendor/lib"+scopeSeparator) != strings.HasPrefix(target, "vendor/lib"+scopeSeparator) {
			t.Errorf("cross-repo edge %s -> %s", source, target)
		}
	}
	if seen != 2 {
		t.Errorf("expected one edge per repository, got %d", seen)
	}
	var buttons int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE name = 'Button'`).Scan(&buttons); err != nil {
		t.Fatal(err)
	}
	if buttons != 2 {
		t.Errorf("both repositories' Button symbols must survive the merge, got %d", buttons)
	}
}

// TestBuildReport_OptOutReportsSkippedSubmodule: --no-recurse-submodules is a
// visible exclusion, and the resulting graph is reported as incomplete rather
// than READY.
func TestBuildReport_OptOutReportsSkippedSubmodule(t *testing.T) {
	super := superprojectFixture(t)
	bridge, runner := workspaceBridge(t, super)

	report, err := bridge.BuildReport(BuildOptions{NoRecurseSubmodules: true})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	if len(runner.built) != 1 {
		t.Fatalf("opt-out must build the superproject only, got %v", runner.built)
	}
	if report.Merged != nil {
		t.Errorf("nothing should have been merged: %+v", report.Merged)
	}
	// An explicit exclusion is a declared scope, not a missing root, so the
	// graph stays ready — but the exclusion itself is never hidden.
	if report.Outcome != CRGReadinessReady || !report.Status.Ready {
		t.Errorf("outcome = %q ready = %v, want a ready graph", report.Outcome, report.Status.Ready)
	}
	if !strings.Contains(report.Summary, "vendor/lib: SKIPPED ("+SkipReasonExcluded+")") {
		t.Errorf("summary must name the excluded submodule and why: %q", report.Summary)
	}
	excluded := report.Status.Roots[1]
	if excluded.Path != "vendor/lib" || excluded.Indexed || !strings.Contains(excluded.Note, "excluded") {
		t.Errorf("status root = %+v, want vendor/lib flagged as excluded", excluded)
	}
}

// TestBuildReport_OptOutSurvivesLaterStatusReads: the exclusion is recorded
// with the build, so a later `code-status` — which has no way to know what the
// build was asked to do — still reports the graph as ready rather than
// nagging about a root the operator deliberately left out.
func TestBuildReport_OptOutSurvivesLaterStatusReads(t *testing.T) {
	super := superprojectFixture(t)
	bridge, _ := workspaceBridge(t, super)
	if _, err := bridge.BuildReport(BuildOptions{NoRecurseSubmodules: true}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	status, err := (&CRGBridge{RepoRoot: super}).Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Ready || status.State != CRGReadinessReady {
		t.Errorf("status = %+v, want ready for a declared-scope graph", status)
	}
	if !strings.Contains(status.Roots[1].Note, "excluded") {
		t.Errorf("root note = %q, want it to state the exclusion", status.Roots[1].Note)
	}
}

// TestBuildReport_EmptySubmoduleIsCoveredNotMissing: a submodule the build DID
// index but found no symbols in (docs-only, or a language CRG cannot parse) is
// a complete answer. Treating it as missing would leave such a workspace
// permanently un-ready with no action that could fix it.
func TestBuildReport_EmptySubmoduleIsCoveredNotMissing(t *testing.T) {
	super := superprojectFixture(t)
	bridge, runner := workspaceBridge(t, super)
	runner.emptyGraphRoot = "vendor/lib"

	report, err := bridge.BuildReport(BuildOptions{})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if report.Outcome != CRGReadinessReady || !report.Status.Ready {
		t.Errorf("outcome = %q ready = %v, want ready", report.Outcome, report.Status.Ready)
	}
	root := report.Status.Roots[1]
	if !root.Indexed || root.Nodes != 0 || !strings.Contains(root.Note, "no symbols") {
		t.Errorf("root = %+v, want it reported as indexed-but-empty", root)
	}
}

// TestBuildReport_UninitializedSubmoduleIsNamed: an uninitialized submodule
// cannot be indexed, and the build says exactly that instead of quietly
// producing a partial graph.
func TestBuildReport_UninitializedSubmoduleIsNamed(t *testing.T) {
	super := superprojectFixture(t)
	base := t.TempDir()
	clone := filepath.Join(base, "clone")
	git(t, base, "clone", "--quiet", filepath.ToSlash(super), clone)
	bridge, runner := workspaceBridge(t, clone)

	report, err := bridge.BuildReport(BuildOptions{})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	if len(runner.built) != 1 {
		t.Fatalf("an uninitialized submodule cannot be built: %v", runner.built)
	}
	if report.Outcome != CRGReadinessIncomplete || report.Status.Ready {
		t.Errorf("outcome = %q ready = %v, want an incomplete, not-ready graph", report.Outcome, report.Status.Ready)
	}
	if !strings.Contains(report.Summary, SkipReasonUninitialized) {
		t.Errorf("summary must state the submodule is uninitialized: %q", report.Summary)
	}
	unindexed := report.Status.Roots[1]
	if unindexed.Indexed || unindexed.Note != SkipReasonUninitialized {
		t.Errorf("status root = %+v, want it flagged uninitialized", unindexed)
	}
}

// TestBuildReport_SubmoduleBuildFailureIsAttributed: a failed submodule build
// fails the whole build, naming the root that failed.
func TestBuildReport_SubmoduleBuildFailureIsAttributed(t *testing.T) {
	super := superprojectFixture(t)
	bridge, runner := workspaceBridge(t, super)
	runner.failRoot = "vendor/lib"

	_, err := bridge.BuildReport(BuildOptions{})
	if err == nil {
		t.Fatal("expected the submodule build failure to propagate")
	}
	if !strings.Contains(err.Error(), "vendor/lib") {
		t.Errorf("error must name the failing root: %v", err)
	}
}

// TestBuildReport_UnmergeableSubmoduleGraphFails: a submodule build that
// produced nothing mergeable fails the build, naming the submodule, rather
// than quietly leaving its code out of the graph.
func TestBuildReport_UnmergeableSubmoduleGraphFails(t *testing.T) {
	super := superprojectFixture(t)
	bridge, runner := workspaceBridge(t, super)
	runner.emptySeedRoot = "vendor/lib"

	_, err := bridge.BuildReport(BuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "merge submodule vendor/lib") {
		t.Fatalf("expected a merge failure naming the submodule, got %v", err)
	}
}

// TestBuildReport_PostprocessFailureFails: derived state that could not be
// rebuilt is a failed build — a merged graph with a stale FTS index is the
// trap this ordering exists to close.
func TestBuildReport_PostprocessFailureFails(t *testing.T) {
	super := superprojectFixture(t)
	bridge, runner := workspaceBridge(t, super)
	runner.postprocessErr = fmt.Errorf("fake postprocess failure")

	_, err := bridge.BuildReport(BuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "postprocess merged graph") {
		t.Fatalf("expected a postprocess failure, got %v", err)
	}
}

// TestBuildReport_SkipPostprocessStillMerges: the explicit opt-out skips the
// derived-state rebuild but still folds the submodule rows in, so the caller
// gets what they asked for and nothing silently different.
func TestBuildReport_SkipPostprocessStillMerges(t *testing.T) {
	super := superprojectFixture(t)
	bridge, runner := workspaceBridge(t, super)

	report, err := bridge.BuildReport(BuildOptions{SkipPostprocess: true})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if len(runner.postprocessed) != 0 {
		t.Errorf("postprocess must be skipped when asked, ran for %v", runner.postprocessed)
	}
	if report.Merged == nil || report.Merged.Nodes != 4 {
		t.Errorf("submodule rows must still be merged: %+v", report.Merged)
	}
}

// TestBuildReport_SkipFlowsPropagates: the flags reach every root's build.
func TestBuildReport_SkipFlowsPropagates(t *testing.T) {
	super := superprojectFixture(t)
	bridge, runner := workspaceBridge(t, super)

	if _, err := bridge.BuildReport(BuildOptions{SkipFlows: true}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	for i, opts := range runner.buildOpts {
		if !opts.SkipFlows {
			t.Errorf("build %d lost SkipFlows: %+v", i, opts)
		}
	}
}

// TestBuildReport_NonRepoFallsBackToSingleRoot: outside a git repository there
// is nothing to enumerate, and the build behaves exactly as it did before.
func TestBuildReport_NonRepoFallsBackToSingleRoot(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCRGRunner{t: t}
	bridge := &CRGBridge{RepoRoot: dir, Bin: "fake", runner: runner}
	// Seed directly: the fake's own seeding needs git enumeration.
	seedGraphDB(t, CRGDBPath(dir), []graphNodeRow{{qualified: "main", name: "main", filePath: "/x/main.go"}}, nil)
	runner.skipSeed = true

	report, err := bridge.BuildReport(BuildOptions{})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if len(runner.built) != 1 || report.Workspace != nil || report.Merged != nil {
		t.Fatalf("expected the legacy single-root path: built=%v workspace=%+v", runner.built, report.Workspace)
	}
	if runner.buildOpts[0].SkipPostprocess {
		t.Error("a single-root build must not have its postprocess deferred")
	}
}

// TestApplyWorkspaceReadiness_PlainRepoIsUntouched: a repository with no
// submodules keeps the pre-existing single-root status shape.
func TestApplyWorkspaceReadiness_PlainRepoIsUntouched(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "plain"), map[string]string{"a.go": "package a\n"})
	seedGraphDB(t, CRGDBPath(repo), []graphNodeRow{{qualified: "a", name: "a", filePath: "/x/a.go"}}, nil)

	status, err := (&CRGBridge{RepoRoot: repo}).Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Ready || len(status.Roots) != 0 {
		t.Errorf("plain repo status = %+v, want ready with no per-root breakdown", status)
	}
}

// TestApplyWorkspaceReadiness_CountErrorLeavesStatusAlone: if the row
// attribution query cannot run, readiness is left as the single-repo path
// computed it rather than being downgraded on a technicality.
func TestApplyWorkspaceReadiness_CountErrorLeavesStatusAlone(t *testing.T) {
	super := superprojectFixture(t)
	db := openTestDB(t, filepath.Join(t.TempDir(), "empty.db"))
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE placeholder (id INTEGER)`); err != nil {
		t.Fatal(err)
	}

	status := &CRGStatus{Ready: true, State: CRGReadinessReady}
	(&CRGBridge{RepoRoot: super}).applyWorkspaceReadiness(status, db)
	if !status.Ready || status.Roots != nil {
		t.Errorf("status = %+v, want it left untouched when attribution fails", status)
	}
}

// TestCountRootRows_MatchesScopeAndPathPrefixes: rows are attributed to a
// submodule by the merge's scope prefix or by file path (absolute or
// superproject-relative, either separator style), so an externally aggregated
// graph is read correctly too.
func TestCountRootRows_MatchesScopeAndPathPrefixes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	subAbs := filepath.Join(dir, "vendor", "lib")
	seedGraphDB(t, dbPath, []graphNodeRow{
		{qualified: "vendor/lib::Scoped", name: "Scoped", filePath: "/elsewhere/a.go"},
		{qualified: "ByAbsPath", name: "ByAbsPath", filePath: filepath.Join(subAbs, "b.go")},
		{qualified: "ByAbsSlashPath", name: "ByAbsSlashPath", filePath: filepath.ToSlash(subAbs) + "/d.go"},
		{qualified: "ByRelPath", name: "ByRelPath", filePath: filepath.Join("vendor", "lib", "c.go")},
		{qualified: "BySlashRelPath", name: "BySlashRelPath", filePath: "vendor/lib/e.go"},
		{qualified: "Unrelated", name: "Unrelated", filePath: "/repo/main.go"},
	}, nil)
	db := openTestDB(t, dbPath)
	defer db.Close()

	counts, err := countRootRows(db, dir, []Submodule{{Path: "vendor/lib", Initialized: true}})
	if err != nil {
		t.Fatalf("countRootRows: %v", err)
	}
	if got := counts["vendor/lib"]; got.Nodes != 5 || got.Files != 5 {
		t.Errorf("submodule count = %+v, want 5 nodes / 5 files", got)
	}
	if got := counts[superprojectRoot]; got.Nodes != 1 {
		t.Errorf("superproject count = %+v, want the 1 unrelated row", got)
	}
}

// TestCountRootRows_NestedRootsAreExclusive: a row inside a nested submodule
// belongs to the nested root only. Counting each root independently would
// count it twice and could drive the superproject's share negative.
func TestCountRootRows_NestedRootsAreExclusive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	outer := filepath.Join(dir, "vendor", "mid")
	nested := filepath.Join(outer, "deps", "leaf")
	seedGraphDB(t, dbPath, []graphNodeRow{
		{qualified: "Root", name: "Root", filePath: filepath.Join(dir, "main.go")},
		{qualified: "Outer", name: "Outer", filePath: filepath.Join(outer, "mid.go")},
		{qualified: "Leaf1", name: "Leaf1", filePath: filepath.Join(nested, "a.go")},
		{qualified: "Leaf2", name: "Leaf2", filePath: filepath.Join(nested, "b.go")},
	}, nil)
	db := openTestDB(t, dbPath)
	defer db.Close()

	counts, err := countRootRows(db, dir, []Submodule{
		{Path: "vendor/mid", Initialized: true},
		{Path: "vendor/mid/deps/leaf", Initialized: true},
	})
	if err != nil {
		t.Fatalf("countRootRows: %v", err)
	}
	want := map[string]rootCount{
		superprojectRoot:       {Nodes: 1, Files: 1},
		"vendor/mid":           {Nodes: 1, Files: 1},
		"vendor/mid/deps/leaf": {Nodes: 2, Files: 2},
	}
	for root, expected := range want {
		if counts[root] != expected {
			t.Errorf("%s = %+v, want %+v", root, counts[root], expected)
		}
	}
}

// TestCountRootRows_FallsBackToPathsWithoutQualifiedName: a graph whose nodes
// table has no qualified_name column is still attributed by file path, so the
// readiness check degrades instead of disappearing.
func TestCountRootRows_FallsBackToPathsWithoutQualifiedName(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	subAbs := filepath.Join(dir, "vendor", "lib")
	db := openTestDB(t, dbPath)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE nodes (file_path TEXT, language TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(subAbs, "b.go"), "/repo/main.go"} {
		if _, err := db.Exec(`INSERT INTO nodes (file_path, language) VALUES (?, 'go')`, p); err != nil {
			t.Fatal(err)
		}
	}

	counts, err := countRootRows(db, dir, []Submodule{{Path: "vendor/lib", Initialized: true}})
	if err != nil {
		t.Fatalf("countRootRows: %v", err)
	}
	if got := counts["vendor/lib"]; got.Nodes != 1 || got.Files != 1 {
		t.Errorf("submodule count = %+v, want 1 node / 1 file", got)
	}
}

// TestStatus_UnindexedSubmoduleIsNamed is the defect in miniature: a graph
// built over the superproject alone, in a checkout whose submodule is right
// there and initialized. Status must call it out instead of reporting READY.
func TestStatus_UnindexedSubmoduleIsNamed(t *testing.T) {
	super := superprojectFixture(t)
	seedGraphDB(t, CRGDBPath(super), []graphNodeRow{
		{qualified: "main", name: "main", filePath: filepath.Join(super, "main.go")},
	}, nil)

	status, err := (&CRGBridge{RepoRoot: super}).Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Ready || status.State != CRGReadinessIncomplete {
		t.Fatalf("status = %+v, want an incomplete, not-ready graph", status)
	}
	missing := status.Roots[1]
	if missing.Indexed || missing.Note != "present in the checkout but absent from the graph" {
		t.Errorf("root = %+v, want it reported as present-but-unindexed", missing)
	}
	if !strings.Contains(status.Message, "vendor/lib") {
		t.Errorf("message = %q, want it to name the missing root", status.Message)
	}
}

// TestCountRootRows_NoNodesTable surfaces a genuinely unreadable graph as an
// error, which leaves readiness untouched rather than falsely downgraded.
func TestCountRootRows_NoNodesTable(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "graph.db"))
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE placeholder (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := countRootRows(db, "/x", []Submodule{{Path: "vendor/lib"}}); err == nil {
		t.Fatal("expected an error when the nodes table is missing")
	}
}

// TestPostprocessRoot_UsesTargetRoot pins that the default runner postprocesses
// the root it is handed (a nonexistent binary makes the attempt observable
// without a real CRG install).
func TestPostprocessRoot_UsesTargetRoot(t *testing.T) {
	bridge := &CRGBridge{RepoRoot: t.TempDir(), Bin: filepath.Join(t.TempDir(), "missing-crg")}
	if err := bridge.postprocessRoot(bridge.RepoRoot, PostprocessOptions{}); err == nil {
		t.Fatal("expected the missing CRG binary to surface an error")
	}
	dir := t.TempDir()
	ok := &CRGBridge{RepoRoot: dir, Bin: writeArgvRecorder(t, dir)}
	if err := ok.postprocessRoot(dir, PostprocessOptions{NoFTS: true}); err != nil {
		t.Errorf("postprocessRoot against a working binary: %v", err)
	}
}

// TestPostprocessArgs pins the flag translation shared by the standalone
// postprocess command and the build's own postprocess pass.
func TestPostprocessArgs(t *testing.T) {
	bare := postprocessArgs("/repo", PostprocessOptions{})
	if strings.Join(bare, " ") != "postprocess "+crgFlagRepo+" /repo" {
		t.Errorf("bare args = %v", bare)
	}
	full := postprocessArgs("/repo", PostprocessOptions{NoFlows: true, NoCommunities: true, NoFTS: true})
	for _, want := range []string{"--no-flows", "--no-communities", "--no-fts"} {
		if !strings.Contains(strings.Join(full, " "), want) {
			t.Errorf("args %v missing %q", full, want)
		}
	}
}

// TestBuildRoot_ArgsCarryFlags pins the flag translation for the default
// runner by capturing what a fake binary receives.
func TestBuildRoot_ArgsCarryFlags(t *testing.T) {
	dir := t.TempDir()
	bin := writeArgvRecorder(t, dir)
	bridge := &CRGBridge{RepoRoot: dir, Bin: bin}

	out, err := bridge.buildRoot(dir, BuildOptions{SkipFlows: true, SkipPostprocess: true})
	if err != nil {
		t.Fatalf("buildRoot: %v", err)
	}
	got := string(out)
	for _, want := range []string{"build", crgFlagRepo, dir, "--skip-flows", "--skip-postprocess"} {
		if !strings.Contains(got, want) {
			t.Errorf("build args %q missing %q", got, want)
		}
	}
}
