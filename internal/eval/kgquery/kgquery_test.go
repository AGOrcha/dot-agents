package kgquery

import (
	"context"
	"errors"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// fakeReader is a tiny in-memory CodeGraphReader used to back the Querier in
// unit tests. It stubs only the read role (the narrow contract kgquery
// depends on); the mutation/note/link/closer methods return zero values and
// are never exercised by this package.
type fakeReader struct {
	nodes    map[string]graphstore.GraphNode
	outEdges map[string][]graphstore.GraphEdge
	inEdges  map[string][]graphstore.GraphEdge

	searchResult []graphstore.GraphNode
	searchErr    error
	getNodeErr   map[string]error
	outErr       map[string]error
	inErr        map[string]error

	// cancelOnSource fires cancel the first time GetEdgesBySource is asked
	// for the named symbol, to exercise mid-traversal context cancellation.
	cancelOnSource string
	cancel         context.CancelFunc
}

func newFake() *fakeReader {
	return &fakeReader{
		nodes:      map[string]graphstore.GraphNode{},
		outEdges:   map[string][]graphstore.GraphEdge{},
		inEdges:    map[string][]graphstore.GraphEdge{},
		getNodeErr: map[string]error{},
		outErr:     map[string]error{},
		inErr:      map[string]error{},
	}
}

// --- CodeGraphReader (only these are used) ---

func (f *fakeReader) GetNode(qn string) (*graphstore.GraphNode, error) {
	if err := f.getNodeErr[qn]; err != nil {
		return nil, err
	}
	n, ok := f.nodes[qn]
	if !ok {
		return nil, nil
	}
	cp := n
	return &cp, nil
}

func (f *fakeReader) GetEdgesBySource(qn string) ([]graphstore.GraphEdge, error) {
	if f.cancelOnSource != "" && qn == f.cancelOnSource && f.cancel != nil {
		f.cancel()
	}
	if err := f.outErr[qn]; err != nil {
		return nil, err
	}
	return f.outEdges[qn], nil
}

func (f *fakeReader) GetEdgesByTarget(qn string) ([]graphstore.GraphEdge, error) {
	if err := f.inErr[qn]; err != nil {
		return nil, err
	}
	return f.inEdges[qn], nil
}

func (f *fakeReader) SearchNodes(string, int) ([]graphstore.GraphNode, error) {
	return f.searchResult, f.searchErr
}

// --- unused reader methods ---

func (f *fakeReader) GetNodesByFile(string) ([]graphstore.GraphNode, error)  { return nil, nil }
func (f *fakeReader) GetEdgesAmong([]string) ([]graphstore.GraphEdge, error) { return nil, nil }
func (f *fakeReader) GetAllFiles() ([]string, error)                         { return nil, nil }
func (f *fakeReader) GetMetadata(string) (string, error)                     { return "", nil }
func (f *fakeReader) GetStats() (graphstore.GraphStats, error)               { return graphstore.GraphStats{}, nil }
func (f *fakeReader) GetImpactRadius([]string, int, int) (graphstore.ImpactResult, error) {
	return graphstore.ImpactResult{}, nil
}

var _ graphstore.CodeGraphReader = (*fakeReader)(nil)

func fn(qn, lang string, start, end int) graphstore.GraphNode {
	return graphstore.GraphNode{
		Kind:          graphstore.NodeKindFunction,
		Name:          qn,
		QualifiedName: qn,
		Language:      lang,
		LineStart:     start,
		LineEnd:       end,
	}
}

func callEdge(src, tgt string) graphstore.GraphEdge {
	return graphstore.GraphEdge{Kind: graphstore.EdgeKindCalls, SourceQualified: src, TargetQualified: tgt}
}

func mustNew(t *testing.T, r graphstore.CodeGraphReader) *Querier {
	t.Helper()
	q, err := New(r)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return q
}

func TestNew(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) should error")
	}
	if _, err := New(newFake()); err != nil {
		t.Fatalf("New(fake): %v", err)
	}
}

func TestSeedSymbols(t *testing.T) {
	f := newFake()
	f.searchResult = []graphstore.GraphNode{
		fn("pkg.Zebra", "go", 1, 10),
		fn("pkg.Alpha", "go", 1, 5),
		fn("pkg.PyThing", "python", 1, 5),
		{Kind: graphstore.NodeKindFile, QualifiedName: "pkg/file.go", Language: "go"},
		{Kind: graphstore.NodeKindFunction, QualifiedName: "pkg.TestX", Language: "go", IsTest: true},
		{Kind: graphstore.NodeKindType, Name: "T", QualifiedName: "pkg.T", Language: "GO"}, // case-insensitive lang
	}
	q := mustNew(t, f)

	seeds, err := q.SeedSymbols(context.Background(), eval.LanguageGo, 10)
	if err != nil {
		t.Fatalf("SeedSymbols: %v", err)
	}
	// Expect Alpha, T, Zebra (go functions/types, no file/test/python), sorted.
	want := []string{"pkg.Alpha", "pkg.T", "pkg.Zebra"}
	if len(seeds) != len(want) {
		t.Fatalf("got %d seeds, want %d: %+v", len(seeds), len(want), seeds)
	}
	for i, w := range want {
		if seeds[i].QualifiedName != w {
			t.Errorf("seed[%d] = %q, want %q", i, seeds[i].QualifiedName, w)
		}
	}
}

func TestSeedSymbolsLimitTruncates(t *testing.T) {
	f := newFake()
	f.searchResult = []graphstore.GraphNode{
		fn("pkg.C", "go", 1, 2), fn("pkg.A", "go", 1, 2), fn("pkg.B", "go", 1, 2),
	}
	seeds, err := mustNew(t, f).SeedSymbols(context.Background(), eval.LanguageGo, 2)
	if err != nil {
		t.Fatalf("SeedSymbols: %v", err)
	}
	if len(seeds) != 2 || seeds[0].QualifiedName != "pkg.A" || seeds[1].QualifiedName != "pkg.B" {
		t.Fatalf("limit truncation wrong: %+v", seeds)
	}
}

func TestSeedSymbolsErrors(t *testing.T) {
	q := mustNew(t, newFake())
	if _, err := q.SeedSymbols(context.Background(), eval.Language("cobol"), 5); err == nil {
		t.Error("invalid language should error")
	}
	if _, err := q.SeedSymbols(context.Background(), eval.LanguageGo, 0); err == nil {
		t.Error("non-positive limit should error")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.SeedSymbols(cancelled, eval.LanguageGo, 5); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx: got %v", err)
	}

	fErr := newFake()
	fErr.searchErr = errors.New("boom")
	if _, err := mustNew(t, fErr).SeedSymbols(context.Background(), eval.LanguageGo, 5); err == nil {
		t.Error("search error should propagate")
	}
}

func TestNeighborhoodForDepthZero(t *testing.T) {
	f := newFake()
	f.nodes["root"] = fn("root", "go", 1, 3)
	f.outEdges["root"] = []graphstore.GraphEdge{callEdge("root", "callee")}
	nb, err := mustNew(t, f).NeighborhoodFor(context.Background(), "root", 0)
	if err != nil {
		t.Fatalf("NeighborhoodFor: %v", err)
	}
	if nb.Root.QualifiedName != "root" {
		t.Errorf("root = %q", nb.Root.QualifiedName)
	}
	if len(nb.Nodes) != 1 || nb.Nodes[0].QualifiedName != "root" {
		t.Errorf("depth 0 should be root only, got %+v", nb.Nodes)
	}
	if len(nb.Edges) != 0 {
		t.Errorf("depth 0 should have no edges, got %+v", nb.Edges)
	}
}

func TestNeighborhoodForDepthOneAndTwo(t *testing.T) {
	f := newFake()
	for _, n := range []string{"root", "a", "b", "c"} {
		f.nodes[n] = fn(n, "go", 1, 2)
	}
	// root -> a (out), b -> root (in); a -> c (out); cycle a -> root.
	f.outEdges["root"] = []graphstore.GraphEdge{callEdge("root", "a")}
	f.inEdges["root"] = []graphstore.GraphEdge{callEdge("b", "root")}
	f.outEdges["a"] = []graphstore.GraphEdge{callEdge("a", "c"), callEdge("a", "root")}

	q := mustNew(t, f)

	d1, err := q.NeighborhoodFor(context.Background(), "root", 1)
	if err != nil {
		t.Fatalf("depth 1: %v", err)
	}
	if got := nodeNames(d1.Nodes); !equalSet(got, []string{"a", "b", "root"}) {
		t.Errorf("depth 1 nodes = %v", got)
	}
	if len(d1.Edges) != 2 {
		t.Errorf("depth 1 edges = %d, want 2", len(d1.Edges))
	}

	d2, err := q.NeighborhoodFor(context.Background(), "root", 2)
	if err != nil {
		t.Fatalf("depth 2: %v", err)
	}
	if got := nodeNames(d2.Nodes); !equalSet(got, []string{"a", "b", "c", "root"}) {
		t.Errorf("depth 2 nodes = %v", got)
	}
	// edges: root->a, b->root, a->c, a->root (deduped) = 4 distinct.
	if len(d2.Edges) != 4 {
		t.Errorf("depth 2 edges = %d, want 4", len(d2.Edges))
	}
	// edges must be sorted deterministically.
	for i := 1; i < len(d2.Edges); i++ {
		if edgeKey(d2.Edges[i-1]) > edgeKey(d2.Edges[i]) {
			t.Errorf("edges not sorted at %d", i)
		}
	}
}

func TestNeighborhoodForDanglingTarget(t *testing.T) {
	f := newFake()
	f.nodes["root"] = fn("root", "go", 1, 2)
	f.outEdges["root"] = []graphstore.GraphEdge{callEdge("root", "ghost")} // ghost has no node
	nb, err := mustNew(t, f).NeighborhoodFor(context.Background(), "root", 1)
	if err != nil {
		t.Fatalf("NeighborhoodFor: %v", err)
	}
	if got := nodeNames(nb.Nodes); !equalSet(got, []string{"root"}) {
		t.Errorf("dangling target should contribute no node, got %v", got)
	}
	if len(nb.Edges) != 1 {
		t.Errorf("dangling edge should still be recorded, got %d", len(nb.Edges))
	}
}

func TestNeighborhoodForErrors(t *testing.T) {
	q := mustNew(t, newFake())
	if _, err := q.NeighborhoodFor(context.Background(), "  ", 1); err == nil {
		t.Error("empty name should error")
	}
	if _, err := q.NeighborhoodFor(context.Background(), "x", -1); err == nil {
		t.Error("negative depth should error")
	}
	if _, err := q.NeighborhoodFor(context.Background(), "missing", 1); err == nil {
		t.Error("missing root should error")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.NeighborhoodFor(cancelled, "x", 1); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx: %v", err)
	}

	// GetNode error on root.
	fRoot := newFake()
	fRoot.getNodeErr["root"] = errors.New("db down")
	if _, err := mustNew(t, fRoot).NeighborhoodFor(context.Background(), "root", 1); err == nil {
		t.Error("root GetNode error should propagate")
	}

	// Outbound edge error during expand.
	fOut := newFake()
	fOut.nodes["root"] = fn("root", "go", 1, 2)
	fOut.outErr["root"] = errors.New("out boom")
	if _, err := mustNew(t, fOut).NeighborhoodFor(context.Background(), "root", 1); err == nil {
		t.Error("outbound edge error should propagate")
	}

	// Inbound edge error during expand.
	fIn := newFake()
	fIn.nodes["root"] = fn("root", "go", 1, 2)
	fIn.inErr["root"] = errors.New("in boom")
	if _, err := mustNew(t, fIn).NeighborhoodFor(context.Background(), "root", 1); err == nil {
		t.Error("inbound edge error should propagate")
	}

	// Neighbor GetNode error.
	fNb := newFake()
	fNb.nodes["root"] = fn("root", "go", 1, 2)
	fNb.outEdges["root"] = []graphstore.GraphEdge{callEdge("root", "n")}
	fNb.getNodeErr["n"] = errors.New("neighbor down")
	if _, err := mustNew(t, fNb).NeighborhoodFor(context.Background(), "root", 1); err == nil {
		t.Error("neighbor GetNode error should propagate")
	}
}

func TestNeighborhoodForCancelMidTraversal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newFake()
	f.cancel = cancel
	f.cancelOnSource = "root" // cancel fires while expanding hop 0
	f.nodes["root"] = fn("root", "go", 1, 2)
	f.nodes["a"] = fn("a", "go", 1, 2)
	f.outEdges["root"] = []graphstore.GraphEdge{callEdge("root", "a")}
	if _, err := mustNew(t, f).NeighborhoodFor(ctx, "root", 2); !errors.Is(err, context.Canceled) {
		t.Errorf("mid-traversal cancel: got %v", err)
	}
}

func TestComplexityProxy(t *testing.T) {
	f := newFake()
	f.nodes["fn"] = fn("fn", "go", 10, 30) // span 21
	f.outEdges["fn"] = []graphstore.GraphEdge{
		callEdge("fn", "x"),
		callEdge("fn", "y"),
		callEdge("fn", "x"), // duplicate callee, counted once
		{Kind: graphstore.EdgeKindContains, SourceQualified: "fn", TargetQualified: "z"}, // non-CALLS ignored
		{Kind: graphstore.EdgeKindCalls, SourceQualified: "fn", TargetQualified: ""},     // empty target ignored
	}
	f.inEdges["fn"] = []graphstore.GraphEdge{
		callEdge("p", "fn"),
		callEdge("q", "fn"),
		{Kind: graphstore.EdgeKindCalls, SourceQualified: "", TargetQualified: "fn"}, // empty source ignored
	}
	c, err := mustNew(t, f).ComplexityProxy(context.Background(), "fn")
	if err != nil {
		t.Fatalf("ComplexityProxy: %v", err)
	}
	if c.SpanLines != 21 {
		t.Errorf("SpanLines = %d, want 21", c.SpanLines)
	}
	if c.FanOut != 2 {
		t.Errorf("FanOut = %d, want 2", c.FanOut)
	}
	if c.FanIn != 2 {
		t.Errorf("FanIn = %d, want 2", c.FanIn)
	}
	if c.Cyclomatic != 3 {
		t.Errorf("Cyclomatic = %d, want 3 (1+FanOut)", c.Cyclomatic)
	}
	if c.QualifiedName != "fn" {
		t.Errorf("QualifiedName = %q", c.QualifiedName)
	}
}

func TestComplexityProxySpanClamp(t *testing.T) {
	f := newFake()
	f.nodes["p"] = graphstore.GraphNode{QualifiedName: "p", Kind: graphstore.NodeKindFunction, LineStart: 5, LineEnd: 5}
	c, err := mustNew(t, f).ComplexityProxy(context.Background(), "p")
	if err != nil {
		t.Fatalf("ComplexityProxy: %v", err)
	}
	if c.SpanLines != 1 {
		t.Errorf("single-line span = %d, want 1", c.SpanLines)
	}

	f.nodes["bad"] = graphstore.GraphNode{QualifiedName: "bad", LineStart: 10, LineEnd: 2} // malformed
	c2, _ := mustNew(t, f).ComplexityProxy(context.Background(), "bad")
	if c2.SpanLines != 1 {
		t.Errorf("malformed span should clamp to 1, got %d", c2.SpanLines)
	}
}

func TestComplexityProxyErrors(t *testing.T) {
	q := mustNew(t, newFake())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.ComplexityProxy(cancelled, "x"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx: %v", err)
	}
	if _, err := q.ComplexityProxy(context.Background(), ""); err == nil {
		t.Error("empty name should error")
	}
	if _, err := q.ComplexityProxy(context.Background(), "missing"); err == nil {
		t.Error("missing symbol should error")
	}

	fGet := newFake()
	fGet.getNodeErr["x"] = errors.New("boom")
	if _, err := mustNew(t, fGet).ComplexityProxy(context.Background(), "x"); err == nil {
		t.Error("GetNode error should propagate")
	}

	fOut := newFake()
	fOut.nodes["x"] = fn("x", "go", 1, 2)
	fOut.outErr["x"] = errors.New("out boom")
	if _, err := mustNew(t, fOut).ComplexityProxy(context.Background(), "x"); err == nil {
		t.Error("outbound error should propagate")
	}

	fIn := newFake()
	fIn.nodes["x"] = fn("x", "go", 1, 2)
	fIn.inErr["x"] = errors.New("in boom")
	if _, err := mustNew(t, fIn).ComplexityProxy(context.Background(), "x"); err == nil {
		t.Error("inbound error should propagate")
	}
}

func nodeNames(nodes []graphstore.GraphNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.QualifiedName
	}
	return out
}

func equalSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
