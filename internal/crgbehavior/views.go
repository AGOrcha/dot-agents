package crgbehavior

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"

	// _ "modernc.org/sqlite": side-effect registers the SQLite driver used to
	// read the legacy bridge's own graph.db (read-only).
	_ "modernc.org/sqlite"
)

// ErrBridgeUnavailable reports that the pinned legacy bridge cannot be driven
// on this machine (no CLI, or no graph.db to read).
//
// It is NOT a green path. Criterion 2 is a dual-read comparison: with one side
// absent there is no evidence, and a gate that reports success without evidence
// is worse than no gate. Callers surface it as an explicit inconclusive verdict
// with a non-zero exit, so "the bridge was missing" can never be mistaken for
// "behavior was preserved".
var ErrBridgeUnavailable = errors.New("crgbehavior: pinned CRG bridge unavailable")

// readOnlyPragma opens the bridge's SQLite store in query-only mode. The gate
// only ever READS legacy state — it must never mutate the graph it compares.
const readOnlyPragma = "?_pragma=query_only(true)"

// sqliteDriver is the database/sql driver the legacy bridge's store speaks.
const sqliteDriver = "sqlite"

// edgeKindImportsFrom is the release's spelling of the import edge; the
// kg-native adapter's schema spells it IMPORTS. The mapping applies only to the
// NATIVE INGESTION corpus — BridgeViews.Edges keeps the upstream spelling, so
// the edge-confidence surface compares what the release actually stored.
const edgeKindImportsFrom = "IMPORTS_FROM"

// unassignedCluster is the canonical cluster key for a node the release's
// community detection left unassigned. It is a first-class value rather than a
// synthesized singleton: inventing a cluster per unassigned node would make a
// partition comparison pass by construction.
const unassignedCluster = ""

// BridgeEdge is one upstream `edges` row including the schema-v9 confidence
// columns. Endpoints are normalized to the comparison id space; Kind keeps the
// release's own spelling.
type BridgeEdge struct {
	Kind           string  `json:"kind"`
	From           string  `json:"from"`
	To             string  `json:"to"`
	FilePath       string  `json:"file_path"`
	Line           int     `json:"line"`
	Confidence     float64 `json:"confidence"`
	ConfidenceTier string  `json:"confidence_tier"`
}

// BridgeFlow is one upstream `flows` row: the release's own flow identity,
// ordered path, and derived criticality.
//
// The upstream `flows.id` is an AUTOINCREMENT rowid — the one genuinely
// nondeterministic field in the row — so identity is re-keyed onto the flow's
// entry-point symbol, which is stable across builds. Everything else (ordered
// path, depth, node/file counts, criticality) is deterministic upstream output
// and is compared EXACTLY.
type BridgeFlow struct {
	// Name is the release's flow name (the sanitized entry-point name).
	Name string `json:"name"`
	// EntryPoint is the entry-point symbol in the comparison id space; it is
	// the flow's stable identity.
	EntryPoint string `json:"entry_point"`
	// Path is the ordered member list decoded from `path_json`, mapped into
	// the comparison id space. Position in this slice IS the upstream
	// flow_memberships position.
	Path []string `json:"path"`
	// Depth is the maximum BFS depth the release reached (its bounded
	// traversal, capped at 15 hops).
	Depth int `json:"depth"`
	// NodeCount and FileCount are the release's own persisted counts.
	NodeCount int `json:"node_count"`
	FileCount int `json:"file_count"`
	// Criticality is the release's weighted 0..1 score.
	Criticality float64 `json:"criticality"`
}

// BridgeFlowSnapshot is one upstream `flow_snapshots` row. v2.3.8 spells the
// critical path's intermediate and last entries with QUALIFIED names (2.2.0
// used bare names), so the snapshot is a release-sensitive surface worth
// comparing in its own right.
type BridgeFlowSnapshot struct {
	// EntryPoint is the snapshot's flow identity, re-keyed off the
	// autoincrement flow_id onto the entry-point symbol.
	EntryPoint   string   `json:"entry_point"`
	Name         string   `json:"name"`
	CriticalPath []string `json:"critical_path"`
	Criticality  float64  `json:"criticality"`
	NodeCount    int      `json:"node_count"`
	FileCount    int      `json:"file_count"`
}

// BridgeCommunitySummary is one upstream `community_summaries` row, re-keyed
// off the autoincrement community id onto the cluster's canonical key.
type BridgeCommunitySummary struct {
	Cluster          string   `json:"cluster"`
	Name             string   `json:"name"`
	Purpose          string   `json:"purpose"`
	KeySymbols       []string `json:"key_symbols"`
	Risk             string   `json:"risk"`
	Size             int      `json:"size"`
	DominantLanguage string   `json:"dominant_language"`
}

// BridgeRisk is one upstream `risk_index` row. The release computes a bounded
// score from caller thresholds, TESTED_BY coverage and security-name keywords —
// a deterministic function of the graph, so every field is compared exactly.
// `last_computed` is a wall-clock timestamp and is deliberately not read.
type BridgeRisk struct {
	QualifiedName    string  `json:"qualified_name"`
	RiskScore        float64 `json:"risk_score"`
	CallerCount      int     `json:"caller_count"`
	TestCoverage     string  `json:"test_coverage"`
	SecurityRelevant bool    `json:"security_relevant"`
}

// BridgeViews is everything the pinned release persisted for one repository at
// one commit, normalized into the comparison id space. Every field is what the
// PYTHON side actually wrote; the gate compares these against the kg-native
// adapter's derivations of the same views.
type BridgeViews struct {
	// Release and Schema record which bridge produced this state.
	Release ReleaseInfo  `json:"release"`
	Schema  SchemaReport `json:"schema"`
	// Symbols and Edges are the release's graph.
	Symbols []crg.Symbol `json:"-"`
	Edges   []BridgeEdge `json:"-"`
	// Flows are the release's persisted `flows` rows, sorted by entry point.
	Flows []BridgeFlow `json:"flows"`
	// FlowSnapshots are the release's persisted `flow_snapshots` rows.
	FlowSnapshots []BridgeFlowSnapshot `json:"flow_snapshots"`
	// Communities maps a symbol id to its canonical cluster key (the smallest
	// member id of its community), or unassignedCluster.
	Communities map[string]string `json:"communities"`
	// CommunitySummaries are the release's persisted `community_summaries`.
	CommunitySummaries []BridgeCommunitySummary `json:"community_summaries"`
	// RiskIndex maps a symbol id to its persisted risk row.
	RiskIndex map[string]BridgeRisk `json:"risk_index"`
	// FTSIndex is the sorted, distinct content of the release's `nodes_fts`
	// index — the qualified names a search can return at all.
	FTSIndex []string `json:"fts_index"`
	// FilesIndexed counts distinct file paths in the graph.
	FilesIndexed int `json:"files_indexed"`
	// CommunitiesAssigned counts nodes the release placed in a community.
	CommunitiesAssigned int `json:"communities_assigned"`
}

// References lowers the release's edges to the kg-native ingestion shape,
// mapping the IMPORTS_FROM spelling onto IMPORTS.
func (v BridgeViews) References() []crg.Reference {
	out := make([]crg.Reference, 0, len(v.Edges))
	for _, e := range v.Edges {
		kind := e.Kind
		if kind == edgeKindImportsFrom {
			kind = "IMPORTS"
		}
		out = append(out, crg.Reference{Kind: kind, From: e.From, To: e.To})
	}
	return out
}

// Corpus lowers the release's graph to the kg-native ingestion corpus.
func (v BridgeViews) Corpus(commit string) crg.Corpus {
	return crg.Corpus{Commit: commit, Symbols: v.Symbols, References: v.References()}
}

// FlowMemberships flattens the release's flows into (flow_id, member, position)
// rows keyed by entry point — the row shape the flow oracles compare.
func (v BridgeViews) FlowMemberships() []crg.FlowMembership {
	var out []crg.FlowMembership
	for _, f := range v.Flows {
		for i, member := range f.Path {
			out = append(out, crg.FlowMembership{FlowID: f.EntryPoint, MemberID: member, Position: i})
		}
	}
	sortMemberships(out)
	return out
}

// sortMemberships orders rows deterministically for comparison and reporting.
func sortMemberships(rows []crg.FlowMembership) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].FlowID != rows[j].FlowID {
			return rows[i].FlowID < rows[j].FlowID
		}
		if rows[i].Position != rows[j].Position {
			return rows[i].Position < rows[j].Position
		}
		return rows[i].MemberID < rows[j].MemberID
	})
}

// bridgeNode is one legacy node row after normalization.
type bridgeNode struct {
	id            int64
	name          string
	qualifiedName string
	filePath      string
	kind          string
	language      string
	lineStart     int
	fileHash      string
	communityID   sql.NullInt64
	nativeID      string
}

// BridgeStore is an open, read-only handle on one pinned-release graph.db. It
// keeps the connection so the gate can run the release's OWN FTS5 search rather
// than approximate it from a token dump.
type BridgeStore struct {
	db     *sql.DB
	norm   Normalizer
	schema SchemaReport
	info   ReleaseInfo
}

// OpenBridgeStore opens a pinned-release graph read-only, probes its schema
// capabilities, and returns a handle. version is the bridge CLI's own
// `--version` answer; it is checked against the pinned release before any view
// is read, so an off-release run fails on the release check rather than
// producing a meaningless diff.
func OpenBridgeStore(repoRoot, dbPath, version string, rel Release) (*BridgeStore, error) {
	if err := rel.CheckVersion(version); err != nil {
		return nil, err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("%w: no graph at %s", ErrBridgeUnavailable, dbPath)
	}
	db, err := sql.Open(sqliteDriver, dbPath+readOnlyPragma)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: open bridge graph: %w", err)
	}
	schema, err := ProbeSchema(db, rel)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &BridgeStore{
		db:     db,
		norm:   NewNormalizer(repoRoot),
		schema: schema,
		info:   ReleaseInfo{Package: PackageName, Version: version, SchemaVersion: schema.SchemaVersion},
	}, nil
}

// Normalizer exposes the id-space normalizer the store was opened with, so a
// live query answer is keyed exactly like the persisted views.
func (s *BridgeStore) Normalizer() Normalizer { return s.norm }

// FlowState samples the summary-table id spaces for the lifecycle probe.
func (s *BridgeStore) FlowState() (FlowState, error) { return ReadFlowState(s.db) }

// Close releases the read-only handle.
func (s *BridgeStore) Close() error { return s.db.Close() }

// Schema returns the capability probe taken at open time.
func (s *BridgeStore) Schema() SchemaReport { return s.schema }

// Release returns the observed release facts.
func (s *BridgeStore) Release() ReleaseInfo { return s.info }

// Views reads every persisted view the probe found usable. A table the probe
// classified as missing or column-incomplete is NOT read: its surfaces are
// already accounted for as uncomputed or as a schema failure.
func (s *BridgeStore) Views() (BridgeViews, error) {
	nodes, err := s.readNodes()
	if err != nil {
		return BridgeViews{}, err
	}
	if len(nodes) == 0 {
		return BridgeViews{}, fmt.Errorf("%w: bridge graph has no nodes", ErrBridgeUnavailable)
	}
	views := s.viewsFromNodes(nodes)
	views.Release, views.Schema = s.info, s.schema
	if views.Edges, err = s.readEdges(); err != nil {
		return BridgeViews{}, err
	}
	byID := nativeIDsByNodeID(nodes)
	if err := s.readDerived(byID, &views); err != nil {
		return BridgeViews{}, err
	}
	return views, nil
}

// readDerived fills the release's derived views, skipping only the tables the
// capability probe already reported as unusable.
func (s *BridgeStore) readDerived(byID map[int64]string, views *BridgeViews) error {
	flowByID, err := s.readFlows(byID)
	if err != nil {
		return err
	}
	for _, f := range flowByID {
		views.Flows = append(views.Flows, f)
	}
	sort.Slice(views.Flows, func(i, j int) bool { return views.Flows[i].EntryPoint < views.Flows[j].EntryPoint })
	if views.FlowSnapshots, err = s.readFlowSnapshots(flowByID); err != nil {
		return err
	}
	if views.CommunitySummaries, err = s.readCommunitySummaries(views.Communities, byID); err != nil {
		return err
	}
	if views.RiskIndex, err = s.readRiskIndex(byID); err != nil {
		return err
	}
	views.FTSIndex, err = s.readFTSIndex()
	return err
}

// readFTSIndex reads the content of the release's `nodes_fts` index — what a
// search can return at all, as distinct from what one search does return.
func (s *BridgeStore) readFTSIndex() ([]string, error) {
	if !s.usable(tableNodesFTS) {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT qualified_name FROM nodes_fts`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge nodes_fts: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var qualified string
		if err := rows.Scan(&qualified); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge nodes_fts row: %w", err)
		}
		normalized, err := s.norm.Qualified(qualified)
		if err != nil {
			return nil, err
		}
		seen[normalized] = true
	}
	if err := rowsErr(rows, tableNodesFTS); err != nil {
		return nil, err
	}
	return sortedSet(seen), nil
}

// usable reports whether a table may be read at all.
func (s *BridgeStore) usable(table string) bool {
	c, ok := s.schema.Of(table)
	return ok && c.Usable()
}

// readNodes reads the release's nodes, normalizing paths and qualified names
// with clean-prefix semantics.
func (s *BridgeStore) readNodes() ([]bridgeNode, error) {
	rows, err := s.db.Query(`SELECT id,name,qualified_name,file_path,kind,
	                                COALESCE(language,''),COALESCE(line_start,0),
	                                COALESCE(file_hash,''),community_id FROM nodes`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge nodes: %w", err)
	}
	defer rows.Close()
	var out []bridgeNode
	for rows.Next() {
		var n bridgeNode
		if err := rows.Scan(&n.id, &n.name, &n.qualifiedName, &n.filePath, &n.kind,
			&n.language, &n.lineStart, &n.fileHash, &n.communityID); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge node: %w", err)
		}
		if n.qualifiedName, err = s.norm.Qualified(n.qualifiedName); err != nil {
			return nil, err
		}
		if n.filePath, err = s.norm.Path(n.filePath); err != nil {
			return nil, err
		}
		n.nativeID = crg.SymbolID(crg.Symbol{QualifiedName: n.qualifiedName, FilePath: n.filePath})
		out = append(out, n)
	}
	return out, rowsErr(rows, tableNodes)
}

// viewsFromNodes builds the symbol corpus and the canonical community partition
// from the normalized node rows.
func (s *BridgeStore) viewsFromNodes(nodes []bridgeNode) BridgeViews {
	symbols := make([]crg.Symbol, 0, len(nodes))
	files := map[string]bool{}
	members := map[int64][]string{}
	assigned := 0
	for _, n := range nodes {
		symbols = append(symbols, crg.Symbol{
			QualifiedName: n.qualifiedName,
			Kind:          n.kind,
			Language:      n.language,
			FilePath:      n.filePath,
			LineStart:     n.lineStart,
			ContentHash:   n.fileHash,
		})
		files[n.filePath] = true
		if n.communityID.Valid {
			assigned++
			members[n.communityID.Int64] = append(members[n.communityID.Int64], n.nativeID)
		}
	}
	return BridgeViews{
		Symbols:             symbols,
		Communities:         canonicalClusters(nodes, members),
		FilesIndexed:        len(files),
		CommunitiesAssigned: assigned,
	}
}

// canonicalClusters re-keys the release's autoincrement community ids onto a
// relabel-invariant key — the smallest member id of the cluster — so two
// implementations' partitions can be compared EXACTLY instead of through a
// similarity score. A node the release left unassigned keeps unassignedCluster
// rather than becoming a synthetic singleton.
func canonicalClusters(nodes []bridgeNode, members map[int64][]string) map[string]string {
	key := make(map[int64]string, len(members))
	for id, ids := range members {
		smallest := ids[0]
		for _, candidate := range ids[1:] {
			if candidate < smallest {
				smallest = candidate
			}
		}
		key[id] = smallest
	}
	out := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.communityID.Valid {
			out[n.nativeID] = key[n.communityID.Int64]
			continue
		}
		out[n.nativeID] = unassignedCluster
	}
	return out
}

// nativeIDsByNodeID maps each release node id to its comparison id.
func nativeIDsByNodeID(nodes []bridgeNode) map[int64]string {
	out := make(map[int64]string, len(nodes))
	for _, n := range nodes {
		out[n.id] = n.nativeID
	}
	return out
}

// readEdges reads the release's edges including the schema-v9 confidence
// columns, preserving the upstream edge-kind spelling.
func (s *BridgeStore) readEdges() ([]BridgeEdge, error) {
	rows, err := s.db.Query(`SELECT kind,source_qualified,target_qualified,
	                                COALESCE(file_path,''),COALESCE(line,0),
	                                COALESCE(confidence,0),COALESCE(confidence_tier,'') FROM edges`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge edges: %w", err)
	}
	defer rows.Close()
	var out []BridgeEdge
	for rows.Next() {
		var e BridgeEdge
		if err := rows.Scan(&e.Kind, &e.From, &e.To, &e.FilePath, &e.Line,
			&e.Confidence, &e.ConfidenceTier); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge edge: %w", err)
		}
		if e.From, err = s.norm.Qualified(e.From); err != nil {
			return nil, err
		}
		if e.To, err = s.norm.Qualified(e.To); err != nil {
			return nil, err
		}
		if e.FilePath != "" {
			if e.FilePath, err = s.norm.Path(e.FilePath); err != nil {
				return nil, err
			}
		}
		out = append(out, e)
	}
	return out, rowsErr(rows, tableEdges)
}

// readFlows reads the release's `flows` rows, decoding `path_json` into the
// ordered member list and re-keying identity onto the entry-point symbol.
func (s *BridgeStore) readFlows(byID map[int64]string) (map[int64]BridgeFlow, error) {
	out := map[int64]BridgeFlow{}
	if !s.usable(tableFlows) {
		return out, nil
	}
	rows, err := s.db.Query(`SELECT id,name,entry_point_id,depth,node_count,file_count,
	                                criticality,path_json FROM flows`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge flows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, entryID int64
		var f BridgeFlow
		var pathJSON string
		if err := rows.Scan(&id, &f.Name, &entryID, &f.Depth, &f.NodeCount,
			&f.FileCount, &f.Criticality, &pathJSON); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge flow: %w", err)
		}
		entry, ok := byID[entryID]
		if !ok {
			return nil, fmt.Errorf("crgbehavior: bridge flow %d names entry node %d that the nodes table does not hold",
				id, entryID)
		}
		f.EntryPoint = entry
		if f.Path, err = decodePath(id, pathJSON, byID); err != nil {
			return nil, err
		}
		out[id] = f
	}
	return out, rowsErr(rows, tableFlows)
}

// decodePath maps the release's ordered `path_json` node ids into the
// comparison id space, preserving order (position IS the flow_memberships
// position upstream writes).
func decodePath(flowID int64, pathJSON string, byID map[int64]string) ([]string, error) {
	if strings.TrimSpace(pathJSON) == "" {
		return nil, nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(pathJSON), &ids); err != nil {
		return nil, fmt.Errorf("crgbehavior: decode bridge flow %d path_json: %w", flowID, err)
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		member, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("crgbehavior: bridge flow %d path names node %d that the nodes table does not hold",
				flowID, id)
		}
		out = append(out, member)
	}
	return out, nil
}

// readFlowSnapshots reads the release's `flow_snapshots` rows, re-keyed off the
// autoincrement flow id onto the entry-point symbol.
func (s *BridgeStore) readFlowSnapshots(flowByID map[int64]BridgeFlow) ([]BridgeFlowSnapshot, error) {
	if !s.usable(tableFlowSnapshots) {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT flow_id,name,entry_point,critical_path,criticality,
	                                node_count,file_count FROM flow_snapshots`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge flow_snapshots: %w", err)
	}
	defer rows.Close()
	var out []BridgeFlowSnapshot
	for rows.Next() {
		var flowID int64
		var entryPoint, criticalPath string
		var snap BridgeFlowSnapshot
		if err := rows.Scan(&flowID, &snap.Name, &entryPoint, &criticalPath,
			&snap.Criticality, &snap.NodeCount, &snap.FileCount); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge flow_snapshot: %w", err)
		}
		flow, ok := flowByID[flowID]
		if !ok {
			return nil, fmt.Errorf("crgbehavior: bridge flow_snapshot names flow %d that the flows table does not hold",
				flowID)
		}
		snap.EntryPoint = flow.EntryPoint
		if snap.CriticalPath, err = s.decodeCriticalPath(criticalPath); err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EntryPoint < out[j].EntryPoint })
	return out, rowsErr(rows, tableFlowSnapshots)
}

// decodeCriticalPath decodes a snapshot's JSON critical path. v2.3.8 writes
// QUALIFIED names for the intermediate and last items, so each entry is
// normalized as a qualified name rather than assumed bare.
func (s *BridgeStore) decodeCriticalPath(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return nil, fmt.Errorf("crgbehavior: decode bridge flow_snapshot critical_path: %w", err)
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		normalized, err := s.norm.Qualified(name)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized)
	}
	return out, nil
}

// readCommunitySummaries reads the release's `community_summaries`, joined to
// `communities` and re-keyed onto the canonical cluster key.
func (s *BridgeStore) readCommunitySummaries(clusters map[string]string,
	byID map[int64]string) ([]BridgeCommunitySummary, error) {
	if !s.usable(tableCommunitySummaries) {
		return nil, nil
	}
	keyByCommunity, err := s.clusterKeys(clusters, byID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT community_id,name,COALESCE(purpose,''),COALESCE(key_symbols,'[]'),
	                                COALESCE(risk,''),COALESCE(size,0),COALESCE(dominant_language,'')
	                         FROM community_summaries`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge community_summaries: %w", err)
	}
	defer rows.Close()
	var out []BridgeCommunitySummary
	for rows.Next() {
		var communityID int64
		var keySymbols string
		var summary BridgeCommunitySummary
		if err := rows.Scan(&communityID, &summary.Name, &summary.Purpose, &keySymbols,
			&summary.Risk, &summary.Size, &summary.DominantLanguage); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge community_summary: %w", err)
		}
		if err := json.Unmarshal([]byte(keySymbols), &summary.KeySymbols); err != nil {
			return nil, fmt.Errorf("crgbehavior: decode bridge community_summary key_symbols: %w", err)
		}
		summary.Cluster = keyByCommunity[communityID]
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cluster < out[j].Cluster })
	return out, rowsErr(rows, tableCommunitySummaries)
}

// clusterKeys maps each release community id onto the canonical cluster key the
// partition uses, by reading the community assignment back off `nodes`.
func (s *BridgeStore) clusterKeys(clusters map[string]string, byID map[int64]string) (map[int64]string, error) {
	rows, err := s.db.Query(`SELECT id,community_id FROM nodes WHERE community_id IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge community assignment: %w", err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var nodeID, communityID int64
		if err := rows.Scan(&nodeID, &communityID); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge community assignment: %w", err)
		}
		if _, seen := out[communityID]; seen {
			continue
		}
		if native, ok := byID[nodeID]; ok {
			out[communityID] = clusters[native]
		}
	}
	return out, rowsErr(rows, tableNodes)
}

// readRiskIndex reads the release's `risk_index` rows in full. `last_computed`
// is a wall-clock timestamp — the one nondeterministic column — and is not read.
func (s *BridgeStore) readRiskIndex(byID map[int64]string) (map[string]BridgeRisk, error) {
	out := map[string]BridgeRisk{}
	if !s.usable(tableRiskIndex) {
		return out, nil
	}
	rows, err := s.db.Query(`SELECT node_id,qualified_name,COALESCE(risk_score,0),
	                                COALESCE(caller_count,0),COALESCE(test_coverage,''),
	                                COALESCE(security_relevant,0) FROM risk_index`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge risk_index: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID int64
		var securityRelevant int
		var risk BridgeRisk
		if err := rows.Scan(&nodeID, &risk.QualifiedName, &risk.RiskScore,
			&risk.CallerCount, &risk.TestCoverage, &securityRelevant); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge risk_index: %w", err)
		}
		native, ok := byID[nodeID]
		if !ok {
			return nil, fmt.Errorf("crgbehavior: bridge risk_index names node %d that the nodes table does not hold",
				nodeID)
		}
		if risk.QualifiedName, err = s.norm.Qualified(risk.QualifiedName); err != nil {
			return nil, err
		}
		risk.SecurityRelevant = securityRelevant != 0
		out[native] = risk
	}
	return out, rowsErr(rows, tableRiskIndex)
}

// SearchFTS runs the release's OWN FTS5 search for one identifier and returns
// the matching qualified names, normalized and sorted.
//
// This is a real `nodes_fts MATCH` query against the release's `porter
// unicode61` index over (name, qualified_name, file_path, signature) — not a
// token dump compared as a set. Reproducing search RESULTS is the observable
// contract a review consumer depends on.
func (s *BridgeStore) SearchFTS(term string) ([]string, error) {
	if !s.usable(tableNodesFTS) {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT qualified_name FROM nodes_fts WHERE nodes_fts MATCH ?`, ftsQuery(term))
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: search bridge nodes_fts for %q: %w", term, err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var qualified string
		if err := rows.Scan(&qualified); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge nodes_fts hit: %w", err)
		}
		normalized, err := s.norm.Qualified(qualified)
		if err != nil {
			return nil, err
		}
		if !seen[normalized] {
			seen[normalized] = true
			out = append(out, normalized)
		}
	}
	sort.Strings(out)
	return out, rowsErr(rows, tableNodesFTS)
}

// ftsQuery quotes an identifier as an FTS5 string literal so an identifier
// containing FTS5 syntax (a leading '-', a '*', a ':') is searched literally
// rather than parsed as a query operator.
func ftsQuery(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
}

// Table names the gate reads. Hoisted so the release fixture, the capability
// probe and the readers share one spelling.
const (
	tableNodes              = "nodes"
	tableEdges              = "edges"
	tableFlows              = "flows"
	tableFlowMemberships    = "flow_memberships"
	tableFlowSnapshots      = "flow_snapshots"
	tableCommunities        = "communities"
	tableCommunitySummaries = "community_summaries"
	tableRiskIndex          = "risk_index"
	tableNodesFTS           = "nodes_fts"
)

// rowsErr wraps a row-iteration error with the table it came from.
func rowsErr(rows *sql.Rows, table string) error {
	return wrapIterErr(rows.Err(), table)
}

// wrapIterErr names the bridge table a row-iteration error came from.
func wrapIterErr(err error, table string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("crgbehavior: iterate bridge %s: %w", table, err)
}
