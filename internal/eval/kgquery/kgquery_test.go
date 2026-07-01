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
// depends on); the mutation/note/link/closer methods are absent because the
// Querier binds to CodeGraphReader, not the whole Store.
type fakeReader struct {
	nodes    map[string]graphstore.GraphNode
	outEdges map[string][]graphstore.GraphEdge
	inEdges  map[string][]graphstore.GraphEdge

	files          []string
	nodesByFile    map[string][]graphstore.GraphNode
	filesErr       error
	nodesByFileErr map[string]error

	getNodeErr map[string]error
	outErr     map[string]error
	inErr      map[string]error

	// cancelOnSource fires cancel the first time GetEdgesBySource is asked
	// for the named symbol; cancelOnGetNode fires cancel when GetNode is
	// asked for the named symbol; cancelOnListFiles fires cancel when
	// GetAllFiles is called. Each exercises a distinct mid-work cancellation
	// window after the top-level guard has already passed.
	cancelOnSource    string
	cancelOnGetNode   string
	cancelOnListFiles bool
	cancel            context.CancelFunc
}

func newFake() *fakeReader {
	return &fakeReader{
		nodes:          map[string]graphstore.GraphNode{},
		outEdges:       map[string][]graphstore.GraphEdge{},
		inEdges:        map[string][]graphstore.GraphEdge{},
		nodesByFile:    map[string][]graphstore.GraphNode{},
		nodesByFileErr: map[string]error{},
		getNodeErr:     map[string]error{},
		outErr:         map[string]error{},
		inErr:          map[string]error{},
	}
}

// addFileNode registers n under file f for the SeedSymbols enumeration path.
func (f *fakeReader) addFileNode(file string, n graphstore.GraphNode) {
	if _, ok := f.nodesByFile[file]; !ok {
		f.files = append(f.files, file)
	}
	f.nodesByFile[file] = append(f.nodesByFile[file], n)
}

// --- CodeGraphReader ---

func (f *fakeReader) GetNode(qn string) (*graphstore.GraphNode, error) {
	if f.cancelOnGetNode != "" && qn == f.cancelOnGetNode && f.cancel != nil {
		f.cancel()
	}
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

func (f *fakeReader) GetNodesByFile(file string) ([]graphstore.GraphNode, error) {
	if err := f.nodesByFileErr[file]; err != nil {
		return nil, err
	}
	return f.nodesByFile[file], nil
}

func (f *fakeReader) GetAllFiles() ([]string, error) {
	if f.cancelOnListFiles && f.cancel != nil {
		f.cancel()
	}
	return f.files, f.filesErr
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

// --- unused reader methods ---

func (f *fakeReader) GetEdgesAmong([]string) ([]graphstore.GraphEdge, error)  { return nil, nil }
func (f *fakeReader) SearchNodes(string, int) ([]graphstore.GraphNode, error) { return nil, nil }
func (f *fakeReader) GetMetadata(string) (string, error)                      { return "", nil }
func (f *fakeReader) GetStats() (graphstore.GraphStats, error) {
	return graphstore.GraphStats{}, nil
}
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
	// Spread candidates across multiple files, deliberately out of qualified-
	// name order relative to file order, plus non-seed noise per file.
	f.addFileNode("z.go", fn("pkg.Zebra", "go", 1, 10))
	f.addFileNode("z.go", graphstore.GraphNode{Kind: graphstore.NodeKindFile, QualifiedName: "z.go", Language: "go"})
	f.addFileNode("a.go", fn("pkg.Alpha", "go", 1, 5))
	f.addFileNode("a.go", graphstore.GraphNode{Kind: graphstore.NodeKindFunction, QualifiedName: "pkg.TestX", Language: "go", IsTest: true})
	f.addFileNode("m.py", fn("pkg.PyThing", "python", 1, 5))
	f.addFileNode("t.go", graphstore.GraphNode{Kind: graphstore.NodeKindType, Name: "T", QualifiedName: "pkg.T", Language: "GO"}) // case-insensitive lang

	q := mustNew(t, f)
	seeds, err := q.SeedSymbols(context.Background(), eval.LanguageGo, 10)
	if err != nil {
		t.Fatalf("SeedSymbols: %v", err)
	}
	// Alpha, T, Zebra: go functions/types only, sorted by qualified name.
	want := []string{"pkg.Alpha", "pkg.T", "pkg.Zebra"}
	if got := nodeNames(seeds); !equalSet(got, want) {
		t.Fatalf("seeds = %v, want %v", got, want)
	}
}

// TestSeedSymbolsDeterministic proves the same (lang, limit) on a fixed graph
// yields the same ordered list across repeated runs (R4 reproducibility), and
// that a valid seed in a lexically-late file is NOT dropped by any pre-filter
// truncation — the cap is applied after a total order over the COMPLETE set.
func TestSeedSymbolsDeterministic(t *testing.T) {
	f := newFake()
	// Many files; the lexically-smallest qualified names live in the
	// lexically-largest files, so a truncating pre-filter would drop them.
	f.addFileNode("zzz.go", fn("pkg.Aaa", "go", 1, 2))
	f.addFileNode("yyy.go", fn("pkg.Bbb", "go", 1, 2))
	f.addFileNode("aaa.go", fn("pkg.Yyy", "go", 1, 2))
	f.addFileNode("bbb.go", fn("pkg.Zzz", "go", 1, 2))
	q := mustNew(t, f)

	first, err := q.SeedSymbols(context.Background(), eval.LanguageGo, 2)
	if err != nil {
		t.Fatalf("SeedSymbols: %v", err)
	}
	// The two smallest qualified names win regardless of file order.
	if got := nodeNames(first); !equalSet(got, []string{"pkg.Aaa", "pkg.Bbb"}) {
		t.Fatalf("cap picked wrong seeds (truncation defeated filtering?): %v", got)
	}
	for i := 0; i < 5; i++ {
		again, err := q.SeedSymbols(context.Background(), eval.LanguageGo, 2)
		if err != nil {
			t.Fatalf("SeedSymbols run %d: %v", i, err)
		}
		if !equalSet(nodeNames(again), nodeNames(first)) {
			t.Fatalf("non-deterministic: run %d = %v, first = %v", i, nodeNames(again), nodeNames(first))
		}
	}
}

func TestSeedSymbolsDedupesByQualifiedName(t *testing.T) {
	f := newFake()
	// Same qualified name reported under two files collapses to one seed.
	f.addFileNode("a.go", fn("pkg.Dup", "go", 1, 2))
	f.addFileNode("b.go", fn("pkg.Dup", "go", 1, 2))
	seeds, err := mustNew(t, f).SeedSymbols(context.Background(), eval.LanguageGo, 10)
	if err != nil {
		t.Fatalf("SeedSymbols: %v", err)
	}
	if len(seeds) != 1 {
		t.Fatalf("duplicate qualified name should collapse, got %d: %v", len(seeds), nodeNames(seeds))
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

	fFiles := newFake()
	fFiles.filesErr = errors.New("files boom")
	if _, err := mustNew(t, fFiles).SeedSymbols(context.Background(), eval.LanguageGo, 5); err == nil {
		t.Error("GetAllFiles error should propagate")
	}

	fNodes := newFake()
	fNodes.files = []string{"a.go"}
	fNodes.nodesByFileErr["a.go"] = errors.New("nodes boom")
	if _, err := mustNew(t, fNodes).SeedSymbols(context.Background(), eval.LanguageGo, 5); err == nil {
		t.Error("GetNodesByFile error should propagate")
	}

	// Cancellation observed mid-enumeration: the top guard passes, then
	// GetAllFiles fires cancel, so the per-file loop check catches it.
	ctx, cancelMid := context.WithCancel(context.Background())
	fMid := newFake()
	fMid.cancel = cancelMid
	fMid.cancelOnListFiles = true
	fMid.files = []string{"a.go"}
	if _, err := mustNew(t, fMid).SeedSymbols(ctx, eval.LanguageGo, 5); !errors.Is(err, context.Canceled) {
		t.Errorf("mid-enumeration cancel: got %v", err)
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

	fRoot := newFake()
	fRoot.getNodeErr["root"] = errors.New("db down")
	if _, err := mustNew(t, fRoot).NeighborhoodFor(context.Background(), "root", 1); err == nil {
		t.Error("root GetNode error should propagate")
	}

	fOut := newFake()
	fOut.nodes["root"] = fn("root", "go", 1, 2)
	fOut.outErr["root"] = errors.New("out boom")
	if _, err := mustNew(t, fOut).NeighborhoodFor(context.Background(), "root", 1); err == nil {
		t.Error("outbound edge error should propagate")
	}

	fIn := newFake()
	fIn.nodes["root"] = fn("root", "go", 1, 2)
	fIn.inErr["root"] = errors.New("in boom")
	if _, err := mustNew(t, fIn).NeighborhoodFor(context.Background(), "root", 1); err == nil {
		t.Error("inbound edge error should propagate")
	}

	fNb := newFake()
	fNb.nodes["root"] = fn("root", "go", 1, 2)
	fNb.outEdges["root"] = []graphstore.GraphEdge{callEdge("root", "n")}
	fNb.getNodeErr["n"] = errors.New("neighbor down")
	if _, err := mustNew(t, fNb).NeighborhoodFor(context.Background(), "root", 1); err == nil {
		t.Error("neighbor GetNode error should propagate")
	}
}

// TestNeighborhoodForCancelDuringExpand exercises the post-expand ctx check:
// depth 1 (the final and only hop), cancellation fires while fetching the
// root's edges, and must be observed rather than swallowed.
func TestNeighborhoodForCancelDuringExpand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newFake()
	f.cancel = cancel
	f.cancelOnSource = "root"
	f.nodes["root"] = fn("root", "go", 1, 2)
	f.nodes["a"] = fn("a", "go", 1, 2)
	f.outEdges["root"] = []graphstore.GraphEdge{callEdge("root", "a")}
	if _, err := mustNew(t, f).NeighborhoodFor(ctx, "root", 1); !errors.Is(err, context.Canceled) {
		t.Errorf("final-hop cancel during expand: got %v", err)
	}
}

// TestNeighborhoodForCancelBetweenNeighbors exercises the per-neighbor ctx
// check: depth 1, cancellation fires resolving the first neighbor, so the
// second neighbor's check catches it within the same (final) hop.
func TestNeighborhoodForCancelBetweenNeighbors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newFake()
	f.cancel = cancel
	f.cancelOnGetNode = "a"
	for _, n := range []string{"root", "a", "b"} {
		f.nodes[n] = fn(n, "go", 1, 2)
	}
	f.outEdges["root"] = []graphstore.GraphEdge{callEdge("root", "a"), callEdge("root", "b")}
	if _, err := mustNew(t, f).NeighborhoodFor(ctx, "root", 1); !errors.Is(err, context.Canceled) {
		t.Errorf("final-hop cancel between neighbors: got %v", err)
	}
}

// TestNeighborhoodForCancelBetweenFrontierNames exercises the per-name ctx
// check in stepFrontier: at depth 2, cancellation becomes pending while the
// first frontier name is processed successfully, so the next name's check
// catches it at the start of the following hop's work.
func TestNeighborhoodForCancelBetweenFrontierNames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newFake()
	f.cancel = cancel
	f.cancelOnGetNode = "c" // fires on c's resolution during hop 1, name "a"
	for _, n := range []string{"root", "a", "b", "c"} {
		f.nodes[n] = fn(n, "go", 1, 2)
	}
	f.outEdges["root"] = []graphstore.GraphEdge{callEdge("root", "a"), callEdge("root", "b")}
	f.outEdges["a"] = []graphstore.GraphEdge{callEdge("a", "c")}
	if _, err := mustNew(t, f).NeighborhoodFor(ctx, "root", 2); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel between frontier names: got %v", err)
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
