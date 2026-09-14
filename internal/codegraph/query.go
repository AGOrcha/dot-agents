package codegraph

import (
	"fmt"
	"math"
	"sort"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// statusOK is the status field every structured query result carries, matching
// the bridge's JSON envelope.
const statusOK = "ok"

// defaultFlowLimit / defaultCommunitySort mirror the bridge's defaults so the
// CLI surface is unchanged.
const (
	defaultFlowLimit     = 20
	sortByCriticality    = "criticality"
	defaultCommunitySort = "size"
	reviewPriorityLimit  = 10
)

// derivedReader is the slice of the store the derived-view accessors below
// read. They read the PERSISTED tables the post-process pass wrote rather
// than recomputing a view on every call — the same thing upstream's MCP
// tools do, and the only way `list_flows` can report the flow ids
// `get_flow` resolves.
type derivedReader interface {
	graphstore.CodeGraphReader
	graphstore.CodeGraphDerived
}

// ── Impact radius (§11.1 row 4) ──────────────────────────────────────────────

// GetImpactRadius returns the blast radius of a changed-file set. Bounds are
// enforced by the store provider (the uniform bounds chokepoint), so a caller's
// depth/limit are a requested ceiling exactly as they are on the bridge path.
func (e *Engine) GetImpactRadius(opts graphstore.ImpactOptions) (*graphstore.CRGImpactResult, error) {
	files, err := e.impactSeedFiles(opts)
	if err != nil {
		return nil, err
	}
	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	result := &graphstore.CRGImpactResult{Status: statusOK, ChangedFiles: files}
	if store == nil {
		result.Summary = "Code graph not built; no impact computed."
		return result, nil
	}
	// The seeds are repo-relative (git's spelling); the graph keys files by
	// their absolute path, so they are resolved before the traversal and the
	// caller's own spelling is echoed back unchanged.
	impact, err := store.GetImpactRadius(e.graphPaths(files), opts.MaxDepth, opts.MaxResults)
	if err != nil {
		return nil, err
	}
	return impactResult(files, impact), nil
}

// graphPaths maps caller-supplied paths onto the spelling the graph stores.
func (e *Engine) graphPaths(files []string) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, e.absPath(file))
	}
	return out
}

// impactSeedFiles resolves the seed file set: the caller's explicit list, else
// the current git diff (the bridge's default).
func (e *Engine) impactSeedFiles(opts graphstore.ImpactOptions) ([]string, error) {
	if len(opts.ChangedFiles) > 0 {
		return opts.ChangedFiles, nil
	}
	return e.changedFiles(e.root, opts.Base)
}

// impactResult projects a store impact result into the bridge's response shape.
func impactResult(files []string, impact graphstore.ImpactResult) *graphstore.CRGImpactResult {
	changed := impactNodes(impact.ChangedNodes)
	impacted := impactNodes(impact.ImpactedNodes)
	return &graphstore.CRGImpactResult{
		Status:        statusOK,
		Summary:       fmt.Sprintf("%d changed symbol(s), %d impacted symbol(s) across %d file(s)", len(changed), len(impacted), len(impact.ImpactedFiles)),
		ChangedFiles:  files,
		ChangedNodes:  changed,
		ImpactedNodes: impacted,
		ImpactedFiles: impact.ImpactedFiles,
		TotalImpacted: len(impacted),
	}
}

// impactNodes converts store nodes to the impact-node wire shape.
func impactNodes(nodes []graphstore.GraphNode) []graphstore.ImpactNode {
	out := make([]graphstore.ImpactNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, graphstore.ImpactNode{
			ID: n.ID, Kind: n.Kind, Name: n.Name, QualifiedName: n.QualifiedName,
			FilePath: n.FilePath, LineStart: n.LineStart, LineEnd: n.LineEnd,
			Language: n.Language, IsTest: n.IsTest,
		})
	}
	return out
}

// ── Flows (§11.1 row 5) ──────────────────────────────────────────────────────

// ListFlows returns the execution flows the last post-process persisted.
func (e *Engine) ListFlows(limit int, sortBy string) (*graphstore.FlowsResult, error) {
	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	result := &graphstore.FlowsResult{Status: statusOK, Summary: "0 flow(s)"}
	if store == nil {
		return result, nil
	}
	rows, err := store.ReadFlows()
	if err != nil {
		return nil, err
	}
	entryPoints, err := flowEntryPointNames(store, rows)
	if err != nil {
		return nil, err
	}
	flows := flowInfos(rows, entryPoints)
	sortFlows(flows, sortBy)
	if limit <= 0 {
		limit = defaultFlowLimit
	}
	if len(flows) > limit {
		flows = flows[:limit]
	}
	result.Flows = flows
	result.Summary = fmt.Sprintf("%d flow(s)", len(flows))
	return result, nil
}

// flowEntryPointNames resolves each flow's entry-point node id to its
// qualified name in one batched read.
func flowEntryPointNames(store derivedReader, rows []graphstore.FlowRow) (map[int64]string, error) {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.EntryPointID)
	}
	nodes, err := store.ReadNodesByID(ids)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(nodes))
	for _, node := range nodes {
		names[node.ID] = node.QualifiedName
	}
	return names, nil
}

// flowInfos projects persisted flow rows onto the wire shape. The id is the
// row's own primary key, so it round-trips through a `get_flow` lookup.
func flowInfos(rows []graphstore.FlowRow, entryPoints map[int64]string) []graphstore.FlowInfo {
	out := make([]graphstore.FlowInfo, 0, len(rows))
	for _, row := range rows {
		entry := entryPoints[row.EntryPointID]
		if entry == "" {
			entry = row.Name
		}
		out = append(out, graphstore.FlowInfo{
			ID:          row.ID,
			Name:        row.Name,
			EntryPoint:  entry,
			StepCount:   row.NodeCount,
			Criticality: row.Criticality,
			Kind:        "call_flow",
		})
	}
	return out
}

// sortFlows orders flows by the requested key; criticality (descending) is
// the default, matching upstream.
func sortFlows(flows []graphstore.FlowInfo, sortBy string) {
	if sortBy == "" {
		sortBy = sortByCriticality
	}
	sort.SliceStable(flows, func(i, j int) bool {
		if sortBy == "name" || sortBy == "entry_point" {
			return flows[i].Name < flows[j].Name
		}
		if flows[i].Criticality != flows[j].Criticality {
			return flows[i].Criticality > flows[j].Criticality
		}
		return flows[i].ID < flows[j].ID
	})
}

// ── Communities (§11.1 row 6) ────────────────────────────────────────────────

// ListCommunities returns the code communities the last post-process
// persisted, with their member symbols.
func (e *Engine) ListCommunities(minSize int, sortBy string) (*graphstore.CommunitiesResult, error) {
	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	result := &graphstore.CommunitiesResult{Status: statusOK, Summary: "0 community/communities"}
	if store == nil {
		return result, nil
	}
	rows, err := store.ReadCommunities()
	if err != nil {
		return nil, err
	}
	communities := make([]graphstore.CommunityInfo, 0, len(rows))
	for _, row := range rows {
		if row.Size < minSize {
			continue
		}
		members, err := communityMembers(store, row.ID)
		if err != nil {
			return nil, err
		}
		communities = append(communities, graphstore.CommunityInfo{
			ID:               row.ID,
			Name:             row.Name,
			Size:             row.Size,
			Cohesion:         row.Cohesion,
			DominantLanguage: row.DominantLanguage,
			Description:      row.Description,
			Members:          members,
		})
	}
	sortCommunities(communities, sortBy)
	result.Communities = communities
	result.Summary = fmt.Sprintf("%d community/communities", len(communities))
	return result, nil
}

// communityMembers lists one community's member symbols by qualified name.
func communityMembers(store derivedReader, id int64) ([]string, error) {
	nodes, err := store.ReadNodesByCommunity(id)
	if err != nil {
		return nil, err
	}
	members := make([]string, 0, len(nodes))
	for _, node := range nodes {
		members = append(members, node.QualifiedName)
	}
	return members, nil
}

// sortCommunities orders communities by the requested key (size descending
// by default, matching upstream).
func sortCommunities(communities []graphstore.CommunityInfo, sortBy string) {
	if sortBy == "" {
		sortBy = defaultCommunitySort
	}
	sort.SliceStable(communities, func(i, j int) bool {
		if sortBy == "cohesion" {
			return communities[i].Cohesion > communities[j].Cohesion
		}
		if sortBy == "name" {
			return communities[i].Name < communities[j].Name
		}
		if communities[i].Size != communities[j].Size {
			return communities[i].Size > communities[j].Size
		}
		return communities[i].Name < communities[j].Name
	})
}

// ── Detect changes (§11.1 row 8) ─────────────────────────────────────────────

// DetectChanges returns the change-impact report for the changed-file set,
// built from the PERSISTED graph and its derived tables: the symbols those
// files declare, the risk_index score the last post-process computed for
// each, the flows they participate in, and the ones with no test edge.
//
// It is file-level by construction. Upstream attributes changed symbols from
// `git diff --unified=0` hunk boundaries, which is why the MCP
// detect_changes tool is routed to the retained bridge; this provider-level
// accessor answers the same question at file granularity for the CLI.
func (e *Engine) DetectChanges(opts graphstore.DetectChangesOptions) (*graphstore.CRGChangeReport, error) {
	target, release, err := e.rooted(opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	defer release()
	files, err := target.detectFiles(opts)
	if err != nil {
		return nil, err
	}
	store, err := target.readStore()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return &graphstore.CRGChangeReport{Summary: changeSummary(0, 0, 0, 0)}, nil
	}
	report, err := target.buildChangeReport(store, files)
	if err != nil {
		return nil, err
	}
	if opts.Brief {
		return &graphstore.CRGChangeReport{Summary: report.Summary}, nil
	}
	return report, nil
}

// detectFiles resolves the file set change detection runs over.
func (e *Engine) detectFiles(opts graphstore.DetectChangesOptions) ([]string, error) {
	if len(opts.Files) > 0 {
		return opts.Files, nil
	}
	return e.changedFiles(e.root, opts.Base)
}

// buildChangeReport assembles the full change-impact report.
func (e *Engine) buildChangeReport(store derivedReader, files []string) (*graphstore.CRGChangeReport, error) {
	changed, err := e.changedNodes(store, files)
	if err != nil {
		return nil, err
	}
	risk, err := riskByQualifiedName(store)
	if err != nil {
		return nil, err
	}
	edges, err := store.ReadAllEdges()
	if err != nil {
		return nil, err
	}
	report := &graphstore.CRGChangeReport{
		ChangedFunctions: changedNodeRows(changed, risk, callerCounts(edges)),
		TestGaps:         testGaps(changed, testedSymbols(edges)),
	}
	report.AffectedFlows, err = flowsContainingChangedSymbols(store, changed)
	if err != nil {
		return nil, err
	}
	report.ReviewPriorities = reviewPriorities(report.ChangedFunctions)
	report.RiskScore = maxRisk(report.ChangedFunctions)
	report.Summary = changeSummary(
		len(report.ChangedFunctions), len(report.AffectedFlows), len(report.TestGaps), report.RiskScore)
	return report, nil
}

// changeSummary renders the report's one-line headline.
func changeSummary(symbols, flows, gaps int, risk float64) string {
	return fmt.Sprintf("%d changed symbol(s), %d affected flow(s), %d test gap(s); risk %.2f",
		symbols, flows, gaps, risk)
}

// changedNodes returns the non-File symbols the changed files declare, in
// stable qualified-name order.
func (e *Engine) changedNodes(store derivedReader, files []string) ([]graphstore.GraphNode, error) {
	var changed []graphstore.GraphNode
	seen := map[string]bool{}
	for _, file := range files {
		abs := e.absPath(file)
		if seen[abs] {
			continue
		}
		seen[abs] = true
		nodes, err := store.GetNodesByFile(abs)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if node.Kind != nodeKindFile {
				changed = append(changed, node)
			}
		}
	}
	sort.SliceStable(changed, func(i, j int) bool {
		return changed[i].QualifiedName < changed[j].QualifiedName
	})
	return changed, nil
}

// riskByQualifiedName reads the persisted risk_index into a lookup.
func riskByQualifiedName(store derivedReader) (map[string]float64, error) {
	rows, err := store.ReadRiskIndex()
	if err != nil {
		return nil, err
	}
	risk := make(map[string]float64, len(rows))
	for _, row := range rows {
		risk[row.QualifiedName] = row.RiskScore
	}
	return risk, nil
}

// changedNodeRows projects changed symbols onto the report's node shape,
// highest risk first.
func changedNodeRows(changed []graphstore.GraphNode, risk map[string]float64, callers map[string]int) []graphstore.CRGChangedNode {
	out := make([]graphstore.CRGChangedNode, 0, len(changed))
	for _, node := range changed {
		out = append(out, graphstore.CRGChangedNode{
			Name:          node.Name,
			QualifiedName: node.QualifiedName,
			FilePath:      node.FilePath,
			RiskScore:     risk[node.QualifiedName],
			Callers:       callers[node.QualifiedName],
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RiskScore > out[j].RiskScore })
	return out
}

// callerCounts counts the CALLS edges targeting each symbol.
func callerCounts(edges []graphstore.GraphEdge) map[string]int {
	counts := map[string]int{}
	for _, edge := range edges {
		if edge.Kind == graphstore.EdgeKindCalls {
			counts[edge.TargetQualified]++
		}
	}
	return counts
}

// testedSymbols is the set of symbols with at least one TESTED_BY edge.
func testedSymbols(edges []graphstore.GraphEdge) map[string]bool {
	tested := map[string]bool{}
	for _, edge := range edges {
		if edge.Kind == graphstore.EdgeKindTestedBy {
			tested[edge.SourceQualified] = true
		}
	}
	return tested
}

// testGaps returns the changed non-test symbols with no TESTED_BY edge.
func testGaps(changed []graphstore.GraphNode, tested map[string]bool) []graphstore.CRGTestGap {
	var out []graphstore.CRGTestGap
	for _, node := range changed {
		if node.IsTest || tested[node.QualifiedName] {
			continue
		}
		out = append(out, graphstore.CRGTestGap{
			QualifiedName: node.QualifiedName,
			FilePath:      node.FilePath,
		})
	}
	return out
}

// flowsContainingChangedSymbols returns the persisted flows that contain at
// least one of the changed symbols.
func flowsContainingChangedSymbols(store derivedReader, changed []graphstore.GraphNode) ([]graphstore.CRGFlow, error) {
	if len(changed) == 0 {
		return nil, nil
	}
	memberships, err := store.ReadFlowMemberships()
	if err != nil {
		return nil, err
	}
	changedIDs := make(map[int64]bool, len(changed))
	for _, node := range changed {
		changedIDs[node.ID] = true
	}
	hit := map[int64]bool{}
	for _, row := range memberships {
		if changedIDs[row.NodeID] {
			hit[row.FlowID] = true
		}
	}
	if len(hit) == 0 {
		return nil, nil
	}
	flows, err := store.ReadFlows()
	if err != nil {
		return nil, err
	}
	out := make([]graphstore.CRGFlow, 0, len(hit))
	for _, flow := range flows {
		if hit[flow.ID] {
			out = append(out, graphstore.CRGFlow{ID: flow.ID, EntryPoint: flow.Name})
		}
	}
	return out, nil
}

// reviewPriorities ranks the highest-risk changed symbols.
func reviewPriorities(changed []graphstore.CRGChangedNode) []graphstore.CRGPriority {
	limit := min(reviewPriorityLimit, len(changed))
	out := make([]graphstore.CRGPriority, 0, limit)
	for _, n := range changed[:limit] {
		out = append(out, graphstore.CRGPriority{
			QualifiedName: n.QualifiedName,
			Reason:        fmt.Sprintf("%d caller(s) in the persisted graph", n.Callers),
			RiskScore:     n.RiskScore,
		})
	}
	return out
}

// maxRisk is the report-level risk score: the highest changed-symbol risk.
func maxRisk(changed []graphstore.CRGChangedNode) float64 {
	highest := 0.0
	for _, n := range changed {
		highest = math.Max(highest, n.RiskScore)
	}
	return highest
}
