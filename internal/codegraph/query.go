package codegraph

import (
	"fmt"
	"sort"
	"time"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
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

// The crg derivation seams. Production always binds the real, parity-verified
// adapter derivations; they are indirected only so tests can exercise the
// store-readback error arms below, which the in-process namespaceView
// projection can never produce on its own.
var (
	flowsFromStore           = crg.FlowsFromStore
	flowMembershipsFromStore = crg.FlowMembershipsFromStore
	communitiesFromStore     = crg.CommunitiesFromStore
	riskIndexFromStore       = crg.RiskIndexFromStore
	postprocessFromStore     = crg.PostprocessFromStore
)

// snapshot loads the persisted graph for a read-only query, returning an empty
// snapshot when the graph has never been built. Every query path below degrades
// to an empty (but well-formed) result in that case rather than erroring — the
// same soft behaviour the bridge had when its database was missing.
func (e *Engine) snapshot() (graphSnapshot, error) {
	store, err := e.readStore()
	if err != nil || store == nil {
		return graphSnapshot{}, err
	}
	return readSnapshot(store)
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
	impact, err := store.GetImpactRadius(files, opts.MaxDepth, opts.MaxResults)
	if err != nil {
		return nil, err
	}
	return impactResult(files, impact), nil
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

// ListFlows returns the execution flows derived from the persisted CALLS graph
// via the crg adapter's flow derivation — the same code the §11.6 flows parity
// oracle compares, so the CLI and the parity gate can never diverge.
func (e *Engine) ListFlows(limit int, sortBy string) (*graphstore.FlowsResult, error) {
	snap, err := e.snapshot()
	if err != nil {
		return nil, err
	}
	flows, err := flowsFromStore(snap.view, crg.Name)
	if err != nil {
		return nil, err
	}
	sortFlows(flows, sortBy)
	if limit <= 0 {
		limit = defaultFlowLimit
	}
	if len(flows) > limit {
		flows = flows[:limit]
	}
	return &graphstore.FlowsResult{
		Status:  statusOK,
		Summary: fmt.Sprintf("%d flow(s)", len(flows)),
		Flows:   flowInfos(snap, flows),
	}, nil
}

// sortFlows orders flows by the requested key; criticality (descending) is the
// default, matching the bridge.
func sortFlows(flows []crg.Flow, sortBy string) {
	if sortBy == "" {
		sortBy = sortByCriticality
	}
	sort.SliceStable(flows, func(i, j int) bool {
		if sortBy == "name" || sortBy == "entry_point" {
			return flows[i].EntryPoint < flows[j].EntryPoint
		}
		if flows[i].Criticality != flows[j].Criticality {
			return flows[i].Criticality > flows[j].Criticality
		}
		return flows[i].ID < flows[j].ID
	})
}

// flowInfos projects derived flows onto the bridge's FlowInfo shape. Flow ids
// are positional: the derivation keys a flow by its entry-point symbol id
// (a string), while the wire shape carries an int64, so the index is the stable
// identifier within one response.
func flowInfos(snap graphSnapshot, flows []crg.Flow) []graphstore.FlowInfo {
	out := make([]graphstore.FlowInfo, 0, len(flows))
	for i, f := range flows {
		name := f.EntryPoint
		if node, ok := snap.nodeByID[f.ID]; ok && node.Name != "" {
			name = node.QualifiedName
		}
		out = append(out, graphstore.FlowInfo{
			ID:          int64(i + 1),
			Name:        name,
			EntryPoint:  f.EntryPoint,
			StepCount:   len(f.Members),
			Criticality: f.Criticality,
			Kind:        "call_flow",
		})
	}
	return out
}

// ── Communities (§11.1 row 6) ────────────────────────────────────────────────

// ListCommunities returns the code communities derived from the persisted
// dependency graph via the crg adapter's partition derivation.
func (e *Engine) ListCommunities(minSize int, sortBy string) (*graphstore.CommunitiesResult, error) {
	snap, err := e.snapshot()
	if err != nil {
		return nil, err
	}
	clusters, err := communitiesFromStore(snap.view, crg.Name)
	if err != nil {
		return nil, err
	}
	communities := communityInfos(snap, clusters, minSize)
	sortCommunities(communities, sortBy)
	return &graphstore.CommunitiesResult{
		Status:      statusOK,
		Summary:     fmt.Sprintf("%d community/communities", len(communities)),
		Communities: communities,
	}, nil
}

// communityInfos groups the cluster map into the bridge's CommunityInfo shape.
func communityInfos(snap graphSnapshot, clusters map[string]string, minSize int) []graphstore.CommunityInfo {
	members := map[string][]string{}
	for id, cluster := range clusters {
		members[cluster] = append(members[cluster], id)
	}
	reps := make([]string, 0, len(members))
	for rep := range members {
		reps = append(reps, rep)
	}
	sort.Strings(reps)

	internal := internalEdgeCounts(snap, clusters)
	out := make([]graphstore.CommunityInfo, 0, len(reps))
	for i, rep := range reps {
		ids := members[rep]
		if len(ids) < minSize {
			continue
		}
		sort.Strings(ids)
		out = append(out, communityInfo(snap, int64(i+1), rep, ids, internal[rep]))
	}
	return out
}

// communityInfo builds one community record. Cohesion is the share of a
// component's possible undirected pairs that are actually connected — a
// structural measure computed from storage, unlike the bridge's LLM-authored
// descriptions (see the documented delta in the consumer audit).
func communityInfo(snap graphSnapshot, id int64, rep string, ids []string, internalEdges int) graphstore.CommunityInfo {
	size := len(ids)
	cohesion := 0.0
	if pairs := size * (size - 1) / 2; pairs > 0 {
		cohesion = float64(internalEdges) / float64(pairs)
	}
	return graphstore.CommunityInfo{
		ID:               id,
		Name:             communityName(snap, rep),
		Size:             size,
		Cohesion:         cohesion,
		DominantLanguage: dominantLanguage(snap, ids),
		Members:          snap.qualifiedNames(ids),
	}
}

// communityName names a community after its representative symbol.
func communityName(snap graphSnapshot, rep string) string {
	if node, ok := snap.nodeByID[rep]; ok {
		return node.QualifiedName
	}
	return rep
}

// dominantLanguage returns the most common language among a community's members.
func dominantLanguage(snap graphSnapshot, ids []string) string {
	counts := map[string]int{}
	for _, id := range ids {
		if node, ok := snap.nodeByID[id]; ok && node.Language != "" {
			counts[node.Language]++
		}
	}
	best, bestCount := "", 0
	for lang, n := range counts {
		if n > bestCount || (n == bestCount && lang < best) {
			best, bestCount = lang, n
		}
	}
	return best
}

// internalEdgeCounts counts edges whose endpoints share a cluster.
func internalEdgeCounts(snap graphSnapshot, clusters map[string]string) map[string]int {
	counts := map[string]int{}
	for _, e := range snap.view.edges {
		if from, to := clusters[e.From], clusters[e.To]; from != "" && from == to {
			counts[from]++
		}
	}
	return counts
}

// sortCommunities orders communities by the requested key (size descending by
// default, matching the bridge).
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

// ── Postprocess (§11.1 row 7) ────────────────────────────────────────────────

// Postprocess recomputes the derived views and records their sizes as store
// metadata. The kg-native backend derives flows/communities/FTS on demand, so
// this is a verification-and-stamp pass rather than a materialization pass; the
// recorded counts are what `da kg code-status` consumers can assert on.
func (e *Engine) Postprocess(opts graphstore.PostprocessOptions) error {
	store, err := e.readStore()
	if err != nil {
		return err
	}
	if store == nil {
		return nil // nothing built yet — a no-op, not a failure
	}
	snap, err := readSnapshot(store)
	if err != nil {
		return err
	}
	post, err := postprocessFromStore(snap.view, crg.Name)
	if err != nil {
		return err
	}
	return storePostprocessMetadata(store, opts, post, e.now().UTC().Format(time.RFC3339))
}

// storePostprocessMetadata writes the per-view counts the operator asked for.
func storePostprocessMetadata(store graphstore.CodeGraphWriter, opts graphstore.PostprocessOptions, post crg.Postprocess, stamp string) error {
	writes := map[string]string{"last_postprocess": stamp}
	if !opts.NoFlows {
		writes["flow_memberships"] = fmt.Sprintf("%d", len(post.FlowMemberships))
	}
	if !opts.NoCommunities {
		writes["communities"] = fmt.Sprintf("%d", distinctValues(post.Communities))
	}
	if !opts.NoFTS {
		writes["fts_tokens"] = fmt.Sprintf("%d", len(post.FTS))
	}
	keys := make([]string, 0, len(writes))
	for k := range writes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := store.SetMetadata(k, writes[k]); err != nil {
			return err
		}
	}
	return nil
}

// distinctValues counts the distinct values of a map (here: cluster ids).
func distinctValues(m map[string]string) int {
	seen := map[string]bool{}
	for _, v := range m {
		seen[v] = true
	}
	return len(seen)
}

// ── Detect changes (§11.1 row 8) ─────────────────────────────────────────────

// DetectChanges returns the change-impact report for the current diff, built
// from the persisted graph: changed symbols with degree-centrality risk, the
// flows they participate in, symbols with no test edge, and the ranked review
// priorities.
func (e *Engine) DetectChanges(opts graphstore.DetectChangesOptions) (*graphstore.CRGChangeReport, error) {
	files, err := e.detectFiles(opts)
	if err != nil {
		return nil, err
	}
	snap, err := e.snapshot()
	if err != nil {
		return nil, err
	}
	changed := changedSymbols(snap, files)
	report := buildChangeReport(snap, changed)
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

// changedSymbols returns the crg note ids of symbols declared in the given
// files, in stable order.
func changedSymbols(snap graphSnapshot, files []string) []string {
	want := make(map[string]bool, len(files))
	for _, f := range files {
		want[f] = true
	}
	var ids []string
	for id, node := range snap.nodeByID {
		if want[node.FilePath] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// buildChangeReport assembles the full change-impact report.
func buildChangeReport(snap graphSnapshot, changed []string) *graphstore.CRGChangeReport {
	risk := riskScores(snap)
	report := &graphstore.CRGChangeReport{
		ChangedFunctions: changedNodeRows(snap, changed, risk),
		TestGaps:         testGaps(snap, changed),
		AffectedFlows:    affectedFlows(snap, changed),
	}
	report.ReviewPriorities = reviewPriorities(report.ChangedFunctions)
	report.RiskScore = maxRisk(report.ChangedFunctions)
	report.Summary = fmt.Sprintf(
		"%d changed symbol(s), %d affected flow(s), %d test gap(s); risk %.2f",
		len(report.ChangedFunctions), len(report.AffectedFlows), len(report.TestGaps), report.RiskScore)
	return report
}

// riskScores returns the normalized (0..1) degree-centrality risk per note id,
// computed by the crg adapter's risk-index derivation.
func riskScores(snap graphSnapshot) map[string]float64 {
	raw := riskIndex(snap)
	max := 0.0
	for _, v := range raw {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		return raw
	}
	out := make(map[string]float64, len(raw))
	for id, v := range raw {
		out[id] = v / max
	}
	return out
}

// riskIndex reads the derived risk index, tolerating an empty graph.
func riskIndex(snap graphSnapshot) map[string]float64 {
	idx, err := riskIndexFromStore(snap.view, crg.Name)
	if err != nil {
		return map[string]float64{}
	}
	return idx
}

// changedNodeRows projects changed symbols onto the report's node shape,
// carrying the caller count the bridge reported.
func changedNodeRows(snap graphSnapshot, changed []string, risk map[string]float64) []graphstore.CRGChangedNode {
	callers := callerCounts(snap)
	out := make([]graphstore.CRGChangedNode, 0, len(changed))
	for _, id := range changed {
		node := snap.nodeByID[id]
		out = append(out, graphstore.CRGChangedNode{
			Name:          node.Name,
			QualifiedName: node.QualifiedName,
			FilePath:      node.FilePath,
			RiskScore:     risk[id],
			Callers:       callers[id],
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RiskScore > out[j].RiskScore })
	return out
}

// callerCounts counts incoming edges per note id.
func callerCounts(snap graphSnapshot) map[string]int {
	counts := map[string]int{}
	for _, e := range snap.view.edges {
		counts[e.To]++
	}
	return counts
}

// testGaps returns the changed non-test symbols with no TESTED_BY edge.
func testGaps(snap graphSnapshot, changed []string) []graphstore.CRGTestGap {
	tested := map[string]bool{}
	for _, e := range snap.view.edges {
		if e.Type == edgeTestedBy {
			tested[e.From] = true
		}
	}
	var out []graphstore.CRGTestGap
	for _, id := range changed {
		node := snap.nodeByID[id]
		if node.IsTest || tested[id] {
			continue
		}
		out = append(out, graphstore.CRGTestGap{QualifiedName: node.QualifiedName, FilePath: node.FilePath})
	}
	return out
}

// affectedFlows returns the derived flows that contain at least one changed
// symbol.
func affectedFlows(snap graphSnapshot, changed []string) []graphstore.CRGFlow {
	rows, err := flowMembershipsFromStore(snap.view, crg.Name)
	if err != nil {
		return nil
	}
	changedSet := make(map[string]bool, len(changed))
	for _, id := range changed {
		changedSet[id] = true
	}
	hit := map[string]bool{}
	for _, row := range rows {
		if changedSet[row.MemberID] {
			hit[row.FlowID] = true
		}
	}
	ids := make([]string, 0, len(hit))
	for id := range hit {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]graphstore.CRGFlow, 0, len(ids))
	for i, id := range ids {
		out = append(out, graphstore.CRGFlow{
			ID:         int64(i + 1),
			EntryPoint: communityName(snap, id),
		})
	}
	return out
}

// reviewPriorities ranks the highest-risk changed symbols.
func reviewPriorities(changed []graphstore.CRGChangedNode) []graphstore.CRGPriority {
	limit := reviewPriorityLimit
	if len(changed) < limit {
		limit = len(changed)
	}
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
	max := 0.0
	for _, n := range changed {
		if n.RiskScore > max {
			max = n.RiskScore
		}
	}
	return max
}
