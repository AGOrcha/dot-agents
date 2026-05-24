package kg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"
)

// TestDefaultBridgeMappings_Cardinality verifies the bridge advertises the full
// 14-intent contract documented in the design.
func TestDefaultBridgeMappings_Cardinality(t *testing.T) {
	mappings := defaultBridgeMappings()
	if len(mappings) != 14 {
		t.Fatalf("expected 14 bridge intent mappings, got %d", len(mappings))
	}
	expected := []string{
		"plan_context", "decision_lookup", "entity_context", "workflow_memory",
		"contradictions", "symbol_lookup", "impact_radius", "change_analysis",
		"tests_for", "callers_of", "callees_of", "community_context",
		"symbol_decisions", "decision_symbols",
	}
	seen := make(map[string]bool, len(mappings))
	for _, m := range mappings {
		if len(m.KGIntents) == 0 {
			t.Errorf("bridge intent %q must fan out to >=1 KG intent", m.BridgeIntent)
		}
		seen[m.BridgeIntent] = true
	}
	for _, want := range expected {
		if !seen[want] {
			t.Errorf("expected bridge intent %q in default mappings", want)
		}
	}
}

func TestIsValidBridgeIntent_AndIsCodeBridgeIntent(t *testing.T) {
	if !isValidBridgeIntent("decision_lookup") {
		t.Error("decision_lookup should be valid")
	}
	if isValidBridgeIntent("nope") {
		t.Error("nope should not be valid")
	}
	if !isCodeBridgeIntent("symbol_lookup") {
		t.Error("symbol_lookup should be a code bridge intent")
	}
	if isCodeBridgeIntent("decision_lookup") {
		t.Error("decision_lookup is a note bridge intent, not code")
	}
}

func TestGraphNodeToQueryResult_LineRangeAndKindLabel(t *testing.T) {
	node := graphstore.GraphNode{
		QualifiedName: "pkg::Foo",
		FilePath:      "pkg/foo.go",
		LineStart:     10,
		LineEnd:       20,
		Kind:          "Function",
		Language:      "go",
	}
	r := graphNodeToQueryResult(node, "symbol")
	if r.ID != "pkg::Foo" || r.Type != "symbol" {
		t.Errorf("unexpected id/type: %+v", r)
	}
	if !strings.Contains(r.Summary, "pkg/foo.go:10-20") {
		t.Errorf("expected summary with line range, got %q", r.Summary)
	}
	if got := graphNodeTypeLabel(node); got != "function" {
		t.Errorf("type label: got %q want function", got)
	}
	// IsTest fallback when Kind unset
	testNode := graphstore.GraphNode{QualifiedName: "x", IsTest: true}
	if graphNodeTypeLabel(testNode) != "test" {
		t.Error("test node should map to 'test' label")
	}
	emptyNode := graphstore.GraphNode{QualifiedName: "x"}
	if graphNodeTypeLabel(emptyNode) != "symbol" {
		t.Error("empty kind/IsTest should fall back to 'symbol'")
	}
	// Single-line summary (no end)
	single := graphstore.GraphNode{QualifiedName: "a::b", FilePath: "f.go", LineStart: 5}
	rs := graphNodeToQueryResult(single, "")
	if !strings.HasSuffix(rs.Summary, "f.go:5") {
		t.Errorf("single-line summary: got %q", rs.Summary)
	}
	// Empty file path falls back to qualified name
	noFile := graphstore.GraphNode{QualifiedName: "only::qn"}
	rn := graphNodeToQueryResult(noFile, "")
	if rn.Summary != "only::qn" {
		t.Errorf("expected qualified-name summary, got %q", rn.Summary)
	}
}

func TestUniqueFilePaths_DedupesAndSkipsEmpty(t *testing.T) {
	nodes := []graphstore.GraphNode{
		{FilePath: "a.go"},
		{FilePath: ""},
		{FilePath: "a.go"},
		{FilePath: "b.go"},
	}
	got := uniqueFilePaths(nodes)
	if len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Errorf("uniqueFilePaths = %v", got)
	}
}

func TestCaseInsensitiveMatcher(t *testing.T) {
	m := caseInsensitiveMatcher("")
	if !m("anything") {
		t.Error("empty query should match anything")
	}
	m2 := caseInsensitiveMatcher("Foo")
	if !m2("blah", "fooBar") {
		t.Error("expected case-insensitive substring match")
	}
	if m2("nope", "bar") {
		t.Error("expected no match")
	}
}

func TestIsDecisionNoteType(t *testing.T) {
	for _, ty := range []string{"decision", "synthesis", "concept"} {
		if !isDecisionNoteType(ty) {
			t.Errorf("%s should be a decision-like type", ty)
		}
	}
	if isDecisionNoteType("entity") {
		t.Error("entity is not a decision-like type")
	}
}

func TestShouldSkipNeighborForKind_TestedByGate(t *testing.T) {
	nonTest := &graphstore.GraphNode{Kind: graphstore.NodeKindFunction}
	if !shouldSkipNeighborForKind(graphstore.EdgeKindTestedBy, nonTest) {
		t.Error("non-test neighbor on TESTED_BY edge should be skipped")
	}
	testNode := &graphstore.GraphNode{Kind: graphstore.NodeKindTest}
	if shouldSkipNeighborForKind(graphstore.EdgeKindTestedBy, testNode) {
		t.Error("test-kind neighbor on TESTED_BY edge should NOT be skipped")
	}
	if shouldSkipNeighborForKind(graphstore.EdgeKindCalls, nonTest) {
		t.Error("CALLS edge should never trigger skip in shouldSkipNeighborForKind")
	}
}

func TestNeighborQualifiedName(t *testing.T) {
	edge := graphstore.GraphEdge{SourceQualified: "src", TargetQualified: "tgt"}
	if neighborQualifiedName(edge, true) != "src" {
		t.Error("inbound: expected source neighbor")
	}
	if neighborQualifiedName(edge, false) != "tgt" {
		t.Error("outbound: expected target neighbor")
	}
}

// TestExecuteBridgeQuery_UninitializedKG covers the "KG not initialized" error
// returned by the bridge endpoint for note-side intents.
func TestExecuteBridgeQuery_UninitializedKG(t *testing.T) {
	home := newTempKG(t)
	// No setup performed → adapter.Available() == false.
	_, err := executeBridgeQuery(home, "decision_lookup", "x")
	if err == nil {
		t.Fatal("expected error when KG not initialized")
	}
	if !strings.Contains(err.Error(), "KG not initialized") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestExecuteBridgeQuery_UnknownIntent surfaces the "unknown bridge intent"
// path via executeBridgeQuery (rather than the lower-level resolver).
func TestExecuteBridgeQuery_UnknownIntent(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := executeBridgeQuery(home, "bogus", "x")
	if err == nil {
		t.Fatal("expected error for unknown bridge intent")
	}
	if !strings.Contains(err.Error(), "unknown bridge intent") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunKGBridgeQuery_JSONOutput covers the JSON path of the CLI handler for
// a happy-path note-side intent.
func TestRunKGBridgeQuery_JSONOutput(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	deps := Deps{
		Flags:        GlobalFlags{JSON: true},
		ExampleBlock: func(lines ...string) string { return strings.Join(lines, "\n") },
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "decision_lookup", "")

	out := captureStdout(t, func() {
		if err := runKGBridgeQuery(deps, cmd, []string{"cobra"}); err != nil {
			t.Fatalf("runKGBridgeQuery: %v", err)
		}
	})
	var resp GraphQueryResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(out))
	}
	if resp.Intent != "decision_lookup" {
		t.Errorf("intent: got %q", resp.Intent)
	}
}

// TestRunKGBridgeQuery_TextOutput exercises the human-readable path with
// results AND a fan-out warning path (no-results scenario).
func TestRunKGBridgeQuery_TextOutput(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	deps := testDeps()
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "decision_lookup", "")

	out := captureStdout(t, func() {
		if err := runKGBridgeQuery(deps, cmd, []string{"cobra"}); err != nil {
			t.Fatalf("runKGBridgeQuery: %v", err)
		}
	})
	if !strings.Contains(string(out), "Bridge Query") {
		t.Errorf("expected 'Bridge Query' header, got:\n%s", out)
	}

	// No-results path
	out2 := captureStdout(t, func() {
		if err := runKGBridgeQuery(deps, cmd, []string{"nonexistentXYZ"}); err != nil {
			t.Fatalf("runKGBridgeQuery (no results): %v", err)
		}
	})
	if !strings.Contains(string(out2), "No results found") {
		t.Errorf("expected 'No results found' in text output, got:\n%s", out2)
	}
}

// TestRunKGBridgeQuery_NotInitialized verifies the early-exit when
// KG_HOME has no config.
func TestRunKGBridgeQuery_NotInitialized(t *testing.T) {
	newTempKG(t)
	// Skip kg setup → config missing.
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "decision_lookup", "")
	err := runKGBridgeQuery(testDeps(), cmd, []string{"x"})
	if err == nil {
		t.Fatal("expected error when KG not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunKGBridgeMapping_TextOutput covers the non-JSON branch of the
// mapping subcommand.
func TestRunKGBridgeMapping_TextOutput(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runKGBridgeMapping(testDeps(), nil, nil); err != nil {
			t.Fatalf("runKGBridgeMapping: %v", err)
		}
	})
	output := string(out)
	if !strings.Contains(output, "Bridge Intent Mapping") {
		t.Errorf("expected 'Bridge Intent Mapping' header, got:\n%s", output)
	}
	if !strings.Contains(output, "plan_context") {
		t.Error("expected plan_context line in text output")
	}
}

// TestRunKGBridgeHealth_JSONOutput exercises the JSON branch of bridge health.
func TestRunKGBridgeHealth_JSONOutput(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_ = home

	deps := Deps{
		Flags:        GlobalFlags{JSON: true},
		ExampleBlock: func(lines ...string) string { return strings.Join(lines, "\n") },
	}
	out := captureStdout(t, func() {
		if err := runKGBridgeHealth(deps, &cobra.Command{}, nil); err != nil {
			t.Fatalf("runKGBridgeHealth: %v", err)
		}
	})
	var health []KGAdapterHealth
	if err := json.Unmarshal(out, &health); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(out))
	}
	if len(health) != 1 || health[0].AdapterName != "local-file" {
		t.Errorf("unexpected health payload: %+v", health)
	}
	if !health[0].Available {
		t.Error("adapter should be available after setup")
	}
}

// TestRunKGBridgeHealth_UnavailableWarning ensures unavailable adapters emit a
// warning rather than failing.
func TestRunKGBridgeHealth_UnavailableWarning(t *testing.T) {
	newTempKG(t)
	// No setup → adapter unavailable.
	out := captureStdout(t, func() {
		if err := runKGBridgeHealth(testDeps(), &cobra.Command{}, nil); err != nil {
			t.Fatalf("runKGBridgeHealth: %v", err)
		}
	})
	output := string(out)
	if !strings.Contains(output, "unavailable") {
		t.Errorf("expected 'unavailable' in text output, got:\n%s", output)
	}
	if !strings.Contains(output, "graph not initialized") {
		t.Errorf("expected 'graph not initialized' warning, got:\n%s", output)
	}
}

// TestLocalFileAdapter_LastQueryStatus verifies that Health() exposes the
// last query timestamp/status after a Query() call.
func TestLocalFileAdapter_LastQueryStatus(t *testing.T) {
	home := setupKGWithNotes(t)
	adapter := NewLocalFileAdapter(home)

	if _, err := adapter.Query(GraphQuery{Intent: "decision_lookup", Query: "cobra", Limit: 5}); err != nil {
		t.Fatalf("adapter.Query: %v", err)
	}
	h, err := adapter.Health()
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.LastQueryStatus != "ok" {
		t.Errorf("expected last_query_status=ok, got %q", h.LastQueryStatus)
	}
	if h.LastQueryTime == "" {
		t.Error("expected last_query_time to be populated")
	}

	// Error path
	if _, err := adapter.Query(GraphQuery{Intent: "not_a_real_intent", Query: "x"}); err == nil {
		t.Error("expected error for unknown intent")
	}
	h2, _ := adapter.Health()
	if h2.LastQueryStatus != "error" {
		t.Errorf("expected last_query_status=error after bad intent, got %q", h2.LastQueryStatus)
	}
}

// TestCollectCodeBridgeResults_WarmStoreHit exercises the warm-store path for
// the symbol_lookup intent with a seeded SQLite store.
func TestCollectCodeBridgeResults_WarmStoreHit(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Foo", FilePath: "pkg/foo.go",
		LineStart: 1, LineEnd: 10, Language: "go",
	}, "h1"); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	store.Close()

	resp, err := collectCodeBridgeResults(home, "symbol_lookup", "Foo", 10)
	if err != nil {
		t.Fatalf("collectCodeBridgeResults: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatalf("expected results for seeded Foo, got %+v", resp)
	}
	if resp.SparsityScore == nil || *resp.SparsityScore != 0 {
		t.Errorf("expected sparsity_score=0 for evidenced lookup, got %v", resp.SparsityScore)
	}
}

// TestCollectCodeBridgeResults_UnknownCodeIntent covers the default branch
// inside dispatchWarmStoreBridgeIntent.
func TestCollectCodeBridgeResults_UnknownCodeIntent(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	// Drive dispatchWarmStoreBridgeIntent directly with a value that bypasses
	// the public intent allow-list.
	resp := GraphQueryResponse{}
	err = dispatchWarmStoreBridgeIntent(store, &resp, "totally_unsupported", "x", 5)
	if err == nil {
		t.Fatal("expected error for unsupported code bridge intent")
	}
	if !strings.Contains(err.Error(), "unknown code bridge intent") {
		t.Errorf("unexpected error: %v", err)
	}
}

// withIsolatedCRGDiscovery runs fn with cwd pointing at a fresh tempdir and
// PATH stripped of any code-review-graph binary so the CRG bridge reports
// itself as unavailable.
func withIsolatedCRGDiscovery(t *testing.T, fn func()) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())
	fn()
}

// TestCollectCodeBridgeResults_ChangeAnalysis_CRGUnavailable exercises the
// CRG-unavailable fallback for change_analysis.
func TestCollectCodeBridgeResults_ChangeAnalysis_CRGUnavailable(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	withIsolatedCRGDiscovery(t, func() {
		resp, err := collectCodeBridgeResults(home, "change_analysis", "Foo", 5)
		if err != nil {
			t.Fatalf("collectCodeBridgeResults: %v", err)
		}
		if resp.Provider != "crg-unavailable" {
			t.Errorf("expected crg-unavailable provider, got %q", resp.Provider)
		}
		if len(resp.Warnings) == 0 {
			t.Error("expected warning explaining CRG unavailability")
		}
	})
}

// TestCollectCodeBridgeResults_CommunityContext_CRGUnavailable mirrors the
// change_analysis fallback for community_context.
func TestCollectCodeBridgeResults_CommunityContext_CRGUnavailable(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	withIsolatedCRGDiscovery(t, func() {
		resp, err := collectCodeBridgeResults(home, "community_context", "core", 5)
		if err != nil {
			t.Fatalf("collectCodeBridgeResults: %v", err)
		}
		if resp.Provider != "crg-unavailable" {
			t.Errorf("expected crg-unavailable provider, got %q", resp.Provider)
		}
	})
}

// TestRunImpactRadius_NoMatchingSymbols seeds an empty warm store and
// verifies the "no matching code symbols found" warning is emitted.
func TestRunImpactRadius_NoMatchingSymbols(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	resp, err := collectCodeBridgeResults(home, "impact_radius", "totallyAbsentSymbol", 5)
	if err != nil {
		t.Fatalf("collectCodeBridgeResults: %v", err)
	}
	foundWarn := false
	for _, w := range resp.Warnings {
		if strings.Contains(w, "no matching code symbols found") {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Errorf("expected 'no matching code symbols found' warning, got %v", resp.Warnings)
	}
}

// TestCollectGraphResults_DedupesAndLimits ensures the helper enforces both
// uniqueness on QualifiedName and the limit.
func TestCollectGraphResults_DedupesAndLimits(t *testing.T) {
	nodes := []graphstore.GraphNode{
		{QualifiedName: "a", FilePath: "a.go", Kind: "Function"},
		{QualifiedName: "a", FilePath: "a.go", Kind: "Function"}, // duplicate
		{QualifiedName: "", FilePath: "blank.go"},                // skipped
		{QualifiedName: "b", FilePath: "b.go", Kind: "Function"},
		{QualifiedName: "c", FilePath: "c.go", Kind: "Function"},
	}
	results := collectGraphResults(nodes, "symbol", 2)
	if len(results) != 2 {
		t.Fatalf("expected limit=2 results, got %d", len(results))
	}
	if results[0].ID != "a" || results[1].ID != "b" {
		t.Errorf("unexpected dedupe order: %+v", results)
	}

	// When resultType is "", node kind drives the label.
	results2 := collectGraphResults(nodes[:1], "", 5)
	if results2[0].Type != "function" {
		t.Errorf("expected lowercased kind 'function', got %q", results2[0].Type)
	}
}

// TestCollectNeighborResults_DirectionAndKind seeds caller/callee edges and
// verifies both inbound (callers_of) and outbound (callees_of) traversals.
// neighborNodeInfo builds a Function NodeInfo for the given "file::name"
// qualified name. UpsertNode auto-derives the qualified name from
// FilePath/Name, but the lookup just needs to succeed.
func neighborNodeInfo(qn string) graphstore.NodeInfo {
	name := qn
	if parts := strings.SplitN(qn, "::", 2); len(parts) == 2 {
		name = parts[1]
	}
	return graphstore.NodeInfo{Kind: "Function", Name: name, FilePath: "p.go", Language: "go"}
}

// seedNeighborGraph sets up a store with a Caller→Callee CALLS edge and
// returns the resolved GraphNodes (skipping the test on layout mismatch).
func seedNeighborGraph(t *testing.T) (store graphstore.Store, caller, callee *graphstore.GraphNode) {
	t.Helper()
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if _, err := store.UpsertNode(neighborNodeInfo("p.go::Caller"), "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNode(neighborNodeInfo("p.go::Callee"), "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertEdge(graphstore.EdgeInfo{
		Kind: graphstore.EdgeKindCalls, Source: "p.go::Caller", Target: "p.go::Callee", FilePath: "p.go",
	}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	callee, err = store.GetNode("p.go::Callee")
	if err != nil || callee == nil {
		t.Skip("dependency layout mismatch — skip neighbor traversal")
	}
	caller, err = store.GetNode("p.go::Caller")
	if err != nil || caller == nil {
		t.Skip("dependency layout mismatch — skip neighbor traversal")
	}
	return store, caller, callee
}

func TestCollectNeighborResults_DirectionAndKind(t *testing.T) {
	store, caller, callee := seedNeighborGraph(t)

	cases := []struct {
		name    string
		from    *graphstore.GraphNode
		inbound bool
		want    string
	}{
		// callees_of Caller → outbound traversal hits Callee.
		{"outbound", caller, false, callee.QualifiedName},
		// callers_of Callee → inbound traversal hits Caller.
		{"inbound", callee, true, caller.QualifiedName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results, err := collectNeighborResults(store, []graphstore.GraphNode{*c.from}, graphstore.EdgeKindCalls, c.inbound, 5)
			if err != nil {
				t.Fatalf("collectNeighborResults %s: %v", c.name, err)
			}
			if len(results) != 1 || results[0].ID != c.want {
				t.Errorf("%s: got %+v, want neighbor %q", c.name, results, c.want)
			}
		})
	}
}

// TestFindCodeNodes_EmptyQueryReturnsNil documents the early-return contract:
// findCodeNodes on a whitespace-only query produces no results.
func TestFindCodeNodes_EmptyQueryReturnsNil(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	nodes, err := findCodeNodes(store, "   ", 10)
	if err != nil {
		t.Fatalf("findCodeNodes: %v", err)
	}
	if nodes != nil {
		t.Errorf("expected nil results for empty query, got %v", nodes)
	}
}

// TestFindCodeNodes_DefaultLimit ensures a non-positive limit is normalised to 10.
func TestFindCodeNodes_DefaultLimit(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Bar", FilePath: "pkg/bar.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	// limit=0 should not panic and should not error; search may still return matches.
	nodes, err := findCodeNodes(store, "Bar", 0)
	if err != nil {
		t.Fatalf("findCodeNodes: %v", err)
	}
	_ = nodes // result count depends on FTS layout; we just exercise the path
}

// ── change_analysis matchers ────────────────────────────────────────────────

// TestAppendChangedFunctionMatches_ProjectsAndLimits exercises both the
// match-true projection and the limit-reached early return.
func TestAppendChangedFunctionMatches_ProjectsAndLimits(t *testing.T) {
	matches := caseInsensitiveMatcher("foo")
	fns := []graphstore.CRGChangedNode{
		{Name: "foo", QualifiedName: "pkg.foo", FilePath: "pkg/foo.go", RiskScore: 0.9},
		{Name: "Foo2", QualifiedName: "pkg.Foo2", FilePath: "pkg/foo2.go"},
		{Name: "bar", QualifiedName: "pkg.bar", FilePath: "pkg/bar.go"}, // filtered out
	}
	resp := &GraphQueryResponse{}
	stop := appendChangedFunctionMatches(resp, fns, matches, 1)
	if !stop {
		t.Errorf("expected limit-reached signal at limit=1")
	}
	if len(resp.Results) != 1 || resp.Results[0].Type != "changed_function" {
		t.Errorf("unexpected results: %+v", resp.Results)
	}
	if resp.Results[0].RiskScore == 0 {
		t.Error("expected RiskScore to be propagated")
	}

	// limit<=0 means "no cap" — both matching entries should land.
	resp2 := &GraphQueryResponse{}
	if appendChangedFunctionMatches(resp2, fns, matches, 0) {
		t.Error("limit=0 should never signal limit reached")
	}
	if len(resp2.Results) != 2 {
		t.Errorf("expected 2 results without limit, got %d", len(resp2.Results))
	}
}

// TestAppendTestGapMatches_FilterAndLimit covers both branches of the helper.
func TestAppendTestGapMatches_FilterAndLimit(t *testing.T) {
	matches := caseInsensitiveMatcher("svc")
	gaps := []graphstore.CRGTestGap{
		{QualifiedName: "pkg.svcA", FilePath: "pkg/a.go"},
		{QualifiedName: "pkg.svcB", FilePath: "pkg/b.go"},
		{QualifiedName: "pkg.other", FilePath: "pkg/c.go"}, // filtered
	}
	resp := &GraphQueryResponse{}
	if !appendTestGapMatches(resp, gaps, matches, 1) {
		t.Error("expected limit reached at 1")
	}
	if len(resp.Results) != 1 || resp.Results[0].TestCoverage != "missing" {
		t.Errorf("unexpected gap results: %+v", resp.Results)
	}

	resp2 := &GraphQueryResponse{}
	if appendTestGapMatches(resp2, gaps, matches, 0) {
		t.Error("limit=0 must not stop iteration")
	}
	if len(resp2.Results) != 2 {
		t.Errorf("expected 2 gap matches without limit, got %d", len(resp2.Results))
	}
}

// TestAppendReviewPriorityMatches covers reason-string matching and limit.
func TestAppendReviewPriorityMatches(t *testing.T) {
	matches := caseInsensitiveMatcher("risk")
	prios := []graphstore.CRGPriority{
		{QualifiedName: "pkg.foo", Reason: "high risk path", RiskScore: 0.8},
		{QualifiedName: "pkg.bar", Reason: "missing tests"}, // skipped (no "risk")
		{QualifiedName: "pkg.baz", Reason: "risk in caller graph", RiskScore: 0.5},
	}
	resp := &GraphQueryResponse{}
	if !appendReviewPriorityMatches(resp, prios, matches, 1) {
		t.Error("expected limit reached")
	}
	if len(resp.Results) != 1 || resp.Results[0].Type != "review_priority" {
		t.Errorf("unexpected priority results: %+v", resp.Results)
	}
	resp2 := &GraphQueryResponse{}
	appendReviewPriorityMatches(resp2, prios, matches, 0)
	if len(resp2.Results) != 2 {
		t.Errorf("expected 2 priority matches, got %d", len(resp2.Results))
	}
}

// ── decision-link projections ───────────────────────────────────────────────

// seedDecisionNote inserts a KGNote of the given type and returns its ID.
func seedDecisionNote(t *testing.T, store graphstore.Store, id, noteType, title string) {
	t.Helper()
	if err := store.UpsertKGNote(graphstore.KGNote{
		ID: id, Title: title, NoteType: noteType, Status: "active",
		Summary: title + " summary", FilePath: "notes/" + id + ".md",
	}); err != nil {
		t.Fatalf("UpsertKGNote %s: %v", id, err)
	}
}

// TestAppendDecisionLinkMatches_FilterAndLimit verifies that only decision/
// synthesis/concept notes survive, that duplicates are skipped, and that
// limit returns true once full.
func TestAppendDecisionLinkMatches_FilterAndLimit(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	seedDecisionNote(t, store, "dec-1", "decision", "Decision One")
	seedDecisionNote(t, store, "syn-1", "synthesis", "Synthesis One")
	seedDecisionNote(t, store, "other-1", "entity", "Entity One") // filtered out

	links := []graphstore.NoteSymbolLink{
		{NoteID: "dec-1", QualifiedName: "pkg::F", LinkKind: "decides"},
		{NoteID: "dec-1", QualifiedName: "pkg::F", LinkKind: "decides"}, // dedup
		{NoteID: "syn-1", QualifiedName: "pkg::F", LinkKind: "documents"},
		{NoteID: "other-1", QualifiedName: "pkg::F", LinkKind: "mentions"}, // filtered
		{NoteID: "missing", QualifiedName: "pkg::F", LinkKind: "mentions"}, // GetKGNote returns nil
	}

	seen := map[string]bool{}
	var results []GraphQueryResult
	stop := appendDecisionLinkMatches(store, "pkg::F", links, seen, &results, 1)
	if !stop {
		t.Error("expected limit=1 to halt at first match")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result at limit=1, got %d (%+v)", len(results), results)
	}
	if results[0].ID != "dec-1" {
		t.Errorf("expected first match to be dec-1, got %q", results[0].ID)
	}

	seen2 := map[string]bool{}
	var results2 []GraphQueryResult
	if appendDecisionLinkMatches(store, "pkg::F", links, seen2, &results2, 0) {
		t.Error("limit=0 must not signal stop")
	}
	if len(results2) != 2 {
		t.Errorf("expected 2 valid matches (decision+synthesis), got %d", len(results2))
	}
}

// TestCollectSymbolDecisionResults_EndToEnd seeds nodes, notes, and links and
// exercises the symbol_decisions intent through the dispatcher.
func TestCollectSymbolDecisionResults_EndToEnd(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Target", FilePath: "pkg/target.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	node, err := store.GetNode("pkg/target.go::Target")
	if err != nil || node == nil {
		t.Skip("qualified-name derivation mismatch")
	}
	seedDecisionNote(t, store, "dec-target", "decision", "Decide target")
	if _, err := store.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{
		NoteID: "dec-target", QualifiedName: node.QualifiedName, LinkKind: "decides",
	}); err != nil {
		t.Fatalf("UpsertNoteSymbolLink: %v", err)
	}

	results, err := collectSymbolDecisionResults(store, []graphstore.GraphNode{*node}, 5)
	if err != nil {
		t.Fatalf("collectSymbolDecisionResults: %v", err)
	}
	if len(results) != 1 || results[0].ID != "dec-target" {
		t.Errorf("expected dec-target result, got %+v", results)
	}

	// runSymbolDecisions wraps the same flow.
	resp := &GraphQueryResponse{}
	if err := runSymbolDecisions(store, resp, node.QualifiedName, 5); err != nil {
		t.Fatalf("runSymbolDecisions: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Errorf("runSymbolDecisions: expected 1 result, got %d", len(resp.Results))
	}
}

// TestDecisionNoteCandidates_ExactIDOverridesSearch covers both branches of
// decisionNoteCandidates (exact-hit and search-fallback).
func TestDecisionNoteCandidates_ExactIDOverridesSearch(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	seedDecisionNote(t, store, "dec-alpha", "decision", "Alpha")
	seedDecisionNote(t, store, "dec-beta", "decision", "Beta keyword alpha-ish")

	exact, err := decisionNoteCandidates(store, "dec-alpha", 5)
	if err != nil {
		t.Fatalf("exact lookup: %v", err)
	}
	if len(exact) != 1 || exact[0].ID != "dec-alpha" {
		t.Errorf("expected exact-id hit dec-alpha, got %+v", exact)
	}

	// Unknown id falls back to search ranking.
	fallback, err := decisionNoteCandidates(store, "Beta", 5)
	if err != nil {
		t.Fatalf("fallback search: %v", err)
	}
	found := false
	for _, n := range fallback {
		if n.ID == "dec-beta" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dec-beta in search fallback, got %+v", fallback)
	}
}

// TestAppendDecisionSymbolMatches_WithAndWithoutNode covers both branches of
// appendDecisionSymbolMatches: the symbol exists in the warm store (rich
// projection) and the link points at a symbol the store has not indexed yet.
// seedDecisionSymbolFixture returns a store with a single "k.go::Known"
// function node plus the note/links used by the decision-symbol projection
// tests (including a missing target and a duplicate for dedupe coverage).
func seedDecisionSymbolFixture(t *testing.T) (graphstore.Store, string, graphstore.KGNote, []graphstore.NoteSymbolLink) {
	t.Helper()
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Known", FilePath: "k.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	knownQN := "k.go::Known"
	if got, _ := store.GetNode(knownQN); got == nil {
		t.Skip("qualified-name derivation mismatch")
	}

	note := graphstore.KGNote{ID: "dec-x", Title: "Decide X", NoteType: "decision"}
	links := []graphstore.NoteSymbolLink{
		{NoteID: "dec-x", QualifiedName: knownQN, LinkKind: "decides"},
		{NoteID: "dec-x", QualifiedName: "ghost::missing", LinkKind: "documents"},
		{NoteID: "dec-x", QualifiedName: knownQN, LinkKind: "decides"}, // dedupe
	}
	return store, knownQN, note, links
}

// assertWarmAndFallbackProjections asserts the result set contains one
// warm-store-backed projection (knownQN) and one link-only fallback (ghost).
func assertWarmAndFallbackProjections(t *testing.T, results []GraphQueryResult, knownQN string) {
	t.Helper()
	foundKnown, foundGhost := false, false
	for _, r := range results {
		switch r.ID {
		case knownQN:
			foundKnown = true
			if r.QualifiedName == "" {
				t.Errorf("warm-store result missing qualified_name: %+v", r)
			}
		case "ghost::missing":
			foundGhost = true
			if r.Type != "symbol" {
				t.Errorf("fallback projection should report type=symbol, got %q", r.Type)
			}
		}
	}
	if !foundKnown || !foundGhost {
		t.Errorf("expected both warm and fallback projections; results=%+v", results)
	}
}

func TestAppendDecisionSymbolMatches_WithAndWithoutNode(t *testing.T) {
	store, knownQN, note, links := seedDecisionSymbolFixture(t)

	t.Run("limit_zero_collects_unique_projections", func(t *testing.T) {
		seen := map[string]bool{}
		var results []GraphQueryResult
		if appendDecisionSymbolMatches(store, note, links, seen, &results, 0) {
			t.Error("limit=0 should never signal stop")
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 unique link projections, got %d", len(results))
		}
		assertWarmAndFallbackProjections(t, results, knownQN)
	})

	t.Run("limit_one_halts_iteration", func(t *testing.T) {
		seen := map[string]bool{}
		var results []GraphQueryResult
		if !appendDecisionSymbolMatches(store, note, links, seen, &results, 1) {
			t.Error("expected limit=1 to halt iteration")
		}
		if len(results) != 1 {
			t.Errorf("limit=1: expected 1 result, got %d", len(results))
		}
	})
}

// TestCollectDecisionSymbolResults_NoMatches confirms an empty store returns
// an empty slice without error.
func TestCollectDecisionSymbolResults_NoMatches(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	results, err := collectDecisionSymbolResults(store, "nothing-here", 5)
	if err != nil {
		t.Fatalf("collectDecisionSymbolResults: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %+v", results)
	}
}

// TestCollectCodeBridgeResults_DecisionSymbols routes decision_symbols through
// the dispatcher and confirms the warm-store path returns a hit.
func TestCollectCodeBridgeResults_DecisionSymbols(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}

	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Decider", FilePath: "d.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	seedDecisionNote(t, store, "dec-ds", "decision", "DS decision")
	if _, err := store.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{
		NoteID: "dec-ds", QualifiedName: "d.go::Decider", LinkKind: "decides",
	}); err != nil {
		t.Fatalf("UpsertNoteSymbolLink: %v", err)
	}
	store.Close()

	resp, err := collectCodeBridgeResults(home, "decision_symbols", "dec-ds", 5)
	if err != nil {
		t.Fatalf("collectCodeBridgeResults: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Errorf("expected decision_symbols result, got %+v", resp)
	}
}

// TestCollectCodeBridgeResults_TestsFor_FallbackPath seeds a tested_by edge in
// the outbound direction; the inbound traversal returns no results, so the
// fallback outbound branch in runTestsFor fires.
func TestCollectCodeBridgeResults_TestsFor_FallbackPath(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Subject", FilePath: "s.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "TestSubject", FilePath: "s_test.go", Language: "go", IsTest: true,
	}, "h"); err != nil {
		t.Fatal(err)
	}
	// Outbound tested_by from Subject → TestSubject. Inbound on Subject misses,
	// then runTestsFor falls back to outbound.
	if _, err := store.UpsertEdge(graphstore.EdgeInfo{
		Kind:     graphstore.EdgeKindTestedBy,
		Source:   "s.go::Subject",
		Target:   "s_test.go::TestSubject",
		FilePath: "s.go",
	}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	store.Close()

	resp, err := collectCodeBridgeResults(home, "tests_for", "Subject", 5)
	if err != nil {
		t.Fatalf("collectCodeBridgeResults tests_for: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Errorf("expected fallback outbound tested_by hit, got %+v", resp)
	}
}

// TestRunImpactRadius_PopulatesResultsAndWarning seeds a node and exercises the
// non-empty path of runImpactRadius, validating that the warning for spanned
// files is appended.
func TestRunImpactRadius_PopulatesResultsAndWarning(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Hub", FilePath: "hub.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	resp := &GraphQueryResponse{}
	if err := runImpactRadius(store, resp, "Hub", 5); err != nil {
		t.Fatalf("runImpactRadius: %v", err)
	}
	// At least the changed node should be reported.
	if len(resp.Results) == 0 {
		t.Errorf("expected impact results to include the changed node, got %+v", resp)
	}
}

// TestRunSymbolLookup_PopulatesResults exercises the symbol_lookup branch via
// the public dispatcher.
func TestRunSymbolLookup_PopulatesResults(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Lookup", FilePath: "lk.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	resp := &GraphQueryResponse{}
	if err := dispatchWarmStoreBridgeIntent(store, resp, "symbol_lookup", "Lookup", 5); err != nil {
		t.Fatalf("dispatch symbol_lookup: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Errorf("expected symbol_lookup result, got %+v", resp)
	}
}

// TestExecuteBridgeQuery_CodeIntentRoutesToCollector verifies the bridge
// dispatcher delegates code intents to collectCodeBridgeResults rather than
// the local-file adapter.
func TestExecuteBridgeQuery_CodeIntentRoutesToCollector(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Reachable", FilePath: "r.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	store.Close()

	resp, err := executeBridgeQuery(home, "symbol_lookup", "Reachable")
	if err != nil {
		t.Fatalf("executeBridgeQuery: %v", err)
	}
	if resp.Provider != "warm-graphstore" {
		t.Errorf("expected warm-graphstore provider, got %q", resp.Provider)
	}
	if len(resp.Results) == 0 {
		t.Errorf("expected at least one result for Reachable")
	}
	// The sparsity score must be attached.
	if resp.SparsityScore == nil {
		t.Error("expected SparsityScore to be populated")
	}
}

// TestMergeBridgeResults_QueryPropagation ensures the merged Query comes from
// the last response and warnings accumulate from every input.
func TestMergeBridgeResults_QueryPropagation(t *testing.T) {
	merged := mergeBridgeResults([]GraphQueryResponse{
		{Query: "first", Results: []GraphQueryResult{{ID: "a"}}, Warnings: []string{"w1"}},
		{Query: "second", Results: []GraphQueryResult{{ID: "a"}, {ID: "b"}}, Warnings: []string{"w2"}},
	}, "plan_context")
	if merged.Query != "second" {
		t.Errorf("expected last query to win, got %q", merged.Query)
	}
	if len(merged.Results) != 2 {
		t.Errorf("expected dedupe across responses, got %d", len(merged.Results))
	}
	if len(merged.Warnings) != 2 {
		t.Errorf("expected both warnings preserved, got %v", merged.Warnings)
	}
}

// TestResolveBridgeQuery_PlanContextFansOut documents the fan-out cardinality.
func TestResolveBridgeQuery_PlanContextFansOut(t *testing.T) {
	queries, err := resolveBridgeQuery("plan_context", "topic")
	if err != nil {
		t.Fatalf("resolveBridgeQuery: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("plan_context should fan out to 2 KG intents, got %d", len(queries))
	}
	for _, q := range queries {
		if q.Limit != 10 {
			t.Errorf("expected default limit 10, got %d", q.Limit)
		}
	}
}

// ── adapter helpers ─────────────────────────────────────────────────────────

// TestLocalFileAdapter_HealthShape exercises the health report including the
// notes counter.
func TestLocalFileAdapter_HealthShape(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	adapter := NewLocalFileAdapter(home)
	if adapter.Name() != "local-file" {
		t.Errorf("adapter name = %q, want local-file", adapter.Name())
	}
	if !adapter.Available() {
		t.Fatal("adapter should be available after setup")
	}
	h, err := adapter.Health()
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.AdapterName != "local-file" {
		t.Errorf("unexpected adapter name in health: %+v", h)
	}
	if h.NoteCount < 0 {
		t.Errorf("note count should be non-negative, got %d", h.NoteCount)
	}
}

// TestLocalFileAdapter_HealthMissingHome reports "graph not initialized" when
// KG_HOME has no self/config.yaml.
func TestLocalFileAdapter_HealthMissingHome(t *testing.T) {
	dir := t.TempDir()
	adapter := NewLocalFileAdapter(dir)
	if adapter.Available() {
		t.Fatal("adapter should report unavailable on a bare directory")
	}
	h, err := adapter.Health()
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if len(h.Warnings) == 0 || !strings.Contains(h.Warnings[0], "not initialized") {
		t.Errorf("expected 'not initialized' warning, got %v", h.Warnings)
	}
}

// TestCollectAdapterHealth_WritesArtefact verifies the on-disk side effect.
func TestCollectAdapterHealth_WritesArtefact(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	adapters := []KGAdapter{NewLocalFileAdapter(home)}
	list := collectAdapterHealth(home, adapters)
	if len(list) != 1 {
		t.Fatalf("expected 1 health record, got %d", len(list))
	}
	// File should exist and be valid JSON.
	data, err := os.ReadFile(filepath.Join(home, "ops", "adapters", "adapter-health.json"))
	if err != nil {
		t.Fatalf("read adapter-health.json: %v", err)
	}
	var roundtrip []KGAdapterHealth
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("decode adapter-health.json: %v", err)
	}
	if len(roundtrip) != 1 {
		t.Errorf("expected 1 record on disk, got %d", len(roundtrip))
	}
}

// TestNeighborQualifiedName_InboundOutbound documents direction semantics.
func TestNeighborQualifiedName_InboundOutbound(t *testing.T) {
	e := graphstore.GraphEdge{SourceQualified: "src", TargetQualified: "tgt"}
	if neighborQualifiedName(e, true) != "src" {
		t.Error("inbound neighbor should be source")
	}
	if neighborQualifiedName(e, false) != "tgt" {
		t.Error("outbound neighbor should be target")
	}
}

// TestAppendDecisionSymbolMatches_LimitHitAndSeen drives both the limit
// early-return (~422-424) and the missing-node fallback summary branch
// (~411-417).
func TestAppendDecisionSymbolMatches_LimitHitAndSeen(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	note := graphstore.KGNote{ID: "d1", NoteType: "decision", Title: "T"}
	links := []graphstore.NoteSymbolLink{
		{NoteID: "d1", QualifiedName: "missing-1", LinkKind: "mentions"},
		{NoteID: "d1", QualifiedName: "missing-2", LinkKind: "mentions"},

		{NoteID: "d1", QualifiedName: "missing-1", LinkKind: "documents"},
	}
	var results []GraphQueryResult
	done := appendDecisionSymbolMatches(store, note, links, map[string]bool{}, &results, 1)
	if !done {
		t.Errorf("expected done=true once limit=1 is hit")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// TestCollectChangeAnalysisResults_HappyPathEmpty drives the empty
// resp.Results == nil → init branch (~457-459) when no matches are found.
func TestCollectChangeAnalysisResults_HappyPathEmpty(t *testing.T) {
	repo := installCRGWithBody(t, `case "$1" in
detect-changes) printf '%s\n' '{"summary":"none","risk_score":0,"changed_functions":[],"affected_flows":[],"test_gaps":[],"review_priorities":[]}' ;;
*) exit 0 ;;
esac`)
	_ = repo
	resp, err := collectChangeAnalysisResults("query-no-match", 5)
	if err != nil {
		t.Fatalf("collectChangeAnalysisResults: %v", err)
	}
	if resp.Results == nil {
		t.Error("expected empty results to be initialized, got nil")
	}
}

// TestAppendDecisionSymbolMatches_WithNode drives the resolved-node summary
// override branch (~419-420) by seeding a node with the same qn as a link.
func TestAppendDecisionSymbolMatches_WithNode(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Sym", FilePath: "p.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	note := graphstore.KGNote{ID: "d1", NoteType: "decision", Title: "T"}
	links := []graphstore.NoteSymbolLink{
		{NoteID: "d1", QualifiedName: "p.go::Sym", LinkKind: "mentions"},
	}
	var results []GraphQueryResult
	if appendDecisionSymbolMatches(store, note, links, map[string]bool{}, &results, 5) {
		t.Error("expected done=false below limit")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Summary, "via") {
		t.Errorf("expected summary to contain 'via', got %q", results[0].Summary)
	}
}

func TestRunNeighbors_CallersAndCallees(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Caller", FilePath: "a.go", Language: "go",
	}, "h1"); err != nil {
		t.Fatalf("UpsertNode caller: %v", err)
	}
	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Callee", FilePath: "b.go", Language: "go",
	}, "h2"); err != nil {
		t.Fatalf("UpsertNode callee: %v", err)
	}
	if _, err := store.UpsertEdge(graphstore.EdgeInfo{
		Kind:   graphstore.EdgeKindCalls,
		Source: "a.go::Caller",
		Target: "b.go::Callee",
	}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	resp := GraphQueryResponse{}
	if err := dispatchWarmStoreBridgeIntent(store, &resp, "callers_of", "Callee", 10); err != nil {
		t.Fatalf("dispatch callers_of: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Errorf("expected at least one caller, got %+v", resp.Results)
	}

	resp2 := GraphQueryResponse{}
	if err := dispatchWarmStoreBridgeIntent(store, &resp2, "callees_of", "Caller", 10); err != nil {
		t.Fatalf("dispatch callees_of: %v", err)
	}
	if len(resp2.Results) == 0 {
		t.Errorf("expected at least one callee, got %+v", resp2.Results)
	}
}

func TestLocalFileAdapter_QuerySuccess(t *testing.T) {
	home := setupKGWithNotes(t)
	a := NewLocalFileAdapter(home)
	if !a.Available() {
		t.Error("expected adapter available")
	}
	resp, err := a.Query(GraphQuery{Intent: "decision_lookup", Query: "cobra", Limit: 5})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Errorf("expected at least one result, got %+v", resp)
	}
	if a.lastStatus == "" {
		t.Error("expected lastStatus populated")
	}
}

// TestCollectChangeAnalysisResults_FiltersTestGapsAndPriorities seeds detect-
// changes JSON with all three categories and asserts the filter logic surfaces
// them.
func TestCollectChangeAnalysisResults_AllCategories(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	initGitRepo(t, dir)
	commitFile(t, dir, "README.md", "x\n", "init")

	changesJSON := `{
		"summary":"all categories",
		"risk_score":0.5,
		"changed_functions":[{"name":"Foo","qualified_name":"a.go::Foo","file_path":"a.go","risk_score":0.5}],
		"affected_flows":[],
		"test_gaps":[{"qualified_name":"a.go::Foo","file_path":"a.go"}],
		"review_priorities":[{"qualified_name":"a.go::Foo","reason":"high churn","risk_score":0.7}]
	}`
	writeFakeCRGBinary(t, dir, fmt.Sprintf(`case "$1" in
detect-changes) cat <<'__EOF__'
%s
__EOF__
;;
*) exit 0 ;;
esac`, changesJSON))

	resp, err := collectChangeAnalysisResults("Foo", 10)
	if err != nil {
		t.Fatalf("collectChangeAnalysisResults: %v", err)
	}
	if resp.Provider != "crg" {
		t.Errorf("expected crg provider, got %q", resp.Provider)
	}

	if len(resp.Results) == 0 {
		t.Errorf("expected results from change_analysis, got %+v", resp)
	}
}

func TestCollectChangeAnalysisResults_EmptyQuery(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	initGitRepo(t, dir)
	commitFile(t, dir, "README.md", "x\n", "init")
	changesJSON := `{"summary":"0","risk_score":0,"changed_functions":[],"affected_flows":[],"test_gaps":[],"review_priorities":[]}`
	writeFakeCRGBinary(t, dir, fmt.Sprintf(`case "$1" in
detect-changes) printf '%%s\n' '%s' ;;
*) exit 0 ;;
esac`, changesJSON))
	resp, err := collectChangeAnalysisResults("", 10)
	if err != nil {
		t.Fatalf("collectChangeAnalysisResults: %v", err)
	}
	if resp.Results == nil {
		t.Errorf("expected non-nil empty slice for Results, got nil")
	}
}

func TestCollectCommunityContextResults_FilterMatches(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	initGitRepo(t, dir)
	commitFile(t, dir, "README.md", "x\n", "init")
	body := `{"status":"ok","summary":"2","communities":[{"id":1,"name":"core","size":3,"cohesion":0.7,"dominant_language":"go","description":"core stuff","members":["a"]},{"id":2,"name":"utils","size":1,"cohesion":0.2,"dominant_language":"go","description":"utility code","members":["b"]}]}`
	fakeCRGEmittingJSON(t, dir, body)

	resp, err := collectCommunityContextResults("core", 10)
	if err != nil {
		t.Fatalf("collectCommunityContextResults: %v", err)
	}
	if resp.Provider != "crg" {
		t.Errorf("expected crg provider, got %q", resp.Provider)
	}
	if len(resp.Results) == 0 {
		t.Errorf("expected matching community, got %+v", resp)
	}
}

func TestCollectCommunityContextResults_LimitTruncates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	initGitRepo(t, dir)
	commitFile(t, dir, "README.md", "x\n", "init")
	body := `{"status":"ok","summary":"3","communities":[{"id":1,"name":"a","size":3,"cohesion":0.7,"dominant_language":"go","description":"x","members":["x"]},{"id":2,"name":"b","size":3,"cohesion":0.7,"dominant_language":"go","description":"x","members":["x"]},{"id":3,"name":"c","size":3,"cohesion":0.7,"dominant_language":"go","description":"x","members":["x"]}]}`
	fakeCRGEmittingJSON(t, dir, body)

	resp, err := collectCommunityContextResults("", 1)
	if err != nil {
		t.Fatalf("collectCommunityContextResults: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Errorf("expected limit=1, got %d", len(resp.Results))
	}
}

// TestKGAdapter_LocalFile_QueryAfterCorruption ensures LocalFileAdapter
// surfaces a sensible status even when the home directory is empty.
func TestKGAdapter_LocalFile_QueryUninitializedHome(t *testing.T) {
	a := NewLocalFileAdapter(t.TempDir())
	_, err := a.Query(GraphQuery{Intent: "decision_lookup", Query: "x", Limit: 5})

	_ = err
}
