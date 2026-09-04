package kg

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"

	_ "modernc.org/sqlite"
)

// Integration tests that exercise the KG commands against the REAL dot-agents
// knowledge corpus and source tree, rather than only the synthetic t.TempDir
// fixtures used by curation_cycle_e2e_test.go and the per-command unit tests.
//
// They close a release-confidence gap: nothing else proves the KG actually
// ingests/queries the real `.agents/workflow/specs/*/design.md` + `research/*.md`
// corpus (doc lane) or builds a code graph over the real Go module (code lane).
//
// Both lanes are gated to keep the default `go test` green and offline:
//   - testing.Short() skips both (the doc lane walks dozens of real files; the
//     code lane shells out to the code-review-graph CLI).
//   - The code lane additionally skips when the real CRG binary is not
//     discoverable (CI installs it into a repo-root .venv per
//     .github/workflows/test.yml "Set up code-review-graph"; locally it is
//     usually absent), mirroring internal/graphstore TestCRGBridgeFreshBuildRealCRG.

// repoRootFromTest returns the dot-agents repo root relative to this test file.
// commands/kg/<file> → repo root is two levels up. Mirrors the runtime.Caller
// approach used by internal/graphstore/crg_test.go so the test reads the
// actual on-disk corpus regardless of the working directory go test runs from.
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate repo root")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(testFile), "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	// Sanity-check we found the module root.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected go.mod at inferred repo root %s: %v", root, err)
	}
	return root
}

// realCorpusSource pairs an on-disk corpus file with the stable source ID it
// becomes once ingested (src-<slug-of-filename>).
type realCorpusSource struct {
	path     string // absolute path on disk
	srcID    string // expected note ID after ingest: "src-" + slugify(basename-without-ext)
	titleTok string // a lower-cased token guaranteed to appear in the title, for source_lookup
}

// collectRealSpecs gathers a representative sample of real spec design docs.
// Every spec file is named design.md, which would collide on ingest (all slug
// to "src-design"). To preserve one note per spec we copy each design.md into
// the KG inbox renamed to "<spec-name>.md", so the source ID and title carry
// the spec name and source_lookup can target it deterministically.
//
// The sample is capped (not all ~47 specs) so the doc lane stays fast under
// -race; the cap is documented in the test and the assertions only require a
// representative subset, not the full corpus.
func collectRealSpecs(t *testing.T, repoRoot string, max int) []realCorpusSource {
	t.Helper()
	specsDir := filepath.Join(repoRoot, ".agents", "workflow", "specs")
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("read specs dir %s: %v", specsDir, err)
	}
	var names []string
	for _, e := range entries {
		if specHasDesign(specsDir, e) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatalf("no spec design.md files found under %s", specsDir)
	}
	sort.Strings(names) // deterministic sample
	if max > 0 && len(names) > max {
		names = names[:max]
	}
	out := make([]realCorpusSource, 0, len(names))
	for _, name := range names {
		out = append(out, realCorpusSource{
			path:     filepath.Join(specsDir, name, "design.md"),
			srcID:    "src-" + slugify(name),
			titleTok: strings.ToLower(name),
		})
	}
	return out
}

// specHasDesign reports whether dir entry e is a spec directory containing a
// design.md file.
func specHasDesign(specsDir string, e os.DirEntry) bool {
	if !e.IsDir() {
		return false
	}
	_, err := os.Stat(filepath.Join(specsDir, e.Name(), "design.md"))
	return err == nil
}

// collectRealResearch gathers a few real research markdown docs. These already
// have unique filenames, so no renaming is needed.
func collectRealResearch(t *testing.T, repoRoot string, max int) []realCorpusSource {
	t.Helper()
	researchDir := filepath.Join(repoRoot, "research")
	entries, err := os.ReadDir(researchDir)
	if err != nil {
		t.Fatalf("read research dir %s: %v", researchDir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		t.Fatalf("no research *.md files found under %s", researchDir)
	}
	sort.Strings(names)
	if max > 0 && len(names) > max {
		names = names[:max]
	}
	out := make([]realCorpusSource, 0, len(names))
	for _, name := range names {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		out = append(out, realCorpusSource{
			path:     filepath.Join(researchDir, name),
			srcID:    "src-" + slugify(base),
			titleTok: strings.ToLower(base),
		})
	}
	return out
}

// ingestRealSource copies the on-disk corpus file into a uniquely-named temp
// file (so its slug → source ID is the spec/research name, not "design") and
// runs runKGIngest over it with an explicit title carrying the same name.
func ingestRealSource(t *testing.T, deps Deps, src realCorpusSource, title string) {
	t.Helper()
	data, err := os.ReadFile(src.path)
	if err != nil {
		t.Fatalf("read corpus file %s: %v", src.path, err)
	}
	// Recover the slug source from the expected ID ("src-<slug>") so the temp
	// filename slugs back to the same source ID inside runKGIngest.
	slug := strings.TrimPrefix(src.srcID, "src-")
	tmpFile := filepath.Join(t.TempDir(), slug+".md")
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		t.Fatalf("stage corpus file %s: %v", tmpFile, err)
	}
	captureStdout(t, func() {
		if err := runKGIngest(deps, newIngestCmd(false, false, title, "markdown"), []string{tmpFile}); err != nil {
			t.Fatalf("runKGIngest %s: %v", src.path, err)
		}
	})
}

// TestKGDocLane_RealCorpus ingests a representative sample of the real
// dot-agents spec + research corpus into a temp KG home and asserts the full
// doc lane (ingest → warm → query → lint) operates over real content.
//
// Scope note: a capped sample (not all ~47 specs) is ingested so the lane
// stays fast under -race; the assertions require a representative subset, not
// the whole corpus.
func TestKGDocLane_RealCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-corpus doc lane in -short mode (walks dozens of real corpus files)")
	}

	repoRoot := repoRootFromTest(t)
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("runKGSetup: %v", err)
	}

	specs := collectRealSpecs(t, repoRoot, 12)
	research := collectRealResearch(t, repoRoot, 3)

	// ingest → assert source notes → warm → query → lint, each step flat.
	ingestRealCorpus(t, specs, research)
	assertSourceNotesExist(t, home, specs, research)
	// At least one extracted note (entity/decision/etc.) beyond the raw source
	// notes should exist — real specs carry entity/decision-shaped prose.
	assertExtractedNotesExist(t, home)
	assertWarmIndexedNotes(t, home, len(specs))
	assertSourceLookupContent(t, specs[0])
	assertLintRunsClean(t, home)
}

// ingestRealCorpus ingests every spec + research source into the active KG home.
func ingestRealCorpus(t *testing.T, specs, research []realCorpusSource) {
	t.Helper()
	deps := testDeps()
	for _, s := range specs {
		ingestRealSource(t, deps, s, s.titleTok)
	}
	for _, r := range research {
		ingestRealSource(t, deps, r, r.titleTok)
	}
}

// assertSourceNotesExist confirms each ingested source produced a source note.
func assertSourceNotesExist(t *testing.T, home string, specs, research []realCorpusSource) {
	t.Helper()
	all := append(append([]realCorpusSource{}, specs...), research...)
	for _, s := range all {
		if exists, _ := noteExists(home, s.srcID); !exists {
			t.Errorf("expected source note %s after ingesting %s", s.srcID, s.path)
		}
	}
}

// assertWarmIndexedNotes warms the SQLite layer and asserts it indexed at least
// one note per spec.
func assertWarmIndexedNotes(t *testing.T, home string, specCount int) {
	t.Helper()
	if err := runKGWarm(newKGWarmCmdForTest(), nil); err != nil {
		t.Fatalf("runKGWarm: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	stats, err := store.GetStats()
	store.Close()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.NotesCount < specCount {
		t.Errorf("warm layer indexed %d notes, expected at least %d (one source note per spec)",
			stats.NotesCount, specCount)
	}
}

// assertSourceLookupContent runs source_lookup for the target spec and asserts
// the matched result is genuinely that spec's source note (right type + title),
// not just any non-empty row.
func assertSourceLookupContent(t *testing.T, target realCorpusSource) {
	t.Helper()
	resp := runJSONQuery(t, newQueryCmd("source_lookup", "", 10), target.titleTok)
	matched := findQueryResult(resp, target.srcID)
	if matched == nil {
		t.Fatalf("expected source_lookup(%q) to surface real spec note %s, got results=%#v",
			target.titleTok, target.srcID, resp.Results)
	}
	if matched.Type != "source" {
		t.Errorf("expected matched note type=source, got %q (result=%#v)", matched.Type, matched)
	}
	if !strings.Contains(strings.ToLower(matched.Title), target.titleTok) {
		t.Errorf("expected matched note title to carry the real spec name %q, got title=%q",
			target.titleTok, matched.Title)
	}
}

// runJSONQuery runs runKGQuery in JSON mode and decodes the response envelope.
func runJSONQuery(t *testing.T, cmd *cobra.Command, query string) GraphQueryResponse {
	t.Helper()
	out := captureStdout(t, func() {
		if err := runKGQuery(jsonModeDeps(), cmd, []string{query}); err != nil {
			t.Fatalf("runKGQuery %q: %v", query, err)
		}
	})
	var resp GraphQueryResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("query JSON invalid: %v\nraw: %s", err, out)
	}
	return resp
}

// assertLintRunsClean runs lint over the real corpus and asserts the report
// persists and reports zero hard errors (warnings are allowed).
func assertLintRunsClean(t *testing.T, home string) {
	t.Helper()
	report, err := runGraphLint(testIO(), home)
	if err != nil {
		t.Fatalf("runGraphLint over real corpus: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "ops", "lint", "lint-report.json")); err != nil {
		t.Errorf("expected lint-report.json persisted after lint: %v", err)
	}
	if report.ErrorCount > 0 {
		t.Errorf("lint reported %d hard errors over the real corpus (results=%#v)",
			report.ErrorCount, report.Results)
	}
}

// jsonModeDeps returns Deps configured for JSON output.
func jsonModeDeps() Deps {
	return Deps{
		Flags:        GlobalFlags{JSON: true},
		ExampleBlock: func(s ...string) string { return strings.Join(s, "\n") },
	}
}

// assertExtractedNotesExist confirms ingest extracted at least one non-source
// note (entity / decision / concept) from the real corpus bodies.
func assertExtractedNotesExist(t *testing.T, home string) {
	t.Helper()
	for _, sub := range []string{"entities", "decisions", "concepts"} {
		entries, err := os.ReadDir(filepath.Join(home, "notes", sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".md") {
				return // found at least one extracted note
			}
		}
	}
	t.Error("expected at least one extracted entity/decision/concept note from the real corpus")
}

// findQueryResult returns the result with the given ID, or nil if absent.
func findQueryResult(resp GraphQueryResponse, id string) *GraphQueryResult {
	for i := range resp.Results {
		if resp.Results[i].ID == id {
			return &resp.Results[i]
		}
	}
	return nil
}

// TestKGCodeLane_RealCorpus builds the code graph over the real repository
// source and asserts the code lane produces meaningful results on real Go
// symbols (nodes exist; impact/changes queries resolve against them).
//
// Gating: skipped under -short and whenever the real code-review-graph binary
// is not discoverable (DiscoverCRGBin). CI installs it into a repo-root .venv
// per .github/workflows/test.yml; locally it is usually absent, so the skip
// keeps the default suite green and offline.
//
// Scope note: the graph is built over a representative subtree
// (commands/kg) rather than the whole module, so the build stays bounded
// under -race in CI. The subtree still contains dozens of real Go symbols, so
// the node-count and impact assertions remain meaningful.
func TestKGCodeLane_RealCorpus(t *testing.T) {
	useBridgeBackend(t)
	if testing.Short() {
		t.Skip("skipping real-corpus code lane in -short mode (shells out to code-review-graph)")
	}
	if runtime.GOOS == "windows" {
		t.Skip("real CRG build is exercised on POSIX CI; Windows CRG discovery is covered by internal/graphstore crg_venv_windows.go tests")
	}

	repoRoot := repoRootFromTest(t)

	// Gate on the REAL CRG binary, mirroring internal/graphstore's real-CRG test.
	crgBin, err := graphstore.DiscoverCRGBin(repoRoot)
	if err != nil {
		t.Skipf("real code-review-graph not available: %v", err)
	}

	// Stage a bounded subtree, build its real code graph, and return the build
	// root + the graph.db node count (already asserted non-empty).
	buildRoot, nodeCount := setupRealCodeGraph(t, repoRoot, crgBin)

	// Flat sequence of code-lane assertions over the real built graph.
	assertCodeStatusMatches(t, buildRoot, nodeCount)
	assertImpactResolves(t, buildRoot)
	assertRealCRGBridgeAndQuery(t, buildRoot, nodeCount)
}

// setupRealCodeGraph stages a bounded real subtree, symlinks the real .venv so
// every bridge over the staged tree shares one working CRG + python, runs the
// build, and returns the build root + the graph.db node count. Build failures
// caused by an un-importable CRG python package skip rather than fail.
func setupRealCodeGraph(t *testing.T, repoRoot, crgBin string) (string, int) {
	t.Helper()
	// Build over a bounded, representative subtree of the real source tree so
	// the build stays fast under -race. Copying keeps the build off the live
	// repo .git/.code-review-graph and avoids mutating developer state.
	buildRoot := stageRealSubtree(t, repoRoot, filepath.Join("commands", "kg"))

	// Symlink the real .venv (the one DiscoverCRGBin just found) into the staged
	// build root so build/code-status/impact discover the same working CRG
	// binary and resolve its sibling python3 (which has code_review_graph).
	linkRealVenv(t, crgBin, buildRoot)

	// NewCRGBridge re-discovers the binary from buildRoot/.venv (the symlink).
	bridge, err := graphstore.NewCRGBridge(buildRoot)
	if err != nil {
		t.Fatalf("NewCRGBridge over staged tree: %v", err)
	}
	report, err := bridge.BuildReport(graphstore.BuildOptions{
		SkipFlows:       true,
		SkipPostprocess: true,
	})
	if err != nil {
		skipOrFailRealCRGBuild(t, report, err)
	}
	if report.Outcome != graphstore.CRGReadinessReady {
		t.Fatalf("expected build outcome=%q over real source, got %q; summary: %s",
			graphstore.CRGReadinessReady, report.Outcome, report.Summary)
	}
	if report.Status == nil || report.Status.Nodes == 0 {
		t.Fatalf("expected non-zero nodes from real source build, got status=%+v", report.Status)
	}
	// The produced graph.db must contain real Go symbol nodes.
	return buildRoot, assertRealGraphDBNonEmpty(t, buildRoot)
}

// assertCodeStatusMatches asserts kg code-status agrees with the graph.db count.
func assertCodeStatusMatches(t *testing.T, buildRoot string, nodeCount int) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", buildRoot, "")
	cmd.Flags().Bool("json", true, "")
	out := captureStdout(t, func() {
		if err := runKGCodeStatus(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGCodeStatus over real graph: %v", err)
		}
	})
	var status graphstore.CRGStatus
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("code-status JSON invalid: %v\nraw: %s", err, out)
	}
	if status.Nodes != nodeCount {
		t.Errorf("code-status Nodes=%d disagrees with graph.db COUNT(*)=%d", status.Nodes, nodeCount)
	}
}

// assertImpactResolves runs kg impact for a real staged file and asserts it
// resolves against the real graph with a coherent state + payload.
func assertImpactResolves(t *testing.T, buildRoot string) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", buildRoot, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Bool("require-graph", true, "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		// sync_code_warm_link.go is a real file in the staged subtree with many
		// exported symbols, so impact should resolve nodes for it.
		if err := runKGImpact(testDeps(), cmd, []string{"sync_code_warm_link.go"}); err != nil {
			t.Fatalf("runKGImpact over real graph: %v", err)
		}
	})
	var impact kgImpactJSONOutput
	if err := json.Unmarshal(out, &impact); err != nil {
		t.Fatalf("impact JSON invalid: %v\nraw: %s", err, out)
	}
	// On a freshly-built graph with no diff, the changed/impacted sets may be
	// empty; the load-bearing assertion is that the build produced real symbol
	// nodes (asserted in setup) and that impact resolves against the real graph
	// without error and reports a coherent state + payload.
	if impact.GraphState == "" {
		t.Errorf("expected non-empty graph_state in impact output, got: %s", out)
	}
	if impact.CRGImpactResult == nil {
		t.Fatalf("expected an impact result payload, got: %s", out)
	}
	if impact.Summary == "" {
		t.Errorf("expected a non-empty impact summary over the real graph, got: %s", out)
	}
}

// assertRealCRGBridgeAndQuery exercises the CRG bridge import + the
// `da kg bridge` query/health surface over the freshly-built real code graph.
//
// Flow: a temp KG_HOME is set up, the real CRG nodes/edges are imported into
// its warm SQLite store via runKGWarmCodeImport (which constructs a CRG bridge
// over buildRoot and reads its graph.db), then runKGBridgeQuery runs the
// symbol_lookup code-bridge intent for a real function from the staged subtree.
// graphNodeCount is the graph.db COUNT(*) the caller already asserted, used to
// cross-check the bridge import against the source of truth.
func assertRealCRGBridgeAndQuery(t *testing.T, buildRoot string, graphNodeCount int) {
	t.Helper()

	// Fresh KG home for the warm store; KG_HOME is what the bridge query reads.
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("runKGSetup for bridge home: %v", err)
	}

	// import → warm-store stats → bridge query → bridge health, each step flat.
	importRealCRGGraph(t, home, buildRoot, graphNodeCount)
	assertWarmStoreStats(t, home, graphNodeCount)
	assertBridgeSymbolLookup(t)
	assertBridgeHealthAvailable(t)
}

// importRealCRGGraph imports the real graph.db nodes/edges into the warm SQLite
// store at home (the CRG bridge path: NewCRGBridge → ReadNodes/ReadEdges) and
// asserts the imported counts are consistent with the graph.db COUNT(*).
func importRealCRGGraph(t *testing.T, home, buildRoot string, graphNodeCount int) {
	t.Helper()
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	nodesImported, edgesImported, err := runKGWarmCodeImport(store, buildRoot)
	store.Close()
	if err != nil {
		t.Fatalf("runKGWarmCodeImport over real CRG graph: %v", err)
	}
	// File-kind nodes plus function/class nodes are all counted in graph.db's
	// nodes table, so the imported count should match the graph.db COUNT(*).
	if nodesImported != graphNodeCount {
		t.Errorf("CRG bridge imported %d nodes but graph.db has %d", nodesImported, graphNodeCount)
	}
	if edgesImported == 0 {
		t.Errorf("expected the CRG bridge to import code edges from the real graph, got 0")
	}
}

// assertWarmStoreStats reopens the warm store and asserts its node count agrees
// with the graph.db COUNT(*).
func assertWarmStoreStats(t *testing.T, home string, graphNodeCount int) {
	t.Helper()
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	stats, err := store.GetStats()
	store.Close()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalNodes != graphNodeCount {
		t.Errorf("warm store TotalNodes=%d disagrees with graph.db COUNT(*)=%d", stats.TotalNodes, graphNodeCount)
	}
}

// assertBridgeSymbolLookup drives the `da kg bridge` symbol_lookup code intent
// for a real function from the staged subtree and asserts the result is that
// real Go symbol with evidenced (non-sparse) provenance.
func assertBridgeSymbolLookup(t *testing.T) {
	t.Helper()
	// runKGWarm is a real exported function defined in the staged
	// sync_code_warm_link.go, so the warm-store-backed symbol_lookup bridge
	// intent must return a node whose qualified name carries it.
	const realSymbol = "runKGWarm"
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "symbol_lookup", "")
	out := captureStdout(t, func() {
		if err := runKGBridgeQuery(jsonModeDeps(), cmd, []string{realSymbol}); err != nil {
			t.Fatalf("runKGBridgeQuery symbol_lookup: %v", err)
		}
	})
	var resp GraphQueryResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("bridge symbol_lookup JSON invalid: %v\nraw: %s", err, out)
	}
	if resp.Intent != "symbol_lookup" {
		t.Errorf("bridge intent: got %q want symbol_lookup", resp.Intent)
	}
	if !bridgeResultMatchesSymbol(resp, realSymbol) {
		t.Fatalf("expected bridge symbol_lookup(%q) to surface the real %s node from the built graph, got results=%#v",
			realSymbol, realSymbol, resp.Results)
	}
	// The matched node must carry real CRG-derived metadata (it's a Go function),
	// proving the result came from the built code graph and not a stub.
	if !bridgeResultHasGoSymbol(resp, realSymbol) {
		t.Errorf("expected the matched %s node to be a Go symbol with a file path, got results=%#v",
			realSymbol, resp.Results)
	}
	// A warm store with imported nodes must NOT be flagged sparse.
	if resp.SparsityScore == nil || *resp.SparsityScore != 0 {
		t.Errorf("expected sparsity_score=0 for an evidenced bridge lookup, got %v", resp.SparsityScore)
	}
}

// assertBridgeHealthAvailable drives `da kg bridge health` and asserts it
// reports an available adapter over the populated KG home.
func assertBridgeHealthAvailable(t *testing.T) {
	t.Helper()
	out := captureStdout(t, func() {
		if err := runKGBridgeHealth(jsonModeDeps(), &cobra.Command{}, nil); err != nil {
			t.Fatalf("runKGBridgeHealth: %v", err)
		}
	})
	var health []KGAdapterHealth
	if err := json.Unmarshal(out, &health); err != nil {
		t.Fatalf("bridge health JSON invalid: %v\nraw: %s", err, out)
	}
	if len(health) == 0 || !health[0].Available {
		t.Errorf("expected bridge health to report an available adapter, got %#v", health)
	}
}

// bridgeResultMatchesSymbol reports whether any result references the given
// symbol via its ID, qualified name, or title.
func bridgeResultMatchesSymbol(resp GraphQueryResponse, symbol string) bool {
	for _, r := range resp.Results {
		if strings.Contains(r.QualifiedName, symbol) ||
			strings.Contains(r.ID, symbol) ||
			strings.Contains(r.Title, symbol) {
			return true
		}
	}
	return false
}

// bridgeResultHasGoSymbol reports whether the result matching symbol carries
// real CRG-derived Go metadata (a .go file path), confirming it came from the
// built code graph rather than a synthetic stub.
func bridgeResultHasGoSymbol(resp GraphQueryResponse, symbol string) bool {
	for _, r := range resp.Results {
		if !strings.Contains(r.QualifiedName, symbol) {
			continue
		}
		if strings.HasSuffix(r.FilePath, ".go") {
			return true
		}
	}
	return false
}

// stageRealSubtree copies a real source subtree into a fresh git repo under a
// temp dir so the CRG build operates on real code without touching the live
// repo's .git or .code-review-graph state. Returns the staged repo root.
func stageRealSubtree(t *testing.T, repoRoot, subtree string) string {
	t.Helper()
	dst := t.TempDir()
	// Init git before staging so the copied files can be committed; CRG's full
	// build parses git-tracked files, so the staged .go files must be committed.
	initGitRepo(t, dst)
	srcDir := filepath.Join(repoRoot, subtree)
	dstDir := filepath.Join(dst, subtree)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if copyGoProductionFiles(t, srcDir, dstDir) == 0 {
		t.Fatalf("no production .go files staged from %s", srcDir)
	}
	commitStagedTree(t, dst)
	return dst
}

// copyGoProductionFiles copies the production (non-test) .go files directly
// under srcDir into dstDir and returns the count copied.
func copyGoProductionFiles(t *testing.T, srcDir, dstDir string) int {
	t.Helper()
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read subtree %s: %v", srcDir, err)
	}
	copied := 0
	for _, e := range entries {
		// Skip dirs, non-Go files, and test files — CRG only needs production
		// symbols and test files inflate the build.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dstDir, e.Name()), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
		copied++
	}
	return copied
}

// commitStagedTree commits everything in the staged repo so CRG's full build
// (which parses tracked files) sees the real source.
func commitStagedTree(t *testing.T, dst string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "stage real subtree"}} {
		if out, err := runGit(t, dst, args...); err != nil {
			t.Fatalf("git %v in staged tree: %v\n%s", args, err, out)
		}
	}
}

// linkRealVenv symlinks the real virtualenv that contains crgBin into
// buildRoot/.venv, so a bridge constructed from buildRoot discovers the same
// working code-review-graph binary and its sibling python3. crgBin is e.g.
// <venv>/bin/code-review-graph, so the venv root is two directories up.
func linkRealVenv(t *testing.T, crgBin, buildRoot string) {
	t.Helper()
	venvRoot := filepath.Dir(filepath.Dir(crgBin)) // <venv>/bin/crg → <venv>
	link := filepath.Join(buildRoot, ".venv")
	if err := os.Symlink(venvRoot, link); err != nil {
		t.Fatalf("symlink real .venv %s -> %s: %v", venvRoot, link, err)
	}
}

// skipOrFailRealCRGBuild mirrors internal/graphstore.skipOrFailCRGBuildErr:
// skip when the CRG binary was discoverable but its Python package is not
// importable in this environment (e.g. a sibling worktree's .venv shim on
// PATH), and fail loudly otherwise.
func skipOrFailRealCRGBuild(t *testing.T, report *graphstore.CRGOperationReport, err error) {
	t.Helper()
	combined := err.Error()
	if report != nil {
		combined += " " + report.Summary
	}
	if strings.Contains(combined, "code_review_graph") &&
		(strings.Contains(combined, "No module named") || strings.Contains(combined, "ModuleNotFoundError")) {
		t.Skipf("real CRG binary discovered but its Python package is not importable here: %v", err)
	}
	t.Fatalf("real CRG BuildReport failed: %v", err)
}

// assertRealGraphDBNonEmpty opens the CRG graph.db under repoRoot and asserts
// the nodes table is non-empty, returning the row count.
func assertRealGraphDBNonEmpty(t *testing.T, repoRoot string) int {
	t.Helper()
	dbPath := graphstore.CRGDBPath(repoRoot)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("graph.db not found at %s: %v", dbPath, err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open graph.db: %v", err)
	}
	defer db.Close()
	var nodeCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&nodeCount); err != nil {
		t.Fatalf("SELECT COUNT(*) FROM nodes: %v", err)
	}
	if nodeCount == 0 {
		t.Fatal("graph.db has zero rows in nodes table after real source build")
	}
	return nodeCount
}
