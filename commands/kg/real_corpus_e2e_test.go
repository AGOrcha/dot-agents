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
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(specsDir, e.Name(), "design.md")); err == nil {
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
	deps := testDeps()

	// ── Ingest the real corpus ────────────────────────────────────────────
	for _, s := range specs {
		ingestRealSource(t, deps, s, s.titleTok)
	}
	for _, r := range research {
		ingestRealSource(t, deps, r, r.titleTok)
	}

	// Every ingested source must have produced a source note on disk.
	for _, s := range append(append([]realCorpusSource{}, specs...), research...) {
		if exists, _ := noteExists(home, s.srcID); !exists {
			t.Errorf("expected source note %s after ingesting %s", s.srcID, s.path)
		}
	}

	// At least one extracted note (entity/decision/etc.) beyond the raw source
	// notes should exist — real specs carry entity/decision-shaped prose.
	assertExtractedNotesExist(t, home)

	// ── Warm the SQLite layer and confirm it indexed the real notes ───────
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
	if stats.NotesCount < len(specs) {
		t.Errorf("warm layer indexed %d notes, expected at least %d (one source note per spec)",
			stats.NotesCount, len(specs))
	}

	// ── Query: source_lookup must surface a real spec note by its name ────
	jsonDeps := Deps{
		Flags:        GlobalFlags{JSON: true},
		ExampleBlock: func(s ...string) string { return strings.Join(s, "\n") },
	}
	target := specs[0]
	out := captureStdout(t, func() {
		if err := runKGQuery(jsonDeps, newQueryCmd("source_lookup", "", 10), []string{target.titleTok}); err != nil {
			t.Fatalf("runKGQuery source_lookup: %v", err)
		}
	})
	var resp GraphQueryResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("source_lookup JSON invalid: %v\nraw: %s", err, out)
	}
	if !queryHasResult(resp, target.srcID) {
		t.Errorf("expected source_lookup(%q) to surface real spec note %s, got results=%#v",
			target.titleTok, target.srcID, resp.Results)
	}

	// ── Lint/maintain must run cleanly over the real corpus ───────────────
	report, err := runGraphLint(testIO(), home)
	if err != nil {
		t.Fatalf("runGraphLint over real corpus: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "ops", "lint", "lint-report.json")); err != nil {
		t.Errorf("expected lint-report.json persisted after lint: %v", err)
	}
	// Lint is allowed to surface warnings (orphan/stale pages are normal for a
	// freshly-ingested corpus) but must not raise hard errors: a non-zero
	// ErrorCount means lint itself choked on real content.
	if report.ErrorCount > 0 {
		t.Errorf("lint reported %d hard errors over the real corpus (results=%#v)",
			report.ErrorCount, report.Results)
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

// queryHasResult reports whether resp contains a result with the given ID.
func queryHasResult(resp GraphQueryResponse, id string) bool {
	for _, r := range resp.Results {
		if r.ID == id {
			return true
		}
	}
	return false
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

	// Build over a bounded, representative subtree of the real source tree so
	// the build stays fast under -race. Copying keeps the build off the live
	// repo .git/.code-review-graph and avoids mutating developer state.
	subtreeName := filepath.Join("commands", "kg")
	buildRoot := stageRealSubtree(t, repoRoot, subtreeName)

	// Symlink the real .venv (the one DiscoverCRGBin just found) into the staged
	// build root so every bridge built from buildRoot — build, code-status, and
	// impact — discovers the same working CRG binary and resolves its sibling
	// python3 (which has the code_review_graph package). Without this, the
	// CRG-internal python query path (runKGImpact) would fall back to a system
	// python lacking the module.
	linkRealVenv(t, crgBin, buildRoot)

	// NewCRGBridge re-discovers the binary from buildRoot/.venv (the symlink),
	// so build/status/impact all share one consistent CRG + python.
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

	// The produced graph.db must contain real Go symbol nodes.
	nodeCount := assertRealGraphDBNonEmpty(t, buildRoot)
	if report.Status == nil || report.Status.Nodes == 0 {
		t.Fatalf("expected non-zero nodes from real source build, got status=%+v", report.Status)
	}

	// code-status must agree with the graph.db the real build produced.
	statusCmd := &cobra.Command{}
	statusCmd.Flags().String("repo", buildRoot, "")
	statusCmd.Flags().Bool("json", true, "")
	statusOut := captureStdout(t, func() {
		if err := runKGCodeStatus(testDeps(), statusCmd, nil); err != nil {
			t.Fatalf("runKGCodeStatus over real graph: %v", err)
		}
	})
	var status graphstore.CRGStatus
	if err := json.Unmarshal(statusOut, &status); err != nil {
		t.Fatalf("code-status JSON invalid: %v\nraw: %s", err, statusOut)
	}
	if status.Nodes != nodeCount {
		t.Errorf("code-status Nodes=%d disagrees with graph.db COUNT(*)=%d", status.Nodes, nodeCount)
	}

	// ── Impact: a real changed Go file must resolve to real symbol nodes ──
	impactCmd := &cobra.Command{}
	impactCmd.Flags().String("repo", buildRoot, "")
	impactCmd.Flags().String("base", "", "")
	impactCmd.Flags().Int("depth", 2, "")
	impactCmd.Flags().Int("limit", 50, "")
	impactCmd.Flags().Bool("require-graph", true, "")
	impactCmd.Flags().Bool("json", true, "")

	impactOut := captureStdout(t, func() {
		// sync_code_warm_link.go is a real file in the staged subtree with many
		// exported symbols, so impact should resolve nodes for it.
		if err := runKGImpact(testDeps(), impactCmd, []string{"sync_code_warm_link.go"}); err != nil {
			t.Fatalf("runKGImpact over real graph: %v", err)
		}
	})
	var impact kgImpactJSONOutput
	if err := json.Unmarshal(impactOut, &impact); err != nil {
		t.Fatalf("impact JSON invalid: %v\nraw: %s", err, impactOut)
	}
	if impact.GraphState == "" {
		t.Errorf("expected non-empty graph_state in impact output, got: %s", impactOut)
	}
	if impact.CRGImpactResult == nil {
		t.Fatalf("expected an impact result payload, got: %s", impactOut)
	}
	// On a freshly-built graph with no diff, the changed/impacted sets may be
	// empty; the load-bearing assertion is that the build produced real symbol
	// nodes (asserted above) and that impact resolves against the real graph
	// without error and reports a coherent state.
	if impact.Summary == "" {
		t.Errorf("expected a non-empty impact summary over the real graph, got: %s", impactOut)
	}
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
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read subtree %s: %v", srcDir, err)
	}
	copied := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		// Skip test files — CRG only needs production symbols and test files
		// inflate the build.
		if strings.HasSuffix(e.Name(), "_test.go") {
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
	if copied == 0 {
		t.Fatalf("no production .go files staged from %s", srcDir)
	}
	// Commit the staged files so CRG's full build (which parses tracked files)
	// sees the real source. git add -A + commit captures the nested tree.
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "stage real subtree"}} {
		if out, err := runGit(t, dst, args...); err != nil {
			t.Fatalf("git %v in staged tree: %v\n%s", args, err, out)
		}
	}
	return dst
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
