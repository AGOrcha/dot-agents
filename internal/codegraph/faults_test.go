package codegraph

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ── build / update failure arms ──────────────────────────────────────────────

func TestBuildReportFailsWhenStoreCannotOpen(t *testing.T) {
	e := unopenableEngine(t)
	if _, err := e.BuildReport(graphstore.BuildOptions{}); err == nil {
		t.Fatal("want an open failure")
	}
}

func TestBuildReportFailsWhenScanRootMissing(t *testing.T) {
	e := Open(filepath.Join(t.TempDir(), "missing"))
	t.Cleanup(func() { _ = e.Close() })
	if _, err := e.BuildReport(graphstore.BuildOptions{}); err == nil {
		t.Fatal("want a scan failure")
	}
}

func TestBuildReportPropagatesStoreFailures(t *testing.T) {
	root := writeFixture(t)
	cases := map[string]*fakeStore{
		"file enumeration": {filesErr: errFake},
		"node write":       {writeErr: errFake},
		"metadata write":   {metaErr: errFake},
		"stale removal":    {files: []string{"gone.go"}, removeErr: errFake},
	}
	for name, store := range cases {
		e := engineWithStore(t, root, store)
		if _, err := e.BuildReport(graphstore.BuildOptions{Postprocess: graphstore.PostprocessNone}); !errors.Is(err, errFake) {
			t.Errorf("%s: err = %v, want the injected failure", name, err)
		}
	}
}

func TestUpdateReportPropagatesFailures(t *testing.T) {
	root := writeFixture(t)
	// A populated graph is what keeps the update on the incremental path;
	// an empty one escalates to a full rebuild before reaching these arms.
	// The stored file must be the graph's ABSOLUTE spelling under root, or
	// the foreign-root guard fires before the injected failure can.
	indexed := Open(root).absPath("lib/lib.go")
	populated := func(store *fakeStore) *fakeStore {
		store.stats = graphstore.GraphStats{TotalNodes: 1, FilesCount: 1}
		store.files = append(store.files, indexed)
		return store
	}
	cases := map[string]*fakeStore{
		"node write":     populated(&fakeStore{writeErr: errFake}),
		"metadata write": populated(&fakeStore{metaErr: errFake}),
		"stats read":     {statsErr: errFake},
	}
	for name, store := range cases {
		e := engineWithStore(t, root, store)
		if _, err := e.UpdateReport(graphstore.UpdateOptions{
			Base:        "HEAD",
			Postprocess: graphstore.PostprocessNone,
		}); !errors.Is(err, errFake) {
			t.Errorf("%s: err = %v, want the injected failure", name, err)
		}
	}
}

func TestUpdateReportFailsWhenStoreCannotOpen(t *testing.T) {
	e := unopenableEngine(t)
	if _, err := e.UpdateReport(graphstore.UpdateOptions{}); err == nil {
		t.Fatal("want an open failure")
	}
}

func TestUpdateReportPropagatesDiffFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{
		stats: graphstore.GraphStats{TotalNodes: 1, FilesCount: 1},
	})
	e.changedFiles = func(string, string) ([]string, error) { return nil, errFake }
	if _, err := e.UpdateReport(graphstore.UpdateOptions{Base: "HEAD"}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected diff failure", err)
	}
}

// TestIncrementalRefusesAForeignGraph covers the guard that stops an
// incremental reconciliation from deleting a graph built somewhere else: every
// stored file would look stale, so the mismatch must be reported instead.
func TestIncrementalRefusesAForeignGraph(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{
		stats: graphstore.GraphStats{TotalNodes: 1, FilesCount: 1},
		files: []string{"/somewhere/else/lib.go"},
	})
	_, err := e.UpdateReport(graphstore.UpdateOptions{Base: "HEAD"})
	if err == nil || !strings.Contains(err.Error(), "different repository root") {
		t.Fatalf("err = %v, want a foreign-root refusal", err)
	}
}

// ── status classification ────────────────────────────────────────────────────

func TestApplyStatsMarksUnbuiltWithoutTimestamp(t *testing.T) {
	status := &graphstore.CRGStatus{}
	applyStats(status, graphstore.GraphStats{TotalNodes: 3, FilesCount: 1})
	if status.State != graphstore.CRGReadinessUnbuilt || status.Message == "" {
		t.Fatalf("status = %+v, want unbuilt with a message", status)
	}
	if status.LastUpdated != nil {
		t.Errorf("last_updated = %q, want null without a build timestamp", *status.LastUpdated)
	}
}

func TestApplyStatsSortsLanguages(t *testing.T) {
	status := &graphstore.CRGStatus{}
	applyStats(status, graphstore.GraphStats{
		TotalNodes: 3, FilesCount: 1, LastUpdated: "2024-01-01T00:00:00",
		Languages: []string{"python", "go"},
	})
	if !status.Ready || status.Languages[0] != "go" || status.Languages[1] != "python" {
		t.Fatalf("status = %+v, want a ready graph with sorted languages", status)
	}
}

// ── status / store failure arms ──────────────────────────────────────────────

func TestStatusReportsStoreOpenFailure(t *testing.T) {
	status, err := unreadableEngine(t).Status()
	if err != nil {
		t.Fatalf("Status must not error: %v", err)
	}
	if status.State != graphstore.CRGReadinessError || status.Message == "" {
		t.Fatalf("status = %+v, want an error state carrying the reason", status)
	}
}

func TestStatusReportsStatsFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{statsErr: errFake})
	status, err := e.Status()
	if err != nil {
		t.Fatalf("Status must not error: %v", err)
	}
	if status.State != graphstore.CRGReadinessError {
		t.Fatalf("status = %+v, want an error state", status)
	}
}

func TestReadPathsDegradeOnStoreOpenFailure(t *testing.T) {
	e := unreadableEngine(t)
	if _, err := e.ReadNodes(0); err == nil {
		t.Error("ReadNodes: want the open failure")
	}
	if _, err := e.ReadEdges(0); err == nil {
		t.Error("ReadEdges: want the open failure")
	}
	if _, err := e.ListFlows(0, ""); err == nil {
		t.Error("ListFlows: want the open failure")
	}
	if _, err := e.GetImpactRadius(graphstore.ImpactOptions{ChangedFiles: []string{"a.go"}}); err == nil {
		t.Error("GetImpactRadius: want the open failure")
	}
	if err := e.Postprocess(graphstore.PostprocessOptions{}); err == nil {
		t.Error("Postprocess: want the open failure")
	}
}

func TestReadPathsPropagateStoreFailures(t *testing.T) {
	root := writeFixture(t)
	if _, err := engineWithStore(t, root, &fakeStore{allNodesErr: errFake}).ReadNodes(0); !errors.Is(err, errFake) {
		t.Errorf("node read: err = %v", err)
	}
	if _, err := engineWithStore(t, root, &fakeStore{allEdgesErr: errFake}).ReadEdges(0); !errors.Is(err, errFake) {
		t.Errorf("edge read: err = %v", err)
	}
	if _, err := engineWithStore(t, root, &fakeStore{communitiesErr: errFake}).ListCommunities(0, ""); !errors.Is(err, errFake) {
		t.Errorf("communities read: err = %v", err)
	}
	if _, err := engineWithStore(t, root, &fakeStore{flowsErr: errFake}).ListFlows(0, ""); !errors.Is(err, errFake) {
		t.Errorf("flows read: err = %v", err)
	}
}

func TestImpactRadiusPropagatesStoreFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{impactErr: errFake})
	_, err := e.GetImpactRadius(graphstore.ImpactOptions{ChangedFiles: []string{"lib/lib.go"}})
	if !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestImpactRadiusPropagatesDiffFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{})
	e.changedFiles = func(string, string) ([]string, error) { return nil, errFake }
	if _, err := e.GetImpactRadius(graphstore.ImpactOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected diff failure", err)
	}
}

func TestDetectChangesPropagatesDiffFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{})
	e.changedFiles = func(string, string) ([]string, error) { return nil, errFake }
	if _, err := e.DetectChanges(graphstore.DetectChangesOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected diff failure", err)
	}
}

// ── post-process failure arms ────────────────────────────────────────────────

func TestPostprocessPropagatesMetadataFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{metaErr: errFake})
	if err := e.Postprocess(graphstore.PostprocessOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected metadata failure", err)
	}
}

// TestPostprocessOnUnbuiltGraphIsNoOp covers the soft arm: a post-process
// against a repository with no graph is nothing to do, not a failure.
func TestPostprocessOnUnbuiltGraphIsNoOp(t *testing.T) {
	e := Open(writeFixture(t))
	t.Cleanup(func() { _ = e.Close() })
	report, err := e.PostprocessReport(graphstore.PostprocessOptions{})
	if err != nil {
		t.Fatalf("PostprocessReport on an unbuilt graph: %v", err)
	}
	if report.Status != statusOK || report.Summary == "" {
		t.Fatalf("report = %+v, want a clean no-op result", report)
	}
}

// ── small pure helpers ───────────────────────────────────────────────────────

func TestSecondsSinceIsNeverNegative(t *testing.T) {
	if got := secondsSince(time.Now().Add(time.Hour)); got != 0 {
		t.Fatalf("secondsSince(future) = %v, want 0", got)
	}
	if got := secondsSince(time.Now().Add(-2 * time.Second)); got < 1.9 {
		t.Fatalf("secondsSince(2s ago) = %v, want about 2", got)
	}
}

func TestRelPathKeepsPathsOutsideTheRepository(t *testing.T) {
	e := Open(t.TempDir())
	t.Cleanup(func() { _ = e.Close() })
	outside := "/somewhere/else/lib.go"
	if got := e.relPath(outside); got != outside {
		t.Fatalf("relPath(%q) = %q, want the absolute path kept", outside, got)
	}
	inside := e.absPath("pkg/lib.go")
	if got := e.relPath(inside); got != "pkg/lib.go" {
		t.Fatalf("relPath(%q) = %q, want the repo-relative path", inside, got)
	}
}

func TestTruncateReturnsEverythingForNonPositiveLimit(t *testing.T) {
	items := []int{1, 2, 3}
	if got := truncate(items, 0); len(got) != 3 {
		t.Fatalf("truncate(0) = %v, want everything", got)
	}
	if got := truncate(items, -1); len(got) != 3 {
		t.Fatalf("truncate(-1) = %v, want everything", got)
	}
}

func TestHashNodeFallsBackOnOutOfRangeOffsets(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", "package x\n\nfunc F() {}\n", parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Hash against a shorter buffer than the one the positions came from.
	if got := hashNode(fset, []byte("p"), file.Decls[0]); got != hashBytes(nil) {
		t.Fatalf("hashNode = %q, want the empty-content hash", got)
	}
}

// TestScanImportEdgesUseRawPathForEveryImportForm replaces the old
// `importAlias` unit test. Import aliases no longer participate in identity at
// all: the release emits one file-scoped IMPORTS_FROM edge per import spec
// whose target is the RAW import path, so an implicit, aliased, blank or dot
// import are indistinguishable in the graph.
func TestScanImportEdgesUseRawPathForEveryImportForm(t *testing.T) {
	root := writeFixture(t)
	writeExtra(t, root, "app/imports.go", `package app

import (
	"strings"
	str "strings"
	_ "example.com/fixture/lib"
	. "errors"
)

func Forms() string { return strings.TrimSpace(str.ToLower("x")) }
`)
	_, corpus, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	file := fixtureFile(root, "app/imports.go")
	imports := map[string]bool{}
	for _, ref := range corpus.References {
		if ref.Kind == edgeImportsFrom && ref.From == file {
			imports[ref.To] = true
		}
	}
	for _, want := range []string{"strings", "example.com/fixture/lib", "errors"} {
		if !imports[want] {
			t.Errorf("missing IMPORTS_FROM target %q; got %v", want, imports)
		}
	}
	if imports["str"] || imports["lib"] {
		t.Errorf("an import alias leaked into an edge target: %v", imports)
	}
}

func TestBaseTypeNameUnwrapsPointersAndGenerics(t *testing.T) {
	ident := ast.NewIdent("Box")
	cases := map[string]ast.Expr{
		"ident":     ident,
		"pointer":   &ast.StarExpr{X: ident},
		"generic":   &ast.IndexExpr{X: ident, Index: ast.NewIdent("T")},
		"generic2":  &ast.IndexListExpr{X: ident, Indices: []ast.Expr{ast.NewIdent("T")}},
		"unrelated": &ast.BasicLit{},
	}
	for name, expr := range cases {
		want := "Box"
		if name == "unrelated" {
			want = ""
		}
		if got := baseTypeName(expr); got != want {
			t.Errorf("%s: baseTypeName = %q, want %q", name, got, want)
		}
	}
}

// ── scanner resolution edge cases ────────────────────────────────────────────

// TestScanNeverResolvesBareNameAcrossFiles replaces the old
// "ignores AMBIGUOUS bare names" case. The release does not resolve a bare
// name across files at all — not even a unique one — so uniqueness stopped
// being the deciding factor: `Greet` calling `decorate` resolves because both
// are in lib.go, and a same-named `decorate` elsewhere changes nothing.
func TestScanNeverResolvesBareNameAcrossFiles(t *testing.T) {
	root := writeFixture(t)
	writeExtra(t, root, "lib/split.go", "package lib\n\nfunc Split() string { return decorate(\"x\") }\n")
	writeExtra(t, root, "other/other.go", "package other\n\nfunc decorate(s string) string { return s }\n")
	_, corpus, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var splitTargets []string
	for _, ref := range corpus.References {
		if ref.Kind == edgeCalls && ref.From == fixtureSymbol(root, "lib/split.go", "Split") {
			splitTargets = append(splitTargets, ref.To)
		}
	}
	// `decorate` lives in lib/lib.go — the same PACKAGE but a different FILE.
	if len(splitTargets) != 1 || splitTargets[0] != "decorate" {
		t.Fatalf("cross-file CALLS targets = %v, want exactly [decorate] (bare)", splitTargets)
	}
	// No CALL anywhere may land on the same-named symbol in the other
	// package. The containment edge from that file to its own `decorate` is
	// not a resolution, so it is excluded rather than asserted against.
	for _, ref := range corpus.References {
		if ref.Kind == edgeCalls && ref.To == fixtureSymbol(root, "other/other.go", "decorate") {
			t.Fatalf("a bare name resolved into another package: %+v", ref)
		}
	}
}

// TestScanLeavesAliasedImportCallsBare pins the other half of the import
// change: an aliased import is not a resolution channel, so a call through the
// alias keeps the bare member name exactly as a call through the package name
// does.
func TestScanLeavesAliasedImportCallsBare(t *testing.T) {
	root := writeFixture(t)
	writeExtra(t, root, "app/alias.go", `package app

import (
	_ "example.com/fixture/lib"
	l "example.com/fixture/lib"
)

func AliasRun() string { return l.Greet(l.Config{}) }
`)
	_, corpus, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	caller := fixtureSymbol(root, "app/alias.go", "AliasRun")
	var targets []string
	for _, ref := range corpus.References {
		if ref.Kind == edgeCalls && ref.From == caller {
			targets = append(targets, ref.To)
		}
	}
	if len(targets) != 1 || targets[0] != "Greet" {
		t.Fatalf("aliased-import CALLS targets = %v, want exactly [Greet] (bare)", targets)
	}
}

func TestScanSkipsUnreadableFile(t *testing.T) {
	root := writeFixture(t)
	// A directory named like a Go file is unreadable as a source file, which is
	// the read-failure arm scanFile must skip rather than fail on.
	writeExtra(t, root, "weird.go/keep.txt", "x")
	files, _, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, f := range files {
		if f.RelPath == "weird.go" {
			t.Fatal("unreadable path must not produce an ingestion unit")
		}
	}
}

// ── git helpers against a real repository ────────────────────────────────────

// initGitRepo creates a one-commit git repository from the fixture and returns
// its root, skipping the test when git is unavailable.
func initGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := writeFixture(t)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "e2e@example.com"},
		{"config", "user.name", "e2e"},
		{"add", "-A"},
		{"commit", "-qm", "fixture"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v failed: %v (%s)", args, err, out)
		}
	}
	return root
}

// TestResolveIncrementalBaseRejectsAnAnchorlessGitRepo covers the arm that
// forces a full rebuild: a git repository whose graph records no
// last-built commit has no usable diff base, and substituting HEAD~1 would
// report a stale graph as up to date.
func TestResolveIncrementalBaseRejectsAnAnchorlessGitRepo(t *testing.T) {
	root := initGitRepo(t)
	e := Open(root)
	t.Cleanup(func() { _ = e.Close() })
	base, ok, err := e.resolveIncrementalBase(&fakeStore{})
	if err != nil {
		t.Fatalf("resolveIncrementalBase: %v", err)
	}
	if ok || base != "" {
		t.Fatalf("resolveIncrementalBase = (%q, %v), want no usable base", base, ok)
	}
}

// TestResolveIncrementalBaseUsesTheStoredAnchor covers the happy arm.
func TestResolveIncrementalBaseUsesTheStoredAnchor(t *testing.T) {
	root := initGitRepo(t)
	e := Open(root)
	t.Cleanup(func() { _ = e.Close() })
	head := headCommit(root)
	base, ok, err := e.resolveIncrementalBase(&fakeStore{meta: map[string]string{metaGitHeadSHA: head}})
	if err != nil {
		t.Fatalf("resolveIncrementalBase: %v", err)
	}
	if !ok || base != head {
		t.Fatalf("resolveIncrementalBase = (%q, %v), want (%q, true)", base, ok, head)
	}
}

func TestHeadCommitResolvesInsideRepo(t *testing.T) {
	if got := headCommit(initGitRepo(t)); len(got) < 7 {
		t.Fatalf("headCommit = %q, want a sha", got)
	}
}

func TestHeadCommitOutsideRepositoryIsEmpty(t *testing.T) {
	if got := headCommit(t.TempDir()); got != "" {
		t.Fatalf("headCommit = %q, want empty outside a git repository", got)
	}
}
