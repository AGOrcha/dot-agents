package codegraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// fixtureFiles is a miniature multi-package Go repo: `app` calls into `lib`,
// `lib` has a helper with no test, and one test file exercises the exported
// entry point. It is enough to produce every edge kind the ingester emits.
var fixtureFiles = map[string]string{
	"go.mod": "module example.com/fixture\n\ngo 1.24\n",
	"lib/lib.go": `package lib

// Config is the library's configuration.
type Config struct {
	Name string
}

// Greet builds a greeting.
func Greet(c Config) string {
	return decorate(c.Name)
}

// decorate is an untested helper.
func decorate(s string) string {
	return "hello " + s
}
`,
	"app/app.go": `package app

import "example.com/fixture/lib"

// Run greets using the library.
func Run(name string) string {
	return lib.Greet(lib.Config{Name: name})
}
`,
	"app/app_test.go": `package app

import "testing"

func TestRun(t *testing.T) {
	if Run("x") == "" {
		t.Fatal("empty")
	}
}
`,
}

// writeFixture materializes fixtureFiles under a fresh temp directory.
func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range fixtureFiles {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// newEngine returns an engine over a fresh fixture with git stubbed out, so no
// test depends on a real repository or on `git` being installed.
func newEngine(t *testing.T, changed []string) (*Engine, string) {
	t.Helper()
	root := writeFixture(t)
	e := Open(root)
	e.changedFiles = func(string, string) ([]string, error) { return changed, nil }
	t.Cleanup(func() { _ = e.Close() })
	return e, root
}

// builtEngine returns an engine whose graph has already been built.
func builtEngine(t *testing.T, changed []string) *Engine {
	t.Helper()
	e, _ := newEngine(t, changed)
	if _, err := e.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	return e
}

// symbolKinds indexes a corpus by qualified name.
func symbolKinds(t *testing.T, root string) map[string]string {
	t.Helper()
	_, corpus, err := Scan(root, "abc123")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	out := map[string]string{}
	for _, s := range corpus.Symbols {
		out[s.QualifiedName] = s.Kind
	}
	return out
}

// fixtureFile is a fixture file's node identity: the absolute, slash-separated
// path, which is also the File node's whole qualified name.
func fixtureFile(root, rel string) string {
	return filepath.ToSlash(filepath.Join(root, filepath.FromSlash(rel)))
}

// fixtureSymbol is the identity of a symbol declared in a fixture file:
// `<abs path>::<symbol path>`, where a method's symbol path is
// `<Receiver>.<Method>`.
func fixtureSymbol(root, rel, symbol string) string {
	return fixtureFile(root, rel) + "::" + symbol
}

func TestScanDeclaresFunctionsTypesAndTests(t *testing.T) {
	root := writeFixture(t)
	kinds := symbolKinds(t, root)
	for _, want := range []struct {
		qualified string
		kind      string
	}{
		{fixtureSymbol(root, "lib/lib.go", "Config"), kindClass},
		{fixtureSymbol(root, "lib/lib.go", "Greet"), kindFunction},
		{fixtureSymbol(root, "lib/lib.go", "decorate"), kindFunction},
		{fixtureSymbol(root, "app/app.go", "Run"), kindFunction},
		{fixtureSymbol(root, "app/app_test.go", "TestRun"), kindTest},
	} {
		if kinds[want.qualified] != want.kind {
			t.Errorf("symbol %q kind = %q, want %q",
				want.qualified, kinds[want.qualified], want.kind)
		}
	}
}

// TestScanEmitsUpstreamEdgeVocabulary pins the four edge kinds and the
// resolution each endpoint gets. The type-usage `IMPORTS` edge this fixture
// used to assert (`app.Run -> lib.Config`) no longer exists: the release emits
// IMPORTS_FROM from the FILE to the raw import path, and models a type
// reference as no edge at all.
func TestScanEmitsUpstreamEdgeVocabulary(t *testing.T) {
	root := writeFixture(t)
	_, corpus, err := Scan(root, "abc123")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range corpus.References {
		seen[r.Kind+" "+r.From+"->"+r.To] = true
	}
	for _, want := range []string{
		// A file contains the symbols declared in it.
		"CONTAINS " + fixtureFile(root, "lib/lib.go") + "->" + fixtureSymbol(root, "lib/lib.go", "Greet"),
		// A same-file call resolves to the full identity.
		"CALLS " + fixtureSymbol(root, "lib/lib.go", "Greet") + "->" + fixtureSymbol(root, "lib/lib.go", "decorate"),
		// A cross-package call through a package selector stays BARE.
		"CALLS " + fixtureSymbol(root, "app/app.go", "Run") + "->Greet",
		// An import edge is file-scoped and targets the raw import path.
		"IMPORTS_FROM " + fixtureFile(root, "app/app.go") + "->example.com/fixture/lib",
		// A test's callee is mirrored back, bare source and all: `Run` is
		// declared in app.go, not in the test file, so it never resolved.
		"TESTED_BY Run->" + fixtureSymbol(root, "app/app_test.go", "TestRun"),
	} {
		if !seen[want] {
			t.Errorf("missing reference %q; got %v", want, seen)
		}
	}
	for _, gone := range []string{
		"IMPORTS " + fixtureSymbol(root, "app/app.go", "Run") + "->" + fixtureSymbol(root, "lib/lib.go", "Config"),
	} {
		if seen[gone] {
			t.Errorf("reference %q must not be emitted: the release has no type-usage edge", gone)
		}
	}
}

func TestScanSkipsVendoredAndHiddenTrees(t *testing.T) {
	root := writeFixture(t)
	for _, dir := range []string{"vendor/pkg", ".hidden"} {
		path := filepath.Join(root, filepath.FromSlash(dir), "x.go")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package x\n\nfunc Hidden() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, pruned := range []string{"vendor/pkg/x.go", ".hidden/x.go"} {
		if _, ok := symbolKinds(t, root)[fixtureSymbol(root, pruned, "Hidden")]; ok {
			t.Errorf("symbol from pruned tree %q was ingested", pruned)
		}
	}
}

func TestScanSkipsUnparseableFile(t *testing.T) {
	root := writeFixture(t)
	if err := os.WriteFile(filepath.Join(root, "broken.go"), []byte("package !!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, _, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, f := range files {
		if f.RelPath == "broken.go" {
			t.Fatal("unparseable file must not produce an ingestion unit")
		}
	}
}

// TestScanIdentifiesRootFileSymbolsByPath replaces the old
// "root package uses the package name as its qualifier" case: there is no
// package qualifier in the identity any more, so a file at the repository root
// is named by its path like every other file.
func TestScanIdentifiesRootFileSymbolsByPath(t *testing.T) {
	root := writeFixture(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kinds := symbolKinds(t, root)
	if kind := kinds[fixtureSymbol(root, "main.go", "main")]; kind != kindFunction {
		t.Fatalf("root file symbol kind = %q, want Function", kind)
	}
	if _, ok := kinds["main.main"]; ok {
		t.Error("a package-qualified identity must not be emitted")
	}
}

func TestScanMethodsCarryReceiverName(t *testing.T) {
	root := writeFixture(t)
	body := "package lib\n\nfunc (c *Config) Label() string { return c.Name }\n"
	if err := os.WriteFile(filepath.Join(root, "lib", "method.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if kind := symbolKinds(t, root)[fixtureSymbol(root, "lib/method.go", "Config.Label")]; kind != kindFunction {
		t.Fatalf("method kind = %q, want Function", kind)
	}
}

func TestScanMissingRootErrors(t *testing.T) {
	if _, _, err := Scan(filepath.Join(t.TempDir(), "nope"), ""); err == nil {
		t.Fatal("want error scanning a missing root")
	}
}

func TestBuildReportCarriesFullBuildCounters(t *testing.T) {
	e := builtEngine(t, nil)
	report, err := e.BuildReport(graphstore.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if report.Status != statusOK || report.BuildType != buildTypeFull {
		t.Fatalf("report = %+v, want an ok full build", report)
	}
	if report.BaseResolved == nil || report.BaseResolved.Valid {
		t.Errorf("base_resolved = %#v, want a present null for a full build", report.BaseResolved)
	}
	if report.FilesParsed == nil || *report.FilesParsed == 0 ||
		report.TotalNodes == nil || *report.TotalNodes == 0 ||
		report.TotalEdges == nil || *report.TotalEdges == 0 {
		t.Fatalf("counters empty: %+v", report)
	}
	if report.Summary != graphstore.FullBuildSummary(*report.FilesParsed, *report.TotalNodes, *report.TotalEdges) {
		t.Errorf("summary = %q, want upstream's full-build sentence", report.Summary)
	}
	status, err := e.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Languages) == 0 || status.Languages[0] != languageGo {
		t.Errorf("languages = %v, want [go]", status.Languages)
	}
}

func TestBuildIsIdempotent(t *testing.T) {
	e := builtEngine(t, nil)
	first, err := e.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if err := e.Build(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	second, err := e.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if first.Nodes != second.Nodes || first.Edges != second.Edges {
		t.Fatalf("rebuild changed counts: %+v vs %+v", first, second)
	}
}

func TestBuildRemovesStaleFiles(t *testing.T) {
	e, root := newEngine(t, nil)
	if _, err := e.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "lib", "lib.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	nodes, err := e.ReadNodes(0)
	if err != nil {
		t.Fatalf("ReadNodes: %v", err)
	}
	stale := fixtureFile(root, "lib/lib.go")
	for _, n := range nodes {
		if n.FilePath == stale {
			t.Fatalf("stale node survived rebuild: %+v", n)
		}
	}
}

func TestStatusUnbuiltWithoutDatabase(t *testing.T) {
	e, _ := newEngine(t, nil)
	status, err := e.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != graphstore.CRGReadinessUnbuilt || status.Ready {
		t.Fatalf("status = %+v, want unbuilt", status)
	}
	if _, statErr := os.Stat(e.DBPath()); statErr == nil {
		t.Fatal("a read must not create the graph database")
	}
}

func TestUpdateNoDiffLeavesGraphUnchanged(t *testing.T) {
	e := builtEngine(t, nil)
	report, err := e.UpdateReport(graphstore.UpdateOptions{Base: "HEAD"})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.BuildType != buildTypeIncremental {
		t.Fatalf("build_type = %q, want %q", report.BuildType, buildTypeIncremental)
	}
	if report.FilesUpdated == nil || *report.FilesUpdated != 0 {
		t.Fatalf("files_updated = %v, want 0", report.FilesUpdated)
	}
	if report.Summary != graphstore.NoChangesSummary() {
		t.Fatalf("summary = %q, want %q", report.Summary, graphstore.NoChangesSummary())
	}
}

func TestUpdateReingestsChangedFile(t *testing.T) {
	e := builtEngine(t, nil)
	// Change the file's content so the hash check does not skip it.
	writeExtra(t, e.root, "lib/lib.go", "package lib\n\nfunc Greet() string { return \"hey\" }\n")
	e.changedFiles = func(string, string) ([]string, error) { return []string{"lib/lib.go"}, nil }

	report, err := e.UpdateReport(graphstore.UpdateOptions{Base: "HEAD"})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.FilesUpdated == nil || *report.FilesUpdated != 1 {
		t.Fatalf("files_updated = %v, want 1", report.FilesUpdated)
	}
	if report.ChangedFiles == nil || len(*report.ChangedFiles) != 1 {
		t.Fatalf("changed_files = %v, want one entry", report.ChangedFiles)
	}
	if report.TotalNodes == nil || *report.TotalNodes == 0 {
		t.Fatalf("total_nodes = %v, want the re-parsed file's rows", report.TotalNodes)
	}
}

func TestUpdateRemovesDeletedFile(t *testing.T) {
	e := builtEngine(t, nil)
	stale := fixtureFile(e.root, "lib/lib.go")
	if err := os.Remove(filepath.Join(e.root, "lib", "lib.go")); err != nil {
		t.Fatal(err)
	}
	e.changedFiles = func(string, string) ([]string, error) { return []string{"lib/lib.go"}, nil }

	report, err := e.UpdateReport(graphstore.UpdateOptions{Base: "HEAD"})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.FilesUpdated == nil || *report.FilesUpdated == 0 {
		t.Fatalf("files_updated = %v, want the purge counted", report.FilesUpdated)
	}
	nodes, err := e.ReadNodes(0)
	if err != nil {
		t.Fatalf("ReadNodes: %v", err)
	}
	for _, n := range nodes {
		if n.FilePath == stale {
			t.Fatalf("deleted file's rows survived the update: %+v", n)
		}
	}
}

func TestUpdatePropagatesGitError(t *testing.T) {
	// A populated graph is what keeps the update incremental; an empty one
	// escalates to a full rebuild and never asks for a diff.
	e := builtEngine(t, nil)
	e.changedFiles = func(string, string) ([]string, error) { return nil, os.ErrPermission }
	if _, err := e.UpdateReport(graphstore.UpdateOptions{Base: "HEAD"}); err == nil {
		t.Fatal("want git error propagated")
	}
}

// TestImpactRadiusReachesResolvedCallees walks the impact radius across the
// call edges the release actually resolves. `Greet -> decorate` is same-file
// and therefore resolved, so it propagates; `app.Run -> lib.Greet` goes
// through a package selector, which the release leaves as the BARE name
// `Greet`, so there is no edge into the changed symbol and the cross-package
// caller is NOT impacted. That is upstream's behaviour for Go, not a gap in
// the traversal: its own postprocess resolution needs the import target to
// name an indexed FILE, and a Go import target is a module path.
func TestImpactRadiusReachesResolvedCallees(t *testing.T) {
	e := builtEngine(t, nil)
	root := e.root
	result, err := e.GetImpactRadius(graphstore.ImpactOptions{
		ChangedFiles: []string{"lib/lib.go"}, MaxDepth: 3,
	})
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	if len(result.ChangedNodes) == 0 {
		t.Fatalf("no changed nodes: %+v", result)
	}
	// `decorate` lives in the changed FILE, so a file-seeded traversal reports
	// it as a CHANGED node, not an impacted one — computeImpactRadius
	// deliberately excludes seeds from ImpactedNodes. The edge is what is
	// being asserted here: without the resolved `Greet -> decorate` call the
	// symbol would not be reachable at all.
	if !containsQualified(result.ChangedNodes, fixtureSymbol(root, "lib/lib.go", "decorate")) {
		t.Fatalf("resolved same-file callee missing from the changed set: %+v", result.ChangedNodes)
	}
	if containsQualified(result.ImpactedNodes, fixtureSymbol(root, "lib/lib.go", "decorate")) {
		t.Fatalf("a seed symbol must not also be reported as impacted: %+v", result.ImpactedNodes)
	}
	if containsQualified(result.ImpactedNodes, fixtureSymbol(root, "app/app.go", "Run")) {
		t.Fatalf("cross-package caller reached: the release leaves `auth.Greet`-style "+
			"targets bare, so no such edge exists: %+v", result.ImpactedNodes)
	}
}

// containsQualified reports whether nodes contain the given qualified name.
func containsQualified(nodes []graphstore.ImpactNode, qual string) bool {
	for _, n := range nodes {
		if n.QualifiedName == qual {
			return true
		}
	}
	return false
}

func TestImpactRadiusOnUnbuiltGraphIsEmpty(t *testing.T) {
	e, _ := newEngine(t, nil)
	result, err := e.GetImpactRadius(graphstore.ImpactOptions{ChangedFiles: []string{"lib/lib.go"}})
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	if len(result.ChangedNodes) != 0 || result.Summary == "" {
		t.Fatalf("want empty, summarised result, got %+v", result)
	}
}

func TestImpactRadiusFallsBackToGitDiff(t *testing.T) {
	e := builtEngine(t, []string{"lib/lib.go"})
	result, err := e.GetImpactRadius(graphstore.ImpactOptions{})
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	if len(result.ChangedFiles) != 1 || result.ChangedFiles[0] != "lib/lib.go" {
		t.Fatalf("changed files = %v", result.ChangedFiles)
	}
}

func TestListFlowsRanksByCriticality(t *testing.T) {
	e := builtEngine(t, nil)
	result, err := e.ListFlows(0, "")
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(result.Flows) == 0 {
		t.Fatal("no flows derived")
	}
	for i := 1; i < len(result.Flows); i++ {
		if result.Flows[i-1].Criticality < result.Flows[i].Criticality {
			t.Fatalf("flows not ordered by criticality: %+v", result.Flows)
		}
	}
	if result.Flows[0].Kind != "call_flow" || result.Flows[0].StepCount == 0 {
		t.Fatalf("unexpected flow shape: %+v", result.Flows[0])
	}
}

func TestListFlowsSortByNameAndLimit(t *testing.T) {
	e := builtEngine(t, nil)
	result, err := e.ListFlows(1, "name")
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(result.Flows) != 1 {
		t.Fatalf("limit ignored: %+v", result.Flows)
	}
}

func TestListFlowsUnbuiltGraphIsEmpty(t *testing.T) {
	e, _ := newEngine(t, nil)
	result, err := e.ListFlows(0, "")
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(result.Flows) != 0 {
		t.Fatalf("want no flows, got %+v", result.Flows)
	}
}

func TestListCommunitiesGroupsConnectedSymbols(t *testing.T) {
	e := builtEngine(t, nil)
	result, err := e.ListCommunities(2, "")
	if err != nil {
		t.Fatalf("ListCommunities: %v", err)
	}
	if len(result.Communities) == 0 {
		t.Fatal("no communities derived")
	}
	top := result.Communities[0]
	if top.Size < 2 || top.DominantLanguage != languageGo || len(top.Members) != top.Size {
		t.Fatalf("unexpected community: %+v", top)
	}
}

func TestListCommunitiesAlternateSorts(t *testing.T) {
	e := builtEngine(t, nil)
	for _, sortBy := range []string{"cohesion", "name", "size"} {
		if _, err := e.ListCommunities(0, sortBy); err != nil {
			t.Fatalf("ListCommunities(%s): %v", sortBy, err)
		}
	}
}

func TestPostprocessReportsDerivedCounts(t *testing.T) {
	e := builtEngine(t, nil)
	report, err := e.PostprocessReport(graphstore.PostprocessOptions{})
	if err != nil {
		t.Fatalf("PostprocessReport: %v", err)
	}
	if report.SignaturesUpdated == nil || !*report.SignaturesUpdated {
		t.Error("signatures_updated missing from a full post-process")
	}
	for name, got := range map[string]*int{
		"fts_indexed":          report.FTSIndexed,
		"flows_detected":       report.FlowsDetected,
		"communities_detected": report.CommunitiesDetected,
	} {
		if got == nil {
			t.Errorf("%s missing from a full post-process", name)
		}
	}
	store, err := e.readStore()
	if err != nil || store == nil {
		t.Fatalf("readStore: %v", err)
	}
	stamp, err := store.GetMetadata(metaLastPostprocessed)
	if err != nil || stamp == "" {
		t.Errorf("metadata[%s] = %q (err %v), want a timestamp", metaLastPostprocessed, stamp, err)
	}
}

func TestPostprocessHonorsSkipFlags(t *testing.T) {
	e := builtEngine(t, nil)
	report, err := e.PostprocessReport(graphstore.PostprocessOptions{
		Flows:       new(false),
		Communities: new(false),
		FTS:         new(false),
	})
	if err != nil {
		t.Fatalf("PostprocessReport: %v", err)
	}
	for name, got := range map[string]*int{
		"fts_indexed":          report.FTSIndexed,
		"flows_detected":       report.FlowsDetected,
		"communities_detected": report.CommunitiesDetected,
	} {
		if got != nil {
			t.Errorf("%s = %d although its step was disabled", name, *got)
		}
	}
}

func TestDetectChangesReportsRiskAndGaps(t *testing.T) {
	e := builtEngine(t, []string{"lib/lib.go"})
	report, err := e.DetectChanges(graphstore.DetectChangesOptions{})
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	if len(report.ChangedFunctions) == 0 {
		t.Fatalf("no changed functions: %+v", report)
	}
	if !hasTestGap(report.TestGaps, fixtureSymbol(e.root, "lib/lib.go", "decorate")) {
		t.Errorf("untested helper missing from test gaps: %+v", report.TestGaps)
	}
	if len(report.ReviewPriorities) == 0 || report.Summary == "" {
		t.Fatalf("incomplete report: %+v", report)
	}
}

// hasTestGap reports whether gaps name the given qualified symbol.
func hasTestGap(gaps []graphstore.CRGTestGap, qual string) bool {
	for _, g := range gaps {
		if g.QualifiedName == qual {
			return true
		}
	}
	return false
}

func TestDetectChangesBriefReturnsSummaryOnly(t *testing.T) {
	e := builtEngine(t, []string{"lib/lib.go"})
	report, err := e.DetectChanges(graphstore.DetectChangesOptions{Brief: true})
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	if report.Summary == "" || len(report.ChangedFunctions) != 0 {
		t.Fatalf("brief report should carry only a summary: %+v", report)
	}
}

func TestDetectChangesFindsAffectedFlows(t *testing.T) {
	e := builtEngine(t, nil)
	report, err := e.DetectChanges(graphstore.DetectChangesOptions{Files: []string{"lib/lib.go"}})
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	if len(report.AffectedFlows) == 0 {
		t.Fatalf("no affected flows: %+v", report)
	}
	if report.RiskScore <= 0 {
		t.Fatalf("risk score = %v, want > 0", report.RiskScore)
	}
}

func TestReadNodesAndEdgesRespectLimit(t *testing.T) {
	e := builtEngine(t, nil)
	nodes, err := e.ReadNodes(2)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("ReadNodes(2) = %d nodes, err %v", len(nodes), err)
	}
	edges, err := e.ReadEdges(1)
	if err != nil || len(edges) != 1 {
		t.Fatalf("ReadEdges(1) = %d edges, err %v", len(edges), err)
	}
}

func TestReadNodesAndEdgesUnbuiltAreEmpty(t *testing.T) {
	e, _ := newEngine(t, nil)
	nodes, err := e.ReadNodes(0)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("ReadNodes = %d, err %v", len(nodes), err)
	}
	edges, err := e.ReadEdges(0)
	if err != nil || len(edges) != 0 {
		t.Fatalf("ReadEdges = %d, err %v", len(edges), err)
	}
}

func TestOpenDefaultsToCurrentDirectory(t *testing.T) {
	e := Open("")
	defer e.Close() //nolint:errcheck
	if e.root != "." {
		t.Fatalf("root = %q, want .", e.root)
	}
	if !strings.HasSuffix(filepath.ToSlash(e.DBPath()), "code-graph.db") {
		t.Fatalf("db path = %q", e.DBPath())
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	e := builtEngine(t, nil)
	if err := e.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
