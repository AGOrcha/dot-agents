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
		if _, err := e.BuildReport(graphstore.BuildOptions{}); !errors.Is(err, errFake) {
			t.Errorf("%s: err = %v, want the injected failure", name, err)
		}
	}
}

func TestBuildReportPropagatesStatusFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{})
	e.status = func() (*graphstore.CRGStatus, error) { return nil, errFake }
	if _, err := e.BuildReport(graphstore.BuildOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected status failure", err)
	}
}

func TestUpdateReportPropagatesFailures(t *testing.T) {
	root := writeFixture(t)
	e := engineWithStore(t, root, &fakeStore{writeErr: errFake})
	if _, err := e.UpdateReport(graphstore.UpdateOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("write err = %v, want the injected failure", err)
	}
	e = engineWithStore(t, root, &fakeStore{metaErr: errFake})
	if _, err := e.UpdateReport(graphstore.UpdateOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("metadata err = %v, want the injected failure", err)
	}
	e = engineWithStore(t, root, &fakeStore{})
	e.status = func() (*graphstore.CRGStatus, error) { return nil, errFake }
	if _, err := e.UpdateReport(graphstore.UpdateOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("status err = %v, want the injected failure", err)
	}
}

func TestUpdateReportFailsWhenScanRootMissing(t *testing.T) {
	e := Open(filepath.Join(t.TempDir(), "missing"))
	e.changedFiles = func(string, string) ([]string, error) { return []string{"a.go"}, nil }
	t.Cleanup(func() { _ = e.Close() })
	if _, err := e.UpdateReport(graphstore.UpdateOptions{}); err == nil {
		t.Fatal("want a scan failure")
	}
}

func TestUpdateReportFailsWhenStoreCannotOpen(t *testing.T) {
	e := unopenableEngine(t)
	if _, err := e.UpdateReport(graphstore.UpdateOptions{}); err == nil {
		t.Fatal("want an open failure")
	}
}

func TestNoDiffReportPropagatesStatusFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{})
	e.changedFiles = func(string, string) ([]string, error) { return nil, nil }
	e.status = func() (*graphstore.CRGStatus, error) { return nil, errFake }
	if _, err := e.UpdateReport(graphstore.UpdateOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected status failure", err)
	}
}

func TestApplyChangedFilesPropagatesRemoveFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{removeErr: errFake})
	e.changedFiles = func(string, string) ([]string, error) { return []string{"deleted.go"}, nil }
	if _, err := e.UpdateReport(graphstore.UpdateOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected remove failure", err)
	}
}

// ── report classification ────────────────────────────────────────────────────

func TestBuildOutcomeReportClassifiesEveryState(t *testing.T) {
	cases := []struct {
		status *graphstore.CRGStatus
		want   string
	}{
		{&graphstore.CRGStatus{Ready: true, State: graphstore.CRGReadinessReady}, graphstore.CRGReadinessReady},
		{&graphstore.CRGStatus{State: graphstore.CRGReadinessBusyOrLocked}, graphstore.CRGReadinessBusyOrLocked},
		{&graphstore.CRGStatus{State: graphstore.CRGReadinessUnbuilt}, graphstore.CRGReadinessUnbuilt},
		{&graphstore.CRGStatus{State: graphstore.CRGReadinessError, Message: "boom"}, graphstore.CRGReadinessError},
	}
	for _, tc := range cases {
		got := buildOutcomeReport("build", tc.status, "ready")
		if got.Outcome != tc.want || got.Summary == "" {
			t.Errorf("state %q -> %+v, want outcome %q with a summary", tc.status.State, got, tc.want)
		}
	}
}

func TestApplyStatsMarksUnbuiltWithoutTimestamp(t *testing.T) {
	status := &graphstore.CRGStatus{LastUpdated: "never"}
	applyStats(status, graphstore.GraphStats{TotalNodes: 3, FilesCount: 1})
	if status.State != graphstore.CRGReadinessUnbuilt || status.Message == "" {
		t.Fatalf("status = %+v, want unbuilt with a message", status)
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

func TestReadPathsPropagateEnumerationFailures(t *testing.T) {
	root := writeFixture(t)
	nodesFake := &fakeStore{files: []string{"lib/lib.go"}, nodesErr: errFake}
	edgesFake := &fakeStore{
		files:    []string{"lib/lib.go"},
		nodes:    map[string][]graphstore.GraphNode{"lib/lib.go": {{Kind: kindFunction, QualifiedName: "lib.Greet", FilePath: "lib/lib.go"}}},
		edgesErr: errFake,
	}
	if _, err := engineWithStore(t, root, &fakeStore{filesErr: errFake}).ReadNodes(0); !errors.Is(err, errFake) {
		t.Errorf("file enumeration: err = %v", err)
	}
	if _, err := engineWithStore(t, root, nodesFake).ReadNodes(0); !errors.Is(err, errFake) {
		t.Errorf("node read: err = %v", err)
	}
	if _, err := engineWithStore(t, root, edgesFake).ReadEdges(0); !errors.Is(err, errFake) {
		t.Errorf("edge read: err = %v", err)
	}
	if _, err := engineWithStore(t, root, nodesFake).ListCommunities(0, ""); !errors.Is(err, errFake) {
		t.Errorf("communities snapshot: err = %v", err)
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

// ── derivation failure arms ──────────────────────────────────────────────────

func TestDerivationFailuresPropagate(t *testing.T) {
	failCRGDerivations(t)
	e := builtEngine(t, []string{"lib/lib.go"})
	if _, err := e.ListFlows(0, ""); !errors.Is(err, errFake) {
		t.Errorf("ListFlows err = %v", err)
	}
	if _, err := e.ListCommunities(0, ""); !errors.Is(err, errFake) {
		t.Errorf("ListCommunities err = %v", err)
	}
	if err := e.Postprocess(graphstore.PostprocessOptions{}); !errors.Is(err, errFake) {
		t.Errorf("Postprocess err = %v", err)
	}
}

func TestDetectChangesToleratesDerivationFailure(t *testing.T) {
	failCRGDerivations(t)
	e := builtEngine(t, []string{"lib/lib.go"})
	report, err := e.DetectChanges(graphstore.DetectChangesOptions{})
	if err != nil {
		t.Fatalf("DetectChanges must degrade, not fail: %v", err)
	}
	if len(report.AffectedFlows) != 0 || report.RiskScore != 0 {
		t.Fatalf("want a degraded report, got %+v", report)
	}
}

func TestPostprocessPropagatesMetadataFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{metaErr: errFake})
	if err := e.Postprocess(graphstore.PostprocessOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected metadata failure", err)
	}
}

// ── small pure helpers ───────────────────────────────────────────────────────

func TestNonEmptyLinesTrimsAndSorts(t *testing.T) {
	got := nonEmptyLines("b.go\n\n  a.go  \r\n")
	if strings.Join(got, ",") != "a.go,b.go" {
		t.Fatalf("nonEmptyLines = %v", got)
	}
	if nonEmptyLines("   \n\n") != nil {
		t.Fatal("blank input must yield no lines")
	}
}

func TestCommunityNameFallsBackToRepresentativeID(t *testing.T) {
	if got := communityName(graphSnapshot{}, "orphan-id"); got != "orphan-id" {
		t.Fatalf("communityName = %q, want the raw id", got)
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

func TestImportAliasHandlesEveryImportForm(t *testing.T) {
	cases := map[string]struct {
		spec *ast.ImportSpec
		path string
		want string
	}{
		"implicit": {&ast.ImportSpec{}, "example.com/x/lib", "lib"},
		"explicit": {&ast.ImportSpec{Name: ast.NewIdent("alias")}, "example.com/x/lib", "alias"},
		"blank":    {&ast.ImportSpec{Name: ast.NewIdent("_")}, "example.com/x/lib", ""},
		"dot":      {&ast.ImportSpec{Name: ast.NewIdent(".")}, "example.com/x/lib", ""},
	}
	for name, tc := range cases {
		if got := importAlias(tc.spec, tc.path); got != tc.want {
			t.Errorf("%s: importAlias = %q, want %q", name, got, tc.want)
		}
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

func TestScanIgnoresAmbiguousBareNames(t *testing.T) {
	root := writeFixture(t)
	writeExtra(t, root, "other/other.go", "package other\n\nfunc decorate(s string) string { return s }\n")
	_, corpus, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, ref := range corpus.References {
		if ref.To == "other.decorate" {
			t.Fatalf("ambiguous bare name resolved to the wrong package: %+v", ref)
		}
	}
}

func TestScanResolvesAliasedImports(t *testing.T) {
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
	found := false
	for _, ref := range corpus.References {
		if ref.From == "app.AliasRun" && ref.To == "lib.Greet" && ref.Kind == edgeCalls {
			found = true
		}
	}
	if !found {
		t.Fatalf("aliased import call not resolved: %+v", corpus.References)
	}
}

func TestScanSkipsUnreadableFile(t *testing.T) {
	root := writeFixture(t)
	// A directory named like a Go file is unreadable as a source file, which is
	// the read-failure arm parseUnit must skip rather than fail on.
	writeExtra(t, root, "weird.go/keep.txt", "x")
	files, _, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, f := range files {
		if f.Path == "weird.go" {
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

func TestGitChangedFilesFallsBackToTrackedFiles(t *testing.T) {
	root := initGitRepo(t)
	// The repository has only a root commit, so HEAD~1 does not resolve.
	files, err := gitChangedFiles(root, "")
	if err != nil {
		t.Fatalf("gitChangedFiles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("want every tracked file when the base does not resolve")
	}
}

func TestGitChangedFilesDiffsAResolvableBase(t *testing.T) {
	root := initGitRepo(t)
	files, err := gitChangedFiles(root, "HEAD")
	if err != nil {
		t.Fatalf("gitChangedFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("HEAD...HEAD = %v, want no changes", files)
	}
}

func TestHeadCommitResolvesInsideRepo(t *testing.T) {
	if got := headCommit(initGitRepo(t)); len(got) < 7 {
		t.Fatalf("headCommit = %q, want a sha", got)
	}
}

func TestGitTrackedFilesFailsOutsideRepo(t *testing.T) {
	if _, err := gitTrackedFiles(t.TempDir()); err == nil {
		t.Fatal("want an error outside a git repository")
	}
}
