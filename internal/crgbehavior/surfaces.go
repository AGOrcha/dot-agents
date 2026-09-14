package crgbehavior

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Surface names — one per observable behavior the pinned release produces and a
// review consumer depends on. Every one is compared with an EXACT oracle: the
// release's output for a given graph is deterministic, so a similarity score
// (rank correlation, partition agreement, token-set overlap) would accept
// outputs a consumer can tell apart.
const (
	// SurfaceChangedNodes — which symbols a task's changed files resolve to.
	SurfaceChangedNodes = "changed_nodes"
	// SurfaceImpactRadius — the blast radius reported for those files.
	SurfaceImpactRadius = "impact_radius"
	// SurfaceFlows — flow identity and ordered path (release `flows` rows).
	SurfaceFlows = "flows"
	// SurfaceFlowMetrics — per-flow depth, node/file counts and the release's
	// weighted 0..1 criticality.
	SurfaceFlowMetrics = "flow_metrics"
	// SurfaceFlowSnapshots — the release's `flow_snapshots` rows, including the
	// v2.3.8 qualified-name critical path.
	SurfaceFlowSnapshots = "flow_snapshots"
	// SurfaceCommunities — the community partition over the changed symbols.
	SurfaceCommunities = "communities"
	// SurfaceCommunitySummaries — the release's `community_summaries` rows.
	SurfaceCommunitySummaries = "community_summaries"
	// SurfaceRiskIndex — the release's `risk_index.risk_score`.
	SurfaceRiskIndex = "risk_index"
	// SurfaceRiskDetail — the rest of the release's risk row: caller_count,
	// test_coverage and security_relevant.
	SurfaceRiskDetail = "risk_detail"
	// SurfaceFTSIndex — the release's `nodes_fts` index content.
	SurfaceFTSIndex = "fts_index"
	// SurfaceFTSSearch — the release's own FTS5 MATCH results.
	SurfaceFTSSearch = "fts_search"
	// SurfaceEdgeConfidence — the schema-v9 edge confidence columns.
	SurfaceEdgeConfidence = "edge_confidence"
	// SurfaceLifecycle — the release's build/postprocess staleness contract.
	SurfaceLifecycle = "lifecycle"
	// SurfaceUpstreamFixture — the live bridge against the recorded,
	// release-pinned upstream behavior fixture for this commit.
	SurfaceUpstreamFixture = "upstream_conformance"
)

// AllSurfaces lists every surface the gate can compare, in report order.
func AllSurfaces() []string {
	return []string{
		SurfaceUpstreamFixture,
		SurfaceChangedNodes,
		SurfaceImpactRadius,
		SurfaceFlows,
		SurfaceFlowMetrics,
		SurfaceFlowSnapshots,
		SurfaceCommunities,
		SurfaceCommunitySummaries,
		SurfaceRiskIndex,
		SurfaceRiskDetail,
		SurfaceFTSIndex,
		SurfaceFTSSearch,
		SurfaceEdgeConfidence,
		SurfaceLifecycle,
	}
}

// SurfaceStatus is a surface's outcome for one task. The four values are
// distinct on purpose: "we compared and they matched", "we compared and they
// differed", "we could not compare", and "the comparison itself blew up" are
// four different facts, and only the first is evidence of preserved behavior.
type SurfaceStatus string

const (
	StatusAgree        SurfaceStatus = "agree"
	StatusDiverge      SurfaceStatus = "diverge"
	StatusNotExercised SurfaceStatus = "not_exercised"
	StatusFailed       SurfaceStatus = "failed"
)

// maxDetailLines caps a per-surface structural diff so one systematically
// divergent surface cannot bury the rest of the report.
const maxDetailLines = 10

// floatFormat is the fixed precision every derived score is rendered at before
// comparison. The release rounds its criticality to four decimals and composes
// risk from exact tenths, so six decimals compares the values the release
// actually produced without admitting binary float noise.
const floatFormat = "%.6f"

// agree reports a surface whose oracle matched.
func agree(name, metric string) Surface {
	return Surface{Name: name, Status: StatusAgree, Metric: metric}
}

// diverge reports a surface whose oracle disagreed.
func diverge(name, metric string, detail []string) Surface {
	return Surface{Name: name, Status: StatusDiverge, Metric: metric, Detail: capDetail(detail)}
}

// notExercised reports a surface this task could not compare, with the measured
// reason. It is never counted as agreement.
func notExercised(name, reason string) Surface {
	return Surface{Name: name, Status: StatusNotExercised, Reason: reason}
}

// failed reports a surface whose comparison could not be computed at all.
func failed(name string, err error) Surface {
	return Surface{Name: name, Status: StatusFailed, Reason: err.Error()}
}

// unimplemented reports a release surface the kg-native adapter cannot answer.
// The release's own rows are shown so the report states exactly what would have
// to be implemented.
func unimplemented(name string, bridgeRows []string) Surface {
	return Surface{
		Name:   name,
		Status: StatusDiverge,
		Metric: fmt.Sprintf("native=unimplemented bridge=%d row(s)", len(bridgeRows)),
		Reason: ErrNativeUnimplemented.Error(),
		Detail: capDetail(prefixed("only in BRIDGE: ", bridgeRows)),
	}
}

// compareRows is the gate's single exact oracle: two row sets agree iff their
// canonical row renderings are equal as SETS. Every surface reduces to it, so
// there is exactly one place where "equal" is defined and no surface can
// quietly acquire a softer notion of equality.
func compareRows(name string, native, bridge []string) Surface {
	metric := fmt.Sprintf("native=%d bridge=%d row(s)", len(native), len(bridge))
	onlyNative, onlyBridge := symmetricDifference(native, bridge)
	if len(onlyNative) == 0 && len(onlyBridge) == 0 {
		return agree(name, metric)
	}
	detail := append(prefixed("only in NATIVE: ", onlyNative), prefixed("only in BRIDGE: ", onlyBridge)...)
	return diverge(name, metric, detail)
}

// symmetricDifference returns the rows unique to each side, sorted.
func symmetricDifference(a, b []string) (onlyA, onlyB []string) {
	inA, inB := setOf(a), setOf(b)
	for _, row := range a {
		if !inB[row] {
			onlyA = append(onlyA, row)
		}
	}
	for _, row := range b {
		if !inA[row] {
			onlyB = append(onlyB, row)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return dedupe(onlyA), dedupe(onlyB)
}

// dedupe removes adjacent duplicates from a sorted slice.
func dedupe(sorted []string) []string {
	out := sorted[:0]
	var prev string
	for i, v := range sorted {
		if i > 0 && v == prev {
			continue
		}
		out = append(out, v)
		prev = v
	}
	return out
}

// prefixed labels diff rows with the side they came from.
func prefixed(prefix string, rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, prefix+r)
	}
	return out
}

// capDetail bounds a structural diff, keeping the report readable.
func capDetail(detail []string) []string {
	if len(detail) <= maxDetailLines {
		return detail
	}
	out := append([]string{}, detail[:maxDetailLines]...)
	return append(out, fmt.Sprintf("... and %d more difference(s)", len(detail)-maxDetailLines))
}

// ── Canonical row renderings ─────────────────────────────────────────────────
//
// Each renderer folds one release row into a single deterministic string. Only
// genuinely nondeterministic upstream values are normalized away before this
// point (autoincrement flow/community ids re-keyed onto stable symbols,
// wall-clock timestamps not read at all); every remaining field is compared.

// idRows renders a bare id set.
func idRows(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

// flowRows renders flow identity plus ordered path.
func flowRows(flows []BridgeFlow) []string {
	out := make([]string, 0, len(flows))
	for _, f := range flows {
		out = append(out, "entry="+f.EntryPoint+" path="+strings.Join(f.Path, " > "))
	}
	sort.Strings(out)
	return out
}

// flowMetricRows renders the release's per-flow derived metrics.
func flowMetricRows(flows []BridgeFlow) []string {
	out := make([]string, 0, len(flows))
	for _, f := range flows {
		out = append(out, fmt.Sprintf("entry=%s depth=%d nodes=%d files=%d criticality="+floatFormat,
			f.EntryPoint, f.Depth, f.NodeCount, f.FileCount, f.Criticality))
	}
	sort.Strings(out)
	return out
}

// flowSnapshotRows renders the release's `flow_snapshots` rows.
func flowSnapshotRows(snapshots []BridgeFlowSnapshot) []string {
	out := make([]string, 0, len(snapshots))
	for _, s := range snapshots {
		out = append(out, fmt.Sprintf("entry=%s name=%s criticality="+floatFormat+" nodes=%d files=%d critical_path=%s",
			s.EntryPoint, s.Name, s.Criticality, s.NodeCount, s.FileCount, strings.Join(s.CriticalPath, " > ")))
	}
	sort.Strings(out)
	return out
}

// communityRows renders a symbol → canonical cluster assignment.
func communityRows(partition map[string]string) []string {
	out := make([]string, 0, len(partition))
	for id, cluster := range partition {
		out = append(out, id+" -> "+clusterLabel(cluster))
	}
	sort.Strings(out)
	return out
}

// clusterLabel names the unassigned cluster explicitly so an absent assignment
// is visible in a diff instead of rendering as an empty tail.
func clusterLabel(cluster string) string {
	if cluster == unassignedCluster {
		return "<unassigned>"
	}
	return cluster
}

// communitySummaryRows renders the release's `community_summaries` rows.
func communitySummaryRows(summaries []BridgeCommunitySummary) []string {
	out := make([]string, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, fmt.Sprintf("cluster=%s name=%s purpose=%s risk=%s size=%d language=%s key_symbols=%s",
			clusterLabel(s.Cluster), s.Name, s.Purpose, s.Risk, s.Size, s.DominantLanguage,
			strings.Join(s.KeySymbols, ",")))
	}
	sort.Strings(out)
	return out
}

// riskScoreRows renders id → risk_score.
func riskScoreRows(scores map[string]float64) []string {
	out := make([]string, 0, len(scores))
	for id, score := range scores {
		out = append(out, id+" risk_score="+strconv.FormatFloat(score, 'f', 6, 64))
	}
	sort.Strings(out)
	return out
}

// riskDetailRows renders the rest of the release's risk row.
func riskDetailRows(risks map[string]BridgeRisk) []string {
	out := make([]string, 0, len(risks))
	for id, r := range risks {
		out = append(out, fmt.Sprintf("%s callers=%d coverage=%s security_relevant=%t",
			id, r.CallerCount, r.TestCoverage, r.SecurityRelevant))
	}
	sort.Strings(out)
	return out
}

// ftsSearchRows renders one search term's result set.
func ftsSearchRows(hits map[string][]string) []string {
	out := make([]string, 0, len(hits))
	for term, matched := range hits {
		sorted := append([]string(nil), matched...)
		sort.Strings(sorted)
		out = append(out, "match("+term+") -> "+strings.Join(sorted, ", "))
	}
	sort.Strings(out)
	return out
}

// edgeConfidenceRows renders the schema-v9 edge confidence columns.
func edgeConfidenceRows(edges []BridgeEdge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, fmt.Sprintf("%s %s -> %s confidence="+floatFormat+" tier=%s",
			e.Kind, e.From, e.To, e.Confidence, e.ConfidenceTier))
	}
	sort.Strings(out)
	return out
}

// restrictScores narrows a risk map to the requested ids.
func restrictScores(risks map[string]BridgeRisk, ids []string) map[string]BridgeRisk {
	out := make(map[string]BridgeRisk, len(ids))
	for _, id := range ids {
		if r, ok := risks[id]; ok {
			out[id] = r
		}
	}
	return out
}

// bridgeScores projects a risk map onto id → risk_score.
func bridgeScores(risks map[string]BridgeRisk) map[string]float64 {
	out := make(map[string]float64, len(risks))
	for id, r := range risks {
		out[id] = r.RiskScore
	}
	return out
}

// restrictPartition narrows a partition to the requested ids.
func restrictPartition(partition map[string]string, ids []string) map[string]string {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		if cluster, ok := partition[id]; ok {
			out[id] = cluster
		}
	}
	return out
}
