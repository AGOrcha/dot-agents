package codegraph

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ── bulk export / snapshot error propagation ─────────────────────────────────

// TestReadEdgesPropagatesNodeEnumerationFailure covers ReadEdges' first read:
// the node walk it needs before it can ask for any edges.
func TestReadEdgesPropagatesNodeEnumerationFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{filesErr: errFake})
	if _, err := e.ReadEdges(0); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected file-enumeration failure", err)
	}
}

// TestSnapshotPropagatesEdgeEnumerationFailure covers readSnapshot's edge read.
// ListCommunities is the shortest public path through e.snapshot().
func TestSnapshotPropagatesEdgeEnumerationFailure(t *testing.T) {
	store := &fakeStore{
		files:    []string{"lib/lib.go"},
		nodes:    map[string][]graphstore.GraphNode{"lib/lib.go": {{Kind: kindFunction, QualifiedName: "lib.Greet", FilePath: "lib/lib.go"}}},
		edgesErr: errFake,
	}
	e := engineWithStore(t, writeFixture(t), store)
	if _, err := e.ListCommunities(0, ""); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected edge-enumeration failure", err)
	}
}

// TestReadAllEdgesCollapsesDuplicateSourcesAndEdges asserts the documented
// de-duplication: a source qualified name is queried once even when two nodes
// share it, and an edge id already collected is not repeated.
func TestReadAllEdgesCollapsesDuplicateSourcesAndEdges(t *testing.T) {
	shared := graphstore.GraphEdge{ID: 7, Kind: edgeCalls, SourceQualified: "lib.Greet", TargetQualified: "lib.decorate"}
	store := &fakeStore{
		edges: map[string][]graphstore.GraphEdge{
			"lib.Greet":    {shared},
			"lib.decorate": {shared, {ID: 9, Kind: edgeCalls, SourceQualified: "lib.decorate", TargetQualified: "lib.Greet"}},
		},
	}
	nodes := []graphstore.GraphNode{
		{QualifiedName: "lib.Greet", FilePath: "lib/lib.go"},
		{QualifiedName: "lib.Greet", FilePath: "lib/other.go"},
		{QualifiedName: "lib.decorate", FilePath: "lib/lib.go"},
	}
	edges, err := readAllEdges(store, nodes)
	if err != nil {
		t.Fatalf("readAllEdges: %v", err)
	}
	if len(edges) != 2 || edges[0].ID != 7 || edges[1].ID != 9 {
		t.Fatalf("readAllEdges = %+v, want edges 7 and 9 exactly once", edges)
	}
	if got := strings.Join(store.edgeSources, ","); got != "lib.Greet,lib.decorate" {
		t.Fatalf("queried sources = %q, want each source once", got)
	}
}

// ── git diff failure arms ────────────────────────────────────────────────────

// fakeGit puts a `git` first on PATH that reports every revision as resolvable
// and then fails the diff, writing diagnostic to stderr.
func fakeGit(t *testing.T, diagnostic string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell git shim is not executable on Windows")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = rev-parse ]; then exit 0; fi\ndone\n"
	if diagnostic != "" {
		script += "echo " + diagnostic + " >&2\n"
	}
	script += "exit 3\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestGitChangedFilesSurfacesDiffDiagnostic(t *testing.T) {
	fakeGit(t, "refusing-to-diff")
	_, err := gitChangedFiles(t.TempDir(), "main")
	if err == nil {
		t.Fatal("want an error when git diff fails")
	}
	if !strings.Contains(err.Error(), "refusing-to-diff") {
		t.Fatalf("err = %v, want git's own diagnostic retained", err)
	}
}

func TestGitChangedFilesReportsSilentDiffFailure(t *testing.T) {
	fakeGit(t, "")
	_, err := gitChangedFiles(t.TempDir(), "main")
	if err == nil {
		t.Fatal("want an error when git diff fails without output")
	}
	if !strings.Contains(err.Error(), "git diff main...HEAD") {
		t.Fatalf("err = %v, want the failing command named", err)
	}
}

// ── scan walk arms ───────────────────────────────────────────────────────────

// TestGoFilesReportsWalkFailure covers the walk-failure arm. The callback
// swallows per-entry errors, so the seam is the only way in.
func TestGoFilesReportsWalkFailure(t *testing.T) {
	orig := walkDir
	t.Cleanup(func() { walkDir = orig })
	walkDir = func(string, fs.WalkDirFunc) error { return errFake }
	if _, err := goFiles(t.TempDir()); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected walk failure", err)
	}
}

// TestScanSkipsUnreadableSubtree asserts an unreadable directory is skipped
// rather than failing the whole scan.
func TestScanSkipsUnreadableSubtree(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not block reads here")
	}
	root := writeFixture(t)
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeExtra(t, root, "locked/hidden.go", "package locked\n\nfunc Hidden() {}\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	files, _, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan must skip the unreadable subtree, not fail: %v", err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Path, "locked/") {
			t.Fatalf("unreadable subtree produced a unit: %+v", f)
		}
	}
}

// TestScanSkipsUnreadableSourceFile asserts a path that enumerates as a Go
// file but cannot be read is dropped rather than failing the scan. A dangling
// symlink is unreadable for every user, including root.
func TestScanSkipsUnreadableSourceFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	root := writeFixture(t)
	if err := os.Symlink(filepath.Join(root, "absent-target"), filepath.Join(root, "broken.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files, _, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan must skip the unreadable file, not fail: %v", err)
	}
	for _, f := range files {
		if f.Path == "broken.go" {
			t.Fatal("unreadable file produced an ingestion unit")
		}
	}
}

// TestScanResolvesSamePackageMethodExpression covers the qualifier arm where a
// selector's qualifier is a same-package receiver type rather than an import.
func TestScanResolvesSamePackageMethodExpression(t *testing.T) {
	root := writeFixture(t)
	writeExtra(t, root, "lib/method.go", `package lib

// Label names the config.
func (c Config) Label() string { return c.Name }

// LabelOf applies the method expression, so the reference qualifier is the
// receiver type rather than an imported package.
func LabelOf(c Config) string { return Config.Label(c) }
`)
	_, corpus, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, ref := range corpus.References {
		if ref.From == "lib.LabelOf" && ref.To == "lib.Config.Label" && ref.Kind == edgeCalls {
			return
		}
	}
	t.Fatalf("method expression not resolved to the receiver-qualified symbol: %+v", corpus.References)
}

// ── query limit / ordering arms ──────────────────────────────────────────────

// TestListFlowsTruncatesToLimit covers the explicit-limit truncation, which
// needs more derived flows than the fixture graph produces on its own.
func TestListFlowsTruncatesToLimit(t *testing.T) {
	orig := flowsFromStore
	t.Cleanup(func() { flowsFromStore = orig })
	flowsFromStore = func(crg.StoreReader, string) ([]crg.Flow, error) {
		return []crg.Flow{
			{ID: "a@f.go", EntryPoint: "a", Criticality: 3},
			{ID: "b@f.go", EntryPoint: "b", Criticality: 2},
			{ID: "c@f.go", EntryPoint: "c", Criticality: 1},
		}, nil
	}
	result, err := builtEngine(t, nil).ListFlows(2, "")
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(result.Flows) != 2 {
		t.Fatalf("ListFlows(2) = %d flows, want 2", len(result.Flows))
	}
	if result.Flows[0].EntryPoint != "a" || result.Flows[1].EntryPoint != "b" {
		t.Fatalf("truncated the wrong flows: %+v", result.Flows)
	}
}

// TestSortFlowsRanksHigherCriticalityFirst covers the criticality comparison
// itself (stubFlows deliberately ties, exercising only the tie-break).
func TestSortFlowsRanksHigherCriticalityFirst(t *testing.T) {
	flows := []crg.Flow{
		{ID: "a@f.go", EntryPoint: "a", Criticality: 1},
		{ID: "z@f.go", EntryPoint: "z", Criticality: 5},
	}
	sortFlows(flows, "criticality")
	if flows[0].EntryPoint != "z" {
		t.Fatalf("criticality sort = %+v, want the most critical flow first", flows)
	}
}

// TestPostprocessPropagatesSnapshotFailure covers Postprocess' snapshot read.
func TestPostprocessPropagatesSnapshotFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{filesErr: errFake})
	if err := e.Postprocess(graphstore.PostprocessOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected snapshot failure", err)
	}
}

// TestDetectChangesPropagatesSnapshotFailure covers DetectChanges' snapshot
// read, which happens after the diff resolves.
func TestDetectChangesPropagatesSnapshotFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{filesErr: errFake})
	if _, err := e.DetectChanges(graphstore.DetectChangesOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected snapshot failure", err)
	}
}
