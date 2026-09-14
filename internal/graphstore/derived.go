package graphstore

import "errors"

// Derived-view storage — upstream code-review-graph v2.3.8's schema-v9
// derived tables, as a segregated Store role.
//
// The code graph has two layers. The EXTRACTED layer (nodes, edges) is
// written per file by CodeGraphWriter as the scanner parses source. The
// DERIVED layer below is recomputed wholesale from the extracted layer by
// the postprocess lifecycle: execution flows and their memberships,
// communities, the three pre-computed summary tables
// (community_summaries, flow_snapshots, risk_index) and the nodes_fts
// full-text index.
//
// Two properties of that layer shape this role and are load-bearing:
//
//  1. Derived rows are REPLACED, never merged. Upstream's persistence
//     functions (flows.store_flows, communities.store_communities,
//     tools/build._compute_summaries) each open one explicit
//     BEGIN IMMEDIATE, DELETE the whole table, INSERT the new row set and
//     commit — so a crash mid-write leaves the previous generation intact
//     rather than a half-recomputed table. Every Replace* method below is
//     that transaction, which is why there is no per-row Insert/Delete on
//     this role: a caller must not be able to construct a partial
//     generation.
//  2. Flows/communities and the summary tables are refreshed by DIFFERENT
//     lifecycle steps. A standalone postprocess recomputes flows,
//     communities and FTS but NOT the summary tables (upstream's
//     run_postprocess never calls _compute_summaries); only a build or
//     update at postprocess="full" refreshes all six. The three
//     ReplaceCommunitySummaries / ReplaceFlowSnapshots / ReplaceRiskIndex
//     writers are therefore separate entry points, deliberately not
//     bundled behind one "refresh derived views" call that would erase the
//     distinction.
//
// Row ids: flows and communities use AUTOINCREMENT primary keys, so
// ReplaceFlows / ReplaceCommunities assign ids and the caller reads them
// back (ReadFlows / ReadCommunities) to build the dependent summary rows.
// Upstream behaves identically, including the consequence that a DELETE
// does not reset the sequence: the second generation of flows in a
// database starts at id N+1, not 1.

// FlowRow is one row of the `flows` table: an execution flow traced
// forward from an entry point over CALLS edges.
type FlowRow struct {
	// ID is the AUTOINCREMENT primary key. It is ignored by ReplaceFlows
	// (SQLite assigns it) and populated by ReadFlows.
	ID int64
	// Name is the entry point's symbol name.
	Name string
	// EntryPointID is the node id the flow was traced from.
	EntryPointID int64
	// Depth is the maximum BFS depth reached from the entry point.
	Depth int
	// NodeCount is len(path) — the number of distinct nodes in the flow.
	NodeCount int
	// FileCount is the number of distinct files the flow's nodes span.
	FileCount int
	// Criticality is the weighted 0..1 risk score, rounded to 4 decimals.
	Criticality float64
	// PathJSON is the JSON array of node ids in first-seen BFS order.
	PathJSON string
	// CreatedAt / UpdatedAt are the datetime('now') stamps the column
	// defaults apply on insert. ReplaceFlows never sets them (upstream
	// does not either), so a replaced generation always carries the
	// insert time of the pass that wrote it; ReadFlows populates them
	// because the flow query tools echo both keys.
	CreatedAt string
	UpdatedAt string
}

// FlowMembershipRow is one row of the `flow_memberships` table: node
// membership in a flow at a given path position (0 is the entry point).
// It is never written directly — ReplaceFlows derives it from the paths it
// is given, so memberships cannot drift from flows.path_json.
type FlowMembershipRow struct {
	FlowID   int64
	NodeID   int64
	Position int
}

// CommunityRow is one row of the `communities` table: a cluster of related
// code nodes.
type CommunityRow struct {
	// ID is the AUTOINCREMENT primary key, assigned by ReplaceCommunities.
	ID int64
	// Name is the generated community name (e.g. "auth-token").
	Name string
	// Level is the hierarchy level; 0 for a flat partition.
	Level int
	// ParentID is the enclosing community, or nil at the top level. The
	// pointer models the nullable column: a 0 parent id and "no parent"
	// are different states.
	ParentID *int64
	// Cohesion is internal/(internal+external) edge share, 4 decimals.
	Cohesion float64
	// Size is the member count.
	Size int
	// DominantLanguage is the most common member language.
	DominantLanguage string
	// Description is the detector's provenance string.
	Description string
}

// CommunitySummaryRow is one row of the `community_summaries` table.
type CommunitySummaryRow struct {
	CommunityID int64
	Name        string
	// Purpose is derived from the members' common file-path prefix.
	Purpose string
	// KeySymbols is a JSON array of the top member names by edge degree,
	// stored as text exactly as upstream writes it.
	KeySymbols string
	// Risk is upstream's placeholder risk band; the summary computation
	// never sets it, so it reads back as the column default "unknown".
	Risk             string
	Size             int
	DominantLanguage string
}

// FlowSnapshotRow is one row of the `flow_snapshots` table: a flattened,
// token-cheap projection of a flow for review tooling.
type FlowSnapshotRow struct {
	FlowID int64
	Name   string
	// EntryPoint is the entry node's QUALIFIED name (v2.3.8; v2.2.0 used
	// the bare name here).
	EntryPoint string
	// CriticalPath is a JSON array of qualified names: the entry point,
	// then path[1:4], then the final node when not already present.
	CriticalPath string
	Criticality  float64
	NodeCount    int
	FileCount    int
}

// RiskIndexRow is one row of the `risk_index` table: a per-symbol review
// risk score.
type RiskIndexRow struct {
	NodeID        int64
	QualifiedName string
	RiskScore     float64
	// CallerCount is the number of CALLS edges targeting this symbol.
	CallerCount int
	// TestCoverage is "tested" or "untested".
	TestCoverage string
	// SecurityRelevant is set when the symbol NAME contains one of
	// upstream's security keywords.
	SecurityRelevant bool
	// LastComputed is the datetime('now') stamp of the computing pass.
	LastComputed string
}

// EdgeRewrite is one edge whose endpoints and/or extra metadata a resolution
// pass has recomputed.
//
// Both endpoints are carried even though a pass only ever moves one of them:
// the writer is a plain UPDATE of both columns plus extra, so the caller
// supplies the endpoint it is NOT changing at its current value. That keeps
// the writer free of "which column am I updating" branching, and makes a
// rewrite self-describing — the row it names is exactly the row it wants.
type EdgeRewrite struct {
	EdgeID          int64
	SourceQualified string
	TargetQualified string
	Extra           map[string]any
}

// ErrFTSUnsupported is returned by RebuildFTS / SearchNodesFTS on a
// backend with no SQLite FTS5 module. It is an explicit capability error,
// not a silent empty result: a caller that needs full-text search must be
// able to tell "no matches" from "this backend cannot do that".
var ErrFTSUnsupported = errors.New("graphstore: full-text search (FTS5) is not supported by this backend")

// CodeGraphDerived is the derived-view slice of the code graph: read the
// six derived tables, replace them atomically, and maintain the two
// node columns and the FTS index the derivation depends on.
//
// Only the postprocess lifecycle (internal/codegraph.Engine) and the
// derived-view query tools depend on this role. The scanner writes through
// CodeGraphWriter and never touches it.
type CodeGraphDerived interface {
	// ReadFlows returns the `flows` rows in primary-key order.
	ReadFlows() ([]FlowRow, error)
	// ReadFlowMemberships returns the `flow_memberships` rows ordered by
	// (flow_id, node_id) — the table's primary key.
	ReadFlowMemberships() ([]FlowMembershipRow, error)
	// ReadCommunities returns the `communities` rows in primary-key order.
	ReadCommunities() ([]CommunityRow, error)
	// ReadCommunitySummaries returns the `community_summaries` rows in
	// primary-key order.
	ReadCommunitySummaries() ([]CommunitySummaryRow, error)
	// ReadFlowSnapshots returns the `flow_snapshots` rows in primary-key
	// order.
	ReadFlowSnapshots() ([]FlowSnapshotRow, error)
	// ReadRiskIndex returns the `risk_index` rows in primary-key order.
	ReadRiskIndex() ([]RiskIndexRow, error)

	// SearchNodesFTS runs an FTS5 MATCH against nodes_fts and returns the
	// matching node ids best-first by BM25 rank. The query is quoted, so
	// FTS5 operators in it are matched literally rather than interpreted.
	// Returns ErrFTSUnsupported on a backend without FTS5.
	SearchNodesFTS(query string, limit int) ([]int64, error)

	// SearchNodesFTSWords runs upstream GraphStore.search_nodes's FTS
	// phase (graph.py): the MATCH expression is the AND of one quoted
	// phrase per word, and there is deliberately NO rank ordering, so
	// FTS5 yields the first limit matches in ascending node id. The
	// bound is applied BEFORE any ordering, which is observable once the
	// match count exceeds the limit — that is why this is not derivable
	// from SearchNodesFTS's rank-ordered result. No words yields no
	// rows. Returns ErrFTSUnsupported on a backend without FTS5.
	SearchNodesFTSWords(words []string, limit int) ([]int64, error)
	// CountNodesFTSWords counts the same match set, unbounded. No words
	// yields 0. Returns ErrFTSUnsupported on a backend without FTS5.
	CountNodesFTSWords(words []string) (int, error)

	// ReplaceFlows atomically replaces `flows` and `flow_memberships`.
	// paths[i] is the node-id path of flows[i]; it drives both the
	// derived membership rows and (as JSON) flows.path_json, so a caller
	// cannot desynchronise the two. Returns the number of flows written.
	ReplaceFlows(flows []FlowRow, paths [][]int64) (int, error)
	// ReplaceCommunities atomically replaces `communities` and
	// re-assigns nodes.community_id: every node is reset to NULL, then the
	// nodes whose qualified name appears in members[i] are pointed at
	// communities[i]. Returns the number of communities written.
	ReplaceCommunities(communities []CommunityRow, members [][]string) (int, error)
	// ReplaceCommunitySummaries atomically replaces `community_summaries`.
	ReplaceCommunitySummaries(rows []CommunitySummaryRow) (int, error)
	// ReplaceFlowSnapshots atomically replaces `flow_snapshots`.
	ReplaceFlowSnapshots(rows []FlowSnapshotRow) (int, error)
	// ReplaceRiskIndex atomically replaces `risk_index`.
	ReplaceRiskIndex(rows []RiskIndexRow) (int, error)

	// ApplyEdgeRewrites atomically writes a set of recomputed edge
	// endpoints and extra metadata, returning the number of rows written.
	// It is the persistence half of bare-endpoint resolution: the pass
	// computes rewrites from the whole edge set, so they must land as one
	// generation or not at all.
	ApplyEdgeRewrites(rewrites []EdgeRewrite) (int, error)

	// RebuildFTS drops and re-creates nodes_fts, then repopulates it from
	// the nodes table, returning the indexed row count. Returns
	// ErrFTSUnsupported on a backend without FTS5.
	RebuildFTS() (int, error)

	// SetNodeSignature writes the rendered signature of one node.
	SetNodeSignature(id int64, signature string) error
	// SetNodeCommunity points one node at a community.
	SetNodeCommunity(id, communityID int64) error
	// NodesWithoutSignature returns the nodes whose signature is still
	// NULL — the backfill worklist.
	NodesWithoutSignature() ([]GraphNode, error)
	// ReadNodesByKind returns the nodes of any of the given kinds,
	// ordered by (kind, id). An empty kinds slice returns no rows.
	ReadNodesByKind(kinds []string) ([]GraphNode, error)
	// ReadNodesByCommunity returns the member nodes of one community in
	// primary-key order. A community with no members yields no rows.
	ReadNodesByCommunity(communityID int64) ([]GraphNode, error)
	// ReadNodesByID resolves node ids in batched IN queries, returning
	// the rows in ASCENDING ID order — not the caller's input order — and
	// silently skipping ids with no row. No ids yields no rows.
	ReadNodesByID(ids []int64) ([]GraphNode, error)
	// ReadAllNodes returns every node in primary-key order.
	ReadAllNodes() ([]GraphNode, error)
	// ReadAllEdges returns every edge in primary-key order.
	ReadAllEdges() ([]GraphEdge, error)
}
