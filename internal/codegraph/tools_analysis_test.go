package codegraph

// Parity tests for the graph-analysis tools of code-review-graph v2.3.8.
//
// The primary assertion is the table test over every recorded call fixture:
// testdata/crg-release/v2.3.8/calls/<tool>__<case>.json holds the release's
// REAL response, and a native handler has to reproduce the whole payload, not
// a selection of keys.
//
// Each case seeds the store directly from graph.json rather than running the
// scanner or the postprocess derivations, so a failure here is this lane's
// failure. Fixtures are discovered by glob rather than named, so cases other
// lanes add for these tools are picked up without an edit.
//
// Two behaviours the fixture repository cannot reach are pinned against the
// release separately, because that repository scores zero surprising
// connections and has no thin community: the composite surprise score and the
// gap categories are compared to goldens taken from the release's own
// analysis.find_surprising_connections / find_knowledge_gaps run over
// hand-seeded graphs (see analysisOracleNote).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// analysisTools are the tools this lane owns. get_suggested_questions_tool is
// listed deliberately: its fixture must exist and it must NOT have a native
// handler, which is how the bridge-only decision stays pinned.
var analysisTools = []string{
	"get_hub_nodes_tool",
	"get_knowledge_gaps_tool",
	"get_surprising_connections_tool",
	"traverse_graph_tool",
	"get_suggested_questions_tool",
}

// analysisBridgeOnlyTools must have no native handler. Both rank by networkx
// betweenness centrality, which above 5000 nodes is estimated from an unseeded
// random sample, so the release does not reproduce its own answer.
var analysisBridgeOnlyTools = map[string]string{
	"get_suggested_questions_tool": "its bridge_node questions come from the " +
		"sampling betweenness estimator",
	"get_bridge_nodes_tool": "betweenness is sampled above 5000 nodes",
}

// analysisOracleNote records how the hand-seeded goldens below were obtained,
// so they can be re-derived rather than trusted.
//
// Each was produced by inserting the rows named in the test directly into a
// v2.3.8 GraphStore and calling the release's own analysis helper:
//
//	from code_review_graph.analysis import (
//	    find_knowledge_gaps, find_hub_nodes, find_surprising_connections)
//	from code_review_graph.graph import GraphStore
//	store = GraphStore(path)
//	store._conn.execute("INSERT INTO nodes (kind, name, qualified_name, "
//	    "file_path, line_start, line_end, language, is_test, updated_at, "
//	    "community_id) VALUES (?,?,?,?,1,2,?,?,0.0,?)", row)
//	store._conn.execute("INSERT INTO edges (kind, source_qualified, "
//	    "target_qualified, file_path, line, updated_at) "
//	    "VALUES (?,?,?,'/r/a.go',1,0.0)", row)
//	store._conn.execute("INSERT INTO communities (id, name, level, cohesion, "
//	    "size, dominant_language, description) VALUES (?,?,0,0.5,0,'go','d')", row)
//	store._conn.commit()
//	find_surprising_connections(store, top_n=10**9)
const analysisOracleNote = "v2.3.8 analysis.py over a hand-seeded GraphStore"

// ─── the table test ──────────────────────────────────────────────────────────

func TestNativeAnalysisToolsMatchReleaseCallFixtures(t *testing.T) {
	fixtures := analysisCallFixtures(t)
	if len(fixtures) == 0 {
		t.Fatal("no call fixtures found for the analysis tools")
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		seen[fixture.tool] = true
		t.Run(fixture.tool+"__"+fixture.caseName, func(t *testing.T) {
			runAnalysisCallFixture(t, fixture)
		})
	}
	for _, tool := range analysisTools {
		if !seen[tool] {
			t.Errorf("no call fixture covers %s", tool)
		}
	}
}

// runAnalysisCallFixture replays one recorded release call against the native
// handler: a bridge-only tool must have no native handler at all, and any other
// tool must be published, bind its recorded arguments, and answer exactly what
// the release recorded.
func runAnalysisCallFixture(t *testing.T, fixture analysisCallFixture) {
	t.Helper()

	if reason, bridgeOnly := analysisBridgeOnlyTools[fixture.tool]; bridgeOnly {
		if _, native := toolHandlers[fixture.tool]; native {
			t.Fatalf("%s must stay on the bridge: %s", fixture.tool, reason)
		}
		return
	}
	handler, native := toolHandlers[fixture.tool]
	if !native {
		t.Fatalf("%s has no native handler", fixture.tool)
	}
	tool, published := crgrelease.Lookup(fixture.tool)
	if !published {
		t.Fatalf("%s is not published by the pinned release", fixture.tool)
	}
	args, bindErr := tool.Bind(fixture.arguments)
	if bindErr != nil {
		t.Fatalf("bind %s: %v", fixture.tool, bindErr)
	}

	engine, root := analysisFixtureEngine(t)
	result, err := handler(engine, args)

	if fixture.isError {
		assertAnalysisFixtureError(t, fixture, result, err)
		return
	}
	if err != nil {
		t.Fatalf("%s: %v", fixture.tool, err)
	}
	if !fixture.hasStructured {
		t.Fatal("fixture records no structured content for a success result")
	}
	want := fixture.wantPayload(t, root)
	got := analysisJSONValue(t, result)
	if diff, ok := analysisDiffJSON("", want, got); !ok {
		t.Fatalf("payload differs at %s%s", diff, analysisTraverseHint(fixture.tool))
	}
}

// assertAnalysisFixtureError checks the arm of a recorded call that failed. A
// bound violation escapes upstream's try block, so the release answers with a
// transport error and NO payload. The handler owns only the message; the
// "Error calling tool '<tool>': " prefix is the server envelope's.
func assertAnalysisFixtureError(t *testing.T, fixture analysisCallFixture, result any, err error) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected an error result, got payload %v", result)
	}
	if fixture.hasStructured {
		t.Fatal("fixture records structured content for an error result")
	}
	if !strings.HasSuffix(fixture.contentText, err.Error()) {
		t.Fatalf("error message\n got: %s\nwant suffix of: %s",
			err.Error(), fixture.contentText)
	}
}

// analysisTraverseHint names the one way a traverse comparison can fail for a
// reason that is not a port bug: the token budget is spent on a dict holding
// two ABSOLUTE paths, so an unusually long temp root makes the default 2000
// budget cut a traversal the release recorded whole.
func analysisTraverseHint(tool string) string {
	if tool != "traverse_graph_tool" {
		return ""
	}
	return "\n(if the difference is `truncated`/`nodes_visited`, check whether " +
		"this host's temp root is long enough to change the token budget: each " +
		"entry costs len(str(entry))//4 over two absolute paths)"
}

// ─── token estimation ────────────────────────────────────────────────────────

// TestTraversalEntryTokenCostMatchesPython pins upstream's
// `len(str(entry)) // 4` against CPython, because that number decides which
// nodes fit the budget and therefore which nodes a client sees.
//
// The expected values were produced by building the same dict in Python
// (names passed through graph._sanitize_name first, exactly as the handler
// does) and printing len(str(entry)) // 4. The cases cover what makes the
// repr non-obvious: quote selection, escaped quotes and backslashes, a kept
// tab, printable non-ASCII and astral code points counted as ONE character
// each, unprintable code points escaped to \xNN / \uXXXX, a control character
// the sanitizer removes before the repr sees it, the 256-code-point name
// truncation, and an empty string.
func TestTraversalEntryTokenCostMatchesPython(t *testing.T) {
	cases := []struct {
		label         string
		name          string
		qualifiedName string
		kind          string
		file          string
		depth         int
		want          int
	}{
		{"plain", "Login", "/r/pkg/auth/auth.go::Login", "Function", "/r/pkg/auth/auth.go", 0, 32},
		{"two digit depth", "main", "/tmp/x::main", "Function", "/tmp/x", 6, 25},
		{"single quote switches to double", "it's", "/r/a.go::it's", "Function", "/r/a.go", 1, 25},
		{"double quote stays single quoted", `say"hi"`, `/r/a.go::say"hi"`, "Class", "/r/a.go", 2, 26},
		{"both quotes escape the single", "both'and\"", "/r/a.go::q", "Class", "/r/a.go", 3, 25},
		{"backslash escaped", `back\slash`, `/r/a.go::back\slash`, "Function", "/r/a.go", 4, 29},
		{"tab survives sanitizing and escapes", "tab\there", "/r/a.go::tab\there", "Function", "/r/a.go", 5, 28},
		{"printable non-ascii counted as one", "naïve_Ünïcode", "/r/ünï.go::naïve_Ünïcode", "Function", "/r/ünï.go", 2, 31},
		{"astral printable", "emoji\U0001F600", "/r/a.go::emoji", "Function", "/r/a.go", 1, 26},
		{"unprintable format char", "zero\u200bwidth", "/r/a.go::z", "Function", "/r/a.go", 1, 27},
		{"unprintable latin1 space", "nbsp\u00a0here", "/r/a.go::n", "Function", "/r/a.go", 1, 26},
		{"control char stripped by sanitizer", "ctrl\x01x", "/r/a.go::c", "Function", "/r/a.go", 1, 25},
		{"name truncated at 256", strings.Repeat("L", 300), "/r/a.go::long", "Function", "/r/a.go", 1, 88},
		{"empty name", "", "/r/a.go::", "File", "/r/a.go", 0, 22},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			entry := traversalEntry{
				name:          SanitizeName(tc.name),
				qualifiedName: tc.qualifiedName,
				kind:          tc.kind,
				file:          tc.file,
				depth:         tc.depth,
			}
			if got := entry.tokenCost(); got != tc.want {
				t.Fatalf("tokenCost = %d, want %d (Python len(str(entry))//4)", got, tc.want)
			}
		})
	}
}

// ─── surprise scoring ────────────────────────────────────────────────────────

// TestSurprisingConnectionsScoreEveryFactor pins the composite score against
// the release. The fixture repository cannot: every one of its scorable edges
// stays inside one community, one language and one test boundary, so its
// recorded response is an empty list and would accept any scorer at all.
//
// The graph below reaches all five factors and the three ways a candidate is
// rejected: an endpoint that is not a node (a bare cross-package target), an
// endpoint with no community (which must NOT count as cross-community), and
// an edge that scores zero.
func TestSurprisingConnectionsScoreEveryFactor(t *testing.T) {
	nodes := []analysisSeedNode{
		{kind: "Function", name: "hub", file: "/r/lib.go", language: "go", community: 2},
		{kind: "Function", name: "periph", file: "/r/app.py", language: "python", community: 1},
		{kind: "Type", name: "TypeThing", file: "/r/lib.go", language: "go", community: 2},
		{kind: "Test", name: "test_hub", file: "/r/app_test.py", language: "python", isTest: true, community: 1},
		{kind: "Function", name: "noComm", file: "/r/lib.go", language: "go"},
	}
	edges := []analysisSeedEdge{
		analysisEdge("CALLS", "/r/app.py::periph", "/r/lib.go::hub"),
		analysisEdge("CALLS", "/r/lib.go::TypeThing", "/r/lib.go::hub"),
		analysisEdge("CALLS", "/r/app_test.py::test_hub", "/r/lib.go::hub"),
		analysisEdge("IMPORTS_FROM", "/r/lib.go::hub", "/r/app.py::periph"),
		analysisEdge("CALLS", "/r/lib.go::noComm", "/r/lib.go::hub"),
		// Target is not a node: never scored, but it still lifts hub's degree.
		analysisEdge("CALLS", "/r/lib.go::hub", "/r/nosuch.go::ghost"),
	}
	// Filler callers push hub over the degree-10 hub threshold and keep the
	// median degree low enough that the threshold's floor is what applies.
	for i := 1; i <= 8; i++ {
		name := fmt.Sprintf("f%d", i)
		nodes = append(nodes, analysisSeedNode{
			kind: "Function", name: name, file: "/r/lib.go", language: "go", community: 2,
		})
		edges = append(edges, analysisEdge("CALLS", "/r/lib.go::"+name, "/r/lib.go::hub"))
	}

	// Golden from analysisOracleNote. Scores, reason lists AND order are the
	// release's: equally surprising edges keep edge-id order, which is why
	// periph->hub precedes hub->periph at 0.7 and noComm precedes f1 at 0.2.
	const wantJSON = `[
	 {"source":"test_hub","source_qualified":"/r/app_test.py::test_hub",
	  "target":"hub","target_qualified":"/r/lib.go::hub","edge_kind":"CALLS",
	  "surprise_score":0.85,
	  "reasons":["cross-community","cross-language","peripheral-to-hub","cross-test-boundary"],
	  "source_community":1,"target_community":2},
	 {"source":"periph","source_qualified":"/r/app.py::periph",
	  "target":"hub","target_qualified":"/r/lib.go::hub","edge_kind":"CALLS",
	  "surprise_score":0.7,
	  "reasons":["cross-community","cross-language","peripheral-to-hub"],
	  "source_community":1,"target_community":2},
	 {"source":"hub","source_qualified":"/r/lib.go::hub",
	  "target":"periph","target_qualified":"/r/app.py::periph","edge_kind":"IMPORTS_FROM",
	  "surprise_score":0.7,
	  "reasons":["cross-community","cross-language","peripheral-to-hub"],
	  "source_community":2,"target_community":1},
	 {"source":"TypeThing","source_qualified":"/r/lib.go::TypeThing",
	  "target":"hub","target_qualified":"/r/lib.go::hub","edge_kind":"CALLS",
	  "surprise_score":0.35,
	  "reasons":["peripheral-to-hub","unusual-edge-kind"],
	  "source_community":2,"target_community":2},
	 {"source":"noComm","source_qualified":"/r/lib.go::noComm",
	  "target":"hub","target_qualified":"/r/lib.go::hub","edge_kind":"CALLS",
	  "surprise_score":0.2,"reasons":["peripheral-to-hub"],
	  "source_community":null,"target_community":2},
	 {"source":"f1","source_qualified":"/r/lib.go::f1","target":"hub",
	  "target_qualified":"/r/lib.go::hub","edge_kind":"CALLS","surprise_score":0.2,
	  "reasons":["peripheral-to-hub"],"source_community":2,"target_community":2},
	 {"source":"f2","source_qualified":"/r/lib.go::f2","target":"hub",
	  "target_qualified":"/r/lib.go::hub","edge_kind":"CALLS","surprise_score":0.2,
	  "reasons":["peripheral-to-hub"],"source_community":2,"target_community":2},
	 {"source":"f3","source_qualified":"/r/lib.go::f3","target":"hub",
	  "target_qualified":"/r/lib.go::hub","edge_kind":"CALLS","surprise_score":0.2,
	  "reasons":["peripheral-to-hub"],"source_community":2,"target_community":2},
	 {"source":"f4","source_qualified":"/r/lib.go::f4","target":"hub",
	  "target_qualified":"/r/lib.go::hub","edge_kind":"CALLS","surprise_score":0.2,
	  "reasons":["peripheral-to-hub"],"source_community":2,"target_community":2},
	 {"source":"f5","source_qualified":"/r/lib.go::f5","target":"hub",
	  "target_qualified":"/r/lib.go::hub","edge_kind":"CALLS","surprise_score":0.2,
	  "reasons":["peripheral-to-hub"],"source_community":2,"target_community":2},
	 {"source":"f6","source_qualified":"/r/lib.go::f6","target":"hub",
	  "target_qualified":"/r/lib.go::hub","edge_kind":"CALLS","surprise_score":0.2,
	  "reasons":["peripheral-to-hub"],"source_community":2,"target_community":2},
	 {"source":"f7","source_qualified":"/r/lib.go::f7","target":"hub",
	  "target_qualified":"/r/lib.go::hub","edge_kind":"CALLS","surprise_score":0.2,
	  "reasons":["peripheral-to-hub"],"source_community":2,"target_community":2},
	 {"source":"f8","source_qualified":"/r/lib.go::f8","target":"hub",
	  "target_qualified":"/r/lib.go::hub","edge_kind":"CALLS","surprise_score":0.2,
	  "reasons":["peripheral-to-hub"],"source_community":2,"target_community":2}
	]`

	engine := analysisSyntheticEngine(t, nodes, edges,
		[]analysisSeedCommunity{{1, "app"}, {2, "lib"}})
	payload := analysisCall(t, engine, "get_surprising_connections_tool", `{"top_n": 100}`)

	if diff, ok := analysisDiffJSON("", analysisParseJSON(t, wantJSON),
		payload["surprising_connections"]); !ok {
		t.Fatalf("surprising_connections differs at %s (golden: %s)", diff, analysisOracleNote)
	}
	analysisRequireEqual(t, "count", float64(13), payload["count"])
	analysisRequireEqual(t, "total", float64(13), payload["total"])
	analysisRequireEqual(t, "truncated", false, payload["truncated"])
	analysisRequireEqual(t, "summary",
		"13 surprising connection(s) ranked by surprise score", payload["summary"])
}

// TestSurprisingConnectionsCeilingCapsAtOneHundred pins the hard ceiling: a
// caller may raise top_n past the default but never past 100, and `total`
// keeps reporting the real candidate count.
func TestSurprisingConnectionsCeilingCapsAtOneHundred(t *testing.T) {
	nodes := []analysisSeedNode{
		{kind: "Function", name: "hub", file: "/r/lib.go", language: "go", community: 1},
	}
	var edges []analysisSeedEdge
	for i := range 120 {
		name := fmt.Sprintf("leaf%d", i)
		nodes = append(nodes, analysisSeedNode{
			kind: "Function", name: name, file: "/r/leaf.py", language: "python", community: 2,
		})
		edges = append(edges, analysisEdge("CALLS", "/r/leaf.py::"+name, "/r/lib.go::hub"))
	}
	engine := analysisSyntheticEngine(t, nodes, edges,
		[]analysisSeedCommunity{{1, "lib"}, {2, "leaf"}})

	payload := analysisCall(t, engine, "get_surprising_connections_tool", `{"top_n": 1000}`)
	analysisRequireEqual(t, "count", float64(maxSurprising), payload["count"])
	analysisRequireEqual(t, "total", float64(120), payload["total"])
	analysisRequireEqual(t, "truncated", true, payload["truncated"])
	analysisRequireEqual(t, "summary",
		"120 surprising connection(s) ranked by surprise score, showing 100 of 120",
		payload["summary"])
}

// ─── knowledge gaps ─────────────────────────────────────────────────────────

// TestKnowledgeGapsCategoriesMatchRelease pins the two categories the fixture
// repository leaves empty — thin communities, including one whose members are
// all gone — together with the exclusions that make untested_hotspots mean
// something: a symbol covered by a TESTED_BY edge and a symbol that IS a test
// are not untested hotspots, however connected they are.
func TestKnowledgeGapsCategoriesMatchRelease(t *testing.T) {
	nodes := []analysisSeedNode{
		{kind: "Function", name: "a1", file: "/r/a.go", language: "go", community: 1},
		{kind: "Function", name: "a2", file: "/r/a.go", language: "go", community: 1},
		{kind: "Function", name: "b1", file: "/r/b.go", language: "go", community: 2},
		{kind: "Function", name: "b2", file: "/r/b.go", language: "go", community: 2},
		{kind: "Function", name: "b3", file: "/r/b.go", language: "go", community: 2},
		{kind: "Function", name: "c1", file: "/r/c.go", language: "go", community: 3},
		{kind: "Function", name: "c2", file: "/r/c.go", language: "go", community: 3},
		{kind: "Function", name: "d1", file: "/r/d.go", language: "go", community: 3},
		{kind: "Function", name: "hot", file: "/r/a.go", language: "go", community: 1},
		{kind: "Function", name: "covered", file: "/r/b.go", language: "go", community: 2},
		{kind: "Test", name: "t1", file: "/r/a_test.go", language: "go", isTest: true, community: 2},
	}
	var edges []analysisSeedEdge
	for i := range 5 {
		edges = append(edges, analysisEdge("CALLS", "/r/a.go::hot",
			fmt.Sprintf("/r/b.go::b%d", i%3+1)))
	}
	for i := range 5 {
		edges = append(edges, analysisEdge("CALLS", "/r/b.go::covered",
			fmt.Sprintf("/r/c.go::c%d", i%2+1)))
	}
	edges = append(edges, analysisEdge("TESTED_BY", "/r/b.go::covered", "/r/a_test.go::t1"))
	for i := range 6 {
		edges = append(edges, analysisEdge("CALLS", "/r/a_test.go::t1",
			fmt.Sprintf("/r/c.go::c%d", i%2+1)))
	}

	// Golden from analysisOracleNote. Community 4 has no members at all and is
	// still reported, at size 0. Community 1 is the only single-file one:
	// community 2 spans b.go and a_test.go, community 3 spans c.go and d.go.
	const wantJSON = `{
	 "isolated_nodes":[
	  {"name":"a1","qualified_name":"/r/a.go::a1","kind":"Function","file":"/r/a.go","degree":0},
	  {"name":"a2","qualified_name":"/r/a.go::a2","kind":"Function","file":"/r/a.go","degree":0},
	  {"name":"b3","qualified_name":"/r/b.go::b3","kind":"Function","file":"/r/b.go","degree":1},
	  {"name":"d1","qualified_name":"/r/d.go::d1","kind":"Function","file":"/r/d.go","degree":0}],
	 "thin_communities":[{"community_id":4,"name":"empty","size":0}],
	 "untested_hotspots":[
	  {"name":"c1","qualified_name":"/r/c.go::c1","kind":"Function","file":"/r/c.go","degree":6},
	  {"name":"c2","qualified_name":"/r/c.go::c2","kind":"Function","file":"/r/c.go","degree":5},
	  {"name":"hot","qualified_name":"/r/a.go::hot","kind":"Function","file":"/r/a.go","degree":5}],
	 "single_file_communities":[
	  {"community_id":1,"name":"one","size":3,"file":"/r/a.go"}]
	}`

	engine := analysisSyntheticEngine(t, nodes, edges, []analysisSeedCommunity{
		{1, "one"}, {2, "two"}, {3, "three"}, {4, "empty"},
	})
	payload := analysisCall(t, engine, "get_knowledge_gaps_tool", `{}`)

	if diff, ok := analysisDiffJSON("", analysisParseJSON(t, wantJSON), payload["gaps"]); !ok {
		t.Fatalf("gaps differ at %s (golden: %s)", diff, analysisOracleNote)
	}
	wantSummary := map[string]any{
		"isolated_nodes": float64(4), "thin_communities": float64(1),
		"untested_hotspots": float64(3), "single_file_communities": float64(1),
	}
	if diff, ok := analysisDiffJSON("", wantSummary, payload["summary"]); !ok {
		t.Fatalf("summary differs at %s", diff)
	}
	analysisRequireEqual(t, "total_gaps", float64(9), payload["total_gaps"])
	analysisRequireEqual(t, "truncated", false, payload["truncated"])
}

// TestKnowledgeGapsInternalCapsBoundTheTotals pins the caps that live INSIDE
// find_knowledge_gaps rather than in the tool: isolated nodes at 50 and
// untested hotspots at 20. They bound `summary` and `total_gaps` too, so a
// repository with 60 isolated nodes reports 50 — capping them in the wrapper
// instead would leave the totals honest and diverge from the release.
func TestKnowledgeGapsInternalCapsBoundTheTotals(t *testing.T) {
	var nodes []analysisSeedNode
	for i := range 60 {
		nodes = append(nodes, analysisSeedNode{
			kind: "Function", name: fmt.Sprintf("iso%d", i), file: "/r/iso.go", language: "go"})
	}
	for i := range 25 {
		nodes = append(nodes, analysisSeedNode{
			kind: "Function", name: fmt.Sprintf("hot%d", i), file: "/r/hot.go", language: "go"})
	}
	for i := range 6 {
		nodes = append(nodes, analysisSeedNode{
			kind: "Function", name: fmt.Sprintf("sink%d", i), file: "/r/sink.go", language: "go"})
	}
	var edges []analysisSeedEdge
	for i := range 25 {
		for j := range 6 {
			edges = append(edges, analysisEdge("CALLS",
				fmt.Sprintf("/r/hot.go::hot%d", i), fmt.Sprintf("/r/sink.go::sink%d", j)))
		}
	}
	engine := analysisSyntheticEngine(t, nodes, edges, nil)

	// max_per_category at the wrapper ceiling, so only the internal caps apply.
	payload := analysisCall(t, engine, "get_knowledge_gaps_tool", `{"max_per_category": 50}`)
	wantSummary := map[string]any{
		"isolated_nodes": float64(maxIsolatedNodes), "thin_communities": float64(0),
		"untested_hotspots": float64(maxUntestedHotspots), "single_file_communities": float64(0),
	}
	if diff, ok := analysisDiffJSON("", wantSummary, payload["summary"]); !ok {
		t.Fatalf("summary differs at %s (golden: %s)", diff, analysisOracleNote)
	}
	analysisRequireEqual(t, "total_gaps",
		float64(maxIsolatedNodes+maxUntestedHotspots), payload["total_gaps"])
	analysisRequireEqual(t, "truncated", false, payload["truncated"])

	gaps, ok := payload["gaps"].(map[string]any)
	if !ok {
		t.Fatalf("gaps is %T, want object", payload["gaps"])
	}
	// The 50 kept isolated nodes are the FIRST 50 in primary-key order, and
	// the 20 kept hotspots are the highest-degree ones with equal degrees
	// still in primary-key order: the six degree-25 sinks, then hot0..hot13.
	analysisRequireNames(t, "isolated_nodes", gaps["isolated_nodes"],
		analysisGeneratedNames("iso%d", maxIsolatedNodes))
	wantHotspots := append(analysisGeneratedNames("sink%d", 6),
		analysisGeneratedNames("hot%d", 14)...)
	analysisRequireNames(t, "untested_hotspots", gaps["untested_hotspots"], wantHotspots)
}

// ─── hub nodes ──────────────────────────────────────────────────────────────

// TestHubNodesCeilingCapsAtOneHundred pins the hub ceiling the fixture
// repository is far too small to reach: twelve candidates never touch it.
func TestHubNodesCeilingCapsAtOneHundred(t *testing.T) {
	var nodes []analysisSeedNode
	var edges []analysisSeedEdge
	for i := range 130 {
		name := fmt.Sprintf("n%d", i)
		nodes = append(nodes, analysisSeedNode{
			kind: "Function", name: name, file: "/r/a.go", language: "go"})
		edges = append(edges, analysisEdge("CALLS", "/r/a.go::"+name, "/r/elsewhere"))
	}
	engine := analysisSyntheticEngine(t, nodes, edges, nil)

	payload := analysisCall(t, engine, "get_hub_nodes_tool", `{"top_n": 10000}`)
	analysisRequireEqual(t, "count", float64(maxHubNodes), payload["count"])
	analysisRequireEqual(t, "total", float64(130), payload["total"])
	analysisRequireEqual(t, "truncated", true, payload["truncated"])
	analysisRequireEqual(t, "summary",
		"130 hub node(s) ranked by degree, showing 100 of 130", payload["summary"])
}

// TestHubNodesReportNullCommunityBeforeDetection pins the nullable community
// id. Community detection has not run on this graph, so every hub reports
// community_id null — not 0, which is what the storage layer's zero value
// would serialize to and which would read as "community 0".
func TestHubNodesReportNullCommunityBeforeDetection(t *testing.T) {
	engine := analysisSyntheticEngine(t,
		[]analysisSeedNode{
			{kind: "Function", name: "a", file: "/r/a.go", language: "go"},
			{kind: "Function", name: "b", file: "/r/a.go", language: "go"},
		},
		[]analysisSeedEdge{analysisEdge("CALLS", "/r/a.go::a", "/r/a.go::b")}, nil)

	payload := analysisCall(t, engine, "get_hub_nodes_tool", `{}`)
	hubs, ok := payload["hub_nodes"].([]any)
	if !ok || len(hubs) != 2 {
		t.Fatalf("hub_nodes = %v, want two entries", payload["hub_nodes"])
	}
	for _, hub := range hubs {
		row, ok := hub.(map[string]any)
		if !ok {
			t.Fatalf("hub entry is %T, want object", hub)
		}
		value, present := row["community_id"]
		if !present {
			t.Fatal("community_id is absent; the release always emits the key")
		}
		if value != nil {
			t.Fatalf("community_id = %v, want null before community detection", value)
		}
	}
}

// ─── traversal ──────────────────────────────────────────────────────────────

// TestTraverseGraphClampsDepthToSix pins the clamp and the fact that the
// CLAMPED value is what comes back as max_depth: the published schema carries
// no bounds, so a caller can ask for depth 99 and must be told what actually
// happened. `depth` is not one of the release's guarded bounds, so 0 and a
// negative are payloads rather than errors.
func TestTraverseGraphClampsDepthToSix(t *testing.T) {
	for _, tc := range []struct{ requested, want int }{
		{99, traverseMaxDepth}, {0, traverseMinDepth}, {-4, traverseMinDepth}, {4, 4},
	} {
		t.Run(fmt.Sprintf("depth_%d", tc.requested), func(t *testing.T) {
			engine, _ := analysisFixtureEngine(t)
			payload := analysisCall(t, engine, "traverse_graph_tool",
				fmt.Sprintf(`{"query": "Login", "depth": %d}`, tc.requested))
			analysisRequireEqual(t, "max_depth", float64(tc.want), payload["max_depth"])
		})
	}
}

// TestTraverseGraphEchoesAnUnknownModeAsDepthFirst pins the mode branch:
// upstream compares for equality with "bfs" and treats everything else as
// depth-first, echoing the caller's string back unchanged. A handler that
// validated the mode, or normalized it, would answer a question the release
// answers differently.
func TestTraverseGraphEchoesAnUnknownModeAsDepthFirst(t *testing.T) {
	// One engine for both calls: traversal is a pure read, and two temp roots
	// would make the two traversals differ on their absolute paths alone.
	engine, _ := analysisFixtureEngine(t)
	strange := analysisCall(t, engine, "traverse_graph_tool",
		`{"query": "Login", "depth": 2, "mode": "sideways"}`)
	analysisRequireEqual(t, "mode", "sideways", strange["mode"])

	depthFirst := analysisCall(t, engine, "traverse_graph_tool",
		`{"query": "Login", "depth": 2, "mode": "dfs"}`)

	if diff, ok := analysisDiffJSON("", depthFirst["traversal"], strange["traversal"]); !ok {
		t.Fatalf("an unknown mode must traverse depth-first; differs at %s", diff)
	}
}

// TestTraverseGraphOnUnbuiltGraphReportsNoMatch pins the unbuilt case. The
// release opens a freshly migrated database, finds nothing, and answers with
// the bare {error, nodes} pair rather than failing the call.
func TestTraverseGraphOnUnbuiltGraphReportsNoMatch(t *testing.T) {
	engine := Open(t.TempDir())
	t.Cleanup(func() { _ = engine.Close() })

	payload := analysisCall(t, engine, "traverse_graph_tool", `{"query": "Login"}`)
	want := map[string]any{"error": "No node matching 'Login'", "nodes": []any{}}
	if diff, ok := analysisDiffJSON("", want, payload); !ok {
		t.Fatalf("unbuilt traversal differs at %s", diff)
	}
}

// TestAnalysisToolsOnUnbuiltGraphReportNothingFound pins the same graceful
// degradation for the ranked analyses, whose recorded unbuilt responses are
// "0 found" payloads rather than errors.
func TestAnalysisToolsOnUnbuiltGraphReportNothingFound(t *testing.T) {
	cases := []struct {
		tool      string
		emptyKey  string
		wantExtra map[string]any
	}{
		{"get_hub_nodes_tool", "hub_nodes", map[string]any{
			"status": "ok", "count": float64(0), "total": float64(0),
			"truncated": false, "summary": "0 hub node(s) ranked by degree",
		}},
		{"get_surprising_connections_tool", "surprising_connections", map[string]any{
			"status": "ok", "count": float64(0), "total": float64(0),
			"truncated": false,
			"summary":   "0 surprising connection(s) ranked by surprise score",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			engine := Open(t.TempDir())
			t.Cleanup(func() { _ = engine.Close() })
			payload := analysisCall(t, engine, tc.tool, `{}`)
			for key, want := range tc.wantExtra {
				analysisRequireEqual(t, key, want, payload[key])
			}
			if rows, ok := payload[tc.emptyKey].([]any); !ok || len(rows) != 0 {
				t.Fatalf("%s = %v, want an empty list", tc.emptyKey, payload[tc.emptyKey])
			}
		})
	}

	t.Run("get_knowledge_gaps_tool", func(t *testing.T) {
		engine := Open(t.TempDir())
		t.Cleanup(func() { _ = engine.Close() })
		payload := analysisCall(t, engine, "get_knowledge_gaps_tool", `{}`)
		want := map[string]any{
			"isolated_nodes": []any{}, "thin_communities": []any{},
			"untested_hotspots": []any{}, "single_file_communities": []any{},
		}
		if diff, ok := analysisDiffJSON("", want, payload["gaps"]); !ok {
			t.Fatalf("gaps differ at %s", diff)
		}
		analysisRequireEqual(t, "total_gaps", float64(0), payload["total_gaps"])
	})
}

// ─── bridge-only routing ────────────────────────────────────────────────────

// TestBetweennessToolsHaveNoNativeHandler pins the routing decision itself.
// Registering either tool would make `da kg serve` answer from a native
// estimate, which cannot match a ranking the release draws from an unseeded
// random sample.
func TestBetweennessToolsHaveNoNativeHandler(t *testing.T) {
	for tool, reason := range analysisBridgeOnlyTools {
		if _, native := toolHandlers[tool]; native {
			t.Errorf("%s has a native handler but must stay on the bridge: %s", tool, reason)
		}
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

// analysisCallFixture is one recorded tools/call response.
type analysisCallFixture struct {
	tool          string
	caseName      string
	arguments     json.RawMessage
	isError       bool
	contentText   string
	structured    map[string]any
	hasStructured bool
}

// wantPayload is the expected payload: the recorded one without the `_graph`
// provenance envelope (attached by Engine.CallTool, not by a handler) and with
// the placeholder repository root resolved to this run's temp root.
func (f analysisCallFixture) wantPayload(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(f.structured)
	if err != nil {
		t.Fatalf("re-encode fixture payload: %v", err)
	}
	resolved := strings.ReplaceAll(string(raw), repoRootPlaceholder,
		analysisJSONEscape(filepath.ToSlash(root)))
	var payload map[string]any
	if err := json.Unmarshal([]byte(resolved), &payload); err != nil {
		t.Fatalf("decode fixture payload: %v", err)
	}
	delete(payload, "_graph")
	return payload
}

// analysisJSONEscape renders a path safely for substitution into encoded JSON.
// A temp root is almost always plain, but a backslash or quote in it would
// otherwise produce invalid JSON rather than a clear failure.
func analysisJSONEscape(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	return strings.Trim(string(encoded), `"`)
}

// analysisCallFixtures discovers every recorded call for the analysis tools,
// sorted, so a case another lane adds is picked up without an edit here.
func analysisCallFixtures(t *testing.T) []analysisCallFixture {
	t.Helper()
	var out []analysisCallFixture
	for _, tool := range analysisTools {
		paths, err := filepath.Glob(filepath.Join(releaseContractDir, "calls", tool+"__*.json"))
		if err != nil {
			t.Fatalf("glob %s fixtures: %v", tool, err)
		}
		sort.Strings(paths)
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var record struct {
				Tool      string          `json:"tool"`
				Case      string          `json:"case"`
				Arguments json.RawMessage `json:"arguments"`
				IsError   bool            `json:"is_error"`
				Content   []struct {
					Text string `json:"text"`
				} `json:"content"`
				Structured *map[string]any `json:"structured_content"`
			}
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			fixture := analysisCallFixture{
				tool: record.Tool, caseName: record.Case,
				arguments: record.Arguments, isError: record.IsError,
			}
			if len(record.Content) > 0 {
				fixture.contentText = record.Content[0].Text
			}
			if record.Structured != nil {
				fixture.structured, fixture.hasStructured = *record.Structured, true
			}
			out = append(out, fixture)
		}
	}
	return out
}

// analysisCall binds raw arguments through the release's schema and runs the
// native handler, failing on anything other than a payload.
func analysisCall(t *testing.T, engine *Engine, tool, arguments string) map[string]any {
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
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	payload, ok := analysisJSONValue(t, result).(map[string]any)
	if !ok {
		t.Fatalf("%s returned %T, want an object", tool, result)
	}
	return payload
}

// analysisJSONValue round-trips a handler result through JSON, which is how a
// client sees it: an int and an int64 both become a number, and a nil `any`
// becomes null.
func analysisJSONValue(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode result: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return decoded
}

func analysisParseJSON(t *testing.T, text string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("parse golden JSON: %v", err)
	}
	return value
}

// analysisDiffJSON reports the first structural difference as a JSON path.
// A `${...}` placeholder on the expected side is a value the generator
// normalized because it is volatile, so it is skipped rather than compared.
func analysisDiffJSON(path string, want, got any) (string, bool) {
	if text, ok := want.(string); ok &&
		strings.HasPrefix(text, "${") && strings.HasSuffix(text, "}") {
		return "", true
	}
	switch expected := want.(type) {
	case map[string]any:
		return analysisDiffJSONObject(path, expected, got)
	case []any:
		return analysisDiffJSONArray(path, expected, got)
	default:
		if want != got {
			return fmt.Sprintf("%s: want %#v, got %#v", analysisPath(path), want, got), false
		}
		return "", true
	}
}

// analysisDiffJSONObject reports the first structural difference inside a JSON
// object: a wrong result kind, a key the result is missing, a differing value,
// or a key the result carries that the expectation does not.
func analysisDiffJSONObject(path string, expected map[string]any, got any) (string, bool) {
	actual, ok := got.(map[string]any)
	if !ok {
		return fmt.Sprintf("%s: want object, got %T (%v)", analysisPath(path), got, got), false
	}
	for _, key := range analysisSortedKeys(expected) {
		value, present := actual[key]
		if !present {
			return fmt.Sprintf("%s: key missing from result", analysisPath(path+"."+key)), false
		}
		if diff, ok := analysisDiffJSON(path+"."+key, expected[key], value); !ok {
			return diff, false
		}
	}
	for _, key := range analysisSortedKeys(actual) {
		if _, present := expected[key]; !present {
			return fmt.Sprintf("%s: unexpected key in result (%v)",
				analysisPath(path+"."+key), actual[key]), false
		}
	}
	return "", true
}

// analysisDiffJSONArray reports the first structural difference inside a JSON
// array: a wrong result kind, a differing length, or the first element that
// differs. Order is significant, exactly as it is in the recorded payloads.
func analysisDiffJSONArray(path string, expected []any, got any) (string, bool) {
	actual, ok := got.([]any)
	if !ok {
		return fmt.Sprintf("%s: want array, got %T (%v)", analysisPath(path), got, got), false
	}
	if len(expected) != len(actual) {
		return fmt.Sprintf("%s: want %d items, got %d",
			analysisPath(path), len(expected), len(actual)), false
	}
	for i := range expected {
		if diff, ok := analysisDiffJSON(fmt.Sprintf("%s[%d]", path, i),
			expected[i], actual[i]); !ok {
			return diff, false
		}
	}
	return "", true
}

func analysisPath(path string) string {
	if path == "" {
		return "$"
	}
	return "$" + path
}

func analysisSortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func analysisRequireEqual(t *testing.T, field string, want, got any) {
	t.Helper()
	if want != got {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// analysisRequireNames asserts the `name` of each row of a gap category, in
// order — the cheap way to pin which rows a cap kept.
func analysisRequireNames(t *testing.T, field string, rows any, want []string) {
	t.Helper()
	list, ok := rows.([]any)
	if !ok {
		t.Fatalf("%s is %T, want array", field, rows)
	}
	got := make([]string, 0, len(list))
	for _, row := range list {
		entry, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("%s entry is %T, want object", field, row)
		}
		name, _ := entry["name"].(string)
		got = append(got, name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s names\n got: %v\nwant: %v", field, got, want)
	}
}

func analysisGeneratedNames(format string, count int) []string {
	names := make([]string, 0, count)
	for i := range count {
		names = append(names, fmt.Sprintf(format, i))
	}
	return names
}

// ─── store seeding ──────────────────────────────────────────────────────────

// analysisSeedNode is one node row to insert. `file` doubles as the language
// probe for surprise scoring, which reads the path's suffix rather than the
// language column.
type analysisSeedNode struct {
	kind      string
	name      string
	parent    string
	file      string
	language  string
	signature string
	isTest    bool
	community int64
}

func (n analysisSeedNode) qualified() string {
	if n.kind == graphstore.NodeKindFile {
		return n.file
	}
	if n.parent != "" {
		return n.file + "::" + n.parent + "." + n.name
	}
	return n.file + "::" + n.name
}

// analysisSeedEdge is one edge row. `file` and `line` are part of the seed
// because the edges table is indexed on them: a lookup by source can be served
// from any of three indexes, and rows that all share one file and line would
// make the neighbour order an artefact of the seeding rather than a property
// of the release's graph.
type analysisSeedEdge struct {
	kind   string
	source string
	target string
	file   string
	line   int
}

// analysisEdge builds a synthetic edge. Its file and line are filled in by
// the seeder from the row's position, which keeps them distinct.
func analysisEdge(kind, source, target string) analysisSeedEdge {
	return analysisSeedEdge{kind: kind, source: source, target: target}
}

type analysisSeedCommunity struct {
	id   int64
	name string
}

// analysisFixtureEngine copies the release's fixture repository into a temp
// root and seeds a graph database with graph.json's EXACT rows — ids,
// signatures, community ids, edge line numbers and NULLs included.
//
// The rows are written with plain SQL rather than through the scanner or
// ReplaceCommunities on purpose: these tests must fail for a handler bug and
// for nothing else, so neither extraction nor the derived-view computations
// are in the path.
func analysisFixtureEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	root := copyReleaseRepo(t)
	nodes, edges, communities := analysisReleaseRows(t, root)
	return analysisSeededEngine(t, root, nodes, edges, communities), root
}

// analysisSyntheticEngine seeds an ad-hoc graph under an empty root, for the
// behaviours the fixture repository cannot produce.
func analysisSyntheticEngine(t *testing.T, nodes []analysisSeedNode,
	edges []analysisSeedEdge, communities []analysisSeedCommunity) *Engine {
	t.Helper()
	return analysisSeededEngine(t, t.TempDir(), nodes, edges, communities)
}

func analysisSeededEngine(t *testing.T, root string, nodes []analysisSeedNode,
	edges []analysisSeedEdge, communities []analysisSeedCommunity) *Engine {
	t.Helper()
	dbPath := graphstore.NativeGraphDBPath(root)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir graph dir: %v", err)
	}
	// Open once to run the migrations, then close so the raw inserts below own
	// the connection.
	migrated, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := migrated.Close(); err != nil {
		t.Fatalf("close migrated store: %v", err)
	}

	db := openGraphDB(t, dbPath)
	for i, node := range nodes {
		// signature and community_id are the two genuinely nullable columns:
		// the backfill and community detection fill them in later, and
		// graphstore scans both through sql.Null*. The remaining text columns
		// are written as '' rather than NULL because that is what
		// graphstore's own writer produces, and the native backend keeps its
		// database under .dot-agents/ precisely so it never reads rows the
		// Python release wrote.
		var signature, community any
		if node.signature != "" {
			signature = node.signature
		}
		if node.community != 0 {
			community = node.community
		}
		isTest := 0
		if node.isTest {
			isTest = 1
		}
		if _, err := db.Exec(
			`INSERT INTO nodes (id, kind, name, qualified_name, file_path,
			   line_start, line_end, language, parent_name, params,
			   return_type, modifiers, is_test, file_hash, extra,
			   updated_at, signature, community_id)
			 VALUES (?, ?, ?, ?, ?, 1, 2, ?, ?, '', '', '', ?, '', '{}',
			         0.0, ?, ?)`,
			i+1, node.kind, node.name, node.qualified(), node.file,
			node.language, node.parent, isTest, signature, community,
		); err != nil {
			t.Fatalf("seed node %s: %v", node.qualified(), err)
		}
	}
	for i, edge := range edges {
		file, line := edge.file, edge.line
		if file == "" {
			file = edge.source
		}
		if line == 0 {
			line = i + 1
		}
		if _, err := db.Exec(
			`INSERT INTO edges (id, kind, source_qualified, target_qualified,
			   file_path, line, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 0.0)`,
			i+1, edge.kind, edge.source, edge.target, file, line,
		); err != nil {
			t.Fatalf("seed edge %s -> %s: %v", edge.source, edge.target, err)
		}
	}
	for _, community := range communities {
		if _, err := db.Exec(
			`INSERT INTO communities (id, name, level, cohesion, size,
			   dominant_language, description)
			 VALUES (?, ?, 0, 0.5, 0, 'go', 'seeded')`,
			community.id, community.name,
		); err != nil {
			t.Fatalf("seed community %d: %v", community.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed connection: %v", err)
	}

	engine := Open(root)
	t.Cleanup(func() { _ = engine.Close() })
	store, err := engine.writeStore()
	if err != nil {
		t.Fatalf("open seeded store: %v", err)
	}
	// traverse_graph_tool finds its start node through the FTS index, which
	// upstream's postprocess pass populates from the nodes table.
	if _, err := store.RebuildFTS(); err != nil {
		t.Fatalf("rebuild FTS: %v", err)
	}
	return engine
}

// analysisReleaseRows reads graph.json and returns its rows with the
// placeholder root resolved, in id order.
func analysisReleaseRows(t *testing.T, root string) (
	[]analysisSeedNode, []analysisSeedEdge, []analysisSeedCommunity) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(releaseContractDir, "graph.json"))
	if err != nil {
		t.Fatalf("read release graph: %v", err)
	}
	var graph struct {
		Nodes []struct {
			ID          int64   `json:"id"`
			Kind        string  `json:"kind"`
			Name        string  `json:"name"`
			FilePath    string  `json:"file_path"`
			Language    *string `json:"language"`
			ParentName  *string `json:"parent_name"`
			Signature   *string `json:"signature"`
			IsTest      int     `json:"is_test"`
			CommunityID *int64  `json:"community_id"`
		} `json:"nodes"`
		Edges []struct {
			ID       int64  `json:"id"`
			Kind     string `json:"kind"`
			Source   string `json:"source_qualified"`
			Target   string `json:"target_qualified"`
			FilePath string `json:"file_path"`
			Line     int    `json:"line"`
		} `json:"edges"`
		Communities []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"communities"`
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatalf("parse release graph: %v", err)
	}
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 || len(graph.Communities) == 0 {
		t.Fatalf("release graph fixture is incomplete: %d nodes, %d edges, %d communities",
			len(graph.Nodes), len(graph.Edges), len(graph.Communities))
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool { return graph.Edges[i].ID < graph.Edges[j].ID })

	slashRoot := filepath.ToSlash(root)
	subst := func(s string) string {
		return strings.ReplaceAll(s, repoRootPlaceholder, slashRoot)
	}
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return subst(*p)
	}

	// Ids are written from slice position, so a fixture whose ids are not
	// 1..N in order would silently seed a different graph.
	nodes := make([]analysisSeedNode, 0, len(graph.Nodes))
	for i, n := range graph.Nodes {
		if n.ID != int64(i+1) {
			t.Fatalf("release node ids are not contiguous from 1: got %d at position %d", n.ID, i)
		}
		community := int64(0)
		if n.CommunityID != nil {
			community = *n.CommunityID
		}
		nodes = append(nodes, analysisSeedNode{
			kind: n.Kind, name: subst(n.Name), parent: deref(n.ParentName),
			file: subst(n.FilePath), language: deref(n.Language),
			signature: deref(n.Signature), isTest: n.IsTest == 1,
			community: community,
		})
	}
	edges := make([]analysisSeedEdge, 0, len(graph.Edges))
	for i, e := range graph.Edges {
		if e.ID != int64(i+1) {
			t.Fatalf("release edge ids are not contiguous from 1: got %d at position %d", e.ID, i)
		}
		edges = append(edges, analysisSeedEdge{
			kind: e.Kind, source: subst(e.Source), target: subst(e.Target),
			file: subst(e.FilePath), line: e.Line,
		})
	}
	communities := make([]analysisSeedCommunity, 0, len(graph.Communities))
	for _, c := range graph.Communities {
		communities = append(communities, analysisSeedCommunity{c.ID, c.Name})
	}
	return nodes, edges, communities
}
