package codegraph

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Graph-analysis tools of code-review-graph v2.3.8: the four structural
// analyses the native backend can reproduce exactly, ported from
// code_review_graph/tools/analysis_tools.py and the helpers it draws from
// code_review_graph/analysis.py, plus traverse_graph from tools/query.py.
//
// Two analyses of that module are deliberately absent and stay on the
// retained bridge (see internal/crgrelease/capability.go):
//
//   - get_bridge_nodes_tool ranks by networkx betweenness centrality, which
//     above 5000 nodes is estimated from an UNSEEDED random sample
//     (nx.betweenness_centrality(..., k=min(500, n)) under
//     @py_random_state("seed") with seed=None). Two successive calls on one
//     graph disagree, so the ranking is not reproducible even by the release
//     against itself.
//   - get_suggested_questions_tool opens with three bridge_node questions
//     drawn from that same estimator, so its `questions`, `count` and
//     `by_priority` inherit the same irreproducibility.
//
// Everything here is otherwise a faithful port, including the parts that only
// look like details: the rankings are STABLE sorts over the store's
// primary-key order, because upstream relies on Python's stable list.sort
// over SQLite's rowid-ordered scans. An unstable sort would return equally
// scored rows in a different sequence on every call.

// Hard ceilings from analysis_tools.py. A caller may raise a per-tool bound
// above its default but never past these: an unbounded top_n returned over
// 500k tokens before upstream's #849 sweep.
const (
	maxHubNodes        = 100
	maxSurprising      = 100
	maxGapsPerCategory = 50
)

// Internal caps applied by find_knowledge_gaps BEFORE the tool wrapper counts
// the categories, so they bound `summary` and `total_gaps` as well. That is
// why they belong to the analysis and not to the wrapper's max_per_category.
const (
	maxIsolatedNodes     = 50
	maxUntestedHotspots  = 20
	thinCommunityMembers = 3
	hotspotMinDegree     = 5
)

// Field sets kept by `detail_level: "minimal"`.
var (
	minimalHubFields      = []string{"name", "kind", "total_degree"}
	minimalSurpriseFields = []string{"source", "target", "edge_kind", "surprise_score"}
	minimalGapFields      = []string{"name", "qualified_name", "community_id", "size", "degree"}
)

func init() {
	RegisterTool("get_hub_nodes_tool", getHubNodesTool)
	RegisterTool("get_knowledge_gaps_tool", getKnowledgeGapsTool)
	RegisterTool("get_surprising_connections_tool", getSurprisingConnectionsTool)
	RegisterTool("traverse_graph_tool", traverseGraphTool)
}

// ── shared analysis input ────────────────────────────────────────────────────

// analysisGraph is the extracted layer as upstream's analysis helpers see it:
// every non-File node in primary-key order, every edge in primary-key order,
// and a qualified-name index over those nodes.
//
// The node set excludes File nodes because every helper here reads
// get_all_nodes(exclude_files=True). That also makes upstream's
// `if src.kind == "File"` guard in find_surprising_connections unreachable —
// the map it indexes can never hold one — so that branch is not reproduced.
//
// Edge endpoints that are not nodes are normal and load-bearing: a
// cross-package CALLS target is a BARE name and an IMPORTS_FROM target is a
// module path. They contribute degree but are never scored as nodes.
type analysisGraph struct {
	nodes []graphstore.GraphNode
	edges []graphstore.GraphEdge
	byQN  map[string]*graphstore.GraphNode
}

func loadAnalysisGraph(e *Engine) (*analysisGraph, error) {
	g := &analysisGraph{byQN: map[string]*graphstore.GraphNode{}}
	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	if store == nil {
		// Never built: upstream opens a freshly migrated database and reads
		// empty tables, so every analysis answers "nothing found" rather than
		// failing the call.
		return g, nil
	}
	all, err := store.ReadAllNodes()
	if err != nil {
		return nil, err
	}
	g.nodes = make([]graphstore.GraphNode, 0, len(all))
	for _, node := range all {
		if node.Kind == graphstore.NodeKindFile {
			continue
		}
		g.nodes = append(g.nodes, node)
	}
	for i := range g.nodes {
		g.byQN[g.nodes[i].QualifiedName] = &g.nodes[i]
	}
	if g.edges, err = store.ReadAllEdges(); err != nil {
		return nil, err
	}
	return g, nil
}

// totalDegrees counts source and target appearances of every qualified name,
// which is the single undirected "degree" the gap and surprise analyses use.
// Hub ranking needs the two directions apart and counts them itself.
func (g *analysisGraph) totalDegrees() map[string]int {
	degree := make(map[string]int, len(g.nodes))
	for _, edge := range g.edges {
		degree[edge.SourceQualified]++
		degree[edge.TargetQualified]++
	}
	return degree
}

// communityValue renders nodes.community_id the way the release does: a real
// id, or null when community detection has not assigned one. A NULL column
// reads back as 0 and AUTOINCREMENT ids start at 1, so 0 is unambiguously
// "unassigned" — and the distinction is observable, because cross-community
// surprise scoring requires BOTH endpoints to have a community.
func communityValue(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// projectRows is analysis_tools._project: keep only the listed fields and drop
// the ones a row does not carry. It builds new rows rather than deleting keys,
// so the caller's rows are never mutated.
func projectRows(rows []map[string]any, fields []string) []map[string]any {
	projected := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		kept := make(map[string]any, len(fields))
		for _, field := range fields {
			if value, ok := row[field]; ok {
				kept[field] = value
			}
		}
		projected = append(projected, kept)
	}
	return projected
}

// ── get_hub_nodes_tool ──────────────────────────────────────────────────────

type hubNode struct {
	name          string
	qualifiedName string
	kind          string
	file          string
	inDegree      int
	outDegree     int
	totalDegree   int
	community     any
}

func (h hubNode) dict() map[string]any {
	return map[string]any{
		"name":           h.name,
		"qualified_name": h.qualifiedName,
		"kind":           h.kind,
		"file":           h.file,
		"in_degree":      h.inDegree,
		"out_degree":     h.outDegree,
		"total_degree":   h.totalDegree,
		"community_id":   h.community,
	}
}

// findHubNodes is analysis.find_hub_nodes with no result bound: the tool asks
// for every candidate so it can report an honest `total`, then cuts the list
// itself.
//
// Nodes with no edges at all are dropped rather than ranked last — a degree-0
// symbol is not a weakly connected hub, it is a gap, and
// get_knowledge_gaps_tool is where it surfaces.
func findHubNodes(g *analysisGraph) []hubNode {
	inDegree := make(map[string]int, len(g.nodes))
	outDegree := make(map[string]int, len(g.nodes))
	for _, edge := range g.edges {
		outDegree[edge.SourceQualified]++
		inDegree[edge.TargetQualified]++
	}
	scored := make([]hubNode, 0, len(g.nodes))
	for _, node := range g.nodes {
		in, out := inDegree[node.QualifiedName], outDegree[node.QualifiedName]
		if in+out == 0 {
			continue
		}
		scored = append(scored, hubNode{
			name:          SanitizeName(node.Name),
			qualifiedName: node.QualifiedName,
			kind:          node.Kind,
			file:          node.FilePath,
			inDegree:      in,
			outDegree:     out,
			totalDegree:   in + out,
			community:     communityValue(node.CommunityID),
		})
	}
	// Stable, so equal-degree hubs keep the store's primary-key order. This is
	// a tie-break clients actually observe: the fixture repository alone has
	// three nodes at degree 5 and six at degree 2.
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].totalDegree > scored[j].totalDegree
	})
	return scored
}

func getHubNodesTool(e *Engine, args crgrelease.Args) (any, error) {
	topN := args.Int("top_n")
	if err := ValidatePositiveInt(topN, "top_n"); err != nil {
		return nil, err
	}
	g, err := loadAnalysisGraph(e)
	if err != nil {
		return nil, err
	}
	visible, total, truncated := Bounded(findHubNodes(g), topN, maxHubNodes)
	hubs := make([]map[string]any, 0, len(visible))
	for _, hub := range visible {
		hubs = append(hubs, hub.dict())
	}
	if args.String("detail_level") == "minimal" {
		hubs = projectRows(hubs, minimalHubFields)
	}
	return map[string]any{
		"status": "ok",
		"summary": fmt.Sprintf("%d hub node(s) ranked by degree%s",
			total, ShownOf(len(hubs), total)),
		"hub_nodes": hubs,
		"count":     len(hubs),
		"total":     total,
		"truncated": truncated,
		"next_tool_suggestions": []string{
			"get_impact_radius -- check blast radius of a hub",
			"query_graph callers_of -- see what calls a hub",
			"get_bridge_nodes -- find architectural chokepoints",
		},
	}, nil
}

// ── get_knowledge_gaps_tool ─────────────────────────────────────────────────

// gapCategory is one named gap list. The categories are a slice rather than a
// map so the analysis runs in upstream's declaration order; the response
// exposes them as objects, where key order carries no meaning.
type gapCategory struct {
	name string
	rows []map[string]any
}

// findKnowledgeGaps is analysis.find_knowledge_gaps.
//
// Its two internal caps (isolated nodes at 50, untested hotspots at 20) apply
// HERE, before the tool counts anything, so `summary` and `total_gaps` report
// the CAPPED lengths. That is upstream's behaviour and it is visible: a
// repository with 60 isolated nodes reports 50. The wrapper's
// max_per_category then cuts the visible rows again without touching those
// totals.
func findKnowledgeGaps(g *analysisGraph, communities []graphstore.CommunityRow) []gapCategory {
	degree := make(map[string]int, len(g.nodes))
	tested := make(map[string]bool)
	for _, edge := range g.edges {
		degree[edge.SourceQualified]++
		degree[edge.TargetQualified]++
		if edge.Kind == graphstore.EdgeKindTestedBy {
			tested[edge.SourceQualified] = true
		}
	}

	isolated := make([]map[string]any, 0, len(g.nodes))
	hotspots := make([]map[string]any, 0, len(g.nodes))
	communitySizes := make(map[int64]int, len(communities))
	communityFiles := make(map[int64]map[string]bool, len(communities))
	for _, node := range g.nodes {
		d := degree[node.QualifiedName]
		// "Isolated" is degree <= 1, not degree 0: a symbol whose only edge is
		// the CONTAINS from its own file is just as disconnected.
		if d <= 1 {
			isolated = append(isolated, gapNodeRow(node, d))
		}
		if d >= hotspotMinDegree && !tested[node.QualifiedName] && !node.IsTest {
			hotspots = append(hotspots, gapNodeRow(node, d))
		}
		if node.CommunityID != 0 {
			communitySizes[node.CommunityID]++
			files := communityFiles[node.CommunityID]
			if files == nil {
				files = map[string]bool{}
				communityFiles[node.CommunityID] = files
			}
			files[node.FilePath] = true
		}
	}
	sort.SliceStable(hotspots, func(i, j int) bool {
		return hotspots[i]["degree"].(int) > hotspots[j]["degree"].(int)
	})

	thin := make([]map[string]any, 0, len(communities))
	singleFile := make([]map[string]any, 0, len(communities))
	for _, community := range communities {
		size := communitySizes[community.ID]
		if size < thinCommunityMembers {
			// A community row whose members have all gone reports size 0 and
			// is thin, which is how a stale partition surfaces.
			thin = append(thin, map[string]any{
				"community_id": community.ID,
				"name":         community.Name,
				"size":         size,
			})
		}
		files := communityFiles[community.ID]
		if len(files) == 1 && size >= thinCommunityMembers {
			singleFile = append(singleFile, map[string]any{
				"community_id": community.ID,
				"name":         community.Name,
				"size":         size,
				"file":         soleKey(files),
			})
		}
	}

	return []gapCategory{
		{"isolated_nodes", capRows(isolated, maxIsolatedNodes)},
		{"thin_communities", thin},
		{"untested_hotspots", capRows(hotspots, maxUntestedHotspots)},
		{"single_file_communities", singleFile},
	}
}

func gapNodeRow(node graphstore.GraphNode, degree int) map[string]any {
	return map[string]any{
		"name":           SanitizeName(node.Name),
		"qualified_name": node.QualifiedName,
		"kind":           node.Kind,
		"file":           node.FilePath,
		"degree":         degree,
	}
}

func capRows(rows []map[string]any, limit int) []map[string]any {
	if len(rows) > limit {
		return rows[:limit]
	}
	return rows
}

// soleKey returns the only key of a one-element set. Upstream's
// `next(iter(files))` is deterministic only because the caller has already
// established that there is exactly one; the signature makes that explicit.
func soleKey(set map[string]bool) string {
	for key := range set {
		return key
	}
	return ""
}

func getKnowledgeGapsTool(e *Engine, args crgrelease.Args) (any, error) {
	maxPerCategory := args.Int("max_per_category")
	if err := ValidatePositiveInt(maxPerCategory, "max_per_category"); err != nil {
		return nil, err
	}
	g, err := loadAnalysisGraph(e)
	if err != nil {
		return nil, err
	}
	communities, err := readCommunityRows(e)
	if err != nil {
		return nil, err
	}

	minimal := args.String("detail_level") == "minimal"
	summary := map[string]any{}
	gaps := map[string]any{}
	truncated := false
	total := 0
	for _, category := range findKnowledgeGaps(g, communities) {
		// Totals come from the list as the analysis produced it: the counts
		// are the whole point of the tool, and capping them here would hide
		// the very gap the caller asked about.
		summary[category.name] = len(category.rows)
		total += len(category.rows)
		visible, _, cut := Bounded(category.rows, maxPerCategory, maxGapsPerCategory)
		if minimal {
			visible = projectRows(visible, minimalGapFields)
		}
		gaps[category.name] = visible
		truncated = truncated || cut
	}
	return map[string]any{
		"status":     "ok",
		"summary":    summary,
		"gaps":       gaps,
		"total_gaps": total,
		"truncated":  truncated,
		"next_tool_suggestions": []string{
			"refactor dead_code -- find unused symbols",
			"get_hub_nodes -- find high-impact nodes",
			"get_suggested_questions -- review prompts",
		},
	}, nil
}

// readCommunityRows returns the `communities` table, or nothing when the graph
// has never been built.
func readCommunityRows(e *Engine) ([]graphstore.CommunityRow, error) {
	store, err := e.readStore()
	if err != nil || store == nil {
		return nil, err
	}
	return store.ReadCommunities()
}

// ── get_surprising_connections_tool ─────────────────────────────────────────

type surprisingEdge struct {
	source          string
	sourceQualified string
	target          string
	targetQualified string
	edgeKind        string
	score           float64
	reasons         []string
	sourceCommunity any
	targetCommunity any
}

func (s surprisingEdge) dict() map[string]any {
	return map[string]any{
		"source":           s.source,
		"source_qualified": s.sourceQualified,
		"target":           s.target,
		"target_qualified": s.targetQualified,
		"edge_kind":        s.edgeKind,
		"surprise_score":   s.score,
		"reasons":          s.reasons,
		"source_community": s.sourceCommunity,
		"target_community": s.targetCommunity,
	}
}

// Composite surprise weights. Every one is a multiple of 0.05, which is what
// makes rounding the accumulated score to two decimals exact.
const (
	surpriseCrossCommunity = 0.3
	surpriseCrossLanguage  = 0.2
	surprisePeripheralHub  = 0.2
	surpriseCrossTest      = 0.15
	surpriseUnusualKind    = 0.15
)

// findSurprisingConnections is analysis.find_surprising_connections with no
// result bound.
//
// Only edges whose BOTH endpoints are nodes of the graph are scored, which
// silently excludes every cross-package call and every import because their
// targets are bare names and module paths. That is real upstream behaviour,
// and it is why the fixture repository scores zero surprising connections.
func findSurprisingConnections(g *analysisGraph) []surprisingEdge {
	degree := g.totalDegrees()
	degrees := make([]int, 0, len(degree))
	for _, d := range degree {
		if d > 0 {
			degrees = append(degrees, d)
		}
	}
	if len(degrees) == 0 {
		return nil
	}
	sort.Ints(degrees)
	// Upstream's "median" is the upper-middle element, not the mean of the two
	// middles. The floor of 10 stops a tiny graph, where triple the median is
	// still 3, from calling every edge peripheral-to-hub.
	median := degrees[len(degrees)/2]
	highDegree := median * 3
	if highDegree < 10 {
		highDegree = 10
	}

	scored := make([]surprisingEdge, 0, len(g.edges))
	for _, edge := range g.edges {
		src, tgt := g.byQN[edge.SourceQualified], g.byQN[edge.TargetQualified]
		if src == nil || tgt == nil {
			continue
		}
		score := 0.0
		reasons := []string{}

		srcCommunity := communityValue(src.CommunityID)
		tgtCommunity := communityValue(tgt.CommunityID)
		if srcCommunity != nil && tgtCommunity != nil && src.CommunityID != tgt.CommunityID {
			score += surpriseCrossCommunity
			reasons = append(reasons, "cross-community")
		}

		srcLang, tgtLang := pathSuffix(src.FilePath), pathSuffix(tgt.FilePath)
		if srcLang != "" && tgtLang != "" && srcLang != tgtLang {
			score += surpriseCrossLanguage
			reasons = append(reasons, "cross-language")
		}

		srcDegree, tgtDegree := degree[edge.SourceQualified], degree[edge.TargetQualified]
		if (srcDegree <= 2 && tgtDegree >= highDegree) ||
			(tgtDegree <= 2 && srcDegree >= highDegree) {
			score += surprisePeripheralHub
			reasons = append(reasons, "peripheral-to-hub")
		}

		if src.IsTest != tgt.IsTest && edge.Kind == graphstore.EdgeKindCalls {
			score += surpriseCrossTest
			reasons = append(reasons, "cross-test-boundary")
		}

		if edge.Kind == graphstore.EdgeKindCalls && src.Kind == graphstore.NodeKindType {
			score += surpriseUnusualKind
			reasons = append(reasons, "unusual-edge-kind")
		}

		if score <= 0 {
			continue
		}
		scored = append(scored, surprisingEdge{
			source:          SanitizeName(src.Name),
			sourceQualified: edge.SourceQualified,
			target:          SanitizeName(tgt.Name),
			targetQualified: edge.TargetQualified,
			edgeKind:        edge.Kind,
			// The accumulated sum carries binary error (0.3+0.2+0.2+0.15 is
			// 0.8500000000000001). Every reachable score is a multiple of
			// 0.05, so scaling by 100 lands on an integer and the rounding
			// mode's tie-break rule is never reached.
			score:           math.Round(score*100) / 100,
			reasons:         reasons,
			sourceCommunity: srcCommunity,
			targetCommunity: tgtCommunity,
		})
	}
	// Stable, so equally surprising edges keep primary-key order.
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	return scored
}

// pathSuffix is upstream's crude language probe:
// `path.rsplit(".", 1)[-1] if "." in path else ""`. It splits on the LAST dot
// of the WHOLE path, so a dotted directory yields a suffix containing a
// separator. Reproduced as-is: the comparison only ever asks whether two
// suffixes differ.
func pathSuffix(path string) string {
	if index := strings.LastIndexByte(path, '.'); index >= 0 {
		return path[index+1:]
	}
	return ""
}

func getSurprisingConnectionsTool(e *Engine, args crgrelease.Args) (any, error) {
	topN := args.Int("top_n")
	if err := ValidatePositiveInt(topN, "top_n"); err != nil {
		return nil, err
	}
	g, err := loadAnalysisGraph(e)
	if err != nil {
		return nil, err
	}
	visible, total, truncated := Bounded(findSurprisingConnections(g), topN, maxSurprising)
	surprises := make([]map[string]any, 0, len(visible))
	for _, surprise := range visible {
		surprises = append(surprises, surprise.dict())
	}
	if args.String("detail_level") == "minimal" {
		surprises = projectRows(surprises, minimalSurpriseFields)
	}
	return map[string]any{
		"status": "ok",
		"summary": fmt.Sprintf("%d surprising connection(s) ranked by surprise score%s",
			total, ShownOf(len(surprises), total)),
		"surprising_connections": surprises,
		"count":                  len(surprises),
		"total":                  total,
		"truncated":              truncated,
		"next_tool_suggestions": []string{
			"get_architecture_overview -- community structure",
			"query_graph callers_of -- trace the coupling",
			"get_bridge_nodes -- find chokepoints",
		},
	}, nil
}

// ── traverse_graph_tool ─────────────────────────────────────────────────────

const (
	traverseMinDepth = 1
	traverseMaxDepth = 6
)

// traverseFrame is one queued (node, depth) pair. One slice serves both modes:
// BFS takes from the front, DFS from the back.
type traverseFrame struct {
	qualifiedName string
	depth         int
}

func traverseGraphTool(e *Engine, args crgrelease.Args) (any, error) {
	query := args.String("query")
	mode := args.String("mode")
	tokenBudget := args.Int("token_budget")
	// The schema publishes no bounds, so the documented 1-6 range is enforced
	// here — and the clamped value, not the requested one, is echoed back as
	// `max_depth`.
	depth := max(traverseMinDepth, min(args.Int("depth"), traverseMaxDepth))

	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	// HybridSearch answers a never-built graph as "no results", so the
	// unbuilt case falls through to the same branch as an unknown symbol.
	hits, _, err := HybridSearch(store, query, "", 1, nil)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		// Upstream returns this bare pair, NOT the standard error response:
		// no `status`, no `summary`, and the call is a success. A client that
		// asked about a symbol the graph does not know gets an answer rather
		// than a transport failure.
		return map[string]any{
			"error": fmt.Sprintf("No node matching '%s'", query),
			"nodes": []any{},
		}, nil
	}
	startQualifiedName := hits[0].Node.QualifiedName

	visited := map[string]int{}
	queue := []traverseFrame{{startQualifiedName, 0}}
	traversal := []map[string]any{}
	approxTokens := 0

	for len(queue) > 0 {
		var current traverseFrame
		if mode == "bfs" {
			current, queue = queue[0], queue[1:]
		} else {
			// Any mode that is not exactly "bfs" is depth-first, including a
			// misspelling: upstream branches on equality rather than on a set
			// of known modes, and echoes the value back unchanged.
			current, queue = queue[len(queue)-1], queue[:len(queue)-1]
		}
		if _, seen := visited[current.qualifiedName]; seen {
			continue
		}
		// The depth cut happens BEFORE the node is marked visited, so a node
		// first reached too deep can still be expanded when a shorter path to
		// it comes off the queue later.
		if current.depth > depth {
			continue
		}
		visited[current.qualifiedName] = current.depth

		node, err := store.GetNode(current.qualifiedName)
		if err != nil {
			return nil, err
		}
		if node == nil {
			// A bare cross-package call target: marked visited so the
			// traversal cannot loop on it, but it contributes no entry and no
			// neighbours.
			continue
		}

		entry := traversalEntry{
			name:          SanitizeName(node.Name),
			qualifiedName: node.QualifiedName,
			kind:          node.Kind,
			file:          node.FilePath,
			depth:         current.depth,
		}
		approxTokens += entry.tokenCost()
		if approxTokens > tokenBudget {
			// The over-budget entry is NOT emitted, and the accumulated total
			// stays over budget, which is what makes `truncated` true below.
			break
		}
		traversal = append(traversal, entry.dict())

		outgoing, err := store.GetEdgesBySource(current.qualifiedName)
		if err != nil {
			return nil, err
		}
		incoming, err := store.GetEdgesByTarget(current.qualifiedName)
		if err != nil {
			return nil, err
		}
		for _, edge := range outgoing {
			if _, seen := visited[edge.TargetQualified]; !seen {
				queue = append(queue, traverseFrame{edge.TargetQualified, current.depth + 1})
			}
		}
		for _, edge := range incoming {
			if _, seen := visited[edge.SourceQualified]; !seen {
				queue = append(queue, traverseFrame{edge.SourceQualified, current.depth + 1})
			}
		}
	}

	return map[string]any{
		"start_node":    startQualifiedName,
		"mode":          mode,
		"max_depth":     depth,
		"nodes_visited": len(traversal),
		"traversal":     traversal,
		"truncated":     approxTokens > tokenBudget,
		"next_tool_suggestions": []string{
			"query_graph callers_of -- focused relationship query",
			"get_impact_radius -- blast radius analysis",
		},
	}, nil
}

// traversalEntry is one row of the traversal, and the unit the token budget is
// spent in.
type traversalEntry struct {
	name          string
	qualifiedName string
	kind          string
	file          string
	depth         int
}

func (t traversalEntry) dict() map[string]any {
	return map[string]any{
		"name":           t.name,
		"qualified_name": t.qualifiedName,
		"kind":           t.kind,
		"file":           t.file,
		"depth":          t.depth,
	}
}

// tokenCost is upstream's `len(str(entry)) // 4`.
//
// This is not an estimate that can be approximated away: it decides where the
// budget cuts and therefore which nodes appear in the response. `str()` of a
// Python dict is its repr, so the cost depends on Python's quoting and
// escaping rules and on the fact that len() counts CODE POINTS, not bytes.
// Both are reproduced by pythonStringRepr.
//
// One consequence worth naming: the entry embeds two ABSOLUTE paths, so the
// same node costs different amounts in different checkouts. A budget that
// cuts mid-traversal in one working copy will not cut at the same node in
// another.
func (t traversalEntry) tokenCost() int {
	repr := "{'name': " + pythonStringRepr(t.name) +
		", 'qualified_name': " + pythonStringRepr(t.qualifiedName) +
		", 'kind': " + pythonStringRepr(t.kind) +
		", 'file': " + pythonStringRepr(t.file) +
		", 'depth': " + strconv.Itoa(t.depth) + "}"
	return utf8.RuneCountInString(repr) / 4
}

// pythonStringRepr renders a string the way CPython's repr() does: single
// quotes, switching to double quotes only when the value contains a single
// quote and no double quote; backslash and the active quote escaped; tab,
// newline and carriage return as short escapes; anything unprintable as \xNN,
// \uXXXX or \UXXXXXXXX.
//
// "Unprintable" is Python's str.isprintable(), i.e. not in categories Cc, Cf,
// Cs, Co, Cn, Zl, Zp or Zs with U+0020 excepted — which is exactly what Go's
// unicode.IsPrint decides. The two can disagree only for a code point whose
// category differs between Go's and CPython's Unicode tables.
func pythonStringRepr(value string) string {
	quote := byte('\'')
	if strings.IndexByte(value, '\'') >= 0 && strings.IndexByte(value, '"') < 0 {
		quote = '"'
	}
	var out strings.Builder
	out.Grow(len(value) + 2)
	out.WriteByte(quote)
	for _, r := range value {
		switch {
		case r == rune(quote) || r == '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case r == '\t':
			out.WriteString(`\t`)
		case r == '\n':
			out.WriteString(`\n`)
		case r == '\r':
			out.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&out, `\x%02x`, r)
		case r < 0x7f || unicode.IsPrint(r):
			out.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&out, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&out, `\u%04x`, r)
		default:
			fmt.Fprintf(&out, `\U%08x`, r)
		}
	}
	out.WriteByte(quote)
	return out.String()
}
