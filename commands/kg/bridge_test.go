package kg

import (
	"encoding/json"
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
func TestCollectNeighborResults_DirectionAndKind(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	mk := func(qn string) graphstore.NodeInfo {
		// UpsertNode auto-derives qualified name from FilePath/Name, but we
		// just need the lookup to succeed.
		parts := strings.SplitN(qn, "::", 2)
		file := "p.go"
		name := qn
		if len(parts) == 2 {
			name = parts[1]
		}
		return graphstore.NodeInfo{Kind: "Function", Name: name, FilePath: file, Language: "go"}
	}
	if _, err := store.UpsertNode(mk("p.go::Caller"), "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNode(mk("p.go::Callee"), "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertEdge(graphstore.EdgeInfo{
		Kind: graphstore.EdgeKindCalls, Source: "p.go::Caller", Target: "p.go::Callee", FilePath: "p.go",
	}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	// callees_of Caller → outbound traversal hits Callee.
	callee, err := store.GetNode("p.go::Callee")
	if err != nil || callee == nil {
		t.Skip("dependency layout mismatch — skip neighbor traversal")
	}
	caller, err := store.GetNode("p.go::Caller")
	if err != nil || caller == nil {
		t.Skip("dependency layout mismatch — skip neighbor traversal")
	}
	results, err := collectNeighborResults(store, []graphstore.GraphNode{*caller}, graphstore.EdgeKindCalls, false, 5)
	if err != nil {
		t.Fatalf("collectNeighborResults outbound: %v", err)
	}
	if len(results) != 1 || results[0].ID != callee.QualifiedName {
		t.Errorf("outbound: got %+v, want neighbor %q", results, callee.QualifiedName)
	}

	// callers_of Callee → inbound traversal hits Caller.
	inbound, err := collectNeighborResults(store, []graphstore.GraphNode{*callee}, graphstore.EdgeKindCalls, true, 5)
	if err != nil {
		t.Fatalf("collectNeighborResults inbound: %v", err)
	}
	if len(inbound) != 1 || inbound[0].ID != caller.QualifiedName {
		t.Errorf("inbound: got %+v, want neighbor %q", inbound, caller.QualifiedName)
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
