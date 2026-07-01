package kgquery

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

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
// qualified name. Only function/class/type nodes in the requested language are
// considered; files and test nodes are excluded. The language match is
// case-insensitive against the node's stored language, which is expected to be
// the canonical lowercase form (e.g. "go", "python").
//
// The result is fully reproducible for a fixed graph state: the candidate set
// is the COMPLETE node population, enumerated file-by-file via GetAllFiles +
// GetNodesByFile (neither of which imposes a LIMIT), then deduped by qualified
// name and put into a total order BEFORE the cap. There is no bounded,
// unordered pre-filter step, so no valid seed can be silently dropped by
// truncation and repeated runs (or a different backend) yield the same list.
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
	files, err := q.reader.GetAllFiles()
	if err != nil {
		return nil, fmt.Errorf("kgquery: list files: %w", err)
	}
	// Iterate files in a stable order; the final sort makes iteration order
	// irrelevant to the output, but a deterministic sweep keeps behavior
	// obvious and independent of the reader's file enumeration order.
	sort.Strings(files)

	candidates, err := q.collectSeedCandidates(ctx, files, strings.ToLower(string(lang)))
	if err != nil {
		return nil, err
	}
	return rankSeeds(candidates, limit), nil
}

// collectSeedCandidates enumerates every node under files and keeps the ones
// that are seed candidates for want (the lowercased language). It is keyed by
// qualified name so a symbol reported under more than one file path collapses
// to a single, deterministically chosen candidate. ctx is honored per file so
// a long enumeration is cancellable.
func (q *Querier) collectSeedCandidates(ctx context.Context, files []string, want string) (map[string]graphstore.GraphNode, error) {
	candidates := map[string]graphstore.GraphNode{}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nodes, err := q.reader.GetNodesByFile(f)
		if err != nil {
			return nil, fmt.Errorf("kgquery: nodes in file %q: %w", f, err)
		}
		for _, n := range nodes {
			if isSeedCandidate(n, want) {
				candidates[n.QualifiedName] = n
			}
		}
	}
	return candidates, nil
}

// isSeedCandidate reports whether n is an eligible seed for the lowercased
// language want: a non-test function/class/type node in that language.
func isSeedCandidate(n graphstore.GraphNode, want string) bool {
	if n.IsTest || !symbolKinds[n.Kind] {
		return false
	}
	return strings.ToLower(n.Language) == want
}

// rankSeeds puts the candidate set into a total order by qualified name and
// applies the cap. The order is imposed over the COMPLETE set before the cap
// so the cap never silently drops a valid seed via truncation.
func rankSeeds(candidates map[string]graphstore.GraphNode, limit int) []graphstore.GraphNode {
	seeds := make([]graphstore.GraphNode, 0, len(candidates))
	for _, n := range candidates {
		seeds = append(seeds, n)
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].QualifiedName < seeds[j].QualifiedName })
	if len(seeds) > limit {
		seeds = seeds[:limit]
	}
	return seeds
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

	tr := &traversal{
		visited: map[string]graphstore.GraphNode{qualifiedName: *root},
		edges:   map[string]graphstore.GraphEdge{},
	}
	frontier := []string{qualifiedName}
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		next, err := q.stepFrontier(ctx, frontier, tr)
		if err != nil {
			return Neighborhood{}, err
		}
		frontier = next
	}

	return Neighborhood{
		Root:  *root,
		Nodes: sortedNodes(tr.visited),
		Edges: sortedEdges(tr.edges),
	}, nil
}

// traversal is the mutable working state of a NeighborhoodFor BFS: the nodes
// already resolved (keyed by qualified name) and the edges gathered so far
// (keyed by edge identity for dedupe).
type traversal struct {
	visited map[string]graphstore.GraphNode
	edges   map[string]graphstore.GraphEdge
}

// stepFrontier expands every name in the current frontier by one hop and
// returns the next frontier (the newly discovered, resolvable neighbors). It
// honors ctx cancellation before each name so a cancel during the current
// (including the final) hop is observed rather than swallowed.
func (q *Querier) stepFrontier(ctx context.Context, frontier []string, tr *traversal) ([]string, error) {
	var next []string
	for _, name := range frontier {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		discovered, err := q.visitNeighbors(ctx, name, tr)
		if err != nil {
			return nil, err
		}
		next = append(next, discovered...)
	}
	return next, nil
}

// visitNeighbors records name's edges into tr, resolves each not-yet-seen
// adjacent symbol, and returns the ones that resolved to a real node.
// Dangling edge targets are marked visited (via a zero-value placeholder) so
// they are neither re-resolved nor emitted as phantom nodes. ctx is checked
// after the edge fetch and before each neighbor lookup so cancellation is
// observed even within a single hop's work.
func (q *Querier) visitNeighbors(ctx context.Context, name string, tr *traversal) ([]string, error) {
	neighbors, err := q.expand(name, tr.edges)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var discovered []string
	for _, nb := range neighbors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, seen := tr.visited[nb]; seen {
			continue
		}
		node, err := q.reader.GetNode(nb)
		if err != nil {
			return nil, fmt.Errorf("kgquery: get neighbor %q: %w", nb, err)
		}
		if node == nil {
			tr.visited[nb] = graphstore.GraphNode{}
			continue
		}
		tr.visited[nb] = *node
		discovered = append(discovered, nb)
	}
	return discovered, nil
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
