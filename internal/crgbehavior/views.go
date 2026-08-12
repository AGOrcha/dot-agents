package crgbehavior

import (
	"database/sql"
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

// ErrBridgeUnavailable reports that the legacy Python CRG side cannot be
// driven on this machine (no graph.db, or no code-review-graph CLI). The gate
// SKIPS rather than fails on it: a missing legacy bridge is an environment
// fact, not a behavior divergence.
var ErrBridgeUnavailable = errors.New("crgbehavior: legacy CRG bridge unavailable")

// readOnlyPragma opens the bridge's SQLite store in query-only mode. The gate
// only ever READS legacy state — it must never mutate the graph it is
// comparing against.
const readOnlyPragma = "?_pragma=query_only(true)"

// edgeKindImportsFrom is the legacy bridge's spelling of the import edge; the
// kg-native adapter's schema spells it IMPORTS. Normalizing it is what lets the
// native community derivation see the same dependency edges the bridge stored.
const edgeKindImportsFrom = "IMPORTS_FROM"

// BridgeViews is the legacy bridge's persisted state for one repository: the
// symbol graph it stored, plus the derived materialized views it computed
// (flow_memberships, community assignment, risk_index, FTS). Every field is
// what the PYTHON side actually persisted — the gate compares these against the
// kg-native adapter's own derivations of the same views.
type BridgeViews struct {
	// Symbols and References are the bridge's graph, normalized to the
	// kg-native ingestion shape so the native adapter can ingest the identical
	// symbol set (divergence must come from the derivations, not the input).
	Symbols    []crg.Symbol
	References []crg.Reference
	// FlowMemberships is the bridge's persisted flow_memberships table, keyed
	// by native symbol ids.
	FlowMemberships []crg.FlowMembership
	// Communities maps native symbol id → the bridge's community id.
	Communities map[string]string
	// RiskIndex maps native symbol id → the bridge's risk_score.
	RiskIndex map[string]float64
	// FTS is the sorted distinct token set of the bridge's nodes_fts index.
	FTS []string
	// FilesIndexed is the count of distinct file paths in the bridge graph.
	FilesIndexed int
}

// Corpus lowers the bridge's graph to the kg-native ingestion corpus for
// commit. Bridge edge endpoints the bridge itself left unresolved (bare call
// targets such as `append`) do not match any symbol and are dropped by
// crg.Corpus.ToGraph — the same resolvable subgraph the bridge's own flow
// derivation runs over.
func (v BridgeViews) Corpus(commit string) crg.Corpus {
	return crg.Corpus{Commit: commit, Symbols: v.Symbols, References: v.References}
}

// bridgeNode is one legacy node row, before normalization.
type bridgeNode struct {
	id            int64
	qualifiedName string
	filePath      string
	kind          string
	language      string
	lineStart     int
	fileHash      string
	communityID   sql.NullInt64
}

// ReadBridgeViews reads the legacy bridge's persisted graph and derived views
// out of its own SQLite store (read-only), normalizing every identifier to the
// kg-native id space so both sides of the gate are keyed identically. It
// returns ErrBridgeUnavailable when the repository has no built graph.
func ReadBridgeViews(repoRoot, dbPath string) (BridgeViews, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return BridgeViews{}, fmt.Errorf("%w: no graph at %s", ErrBridgeUnavailable, dbPath)
	}
	return readFromStore(sqliteDriver, dbPath, repoRoot)
}

// sqliteDriver is the database/sql driver the legacy bridge's store speaks.
const sqliteDriver = "sqlite"

// readFromStore opens the legacy store in query-only mode and reads it back.
func readFromStore(driver, dbPath, repoRoot string) (BridgeViews, error) {
	db, err := sql.Open(driver, dbPath+readOnlyPragma)
	if err != nil {
		return BridgeViews{}, fmt.Errorf("crgbehavior: open bridge graph: %w", err)
	}
	defer db.Close()
	return readViews(db, repoRoot)
}

// readViews runs every bridge-side read against an already-open store.
func readViews(db *sql.DB, repoRoot string) (BridgeViews, error) {
	nodes, err := readNodes(db, repoRoot)
	if err != nil {
		return BridgeViews{}, err
	}
	if len(nodes) == 0 {
		return BridgeViews{}, fmt.Errorf("%w: bridge graph has no nodes", ErrBridgeUnavailable)
	}
	refs, err := readReferences(db, repoRoot)
	if err != nil {
		return BridgeViews{}, err
	}
	views := viewsFromNodes(nodes)
	views.References = refs
	if err := readDerivedViews(db, nodes, repoRoot, &views); err != nil {
		return BridgeViews{}, err
	}
	return views, nil
}

// readDerivedViews fills the flow / FTS / risk views the bridge persisted.
func readDerivedViews(db *sql.DB, nodes []bridgeNode, repoRoot string, views *BridgeViews) error {
	idByNode := nativeIDsByNodeID(nodes)
	byFlow, err := readFlowMemberships(db, idByNode)
	if err != nil {
		return err
	}
	views.FlowMemberships = keyFlowsByEntryPoint(byFlow)
	tokens, err := readFTS(db, repoRoot)
	if err != nil {
		return err
	}
	views.FTS = sortedSet(tokens)
	risk, err := readRiskIndex(db, idByNode)
	if err != nil {
		return err
	}
	views.RiskIndex = risk
	return nil
}

// viewsFromNodes builds the symbol corpus and community assignment from the
// normalized node rows.
func viewsFromNodes(nodes []bridgeNode) BridgeViews {
	symbols := make([]crg.Symbol, 0, len(nodes))
	communities := make(map[string]string, len(nodes))
	files := map[string]bool{}
	for _, n := range nodes {
		sym := crg.Symbol{
			QualifiedName: n.qualifiedName,
			Kind:          n.kind,
			Language:      n.language,
			FilePath:      n.filePath,
			LineStart:     n.lineStart,
			ContentHash:   n.fileHash,
		}
		symbols = append(symbols, sym)
		files[n.filePath] = true
		communities[crg.SymbolID(sym)] = communityID(n)
	}
	return BridgeViews{Symbols: symbols, Communities: communities, FilesIndexed: len(files)}
}

// communityID is the bridge's cluster id for a node. A node the bridge left
// unassigned is its own singleton cluster, matching the kg-native partition's
// convention that every node is covered.
func communityID(n bridgeNode) string {
	if !n.communityID.Valid {
		return "unassigned:" + n.qualifiedName
	}
	return fmt.Sprintf("c%d", n.communityID.Int64)
}

// nativeIDsByNodeID maps each legacy integer node id to its native symbol id.
func nativeIDsByNodeID(nodes []bridgeNode) map[int64]string {
	out := make(map[int64]string, len(nodes))
	for _, n := range nodes {
		out[n.id] = crg.SymbolID(crg.Symbol{QualifiedName: n.qualifiedName, FilePath: n.filePath})
	}
	return out
}

// readNodes reads the bridge's nodes, normalizing absolute paths and qualified
// names to repo-relative form.
func readNodes(db *sql.DB, repoRoot string) ([]bridgeNode, error) {
	rows, err := db.Query(`SELECT id,qualified_name,file_path,kind,
	                              COALESCE(language,''),COALESCE(line_start,0),
	                              COALESCE(file_hash,''),community_id FROM nodes`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge nodes: %w", err)
	}
	defer rows.Close()
	var out []bridgeNode
	for rows.Next() {
		var n bridgeNode
		if err := rows.Scan(&n.id, &n.qualifiedName, &n.filePath, &n.kind,
			&n.language, &n.lineStart, &n.fileHash, &n.communityID); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge node: %w", err)
		}
		n.qualifiedName = relativize(n.qualifiedName, repoRoot)
		n.filePath = relativize(n.filePath, repoRoot)
		out = append(out, n)
	}
	return out, rowsErr(rows, "nodes")
}

// readReferences reads the bridge's edges, normalizing endpoints and mapping
// the legacy IMPORTS_FROM kind onto the kg-native IMPORTS spelling.
func readReferences(db *sql.DB, repoRoot string) ([]crg.Reference, error) {
	rows, err := db.Query(`SELECT kind,source_qualified,target_qualified FROM edges`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge edges: %w", err)
	}
	defer rows.Close()
	var out []crg.Reference
	for rows.Next() {
		var kind, from, to string
		if err := rows.Scan(&kind, &from, &to); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge edge: %w", err)
		}
		if kind == edgeKindImportsFrom {
			kind = "IMPORTS"
		}
		out = append(out, crg.Reference{
			Kind: kind,
			From: relativize(from, repoRoot),
			To:   relativize(to, repoRoot),
		})
	}
	return out, rowsErr(rows, "edges")
}

// readFlowMemberships reads the bridge's persisted flow_memberships rows,
// grouped by legacy flow id with node ids mapped into the native id space. A
// membership whose node is absent from the node table is dropped (it cannot be
// compared).
func readFlowMemberships(db *sql.DB, idByNode map[int64]string) (map[int64][]crg.FlowMembership, error) {
	rows, err := db.Query(`SELECT flow_id,node_id,position FROM flow_memberships`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge flow_memberships: %w", err)
	}
	defer rows.Close()
	byFlow := map[int64][]crg.FlowMembership{}
	for rows.Next() {
		var flowID, nodeID int64
		var position int
		if err := rows.Scan(&flowID, &nodeID, &position); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge flow_membership: %w", err)
		}
		member, ok := idByNode[nodeID]
		if !ok {
			continue
		}
		byFlow[flowID] = append(byFlow[flowID], crg.FlowMembership{MemberID: member, Position: position})
	}
	return byFlow, rowsErr(rows, "flow_memberships")
}

// keyFlowsByEntryPoint re-keys each legacy flow by its position-0 member — the
// entry-point symbol id the kg-native derivation uses as flow_id. Comparing on
// the entry point rather than the bridge's autoincrement id is what makes the
// two flow_memberships row sets comparable at all.
func keyFlowsByEntryPoint(byFlow map[int64][]crg.FlowMembership) []crg.FlowMembership {
	var out []crg.FlowMembership
	for _, members := range byFlow {
		entry := entryPointOf(members)
		if entry == "" {
			continue
		}
		for _, m := range members {
			out = append(out, crg.FlowMembership{FlowID: entry, MemberID: m.MemberID, Position: m.Position})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FlowID != out[j].FlowID {
			return out[i].FlowID < out[j].FlowID
		}
		return out[i].Position < out[j].Position
	})
	return out
}

// entryPointOf returns the position-0 member id of a legacy flow.
func entryPointOf(members []crg.FlowMembership) string {
	for _, m := range members {
		if m.Position == 0 {
			return m.MemberID
		}
	}
	return ""
}

// readRiskIndex reads the bridge's persisted risk_index scores, keyed natively.
func readRiskIndex(db *sql.DB, idByNode map[int64]string) (map[string]float64, error) {
	rows, err := db.Query(`SELECT node_id,COALESCE(risk_score,0) FROM risk_index`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge risk_index: %w", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var nodeID int64
		var score float64
		if err := rows.Scan(&nodeID, &score); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge risk_index: %w", err)
		}
		if id, ok := idByNode[nodeID]; ok {
			out[id] = score
		}
	}
	return out, rowsErr(rows, "risk_index")
}

// readFTS reads the bridge's FTS index content — the searchable qualified-name
// tokens — normalized to repo-relative form.
func readFTS(db *sql.DB, repoRoot string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT qualified_name FROM nodes_fts`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge nodes_fts: %w", err)
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var qn string
		if err := rows.Scan(&qn); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge fts token: %w", err)
		}
		set[relativize(qn, repoRoot)] = true
	}
	return set, rowsErr(rows, "nodes_fts")
}

// sortedSet returns a set's members in sorted order.
func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

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

// relativize strips the repository root prefix from an absolute bridge path or
// qualified name ("/abs/repo/pkg/f.go::Sym" → "pkg/f.go::Sym"), leaving values
// that are already repo-relative untouched.
func relativize(value, repoRoot string) string {
	if repoRoot == "" {
		return value
	}
	prefix := strings.TrimSuffix(repoRoot, "/") + "/"
	return strings.ReplaceAll(value, prefix, "")
}
