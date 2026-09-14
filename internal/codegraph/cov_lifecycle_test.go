package codegraph

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// This suite drives the lifecycle's incremental-update and post-build
// machinery — the arms lifecycle_test.go's replay of the release fixtures
// never reaches, because the fixture repository never loses a file, never has
// a dependent to expand and never has a store that fails.
//
// Two things are asserted here that nothing else can assert:
//
//   - the DEPENDENT CONE. An incremental update re-parses the diff AND the
//     files that depend on it, bounded by hops and a file cap. Getting that
//     wrong produces a graph that is quietly wrong rather than one that fails.
//   - WHICH STAGE failed. One update reads the stored file list three times
//     and one build commits three times, so "the update returned an error" is
//     not a useful oracle; covLifeStore names the call, so a failure injected
//     into the stale purge cannot be satisfied by the root check breaking.

// ── Staged failure injection ─────────────────────────────────────────────────

// covLifeStore narrows fakeStore's one-error-per-method injection down to ONE
// call of a repeated store seam.
//
// The lifecycle reads the stored file list three times inside a single
// incremental update (the root check, the stale purge, then the re-ingest's
// stored set), re-reads all nodes twice inside a single post-process (the
// shared graph read, then the summary refresh), commits three times inside a
// single build and writes several distinct metadata keys. fakeStore's flat
// error fields cannot tell those apart, so a test written with them would
// pass while the wrong stage was the one that broke.
type covLifeStore struct {
	*fakeStore

	calls  map[string]int
	failOn map[string]int

	// The derived-view writers fakeStore folds into its single writeErr.
	// Splitting them apart is what lets a test fail exactly one of the
	// five writes a post-process performs.
	flowWriteErr      error
	communityWriteErr error
	summaryWriteErr   error
	snapshotWriteErr  error
	signatureWriteErr error
}

// Seam names. A keyed seam (a path, a metadata key) is named through
// covLifeSeam so one file's reads can fail while another's succeed.
const (
	covLifeSeamFiles    = "GetAllFiles"
	covLifeSeamNodes    = "GetNodesByFile"
	covLifeSeamEdges    = "GetEdgesByTarget"
	covLifeSeamRemove   = "RemoveFileData"
	covLifeSeamMetadata = "SetMetadata"
	covLifeSeamAllNodes = "ReadAllNodes"
	covLifeSeamFlows    = "ReadFlows"
	covLifeSeamCommit   = "Commit"
)

// covLifeSeam names one seam applied to one argument.
func covLifeSeam(seam, arg string) string { return seam + ":" + arg }

// fails counts a call of seam and reports whether this is the call the test
// marked. An unnamed seam never fails: failOn reads 0 and call indices start
// at 1.
func (s *covLifeStore) fails(seam string) bool {
	s.calls[seam]++
	return s.failOn[seam] == s.calls[seam]
}

func (s *covLifeStore) GetAllFiles() ([]string, error) {
	if s.fails(covLifeSeamFiles) {
		return nil, errFake
	}
	return s.fakeStore.GetAllFiles()
}

func (s *covLifeStore) GetNodesByFile(path string) ([]graphstore.GraphNode, error) {
	if s.fails(covLifeSeam(covLifeSeamNodes, path)) {
		return nil, errFake
	}
	return s.fakeStore.GetNodesByFile(path)
}

func (s *covLifeStore) GetEdgesByTarget(qualified string) ([]graphstore.GraphEdge, error) {
	if s.fails(covLifeSeam(covLifeSeamEdges, qualified)) {
		return nil, errFake
	}
	return s.fakeStore.GetEdgesByTarget(qualified)
}

func (s *covLifeStore) RemoveFileData(path string) error {
	if s.fails(covLifeSeam(covLifeSeamRemove, path)) {
		return errFake
	}
	return s.fakeStore.RemoveFileData(path)
}

func (s *covLifeStore) SetMetadata(key, value string) error {
	if s.fails(covLifeSeam(covLifeSeamMetadata, key)) {
		return errFake
	}
	return s.fakeStore.SetMetadata(key, value)
}

func (s *covLifeStore) ReadAllNodes() ([]graphstore.GraphNode, error) {
	if s.fails(covLifeSeamAllNodes) {
		return nil, errFake
	}
	return s.fakeStore.ReadAllNodes()
}

func (s *covLifeStore) ReadFlows() ([]graphstore.FlowRow, error) {
	if s.fails(covLifeSeamFlows) {
		return nil, errFake
	}
	return s.fakeStore.ReadFlows()
}

func (s *covLifeStore) Commit() error {
	if s.fails(covLifeSeamCommit) {
		return errFake
	}
	return s.fakeStore.Commit()
}

func (s *covLifeStore) SetNodeSignature(id int64, signature string) error {
	if s.signatureWriteErr != nil {
		return s.signatureWriteErr
	}
	return s.fakeStore.SetNodeSignature(id, signature)
}

func (s *covLifeStore) ReplaceFlows(flows []graphstore.FlowRow, paths [][]int64) (int, error) {
	if s.flowWriteErr != nil {
		return 0, s.flowWriteErr
	}
	return s.fakeStore.ReplaceFlows(flows, paths)
}

func (s *covLifeStore) ReplaceCommunities(rows []graphstore.CommunityRow, members [][]string) (int, error) {
	if s.communityWriteErr != nil {
		return 0, s.communityWriteErr
	}
	return s.fakeStore.ReplaceCommunities(rows, members)
}

func (s *covLifeStore) ReplaceCommunitySummaries(rows []graphstore.CommunitySummaryRow) (int, error) {
	if s.summaryWriteErr != nil {
		return 0, s.summaryWriteErr
	}
	return s.fakeStore.ReplaceCommunitySummaries(rows)
}

func (s *covLifeStore) ReplaceFlowSnapshots(rows []graphstore.FlowSnapshotRow) (int, error) {
	if s.snapshotWriteErr != nil {
		return 0, s.snapshotWriteErr
	}
	return s.fakeStore.ReplaceFlowSnapshots(rows)
}

// ── Scenario ────────────────────────────────────────────────────────────────

// covLifeGraph is one lifecycle scenario: a fixture repository, the fake graph
// it was "built" into, and the engine that joins them.
type covLifeGraph struct {
	root   string
	engine *Engine
	store  *covLifeStore
}

// covLifeIncremental prepares an engine that stays on the incremental path: a
// populated graph that already holds the fixture's own lib file — so the
// foreign-root check passes and the stale purge keeps it — and a diff naming
// that one file.
func covLifeIncremental(t *testing.T) *covLifeGraph {
	t.Helper()
	root := writeFixture(t)
	store := &covLifeStore{
		fakeStore: &fakeStore{
			stats: graphstore.GraphStats{TotalNodes: 1, FilesCount: 1},
			files: []string{fixtureFile(root, "lib/lib.go")},
		},
		calls:  map[string]int{},
		failOn: map[string]int{},
	}
	e := engineWithStore(t, root, store)
	t.Cleanup(func() { _ = e.Close() })
	return &covLifeGraph{root: root, engine: e, store: store}
}

// fail marks the nth call of seam as the one that fails.
func (g *covLifeGraph) fail(seam string, nth int) {
	g.store.failOn[seam] = nth
}

// diff replaces the repo-relative paths the engine's change discovery reports.
func (g *covLifeGraph) diff(rels ...string) {
	g.engine.changedFiles = func(string, string) ([]string, error) { return rels, nil }
}

// file is the graph identity of one fixture path.
func (g *covLifeGraph) file(rel string) string { return fixtureFile(g.root, rel) }

func (g *covLifeGraph) build() (*graphstore.CRGOperationReport, error) {
	return g.engine.BuildReport(graphstore.BuildOptions{})
}

func (g *covLifeGraph) update() (*graphstore.CRGOperationReport, error) {
	return g.engine.UpdateReport(graphstore.UpdateOptions{})
}

func (g *covLifeGraph) postprocess() (*graphstore.CRGOperationReport, error) {
	return g.engine.PostprocessReport(graphstore.PostprocessOptions{})
}

// covLifeDeclareSymbol makes the graph hold one symbol for the fixture's lib
// file. That is what gives the dependent expansion a qualified name to expand
// from, and the re-ingest's hash check a row to compare against.
func covLifeDeclareSymbol(g *covLifeGraph, hash string) string {
	lib := g.file("lib/lib.go")
	g.store.fakeStore.nodes = map[string][]graphstore.GraphNode{
		lib: {{
			ID:            1,
			Kind:          kindFunction,
			Name:          "Greet",
			QualifiedName: lib + "::Greet",
			FilePath:      lib,
			FileHash:      hash,
		}},
	}
	return lib + "::Greet"
}

// covLifeWantInjected asserts a lifecycle call failed with the injected
// sentinel rather than succeeding or failing for some other reason.
func covLifeWantInjected(t *testing.T, stage string, err error) {
	t.Helper()
	if !errors.Is(err, errFake) {
		t.Fatalf("%s: err = %v, want the injected failure", stage, err)
	}
}

// ── Dependent expansion ─────────────────────────────────────────────────────

// TestCovLifeDependentHopsReadsTheEnvironmentOverride pins the bound upstream
// exposes as CRG_DEPENDENT_HOPS. A value that is not a positive integer must
// leave the default in place rather than collapse the expansion to zero hops,
// which would silently stop re-parsing dependents at all.
func TestCovLifeDependentHopsReadsTheEnvironmentOverride(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{raw: "", want: defaultDependentHops},
		{raw: "1", want: 1},
		{raw: "5", want: 5},
		{raw: "0", want: defaultDependentHops},
		{raw: "-3", want: defaultDependentHops},
		{raw: "two", want: defaultDependentHops},
	} {
		t.Run("CRG_DEPENDENT_HOPS="+tc.raw, func(t *testing.T) {
			t.Setenv("CRG_DEPENDENT_HOPS", tc.raw)
			if got := dependentHops(); got != tc.want {
				t.Fatalf("dependentHops() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestCovLifeUpdateReingestsDependentsOfAChangedFile pins why an incremental
// update is not merely "re-parse the diff": a file that imports the changed
// file and a file that calls a symbol it declares both hold graph rows derived
// from it, so both have to be re-ingested. The cycle back to the changed file
// on the second hop must be dropped, not re-expanded.
func TestCovLifeUpdateReingestsDependentsOfAChangedFile(t *testing.T) {
	g := covLifeIncremental(t)
	lib, app, appTest := g.file("lib/lib.go"), g.file("app/app.go"), g.file("app/app_test.go")
	greet := covLifeDeclareSymbol(g, "a-hash-that-no-longer-matches")
	g.store.fakeStore.edgesByTarget = map[string][]graphstore.GraphEdge{
		lib:   {{Kind: graphstore.EdgeKindImportsFrom, FilePath: app}},
		greet: {{Kind: graphstore.EdgeKindCalls, FilePath: appTest}},
		// The second hop leads back to the file the expansion started
		// from. Re-expanding it would not terminate on a cycle.
		app: {{Kind: graphstore.EdgeKindImportsFrom, FilePath: lib}},
	}

	report, err := g.update()
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if got := *report.DependentFiles; !slices.Equal(got, []string{"app/app.go", "app/app_test.go"}) {
		t.Errorf("dependent_files = %v, want the importer and the caller", got)
	}
	if got := *report.ChangedFiles; !slices.Equal(got, []string{"lib/lib.go"}) {
		t.Errorf("changed_files = %v, want only the diff", got)
	}
	// The changed file plus both dependents were re-parsed; nothing else.
	if got := *report.FilesUpdated; got != 3 {
		t.Errorf("files_updated = %d, want 3 (the diff plus both dependents)", got)
	}
}

// TestCovLifeFindDependentsStopsAtTheFileCap pins the bound that keeps one
// churned hub file from turning an incremental update into a whole-repository
// re-parse: the expansion stops collecting at maxDependentFiles and the result
// is truncated to it.
func TestCovLifeFindDependentsStopsAtTheFileCap(t *testing.T) {
	const seed = "/repo/hub.go"
	importers := make([]graphstore.GraphEdge, maxDependentFiles+1)
	for i := range importers {
		importers[i] = graphstore.GraphEdge{
			Kind:     graphstore.EdgeKindImportsFrom,
			FilePath: fmt.Sprintf("/repo/dep%04d.go", i),
		}
	}
	store := &fakeStore{edgesByTarget: map[string][]graphstore.GraphEdge{seed: importers}}

	deps, err := findDependents(store, seed)
	if err != nil {
		t.Fatalf("findDependents: %v", err)
	}
	if len(deps) != maxDependentFiles {
		t.Fatalf("dependents = %d, want the %d-file cap", len(deps), maxDependentFiles)
	}
	if deps[0] != "/repo/dep0000.go" {
		t.Errorf("dependents[0] = %q, want the sorted first dependent", deps[0])
	}
}

// TestCovLifeSingleHopIgnoresNonDependentEdges pins the edge-kind filter. A
// file that merely contains a reference the graph classified as something
// other than a dependency — and a file-level edge that is not an import — must
// not drag a file into the re-parse set.
func TestCovLifeSingleHopIgnoresNonDependentEdges(t *testing.T) {
	const seed = "/repo/lib.go"
	store := &fakeStore{
		edgesByTarget: map[string][]graphstore.GraphEdge{
			seed: {
				{Kind: graphstore.EdgeKindImportsFrom, FilePath: "/repo/importer.go"},
				{Kind: graphstore.EdgeKindCalls, FilePath: "/repo/noise.go"},
				// A self-edge: a file never depends on itself.
				{Kind: graphstore.EdgeKindImportsFrom, FilePath: seed},
			},
		},
	}

	deps, err := singleHopDependents(store, seed)
	if err != nil {
		t.Fatalf("singleHopDependents: %v", err)
	}
	if !slices.Equal(deps, []string{"/repo/importer.go"}) {
		t.Fatalf("dependents = %v, want only the importing file", deps)
	}
}

// ── Incremental update failures ─────────────────────────────────────────────

// TestCovLifeUpdatePropagatesStagedStoreFailures pins that every read the
// incremental path performs is checked, and names the stage. The stored file
// list alone is read three times; injecting into one of them and satisfying
// the test with another would hide a whole stage's missing error check.
func TestCovLifeUpdatePropagatesStagedStoreFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(*covLifeGraph)
	}{
		{"foreign-root check", func(g *covLifeGraph) { g.fail(covLifeSeamFiles, 1) }},
		{"stale purge", func(g *covLifeGraph) { g.fail(covLifeSeamFiles, 2) }},
		{"stored file set", func(g *covLifeGraph) { g.fail(covLifeSeamFiles, 3) }},
		{"importer lookup", func(g *covLifeGraph) {
			g.fail(covLifeSeam(covLifeSeamEdges, g.file("lib/lib.go")), 1)
		}},
		{"declared symbol lookup", func(g *covLifeGraph) {
			g.fail(covLifeSeam(covLifeSeamNodes, g.file("lib/lib.go")), 1)
		}},
		{"symbol dependent lookup", func(g *covLifeGraph) {
			g.fail(covLifeSeam(covLifeSeamEdges, covLifeDeclareSymbol(g, "stale")), 1)
		}},
		{"content hash lookup", func(g *covLifeGraph) {
			covLifeDeclareSymbol(g, "stale")
			// The dependent expansion reads the file's nodes first; the
			// hash check is the second read of the same file.
			g.fail(covLifeSeam(covLifeSeamNodes, g.file("lib/lib.go")), 2)
		}},
		{"changed file write", func(g *covLifeGraph) { g.store.fakeStore.writeErr = errFake }},
		{"stale removal", func(g *covLifeGraph) {
			g.store.fakeStore.files = append(g.store.fakeStore.files, g.file("dropped/dropped.go"))
			g.fail(covLifeSeam(covLifeSeamRemove, g.file("dropped/dropped.go")), 1)
		}},
		{"build stamp", func(g *covLifeGraph) {
			g.fail(covLifeSeam(covLifeSeamMetadata, metaLastBuildType), 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := covLifeIncremental(t)
			tc.arrange(g)
			_, err := g.update()
			covLifeWantInjected(t, tc.name, err)
		})
	}
}

// TestCovLifeUpdateFailsWhenTheStoredBaseCannotBeRead pins that the automatic
// base resolution reports a metadata read failure. Swallowing it would make
// the update diff against nothing and report a stale graph as up to date.
func TestCovLifeUpdateFailsWhenTheStoredBaseCannotBeRead(t *testing.T) {
	g := covLifeIncremental(t)
	if err := os.Mkdir(filepath.Join(g.root, ".git"), 0o755); err != nil {
		t.Fatalf("mark the fixture as git: %v", err)
	}
	if vcs := graphstore.DetectVCS(g.root); vcs != graphstore.VCSGit {
		t.Fatalf("DetectVCS = %q, want git — the base resolution would not read metadata", vcs)
	}
	g.store.fakeStore.metaErr = errFake

	_, err := g.update()
	covLifeWantInjected(t, "stored base", err)
}

// TestCovLifeUpdateDiscardsTheReport pins the report-free wrapper: it must
// carry the failure through rather than absorb it along with the report.
func TestCovLifeUpdateDiscardsTheReport(t *testing.T) {
	if err := builtEngine(t, nil).Update(graphstore.UpdateOptions{}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	g := covLifeIncremental(t)
	g.fail(covLifeSeamFiles, 1)
	covLifeWantInjected(t, "Update", g.engine.Update(graphstore.UpdateOptions{}))
}

// ── Re-ingest of vanished paths ─────────────────────────────────────────────

// TestCovLifeIngestChangedFilesPurgesVanishedPaths drives the re-ingest
// directly, because that is the only way to present it with its purge signal.
//
// The re-ingest treats "the graph holds rows for this path, the scanner did
// not return it, and it is not on disk" as a delete. incrementalUpdate's stale
// reconciliation normally gets there first, so the arm exists for exactly what
// this test supplies: a path the DIFF named — a delete, or a rename's source —
// whose rows must not outlive the file. A path that is simply not source we
// index must be left alone instead.
func TestCovLifeIngestChangedFilesPurgesVanishedPaths(t *testing.T) {
	g := covLifeIncremental(t)
	gone := g.file("gone/gone.go")
	g.store.fakeStore.files = []string{gone}

	ingest, err := g.engine.ingestChangedFiles(g.store, []string{"lib/lib.go", "gone/gone.go", "README.md"})
	if err != nil {
		t.Fatalf("ingestChangedFiles: %v", err)
	}
	if ingest.parsed != 1 {
		t.Errorf("parsed = %d, want only the file that still exists", ingest.parsed)
	}
	if ingest.purged != 1 {
		t.Errorf("purged = %d, want the vanished file's rows removed", ingest.purged)
	}
	if ingest.nodes == 0 || ingest.edges == 0 {
		t.Errorf("nodes/edges = %d/%d, want the re-parsed file's rows counted", ingest.nodes, ingest.edges)
	}
}

// TestCovLifeIngestChangedFilesReportsAFailedPurge pins that a purge which
// cannot be performed is an error. Reporting the file as removed while its
// rows survive would leave the graph claiming a file that is gone.
func TestCovLifeIngestChangedFilesReportsAFailedPurge(t *testing.T) {
	g := covLifeIncremental(t)
	gone := g.file("gone/gone.go")
	g.store.fakeStore.files = []string{gone}
	g.fail(covLifeSeam(covLifeSeamRemove, gone), 1)

	_, err := g.engine.ingestChangedFiles(g.store, []string{"gone/gone.go"})
	covLifeWantInjected(t, "purge", err)
}

// ── Real-store incremental behaviour ────────────────────────────────────────

// TestCovLifeIncrementalUpdateReconcilesEveryDiffKind drives one update over a
// REAL graph whose diff carries an addition, a modification and a deletion,
// and asserts the graph afterwards. The counters alone would not catch a
// reconciliation that reported the right numbers and wrote the wrong rows.
func TestCovLifeIncrementalUpdateReconcilesEveryDiffKind(t *testing.T) {
	root := writeFixture(t)
	e := Open(root)
	t.Cleanup(func() { _ = e.Close() })
	var changed []string
	e.changedFiles = func(string, string) ([]string, error) { return changed, nil }
	if _, err := e.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	writeExtra(t, root, "lib/extra.go", "package lib\n\n// Extra decorates a constant.\nfunc Extra() string { return decorate(\"e\") }\n")
	writeExtra(t, root, "lib/lib.go", fixtureFiles["lib/lib.go"]+"\n// Added is new in this revision.\nfunc Added() string { return \"added\" }\n")
	if err := os.Remove(filepath.Join(root, "app", "app_test.go")); err != nil {
		t.Fatalf("remove the deleted file: %v", err)
	}
	changed = []string{"lib/extra.go", "lib/lib.go", "app/app_test.go"}

	report, err := e.UpdateReport(graphstore.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.BuildType != buildTypeIncremental {
		t.Fatalf("build_type = %q, want an incremental update", report.BuildType)
	}
	if got := *report.StaleFilesRemoved; got != 1 {
		t.Errorf("stale_files_removed = %d, want the deleted file", got)
	}
	covLifeWantSymbols(t, e, fixtureFile(root, "lib/extra.go"), "Extra")
	covLifeWantSymbols(t, e, fixtureFile(root, "lib/lib.go"), "Added", "Greet")
	covLifeWantSymbols(t, e, fixtureFile(root, "app/app_test.go"))
}

// covLifeWantSymbols asserts the graph holds exactly the named declarations
// for a file — no names means the file must hold no rows at all.
func covLifeWantSymbols(t *testing.T, e *Engine, absPath string, names ...string) {
	t.Helper()
	nodes, err := storeNodesByFile(t, e, absPath)
	if err != nil {
		t.Fatalf("read %s: %v", absPath, err)
	}
	if len(names) == 0 {
		if len(nodes) != 0 {
			t.Errorf("%s still holds %d node(s) after its deletion", absPath, len(nodes))
		}
		return
	}
	declared := map[string]bool{}
	for _, node := range nodes {
		declared[node.Name] = true
	}
	for _, name := range names {
		if !declared[name] {
			t.Errorf("%s: %q is not in the graph, declared = %v", absPath, name, declared)
		}
	}
}

// TestCovLifeUnchangedDiffEntryIsNotReingested pins the hash check the
// re-ingest performs. A diff can name a file whose content the graph already
// holds — every dependent is pulled in for exactly that reason — and
// re-writing its rows anyway would churn the timestamps every staleness
// comparison in the tool surface reads.
func TestCovLifeUnchangedDiffEntryIsNotReingested(t *testing.T) {
	e := builtEngine(t, []string{"lib/lib.go"})

	report, err := e.UpdateReport(graphstore.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if got := *report.ChangedFiles; !slices.Equal(got, []string{"lib/lib.go"}) {
		t.Fatalf("changed_files = %v, want the diff the engine reported", got)
	}
	if got := *report.FilesUpdated; got != 0 {
		t.Errorf("files_updated = %d, want 0: the content already matches", got)
	}
	if report.Summary != graphstore.NoChangesSummary() {
		t.Errorf("summary = %q, want the no-changes summary", report.Summary)
	}
}

// ── Post-build failures ─────────────────────────────────────────────────────

// covLifePendingSignature gives the signature pass a node to render, so the
// per-node write is reached rather than the worklist coming back empty.
func covLifePendingSignature(g *covLifeGraph) {
	g.store.fakeStore.pendingSignatures = []graphstore.GraphNode{{
		ID:            1,
		Kind:          kindFunction,
		Name:          "Greet",
		QualifiedName: g.file("lib/lib.go") + "::Greet",
		FilePath:      g.file("lib/lib.go"),
	}}
}

// TestCovLifeBuildPropagatesPostBuildFailures pins that every stage of the
// post-build pass reports its failure instead of returning a report that
// claims work which did not happen.
//
// The stages are named individually because several of them use the same store
// method: one build commits three times and re-reads all nodes twice, so a
// test that only asserted "the build failed" would pass with the error check
// missing from all but one of them.
func TestCovLifeBuildPropagatesPostBuildFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(*covLifeGraph)
	}{
		{"signature worklist", func(g *covLifeGraph) { g.store.fakeStore.signatureErr = errFake }},
		{"signature write", func(g *covLifeGraph) {
			covLifePendingSignature(g)
			g.store.signatureWriteErr = errFake
		}},
		{"signature commit", func(g *covLifeGraph) { g.fail(covLifeSeamCommit, 2) }},
		{"fts rebuild", func(g *covLifeGraph) { g.store.fakeStore.ftsErr = errFake }},
		{"shared node read", func(g *covLifeGraph) { g.fail(covLifeSeamAllNodes, 1) }},
		{"shared edge read", func(g *covLifeGraph) { g.store.fakeStore.allEdgesErr = errFake }},
		{"flow write", func(g *covLifeGraph) { g.store.flowWriteErr = errFake }},
		{"community write", func(g *covLifeGraph) { g.store.communityWriteErr = errFake }},
		{"summary node re-read", func(g *covLifeGraph) { g.fail(covLifeSeamAllNodes, 2) }},
		{"summary flow read", func(g *covLifeGraph) { g.fail(covLifeSeamFlows, 1) }},
		{"summary community read", func(g *covLifeGraph) { g.store.fakeStore.communitiesErr = errFake }},
		{"community summary write", func(g *covLifeGraph) { g.store.summaryWriteErr = errFake }},
		{"flow snapshot write", func(g *covLifeGraph) { g.store.snapshotWriteErr = errFake }},
		{"postprocess timestamp", func(g *covLifeGraph) {
			g.fail(covLifeSeam(covLifeSeamMetadata, metaLastPostprocessed), 1)
		}},
		{"postprocess level", func(g *covLifeGraph) {
			g.fail(covLifeSeam(covLifeSeamMetadata, metaPostprocessLevel), 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := covLifeIncremental(t)
			tc.arrange(g)
			_, err := g.build()
			covLifeWantInjected(t, tc.name, err)
		})
	}
}

// TestCovLifeStandalonePostprocessPropagatesStoreFailures pins the same
// discipline for the standalone pass, whose stage list is deliberately shorter
// than a build's.
func TestCovLifeStandalonePostprocessPropagatesStoreFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(*covLifeGraph)
	}{
		{"signature worklist", func(g *covLifeGraph) { g.store.fakeStore.signatureErr = errFake }},
		{"fts rebuild", func(g *covLifeGraph) { g.store.fakeStore.ftsErr = errFake }},
		{"graph node read", func(g *covLifeGraph) { g.store.fakeStore.allNodesErr = errFake }},
		{"graph edge read", func(g *covLifeGraph) { g.store.fakeStore.allEdgesErr = errFake }},
		{"flow write", func(g *covLifeGraph) { g.store.flowWriteErr = errFake }},
		{"community write", func(g *covLifeGraph) { g.store.communityWriteErr = errFake }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := covLifeIncremental(t)
			tc.arrange(g)
			_, err := g.postprocess()
			covLifeWantInjected(t, tc.name, err)
		})
	}
}

// TestCovLifeIncrementalFlowRetracePropagatesFailures pins the arm only an
// update reaches: flows are RE-TRACED from the ones already stored, so both
// that read and the replacing write have to be checked. A swallowed failure
// here would drop every previously detected flow.
func TestCovLifeIncrementalFlowRetracePropagatesFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(*covLifeGraph)
	}{
		{"stored flow read", func(g *covLifeGraph) { g.fail(covLifeSeamFlows, 1) }},
		{"retraced flow write", func(g *covLifeGraph) { g.store.flowWriteErr = errFake }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := covLifeIncremental(t)
			tc.arrange(g)
			_, err := g.update()
			covLifeWantInjected(t, tc.name, err)
		})
	}
}

// ── Status ──────────────────────────────────────────────────────────────────

// TestCovLifeStatusTreatsUnreadableMetadataAsAbsent pins that `status --json`
// stays a diagnostic: a metadata read that fails reports the key as null, the
// same as one that was never written, rather than failing the whole status.
func TestCovLifeStatusTreatsUnreadableMetadataAsAbsent(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{
		stats: graphstore.GraphStats{
			TotalNodes:  3,
			TotalEdges:  2,
			FilesCount:  2,
			LastUpdated: "2024-05-01T10:00:00",
		},
		metaErr: errFake,
	})

	status, err := e.Status()
	if err != nil {
		t.Fatalf("Status must stay a diagnostic: %v", err)
	}
	if !status.Ready || status.State != graphstore.CRGReadinessReady {
		t.Fatalf("state = %q ready = %v, want a ready graph", status.State, status.Ready)
	}
	for name, got := range map[string]*string{
		metaGitBranch:   status.BuiltOnBranch,
		metaGitHeadSHA:  status.BuiltAtCommit,
		metaSVNBranch:   status.SVNBranch,
		metaSVNRevision: status.SVNRevision,
	} {
		if got != nil {
			t.Errorf("%s = %q, want null when the metadata cannot be read", name, *got)
		}
	}
}

// ── Metadata stamping ───────────────────────────────────────────────────────

// TestCovLifeBuildNeverStampsEmptyVCSMetadata pins the rule the stamp's
// append guard exists for. A working copy whose VCS probe produces nothing
// must leave the keys unwritten, because upstream reads a stored branch or
// revision back and an empty string would overwrite a good one.
func TestCovLifeBuildNeverStampsEmptyVCSMetadata(t *testing.T) {
	root := writeFixture(t)
	if err := os.Mkdir(filepath.Join(root, ".svn"), 0o755); err != nil {
		t.Fatalf("mark the fixture as svn: %v", err)
	}
	if vcs := graphstore.DetectVCS(root); vcs != graphstore.VCSSVN {
		t.Fatalf("DetectVCS = %q, want svn", vcs)
	}
	e := Open(root)
	t.Cleanup(func() { _ = e.Close() })
	if _, err := e.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	store, err := e.readStore()
	if err != nil || store == nil {
		t.Fatalf("readStore = %v, %v", store, err)
	}
	if got, err := store.GetMetadata(metaLastBuildType); err != nil || got != buildTypeFull {
		t.Fatalf("last_build_type = %q, %v; want %q", got, err, buildTypeFull)
	}
	for _, key := range []string{metaSVNBranch, metaSVNRevision} {
		got, err := store.GetMetadata(key)
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		if got != "" {
			t.Errorf("%s = %q, want no row: an empty probe must not write one", key, got)
		}
	}
}

// ── Submodules ──────────────────────────────────────────────────────────────

// TestCovLifeSubmoduleSourcesNeedTheOptIn pins the flag's effect. The native
// scanner walks the tree, so it sees a nested repository's sources whether or
// not the caller asked for them; leaving the flag inert would ingest a
// submodule that `git ls-files` would have stopped at.
func TestCovLifeSubmoduleSourcesNeedTheOptIn(t *testing.T) {
	root := writeFixture(t)
	writeExtra(t, root, "nested/nested.go", "package nested\n\n// Nested is a submodule's source.\nfunc Nested() string { return \"n\" }\n")
	if err := os.Mkdir(filepath.Join(root, "nested", ".git"), 0o755); err != nil {
		t.Fatalf("mark the nested directory as a repository: %v", err)
	}
	t.Setenv("CRG_RECURSE_SUBMODULES", "0")

	excluded := covLifeBuildFileCount(t, root, nil)
	included := covLifeBuildFileCount(t, root, new(true))
	if included != excluded+1 {
		t.Fatalf("files_parsed = %d with the opt-in and %d without; want exactly the nested file more",
			included, excluded)
	}
}

// covLifeBuildFileCount builds root into its own database and returns how many
// files the build parsed.
func covLifeBuildFileCount(t *testing.T, root string, recurse *bool) int {
	t.Helper()
	e := Open(root)
	e.dbPath = filepath.Join(t.TempDir(), "code-graph.db")
	t.Cleanup(func() { _ = e.Close() })
	report, err := e.BuildReport(graphstore.BuildOptions{RecurseSubmodules: recurse})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	return *report.FilesParsed
}

// ── Path resolution ─────────────────────────────────────────────────────────

// TestCovLifeAbsPathKeepsAnAbsoluteInput pins that the graph-identity mapping
// is idempotent for a path that is already absolute. Re-joining it onto the
// repository root would produce an identity no stored row carries.
func TestCovLifeAbsPathKeepsAnAbsoluteInput(t *testing.T) {
	e := Open(t.TempDir())
	t.Cleanup(func() { _ = e.Close() })
	outside := filepath.Join(t.TempDir(), "vendor", "pkg", "file.go")

	if got := e.absPath(outside); got != NormalizeFilePath(outside) {
		t.Errorf("absPath(%q) = %q, want it unchanged", outside, got)
	}
	want := NormalizeFilePath(filepath.Join(e.absRoot(), "lib", "lib.go"))
	if got := e.absPath("lib/lib.go"); got != want {
		t.Errorf("absPath(relative) = %q, want %q", got, want)
	}
}

// TestCovLifeUnresolvableWorkingDirectoryIsReported pins the single failure
// mode of resolving a relative path: the process working directory no longer
// exists, so nothing relative can be made absolute.
//
// Every lifecycle entry point takes a repo_root, and the scan takes the
// engine's own root. Both must report that rather than carry on against a
// half-resolved tree, because the graph's whole identity scheme is the
// absolute path of each file.
func TestCovLifeUnresolvableWorkingDirectoryIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a working directory in use cannot be removed here")
	}
	relative := Open("relative-root")
	t.Cleanup(func() { _ = relative.Close() })
	scoped := covLifeIncremental(t).engine
	incremental := engineWithStore(t, "relative-root", &fakeStore{})
	t.Cleanup(func() { _ = incremental.Close() })

	covLifeWithoutWorkingDirectory(t, func() {
		if got := relative.absRoot(); got != "relative-root" {
			t.Errorf("absRoot() = %q, want the root left unresolved", got)
		}
		covLifeWantRootError(t, "BuildReport", func() error {
			_, err := scoped.BuildReport(graphstore.BuildOptions{RepoRoot: "elsewhere"})
			return err
		})
		covLifeWantRootError(t, "UpdateReport", func() error {
			_, err := scoped.UpdateReport(graphstore.UpdateOptions{RepoRoot: "elsewhere"})
			return err
		})
		covLifeWantRootError(t, "PostprocessReport", func() error {
			_, err := scoped.PostprocessReport(graphstore.PostprocessOptions{RepoRoot: "elsewhere"})
			return err
		})
		if _, err := incremental.incrementalUpdate(incremental.store, nonGitAutoBase); err == nil {
			t.Error("incrementalUpdate: want the unresolvable scan root reported")
		}
	})
}

// covLifeDeepPathBytes is comfortably past the 1024-byte path buffer darwin's
// getcwd answers from. Linux's buffer is larger, but there a removed working
// directory fails outright, so the depth only has to satisfy the tighter one.
const covLifeDeepPathBytes = 1100

// covLifeWithoutWorkingDirectory runs fn with the process working directory
// unresolvable — the only state in which making a relative path absolute
// fails.
//
// Both halves of the setup are load-bearing. The directory has to be DELETED,
// so its path cannot be reconstructed by walking back up from it; and it has
// to sit deeper than the kernel's path buffer, because darwin's getcwd
// shortcut still answers for a deleted directory and only a path too long for
// that buffer falls through to the walk. The path is built one short segment
// at a time because an absolute path past the limit cannot be passed to
// mkdir at all.
func covLifeWithoutWorkingDirectory(t *testing.T, fn func()) {
	t.Helper()
	base := t.TempDir()
	t.Chdir(base)
	segment := strings.Repeat("d", 200)
	for depth := len(base); depth < covLifeDeepPathBytes; depth += len(segment) + 1 {
		if err := os.Mkdir(segment, 0o755); err != nil {
			t.Fatalf("create a path segment: %v", err)
		}
		if err := os.Chdir(segment); err != nil {
			t.Fatalf("descend into a path segment: %v", err)
		}
	}
	if err := os.Remove(filepath.Join("..", segment)); err != nil {
		t.Fatalf("remove the working directory: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Fatalf("the removed working directory still resolves; this test's premise no longer holds")
	}
	fn()
}

// covLifeWantRootError asserts a call failed and named the root it could not
// resolve, so the caller can tell which path was at fault.
func covLifeWantRootError(t *testing.T, name string, call func() error) {
	t.Helper()
	err := call()
	if err == nil {
		t.Errorf("%s: want a repo-root resolution failure", name)
		return
	}
	if !strings.Contains(err.Error(), "elsewhere") {
		t.Errorf("%s: err = %v, want it to name the unresolvable root", name, err)
	}
}

// ── Tool surface ────────────────────────────────────────────────────────────

// TestCovLifeMutatingToolsSurfaceLifecycleFailures pins the handlers' error
// contract: a lifecycle failure becomes a tools/call failure, not a result
// dict that looks like a successful no-op.
func TestCovLifeMutatingToolsSurfaceLifecycleFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tool   string
		args   map[string]any
		engine func(*testing.T) *Engine
	}{
		{
			name:   "full rebuild",
			tool:   "build_or_update_graph_tool",
			args:   map[string]any{"full_rebuild": true},
			engine: unopenableEngine,
		},
		{
			name:   "incremental update",
			tool:   "build_or_update_graph_tool",
			args:   map[string]any{},
			engine: unopenableEngine,
		},
		{
			name:   "standalone postprocess",
			tool:   "run_postprocess_tool",
			args:   map[string]any{},
			engine: unreadableEngine,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := bindLifecycleToolArgs(t, nil, tc.tool, tc.args)
			handler, ok := toolHandlers[tc.tool]
			if !ok {
				t.Fatalf("%s has no native handler", tc.tool)
			}
			payload, err := handler(tc.engine(t), args)
			if err == nil {
				t.Fatalf("%s returned %v, want the lifecycle failure", tc.tool, payload)
			}
			if payload != nil {
				t.Errorf("%s returned a payload alongside its failure: %v", tc.tool, payload)
			}
		})
	}
}

// TestCovLifeReportPayloadRefusesAnUnprojectableReport pins the projection's
// two refusals. Both would otherwise reach a caller as a silently empty result
// dict, which reads as "the operation did nothing" rather than "it failed".
func TestCovLifeReportPayloadRefusesAnUnprojectableReport(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report *graphstore.CRGOperationReport
	}{
		{name: "no report at all", report: nil},
		{
			name: "a timing that cannot be encoded",
			report: &graphstore.CRGOperationReport{
				Status:            statusOK,
				PostprocessTiming: &graphstore.PostprocessTiming{SignaturesS: math.Inf(1)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := lifecycleReportPayload("build_or_update_graph_tool", tc.report)
			if err == nil {
				t.Fatalf("payload = %v, want a refusal", payload)
			}
			if !strings.Contains(err.Error(), "build_or_update_graph_tool") {
				t.Errorf("err = %v, want it to name the tool", err)
			}
		})
	}
}
