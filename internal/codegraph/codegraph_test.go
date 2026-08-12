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

func TestScanDeclaresFunctionsAndTypes(t *testing.T) {
	root := writeFixture(t)
	kinds := symbolKinds(t, root)
	want := map[string]string{
		"lib.Config":   kindType,
		"lib.Greet":    kindFunction,
		"lib.decorate": kindFunction,
		"app.Run":      kindFunction,
		"app.TestRun":  kindFunction,
	}
	for qual, kind := range want {
		if kinds[qual] != kind {
			t.Errorf("symbol %q kind = %q, want %q", qual, kinds[qual], kind)
		}
	}
}

func TestScanResolvesCallImportAndTestEdges(t *testing.T) {
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
		"CALLS lib.Greet->lib.decorate",
		"CALLS app.Run->lib.Greet",
		"IMPORTS app.Run->lib.Config",
		"TESTED_BY app.Run->app.TestRun",
	} {
		if !seen[want] {
			t.Errorf("missing reference %q; got %v", want, seen)
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
	if _, ok := symbolKinds(t, root)["vendor/pkg.Hidden"]; ok {
		t.Error("vendored symbol was ingested")
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
		if f.Path == "broken.go" {
			t.Fatal("unparseable file must not produce an ingestion unit")
		}
	}
}

func TestScanRootPackageUsesPackageName(t *testing.T) {
	root := writeFixture(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if kind := symbolKinds(t, root)["main.main"]; kind != kindFunction {
		t.Fatalf("root package symbol kind = %q, want Function", kind)
	}
}

func TestScanMethodsCarryReceiverName(t *testing.T) {
	root := writeFixture(t)
	body := "package lib\n\nfunc (c *Config) Label() string { return c.Name }\n"
	if err := os.WriteFile(filepath.Join(root, "lib", "method.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if kind := symbolKinds(t, root)["lib.Config.Label"]; kind != kindFunction {
		t.Fatalf("method kind = %q, want Function", kind)
	}
}

func TestScanMissingRootErrors(t *testing.T) {
	if _, _, err := Scan(filepath.Join(t.TempDir(), "nope"), ""); err == nil {
		t.Fatal("want error scanning a missing root")
	}
}

func TestBuildReportPopulatesStatus(t *testing.T) {
	e := builtEngine(t, nil)
	report, err := e.BuildReport(graphstore.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if report.Outcome != graphstore.CRGReadinessReady {
		t.Fatalf("outcome = %q, want ready (%s)", report.Outcome, report.Summary)
	}
	if report.Status.Nodes == 0 || report.Status.Edges == 0 || report.Status.Files == 0 {
		t.Fatalf("status counts empty: %+v", report.Status)
	}
	if !strings.Contains(report.Status.Languages, languageGo) {
		t.Errorf("languages = %q, want go", report.Status.Languages)
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
	for _, n := range nodes {
		if n.FilePath == "lib/lib.go" {
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
	report, err := e.UpdateReport(graphstore.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.Outcome != "no_diff" {
		t.Fatalf("outcome = %q, want no_diff", report.Outcome)
	}
}

func TestUpdateReingestsChangedFile(t *testing.T) {
	e := builtEngine(t, []string{"lib/lib.go"})
	report, err := e.UpdateReport(graphstore.UpdateOptions{Base: "HEAD~1"})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.Outcome != "updated" {
		t.Fatalf("outcome = %q, want updated", report.Outcome)
	}
	if len(report.ChangedFiles) != 1 {
		t.Fatalf("changed files = %v", report.ChangedFiles)
	}
}

func TestUpdateRemovesDeletedFile(t *testing.T) {
	e := builtEngine(t, []string{"lib/lib.go"})
	root := e.root
	if err := os.Remove(filepath.Join(root, "lib", "lib.go")); err != nil {
		t.Fatal(err)
	}
	report, err := e.UpdateReport(graphstore.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.Outcome != "no_mutation" {
		t.Fatalf("outcome = %q, want no_mutation", report.Outcome)
	}
	if err := e.Update(graphstore.UpdateOptions{}); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func TestUpdatePropagatesGitError(t *testing.T) {
	e, _ := newEngine(t, nil)
	e.changedFiles = func(string, string) ([]string, error) { return nil, os.ErrPermission }
	if _, err := e.UpdateReport(graphstore.UpdateOptions{}); err == nil {
		t.Fatal("want git error propagated")
	}
}

func TestImpactRadiusReachesCallers(t *testing.T) {
	e := builtEngine(t, nil)
	result, err := e.GetImpactRadius(graphstore.ImpactOptions{ChangedFiles: []string{"lib/lib.go"}, MaxDepth: 3})
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	if len(result.ChangedNodes) == 0 {
		t.Fatalf("no changed nodes: %+v", result)
	}
	if !containsQualified(result.ImpactedNodes, "app.Run") {
		t.Fatalf("app.Run not in impact radius: %+v", result.ImpactedNodes)
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

func TestPostprocessRecordsDerivedCounts(t *testing.T) {
	e := builtEngine(t, nil)
	if err := e.Postprocess(graphstore.PostprocessOptions{}); err != nil {
		t.Fatalf("Postprocess: %v", err)
	}
	store, err := e.readStore()
	if err != nil || store == nil {
		t.Fatalf("readStore: %v", err)
	}
	for _, key := range []string{"last_postprocess", "flow_memberships", "communities", "fts_tokens"} {
		v, gerr := store.GetMetadata(key)
		if gerr != nil || v == "" {
			t.Errorf("metadata %q = %q (err %v)", key, v, gerr)
		}
	}
}

func TestPostprocessHonorsSkipFlags(t *testing.T) {
	e := builtEngine(t, nil)
	if err := e.Postprocess(graphstore.PostprocessOptions{NoFlows: true, NoCommunities: true, NoFTS: true}); err != nil {
		t.Fatalf("Postprocess: %v", err)
	}
	store, err := e.readStore()
	if err != nil || store == nil {
		t.Fatalf("readStore: %v", err)
	}
	if v, _ := store.GetMetadata("flow_memberships"); v != "" {
		t.Fatalf("flow_memberships written despite NoFlows: %q", v)
	}
}

func TestPostprocessUnbuiltIsNoOp(t *testing.T) {
	e, _ := newEngine(t, nil)
	if err := e.Postprocess(graphstore.PostprocessOptions{}); err != nil {
		t.Fatalf("Postprocess on unbuilt graph: %v", err)
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
	if !hasTestGap(report.TestGaps, "lib.decorate") {
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

func TestGitChangedFilesReportsError(t *testing.T) {
	if _, err := gitChangedFiles(t.TempDir(), "HEAD~1"); err == nil {
		t.Fatal("want error outside a git repository")
	}
}

func TestHeadCommitOutsideRepoIsEmpty(t *testing.T) {
	if got := headCommit(t.TempDir()); got != "" {
		t.Fatalf("headCommit = %q, want empty outside a repo", got)
	}
}
