package codegraph

// Community tools — the native half of code-review-graph v2.3.8's
// tools/community_tools.py (tools 13-15) plus the two query helpers it leans
// on from communities.py (`get_communities`, `get_architecture_overview`).
//
// This file is PROJECTION only. Community detection — Leiden partitioning,
// test-node reassignment, oversized splitting, name generation, cohesion — is
// the postprocess lifecycle's job and lands in the `communities` table; these
// three tools read that table back and shape it into the release's responses.
// Re-detecting here would mean `list_communities_tool` could disagree with
// `get_community_tool` about what a community even is.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Hard response ceilings, above whatever the caller asked for.
//
// They exist because these payloads embed absolute qualified names: upstream
// measured a single 3000-member community serializing to >130k tokens, and a
// standard architecture overview to >600k, before the caps were added. They
// are ceilings, not defaults — `size` and the `*_total` fields always report
// the untruncated counts, so a capped response is short but never misleading.
const (
	commMaxCommunities = 200
	commMaxMembers     = 25
	commMaxCrossEdges  = 200
)

func init() {
	RegisterTool("list_communities_tool", listCommunitiesTool)
	RegisterTool("get_community_tool", getCommunityTool)
	RegisterTool("get_architecture_overview_tool", getArchitectureOverviewTool)
}

// ── Tool 13: list_communities ────────────────────────────────────────────────

func listCommunitiesTool(e *Engine, args crgrelease.Args) (any, error) {
	maxResults := args.Int("max_results")
	maxMembers := args.Int("max_members")
	if err := ValidatePositiveInt(maxResults, "max_results"); err != nil {
		return nil, err
	}
	if err := ValidatePositiveInt(maxMembers, "max_members"); err != nil {
		return nil, err
	}

	all, err := commGetCommunities(e, args.String("sort_by"), args.Int("min_size"))
	if err != nil {
		return nil, err
	}
	visible, total, truncated := Bounded(all, maxResults, commMaxCommunities)

	communities := make([]any, 0, len(visible))
	if args.String("detail_level") == "minimal" {
		// Minimal is deliberately not "fewer members": it drops the member
		// list, the description and the ids entirely, leaving the three
		// fields that let a caller decide which community to ask about.
		for _, community := range visible {
			communities = append(communities, map[string]any{
				"name":     community["name"],
				"size":     community["size"],
				"cohesion": community["cohesion"],
			})
		}
	} else {
		for _, community := range visible {
			communities = append(communities, commCapMembers(community, maxMembers))
		}
	}

	result := map[string]any{
		"status": "ok",
		"summary": fmt.Sprintf("Found %d communities", total) +
			ShownOf(len(communities), total),
		"communities": communities,
		"total":       total,
		"truncated":   truncated,
	}
	return e.AttachHints("list_communities", result), nil
}

// ── Tool 14: get_community ───────────────────────────────────────────────────

func getCommunityTool(e *Engine, args crgrelease.Args) (any, error) {
	maxMembers := args.Int("max_members")
	if err := ValidatePositiveInt(maxMembers, "max_members"); err != nil {
		return nil, err
	}

	all, err := commGetCommunities(e, "size", 0)
	if err != nil {
		return nil, err
	}

	// The two selectors are exclusive, not a fallback chain: an id that
	// matches nothing reports not_found rather than quietly matching a name.
	var selected map[string]any
	if wanted, ok := args.OptInt("community_id"); ok {
		for _, community := range all {
			if community["id"] == int64(wanted) {
				selected = community
				break
			}
		}
	} else if wanted, ok := args.OptString("community_name"); ok {
		needle := strings.ToLower(wanted)
		for _, community := range all {
			name, _ := community["name"].(string)
			if strings.Contains(strings.ToLower(name), needle) {
				selected = community
				break
			}
		}
	}

	if selected == nil {
		// Early return, so no `_hints`: the release only generates hints on
		// the success path and a not_found carrying next-step suggestions
		// would be a different payload.
		return map[string]any{
			"status":  "not_found",
			"summary": "No community found matching the given criteria.",
		}, nil
	}

	if args.Bool("include_members") {
		id, _ := selected["id"].(int64)
		details, err := commMemberDetails(e, id)
		if err != nil {
			return nil, err
		}
		selected["member_details"] = details
	}
	community := commCapMembers(selected, maxMembers)

	name, _ := community["name"].(string)
	size, _ := community["size"].(int)
	cohesion, _ := community["cohesion"].(float64)
	result := map[string]any{
		"status": "ok",
		"summary": fmt.Sprintf("Community '%s': %d nodes, cohesion %.4f",
			name, size, cohesion),
		"community": community,
	}
	return e.AttachHints("get_community", result), nil
}

// ── Tool 15: get_architecture_overview ───────────────────────────────────────

func getArchitectureOverviewTool(e *Engine, args crgrelease.Args) (any, error) {
	maxResults := args.Int("max_results")
	maxMembers := args.Int("max_members")
	if err := ValidatePositiveInt(maxResults, "max_results"); err != nil {
		return nil, err
	}
	if err := ValidatePositiveInt(maxMembers, "max_members"); err != nil {
		return nil, err
	}

	full, err := commArchitectureOverview(e)
	if err != nil {
		return nil, err
	}

	minimal := args.String("detail_level") == "minimal"
	warnings := full.warnings
	var communities, crossEdges []any
	if minimal {
		communities, crossEdges = commMinimalOverview(full)
	} else {
		communities = make([]any, 0, len(full.communities))
		for _, community := range full.communities {
			communities = append(communities, commCapMembers(community, maxMembers))
		}
		crossEdges = full.crossEdges
	}

	cross, crossTotal, truncated := Bounded(crossEdges, maxResults, commMaxCrossEdges)
	// One warning per highly-coupled pair grows quadratically with the
	// community count, so it carries the same bound as the edge rows.
	warnings, warnTotal, warnCut := Bounded(warnings, maxResults, commMaxCrossEdges)
	// `warnCut` cannot actually add anything: a warning exists only for a
	// coupled PAIR, so warnings are never more numerous than the rows, and
	// both lists are cut at the same limit. The term is kept because it is
	// upstream's, and because that argument stops holding the moment either
	// bound changes.
	truncated = truncated || warnCut

	// Minimal counts aggregated PAIRS where standard counts individual
	// edges, so the label has to move with the unit or the number lies.
	crossLabel := "cross-community edges"
	if minimal {
		crossLabel = "community pairs"
	}
	result := map[string]any{
		"status": "ok",
		"summary": fmt.Sprintf("Architecture: %d communities, %d %s",
			len(communities), crossTotal, crossLabel) +
			ShownOf(len(cross), crossTotal) +
			fmt.Sprintf(", %d warning(s)", warnTotal),
		"communities":                 communities,
		"cross_community_edges":       cross,
		"warnings":                    warnings,
		"cross_community_edges_total": crossTotal,
		"truncated":                   truncated,
	}
	e.AttachHints("get_architecture_overview", result)
	if minimal {
		// Measured against the FULL overview this call avoided sending, and
		// attached last because the returned-token side of the ratio is the
		// payload as it stands.
		AttachContextSavings(result, EstimateTokens(full.payload()))
	}
	return result, nil
}

// ── communities.py: get_communities ──────────────────────────────────────────

// commGetCommunities is upstream communities.get_communities: the stored
// community rows, filtered by size and ordered by the requested column, each
// carrying its member qualified names.
//
// An unrecognised sortBy silently falls back to "size" — the release runs the
// value through an f-string into ORDER BY, so rejecting it would be a stricter
// contract than the one callers were given.
func commGetCommunities(e *Engine, sortBy string, minSize int) ([]map[string]any, error) {
	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	if store == nil {
		// Never built: the release opens a fresh database here and reads an
		// empty communities table, so an empty list is the same answer.
		return nil, nil
	}
	rows, err := store.ReadCommunities()
	if err != nil {
		return nil, err
	}

	kept := make([]graphstore.CommunityRow, 0, len(rows))
	for _, row := range rows {
		if row.Size >= minSize {
			kept = append(kept, row)
		}
	}
	commSortRows(kept, sortBy)

	communities := make([]map[string]any, 0, len(kept))
	for _, row := range kept {
		members, err := store.ReadNodesByCommunity(row.ID)
		if err != nil {
			return nil, err
		}
		names := make([]any, 0, len(members))
		for _, member := range members {
			names = append(names, SanitizeName(member.QualifiedName))
		}
		communities = append(communities, map[string]any{
			"id":                row.ID,
			"name":              SanitizeName(row.Name),
			"level":             row.Level,
			"cohesion":          row.Cohesion,
			"size":              row.Size,
			"dominant_language": row.DominantLanguage,
			"description":       SanitizeName(row.Description),
			"members":           names,
		})
	}
	return communities, nil
}

// commSortRows applies the release's ORDER BY: size and cohesion descending,
// name ascending, anything else treated as size.
//
// The sort is stable over primary-key order. Upstream's SQL leaves ties to
// SQLite's sorter, which does not promise an order; pinning ties to insertion
// order makes the native answer reproducible without contradicting the release
// on any input where the release itself is deterministic.
func commSortRows(rows []graphstore.CommunityRow, sortBy string) {
	switch sortBy {
	case "cohesion":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Cohesion > rows[j].Cohesion })
	case "name":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	default:
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Size > rows[j].Size })
	}
}

// commMemberDetails projects a community's member nodes for `include_members`.
func commMemberDetails(e *Engine, communityID int64) ([]any, error) {
	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return []any{}, nil
	}
	members, err := store.ReadNodesByCommunity(communityID)
	if err != nil {
		return nil, err
	}
	details := make([]any, 0, len(members))
	for _, member := range members {
		details = append(details, NodeToDict(member))
	}
	return details, nil
}

// commCapMembers returns a copy of a community whose member lists are bounded.
//
// `size` already carries the true member count, so the caller never loses the
// total; `<key>_total` and `members_truncated` mark exactly what was cut.
func commCapMembers(community map[string]any, maxMembers int) map[string]any {
	capped := make(map[string]any, len(community)+3)
	for key, value := range community {
		capped[key] = value
	}
	for _, key := range []string{"members", "member_details"} {
		rows, ok := capped[key].([]any)
		if !ok {
			continue
		}
		visible, total, truncated := Bounded(rows, maxMembers, commMaxMembers)
		capped[key] = visible
		if truncated {
			capped[key+"_total"] = total
			capped["members_truncated"] = true
		}
	}
	return capped
}

// ── communities.py: get_architecture_overview ────────────────────────────────

// commOverview is the un-bounded architecture overview: the three lists the
// release's `get_architecture_overview` returns, kept as a struct so the tool
// can measure the full payload for its context-savings estimate while shaping
// a smaller one for the response.
type commOverview struct {
	communities []map[string]any
	crossEdges  []any
	warnings    []any
}

// payload renders the overview as the dict the release estimates tokens over.
func (o commOverview) payload() map[string]any {
	communities := make([]any, 0, len(o.communities))
	for _, community := range o.communities {
		communities = append(communities, community)
	}
	return map[string]any{
		"communities":           communities,
		"cross_community_edges": o.crossEdges,
		"warnings":              o.warnings,
	}
}

// commArchitectureOverview counts coupling that crosses community boundaries
// and warns about the heaviest pairs.
func commArchitectureOverview(e *Engine) (commOverview, error) {
	communities, err := commGetCommunities(e, "size", 0)
	if err != nil {
		return commOverview{}, err
	}

	// nodeCommunity is keyed by the SANITIZED member names while the edges
	// below are looked up by their raw endpoints. That is upstream's
	// asymmetry, not an oversight: a symbol whose name carries control
	// characters is simply never matched, and "fixing" it here would report
	// coupling the release does not.
	nodeCommunity := map[string]int64{}
	names := map[int64]string{}
	for _, community := range communities {
		id, _ := community["id"].(int64)
		names[id], _ = community["name"].(string)
		members, _ := community["members"].([]any)
		for _, member := range members {
			if qualified, ok := member.(string); ok {
				nodeCommunity[qualified] = id
			}
		}
	}

	edges, err := commAllEdges(e)
	if err != nil {
		return commOverview{}, err
	}

	overview := commOverview{communities: communities, crossEdges: []any{}, warnings: []any{}}
	counts := map[[2]int64]int{}
	var pairOrder [][2]int64
	for _, edge := range edges {
		// Test code calling production code is the expected shape of a test
		// suite, not an architectural smell, so TESTED_BY never counts as
		// coupling.
		if edge.Kind == graphstore.EdgeKindTestedBy {
			continue
		}
		source, sourceKnown := nodeCommunity[edge.SourceQualified]
		target, targetKnown := nodeCommunity[edge.TargetQualified]
		if !sourceKnown || !targetKnown || source == target {
			continue
		}
		pair := [2]int64{source, target}
		if pair[0] > pair[1] {
			pair[0], pair[1] = pair[1], pair[0]
		}
		if _, seen := counts[pair]; !seen {
			pairOrder = append(pairOrder, pair)
		}
		counts[pair]++
		overview.crossEdges = append(overview.crossEdges, map[string]any{
			"source_community": source,
			"target_community": target,
			"edge_kind":        edge.Kind,
			"source":           SanitizeName(edge.SourceQualified),
			"target":           SanitizeName(edge.TargetQualified),
		})
	}

	for _, pair := range commByCountDescending(counts, pairOrder) {
		count := counts[pair]
		if count <= 10 {
			continue
		}
		first := commCommunityLabel(names, pair[0])
		second := commCommunityLabel(names, pair[1])
		// Coupling to a test-dominated community is the test suite again,
		// this time recognised by name because the nodes themselves may not
		// be marked.
		if commIsTestCommunity(first) || commIsTestCommunity(second) {
			continue
		}
		overview.warnings = append(overview.warnings, fmt.Sprintf(
			"High coupling (%d edges) between '%s' and '%s'", count, first, second))
	}
	return overview, nil
}

// commAllEdges reads every edge, tolerating a never-built graph.
func commAllEdges(e *Engine) ([]graphstore.GraphEdge, error) {
	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, nil
	}
	return store.ReadAllEdges()
}

// commMinimalOverview compresses the overview for `detail_level="minimal"`.
//
// The full overview embeds every community's member list and every individual
// cross-community edge, which upstream measured at >600KB on a medium repo.
// Minimal mode drops the member lists and collapses the edge list to one row
// per community pair with a count and the leading edge kinds — enough to spot
// a coupling smell, small enough to read.
func commMinimalOverview(full commOverview) (communities, crossPairs []any) {
	minimalFields := []string{"id", "name", "size", "cohesion", "dominant_language"}
	names := map[int64]string{}
	communities = make([]any, 0, len(full.communities))
	for _, community := range full.communities {
		reduced := make(map[string]any, len(minimalFields))
		for _, field := range minimalFields {
			if value, ok := community[field]; ok {
				reduced[field] = value
			}
		}
		if id, ok := reduced["id"].(int64); ok {
			names[id], _ = reduced["name"].(string)
		}
		communities = append(communities, reduced)
	}

	counts := map[[2]int64]int{}
	kinds := map[[2]int64]map[string]int{}
	kindOrder := map[[2]int64][]string{}
	var pairOrder [][2]int64
	for _, entry := range full.crossEdges {
		edge, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		source, _ := edge["source_community"].(int64)
		target, _ := edge["target_community"].(int64)
		// Canonical (low, high) ordering so A->B and B->A aggregate together.
		pair := [2]int64{source, target}
		if pair[0] > pair[1] {
			pair[0], pair[1] = pair[1], pair[0]
		}
		if _, seen := counts[pair]; !seen {
			pairOrder = append(pairOrder, pair)
			kinds[pair] = map[string]int{}
		}
		counts[pair]++
		kind, _ := edge["edge_kind"].(string)
		if _, seen := kinds[pair][kind]; !seen {
			kindOrder[pair] = append(kindOrder[pair], kind)
		}
		kinds[pair][kind]++
	}

	crossPairs = make([]any, 0, len(pairOrder))
	for _, pair := range commByCountDescending(counts, pairOrder) {
		top, _, _ := Bounded(commRankedKinds(kinds[pair], kindOrder[pair]), 3, 3)
		crossPairs = append(crossPairs, map[string]any{
			"source_community": commCommunityLabel(names, pair[0]),
			"target_community": commCommunityLabel(names, pair[1]),
			"edge_count":       counts[pair],
			"top_kinds":        top,
		})
	}
	return communities, crossPairs
}

// commCommunityLabel names a community id, falling back to the release's
// `community-<id>` placeholder when the id is not in the current partition.
func commCommunityLabel(names map[int64]string, id int64) string {
	if name, ok := names[id]; ok {
		return name
	}
	return fmt.Sprintf("community-%d", id)
}

// commByCountDescending orders keys by count, highest first, breaking ties by
// first-insertion order.
//
// That is Python's `Counter.most_common()`: a stable sort over insertion
// order, which is observable here because it decides both the warning order
// and the minimal overview's row order.
func commByCountDescending(counts map[[2]int64]int, order [][2]int64) [][2]int64 {
	ranked := make([][2]int64, len(order))
	copy(ranked, order)
	sort.SliceStable(ranked, func(i, j int) bool {
		return counts[ranked[i]] > counts[ranked[j]]
	})
	return ranked
}

// commRankedKinds is commByCountDescending for the edge-kind counter.
func commRankedKinds(counts map[string]int, order []string) []any {
	ranked := make([]string, len(order))
	copy(ranked, order)
	sort.SliceStable(ranked, func(i, j int) bool {
		return counts[ranked[i]] > counts[ranked[j]]
	})
	out := make([]any, 0, len(ranked))
	for _, kind := range ranked {
		out = append(out, kind)
	}
	return out
}

// commTestCommunityPattern is upstream's `_TEST_COMMUNITY_RE`, verbatim.
// Coupling between test and production code is expected, so a pair naming a
// test-dominated community is not reported as an architectural smell.
var commTestCommunityPattern = regexp.MustCompile(
	`(?i)(^test[-/]|[-/]test([:/]|$)|it:should|describe:|spec[-/]|[-/]spec$)`)

// commIsTestCommunity reports whether a community NAME marks it as test code.
func commIsTestCommunity(name string) bool {
	return commTestCommunityPattern.MatchString(name)
}
