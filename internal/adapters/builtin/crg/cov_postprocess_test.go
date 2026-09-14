package crg

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ---------------------------------------------------------------------------
// Entry-point detection
// ---------------------------------------------------------------------------

// covPPRecognisedDecorators is one decorator per framework family in
// frameworkDecoratorPatterns. Every one of them must mark its function a
// framework-invoked entry point.
var covPPRecognisedDecorators = []string{
	"app.get",                  // FastAPI / Flask route
	"router.delete",            // APIRouter
	"blueprint.before_request", // Flask blueprint hook
	"after_response",           // bare request/response hook
	"click.command",            // Click
	"mygroup.command",          // Click subgroup
	"field_validator",          // Pydantic
	"shared_task",              // Celery
	"receiver",                 // Django signals
	"api_view",                 // Django REST
	"action",                   // DRF viewset action
	"pytest.fixture",           // pytest
	"override_settings",        // Django test settings
	"event.listens_for",        // SQLAlchemy
	"GetMapping",               // Spring MVC
	"Scheduled",                // Spring scheduling
	"KafkaListener",            // Spring Kafka
	"WorkflowMethod",           // Temporal
	"Injectable",               // Angular / NestJS
	"Resolver",                 // GraphQL
	"app.use",                  // Express middleware mount
	"@Composable",              // Jetpack Compose
	"HiltViewModel",            // Hilt
	"agent.tool_plain",         // pydantic-ai
	"tool",                     // bare @tool
	"starlette.middleware",     // ASGI middleware
	"bp.route",                 // Flask blueprint route
}

// covPPUnrecognisedDecorators are ordinary Python/Java decorators that carry
// no framework-invocation signal. They must NOT promote their function.
var covPPUnrecognisedDecorators = []string{"@noop", "wraps", "dataclass", "property"}

func TestCovPPHasFrameworkDecorator_RecognisesEveryFrameworkFamily(t *testing.T) {
	for _, dec := range covPPRecognisedDecorators {
		node := testNode(1, graphstore.NodeKindFunction, "worker", "a.py")
		node.Extra = map[string]any{"decorators": dec}
		if !hasFrameworkDecorator(node) {
			t.Errorf("decorator %q must be recognised as framework-invoked", dec)
		}
	}
	for _, dec := range covPPUnrecognisedDecorators {
		node := testNode(1, graphstore.NodeKindFunction, "worker", "a.py")
		node.Extra = map[string]any{"decorators": dec}
		if hasFrameworkDecorator(node) {
			t.Errorf("decorator %q must not be treated as framework-invoked", dec)
		}
	}
	if hasFrameworkDecorator(testNode(1, graphstore.NodeKindFunction, "worker", "a.py")) {
		t.Error("a node with no decorators must not be framework-invoked")
	}
}

// covPPDecoratorShape is one shape node.Extra["decorators"] arrives in.
type covPPDecoratorShape struct {
	name      string
	value     any
	wantList  []string
	wantEntry bool
}

var covPPDecoratorShapes = []covPPDecoratorShape{
	{"single string", "app.get", []string{"app.get"}, true},
	{"empty string", "", nil, false},
	{"unrecognised string", "@noop", []string{"@noop"}, false},
	{"string slice", []string{"@noop", "router.post"}, []string{"@noop", "router.post"}, true},
	{"any slice drops non-strings", []any{"pytest.mark", 7}, []string{"pytest.mark"}, true},
	{"unexpected type", 42, nil, false},
	{"absent", nil, nil, false},
}

// TestCovPPDecoratorsOf_NormalisesEveryExtraShape proves the decorator signal
// survives every shape node.Extra["decorators"] can hold — a bare string, a
// Go slice, and the []any a list takes on after a JSON round-trip through the
// store — and that `worker` is promoted to an entry point ONLY when the
// normalised list carries a framework decorator. `worker` is called by
// `caller`, so the uncalled-root signal cannot mask the decorator one.
func TestCovPPDecoratorsOf_NormalisesEveryExtraShape(t *testing.T) {
	for _, tc := range covPPDecoratorShapes {
		t.Run(tc.name, func(t *testing.T) {
			worker := testNode(2, graphstore.NodeKindFunction, "worker", "a.py")
			if tc.value != nil {
				worker.Extra = map[string]any{"decorators": tc.value}
			}
			if got := decoratorsOf(worker); !reflect.DeepEqual(got, tc.wantList) {
				t.Fatalf("decoratorsOf = %#v, want %#v", got, tc.wantList)
			}
			covPPAssertDecoratorEntry(t, worker, tc.wantEntry)
		})
	}
}

// covPPAssertDecoratorEntry checks whether `worker` shows up as an entry point
// alongside its caller.
func covPPAssertDecoratorEntry(t *testing.T, worker graphstore.GraphNode, wantEntry bool) {
	t.Helper()
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.py"), worker,
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.py::caller", worker.QualifiedName, "a.py"),
	}
	var names []string
	for _, ep := range DetectEntryPoints(nodes, edges, false) {
		names = append(names, ep.Name)
	}
	want := []string{"caller"}
	if wantEntry {
		want = append(want, "worker")
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("entry points = %v, want %v", names, want)
	}
}

// covPPEntryNames are names every language's convention table marks as an
// entry point.
var covPPEntryNames = []string{
	"main", "__main__", "test_thing", "TestThing", "on_message", "handle_it",
	"handler", "handle", "lambda_handler", "upgrade", "downgrade", "lifespan",
	"get_db", "onCreate", "doGet", "do_GET", "log_message", "middleware",
	"errorHandler", "ngOnInit", "transform", "canActivate", "componentDidMount",
}

// covPPNonEntryNames are near-misses of the patterns above: each one differs
// from a real convention by an anchor, so none may match.
var covPPNonEntryNames = []string{
	"mainly", "Testing", "handled", "upgraded", "renderer", "doGetter", "resolver",
}

func TestCovPPMatchesEntryName_ConventionsAndNearMisses(t *testing.T) {
	for _, name := range covPPEntryNames {
		if !matchesEntryName(testNode(1, graphstore.NodeKindFunction, name, "a.go")) {
			t.Errorf("%q must match an entry-name convention", name)
		}
	}
	for _, name := range covPPNonEntryNames {
		if matchesEntryName(testNode(1, graphstore.NodeKindFunction, name, "a.go")) {
			t.Errorf("%q must not match an entry-name convention", name)
		}
	}
}

// TestCovPPMatchesEntryName_LanguageTableIsScoped proves the per-language
// table only fires for its own language: `boot` and `__invoke` are PHP
// conventions and must not promote a Go symbol.
func TestCovPPMatchesEntryName_LanguageTableIsScoped(t *testing.T) {
	for _, name := range []string{"boot", "register", "__invoke"} {
		php := testNode(1, graphstore.NodeKindFunction, name, "a.php")
		php.Language = "php"
		if !matchesEntryName(php) {
			t.Errorf("php %q must match the php entry-name table", name)
		}
		if matchesEntryName(testNode(1, graphstore.NodeKindFunction, name, "a.go")) {
			t.Errorf("go %q must not inherit php conventions", name)
		}
	}
}

// TestCovPPDetectEntryPoints_SkipsVerilogConstructs proves a Verilog
// declaration is excluded even though nothing calls it: hardware constructs
// are not callable code, so every one of them would otherwise register as a
// root and drown the real flows.
func TestCovPPDetectEntryPoints_SkipsVerilogConstructs(t *testing.T) {
	hw := testNode(1, graphstore.NodeKindFunction, "alu", "cpu.v")
	hw.Language = "verilog"
	hw.Extra = map[string]any{"verilog_kind": "module"}
	nodes := []graphstore.GraphNode{hw, testNode(2, graphstore.NodeKindFunction, "prod", "a.go")}

	eps := DetectEntryPoints(nodes, nil, false)
	if len(eps) != 1 || eps[0].Name != "prod" {
		t.Fatalf("a verilog construct must not be an entry point, got %+v", eps)
	}
}

// ---------------------------------------------------------------------------
// Flow tracing
// ---------------------------------------------------------------------------

// covPPChainGraph builds n Function nodes wired into a single CALLS chain
// n1 -> n2 -> ... -> nN. name and file map the 1-based index to the node's
// symbol name and file path.
func covPPChainGraph(n int, name, file func(int) string) ([]graphstore.GraphNode, []graphstore.GraphEdge) {
	nodes := make([]graphstore.GraphNode, 0, n)
	for i := 1; i <= n; i++ {
		nodes = append(nodes, testNode(int64(i), graphstore.NodeKindFunction, name(i), file(i)))
	}
	edges := make([]graphstore.GraphEdge, 0, n)
	for i := 0; i+1 < len(nodes); i++ {
		edges = append(edges, testEdge(int64(i+1), graphstore.EdgeKindCalls,
			nodes[i].QualifiedName, nodes[i+1].QualifiedName, nodes[i].FilePath))
	}
	return nodes, edges
}

// covPPNumbered returns an index -> "<prefix><i>" namer.
func covPPNumbered(prefix string) func(int) string {
	return func(i int) string { return prefix + strconv.Itoa(i) }
}

// TestCovPPTraceFlows_DefaultDepthBoundsALongChain proves maxDepth <= 0 falls
// back to DefaultFlowMaxDepth rather than tracing without a bound: a 17-link
// chain is cut at the 15th hop, so the flow holds 16 nodes and reports the
// depth it REACHED.
func TestCovPPTraceFlows_DefaultDepthBoundsALongChain(t *testing.T) {
	nodes, edges := covPPChainGraph(17, covPPNumbered("step"), func(int) string { return "chain.go" })

	flows, paths := TraceFlows(nodes, edges, 0, false)
	if len(flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(flows))
	}
	if flows[0].Depth != DefaultFlowMaxDepth {
		t.Errorf("depth = %d, want %d", flows[0].Depth, DefaultFlowMaxDepth)
	}
	if flows[0].NodeCount != DefaultFlowMaxDepth+1 {
		t.Errorf("node count = %d, want %d", flows[0].NodeCount, DefaultFlowMaxDepth+1)
	}
	if got := len(paths[0]); got != DefaultFlowMaxDepth+1 {
		t.Fatalf("path length = %d, want %d", got, DefaultFlowMaxDepth+1)
	}
	if paths[0][len(paths[0])-1] != int64(DefaultFlowMaxDepth+1) {
		t.Errorf("last traced node = %d, want %d", paths[0][len(paths[0])-1], DefaultFlowMaxDepth+1)
	}
}

// TestCovPPTraceFlows_CycleAndSelfEdgeVisitEachNodeOnce proves the BFS
// terminates on cyclic call graphs and records each node exactly once: `alpha`
// and `beta` call each other, and `beta` also calls itself.
func TestCovPPTraceFlows_CycleAndSelfEdgeVisitEachNodeOnce(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "main", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "alpha", "a.go"),
		testNode(3, graphstore.NodeKindFunction, "beta", "a.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::main", "a.go::alpha", "a.go"),
		testEdge(2, graphstore.EdgeKindCalls, "a.go::alpha", "a.go::beta", "a.go"),
		testEdge(3, graphstore.EdgeKindCalls, "a.go::beta", "a.go::alpha", "a.go"),
		testEdge(4, graphstore.EdgeKindCalls, "a.go::beta", "a.go::beta", "a.go"),
	}
	flows, paths := TraceFlows(nodes, edges, DefaultFlowMaxDepth, false)
	if len(flows) != 1 {
		t.Fatalf("expected 1 flow from `main`, got %d: %+v", len(flows), flows)
	}
	if !reflect.DeepEqual(paths[0], []int64{1, 2, 3}) {
		t.Fatalf("path = %v, want [1 2 3]", paths[0])
	}
	if flows[0].Depth != 2 {
		t.Errorf("depth = %d, want 2", flows[0].Depth)
	}
}

// TestCovPPComputeCriticality_AllFactorsSaturate drives every criticality
// factor to its ceiling at once. The five weights sum to 1.0, so a flow that
// saturates all of them scores exactly 1.0:
//
//	12 files (> 4+1), 5 unresolved calls (>= 5), every node security-named,
//	no TESTED_BY edge anywhere, depth 11 (> 10).
func TestCovPPComputeCriticality_AllFactorsSaturate(t *testing.T) {
	const chain = 12
	nodes, edges := covPPChainGraph(chain, covPPNumbered("authStep"),
		func(i int) string { return "f" + strconv.Itoa(i) + ".go" })
	for i := 1; i <= 5; i++ {
		edges = append(edges, testEdge(int64(100+i), graphstore.EdgeKindCalls,
			nodes[0].QualifiedName, "externalLib.Call"+strconv.Itoa(i), nodes[0].FilePath))
	}

	flows, _ := TraceFlows(nodes, edges, DefaultFlowMaxDepth, false)
	if len(flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(flows))
	}
	if flows[0].Criticality != 1.0 {
		t.Errorf("criticality = %v, want 1 (every factor saturated)", flows[0].Criticality)
	}
	if flows[0].FileCount != chain || flows[0].Depth != chain-1 {
		t.Errorf("fileCount/depth = %d/%d, want %d/%d",
			flows[0].FileCount, flows[0].Depth, chain, chain-1)
	}
}

// TestCovPPComputeCriticality_UnresolvableNodeIDsScoreZero proves the guard
// upstream needs when a stored path outlives the nodes it names: an empty id
// list, and a list whose ids no longer resolve, both score 0 rather than
// dividing by a zero node count.
func TestCovPPComputeCriticality_UnresolvableNodeIDsScoreZero(t *testing.T) {
	g := newGraphIndex(
		[]graphstore.GraphNode{testNode(1, graphstore.NodeKindFunction, "kept", "a.go")}, nil)

	if got := g.computeCriticality(nil, 3); got != 0 {
		t.Errorf("criticality of an empty path = %v, want 0", got)
	}
	if got := g.computeCriticality([]int64{404, 405}, 3); got != 0 {
		t.Errorf("criticality of a wholly stale path = %v, want 0", got)
	}
}

// TestCovPPIncrementalTraceFlows_MalformedStoredPathTouchesNothing pins how a
// corrupt path_json behaves. The blob is store-owned data, so an unreadable
// one yields no ids: the flow then appears to touch no changed file and is
// kept verbatim with an empty path, where the same flow with a readable path
// is dropped and re-traced.
func TestCovPPIncrementalTraceFlows_MalformedStoredPathTouchesNothing(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "main", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "tail", "b.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::main", "b.go::tail", "a.go"),
	}
	stored := graphstore.FlowRow{ID: 7, Name: "main", EntryPointID: 1, PathJSON: "[1,2]"}

	retraced, _, keep, keepPaths := IncrementalTraceFlows(nodes, edges,
		[]graphstore.FlowRow{stored}, []string{"b.go"}, 0)
	if len(keep) != 0 || len(retraced) != 1 {
		t.Fatalf("a readable path through a changed file must be re-traced, keep=%d retraced=%d",
			len(keep), len(retraced))
	}

	stored.PathJSON = "{not-an-array"
	retraced, _, keep, keepPaths = IncrementalTraceFlows(nodes, edges,
		[]graphstore.FlowRow{stored}, []string{"b.go"}, 0)
	if len(retraced) != 0 {
		t.Fatalf("an unreadable path names no changed file, so nothing re-traces: %+v", retraced)
	}
	if len(keep) != 1 || keep[0].ID != 7 {
		t.Fatalf("the flow must be kept verbatim, got %+v", keep)
	}
	if keepPaths[0] != nil {
		t.Errorf("kept path = %v, want nil for an undecodable blob", keepPaths[0])
	}
}

// TestCovPPIncrementalTraceFlows_NothingStoredAndNothingChanged proves the
// doubly-empty case returns four empty results rather than allocating a path
// slice for zero rows.
func TestCovPPIncrementalTraceFlows_NothingStoredAndNothingChanged(t *testing.T) {
	nodes := []graphstore.GraphNode{testNode(1, graphstore.NodeKindFunction, "main", "a.go")}
	retraced, retracedPaths, keep, keepPaths := IncrementalTraceFlows(nodes, nil, nil, nil, 0)
	if retraced != nil || retracedPaths != nil || keep != nil || keepPaths != nil {
		t.Fatalf("expected four nil results, got %v/%v/%v/%v",
			retraced, retracedPaths, keep, keepPaths)
	}
}

// TestCovPPDecodePath_StoreOwnedBlobsNeverError pins decodePath's three
// inputs: a real array, an empty column, and a corrupt blob.
func TestCovPPDecodePath_StoreOwnedBlobsNeverError(t *testing.T) {
	cases := []struct {
		name string
		blob string
		want []int64
	}{
		{"array", "[3,1,2]", []int64{3, 1, 2}},
		{"empty column", "", nil},
		{"corrupt blob", `{"path": [1]}`, nil},
		{"wrong element type", `["a"]`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodePath(tc.blob); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decodePath(%q) = %v, want %v", tc.blob, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Name sanitising and truncation
// ---------------------------------------------------------------------------

// TestCovPPSanitizeName_StripsControlCharactersKeepingTabAndNewline proves an
// adversarial symbol name cannot carry ASCII control characters into a tool
// response, while the two whitespace controls upstream keeps survive.
func TestCovPPSanitizeName_StripsControlCharactersKeepingTabAndNewline(t *testing.T) {
	got := sanitizeName("ev\x00i\x07l\x1b\tkeep\nme")
	if want := "evil\tkeep\nme"; got != want {
		t.Fatalf("sanitizeName = %q, want %q", got, want)
	}
}

// TestCovPPSanitizeName_TruncatesOnACodePointBoundary proves the cut is by
// code point, not byte: 300 two-byte runes come back as exactly
// maxSanitizedNameLen runes of still-valid UTF-8, where a byte-based cut would
// have kept 256 bytes and split a rune.
func TestCovPPSanitizeName_TruncatesOnACodePointBoundary(t *testing.T) {
	got := sanitizeName(strings.Repeat("é", 300))
	if n := utf8.RuneCountInString(got); n != maxSanitizedNameLen {
		t.Fatalf("rune count = %d, want %d", n, maxSanitizedNameLen)
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncation split a multi-byte rune")
	}
	if got != strings.Repeat("é", maxSanitizedNameLen) {
		t.Fatal("truncation must keep the leading runes verbatim")
	}
}

func TestCovPPTruncateRunes_Bounds(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"under the limit", "abc", 10, "abc"},
		{"exactly at the limit", "abc", 3, "abc"},
		{"over the limit", "abcdef", 3, "abc"},
		{"zero limit", "abc", 0, ""},
		{"multi-byte", "αβγδ", 2, "αβ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncateRunes(tc.in, tc.limit); got != tc.want {
				t.Fatalf("truncateRunes(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Community detection
// ---------------------------------------------------------------------------

// TestCovPPDetectCommunities_EmptyGraphYieldsNoCommunities proves the detector
// survives a graph with no code nodes — the File-only case a freshly scanned
// asset directory produces.
func TestCovPPDetectCommunities_EmptyGraphYieldsNoCommunities(t *testing.T) {
	fileOnly := []graphstore.GraphNode{{
		ID: 1, Kind: graphstore.NodeKindFile, Name: "a.go",
		QualifiedName: "a.go", FilePath: "a.go", Language: "go",
	}}
	for _, nodes := range [][]graphstore.GraphNode{nil, fileOnly} {
		rows, members := DetectCommunities(nodes, nil, 0)
		if len(rows) != 0 || len(members) != 0 {
			t.Fatalf("expected no communities, got %+v / %+v", rows, members)
		}
	}
}

// TestCovPPDetectCommunities_DefaultMinSizeDropsSmallGroups proves minSize <= 0
// falls back to DefaultCommunityMinSize: the two-symbol directory qualifies,
// the one-symbol directory does not.
func TestCovPPDetectCommunities_DefaultMinSizeDropsSmallGroups(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "alphaOne", "repo/a/x.go"),
		testNode(2, graphstore.NodeKindFunction, "alphaTwo", "repo/a/x.go"),
		testNode(3, graphstore.NodeKindFunction, "betaSolo", "repo/b/y.go"),
	}
	rows, members := DetectCommunities(nodes, nil, 0)
	if len(rows) != 1 {
		t.Fatalf("expected 1 community, got %d: %+v", len(rows), rows)
	}
	if rows[0].Description != "Directory-based community: a" || rows[0].Size != 2 {
		t.Fatalf("community = %+v, want the 2-member `a` directory", rows[0])
	}
	if !reflect.DeepEqual(members[0], []string{"repo/a/x.go::alphaOne", "repo/a/x.go::alphaTwo"}) {
		t.Fatalf("members = %v", members[0])
	}
}

// TestCovPPDetectCommunities_StopsDeepeningAtTheGroupTarget proves the
// depth search stops as soon as communityGroupTarget groups qualify: ten
// two-symbol packages, each with a deeper `sub` directory that would have
// split them further, are grouped at depth 1 and the `sub` level is never
// reached.
func TestCovPPDetectCommunities_StopsDeepeningAtTheGroupTarget(t *testing.T) {
	var nodes []graphstore.GraphNode
	id := int64(0)
	for d := range communityGroupTarget {
		dir := "repo/pkg" + strconv.Itoa(d) + "/sub/x.go"
		for _, name := range []string{"alphaOne", "alphaTwo"} {
			id++
			nodes = append(nodes, testNode(id, graphstore.NodeKindFunction,
				name+strconv.Itoa(d), dir))
		}
	}
	rows, _ := DetectCommunities(nodes, nil, DefaultCommunityMinSize)
	if len(rows) != communityGroupTarget {
		t.Fatalf("expected %d communities, got %d", communityGroupTarget, len(rows))
	}
	if rows[0].Description != "Directory-based community: pkg0" {
		t.Fatalf("grouping deepened past the target: %q", rows[0].Description)
	}
}

// TestCovPPDetectCommunities_RootLevelFilesGroupByStem proves a file at the
// repository root — one with no directory segments and no extension at all —
// still forms a community, bucketed and named by its stem.
func TestCovPPDetectCommunities_RootLevelFilesGroupByStem(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "renderReport", "Makefile"),
		testNode(2, graphstore.NodeKindFunction, "renderChart", "Makefile"),
	}
	rows, _ := DetectCommunities(nodes, nil, DefaultCommunityMinSize)
	if len(rows) != 1 {
		t.Fatalf("expected 1 community, got %+v", rows)
	}
	if rows[0].Description != "Directory-based community: Makefile" {
		t.Errorf("description = %q", rows[0].Description)
	}
	if rows[0].Name != "makefile-render" {
		t.Errorf("name = %q, want makefile-render", rows[0].Name)
	}
}

// TestCovPPCommonPrefixLen_BoundedByTheShallowestPath proves the shared-prefix
// scan never indexes past the shortest path, and that a divergence at the
// first segment yields no prefix at all.
func TestCovPPCommonPrefixLen_BoundedByTheShallowestPath(t *testing.T) {
	cases := []struct {
		name  string
		parts [][]string
		want  int
	}{
		{"no paths", nil, 0},
		{"shallowest first", [][]string{{"repo"}, {"repo", "a", "b"}}, 1},
		{"shallowest last", [][]string{{"repo", "a", "b"}, {"repo"}}, 1},
		{"diverges immediately", [][]string{{"src", "a"}, {"lib", "a"}}, 0},
		{"fully shared", [][]string{{"repo", "a"}, {"repo", "a"}}, 2},
		{"root-level file", [][]string{{}, {"repo"}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commonPrefixLen(tc.parts); got != tc.want {
				t.Fatalf("commonPrefixLen(%v) = %d, want %d", tc.parts, got, tc.want)
			}
		})
	}
}

func TestCovPPStripExtension_LastDotOnly(t *testing.T) {
	cases := map[string]string{
		"main.go":      "main",
		"a.test.ts":    "a.test",
		"Makefile":     "Makefile",
		".gitignore":   "",
		"archive.tar.": "archive.tar",
	}
	for in, want := range cases {
		if got := stripExtension(in); got != want {
			t.Errorf("stripExtension(%q) = %q, want %q", in, got, want)
		}
	}
}

// covPPLangNode builds a Function node with an explicit language.
func covPPLangNode(id int64, name, file, lang string) graphstore.GraphNode {
	n := testNode(id, graphstore.NodeKindFunction, name, file)
	n.Language = lang
	return n
}

// TestCovPPDominantLanguage_MajorityTieAndAbsent pins the language column of a
// mixed-language directory: the majority wins, a tie keeps first-seen order,
// and a community whose members carry no language at all reports none.
func TestCovPPDominantLanguage_MajorityTieAndAbsent(t *testing.T) {
	cases := []struct {
		name    string
		members []graphstore.GraphNode
		want    string
	}{
		{"majority wins", []graphstore.GraphNode{
			covPPLangNode(1, "a", "x.py", "python"),
			covPPLangNode(2, "b", "x.go", "go"),
			covPPLangNode(3, "c", "y.go", "go"),
		}, "go"},
		{"tie keeps first seen", []graphstore.GraphNode{
			covPPLangNode(1, "a", "x.py", "python"),
			covPPLangNode(2, "b", "x.go", "go"),
		}, "python"},
		{"no language at all", []graphstore.GraphNode{
			covPPLangNode(1, "a", "x", ""), covPPLangNode(2, "b", "y", ""),
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dominantLanguage(tc.members); got != tc.want {
				t.Fatalf("dominantLanguage = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCovPPDominantLanguage_SurfacesOnTheCommunityRow proves the absent-language
// case reaches the persisted row rather than being an internal detail.
func TestCovPPDominantLanguage_SurfacesOnTheCommunityRow(t *testing.T) {
	nodes := []graphstore.GraphNode{
		covPPLangNode(1, "alphaOne", "repo/a/x.go", ""),
		covPPLangNode(2, "alphaTwo", "repo/a/x.go", ""),
	}
	rows, _ := DetectCommunities(nodes, nil, DefaultCommunityMinSize)
	if len(rows) != 1 || rows[0].DominantLanguage != "" {
		t.Fatalf("dominant language must be empty, got %+v", rows)
	}
}

// covPPClassNode builds a Class node.
func covPPClassNode(id int64, name, file string) graphstore.GraphNode {
	return testNode(id, graphstore.NodeKindClass, name, file)
}

// covPPTestNode builds a Test node flagged as a test.
func covPPTestNode(id int64, name, file string) graphstore.GraphNode {
	n := testNode(id, graphstore.NodeKindTest, name, file)
	n.IsTest = true
	return n
}

// TestCovPPGenerateCommunityName_EveryNamingRoute walks the namer's decision
// table. A directory named `_` slugifies to nothing, which is how the
// prefix-less routes are reached.
func TestCovPPGenerateCommunityName_EveryNamingRoute(t *testing.T) {
	cases := []struct {
		name    string
		members []graphstore.GraphNode
		want    string
	}{
		{"no members", nil, "empty"},
		{"dominant class with prefix", []graphstore.GraphNode{
			covPPClassNode(1, "Session", "pkg/x.go"),
			covPPClassNode(2, "Session", "pkg/x.go"),
			covPPClassNode(3, "Other", "pkg/x.go"),
		}, "pkg-session"},
		{"dominant class without prefix", []graphstore.GraphNode{
			covPPClassNode(1, "Session", "_/x.go"),
			covPPClassNode(2, "Session", "_/x.go"),
		}, "session"},
		{"minority class falls back to keywords", []graphstore.GraphNode{
			covPPClassNode(1, "Session", "pkg/x.go"),
			testNode(2, graphstore.NodeKindFunction, "cacheAlpha", "pkg/x.go"),
			testNode(3, graphstore.NodeKindFunction, "cacheBeta", "pkg/x.go"),
		}, "pkg-cache"},
		{"prefix only", []graphstore.GraphNode{
			testNode(1, graphstore.NodeKindFunction, "get", "pkg/x.go"),
			testNode(2, graphstore.NodeKindFunction, "set", "pkg/x.go"),
		}, "pkg"},
		{"keyword only", []graphstore.GraphNode{
			testNode(1, graphstore.NodeKindFunction, "alphaThing", "_/x.go"),
			testNode(2, graphstore.NodeKindFunction, "alphaOther", "_/x.go"),
		}, "alpha"},
		{"neither", []graphstore.GraphNode{
			testNode(1, graphstore.NodeKindFunction, "get", "_/x.go"),
			testNode(2, graphstore.NodeKindFunction, "set", "_/x.go"),
		}, "cluster"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := generateCommunityName(tc.members); got != tc.want {
				t.Fatalf("generateCommunityName = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCovPPNamingMembers_TestsOnlyNameATestOnlyCommunity proves the naming
// vocabulary drops test symbols whenever any production member exists, and
// falls back to the tests when there is nothing else to name the package
// after.
func TestCovPPNamingMembers_TestsOnlyNameATestOnlyCommunity(t *testing.T) {
	tests := []graphstore.GraphNode{
		covPPTestNode(1, "TestHarness", "pkg/x_test.go"),
		covPPTestNode(2, "TestHarnessTwo", "pkg/x_test.go"),
	}
	if got := generateCommunityName(tests); got != "pkg-harness" {
		t.Fatalf("a test-only community must be named after its tests, got %q", got)
	}

	withProduction := append([]graphstore.GraphNode{
		testNode(3, graphstore.NodeKindFunction, "cacheAlpha", "pkg/x.go"),
	}, tests...)
	if got := generateCommunityName(withProduction); got != "pkg-cache" {
		t.Fatalf("production members must own the vocabulary, got %q", got)
	}
	if got := namingMembers(tests); len(got) != 2 {
		t.Fatalf("a test-only community keeps all its members, got %d", len(got))
	}
}

func TestCovPPExtractFilePrefix_EmptyAndRootLevel(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  string
	}{
		{"no paths", nil, ""},
		{"parent directory", []string{"src/auth/a.go", "src/auth/b.go"}, "auth"},
		{"root-level file", []string{"main.go", "main.go"}, "main"},
		{"windows separators", []string{`src\auth\a.go`}, "auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractFilePrefix(tc.paths); got != tc.want {
				t.Fatalf("extractFilePrefix(%v) = %q, want %q", tc.paths, got, tc.want)
			}
		})
	}
}

// TestCovPPExtractKeywords_OnlyDeclarationKindsContribute proves a File node
// contributes no naming vocabulary — only Function, Class, Test and Type
// members do — and that generic words are filtered out.
func TestCovPPExtractKeywords_OnlyDeclarationKindsContribute(t *testing.T) {
	members := []graphstore.GraphNode{
		{ID: 1, Kind: graphstore.NodeKindFile, Name: "cacheFile", QualifiedName: "cacheFile.go",
			FilePath: "cacheFile.go", Language: "go"},
		testNode(2, graphstore.NodeKindFunction, "getToken", "a.go"),
		testNode(3, graphstore.NodeKindType, "tokenBag", "a.go"),
	}
	// "get" is a common word and "cacheFile" belongs to a File node, so
	// neither reaches the vocabulary.
	if got := extractKeywords(members); !reflect.DeepEqual(got, []string{"token", "bag"}) {
		t.Fatalf("keywords = %v, want [token bag]", got)
	}
}

func TestCovPPToSlug_TruncatesAtAWordBoundary(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"short", "AuthToken", "auth-token"},
		{"non-alphanumeric becomes a separator", "a/b.c", "a-b-c"},
		{"slugifies to nothing", "_", ""},
		{
			"long with a boundary inside the bound",
			"alphabetical beta gamma delta epsilon",
			"alphabetical-beta-gamma-delta",
		},
		{
			"long single word has no boundary",
			strings.Repeat("z", 40),
			strings.Repeat("z", slugMaxLen),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toSlug(tc.in)
			if got != tc.want {
				t.Fatalf("toSlug(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if len(got) > slugMaxLen {
				t.Fatalf("slug %q exceeds the %d-character bound", got, slugMaxLen)
			}
		})
	}
}

// TestCovPPDedupeCommunityNames_CollisionsAreDisambiguated is the collision
// case the detector must resolve: three sibling `util` directories whose
// members all revolve around `cache` generate the identical name
// "util-cache". The LARGEST keeps it; the next takes a distinguishing keyword
// from its own vocabulary; the last has no keyword outside the base name, so
// it falls back to a numeric suffix.
func TestCovPPDedupeCommunityNames_CollisionsAreDisambiguated(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "cacheAlpha", "repo/x/util/a.go"),
		testNode(2, graphstore.NodeKindFunction, "cacheBeta", "repo/x/util/a.go"),
		testNode(3, graphstore.NodeKindFunction, "cacheGamma", "repo/x/util/a.go"),
		testNode(4, graphstore.NodeKindFunction, "cacheDelta", "repo/y/util/b.go"),
		testNode(5, graphstore.NodeKindFunction, "deltaCache", "repo/y/util/b.go"),
		testNode(6, graphstore.NodeKindFunction, "cacheUtil", "repo/z/util/c.go"),
		testNode(7, graphstore.NodeKindFunction, "utilCache", "repo/z/util/c.go"),
	}
	rows, _ := DetectCommunities(nodes, nil, DefaultCommunityMinSize)
	if len(rows) != 3 {
		t.Fatalf("expected 3 communities, got %d: %+v", len(rows), rows)
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	want := []string{"util-cache", "util-cache-delta", "util-cache-2"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if rows[0].Size != 3 {
		t.Errorf("the largest community must keep the base name, sizes start at %d", rows[0].Size)
	}
}

// TestCovPPDedupeCommunityNames_UnnamedRowsAndStaleMembers proves two guards
// of the rename pass: a community with no generated name is never deduped
// (so two of them do not collide into a rename loop), and a member qualified
// name that no longer resolves to a node contributes no vocabulary instead of
// faulting.
func TestCovPPDedupeCommunityNames_UnnamedRowsAndStaleMembers(t *testing.T) {
	rows := []graphstore.CommunityRow{
		{Name: "", Size: 4},
		{Name: "", Size: 3},
		{Name: "dup", Size: 3},
		{Name: "dup", Size: 2},
	}
	members := [][]string{
		{"a.go::gone"}, {"a.go::gone"},
		{"a.go::keptOne"},
		{"gone.go::ghostCache", "a.go::zetaWorker"},
	}
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "keptOne", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "zetaWorker", "a.go"),
	}
	dedupeCommunityNames(rows, members, nodes)

	if rows[0].Name != "" || rows[1].Name != "" {
		t.Fatalf("unnamed communities must be left alone, got %q/%q", rows[0].Name, rows[1].Name)
	}
	if rows[2].Name != "dup" {
		t.Errorf("the largest duplicate keeps the base name, got %q", rows[2].Name)
	}
	if rows[3].Name != "dup-zeta" {
		t.Errorf("name = %q, want dup-zeta from the one member that still resolves", rows[3].Name)
	}
}

func TestCovPPMemberNodes_SkipsUnresolvableNames(t *testing.T) {
	kept := testNode(1, graphstore.NodeKindFunction, "kept", "a.go")
	byQN := map[string]graphstore.GraphNode{kept.QualifiedName: kept}

	got := memberNodes([]string{"gone.go::ghost", kept.QualifiedName}, byQN)
	if len(got) != 1 || got[0].Name != "kept" {
		t.Fatalf("memberNodes = %+v, want only `kept`", got)
	}
	if n := memberNodes(nil, byQN); len(n) != 0 {
		t.Fatalf("memberNodes(nil) = %+v, want empty", n)
	}
}

// ---------------------------------------------------------------------------
// Summary tables
// ---------------------------------------------------------------------------

func TestCovPPPurposeFromPaths_LastSharedDirectoryComponent(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  string
	}{
		{"no paths", nil, ""},
		{"no directory at all", []string{"main.go", "makefile"}, ""},
		{"single shared component", []string{"a/auth.go", "a/auth_test.go"}, "a"},
		{"nested", []string{"src/pkg/a.go", "src/pkg/b.go"}, "pkg"},
		{"nothing in common", []string{"src/a.go", "lib/b.go"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := purposeFromPaths(tc.paths); got != tc.want {
				t.Fatalf("purposeFromPaths(%v) = %q, want %q", tc.paths, got, tc.want)
			}
		})
	}
}

// TestCovPPPurposeFromPaths_OnlySamplesTheFirstPaths proves the upstream
// slice: a 21st path from an unrelated tree cannot dilute the prefix, because
// only the first purposePathSample paths are read.
func TestCovPPPurposeFromPaths_OnlySamplesTheFirstPaths(t *testing.T) {
	paths := make([]string, 0, purposePathSample+1)
	for i := range purposePathSample {
		paths = append(paths, "src/pkg/f"+strconv.Itoa(i)+".go")
	}
	paths = append(paths, "other/z.go")
	if got := purposeFromPaths(paths); got != "pkg" {
		t.Fatalf("purposeFromPaths = %q, want pkg", got)
	}
}

func TestCovPPCommonStringPrefix_CharacterWise(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  string
	}{
		{"no paths", nil, ""},
		{"single path is its own prefix", []string{"src/a.go"}, "src/a.go"},
		{"partial component", []string{"a/auth.go", "a/auth_test.go"}, "a/auth"},
		{"diverges at once", []string{"src/a.go", "lib/b.go", "src/c.go"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commonStringPrefix(tc.paths); got != tc.want {
				t.Fatalf("commonStringPrefix(%v) = %q, want %q", tc.paths, got, tc.want)
			}
		})
	}
}

// TestCovPPComputeFlowSnapshots_OrphanedFlowStillProducesARow proves a flow
// whose entry node has been deleted and whose path column is empty still
// yields a snapshot: the entry point falls back to the stringified node id and
// the critical path renders as an empty JSON array, never "null".
func TestCovPPComputeFlowSnapshots_OrphanedFlowStillProducesARow(t *testing.T) {
	nodes := []graphstore.GraphNode{testNode(1, graphstore.NodeKindFunction, "kept", "a.go")}
	flows := []graphstore.FlowRow{{ID: 9, Name: "ghost", EntryPointID: 404, PathJSON: ""}}

	got := computeFlowSnapshots(nodes, flows)
	if len(got) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(got))
	}
	if got[0].EntryPoint != "404" {
		t.Errorf("entry point = %q, want the stringified id", got[0].EntryPoint)
	}
	if got[0].CriticalPath != "[]" {
		t.Errorf("critical path = %q, want []", got[0].CriticalPath)
	}
}

// TestCovPPCriticalPath_EmptyPathHasNoEntry proves an empty path yields no
// critical path at all, rather than a one-element list naming an entry point
// the flow does not contain.
func TestCovPPCriticalPath_EmptyPathHasNoEntry(t *testing.T) {
	if got := criticalPath(nil, "a.go::main", map[int64]string{1: "a.go::main"}); got != nil {
		t.Fatalf("criticalPath(nil) = %v, want nil", got)
	}
}

// TestCovPPPyJSONStringArray_ShortControlEscapes pins the four short escapes
// Python's json.dumps emits for control characters that have them, alongside
// the \uXXXX form for one that does not.
func TestCovPPPyJSONStringArray_ShortControlEscapes(t *testing.T) {
	got := pyJSONStringArray([]string{"a\bb", "c\fd", "e\rf", "g\x01h"})
	want := `["a\bb", "c\fd", "e\rf", "g\u0001h"]`
	if got != want {
		t.Fatalf("pyJSONStringArray = %s, want %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// Bare-endpoint resolution
// ---------------------------------------------------------------------------

// TestCovPPResolveBareCallTargets_NothingBareIsANoOp proves the pass returns
// no rewrites at all when every CALLS target is already qualified, so a
// caller never persists a no-op generation.
func TestCovPPResolveBareCallTargets_NothingBareIsANoOp(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "helper", "a.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::caller", "a.go::helper", "a.go"),
	}
	rewrites, resolved := ResolveBareCallTargets(nodes, edges)
	if rewrites != nil || resolved != 0 {
		t.Fatalf("expected no rewrites, got %+v / %d", rewrites, resolved)
	}
}

// TestCovPPResolveBareCallTargets_ForeignVerdictIsNotOverwritten proves the
// pass yields to another resolver: an edge already carrying ambiguity or
// unresolved metadata it did not write is left untouched, because overwriting
// it would discard stronger evidence.
func TestCovPPResolveBareCallTargets_ForeignVerdictIsNotOverwritten(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "helper", "a.go"),
	}
	for _, key := range []string{ambiguousTargetsKey, unresolvedTargetsKey} {
		t.Run(key, func(t *testing.T) {
			edge := testEdge(1, graphstore.EdgeKindCalls, "a.go::caller", "helper", "a.go")
			edge.Extra = map[string]any{key: []string{"elsewhere.go::helper"}}
			rewrites, resolved := ResolveBareCallTargets(nodes, []graphstore.GraphEdge{edge})
			if rewrites != nil || resolved != 0 {
				t.Fatalf("a foreign verdict must be preserved, got %+v / %d", rewrites, resolved)
			}
		})
	}
}

// TestCovPPResolveBareCallTargets_CorruptRecordedNameIsSkipped proves the pass
// refuses to act on an edge whose recorded original name is not a string: the
// recorded value is what a re-run resolves against, so a corrupt one must not
// be coerced into a target.
func TestCovPPResolveBareCallTargets_CorruptRecordedNameIsSkipped(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "helper", "a.go"),
	}
	edge := testEdge(1, graphstore.EdgeKindCalls, "a.go::caller", "helper", "a.go")
	edge.Extra = map[string]any{bareCallTargetKey: 42}

	rewrites, resolved := ResolveBareCallTargets(nodes, []graphstore.GraphEdge{edge})
	if rewrites != nil || resolved != 0 {
		t.Fatalf("a corrupt recorded name must be skipped, got %+v / %d", rewrites, resolved)
	}
}

// covPPHelperDeclarations returns n Function nodes all named "helper", each in
// its own file, plus a caller in an unrelated file.
func covPPHelperDeclarations(n int) ([]graphstore.GraphNode, []string) {
	nodes := []graphstore.GraphNode{testNode(1, graphstore.NodeKindFunction, "caller", "call.go")}
	qns := make([]string, 0, n)
	for i := range n {
		node := testNode(int64(i+2), graphstore.NodeKindFunction, "helper",
			"d"+strconv.Itoa(i)+".go")
		nodes = append(nodes, node)
		qns = append(qns, node.QualifiedName)
	}
	return nodes, qns
}

// covPPManagedBareEdge is a CALLS edge this pass already managed: the original
// bare name is recorded, so a re-run re-evaluates it against the current graph.
func covPPManagedBareEdge() graphstore.GraphEdge {
	e := testEdge(1, graphstore.EdgeKindCalls, "call.go::caller", "helper", "call.go")
	e.Extra = map[string]any{bareCallTargetKey: "helper"}
	return e
}

// TestCovPPRecordCandidates_UnresolvedListsEverySameNamedDeclaration proves
// the diagnostic half of the evidence rule. With no same-file or import
// evidence the endpoint stays bare and the pass records every same-named
// declaration anywhere — capped at maxRecordedCandidates, with the true count
// and a truncation flag kept alongside so a reader is never misled about how
// many there were.
func TestCovPPRecordCandidates_UnresolvedListsEverySameNamedDeclaration(t *testing.T) {
	t.Run("within the cap", func(t *testing.T) {
		nodes, qns := covPPHelperDeclarations(2)
		extra := covPPResolveExtra(t, nodes, covPPManagedBareEdge())
		covPPAssertUnresolved(t, extra, qns, 2, false)
	})
	t.Run("truncated", func(t *testing.T) {
		nodes, qns := covPPHelperDeclarations(maxRecordedCandidates + 1)
		extra := covPPResolveExtra(t, nodes, covPPManagedBareEdge())
		covPPAssertUnresolved(t, extra, qns[:maxRecordedCandidates],
			maxRecordedCandidates+1, true)
	})
}

// covPPResolveExtra runs the CALLS pass over one edge and returns the extra map
// of the single rewrite it must produce, asserting the endpoint stayed bare.
func covPPResolveExtra(
	t *testing.T, nodes []graphstore.GraphNode, edge graphstore.GraphEdge,
) map[string]any {
	t.Helper()
	rewrites, resolved := ResolveBareCallTargets(nodes, []graphstore.GraphEdge{edge})
	if len(rewrites) != 1 {
		t.Fatalf("expected 1 rewrite, got %+v", rewrites)
	}
	if resolved != 0 {
		t.Fatalf("an unresolved endpoint must not count as resolved, got %d", resolved)
	}
	if rewrites[0].TargetQualified != "helper" {
		t.Fatalf("target = %q, want the bare name", rewrites[0].TargetQualified)
	}
	return rewrites[0].Extra
}

// covPPAssertUnresolved checks the three unresolved-state extra keys.
func covPPAssertUnresolved(
	t *testing.T, extra map[string]any, wantList []string, wantCount int, wantTruncated bool,
) {
	t.Helper()
	if got := extra[unresolvedTargetsKey]; !reflect.DeepEqual(got, wantList) {
		t.Errorf("unresolved targets = %v, want %v", got, wantList)
	}
	if got := extra[resolutionUnresolved+"_target_count"]; got != wantCount {
		t.Errorf("unresolved count = %v, want %d", got, wantCount)
	}
	if got := extra[resolutionUnresolved+"_targets_truncated"]; got != wantTruncated {
		t.Errorf("truncated = %v, want %v", got, wantTruncated)
	}
	if hasKey(extra, ambiguousTargetsKey) {
		t.Error("the ambiguous state must be cleared when recording an unresolved one")
	}
}
