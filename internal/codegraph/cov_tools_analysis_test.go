package codegraph

// Coverage-completing behaviour tests for the graph-analysis tools.
//
// tools_analysis_test.go pins the release-recorded responses; this file pins
// the contracts no recorded call can reach, because the fixture repository is
// never empty, never fully disconnected, never large enough to tie a stable
// sort against an unstable one, and never returns a store failure:
//
//   - what an EMPTY and a fully ISOLATED graph answer, and why a degree-0
//     symbol is a gap rather than the weakest hub;
//   - that a store read failure surfaces as a failed call rather than as a
//     plausible-looking empty payload, for every read each handler performs;
//   - that the traversal is really breadth- vs depth-first, and that the token
//     budget cuts it mid-traversal rather than after the fact;
//   - CPython's repr rules for the escapes the budget is counted in.
//
// Every expectation over Python semantics (repr output, len(str(entry))//4) is
// a golden taken from CPython itself, recorded next to the case.

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// covAnRankedTools are the three ranked analyses. They share a load step, so
// a store failure has to surface identically from all three.
var covAnRankedTools = []string{
	"get_hub_nodes_tool",
	"get_knowledge_gaps_tool",
	"get_surprising_connections_tool",
}

// ─── degenerate graphs ──────────────────────────────────────────────────────

// TestCovAnAnalysesOnABuiltButEmptyGraphReportNothingFound pins the built-but-
// empty graph, which is NOT the never-built one: the database exists and the
// tables are readable, they are simply empty. Every analysis must answer "0
// found" with the keys present, so a client can tell an indexed-but-empty
// repository from a transport failure.
func TestCovAnAnalysesOnABuiltButEmptyGraphReportNothingFound(t *testing.T) {
	engine := analysisSyntheticEngine(t, nil, nil, nil)

	hubs := analysisCall(t, engine, "get_hub_nodes_tool", `{}`)
	analysisRequireEqual(t, "hub_nodes count", float64(0), hubs["count"])
	analysisRequireEqual(t, "hub_nodes total", float64(0), hubs["total"])
	analysisRequireEqual(t, "hub_nodes truncated", false, hubs["truncated"])
	analysisRequireEqual(t, "hub_nodes summary",
		"0 hub node(s) ranked by degree", hubs["summary"])
	analysisRequireNames(t, "hub_nodes", hubs["hub_nodes"], nil)

	surprises := analysisCall(t, engine, "get_surprising_connections_tool", `{}`)
	analysisRequireEqual(t, "surprising total", float64(0), surprises["total"])
	analysisRequireEqual(t, "surprising summary",
		"0 surprising connection(s) ranked by surprise score", surprises["summary"])
	covAnRequireSources(t, "surprising_connections", surprises["surprising_connections"], nil)

	gaps := analysisCall(t, engine, "get_knowledge_gaps_tool", `{}`)
	analysisRequireEqual(t, "total_gaps", float64(0), gaps["total_gaps"])
	analysisRequireEqual(t, "gaps truncated", false, gaps["truncated"])
	wantSummary := map[string]any{
		"isolated_nodes": float64(0), "thin_communities": float64(0),
		"untested_hotspots": float64(0), "single_file_communities": float64(0),
	}
	if diff, ok := analysisDiffJSON("", wantSummary, gaps["summary"]); !ok {
		t.Fatalf("gap summary differs at %s", diff)
	}

	traversal := analysisCall(t, engine, "traverse_graph_tool", `{"query": "anything"}`)
	covAnRequireNoMatch(t, traversal, "anything")
}

// TestCovAnHubNodesDropDegreeZeroSymbols pins the division of labour between
// the two tools the file's comment names: a symbol with no edge at all is not
// the weakest hub, it is a gap. Ranking it last instead of dropping it would
// let a caller read "hub" off a completely disconnected symbol.
func TestCovAnHubNodesDropDegreeZeroSymbols(t *testing.T) {
	engine := analysisSyntheticEngine(t,
		[]analysisSeedNode{
			{kind: "Function", name: "a", file: "/r/a.go", language: "go"},
			{kind: "Function", name: "b", file: "/r/a.go", language: "go"},
			{kind: "Function", name: "c", file: "/r/a.go", language: "go"},
			{kind: "Function", name: "orphan", file: "/r/a.go", language: "go"},
		},
		[]analysisSeedEdge{
			analysisEdge("CALLS", "/r/a.go::a", "/r/a.go::b"),
			analysisEdge("CALLS", "/r/a.go::b", "/r/a.go::c"),
			analysisEdge("CALLS", "/r/a.go::a", "/r/a.go::c"),
		}, nil)

	hubs := analysisCall(t, engine, "get_hub_nodes_tool", `{}`)
	// All three tie at degree 2, so the stable sort keeps primary-key order.
	analysisRequireNames(t, "hub_nodes", hubs["hub_nodes"], []string{"a", "b", "c"})
	analysisRequireEqual(t, "total", float64(3), hubs["total"])

	gaps := covAnGapCategories(t, analysisCall(t, engine, "get_knowledge_gaps_tool", `{}`))
	analysisRequireNames(t, "isolated_nodes", gaps["isolated_nodes"], []string{"orphan"})
}

// TestCovAnAnalysesOnAFullyIsolatedGraphRankNoHubs pins the graph with nodes
// and no edges at all: every candidate is dropped by the degree filter, so the
// hub ranking is empty while every node is reported as a gap. `total` is the
// candidate count, not the node count.
func TestCovAnAnalysesOnAFullyIsolatedGraphRankNoHubs(t *testing.T) {
	nodes := make([]analysisSeedNode, 0, 3)
	for i := range 3 {
		nodes = append(nodes, analysisSeedNode{
			kind: "Function", name: fmt.Sprintf("iso%d", i),
			file: "/r/iso.go", language: "go"})
	}
	engine := analysisSyntheticEngine(t, nodes, nil, nil)

	hubs := analysisCall(t, engine, "get_hub_nodes_tool", `{}`)
	analysisRequireNames(t, "hub_nodes", hubs["hub_nodes"], nil)
	analysisRequireEqual(t, "total", float64(0), hubs["total"])
	analysisRequireEqual(t, "summary", "0 hub node(s) ranked by degree", hubs["summary"])

	gaps := covAnGapCategories(t, analysisCall(t, engine, "get_knowledge_gaps_tool", `{}`))
	analysisRequireNames(t, "isolated_nodes", gaps["isolated_nodes"],
		analysisGeneratedNames("iso%d", 3))
}

// ─── store-failure propagation ──────────────────────────────────────────────

// TestCovAnAnalysesPropagateAnUnopenableStore pins the one failure every tool
// in the file shares: the database exists, so the graph reads as built, but it
// cannot be opened. That must fail the call. Degrading to an empty payload
// here would be indistinguishable from a genuinely empty repository, which is
// the answer the unbuilt case is allowed to give and this case is not.
func TestCovAnAnalysesPropagateAnUnopenableStore(t *testing.T) {
	arguments := map[string]string{"traverse_graph_tool": `{"query": "Login"}`}
	for _, tool := range append([]string{"traverse_graph_tool"}, covAnRankedTools...) {
		t.Run(tool, func(t *testing.T) {
			args, ok := arguments[tool]
			if !ok {
				args = `{}`
			}
			covAnRequireCallFails(t, unreadableEngine(t), tool, args)
		})
	}
}

// TestCovAnAnalysesPropagateExtractedLayerReadFailures pins the two reads the
// shared load performs. Each is injected on its own, so a handler that
// swallowed one of them would still fail this test on the other.
func TestCovAnAnalysesPropagateExtractedLayerReadFailures(t *testing.T) {
	cases := map[string]*fakeStore{
		"node read": {allNodesErr: errFake},
		"edge read": {allEdgesErr: errFake},
	}
	for label, store := range cases {
		for _, tool := range covAnRankedTools {
			t.Run(label+"/"+tool, func(t *testing.T) {
				engine := engineWithStore(t, t.TempDir(), store)
				err := covAnRequireCallFails(t, engine, tool, `{}`)
				if !errors.Is(err, errFake) {
					t.Fatalf("err = %v, want the injected %s failure", err, label)
				}
			})
		}
	}
}

// TestCovAnKnowledgeGapsPropagateCommunityReadFailure pins the read only the
// gap analysis performs: the community table. The extracted layer loads
// cleanly here, so a handler that reported the categories it could compute and
// silently dropped thin_communities and single_file_communities would pass
// every other test in the package.
func TestCovAnKnowledgeGapsPropagateCommunityReadFailure(t *testing.T) {
	engine := covAnFaultyEngine(t, covAnFaultyStore{communitiesErr: errFake},
		[]analysisSeedNode{{kind: "Function", name: "a", file: "/r/a.go", language: "go"}},
		nil)
	err := covAnRequireCallFails(t, engine, "get_knowledge_gaps_tool", `{}`)
	if !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected communities-read failure", err)
	}
}

// TestCovAnTraverseGraphPropagatesStoreFailures pins every read the traversal
// performs after the seed is resolved. They are injected separately because
// they happen at different points of the walk: the search decides whether
// there is a start node at all, and the node and the two edge lookups run once
// per visited node, so swallowing any one of them would return a SHORTER
// traversal that still looks well-formed.
func TestCovAnTraverseGraphPropagatesStoreFailures(t *testing.T) {
	cases := map[string]covAnFaultyStore{
		"seed search":    {searchErr: errFake},
		"node lookup":    {nodeErr: errFake},
		"outgoing edges": {outgoingErr: errFake},
		"incoming edges": {incomingErr: errFake},
	}
	nodes := []analysisSeedNode{
		{kind: "Function", name: "alpha", file: "/r/a.go", language: "go"},
		{kind: "Function", name: "beta", file: "/r/a.go", language: "go"},
	}
	edges := []analysisSeedEdge{analysisEdge("CALLS", "/r/a.go::alpha", "/r/a.go::beta")}
	for label, faults := range cases {
		t.Run(label, func(t *testing.T) {
			engine := covAnFaultyEngine(t, faults, nodes, edges)
			err := covAnRequireCallFails(t, engine, "traverse_graph_tool",
				`{"query": "alpha"}`)
			if !errors.Is(err, errFake) {
				t.Fatalf("err = %v, want the injected %s failure", err, label)
			}
		})
	}
}

// ─── traversal ──────────────────────────────────────────────────────────────

// covAnTraverseNodes is a graph whose every node has at most one outgoing and
// one incoming edge, so the queue's contents never depend on the order SQLite
// happens to return one node's edges in. The seed has one of each, which is
// what makes the two modes diverge: the outgoing neighbour is queued first, so
// breadth-first takes it first and depth-first takes the incoming one.
var covAnTraverseNodes = []analysisSeedNode{
	{kind: "Function", name: "rootFn", file: "/r/a.go", language: "go"},
	{kind: "Function", name: "calleeFn", file: "/r/a.go", language: "go"},
	{kind: "Function", name: "callerFn", file: "/r/a.go", language: "go"},
	{kind: "Function", name: "deepFn", file: "/r/a.go", language: "go"},
}

var covAnTraverseEdges = []analysisSeedEdge{
	analysisEdge("CALLS", "/r/a.go::rootFn", "/r/a.go::calleeFn"),
	analysisEdge("CALLS", "/r/a.go::callerFn", "/r/a.go::rootFn"),
	analysisEdge("CALLS", "/r/a.go::calleeFn", "/r/a.go::deepFn"),
}

// TestCovAnTraverseGraphOrdersByMode pins that the mode is really the
// traversal discipline and not a label. Both modes reach the same four nodes
// at the same depths; only the ORDER differs, and it differs in the one place
// the disciplines disagree — whether the seed's neighbours are expanded before
// or after the deeper node behind the first of them.
func TestCovAnTraverseGraphOrdersByMode(t *testing.T) {
	cases := []struct {
		mode  string
		order []string
	}{
		{"bfs", []string{"rootFn", "calleeFn", "callerFn", "deepFn"}},
		{"dfs", []string{"rootFn", "callerFn", "calleeFn", "deepFn"}},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			engine := analysisSyntheticEngine(t, covAnTraverseNodes, covAnTraverseEdges, nil)
			payload := analysisCall(t, engine, "traverse_graph_tool",
				fmt.Sprintf(`{"query": "rootFn", "mode": %q}`, tc.mode))
			analysisRequireEqual(t, "start_node", "/r/a.go::rootFn", payload["start_node"])
			analysisRequireEqual(t, "mode", tc.mode, payload["mode"])
			analysisRequireEqual(t, "nodes_visited", float64(4), payload["nodes_visited"])
			analysisRequireEqual(t, "truncated", false, payload["truncated"])
			analysisRequireNames(t, "traversal", payload["traversal"], tc.order)
			covAnRequireDepths(t, payload["traversal"],
				map[string]float64{"rootFn": 0, "calleeFn": 1, "callerFn": 1, "deepFn": 2})
		})
	}
}

// covAnChainCosts are the per-entry token costs of the four nodes of the chain
// below, as CPython computes them:
//
//	len(str({'name': 'alpha', 'qualified_name': '/r/a.go::alpha',
//	         'kind': 'Function', 'file': '/r/a.go', 'depth': 0})) // 4
//
// giving 26, 25, 26, 26 at depths 0..3. The paths are seeded literals rather
// than temp-root paths precisely so these numbers are stable on every host.
var covAnChainCosts = []int{26, 25, 26, 26}

// TestCovAnTraverseGraphTokenBudgetCutsMidTraversal pins where the budget
// cuts. A budget of exactly the first two entries keeps both — the check is
// `>`, not `>=` — and drops the third WITHOUT emitting it, which is the
// difference between a truncated traversal and one that overshoots its budget
// by an entry. The walk stops there rather than skipping the expensive node
// and continuing, so the result is always a connected prefix.
func TestCovAnTraverseGraphTokenBudgetCutsMidTraversal(t *testing.T) {
	chain := []string{"alpha", "beta", "gamma", "delta"}
	nodes := make([]analysisSeedNode, 0, len(chain))
	for _, name := range chain {
		nodes = append(nodes, analysisSeedNode{
			kind: "Function", name: name, file: "/r/a.go", language: "go"})
	}
	edges := make([]analysisSeedEdge, 0, len(chain)-1)
	for i := range len(chain) - 1 {
		edges = append(edges, analysisEdge("CALLS",
			"/r/a.go::"+chain[i], "/r/a.go::"+chain[i+1]))
	}

	exact := covAnChainCosts[0] + covAnChainCosts[1]
	cases := []struct {
		label     string
		budget    int
		kept      []string
		truncated bool
	}{
		{"first entry alone overshoots", covAnChainCosts[0] - 1, nil, true},
		{"budget equal to two entries keeps both", exact, chain[:2], true},
		{"generous budget keeps the whole chain", 1000, chain, false},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			engine := analysisSyntheticEngine(t, nodes, edges, nil)
			payload := analysisCall(t, engine, "traverse_graph_tool",
				fmt.Sprintf(`{"query": "alpha", "token_budget": %d}`, tc.budget))
			analysisRequireNames(t, "traversal", payload["traversal"], tc.kept)
			analysisRequireEqual(t, "nodes_visited", float64(len(tc.kept)),
				payload["nodes_visited"])
			analysisRequireEqual(t, "truncated", tc.truncated, payload["truncated"])
		})
	}
}

// TestCovAnTraverseGraphAmbiguousSeedFollowsTheSearchRanking pins how an
// ambiguous seed is resolved. Two nodes are named `Handler`; the query is
// PascalCase, so the search boosts type-like kinds and the Type wins even
// though the Function is the lower-numbered row. Taking the store's first
// matching row instead would start the walk somewhere else.
func TestCovAnTraverseGraphAmbiguousSeedFollowsTheSearchRanking(t *testing.T) {
	engine := analysisSyntheticEngine(t,
		[]analysisSeedNode{
			{kind: "Function", name: "Handler", file: "/r/a.go", language: "go"},
			{kind: "Type", name: "Handler", file: "/r/b.go", language: "go"},
		}, nil, nil)

	payload := analysisCall(t, engine, "traverse_graph_tool", `{"query": "Handler"}`)
	analysisRequireEqual(t, "start_node", "/r/b.go::Handler", payload["start_node"])
	analysisRequireEqual(t, "nodes_visited", float64(1), payload["nodes_visited"])
	analysisRequireNames(t, "traversal", payload["traversal"], []string{"Handler"})
}

// TestCovAnTraverseGraphSeedMatchingNothingIsNotAnError pins the unmatched
// seed on a POPULATED graph, which the unbuilt case cannot tell apart from an
// empty index: the graph has nodes, none of them matches, and the answer is
// still the bare {error, nodes} pair with no `status` and no failed call.
func TestCovAnTraverseGraphSeedMatchingNothingIsNotAnError(t *testing.T) {
	engine := analysisSyntheticEngine(t,
		[]analysisSeedNode{
			{kind: "Function", name: "alpha", file: "/r/a.go", language: "go"},
		}, nil, nil)

	payload := analysisCall(t, engine, "traverse_graph_tool", `{"query": "zzzznomatch"}`)
	covAnRequireNoMatch(t, payload, "zzzznomatch")
}

// ─── surprise scoring ───────────────────────────────────────────────────────

// TestCovAnSurpriseIgnoresExtensionlessPaths pins the language probe's
// empty-suffix guard. A path with no dot at all yields no language, and an
// unknown language must NOT count as a different one: a Makefile calling into
// Go is not evidence of a cross-language coupling, it is evidence that the
// probe cannot tell. The `.py` pair in the same graph shows the factor does
// fire when both suffixes are known and differ.
func TestCovAnSurpriseIgnoresExtensionlessPaths(t *testing.T) {
	engine := analysisSyntheticEngine(t,
		[]analysisSeedNode{
			{kind: "Function", name: "orchestrate", file: "/r/Makefile", community: 1},
			{kind: "Function", name: "build", file: "/r/build.go", language: "go", community: 2},
			{kind: "Function", name: "runner", file: "/r/app.py", language: "python", community: 1},
			{kind: "Function", name: "helper", file: "/r/help.go", language: "go", community: 2},
		},
		[]analysisSeedEdge{
			analysisEdge("CALLS", "/r/Makefile::orchestrate", "/r/build.go::build"),
			analysisEdge("CALLS", "/r/app.py::runner", "/r/help.go::helper"),
		},
		[]analysisSeedCommunity{{1, "drivers"}, {2, "go"}})

	payload := analysisCall(t, engine, "get_surprising_connections_tool", `{"top_n": 10}`)
	// Golden from analysisOracleNote: the extensionless edge scores
	// cross-community only, so it ranks BELOW the cross-language pair.
	const wantJSON = `[
	 {"source":"runner","source_qualified":"/r/app.py::runner",
	  "target":"helper","target_qualified":"/r/help.go::helper","edge_kind":"CALLS",
	  "surprise_score":0.5,"reasons":["cross-community","cross-language"],
	  "source_community":1,"target_community":2},
	 {"source":"orchestrate","source_qualified":"/r/Makefile::orchestrate",
	  "target":"build","target_qualified":"/r/build.go::build","edge_kind":"CALLS",
	  "surprise_score":0.3,"reasons":["cross-community"],
	  "source_community":1,"target_community":2}
	]`
	if diff, ok := analysisDiffJSON("", analysisParseJSON(t, wantJSON),
		payload["surprising_connections"]); !ok {
		t.Fatalf("surprising_connections differs at %s (golden: %s)", diff, analysisOracleNote)
	}
	analysisRequireEqual(t, "total", float64(2), payload["total"])
}

// covAnTieLeaves is large enough that an unstable sort would visibly permute
// the equally scored edges: Go's sort falls back to insertion sort, which is
// accidentally stable, for short slices only.
const covAnTieLeaves = 20

// TestCovAnSurpriseTieBreakKeepsEdgeOrder pins the tie-break the file's
// comment calls out. Every edge below scores identically, so the only thing
// that can order them is the store's primary-key order — and the proof that
// nothing else does is that seeding the SAME graph with the edges reversed
// reverses the response. A hidden secondary key (a name, a degree, a qualified
// name) would return one of the two orders for both seedings.
func TestCovAnSurpriseTieBreakKeepsEdgeOrder(t *testing.T) {
	forward := analysisGeneratedNames("leaf%02d", covAnTieLeaves)
	reversed := make([]string, 0, len(forward))
	for i := len(forward) - 1; i >= 0; i-- {
		reversed = append(reversed, forward[i])
	}
	for _, order := range [][]string{forward, reversed} {
		t.Run("seeded "+order[0]+" first", func(t *testing.T) {
			nodes, edges := covAnTieGraph(order)
			engine := analysisSyntheticEngine(t, nodes, edges,
				[]analysisSeedCommunity{{1, "core"}, {2, "leaves"}})
			payload := analysisCall(t, engine, "get_surprising_connections_tool",
				`{"top_n": 100}`)
			analysisRequireEqual(t, "total", float64(covAnTieLeaves), payload["total"])
			covAnRequireSources(t, "surprising_connections",
				payload["surprising_connections"], order)
			covAnRequireUniformScore(t, payload["surprising_connections"], 0.5)
		})
	}
}

// covAnTieGraph links every named leaf to one core node across a community
// boundary, in the given order. All the edges share a language and a test
// boundary, so each scores cross-community plus peripheral-to-hub and nothing
// else: 0.5, identically.
func covAnTieGraph(leaves []string) ([]analysisSeedNode, []analysisSeedEdge) {
	nodes := []analysisSeedNode{
		{kind: "Function", name: "core", file: "/r/core.go", language: "go", community: 1},
	}
	edges := make([]analysisSeedEdge, 0, len(leaves))
	for _, name := range leaves {
		nodes = append(nodes, analysisSeedNode{
			kind: "Function", name: name, file: "/r/leaf.go", language: "go", community: 2})
		edges = append(edges, analysisEdge("CALLS", "/r/leaf.go::"+name, "/r/core.go::core"))
	}
	return nodes, edges
}

// ─── the one-element set ────────────────────────────────────────────────────

// TestCovAnSoleKeyReturnsTheOnlyMember pins the helper's whole contract. It
// stands in for upstream's `next(iter(files))`, which raises on an empty set;
// the port answers the empty string instead, so a community row whose file set
// has been emptied reports a blank file rather than aborting the analysis that
// was asked for every other category too.
func TestCovAnSoleKeyReturnsTheOnlyMember(t *testing.T) {
	cases := map[string]struct {
		set  map[string]bool
		want string
	}{
		"one member":  {map[string]bool{"/r/only.go": true}, "/r/only.go"},
		"empty set":   {map[string]bool{}, ""},
		"nil set":     {nil, ""},
		"false value": {map[string]bool{"/r/present.go": false}, "/r/present.go"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			if got := soleKey(tc.set); got != tc.want {
				t.Fatalf("soleKey = %q, want %q", got, tc.want)
			}
		})
	}
}

// ─── CPython repr ───────────────────────────────────────────────────────────

// TestCovAnPythonStringReprMatchesCPython pins the repr rules the token budget
// is counted in. Every `want` is CPython's own `repr(value)`, so the table is
// a transcript rather than a restatement of the implementation.
//
// The cases are the decisions the function actually makes: which quote to use,
// which characters get a short escape, and which of \xNN / \uXXXX / \UXXXXXXXX
// an unprintable code point gets — the last of which depends on the code
// point's width, not on its category.
func TestCovAnPythonStringReprMatchesCPython(t *testing.T) {
	cases := []struct {
		label string
		value string
		want  string
	}{
		{"plain ascii", "hello", `'hello'`},
		{"empty", "", `''`},
		{"single quote switches to double", "it's", `"it's"`},
		{"double quote stays single quoted", `say"hi"`, `'say"hi"'`},
		{"both quotes escape the single", "both'and\"", `'both\'and"'`},
		{"backslash escaped", `a\b`, `'a\\b'`},
		{"tab short escape", "a\tb", `'a\tb'`},
		{"newline short escape", "a\nb", `'a\nb'`},
		{"carriage return short escape", "a\rb", `'a\rb'`},
		{"null byte hex escape", "a\x00b", `'a\x00b'`},
		{"unit separator hex escape", "a\x1fb", `'a\x1fb'`},
		{"delete hex escape", "a\x7fb", `'a\x7fb'`},
		{"latin1 nbsp hex escape", "a\u00a0b", `'a\xa0b'`},
		{"printable non ascii kept", "na\u00efve", `'naïve'`},
		{"bmp unprintable u escape", "a\u200bb", `'a\u200bb'`},
		{"line separator u escape", "a\u2028b", `'a\u2028b'`},
		{"astral printable kept", "x\U0001f600", `'x😀'`},
		{"astral format tag U escape", "x\U000e0001", `'x\U000e0001'`},
		{"astral unassigned U escape", "x\U0010fffe", `'x\U0010fffe'`},
		{"quote selection survives escapes", "it's\ta\nb", `"it's\ta\nb"`},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if got := pythonStringRepr(tc.value); got != tc.want {
				t.Fatalf("pythonStringRepr(%q) = %s, want %s (CPython repr)",
					tc.value, got, tc.want)
			}
		})
	}
}

// TestCovAnTokenCostCountsEscapeSequences pins the consequence of those rules
// for the budget: an escape sequence costs its RENDERED code points, not the
// one character it stands for, so a node whose path carries a newline or an
// astral code point is charged more than its length suggests. The entry's two
// path fields reach the repr unsanitized, which is what makes this reachable
// from a real graph.
//
// The expected values are CPython's `len(str(entry)) // 4` over the same dict.
func TestCovAnTokenCostCountsEscapeSequences(t *testing.T) {
	cases := []struct {
		label         string
		qualifiedName string
		file          string
		depth         int
		want          int
	}{
		{"newline in file path", "/r/a.go::Login", "/r/two\nlines.go", 1, 28},
		{"carriage return in file path", "/r/a.go::Login", "/r/two\rlines.go", 2, 28},
		{"control char in qualified name", "/r/a\x01.go::Login", "/r/a\x01.go", 3, 28},
		{"delete char in file path", "/r/a.go::Login", "/r/del\x7f.go", 4, 27},
		{"astral format tag in file path", "/r/a.go::Login", "/r/tag\U000e0001.go", 5, 29},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			entry := traversalEntry{
				name:          "Login",
				qualifiedName: tc.qualifiedName,
				kind:          "Function",
				file:          tc.file,
				depth:         tc.depth,
			}
			if got := entry.tokenCost(); got != tc.want {
				t.Fatalf("tokenCost = %d, want %d (Python len(str(entry))//4)", got, tc.want)
			}
		})
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

// covAnFaultyStore wraps a real store and fails exactly the reads it is asked
// to fail. Wrapping rather than faking keeps every other read genuine, so a
// propagation test drives the handler's real path right up to the failure.
type covAnFaultyStore struct {
	graphstore.Store
	searchErr      error
	nodeErr        error
	outgoingErr    error
	incomingErr    error
	communitiesErr error
}

func (s *covAnFaultyStore) SearchNodesFTS(query string, limit int) ([]int64, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.Store.SearchNodesFTS(query, limit)
}

func (s *covAnFaultyStore) GetNode(qualifiedName string) (*graphstore.GraphNode, error) {
	if s.nodeErr != nil {
		return nil, s.nodeErr
	}
	return s.Store.GetNode(qualifiedName)
}

func (s *covAnFaultyStore) GetEdgesBySource(qualifiedName string) ([]graphstore.GraphEdge, error) {
	if s.outgoingErr != nil {
		return nil, s.outgoingErr
	}
	return s.Store.GetEdgesBySource(qualifiedName)
}

func (s *covAnFaultyStore) GetEdgesByTarget(qualifiedName string) ([]graphstore.GraphEdge, error) {
	if s.incomingErr != nil {
		return nil, s.incomingErr
	}
	return s.Store.GetEdgesByTarget(qualifiedName)
}

func (s *covAnFaultyStore) ReadCommunities() ([]graphstore.CommunityRow, error) {
	if s.communitiesErr != nil {
		return nil, s.communitiesErr
	}
	return s.Store.ReadCommunities()
}

// covAnFaultyEngine seeds a real graph and then interposes the fault wrapper,
// so the failing read is the only thing about the engine that is not real.
func covAnFaultyEngine(t *testing.T, faults covAnFaultyStore,
	nodes []analysisSeedNode, edges []analysisSeedEdge) *Engine {
	t.Helper()
	engine := analysisSyntheticEngine(t, nodes, edges, nil)
	faults.Store = engine.store
	engine.store = &faults
	return engine
}

// covAnRequireCallFails runs a native handler and requires a failed call. A
// payload here would be a plausible-looking answer built from a read that did
// not happen, which is the failure mode every propagation test above exists
// to rule out.
func covAnRequireCallFails(t *testing.T, engine *Engine, tool, arguments string) error {
	t.Helper()
	published, ok := crgrelease.Lookup(tool)
	if !ok {
		t.Fatalf("%s is not published by the pinned release", tool)
	}
	args, bindErr := published.Bind(json.RawMessage(arguments))
	if bindErr != nil {
		t.Fatalf("bind %s %s: %v", tool, arguments, bindErr)
	}
	handler, native := toolHandlers[tool]
	if !native {
		t.Fatalf("%s has no native handler", tool)
	}
	result, err := handler(engine, args)
	if err == nil {
		t.Fatalf("%s returned payload %v, want the store failure reported", tool, result)
	}
	return err
}

// covAnRequireNoMatch asserts the bare pair an unresolved seed answers with:
// an error message, an empty node list, and nothing else.
func covAnRequireNoMatch(t *testing.T, payload map[string]any, query string) {
	t.Helper()
	want := map[string]any{
		"error": fmt.Sprintf("No node matching '%s'", query),
		"nodes": []any{},
	}
	if diff, ok := analysisDiffJSON("", want, payload); !ok {
		t.Fatalf("unmatched-seed payload differs at %s", diff)
	}
}

// covAnGapCategories unwraps the `gaps` object of a knowledge-gaps response.
func covAnGapCategories(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	gaps, ok := payload["gaps"].(map[string]any)
	if !ok {
		t.Fatalf("gaps is %T, want object", payload["gaps"])
	}
	return gaps
}

// covAnRequireSources asserts the `source` of each surprising connection, in
// order.
func covAnRequireSources(t *testing.T, field string, rows any, want []string) {
	t.Helper()
	got := covAnStringField(t, field, rows, "source")
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s sources\n got: %v\nwant: %v", field, got, want)
	}
}

// covAnRequireUniformScore asserts every row carries the same surprise score,
// which is what makes the order above a tie-break rather than a ranking.
func covAnRequireUniformScore(t *testing.T, rows any, want float64) {
	t.Helper()
	list, ok := rows.([]any)
	if !ok {
		t.Fatalf("surprising_connections is %T, want array", rows)
	}
	for i, row := range list {
		entry, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("row %d is %T, want object", i, row)
		}
		if entry["surprise_score"] != want {
			t.Fatalf("row %d surprise_score = %v, want %v", i, entry["surprise_score"], want)
		}
	}
}

// covAnRequireDepths asserts the depth recorded for each named traversal row,
// so a mode change that reordered the walk but also changed the depths a node
// is reported at cannot pass as a reordering.
func covAnRequireDepths(t *testing.T, rows any, want map[string]float64) {
	t.Helper()
	list, ok := rows.([]any)
	if !ok {
		t.Fatalf("traversal is %T, want array", rows)
	}
	for i, row := range list {
		entry, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("traversal row %d is %T, want object", i, row)
		}
		name, _ := entry["name"].(string)
		expected, named := want[name]
		if !named {
			t.Fatalf("traversal row %d is unexpected node %q", i, name)
		}
		if entry["depth"] != expected {
			t.Fatalf("%s depth = %v, want %v", name, entry["depth"], expected)
		}
	}
}

// covAnStringField collects one string field from every row of a response
// list, in order.
func covAnStringField(t *testing.T, field string, rows any, key string) []string {
	t.Helper()
	list, ok := rows.([]any)
	if !ok {
		t.Fatalf("%s is %T, want array", field, rows)
	}
	values := make([]string, 0, len(list))
	for _, row := range list {
		entry, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("%s entry is %T, want object", field, row)
		}
		value, _ := entry[key].(string)
		values = append(values, value)
	}
	return values
}
