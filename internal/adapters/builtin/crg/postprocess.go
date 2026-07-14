package crg

import (
	"sort"
	"strconv"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Edge kinds the derived-view computations partition the graph by. They mirror
// the schema.yaml edge_types (CALLS/TESTED_BY/IMPORTS) so the derivation and the
// schema share one spelling.
const (
	edgeCalls    = "CALLS"
	edgeImports  = "IMPORTS"
	edgeTestedBy = "TESTED_BY"
)

// FlowMembership is one (flow_id, member_id, position) row of the
// flow_memberships materialized view (§11.1 `flows` row). flow_id keys the
// execution flow by its entry-point symbol id; member_id is the symbol at
// `position` along the flow's deterministic CALLS traversal (position 0 is the
// entry point). The §11.6 `flows` parity oracle is SET EQUALITY over these
// (flow_id, member_id, position) rows — per O6 refinement C / the parity
// proposal §C, which replaces the un-testable "bytes-equivalent" criterion.
type FlowMembership struct {
	FlowID   string
	MemberID string
	Position int
}

// Flow is one execution flow: an entry-point symbol plus the ordered members
// reachable from it over CALLS edges, and a derived criticality (StepCount).
// It mirrors the legacy bridge's FlowInfo shape (entry point, step count,
// criticality) so the migration is field-comparable, but the parity oracle is
// the flow_memberships row set, not the free-text bridge summary.
type Flow struct {
	// ID is the entry-point symbol id (qualified_name@file_path) — the flow_id.
	ID string
	// EntryPoint is the entry-point symbol's qualified name.
	EntryPoint string
	// Members are the ordered member ids; position == index (entry at 0).
	Members []string
	// Criticality is the derived rank score (here the step count): a longer /
	// wider reachable flow is more critical.
	Criticality float64
}

// Postprocess bundles the derived materialized views a bootstrap `postprocess`
// step computes (§11.1 `postprocess` row: "one view per derived data shape").
// Every view is computed by READING BACK the persisted namespace, never from
// the input corpus — the same discipline SnapshotFromStore/ImpactRadiusFromStore
// follow, so a divergent write is reflected and parity is verified, not
// guaranteed by construction.
//
// The §11.6 parity oracle is per-view structural/ranking equivalence (O6
// refinement C / parity proposal §C — NOT the literal "bytes-equivalent" text,
// which can never pass against the bridge's LLM-derived community summaries):
//
//   - FlowMemberships — set equality over (flow_id, member_id, position) rows.
//   - Communities     — partition equivalence via graphstore.PartitionAgreement
//     (cluster ids may differ; summary fields are excluded per open-question 4).
//   - RiskIndex       — Spearman rank correlation ≥ graphstore.DefaultSpearmanTau
//     via graphstore.SpearmanTau.
//   - FTS             — set equality over the searchable token set.
type Postprocess struct {
	FlowMemberships []FlowMembership
	// Communities maps every persisted node id → its cluster id.
	Communities map[string]string
	// RiskIndex maps every persisted node id → its risk score.
	RiskIndex map[string]float64
	// FTS is the sorted, distinct searchable token set (qualified names).
	FTS []string
}

// FlowsFromStore reads the persisted namespace back and computes its execution
// flows over CALLS edges. An execution flow starts at an entry point (a symbol
// with ≥1 outgoing CALLS edge and zero incoming CALLS edges — a call-graph
// root) and its members are the symbols reached by a deterministic BFS over the
// CALLS graph (frontier expanded in sorted-id order), so positions are stable.
func FlowsFromStore(store StoreReader, ns string) ([]Flow, error) {
	notes, edges, err := readNamespace(store, ns)
	if err != nil {
		return nil, err
	}
	return flowsFromGraph(notes, edges), nil
}

// flowsFromGraph is the pure flow derivation over persisted notes/edges.
func flowsFromGraph(notes []sdk.Note, edges []sdk.Edge) []Flow {
	byID := notesByID(notes)
	calls := adjacencyByKind(edges, edgeCalls)
	sortAdjacency(calls)
	incoming := incomingByKind(edges, edgeCalls)

	entries := make([]string, 0)
	for id := range byID {
		if len(calls[id]) > 0 && incoming[id] == 0 {
			entries = append(entries, id)
		}
	}
	sort.Strings(entries)

	flows := make([]Flow, 0, len(entries))
	for _, entry := range entries {
		members := traverseCalls(entry, calls)
		flows = append(flows, Flow{
			ID:          entry,
			EntryPoint:  fieldString(byID[entry], fieldQualified),
			Members:     members,
			Criticality: float64(len(members)),
		})
	}
	return flows
}

// traverseCalls returns the members of a flow: a deterministic BFS over the
// CALLS graph from entry, in first-seen order (position == slice index). Cycles
// terminate because a node is enqueued at most once.
func traverseCalls(entry string, calls map[string][]string) []string {
	seen := map[string]bool{entry: true}
	order := []string{entry}
	queue := []string{entry}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, nb := range calls[n] {
			if !seen[nb] {
				seen[nb] = true
				order = append(order, nb)
				queue = append(queue, nb)
			}
		}
	}
	return order
}

// FlowMembershipsFromStore reads the namespace back and returns the flat
// flow_memberships rows (the §11.1 `flows` materialized-view shape) sorted by
// (flow_id, position). Set equality over these rows is the `flows` parity
// oracle.
func FlowMembershipsFromStore(store StoreReader, ns string) ([]FlowMembership, error) {
	flows, err := FlowsFromStore(store, ns)
	if err != nil {
		return nil, err
	}
	return flowMemberships(flows), nil
}

// flowMemberships flattens flows to sorted (flow_id, member_id, position) rows.
func flowMemberships(flows []Flow) []FlowMembership {
	var rows []FlowMembership
	for _, f := range flows {
		for pos, member := range f.Members {
			rows = append(rows, FlowMembership{FlowID: f.ID, MemberID: member, Position: pos})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].FlowID != rows[j].FlowID {
			return rows[i].FlowID < rows[j].FlowID
		}
		return rows[i].Position < rows[j].Position
	})
	return rows
}

// CommunitiesFromStore reads the namespace back and computes the community
// partition: the weakly-connected components of the structural symbol graph
// (CALLS ∪ IMPORTS edges — dependency relationships; TESTED_BY is a test-
// coverage relationship, not community membership). Every persisted node id is
// present in the result mapped to its cluster id (the smallest member id in its
// component, a stable relabel-invariant representative). The `communities`
// parity oracle is graphstore.PartitionAgreement over two such maps.
func CommunitiesFromStore(store StoreReader, ns string) (map[string]string, error) {
	notes, edges, err := readNamespace(store, ns)
	if err != nil {
		return nil, err
	}
	return communitiesFromGraph(notes, edges), nil
}

// communitiesFromGraph is the pure community partition over persisted graph
// data: undirected connected components over CALLS ∪ IMPORTS edges.
func communitiesFromGraph(notes []sdk.Note, edges []sdk.Edge) map[string]string {
	adj := map[string][]string{}
	for _, e := range edges {
		if e.Type != edgeCalls && e.Type != edgeImports {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	sortAdjacency(adj)

	ids := make([]string, 0, len(notes))
	for _, n := range notes {
		ids = append(ids, n.ID)
	}
	sort.Strings(ids)

	cluster := make(map[string]string, len(ids))
	for _, id := range ids {
		if _, done := cluster[id]; done {
			continue
		}
		component := collectComponent(id, adj)
		rep := component[0] // sorted first == smallest id
		for _, m := range component {
			cluster[m] = rep
		}
	}
	return cluster
}

// collectComponent returns the sorted node ids of the connected component
// containing start, over the undirected adjacency adj.
func collectComponent(start string, adj map[string][]string) []string {
	seen := map[string]bool{start: true}
	stack := []string{start}
	var members []string
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		members = append(members, n)
		for _, nb := range adj[n] {
			if !seen[nb] {
				seen[nb] = true
				stack = append(stack, nb)
			}
		}
	}
	sort.Strings(members)
	return members
}

// RiskIndexFromStore reads the namespace back and computes the risk_index
// derived table: a per-node degree-centrality score (in-degree + out-degree
// over ALL edge kinds — any relationship widens a symbol's blast radius). It is
// the rank-ordered derived table the §11.6 postprocess oracle compares via
// graphstore.SpearmanTau (≥ graphstore.DefaultSpearmanTau).
func RiskIndexFromStore(store StoreReader, ns string) (map[string]float64, error) {
	notes, edges, err := readNamespace(store, ns)
	if err != nil {
		return nil, err
	}
	return riskIndexFromGraph(notes, edges), nil
}

// riskIndexFromGraph is the pure degree-centrality risk score over persisted
// graph data. Every persisted node id is present (isolated nodes score 0).
func riskIndexFromGraph(notes []sdk.Note, edges []sdk.Edge) map[string]float64 {
	risk := make(map[string]float64, len(notes))
	for _, n := range notes {
		risk[n.ID] = 0
	}
	for _, e := range edges {
		if _, ok := risk[e.From]; ok {
			risk[e.From]++
		}
		if _, ok := risk[e.To]; ok {
			risk[e.To]++
		}
	}
	return risk
}

// FTSFromStore reads the namespace back and returns the full-text-search token
// set: the sorted, distinct qualified names of persisted symbols. The FTS
// derived computation's parity oracle is set equality over this token set.
func FTSFromStore(store StoreReader, ns string) ([]string, error) {
	notes, _, err := readNamespace(store, ns)
	if err != nil {
		return nil, err
	}
	return ftsTokens(notes), nil
}

// ftsTokens returns the sorted distinct qualified-name tokens of the notes.
func ftsTokens(notes []sdk.Note) []string {
	set := map[string]bool{}
	for _, n := range notes {
		if qn := fieldString(n, fieldQualified); qn != "" {
			set[qn] = true
		}
	}
	tokens := make([]string, 0, len(set))
	for tok := range set {
		tokens = append(tokens, tok)
	}
	sort.Strings(tokens)
	return tokens
}

// PostprocessFromStore reads the persisted namespace back once and computes all
// derived materialized views the bootstrap `postprocess` step produces. Every
// view is derived from storage readback, so a divergent write is detectable
// (verified, not guaranteed by construction).
func PostprocessFromStore(store StoreReader, ns string) (Postprocess, error) {
	notes, edges, err := readNamespace(store, ns)
	if err != nil {
		return Postprocess{}, err
	}
	return Postprocess{
		FlowMemberships: flowMemberships(flowsFromGraph(notes, edges)),
		Communities:     communitiesFromGraph(notes, edges),
		RiskIndex:       riskIndexFromGraph(notes, edges),
		FTS:             ftsTokens(notes),
	}, nil
}

// adjacencyByKind builds the directed From→To adjacency over edges of the given
// kind.
func adjacencyByKind(edges []sdk.Edge, kind string) map[string][]string {
	adj := map[string][]string{}
	for _, e := range edges {
		if e.Type == kind {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}
	return adj
}

// incomingByKind counts incoming edges per node id for edges of the given kind.
func incomingByKind(edges []sdk.Edge, kind string) map[string]int {
	in := map[string]int{}
	for _, e := range edges {
		if e.Type == kind {
			in[e.To]++
		}
	}
	return in
}

// sortAdjacency sorts each adjacency list so traversals are deterministic.
func sortAdjacency(adj map[string][]string) {
	for id := range adj {
		sort.Strings(adj[id])
	}
}

// CompareFlowMemberships is the `flows`-row parity oracle (O6 refinement C /
// parity proposal §C): two implementations agree iff their flow_memberships
// row SETS are equal. It returns a report whose Detail lists the symmetric
// difference on failure — the same shape as graphstore.CompareUpserts, kept in
// this package because flow_memberships is a CRG-adapter-owned shape.
func CompareFlowMemberships(a, b []FlowMembership) graphstore.ParityReport {
	rep := graphstore.ParityReport{Row: "flows", Pass: true}
	setA := membershipSet(a)
	setB := membershipSet(b)
	for k := range setA {
		if !setB[k] {
			failReport(&rep, "flow_membership only in a: "+k)
		}
	}
	for k := range setB {
		if !setA[k] {
			failReport(&rep, "flow_membership only in b: "+k)
		}
	}
	return rep
}

// failReport marks a graphstore.ParityReport failed with a diagnostic reason.
// graphstore's own (*ParityReport).fail is unexported, so this package sets the
// exported Pass/Detail fields directly to build reports for the CRG-owned
// derived-view oracles (flow_memberships, FTS).
func failReport(rep *graphstore.ParityReport, reason string) {
	rep.Pass = false
	rep.Detail = append(rep.Detail, reason)
}

// membershipSet keys a flow_memberships slice by (flow_id, position, member_id).
func membershipSet(rows []FlowMembership) map[string]bool {
	set := make(map[string]bool, len(rows))
	for _, r := range rows {
		set[membershipKey(r)] = true
	}
	return set
}

// membershipKey is the set-membership key for a flow_membership row.
func membershipKey(r FlowMembership) string {
	return r.FlowID + "\x00" + strconv.Itoa(r.Position) + "\x00" + r.MemberID
}

// CompareFTS is the FTS derived-computation parity oracle: set equality over
// the searchable token sets. Inputs are the sorted token slices FTSFromStore
// returns.
func CompareFTS(a, b []string) graphstore.ParityReport {
	rep := graphstore.ParityReport{Row: "postprocess.fts", Pass: true}
	setA := stringSet(a)
	setB := stringSet(b)
	for tok := range setA {
		if !setB[tok] {
			failReport(&rep, "fts token only in a: "+strconv.Quote(tok))
		}
	}
	for tok := range setB {
		if !setA[tok] {
			failReport(&rep, "fts token only in b: "+strconv.Quote(tok))
		}
	}
	return rep
}

// stringSet builds a set from a string slice.
func stringSet(xs []string) map[string]bool {
	set := make(map[string]bool, len(xs))
	for _, x := range xs {
		set[x] = true
	}
	return set
}
