package codegraph

import (
	"sort"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// nodeKindFile is the structural node kind used for file-level rows. File nodes
// exist so `GetAllFiles` and the status file count work exactly as they did on
// the bridge; they are excluded from the crg symbol namespace, which declares
// only Function and Type symbols.
const nodeKindFile = graphstore.NodeKindFile

// namespaceView projects the persisted code graph into the crg adapter's
// namespace shape so every parity-verified derivation in
// internal/adapters/builtin/crg (flows, communities, risk index, FTS, impact
// radius, build snapshot) runs unchanged against the kg-native store.
//
// It satisfies crg.StoreReader. The namespace token is accepted and ignored:
// token enforcement is the SDK storage layer's contract (§8.2), and this is a
// read-only in-process projection of a store the caller already holds, not a
// second storage backend that could widen anyone's authority.
type namespaceView struct {
	notes []sdk.Note
	edges []sdk.Edge
}

// Notes returns the projected symbol notes.
func (v namespaceView) Notes(_ sdk.Token, _ string) ([]sdk.Note, error) { return v.notes, nil }

// Edges returns the projected symbol edges.
func (v namespaceView) Edges(_ sdk.Token, _ string) ([]sdk.Edge, error) { return v.edges, nil }

// graphSnapshot is one full read of the persisted code graph plus the lookups
// the engine's query paths need on top of it.
type graphSnapshot struct {
	// nodes are every persisted node, File rows included.
	nodes []graphstore.GraphNode
	// edges are every persisted edge.
	edges []graphstore.GraphEdge
	// view is the crg-namespace projection of the symbol subgraph.
	view namespaceView
	// idByQualified maps a symbol's qualified name to its crg note id.
	idByQualified map[string]string
	// nodeByID maps a crg note id back to the persisted node.
	nodeByID map[string]graphstore.GraphNode
}

// readSnapshot loads the whole persisted graph once. Every derived view the
// engine serves is computed from this single read, so one command never issues
// the same table scan twice.
func readSnapshot(store graphstore.CodeGraphReader) (graphSnapshot, error) {
	nodes, err := readAllNodes(store)
	if err != nil {
		return graphSnapshot{}, err
	}
	edges, err := readAllEdges(store, nodes)
	if err != nil {
		return graphSnapshot{}, err
	}
	return buildSnapshot(nodes, edges), nil
}

// buildSnapshot lowers persisted nodes/edges into the crg namespace projection.
// Lowering goes through crg.Corpus.ToGraph so the note/edge field names and the
// symbol-id derivation have exactly one definition, shared with the adapter's
// own ingestion path.
func buildSnapshot(nodes []graphstore.GraphNode, edges []graphstore.GraphEdge) graphSnapshot {
	corpus := crg.Corpus{}
	for _, n := range nodes {
		if n.Kind == nodeKindFile {
			continue
		}
		corpus.Symbols = append(corpus.Symbols, symbolFromNode(n))
	}
	for _, e := range edges {
		corpus.References = append(corpus.References, crg.Reference{
			Kind: e.Kind, From: e.SourceQualified, To: e.TargetQualified,
		})
	}
	notes, sdkEdges := corpus.ToGraph()
	snap := graphSnapshot{
		nodes:         nodes,
		edges:         edges,
		view:          namespaceView{notes: notes, edges: sdkEdges},
		idByQualified: make(map[string]string, len(corpus.Symbols)),
		nodeByID:      make(map[string]graphstore.GraphNode, len(corpus.Symbols)),
	}
	indexSnapshot(&snap, nodes, corpus.Symbols)
	return snap
}

// indexSnapshot fills the qualified-name and note-id lookups. Symbols and the
// non-File node slice are index-aligned because both are produced by the same
// filtered walk in buildSnapshot.
func indexSnapshot(snap *graphSnapshot, nodes []graphstore.GraphNode, symbols []crg.Symbol) {
	i := 0
	for _, n := range nodes {
		if n.Kind == nodeKindFile {
			continue
		}
		id := crg.SymbolID(symbols[i])
		i++
		if _, seen := snap.idByQualified[n.QualifiedName]; !seen {
			snap.idByQualified[n.QualifiedName] = id
		}
		snap.nodeByID[id] = n
	}
}

// symbolFromNode lowers a persisted node to the corpus symbol shape. The
// per-symbol content hash rides in `extra` because the store's own file_hash
// column is file-scoped, and the O5 source_mutation driver needs symbol-level
// change detection.
func symbolFromNode(n graphstore.GraphNode) crg.Symbol {
	hash := n.FileHash
	if v, ok := n.Extra[extraContentHash].(string); ok && v != "" {
		hash = v
	}
	return crg.Symbol{
		QualifiedName: n.QualifiedName,
		Kind:          n.Kind,
		Language:      n.Language,
		FilePath:      n.FilePath,
		LineStart:     n.LineStart,
		ContentHash:   hash,
	}
}

// readAllNodes enumerates every persisted node by walking the file list. The
// published contract has no "all nodes" read, so file enumeration plus the
// per-file node read is the contract-legal way to export the whole graph.
func readAllNodes(store graphstore.CodeGraphReader) ([]graphstore.GraphNode, error) {
	files, err := store.GetAllFiles()
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []graphstore.GraphNode
	for _, f := range files {
		nodes, err := store.GetNodesByFile(f)
		if err != nil {
			return nil, err
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].QualifiedName < nodes[j].QualifiedName })
		out = append(out, nodes...)
	}
	return out, nil
}

// readAllEdges enumerates every persisted edge by asking for each node's
// outgoing edges. Duplicate rows (an edge reachable from two identically-named
// sources) are collapsed by edge id.
func readAllEdges(store graphstore.CodeGraphReader, nodes []graphstore.GraphNode) ([]graphstore.GraphEdge, error) {
	seenSource := map[string]bool{}
	seenEdge := map[int64]bool{}
	var out []graphstore.GraphEdge
	for _, n := range nodes {
		if seenSource[n.QualifiedName] {
			continue
		}
		seenSource[n.QualifiedName] = true
		edges, err := store.GetEdgesBySource(n.QualifiedName)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if seenEdge[e.ID] {
				continue
			}
			seenEdge[e.ID] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// qualifiedNames maps crg note ids back to qualified names, preserving order.
func (s graphSnapshot) qualifiedNames(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if n, ok := s.nodeByID[id]; ok {
			out = append(out, n.QualifiedName)
		}
	}
	return out
}
