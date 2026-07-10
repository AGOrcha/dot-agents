package crg

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// bridgeNS is the mirror namespace the parity tests seed independently to model
// the legacy Python bridge's persisted state (the crg-bridge adapter's
// namespace). The derived-view parity is computed by reading BOTH namespaces
// back through the Store seam — never from corpus arithmetic — so a real
// storage divergence is detectable (proven by TestPostprocessParity_Catches*).
const bridgeNS = "crg-bridge"

// bridgeSeed writes c's graph into the bridge mirror namespace through a
// bootstrap token — the EXTERNAL process's grant, not the adapter (§11.2: the
// mirror adapter has no write path). Models the bridge having already persisted
// its state for this commit.
func bridgeSeed(t *testing.T, store *sdk.MemStore, c Corpus) {
	t.Helper()
	notes, edges := c.ToGraph()
	tok := sdk.BootstrapToken(bridgeNS)
	if err := store.WriteNotes(tok, bridgeNS, notes); err != nil {
		t.Fatalf("bridge seed notes: %v", err)
	}
	if err := store.WriteEdges(tok, bridgeNS, edges); err != nil {
		t.Fatalf("bridge seed edges: %v", err)
	}
}

// bootstrapNative ingests c through the kg-native adapter into the crg
// namespace and returns its store.
func bootstrapNative(t *testing.T, c Corpus) *sdk.MemStore {
	t.Helper()
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	if _, err := Bootstrap(s, store, c, nil); err != nil {
		t.Fatalf("native bootstrap: %v", err)
	}
	return store
}

// parityCorpusFiles loads the 10 pinned parity corpus files (reusing parityDir
// from crg_test.go, same package).
func parityCorpusFiles(t *testing.T) []string {
	t.Helper()
	dir := parityDir(t)
	files, err := SortedCorpusFiles(filepath.Join(dir, "corpus"))
	if err != nil {
		t.Fatalf("corpus files: %v", err)
	}
	if len(files) != 10 {
		t.Fatalf("parity corpus requires 10 commits, got %d", len(files))
	}
	return files
}

// TestPostprocessParity_TenCommitDualRead is the t6a hard test for the flows /
// communities / postprocess surfaces. For each of the 10 pinned commits it
// ingests the corpus through the kg-native adapter (writing kg_crg.*) AND seeds
// the bridge mirror state (kg_crg-bridge.*) INDEPENDENTLY, then compares the
// four derived views by READING BOTH NAMESPACES BACK from the Store seam under
// the O6-refinement-C / parity-proposal-§C oracles (NOT the literal §11.6
// "bytes-equivalent" text — see the Postprocess doc comment):
//
//   - flow_memberships — set equality (CompareFlowMemberships)
//   - communities      — partition equivalence (graphstore.PartitionAgreement)
//   - risk_index        — Spearman rank correlation (graphstore.SpearmanTau)
//   - fts               — token-set equality (CompareFTS)
func TestPostprocessParity_TenCommitDualRead(t *testing.T) {
	for i, f := range parityCorpusFiles(t) {
		c, err := LoadCorpus(f)
		if err != nil {
			t.Fatalf("commit %d load: %v", i, err)
		}
		nativeStore := bootstrapNative(t, c)
		bridgeStore := sdk.NewMemStore()
		bridgeSeed(t, bridgeStore, c)

		nativePP := postprocessOf(t, nativeStore, Name)
		bridgePP := postprocessOf(t, bridgeStore, bridgeNS)

		// flows — set equality
		if rep := CompareFlowMemberships(nativePP.FlowMemberships, bridgePP.FlowMemberships); !rep.Pass {
			t.Fatalf("commit %d flows parity FAILED: %v", i, rep.Detail)
		}
		if len(nativePP.FlowMemberships) == 0 {
			t.Fatalf("commit %d: expected nonempty flow_memberships", i)
		}
		// communities — partition equivalence
		agree, ok := graphstore.PartitionAgreement(nativePP.Communities, bridgePP.Communities)
		if !ok || agree != 1.0 {
			t.Fatalf("commit %d communities parity FAILED: agreement=%v ok=%v (want 1.0,true)", i, agree, ok)
		}
		if n := distinctClusters(nativePP.Communities); n < 2 {
			t.Fatalf("commit %d: community partition should be non-trivial, got %d clusters", i, n)
		}
		// risk_index — Spearman rank correlation
		tau, ok := graphstore.SpearmanTau(nativePP.RiskIndex, bridgePP.RiskIndex)
		if !ok || tau < graphstore.DefaultSpearmanTau {
			t.Fatalf("commit %d risk_index parity FAILED: tau=%v ok=%v (want >= %v,true)", i, tau, ok, graphstore.DefaultSpearmanTau)
		}
		// fts — token-set equality
		if rep := CompareFTS(nativePP.FTS, bridgePP.FTS); !rep.Pass {
			t.Fatalf("commit %d fts parity FAILED: %v", i, rep.Detail)
		}
		if len(nativePP.FTS) != len(c.Symbols) {
			t.Fatalf("commit %d: fts token count = %d, want %d (one per distinct symbol)", i, len(nativePP.FTS), len(c.Symbols))
		}
	}
}

// TestPostprocessParity_CatchesDivergence proves the readback-based parity is
// NOT tautological: when the bridge's persisted graph is missing a symbol that
// participates in a flow (and its edges), every derived-view oracle must FAIL.
func TestPostprocessParity_CatchesDivergence(t *testing.T) {
	c, err := LoadCorpus(parityCorpusFiles(t)[0])
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	nativeStore := bootstrapNative(t, c)
	nativePP := postprocessOf(t, nativeStore, Name)

	// Pick a genuine flow member (position > 0 → not an entry point) so removing
	// it from the bridge graph provably changes flow_memberships too.
	dropID := ""
	for _, m := range nativePP.FlowMemberships {
		if m.Position > 0 {
			dropID = m.MemberID
			break
		}
	}
	if dropID == "" {
		t.Fatal("no non-entry flow member found; corpus cannot exercise the flows divergence path")
	}
	divergent := removeSymbolByID(c, dropID)

	bridgeStore := sdk.NewMemStore()
	bridgeSeed(t, bridgeStore, divergent) // bridge is missing dropID + its edges
	bridgePP := postprocessOf(t, bridgeStore, bridgeNS)

	if rep := CompareFlowMemberships(nativePP.FlowMemberships, bridgePP.FlowMemberships); rep.Pass {
		t.Fatal("dropping a flow member from the bridge MUST fail flow_memberships parity")
	}
	if _, ok := graphstore.PartitionAgreement(nativePP.Communities, bridgePP.Communities); ok {
		t.Fatal("a dropped node changes the community node set — PartitionAgreement must report ok=false")
	}
	if _, ok := graphstore.SpearmanTau(nativePP.RiskIndex, bridgePP.RiskIndex); ok {
		t.Fatal("a dropped node changes the risk_index node set — SpearmanTau must report ok=false")
	}
	if rep := CompareFTS(nativePP.FTS, bridgePP.FTS); rep.Pass {
		t.Fatal("a dropped symbol MUST fail fts token-set parity")
	}
}

// postprocessOf reads a namespace back and computes its derived views.
func postprocessOf(t *testing.T, store *sdk.MemStore, ns string) Postprocess {
	t.Helper()
	pp, err := PostprocessFromStore(store, ns)
	if err != nil {
		t.Fatalf("postprocess %s: %v", ns, err)
	}
	return pp
}

// distinctClusters counts the distinct cluster ids in a partition.
func distinctClusters(partition map[string]string) int {
	seen := map[string]bool{}
	for _, c := range partition {
		seen[c] = true
	}
	return len(seen)
}

// removeSymbolByID returns a copy of c with the symbol whose id (qn@file) is
// dropID removed. ToGraph then drops every reference touching it as dangling,
// so the divergent corpus models a bridge graph genuinely missing that symbol.
func removeSymbolByID(c Corpus, dropID string) Corpus {
	out := Corpus{Commit: c.Commit, References: c.References}
	for _, s := range c.Symbols {
		if SymbolID(s) != dropID {
			out.Symbols = append(out.Symbols, s)
		}
	}
	return out
}

func TestFlowsFromStore_EntryPointsAndOrdering(t *testing.T) {
	c := Corpus{
		Commit: "flows",
		Symbols: []Symbol{
			{QualifiedName: "a", Kind: kindFn, Language: "go", FilePath: "a.go", ContentHash: "h"},
			{QualifiedName: "b", Kind: kindFn, Language: "go", FilePath: "b.go", ContentHash: "h"},
			{QualifiedName: "c", Kind: kindFn, Language: "go", FilePath: "c.go", ContentHash: "h"},
			{QualifiedName: "d", Kind: kindFn, Language: "go", FilePath: "d.go", ContentHash: "h"},
			{QualifiedName: "iso", Kind: kindType, Language: "go", FilePath: "iso.go", ContentHash: "h"},
		},
		References: []Reference{
			{Kind: edgeCalls, From: "a", To: "b"},
			{Kind: edgeCalls, From: "b", To: "c"},
			{Kind: edgeCalls, From: "a", To: "d"},
		},
	}
	store := bootstrapNative(t, c)
	flows, err := FlowsFromStore(store, Name)
	if err != nil {
		t.Fatalf("flows: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("expected exactly 1 flow (entry point a), got %d: %+v", len(flows), flows)
	}
	f := flows[0]
	if f.EntryPoint != "a" {
		t.Fatalf("entry point = %q, want a", f.EntryPoint)
	}
	if len(f.Members) == 0 || f.Members[0] != SymbolID(c.Symbols[0]) {
		t.Fatalf("flow member[0] must be the entry point id, got %v", f.Members)
	}
	if f.Criticality != 4 {
		t.Fatalf("criticality = %v, want 4 (a,b,c,d reachable)", f.Criticality)
	}
	members := map[string]bool{}
	for _, m := range f.Members {
		members[m] = true
	}
	for _, qn := range []string{"b", "c", "d"} {
		if !members[symIDOf(c, qn)] {
			t.Fatalf("flow should contain %q; members = %v", qn, f.Members)
		}
	}
	if members[symIDOf(c, "iso")] {
		t.Fatal("isolated non-called node must not be a flow member")
	}

	// flow_memberships positions are contiguous from 0 for the single flow.
	rows, err := FlowMembershipsFromStore(store, Name)
	if err != nil {
		t.Fatalf("flow memberships: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected 4 flow_membership rows, got %d", len(rows))
	}
	for pos, r := range rows {
		if r.Position != pos {
			t.Fatalf("row %d position = %d, want %d (contiguous)", pos, r.Position, pos)
		}
		if r.FlowID != f.ID {
			t.Fatalf("row %d flow id = %q, want %q", pos, r.FlowID, f.ID)
		}
	}
}

func TestCommunitiesFromStore_PartitionIgnoresTestedBy(t *testing.T) {
	c := Corpus{
		Commit: "communities",
		Symbols: []Symbol{
			{QualifiedName: "a", Kind: kindFn, FilePath: "a.go"},
			{QualifiedName: "b", Kind: kindFn, FilePath: "b.go"},
			{QualifiedName: "x", Kind: kindFn, FilePath: "x.go"},
			{QualifiedName: "y", Kind: kindType, FilePath: "y.go"},
			{QualifiedName: "iso", Kind: kindType, FilePath: "iso.go"},
		},
		References: []Reference{
			{Kind: edgeCalls, From: "a", To: "b"},    // cluster {a,b}
			{Kind: edgeImports, From: "x", To: "y"},  // cluster {x,y}
			{Kind: edgeTestedBy, From: "a", To: "x"}, // TESTED_BY must NOT merge clusters
		},
	}
	store := bootstrapNative(t, c)
	part, err := CommunitiesFromStore(store, Name)
	if err != nil {
		t.Fatalf("communities: %v", err)
	}
	if len(part) != 5 {
		t.Fatalf("partition must cover all 5 nodes, got %d", len(part))
	}
	aID, bID := symIDOf(c, "a"), symIDOf(c, "b")
	xID, yID := symIDOf(c, "x"), symIDOf(c, "y")
	isoID := symIDOf(c, "iso")
	if part[aID] != part[bID] {
		t.Fatal("a and b (CALLS-connected) must share a community")
	}
	if part[xID] != part[yID] {
		t.Fatal("x and y (IMPORTS-connected) must share a community")
	}
	if part[aID] == part[xID] {
		t.Fatal("TESTED_BY edge must NOT merge the {a,b} and {x,y} communities")
	}
	if part[isoID] != isoID {
		t.Fatalf("isolated node's cluster id must be itself, got %q", part[isoID])
	}
	if got := distinctClusters(part); got != 3 {
		t.Fatalf("expected 3 communities, got %d", got)
	}
}

func TestRiskIndexFromStore_DegreeCentrality(t *testing.T) {
	c := Corpus{
		Commit: "risk",
		Symbols: []Symbol{
			{QualifiedName: "hub", Kind: kindFn, FilePath: "hub.go"},
			{QualifiedName: "a", Kind: kindFn, FilePath: "a.go"},
			{QualifiedName: "b", Kind: kindFn, FilePath: "b.go"},
			{QualifiedName: "c", Kind: kindFn, FilePath: "c.go"},
			{QualifiedName: "z", Kind: kindType, FilePath: "z.go"},
		},
		References: []Reference{
			{Kind: edgeCalls, From: "hub", To: "a"},
			{Kind: edgeCalls, From: "hub", To: "b"},
			{Kind: edgeImports, From: "c", To: "hub"},
		},
	}
	store := bootstrapNative(t, c)
	risk, err := RiskIndexFromStore(store, Name)
	if err != nil {
		t.Fatalf("risk index: %v", err)
	}
	if got := risk[symIDOf(c, "hub")]; got != 3 {
		t.Fatalf("hub risk = %v, want 3 (2 out CALLS + 1 in IMPORTS)", got)
	}
	if got := risk[symIDOf(c, "a")]; got != 1 {
		t.Fatalf("a risk = %v, want 1", got)
	}
	if got := risk[symIDOf(c, "z")]; got != 0 {
		t.Fatalf("isolated z risk = %v, want 0", got)
	}
	if len(risk) != 5 {
		t.Fatalf("risk index must score all 5 nodes, got %d", len(risk))
	}
}

func TestFTSFromStore_SortedDistinctTokens(t *testing.T) {
	c := Corpus{
		Commit: "fts",
		Symbols: []Symbol{
			{QualifiedName: "zeta", Kind: kindFn, FilePath: "z.go"},
			{QualifiedName: "alpha", Kind: kindFn, FilePath: "a.go"},
			{QualifiedName: "dup", Kind: kindFn, FilePath: "a.go"},
			{QualifiedName: "dup", Kind: kindFn, FilePath: "b.go"}, // same qn, different file
		},
	}
	store := bootstrapNative(t, c)
	tokens, err := FTSFromStore(store, Name)
	if err != nil {
		t.Fatalf("fts: %v", err)
	}
	want := []string{"alpha", "dup", "zeta"}
	if strings.Join(tokens, ",") != strings.Join(want, ",") {
		t.Fatalf("fts tokens = %v, want sorted distinct %v", tokens, want)
	}
}

// symIDOf resolves the persisted symbol id for a qualified name in c (the first
// symbol with that name, matching ToGraph's id assignment).
func symIDOf(c Corpus, qn string) string {
	for _, s := range c.Symbols {
		if s.QualifiedName == qn {
			return SymbolID(s)
		}
	}
	return ""
}

// TestPostprocessFromStore_PropagatesReadError proves each derived-view surface
// propagates a Store readback failure rather than returning partial data.
func TestPostprocessFromStore_PropagatesReadError(t *testing.T) {
	fs := &edgeFailStore{} // notes read fine, edges read fails
	cases := []struct {
		name string
		run  func() error
	}{
		{"FlowsFromStore", func() error { _, err := FlowsFromStore(fs, Name); return err }},
		{"FlowMembershipsFromStore", func() error { _, err := FlowMembershipsFromStore(fs, Name); return err }},
		{"CommunitiesFromStore", func() error { _, err := CommunitiesFromStore(fs, Name); return err }},
		{"RiskIndexFromStore", func() error { _, err := RiskIndexFromStore(fs, Name); return err }},
		{"FTSFromStore", func() error { _, err := FTSFromStore(fs, Name); return err }},
		{"PostprocessFromStore", func() error { _, err := PostprocessFromStore(fs, Name); return err }},
	}
	for _, tc := range cases {
		if err := tc.run(); err == nil {
			t.Fatalf("%s must propagate an Edges read failure", tc.name)
		}
	}
}

func TestCompareFlowMemberships_SymmetricDifference(t *testing.T) {
	base := []FlowMembership{
		{FlowID: "f", MemberID: "a", Position: 0},
		{FlowID: "f", MemberID: "b", Position: 1},
	}
	if rep := CompareFlowMemberships(base, base); !rep.Pass {
		t.Fatalf("identical membership sets must pass: %v", rep.Detail)
	}
	extra := append(append([]FlowMembership{}, base...), FlowMembership{FlowID: "f", MemberID: "c", Position: 2})
	if rep := CompareFlowMemberships(extra, base); rep.Pass {
		t.Fatal("a row only in a must fail")
	}
	if rep := CompareFlowMemberships(base, extra); rep.Pass {
		t.Fatal("a row only in b must fail")
	}
}

func TestCompareFTS_SymmetricDifference(t *testing.T) {
	base := []string{"alpha", "beta"}
	if rep := CompareFTS(base, base); !rep.Pass {
		t.Fatalf("identical token sets must pass: %v", rep.Detail)
	}
	if rep := CompareFTS([]string{"alpha", "beta", "gamma"}, base); rep.Pass {
		t.Fatal("a token only in a must fail")
	}
	if rep := CompareFTS(base, []string{"alpha", "beta", "gamma"}); rep.Pass {
		t.Fatal("a token only in b must fail")
	}
}
