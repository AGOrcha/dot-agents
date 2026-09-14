package crg

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Derived-view parity against the release contract.
//
// The oracle is testdata/crg-release/v2.3.8/graph.json — upstream
// code-review-graph v2.3.8's own nodes, edges and derived rows for the fixture
// repository, captured from a real install. These tests seed a native store
// with the fixture's nodes and edges, run the native derivations, and compare
// every produced row against the fixture FIELD FOR FIELD, including the
// 4-decimal criticality and cohesion values.
//
// That is a deliberate replacement for the previous oracles, which compared
// two native computations to each other under loose statistical predicates
// (partition agreement, Spearman rank correlation, token-set equality). Those
// could not fail on a wrong-but-self-consistent algorithm — and the algorithm
// WAS wrong: communities were weakly-connected components over CALLS ∪
// IMPORTS, where upstream groups by directory; risk_index was degree
// centrality, where upstream uses a caller/coverage/security formula. Direct
// comparison against recorded upstream values is the only oracle that catches
// that class of divergence.

// lastComputedStamp is the risk_index.last_computed value these tests pass in.
// Upstream writes SQLite's datetime('now') there, so the generator omits the
// column from the fixture and nothing asserts on it; a fixed stamp keeps the
// computation deterministic.
const lastComputedStamp = "2026-01-01 00:00:00"

// releaseGraphPath resolves the release-contract graph fixture relative to
// this test file.
func releaseGraphPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	return filepath.Join(root, "testdata", "crg-release", "v2.3.8", "graph.json")
}

// seededStore opens a FRESH native store and seeds it with the release
// fixture's nodes and edges, returning the store, its file path and the loaded
// fixture.
//
// The repo root is the test's own temp dir and is substituted into the
// fixture, so nothing here depends on the machine that generated it. The store
// must be fresh: node ids come from AUTOINCREMENT insertion order, and every
// derived row is keyed by them.
func seededStore(t *testing.T) (*graphstore.SQLiteStore, string, ReleaseGraph) {
	t.Helper()
	root := t.TempDir()
	fixture, err := LoadReleaseGraph(releaseGraphPath(t), root)
	if err != nil {
		t.Fatalf("load release graph: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "graph.db")
	store, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	for i, n := range fixture.NodeInfos() {
		id, err := store.UpsertNode(n, "hash")
		if err != nil {
			t.Fatalf("seed node %d (%s): %v", i, n.Name, err)
		}
		if want := int64(i + 1); id != want {
			t.Fatalf("seed node %d got id %d, want %d — a fresh store must "+
				"reproduce the fixture's node ids", i, id, want)
		}
	}
	// Identity construction must agree with upstream's, or every derived row
	// keyed by a qualified name silently misses.
	stored, err := store.ReadAllNodes()
	if err != nil {
		t.Fatalf("read seeded nodes: %v", err)
	}
	for i, n := range stored {
		if want := fixture.Nodes[i].QualifiedName; n.QualifiedName != want {
			t.Fatalf("node %d qualified name = %q, want %q", n.ID, n.QualifiedName, want)
		}
	}
	for i, e := range fixture.EdgeInfos() {
		if _, err := store.UpsertEdge(e); err != nil {
			t.Fatalf("seed edge %d (%s): %v", i, e.Kind, err)
		}
	}
	return store, dbPath, fixture
}

// runFullPostprocess runs the complete postprocess lifecycle at
// postprocess="full": bare-endpoint resolution, signatures, FTS, flows,
// communities AND the three summary tables. This is upstream
// _run_postprocess's order.
func runFullPostprocess(t *testing.T, store *graphstore.SQLiteStore) {
	t.Helper()
	runStandalonePostprocess(t, store)
	refreshSummaryTables(t, store)
}

// runStandalonePostprocess runs the steps a STANDALONE postprocess runs:
// bare-endpoint resolution, signatures, FTS, flows and communities — and
// deliberately NOT the summary tables. Upstream's run_postprocess never calls
// _compute_summaries; that split is the behavioural contract
// TestPostprocessSplit_StandaloneLeavesSummaryTablesUntouched exercises.
func runStandalonePostprocess(t *testing.T, store *graphstore.SQLiteStore) {
	t.Helper()
	nodes, edges := graphContents(t, store)

	callRewrites, _ := ResolveBareCallTargets(nodes, edges)
	if _, err := store.ApplyEdgeRewrites(callRewrites); err != nil {
		t.Fatalf("apply call-target rewrites: %v", err)
	}
	testedRewrites, _ := ResolveBareTestedBySources(nodes, edges)
	if _, err := store.ApplyEdgeRewrites(testedRewrites); err != nil {
		t.Fatalf("apply tested-by rewrites: %v", err)
	}

	backfillSignatures(t, store)
	if _, err := store.RebuildFTS(); err != nil {
		t.Fatalf("rebuild fts: %v", err)
	}

	nodes, edges = graphContents(t, store)
	flows, paths := TraceFlows(nodes, edges, DefaultFlowMaxDepth, false)
	if _, err := store.ReplaceFlows(flows, paths); err != nil {
		t.Fatalf("replace flows: %v", err)
	}
	communities, members := DetectCommunities(nodes, edges, DefaultCommunityMinSize)
	if _, err := store.ReplaceCommunities(communities, members); err != nil {
		t.Fatalf("replace communities: %v", err)
	}
}

// refreshSummaryTables is the summary-table step a full build/update runs and
// a standalone postprocess does not.
func refreshSummaryTables(t *testing.T, store *graphstore.SQLiteStore) {
	t.Helper()
	// Re-read AFTER storing communities: ReplaceCommunities rewrites
	// nodes.community_id, and the summaries group by it.
	nodes, edges := graphContents(t, store)
	storedFlows, err := store.ReadFlows()
	if err != nil {
		t.Fatalf("read flows: %v", err)
	}
	storedCommunities, err := store.ReadCommunities()
	if err != nil {
		t.Fatalf("read communities: %v", err)
	}
	summaries := ComputeSummaries(nodes, edges, storedFlows, storedCommunities, lastComputedStamp)
	if _, err := store.ReplaceCommunitySummaries(summaries.CommunitySummaries); err != nil {
		t.Fatalf("replace community summaries: %v", err)
	}
	if _, err := store.ReplaceFlowSnapshots(summaries.FlowSnapshots); err != nil {
		t.Fatalf("replace flow snapshots: %v", err)
	}
	if _, err := store.ReplaceRiskIndex(summaries.RiskIndex); err != nil {
		t.Fatalf("replace risk index: %v", err)
	}
}

// graphContents reads the whole persisted graph back.
func graphContents(t *testing.T, store *graphstore.SQLiteStore) ([]graphstore.GraphNode, []graphstore.GraphEdge) {
	t.Helper()
	nodes, err := store.ReadAllNodes()
	if err != nil {
		t.Fatalf("read nodes: %v", err)
	}
	edges, err := store.ReadAllEdges()
	if err != nil {
		t.Fatalf("read edges: %v", err)
	}
	return nodes, edges
}

// backfillSignatures renders and stores the signature of every node that lacks
// one, the way upstream's postprocess signature pass does.
func backfillSignatures(t *testing.T, store *graphstore.SQLiteStore) {
	t.Helper()
	pending, err := store.NodesWithoutSignature()
	if err != nil {
		t.Fatalf("nodes without signature: %v", err)
	}
	for _, n := range pending {
		if err := store.SetNodeSignature(n.ID, NodeSignature(n)); err != nil {
			t.Fatalf("set signature for node %d: %v", n.ID, err)
		}
	}
}

// TestDerivedViewParity_ReleaseContract is the acceptance test: a full
// postprocess over the fixture's graph must reproduce upstream's six derived
// tables exactly.
func TestDerivedViewParity_ReleaseContract(t *testing.T) {
	store, _, fixture := seededStore(t)
	runFullPostprocess(t, store)

	t.Run("flows", func(t *testing.T) {
		got, err := store.ReadFlows()
		if err != nil {
			t.Fatalf("read flows: %v", err)
		}
		// created_at/updated_at are wall-clock and absent from the fixture.
		for i := range got {
			got[i].CreatedAt = ""
			got[i].UpdatedAt = ""
		}
		assertRowsEqual(t, "flows", got, fixture.Flows)
	})

	t.Run("flow_memberships", func(t *testing.T) {
		got, err := store.ReadFlowMemberships()
		if err != nil {
			t.Fatalf("read flow memberships: %v", err)
		}
		assertRowsEqual(t, "flow_memberships", got, fixture.FlowMemberships)
	})

	t.Run("communities", func(t *testing.T) {
		got, err := store.ReadCommunities()
		if err != nil {
			t.Fatalf("read communities: %v", err)
		}
		assertRowsEqual(t, "communities", got, fixture.Communities)
	})

	t.Run("community_summaries", func(t *testing.T) {
		got, err := store.ReadCommunitySummaries()
		if err != nil {
			t.Fatalf("read community summaries: %v", err)
		}
		assertRowsEqual(t, "community_summaries", got, fixture.CommunitySummaries)
	})

	t.Run("flow_snapshots", func(t *testing.T) {
		got, err := store.ReadFlowSnapshots()
		if err != nil {
			t.Fatalf("read flow snapshots: %v", err)
		}
		assertRowsEqual(t, "flow_snapshots", got, fixture.FlowSnapshots)
	})

	t.Run("risk_index", func(t *testing.T) {
		got, err := store.ReadRiskIndex()
		if err != nil {
			t.Fatalf("read risk index: %v", err)
		}
		// last_computed is wall-clock upstream and absent from the fixture.
		for i := range got {
			got[i].LastComputed = ""
		}
		assertRowsEqual(t, "risk_index", got, fixture.RiskIndex)
	})

	t.Run("node_community_assignment", func(t *testing.T) {
		nodes, _ := graphContents(t, store)
		for i, n := range nodes {
			if want := fixture.Nodes[i].CommunityID; n.CommunityID != want {
				t.Errorf("node %d (%s) community_id = %d, want %d",
					n.ID, n.Name, n.CommunityID, want)
			}
		}
	})
}

// assertRowsEqual compares two row slices element by element, so a failure
// names the offending row instead of dumping one opaque whole-slice diff.
func assertRowsEqual[T any](t *testing.T, table string, got, want []T) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d rows, want %d\n got: %+v\nwant: %+v",
			table, len(got), len(want), got, want)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("%s row %d mismatch\n got: %+v\nwant: %+v", table, i, got[i], want[i])
		}
	}
}

// TestPostprocessSplit_StandaloneLeavesSummaryTablesUntouched proves the
// lifecycle split the release pins in testdata/crg-release/v2.3.8/
// lifecycle.json (summary_tables_before/after_standalone_postprocess): a
// standalone postprocess rebuilds flows, communities and FTS but must NOT
// recompute community_summaries, flow_snapshots or risk_index.
//
// The fixture records those three tables as empty on both sides of the
// standalone run, so equality there is a weak signal. This test makes it
// strong by running a FULL postprocess first — populating all three — and then
// asserting the standalone run leaves them byte-identical while the flows and
// communities around them are genuinely replaced.
func TestPostprocessSplit_StandaloneLeavesSummaryTablesUntouched(t *testing.T) {
	store, _, _ := seededStore(t)
	runFullPostprocess(t, store)

	summariesBefore := readSummaryTables(t, store)
	if len(summariesBefore.CommunitySummaries) == 0 ||
		len(summariesBefore.FlowSnapshots) == 0 ||
		len(summariesBefore.RiskIndex) == 0 {
		t.Fatal("a full postprocess must populate all three summary tables; " +
			"an empty baseline cannot prove a standalone run leaves them alone")
	}

	flowsBefore, err := store.ReadFlows()
	if err != nil {
		t.Fatalf("read flows: %v", err)
	}

	runStandalonePostprocess(t, store)

	summariesAfter := readSummaryTables(t, store)
	if !reflect.DeepEqual(summariesBefore, summariesAfter) {
		t.Fatalf("standalone postprocess must not touch the summary tables\n"+
			"before: %+v\n after: %+v", summariesBefore, summariesAfter)
	}

	// The standalone run really did do its work: flows were replaced, which on
	// SQLite means fresh AUTOINCREMENT ids (a DELETE does not reset the
	// sequence). Upstream behaves identically, which is why the summary rows
	// left behind above are not merely stale but DANGLING — flow_snapshots
	// still reference the previous generation's flow ids.
	flowsAfter, err := store.ReadFlows()
	if err != nil {
		t.Fatalf("read flows: %v", err)
	}
	if len(flowsAfter) != len(flowsBefore) {
		t.Fatalf("standalone postprocess changed the flow count: %d -> %d",
			len(flowsBefore), len(flowsAfter))
	}
	if flowsAfter[0].ID == flowsBefore[0].ID {
		t.Fatalf("standalone postprocess did not actually replace flows "+
			"(first flow id still %d)", flowsBefore[0].ID)
	}
	for _, snapshot := range summariesAfter.FlowSnapshots {
		if snapshot.FlowID == flowsBefore[0].ID {
			return // found the stale reference; the contract holds
		}
	}
	t.Error("expected a flow_snapshots row still keyed by the PREVIOUS " +
		"generation's flow id — the summary tables were not left exactly as " +
		"the last full build wrote them")
}

// TestPostprocessSplit_FullRefreshesSummaryTables is the other half of the
// split: a full postprocess must populate the summary tables a standalone one
// left alone, with the release's recorded values.
func TestPostprocessSplit_FullRefreshesSummaryTables(t *testing.T) {
	store, _, fixture := seededStore(t)
	runStandalonePostprocess(t, store)

	if s := readSummaryTables(t, store); len(s.CommunitySummaries) != 0 ||
		len(s.FlowSnapshots) != 0 || len(s.RiskIndex) != 0 {
		t.Fatalf("a standalone postprocess on a fresh graph must leave the "+
			"summary tables empty, got %+v", s)
	}

	refreshSummaryTables(t, store)
	after := readSummaryTables(t, store)

	if len(after.CommunitySummaries) != len(fixture.CommunitySummaries) ||
		len(after.FlowSnapshots) != len(fixture.FlowSnapshots) ||
		len(after.RiskIndex) != len(fixture.RiskIndex) {
		t.Fatalf("summary table row counts = %d/%d/%d, want %d/%d/%d",
			len(after.CommunitySummaries), len(after.FlowSnapshots), len(after.RiskIndex),
			len(fixture.CommunitySummaries), len(fixture.FlowSnapshots), len(fixture.RiskIndex))
	}
	// At least one row VALUE, not just the counts: key_symbols is the ranked
	// output of the summary computation, so it changes if anything upstream of
	// it is wrong.
	if got, want := after.CommunitySummaries[0].KeySymbols, fixture.CommunitySummaries[0].KeySymbols; got != want {
		t.Errorf("community_summaries[0].key_symbols = %s, want %s", got, want)
	}
	if got, want := after.RiskIndex[0].RiskScore, fixture.RiskIndex[0].RiskScore; got != want {
		t.Errorf("risk_index[0].risk_score = %v, want %v", got, want)
	}
}

// readSummaryTables snapshots the three summary tables for before/after
// comparison, with the wall-clock stamp blanked so an unchanged table cannot
// look changed.
func readSummaryTables(t *testing.T, store *graphstore.SQLiteStore) Summaries {
	t.Helper()
	communitySummaries, err := store.ReadCommunitySummaries()
	if err != nil {
		t.Fatalf("read community summaries: %v", err)
	}
	flowSnapshots, err := store.ReadFlowSnapshots()
	if err != nil {
		t.Fatalf("read flow snapshots: %v", err)
	}
	riskIndex, err := store.ReadRiskIndex()
	if err != nil {
		t.Fatalf("read risk index: %v", err)
	}
	for i := range riskIndex {
		riskIndex[i].LastComputed = ""
	}
	return Summaries{
		CommunitySummaries: communitySummaries,
		FlowSnapshots:      flowSnapshots,
		RiskIndex:          riskIndex,
	}
}

// TestRebuildFTS_IndexesEveryNodeAndMatches proves the FTS index is real: it
// must expose all 16 of the fixture's rows with upstream's four indexed
// columns, and a MATCH must return the node ids the fixture's own rows imply.
//
// nodes_fts is an EXTERNAL-CONTENT FTS5 table, so it stores no copy of the
// rows — it projects nodes(name, qualified_name, file_path, signature) by
// rowid. The row content is therefore read straight out of nodes_fts (over a
// raw connection, because that projection is not a Store operation) and
// compared to the fixture's recorded values.
func TestRebuildFTS_IndexesEveryNodeAndMatches(t *testing.T) {
	store, dbPath, fixture := seededStore(t)
	backfillSignatures(t, store)

	indexed, err := store.RebuildFTS()
	if err != nil {
		t.Fatalf("rebuild fts: %v", err)
	}
	if indexed != len(fixture.FTS) {
		t.Fatalf("RebuildFTS indexed %d rows, want %d", indexed, len(fixture.FTS))
	}

	assertRowsEqual(t, "nodes_fts", readFTSRows(t, dbPath), fixture.FTS)

	t.Run("single_match_term", func(t *testing.T) {
		// "Login" tokenizes out of node 8 only: its name is exactly `Login`
		// and its qualified name ends in `::Login`. Nodes 3 (handleLogin) and
		// 13 (TestLogin) are single unicode61 tokens `handlelogin` /
		// `testlogin` — FTS5 does not split camelCase — so they do NOT match.
		// One expected id makes the assertion order-independent.
		ids, err := store.SearchNodesFTS("Login", 20)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if !reflect.DeepEqual(ids, []int64{8}) {
			t.Fatalf("SearchNodesFTS(%q) = %v, want [8]", "Login", ids)
		}
	})

	t.Run("multi_match_term", func(t *testing.T) {
		// "token" reaches four fixture rows through two different indexed
		// columns: nodes 15 and 16 via the file path `token.go`, and nodes 10
		// and 11 via their signatures' `(token string)` parameter. Compared as
		// a SET, because the order is FTS5's BM25 ranking and that is not a
		// contract this lane owns.
		ids, err := store.SearchNodesFTS("token", 20)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		assertIDSet(t, ids, []int64{10, 11, 15, 16})
	})

	t.Run("word_search_and_count_agree", func(t *testing.T) {
		ids, err := store.SearchNodesFTSWords([]string{"token"}, 20)
		if err != nil {
			t.Fatalf("word search: %v", err)
		}
		assertIDSet(t, ids, []int64{10, 11, 15, 16})
		count, err := store.CountNodesFTSWords([]string{"token"})
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 4 {
			t.Fatalf("CountNodesFTSWords = %d, want 4", count)
		}
	})

	t.Run("no_match_is_empty_not_error", func(t *testing.T) {
		ids, err := store.SearchNodesFTS("nosuchsymbolanywhere", 20)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("expected no matches, got %v", ids)
		}
	})
}

// readFTSRows reads the four indexed columns straight out of nodes_fts.
func readFTSRows(t *testing.T, dbPath string) []ReleaseFTSRow {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(
		"SELECT rowid, name, qualified_name, file_path, signature FROM nodes_fts ORDER BY rowid")
	if err != nil {
		t.Fatalf("query nodes_fts: %v", err)
	}
	defer rows.Close()
	var out []ReleaseFTSRow
	for rows.Next() {
		var r ReleaseFTSRow
		if err := rows.Scan(&r.NodeID, &r.Name, &r.QualifiedName, &r.FilePath, &r.Signature); err != nil {
			t.Fatalf("scan nodes_fts: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("nodes_fts rows: %v", err)
	}
	return out
}

// assertIDSet compares node ids ignoring order.
func assertIDSet(t *testing.T, got, want []int64) {
	t.Helper()
	gotSet := make(map[int64]bool, len(got))
	for _, id := range got {
		gotSet[id] = true
	}
	if len(gotSet) != len(want) {
		t.Fatalf("got ids %v, want set %v", got, want)
	}
	for _, id := range want {
		if !gotSet[id] {
			t.Fatalf("got ids %v, want set %v (missing %d)", got, want, id)
		}
	}
}

// ---------------------------------------------------------------------------
// Focused algorithm contracts
// ---------------------------------------------------------------------------

// testNode is a terse graph-node builder for the focused tests below.
func testNode(id int64, kind, name, file string) graphstore.GraphNode {
	return graphstore.GraphNode{
		ID: id, Kind: kind, Name: name, FilePath: file,
		QualifiedName: file + "::" + name, Language: "go",
	}
}

// testEdge is a terse graph-edge builder for the focused tests below.
func testEdge(id int64, kind, source, target, file string) graphstore.GraphEdge {
	return graphstore.GraphEdge{
		ID: id, Kind: kind, SourceQualified: source, TargetQualified: target, FilePath: file,
	}
}

// TestDetectEntryPoints_SignalsAreIndependent proves the three entry-point
// signals are OR-ed rather than tried in priority order. `handle_thing` is
// invoked by `caller` yet is still an entry point because its name matches
// `^handle_`; `plain` is not called at all and qualifies on that alone;
// `quiet` is called and matches nothing, so it is not an entry point.
func TestDetectEntryPoints_SignalsAreIndependent(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "handle_thing", "a.go"),
		testNode(3, graphstore.NodeKindFunction, "quiet", "a.go"),
		testNode(4, graphstore.NodeKindFunction, "plain", "a.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::caller", "a.go::handle_thing", "a.go"),
		testEdge(2, graphstore.EdgeKindCalls, "a.go::caller", "a.go::quiet", "a.go"),
	}
	var names []string
	for _, ep := range DetectEntryPoints(nodes, edges, false) {
		names = append(names, ep.Name)
	}
	want := []string{"caller", "handle_thing", "plain"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("entry points = %v, want %v", names, want)
	}
}

// TestDetectEntryPoints_FileSourcedCallsDoNotHideARoot proves the
// include_file_sources=False filter: a symbol invoked only from module scope
// (a CALLS edge whose SOURCE is a File node) is still a root, because nothing
// in the code calls it.
func TestDetectEntryPoints_FileSourcedCallsDoNotHideARoot(t *testing.T) {
	nodes := []graphstore.GraphNode{
		{ID: 1, Kind: graphstore.NodeKindFile, Name: "a.go", QualifiedName: "a.go", FilePath: "a.go"},
		testNode(2, graphstore.NodeKindFunction, "runJob", "a.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go", "a.go::runJob", "a.go"),
	}
	eps := DetectEntryPoints(nodes, edges, false)
	if len(eps) != 1 || eps[0].Name != "runJob" {
		t.Fatalf("a module-scope-only callee must remain an entry point, got %+v", eps)
	}
}

// TestDetectEntryPoints_TestsExcludedUnlessRequested proves both test
// filters: the is_test flag AND upstream's path pattern.
func TestDetectEntryPoints_TestsExcludedUnlessRequested(t *testing.T) {
	flagged := testNode(1, graphstore.NodeKindTest, "TestThing", "a_test.go")
	flagged.IsTest = true
	byPath := testNode(2, graphstore.NodeKindFunction, "helper", "pkg/__tests__/helper.go")
	nodes := []graphstore.GraphNode{
		flagged, byPath, testNode(3, graphstore.NodeKindFunction, "prod", "a.go"),
	}

	if eps := DetectEntryPoints(nodes, nil, false); len(eps) != 1 || eps[0].Name != "prod" {
		t.Fatalf("include_tests=false must keep only production entry points, got %+v", eps)
	}
	if eps := DetectEntryPoints(nodes, nil, true); len(eps) != 3 {
		t.Fatalf("include_tests=true must keep all three, got %d", len(eps))
	}
}

// TestComputeCriticality_WeightsAndRounding pins the weighted sum on a graph
// built so every factor has a known value.
//
// Flow: entry -> mid (different file), mid -> an unresolved external callee.
//   - file spread  = (2-1)/4  = 0.25 -> 0.25 * 0.30 = 0.075
//   - external     = 1/5      = 0.2  -> 0.2  * 0.20 = 0.04
//   - security     = 0/2      = 0.0  -> 0.0  * 0.25 = 0
//   - test gap     = 1 - 1/2  = 0.5  -> 0.5  * 0.15 = 0.075
//   - depth        = 1/10     = 0.1  -> 0.1  * 0.10 = 0.01
//
// total 0.2. `entry` carries a TESTED_BY edge so the coverage term is a
// fraction rather than the 1.0 a wholly untested flow produces.
func TestComputeCriticality_WeightsAndRounding(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "entry", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "mid", "b.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::entry", "b.go::mid", "a.go"),
		testEdge(2, graphstore.EdgeKindCalls, "b.go::mid", "unresolvedCallee", "b.go"),
		testEdge(3, graphstore.EdgeKindTestedBy, "a.go::entry", "a_test.go::TestEntry", "a_test.go"),
	}
	flows, paths := TraceFlows(nodes, edges, DefaultFlowMaxDepth, false)
	if len(flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(flows))
	}
	if got := flows[0].Criticality; got != 0.2 {
		t.Fatalf("criticality = %v, want 0.2", got)
	}
	if got := flows[0].Depth; got != 1 {
		t.Fatalf("depth = %d, want 1 (the depth REACHED, not max_depth)", got)
	}
	if got := flows[0].FileCount; got != 2 {
		t.Fatalf("file count = %d, want 2", got)
	}
	if !reflect.DeepEqual(paths[0], []int64{1, 2}) {
		t.Fatalf("path = %v, want [1 2]", paths[0])
	}
}

// TestTraceFlows_SkipsTrivialFlows proves a single-node flow is dropped: a
// function whose only call leaves the graph has nothing to trace.
func TestTraceFlows_SkipsTrivialFlows(t *testing.T) {
	nodes := []graphstore.GraphNode{testNode(1, graphstore.NodeKindFunction, "lonely", "a.go")}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::lonely", "somewhereElse", "a.go"),
	}
	if flows, _ := TraceFlows(nodes, edges, DefaultFlowMaxDepth, false); len(flows) != 0 {
		t.Fatalf("a one-node flow must be skipped, got %+v", flows)
	}
}

// TestTraceFlows_RespectsMaxDepth proves the BFS stops expanding at max depth:
// with maxDepth=1 the third link is never followed.
func TestTraceFlows_RespectsMaxDepth(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "a", "f.go"),
		testNode(2, graphstore.NodeKindFunction, "b", "f.go"),
		testNode(3, graphstore.NodeKindFunction, "c", "f.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "f.go::a", "f.go::b", "f.go"),
		testEdge(2, graphstore.EdgeKindCalls, "f.go::b", "f.go::c", "f.go"),
	}
	_, paths := TraceFlows(nodes, edges, 1, false)
	if len(paths) != 1 || !reflect.DeepEqual(paths[0], []int64{1, 2}) {
		t.Fatalf("maxDepth=1 must stop after one hop, got %v", paths)
	}
	_, deep := TraceFlows(nodes, edges, 2, false)
	if len(deep) != 1 || !reflect.DeepEqual(deep[0], []int64{1, 2, 3}) {
		t.Fatalf("maxDepth=2 must reach the third node, got %v", deep)
	}
}

// TestDetectCommunities_DirectoryGroupingAndDedupe proves the directory
// detector and the duplicate-name disambiguation together: two sibling `util`
// directories group separately, and because both generate the same name the
// LARGER keeps it while the smaller gains a distinguishing suffix.
func TestDetectCommunities_DirectoryGroupingAndDedupe(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "alphaOne", "repo/a/util/x.go"),
		testNode(2, graphstore.NodeKindFunction, "alphaTwo", "repo/a/util/x.go"),
		testNode(3, graphstore.NodeKindFunction, "alphaThree", "repo/a/util/x.go"),
		testNode(4, graphstore.NodeKindFunction, "betaOne", "repo/b/util/y.go"),
		testNode(5, graphstore.NodeKindFunction, "betaTwo", "repo/b/util/y.go"),
	}
	rows, members := DetectCommunities(nodes, nil, DefaultCommunityMinSize)
	if len(rows) != 2 {
		t.Fatalf("expected 2 communities, got %d: %+v", len(rows), rows)
	}
	if rows[0].Size != 3 || rows[1].Size != 2 {
		t.Fatalf("community sizes = %d,%d want 3,2", rows[0].Size, rows[1].Size)
	}
	if rows[0].Description != "Directory-based community: a/util" {
		t.Errorf("community[0] description = %q", rows[0].Description)
	}
	if rows[1].Description != "Directory-based community: b/util" {
		t.Errorf("community[1] description = %q", rows[1].Description)
	}
	if rows[0].Name == rows[1].Name {
		t.Fatalf("duplicate community names must be disambiguated, both are %q", rows[0].Name)
	}
	if len(members[0]) != 3 || len(members[1]) != 2 {
		t.Fatalf("member counts = %d,%d want 3,2", len(members[0]), len(members[1]))
	}
}

// TestDetectCommunities_CohesionCountsCrossEdgesAsExternalForBoth pins the
// cohesion formula: an edge inside a community is internal; an edge spanning
// two communities is external for BOTH of them.
func TestDetectCommunities_CohesionCountsCrossEdgesAsExternalForBoth(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "a1", "repo/a/x.go"),
		testNode(2, graphstore.NodeKindFunction, "a2", "repo/a/x.go"),
		testNode(3, graphstore.NodeKindFunction, "b1", "repo/b/y.go"),
		testNode(4, graphstore.NodeKindFunction, "b2", "repo/b/y.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "repo/a/x.go::a1", "repo/a/x.go::a2", "repo/a/x.go"),
		testEdge(2, graphstore.EdgeKindCalls, "repo/a/x.go::a1", "repo/b/y.go::b1", "repo/a/x.go"),
	}
	rows, _ := DetectCommunities(nodes, edges, DefaultCommunityMinSize)
	if len(rows) != 2 {
		t.Fatalf("expected 2 communities, got %d", len(rows))
	}
	// Community a: 1 internal, 1 external -> 0.5.
	// Community b: 0 internal, 1 external -> 0.0.
	if rows[0].Cohesion != 0.5 {
		t.Errorf("community a cohesion = %v, want 0.5", rows[0].Cohesion)
	}
	if rows[1].Cohesion != 0.0 {
		t.Errorf("community b cohesion = %v, want 0", rows[1].Cohesion)
	}
}

// TestDetectCommunities_ExcludesFileNodes proves File nodes never become
// community members, which is why callers pass the full node set.
func TestDetectCommunities_ExcludesFileNodes(t *testing.T) {
	nodes := []graphstore.GraphNode{
		{ID: 1, Kind: graphstore.NodeKindFile, Name: "repo/a/x.go",
			QualifiedName: "repo/a/x.go", FilePath: "repo/a/x.go", Language: "go"},
		testNode(2, graphstore.NodeKindFunction, "alpha", "repo/a/x.go"),
		testNode(3, graphstore.NodeKindFunction, "beta", "repo/a/x.go"),
	}
	rows, members := DetectCommunities(nodes, nil, DefaultCommunityMinSize)
	if len(rows) != 1 || rows[0].Size != 2 {
		t.Fatalf("File nodes must not be members, got %+v", rows)
	}
	for _, qn := range members[0] {
		if qn == "repo/a/x.go" {
			t.Fatal("the File node leaked into the member list")
		}
	}
}

// TestComputeRiskIndex_ScoringBands pins the risk formula's three additive
// terms and the caller-count branch, which is an if/else-if and therefore
// exclusive.
// riskSomeCallersUntested is the expected score for a symbol with 4 callers
// and no test: 0.15 + 0.3, accumulated at RUNTIME in the order
// computeRiskIndex adds them.
//
// It cannot be written as the literal 0.45, and it cannot be written as the
// constant expression `0.15 + 0.3` either. In float64 that sum is
// 0.44999999999999996 — which is exactly what both the native code and
// upstream store, because both accumulate at float64 precision — whereas Go
// evaluates an untyped constant expression at arbitrary precision and folds
// `0.15 + 0.3` to precisely 0.45. The two do not compare equal, so the
// expectation has to be produced the same way the value is.
var riskSomeCallersUntested = func() float64 {
	score := 0.0
	score += 0.15
	score += 0.3
	return score
}()

func TestComputeRiskIndex_ScoringBands(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "hotPath", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "warmPath", "a.go"),
		testNode(3, graphstore.NodeKindFunction, "coldLogin", "a.go"),
		testNode(4, graphstore.NodeKindFunction, "testedPlain", "a.go"),
	}
	var edges []graphstore.GraphEdge
	id := int64(0)
	add := func(kind, src, tgt string) {
		id++
		edges = append(edges, testEdge(id, kind, src, tgt, "a.go"))
	}
	for range 12 {
		add(graphstore.EdgeKindCalls, "a.go::caller", "a.go::hotPath")
	}
	for range 4 {
		add(graphstore.EdgeKindCalls, "a.go::caller", "a.go::warmPath")
	}
	add(graphstore.EdgeKindTestedBy, "a.go::testedPlain", "a_test.go::TestPlain")

	got := ComputeSummaries(nodes, edges, nil, nil, lastComputedStamp).RiskIndex
	want := []graphstore.RiskIndexRow{
		// 12 callers (+0.3) + untested (+0.3); the name is not security-ish.
		{NodeID: 1, QualifiedName: "a.go::hotPath", RiskScore: 0.6, CallerCount: 12,
			TestCoverage: "untested", LastComputed: lastComputedStamp},
		// 4 callers (+0.15) + untested (+0.3) -- see riskSomeCallersUntested
		// for why that is not the literal 0.45.
		{NodeID: 2, QualifiedName: "a.go::warmPath", RiskScore: riskSomeCallersUntested, CallerCount: 4,
			TestCoverage: "untested", LastComputed: lastComputedStamp},
		// untested (+0.3) + the security keyword `login` in the NAME (+0.4).
		{NodeID: 3, QualifiedName: "a.go::coldLogin", RiskScore: 0.3 + 0.4,
			TestCoverage: "untested", SecurityRelevant: true, LastComputed: lastComputedStamp},
		// A TESTED_BY edge sourced at the node makes it tested: score 0.
		{NodeID: 4, QualifiedName: "a.go::testedPlain", TestCoverage: "tested",
			LastComputed: lastComputedStamp},
	}
	assertRowsEqual(t, "risk_index", got, want)
}

// TestSecurityKeywordSetsDiffer pins the deliberate difference between the two
// security keyword checks. risk_index matches the symbol NAME against a
// SHORTER set, so a plainly-named symbol in an `auth` package is NOT
// security-relevant there — while flow criticality, which matches the
// qualified name too and uses the longer set, DOES count it. Collapsing the
// two would change both tables' values.
func TestSecurityKeywordSetsDiffer(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "helper", "pkg/auth/x.go"),
		testNode(2, graphstore.NodeKindFunction, "callee", "pkg/auth/x.go"),
	}

	risk := ComputeSummaries(nodes, nil, nil, nil, lastComputedStamp).RiskIndex
	if len(risk) != 2 {
		t.Fatalf("expected 2 risk rows, got %d", len(risk))
	}
	if risk[0].SecurityRelevant {
		t.Error("risk_index.security_relevant must match the NAME only, " +
			"not the qualified name's `auth` path segment")
	}
	if risk[0].RiskScore != 0.3 {
		t.Errorf("risk score = %v, want 0.3 (untested only)", risk[0].RiskScore)
	}

	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "pkg/auth/x.go::helper", "pkg/auth/x.go::callee", "pkg/auth/x.go"),
	}
	flows, _ := TraceFlows(nodes, edges, DefaultFlowMaxDepth, false)
	if len(flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(flows))
	}
	// security 2/2 = 1.0 (*0.25) + test gap 1.0 (*0.15) + depth 0.1 (*0.10).
	if got := flows[0].Criticality; got != 0.41 {
		t.Errorf("criticality = %v, want 0.41 — the `auth` path segment must "+
			"count as security evidence HERE", got)
	}
}

// TestComputeFlowSnapshots_CriticalPathShape pins the critical-path rule for
// the short, exact and long cases, including that the entry point is the
// QUALIFIED name (v2.3.8 changed this from the bare name) and that the final
// node is not duplicated when the intermediate slice already reached it.
func TestComputeFlowSnapshots_CriticalPathShape(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "a", "f.go"),
		testNode(2, graphstore.NodeKindFunction, "b", "f.go"),
		testNode(3, graphstore.NodeKindFunction, "c", "f.go"),
		testNode(4, graphstore.NodeKindFunction, "d", "f.go"),
		testNode(5, graphstore.NodeKindFunction, "e", "f.go"),
	}
	flows := []graphstore.FlowRow{
		{ID: 1, Name: "two", EntryPointID: 1, PathJSON: "[1, 2]"},
		{ID: 2, Name: "four", EntryPointID: 1, PathJSON: "[1, 2, 3, 4]"},
		{ID: 3, Name: "five", EntryPointID: 1, PathJSON: "[1, 2, 3, 4, 5]"},
	}
	got := ComputeSummaries(nodes, nil, flows, nil, lastComputedStamp).FlowSnapshots
	if len(got) != 3 {
		t.Fatalf("expected 3 snapshots, got %d", len(got))
	}
	// Two nodes: the intermediate slice is skipped entirely -> [entry, last].
	if want := `["f.go::a", "f.go::b"]`; got[0].CriticalPath != want {
		t.Errorf("short critical path = %s, want %s", got[0].CriticalPath, want)
	}
	// Four nodes: entry + path[1:4] already ends at the last node, so it is
	// NOT appended a second time.
	if want := `["f.go::a", "f.go::b", "f.go::c", "f.go::d"]`; got[1].CriticalPath != want {
		t.Errorf("four-node critical path = %s, want %s", got[1].CriticalPath, want)
	}
	// Five nodes: entry + path[1:4] + the last node.
	if want := `["f.go::a", "f.go::b", "f.go::c", "f.go::d", "f.go::e"]`; got[2].CriticalPath != want {
		t.Errorf("long critical path = %s, want %s", got[2].CriticalPath, want)
	}
	if got[0].EntryPoint != "f.go::a" {
		t.Errorf("entry point = %q, want the qualified name", got[0].EntryPoint)
	}
}

// TestPyJSONStringArray_MatchesPythonEncoder pins the two ways encoding/json
// would diverge from upstream's stored TEXT: the ", " separator, and
// json.dumps' ensure_ascii escaping versus Go's HTML escaping.
func TestPyJSONStringArray_MatchesPythonEncoder(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   string
	}{
		{"empty_is_a_list_not_null", nil, `[]`},
		{"separator_has_a_space", []string{"a", "b"}, `["a", "b"]`},
		// Go's json.Marshal would emit \u003c / \u003e / \u0026 here.
		{"angle_brackets_stay_literal", []string{"Map<K,V>", "a&b"}, `["Map<K,V>", "a&b"]`},
		// Python escapes non-ASCII; Go's json.Marshal would not.
		{"non_ascii_escaped", []string{"Ünïcode"}, `["\u00dcn\u00efcode"]`},
		// Above the BMP Python emits a surrogate PAIR.
		{"astral_surrogate_pair", []string{"\U0001F600"}, `["\ud83d\ude00"]`},
		{"quotes_and_backslash", []string{`he said "hi"\`}, `["he said \"hi\"\\"]`},
		{"control_chars", []string{"a\tb\nc\x01"}, `["a\tb\nc\u0001"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pyJSONStringArray(tc.values); got != tc.want {
				t.Fatalf("pyJSONStringArray(%q) = %s, want %s", tc.values, got, tc.want)
			}
		})
	}
}

// TestRoundTo4_MatchesPythonRound pins Python's round() semantics.
//
// The first three cases are EXACT binary ties at the 4th decimal (they are
// n/32, so their decimal expansion terminates in a 5) and are the ones that
// distinguish the two rounding rules: ties-to-even keeps 0.0312 and 0.1562 but
// carries 0.0937 up to 0.0938, whereas math.Round's half-away-from-zero would
// give 0.0313 and 0.1563. The rest are non-ties and pin ordinary
// nearest-rounding — including 0.12345, which is NOT a tie because its double
// lies slightly above the decimal and therefore rounds up.
func TestRoundTo4_MatchesPythonRound(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0.03125, 0.0312}, // exact tie, preceding digit even -> stays
		{0.15625, 0.1562}, // exact tie, preceding digit even -> stays
		{0.09375, 0.0938}, // exact tie, preceding digit odd  -> up
		{0.12345, 0.1235}, // not a tie (double is above) -> up
		{0.12365, 0.1236}, // not a tie (double is below) -> down
		{1.0 / 3.0, 0.3333},
		{2.0 / 3.0, 0.6667},
		{2.0 / 7.0, 0.2857}, // the fixture's cmd-handle cohesion
		{0.0, 0.0},
		{1.0, 1.0},
	}
	for _, tc := range cases {
		if got := roundTo4(tc.in); got != tc.want {
			t.Errorf("roundTo4(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestNodeSignature_PerKind pins the signature renderer, including upstream's
// Python-flavoured "def"/"class" prefixes and the doubled parentheses that
// come from params already carrying its own.
func TestNodeSignature_PerKind(t *testing.T) {
	cases := []struct {
		name string
		node graphstore.GraphNode
		want string
	}{
		{"function", graphstore.GraphNode{Kind: graphstore.NodeKindFunction, Name: "Login",
			Params: "(user, password string)"}, "def Login((user, password string))"},
		{"function_with_return", graphstore.GraphNode{Kind: graphstore.NodeKindFunction,
			Name: "f", Params: "()", ReturnType: "error"}, "def f(()) -> error"},
		{"test", graphstore.GraphNode{Kind: graphstore.NodeKindTest, Name: "TestLogin",
			Params: "(t *testing.T)"}, "def TestLogin((t *testing.T))"},
		{"class", graphstore.GraphNode{Kind: graphstore.NodeKindClass, Name: "Session"},
			"class Session"},
		{"file_is_its_path", graphstore.GraphNode{Kind: graphstore.NodeKindFile,
			Name: "/repo/a.go"}, "/repo/a.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NodeSignature(tc.node); got != tc.want {
				t.Fatalf("NodeSignature = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Bare-endpoint resolution
// ---------------------------------------------------------------------------
//
// This pass is a NO-OP on the release fixture, so the parity test above cannot
// cover it. These tests are its contract.

// TestResolveBareCallTargets_SameFileEvidenceResolves proves the positive arm:
// a bare CALLS target with exactly one same-file declaration is rewritten to
// that declaration's qualified name, and the original bare name is recorded so
// the rewrite is reversible and the pass idempotent.
func TestResolveBareCallTargets_SameFileEvidenceResolves(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "helper", "a.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::caller", "helper", "a.go"),
	}
	rewrites, resolved := ResolveBareCallTargets(nodes, edges)
	if resolved != 1 {
		t.Fatalf("resolved = %d, want 1", resolved)
	}
	if len(rewrites) != 1 {
		t.Fatalf("expected 1 rewrite, got %d", len(rewrites))
	}
	r := rewrites[0]
	if r.EdgeID != 1 || r.TargetQualified != "a.go::helper" {
		t.Fatalf("rewrite = %+v, want edge 1 -> a.go::helper", r)
	}
	if r.SourceQualified != "a.go::caller" {
		t.Errorf("the source endpoint must be carried through unchanged, got %q", r.SourceQualified)
	}
	if got := r.Extra[bareCallTargetKey]; got != "helper" {
		t.Errorf("extra[%s] = %v, want the original bare name", bareCallTargetKey, got)
	}
}

// TestResolveBareCallTargets_AmbiguityIsRecordedNotGuessed proves the negative
// arm, which is the one that matters: two same-file candidates means the graph
// cannot choose, so the edge stays BARE and the candidates are recorded.
// Guessing here would silently redirect every flow, community and impact query
// crossing the edge.
func TestResolveBareCallTargets_AmbiguityIsRecordedNotGuessed(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.go"),
		{ID: 2, Kind: graphstore.NodeKindFunction, Name: "helper", FilePath: "a.go",
			QualifiedName: "a.go::Alpha.helper", Language: "go"},
		{ID: 3, Kind: graphstore.NodeKindFunction, Name: "helper", FilePath: "a.go",
			QualifiedName: "a.go::Beta.helper", Language: "go"},
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::caller", "helper", "a.go"),
	}
	rewrites, resolved := ResolveBareCallTargets(nodes, edges)
	if resolved != 0 {
		t.Fatalf("resolved = %d, want 0 — two candidates must not be guessed", resolved)
	}
	if len(rewrites) != 1 {
		t.Fatalf("expected 1 metadata-only rewrite, got %d", len(rewrites))
	}
	r := rewrites[0]
	if r.TargetQualified != "helper" {
		t.Fatalf("target = %q, want the bare name left alone", r.TargetQualified)
	}
	candidates, ok := r.Extra[ambiguousTargetsKey].([]string)
	if !ok {
		t.Fatalf("extra[%s] = %#v, want a candidate list",
			ambiguousTargetsKey, r.Extra[ambiguousTargetsKey])
	}
	if !reflect.DeepEqual(candidates, []string{"a.go::Alpha.helper", "a.go::Beta.helper"}) {
		t.Errorf("ambiguous candidates = %v", candidates)
	}
	if got := r.Extra["ambiguous_target_count"]; got != 2 {
		t.Errorf("ambiguous_target_count = %v, want 2", got)
	}
	if got := r.Extra["ambiguous_targets_truncated"]; got != false {
		t.Errorf("ambiguous_targets_truncated = %v, want false", got)
	}
	if _, present := r.Extra[unresolvedTargetsKey]; present {
		t.Error("the opposite resolution state's keys must be cleared, not merged")
	}
}

// TestResolveBareCallTargets_NoEvidenceIsUnresolved proves the third state: a
// candidate exists but in an unrelated, unimported file, so there is no
// evidence at all. A globally unique name is deliberately NOT enough, and an
// unmanaged edge in that state needs no write.
func TestResolveBareCallTargets_NoEvidenceIsUnresolved(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "helper", "elsewhere.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::caller", "helper", "a.go"),
	}
	rewrites, resolved := ResolveBareCallTargets(nodes, edges)
	if resolved != 0 {
		t.Fatalf("resolved = %d, want 0 — a globally unique name is not evidence", resolved)
	}
	if len(rewrites) != 0 {
		t.Fatalf("an unmanaged edge with no evidence needs no write, got %+v", rewrites)
	}
}

// TestResolveBareCallTargets_ImportEvidenceResolves proves the second evidence
// arm: a candidate in a file the call site imports is evidence even though it
// is not the same file.
//
// Go cannot reach this arm — a Go IMPORTS_FROM target is a module path, never a
// file — but the rule is shared across the scanner's languages, so it is
// exercised here with a path-style import target.
func TestResolveBareCallTargets_ImportEvidenceResolves(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.py"),
		testNode(2, graphstore.NodeKindFunction, "helper", "lib.py"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindImportsFrom, "a.py", "lib.py::helper", "a.py"),
		testEdge(2, graphstore.EdgeKindCalls, "a.py::caller", "helper", "a.py"),
	}
	rewrites, resolved := ResolveBareCallTargets(nodes, edges)
	if resolved != 1 || len(rewrites) != 1 {
		t.Fatalf("import evidence must resolve: resolved=%d rewrites=%+v", resolved, rewrites)
	}
	if rewrites[0].TargetQualified != "lib.py::helper" {
		t.Fatalf("target = %q, want lib.py::helper", rewrites[0].TargetQualified)
	}
}

// TestResolveBareCallTargets_Idempotent proves a second pass over an
// already-resolved edge produces no write. The pass deliberately re-selects
// edges it has managed (their extra carries the original bare name) so it CAN
// revise a verdict once the graph changes, which makes "no change" an outcome
// it has to decide rather than one it gets by not looking.
func TestResolveBareCallTargets_Idempotent(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "helper", "a.go"),
	}
	resolvedEdge := testEdge(1, graphstore.EdgeKindCalls, "a.go::caller", "a.go::helper", "a.go")
	resolvedEdge.Extra = map[string]any{bareCallTargetKey: "helper"}

	rewrites, resolved := ResolveBareCallTargets(nodes, []graphstore.GraphEdge{resolvedEdge})
	if resolved != 0 || len(rewrites) != 0 {
		t.Fatalf("re-running over a resolved edge must be a no-op, got resolved=%d rewrites=%+v",
			resolved, rewrites)
	}
}

// TestResolveBareCallTargets_DoesNotMutateInputEdges proves the pass is pure
// over its inputs: the computed extra must be a COPY, or a caller that reuses
// its edge slice for a later stage would see a verdict it never persisted.
func TestResolveBareCallTargets_DoesNotMutateInputEdges(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "caller", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "helper", "a.go"),
	}
	original := map[string]any{"parser": "go"}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::caller", "helper", "a.go"),
	}
	edges[0].Extra = original

	rewrites, _ := ResolveBareCallTargets(nodes, edges)
	if len(rewrites) != 1 {
		t.Fatalf("expected 1 rewrite, got %d", len(rewrites))
	}
	if _, leaked := original[bareCallTargetKey]; leaked {
		t.Error("the pass must not write into the caller's extra map")
	}
	if edges[0].TargetQualified != "helper" {
		t.Errorf("the caller's edge must be untouched, target is now %q", edges[0].TargetQualified)
	}
	if got := rewrites[0].Extra["parser"]; got != "go" {
		t.Errorf("pre-existing extra keys must be preserved, parser = %v", got)
	}
}

// TestResolveBareTestedBySources_ResolvesTheSourceEndpoint proves the mirror
// case: TESTED_BY is stored production -> test, so it is the SOURCE that is
// left bare when the production symbol could not be resolved.
func TestResolveBareTestedBySources_ResolvesTheSourceEndpoint(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "Login", "a_test.go"),
		testNode(2, graphstore.NodeKindTest, "TestLogin", "a_test.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindTestedBy, "Login", "a_test.go::TestLogin", "a_test.go"),
	}
	rewrites, resolved := ResolveBareTestedBySources(nodes, edges)
	if resolved != 1 || len(rewrites) != 1 {
		t.Fatalf("resolved=%d rewrites=%+v, want one resolution", resolved, rewrites)
	}
	r := rewrites[0]
	if r.SourceQualified != "a_test.go::Login" {
		t.Errorf("source = %q, want a_test.go::Login", r.SourceQualified)
	}
	if r.TargetQualified != "a_test.go::TestLogin" {
		t.Errorf("target must be carried through unchanged, got %q", r.TargetQualified)
	}
	if got := r.Extra[bareTestedBySourceKey]; got != "Login" {
		t.Errorf("extra[%s] = %v, want the original bare name", bareTestedBySourceKey, got)
	}
}

// TestResolveBareEndpoints_NoOpOnReleaseFixture documents and pins the
// property that makes the tests above necessary: every bare endpoint in the
// release fixture either has no same-file declaration (Login, ValidateToken
// and mintToken are declared in other files, and Go imports are module paths
// rather than files) or no declaration at all (Fatal). The pass therefore
// changes nothing, which is why graph.json matches raw parser output.
//
// If this ever starts resolving something, the fixture's edges have changed
// and the parity expectations above must be re-derived.
func TestResolveBareEndpoints_NoOpOnReleaseFixture(t *testing.T) {
	_, _, fixture := seededStore(t)
	callRewrites, callResolved := ResolveBareCallTargets(fixture.Nodes, fixture.Edges)
	if callResolved != 0 || len(callRewrites) != 0 {
		t.Errorf("CALLS resolution on the fixture: resolved=%d rewrites=%+v, want none",
			callResolved, callRewrites)
	}
	testedRewrites, testedResolved := ResolveBareTestedBySources(fixture.Nodes, fixture.Edges)
	if testedResolved != 0 || len(testedRewrites) != 0 {
		t.Errorf("TESTED_BY resolution on the fixture: resolved=%d rewrites=%+v, want none",
			testedResolved, testedRewrites)
	}
}

// ---------------------------------------------------------------------------
// Incremental flow tracing
// ---------------------------------------------------------------------------

// TestIncrementalTraceFlows_RetracesOnlyAffectedFlows proves the partition: a
// flow with a member in a changed file is re-traced, and an unrelated flow is
// kept untouched.
func TestIncrementalTraceFlows_RetracesOnlyAffectedFlows(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "mainA", "a.go"),
		testNode(2, graphstore.NodeKindFunction, "helperA", "a.go"),
		testNode(3, graphstore.NodeKindFunction, "mainB", "b.go"),
		testNode(4, graphstore.NodeKindFunction, "helperB", "b.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "a.go::mainA", "a.go::helperA", "a.go"),
		testEdge(2, graphstore.EdgeKindCalls, "b.go::mainB", "b.go::helperB", "b.go"),
	}
	existing, _ := TraceFlows(nodes, edges, DefaultFlowMaxDepth, false)
	if len(existing) != 2 {
		t.Fatalf("expected 2 baseline flows, got %d", len(existing))
	}
	// TraceFlows leaves PathJSON empty (the store writes it), so supply the
	// stored form the incremental pass reads.
	storedPaths := map[int64]string{1: "[1, 2]", 3: "[3, 4]"}
	for i := range existing {
		existing[i].ID = int64(i + 1)
		existing[i].PathJSON = storedPaths[existing[i].EntryPointID]
	}

	retraced, retracedPaths, keep, keepPaths := IncrementalTraceFlows(
		nodes, edges, existing, []string{"a.go"}, DefaultFlowMaxDepth)

	if len(retraced) != 1 || retraced[0].EntryPointID != 1 {
		t.Fatalf("expected only the a.go flow re-traced, got %+v", retraced)
	}
	if !reflect.DeepEqual(retracedPaths[0], []int64{1, 2}) {
		t.Errorf("re-traced path = %v, want [1 2]", retracedPaths[0])
	}
	if len(keep) != 1 || keep[0].EntryPointID != 3 {
		t.Fatalf("expected the b.go flow kept, got %+v", keep)
	}
	if !reflect.DeepEqual(keepPaths[0], []int64{3, 4}) {
		t.Errorf("kept path = %v, want [3 4]", keepPaths[0])
	}
}

// TestIncrementalTraceFlows_RetracesAnEntryPointWhoseTailChanged proves the
// second relevance condition: the entry point's own file did NOT change, but it
// anchored a flow reaching into the changed file, so it must be re-traced.
// Filtering only on the entry point's file would silently drop the flow.
func TestIncrementalTraceFlows_RetracesAnEntryPointWhoseTailChanged(t *testing.T) {
	nodes := []graphstore.GraphNode{
		testNode(1, graphstore.NodeKindFunction, "head", "head.go"),
		testNode(2, graphstore.NodeKindFunction, "tail", "tail.go"),
	}
	edges := []graphstore.GraphEdge{
		testEdge(1, graphstore.EdgeKindCalls, "head.go::head", "tail.go::tail", "head.go"),
	}
	existing := []graphstore.FlowRow{
		{ID: 1, Name: "head", EntryPointID: 1, PathJSON: "[1, 2]"},
	}
	retraced, _, keep, _ := IncrementalTraceFlows(
		nodes, edges, existing, []string{"tail.go"}, DefaultFlowMaxDepth)
	if len(retraced) != 1 || retraced[0].EntryPointID != 1 {
		t.Fatalf("a flow whose TAIL changed must be re-traced, got %+v", retraced)
	}
	if len(keep) != 0 {
		t.Fatalf("the affected flow must not also be kept, got %+v", keep)
	}
}

// TestIncrementalTraceFlows_NoChangedFilesKeepsEverything proves the
// short-circuit: with nothing changed there is nothing to re-trace.
func TestIncrementalTraceFlows_NoChangedFilesKeepsEverything(t *testing.T) {
	existing := []graphstore.FlowRow{{ID: 1, EntryPointID: 1, PathJSON: "[1, 2]"}}
	retraced, _, keep, keepPaths := IncrementalTraceFlows(nil, nil, existing, nil, DefaultFlowMaxDepth)
	if len(retraced) != 0 {
		t.Fatalf("no changed files must re-trace nothing, got %+v", retraced)
	}
	if len(keep) != 1 || !reflect.DeepEqual(keepPaths[0], []int64{1, 2}) {
		t.Fatalf("existing flows must be kept verbatim, got %+v / %v", keep, keepPaths)
	}
}

// TestCommunitiesAffected_SkipGuard pins the incremental community guard:
// upstream skips re-detection entirely, and reports 0 communities, when no
// changed file holds a node that already belongs to one.
func TestCommunitiesAffected_SkipGuard(t *testing.T) {
	assigned := testNode(1, graphstore.NodeKindFunction, "a", "a.go")
	assigned.CommunityID = 7
	unassigned := testNode(2, graphstore.NodeKindFunction, "b", "b.go")
	nodes := []graphstore.GraphNode{assigned, unassigned}

	if !CommunitiesAffected(nodes, []string{"a.go"}) {
		t.Error("a changed file holding a community member must be affected")
	}
	if CommunitiesAffected(nodes, []string{"b.go"}) {
		t.Error("a changed file with no community member must NOT be affected")
	}
	if CommunitiesAffected(nodes, nil) {
		t.Error("no changed files must not be affected")
	}
}
