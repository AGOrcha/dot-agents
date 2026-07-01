package kgquery

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// seedSearchFetch is the internal ceiling requested from the reader's
// SearchNodes when gathering seed candidates. It is generous relative to the
// caller-facing limit because the raw search set includes files, tests, and
// other languages that SeedSymbols filters out before applying the limit. The
// provider clamps this to its own hard cap (graphstore CONTRACT.md guarantee
// #1), which bounds seeding on very large graphs — acceptable for v1.
const seedSearchFetch = 500

// symbolKinds are the node kinds SeedSymbols considers a candidate code site.
// Files and tests are never seeds: a task is framed around a function or a
// type, and tests are hidden from the agent (R4 decision D4.7).
var symbolKinds = map[string]bool{
	graphstore.NodeKindFunction: true,
	graphstore.NodeKindClass:    true,
	graphstore.NodeKindType:     true,
}

// Querier is the read-only KG adapter for the task generator. It wraps a
// [graphstore.CodeGraphReader] and exposes the three questions the generator
// and difficulty-derivation ask of the graph. It issues no writes and holds
// no mutable state, so a single Querier is safe to reuse across generations
// (subject to the reader's own single-goroutine ownership rule).
type Querier struct {
	reader graphstore.CodeGraphReader
}

// New returns a Querier backed by reader. It errors on a nil reader so a
// mis-wired harness fails at construction rather than at first query.
func New(reader graphstore.CodeGraphReader) (*Querier, error) {
	if reader == nil {
		return nil, fmt.Errorf("kgquery: reader is required")
	}
	return &Querier{reader: reader}, nil
}

// Neighborhood is the call-graph vicinity of a root symbol: the root itself,
// every node reachable within the requested depth (including the root), and
// the edges among them. Nodes and Edges are sorted deterministically so the
// same graph state yields byte-identical downstream difficulty signals.
type Neighborhood struct {
	Root  graphstore.GraphNode
	Nodes []graphstore.GraphNode
	Edges []graphstore.GraphEdge
}

// Complexity is a reproducible complexity proxy for a single symbol, derived
// from stored structure only (no re-parse). SpanLines is the symbol's source
// extent; FanOut/FanIn are its distinct outbound/inbound CALLS relations; and
// Cyclomatic is a McCabe-style proxy (1 + FanOut) that treats each distinct
// callee as a branch. Difficulty-derivation buckets on these fields.
type Complexity struct {
	QualifiedName string
	SpanLines     int
	FanOut        int
	FanIn         int
	Cyclomatic    int
}

// SeedSymbols returns up to limit candidate seed symbols for lang, sorted by
// qualified name for determinism. Only function/class/type nodes in the
// requested language are considered; files and test nodes are excluded. The
// language match is case-insensitive against the node's stored language, which
// is expected to be the canonical lowercase form (e.g. "go", "python").
func (q *Querier) SeedSymbols(ctx context.Context, lang eval.Language, limit int) ([]graphstore.GraphNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !lang.Valid() {
		return nil, fmt.Errorf("kgquery: invalid language %q", lang)
	}
	if limit <= 0 {
		return nil, fmt.Errorf("kgquery: limit must be positive, got %d", limit)
	}
	// Empty query matches every node name; we filter to language + symbol
	// kind ourselves rather than push it into the search predicate.
	nodes, err := q.reader.SearchNodes("", seedSearchFetch)
	if err != nil {
		return nil, fmt.Errorf("kgquery: search seed candidates: %w", err)
	}
	want := strings.ToLower(string(lang))
	seeds := make([]graphstore.GraphNode, 0, len(nodes))
	for _, n := range nodes {
		if n.IsTest || !symbolKinds[n.Kind] {
			continue
		}
		if strings.ToLower(n.Language) != want {
			continue
		}
		seeds = append(seeds, n)
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].QualifiedName < seeds[j].QualifiedName })
	if len(seeds) > limit {
		seeds = seeds[:limit]
	}
	return seeds, nil
}

// NeighborhoodFor returns the neighborhood of qualifiedName out to depth hops,
// traversing both outbound and inbound edges. Depth 0 is the root alone;
// depth 1 adds its direct neighbors; and so on. Cycles are handled via a
// visited set. It errors when qualifiedName is empty, depth is negative, or
// the root symbol is not in the graph.
func (q *Querier) NeighborhoodFor(ctx context.Context, qualifiedName string, depth int) (Neighborhood, error) {
	if err := ctx.Err(); err != nil {
		return Neighborhood{}, err
	}
	if strings.TrimSpace(qualifiedName) == "" {
		return Neighborhood{}, fmt.Errorf("kgquery: qualified name is required")
	}
	if depth < 0 {
		return Neighborhood{}, fmt.Errorf("kgquery: depth must be non-negative, got %d", depth)
	}
	root, err := q.reader.GetNode(qualifiedName)
	if err != nil {
		return Neighborhood{}, fmt.Errorf("kgquery: get root %q: %w", qualifiedName, err)
	}
	if root == nil {
		return Neighborhood{}, fmt.Errorf("kgquery: symbol %q not found", qualifiedName)
	}

	visitedNodes := map[string]graphstore.GraphNode{qualifiedName: *root}
	edgeSet := map[string]graphstore.GraphEdge{}
	frontier := []string{qualifiedName}

	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		if err := ctx.Err(); err != nil {
			return Neighborhood{}, err
		}
		var next []string
		for _, name := range frontier {
			neighbors, err := q.expand(name, edgeSet)
			if err != nil {
				return Neighborhood{}, err
			}
			for _, nb := range neighbors {
				if _, seen := visitedNodes[nb]; seen {
					continue
				}
				node, err := q.reader.GetNode(nb)
				if err != nil {
					return Neighborhood{}, fmt.Errorf("kgquery: get neighbor %q: %w", nb, err)
				}
				if node == nil {
					// Dangling edge target: record it visited so we do not
					// re-resolve it, but it contributes no node.
					visitedNodes[nb] = graphstore.GraphNode{}
					continue
				}
				visitedNodes[nb] = *node
				next = append(next, nb)
			}
		}
		frontier = next
	}

	return Neighborhood{
		Root:  *root,
		Nodes: sortedNodes(visitedNodes),
		Edges: sortedEdges(edgeSet),
	}, nil
}

// expand loads the outbound and inbound edges of name, records them in
// edgeSet (deduped by identity), and returns the distinct adjacent qualified
// names discovered.
func (q *Querier) expand(name string, edgeSet map[string]graphstore.GraphEdge) ([]string, error) {
	out, err := q.reader.GetEdgesBySource(name)
	if err != nil {
		return nil, fmt.Errorf("kgquery: outbound edges of %q: %w", name, err)
	}
	in, err := q.reader.GetEdgesByTarget(name)
	if err != nil {
		return nil, fmt.Errorf("kgquery: inbound edges of %q: %w", name, err)
	}
	seen := map[string]bool{}
	var neighbors []string
	add := func(e graphstore.GraphEdge, adjacent string) {
		edgeSet[edgeKey(e)] = e
		if adjacent != "" && adjacent != name && !seen[adjacent] {
			seen[adjacent] = true
			neighbors = append(neighbors, adjacent)
		}
	}
	for _, e := range out {
		add(e, e.TargetQualified)
	}
	for _, e := range in {
		add(e, e.SourceQualified)
	}
	return neighbors, nil
}

// ComplexityProxy computes the reproducible complexity proxy for
// qualifiedName. It errors when the name is empty or the symbol is absent.
func (q *Querier) ComplexityProxy(ctx context.Context, qualifiedName string) (Complexity, error) {
	if err := ctx.Err(); err != nil {
		return Complexity{}, err
	}
	if strings.TrimSpace(qualifiedName) == "" {
		return Complexity{}, fmt.Errorf("kgquery: qualified name is required")
	}
	node, err := q.reader.GetNode(qualifiedName)
	if err != nil {
		return Complexity{}, fmt.Errorf("kgquery: get symbol %q: %w", qualifiedName, err)
	}
	if node == nil {
		return Complexity{}, fmt.Errorf("kgquery: symbol %q not found", qualifiedName)
	}
	out, err := q.reader.GetEdgesBySource(qualifiedName)
	if err != nil {
		return Complexity{}, fmt.Errorf("kgquery: outbound edges of %q: %w", qualifiedName, err)
	}
	in, err := q.reader.GetEdgesByTarget(qualifiedName)
	if err != nil {
		return Complexity{}, fmt.Errorf("kgquery: inbound edges of %q: %w", qualifiedName, err)
	}
	fanOut := distinctCallees(out)
	fanIn := distinctCallers(in)
	return Complexity{
		QualifiedName: qualifiedName,
		SpanLines:     spanLines(*node),
		FanOut:        fanOut,
		FanIn:         fanIn,
		Cyclomatic:    1 + fanOut,
	}, nil
}

// spanLines is the source extent of a node in lines, clamped to at least 1 so
// a single-line symbol counts as one line and malformed spans do not go
// negative.
func spanLines(n graphstore.GraphNode) int {
	span := n.LineEnd - n.LineStart + 1
	if span < 1 {
		return 1
	}
	return span
}

// distinctCallees counts the distinct CALLS targets among outbound edges.
func distinctCallees(edges []graphstore.GraphEdge) int {
	seen := map[string]bool{}
	for _, e := range edges {
		if e.Kind == graphstore.EdgeKindCalls && e.TargetQualified != "" {
			seen[e.TargetQualified] = true
		}
	}
	return len(seen)
}

// distinctCallers counts the distinct CALLS sources among inbound edges.
func distinctCallers(edges []graphstore.GraphEdge) int {
	seen := map[string]bool{}
	for _, e := range edges {
		if e.Kind == graphstore.EdgeKindCalls && e.SourceQualified != "" {
			seen[e.SourceQualified] = true
		}
	}
	return len(seen)
}

// edgeKey is a stable identity for deduping edges gathered from both
// directions during traversal.
func edgeKey(e graphstore.GraphEdge) string {
	return e.SourceQualified + "\x00" + e.TargetQualified + "\x00" + e.Kind
}

func sortedNodes(m map[string]graphstore.GraphNode) []graphstore.GraphNode {
	out := make([]graphstore.GraphNode, 0, len(m))
	for _, n := range m {
		if n.QualifiedName == "" {
			continue // dangling-target placeholder
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QualifiedName < out[j].QualifiedName })
	return out
}

func sortedEdges(m map[string]graphstore.GraphEdge) []graphstore.GraphEdge {
	out := make([]graphstore.GraphEdge, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return edgeKey(out[i]) < edgeKey(out[j]) })
	return out
}
