package crgbehavior

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Surface names — one per review-relevant query the gate replays.
const (
	SurfaceChangedNodes = "changed_nodes"
	SurfaceImpactRadius = "impact_radius"
	SurfaceFlows        = "flows"
	SurfaceFlowOrder    = "flow_order"
	SurfaceCommunities  = "communities"
	SurfaceRiskIndex    = "risk_index"
	SurfaceFTS          = "fts"
)

// advisoryReasons lists the surfaces that are REPORTED but do not fail the gate
// today, each with the measured reason it cannot be strict yet. §11.4 sign-off
// flips them to gating with Config.Strict (no code change) once the underlying
// derivation difference is resolved or accepted.
var advisoryReasons = map[string]string{
	SurfaceImpactRadius: "the legacy bridge resolves CALLS targets by bare symbol name at query time " +
		"(most stored call edges have an unresolved target), so its blast radius includes " +
		"name collisions a storage-id traversal cannot reproduce",
	SurfaceCommunities: "the legacy bridge's communities are file-scoped clusters; the kg-native " +
		"partition is connected components over CALLS+IMPORTS — different notions of community",
	SurfaceRiskIndex: "the legacy bridge's risk_score is a coverage/caller heuristic with few " +
		"distinct values; the kg-native risk_index is degree centrality",
	SurfaceFlowOrder: "the legacy flow_memberships table is keyed (flow_id, node_id) and numbers " +
		"its steps along the bridge's own path order; the kg-native positions follow a " +
		"deterministic sorted BFS, so step numbers may differ where membership agrees",
}

// maxDetailLines caps the per-surface structural diff so one systematically
// divergent surface cannot bury the rest of the report.
const maxDetailLines = 10

// noteFieldFilePath is the CRG adapter's file-path note field, read back to map
// a review task's changed files onto persisted symbol ids.
const noteFieldFilePath = "file_path"

// DefaultDepth is the impact-radius hop budget the review skills query at.
const DefaultDepth = 2

// DefaultMaxResults bounds the legacy bridge's impact query.
const DefaultMaxResults = 2000

// BridgeImpact is the legacy bridge's answer to one review task's impact-radius
// query, normalized into the kg-native id space.
type BridgeImpact struct {
	// ChangedIDs are the symbols the bridge resolved for the changed files.
	ChangedIDs []string
	// ImpactedIDs are the symbols the bridge reported as blast radius.
	ImpactedIDs []string
	// Truncated reports that the bridge capped its own result set.
	Truncated bool
}

// ImpactQuerier is the live legacy query surface the gate drives once per
// review task. The production implementation shells out to the Python CRG
// (live.go); tests inject a recorded double.
type ImpactQuerier interface {
	ImpactRadius(changedFiles []string, maxDepth, maxResults int) (BridgeImpact, error)
}

// Config parameterizes a gate run.
type Config struct {
	// RepoRoot is the repository the corpus and bridge graph belong to.
	RepoRoot string
	// Manifest is the pinned review-task corpus.
	Manifest Manifest
	// Depth is the impact-radius hop budget (DefaultDepth when zero).
	Depth int
	// MaxResults bounds the bridge's impact query (DefaultMaxResults when zero).
	MaxResults int
	// MaxTasks caps how many corpus tasks run (all when zero).
	MaxTasks int
	// Strict promotes the advisory surfaces to gating — the §11.4 sign-off flip.
	Strict bool
	// SpearmanTau is the risk_index rank-correlation floor
	// (graphstore.DefaultSpearmanTau when zero).
	SpearmanTau float64
}

// nativeSide is the kg-native adapter's state for a gate run: the persisted
// namespace plus the derived views computed from its readback.
type nativeSide struct {
	store    crg.StoreReader
	fileByID map[string]string
	postproc crg.Postprocess
}

// Run executes the behavior-preservation gate: for every pinned review task it
// drives the review-relevant queries against BOTH the legacy bridge state and
// the kg-native adapter, and applies the §11.6 structural oracles. The native
// side is driven through the adapter/Store API directly (crg.Bootstrap plus the
// *FromStore readback surfaces), so the gate is independent of which backend
// the `da kg` commands are currently wired to.
func Run(cfg Config, views BridgeViews, impact ImpactQuerier) (Report, error) {
	cfg = cfg.withDefaults()
	if err := checkNormalized(views); err != nil {
		return Report{}, err
	}
	native, err := bootstrapNative(sdk.NewMemStore(), views, cfg.Manifest.Head)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		RepoRoot:      cfg.RepoRoot,
		Head:          cfg.Manifest.Head,
		Strict:        cfg.Strict,
		GraphSymbols:  len(views.Symbols),
		GraphEdges:    len(views.References),
		GraphFiles:    views.FilesIndexed,
		NativeSymbols: len(native.fileByID),
	}
	for _, task := range cfg.tasks() {
		tr, err := evaluateTask(cfg, task, views, native, impact)
		if err != nil {
			return Report{}, err
		}
		report.Tasks = append(report.Tasks, tr)
	}
	return report, nil
}

// checkNormalized fails fast when the bridge graph's paths did not normalize to
// the repo-relative form the corpus pins its queries in — the symptom of a
// graph built under a different absolute root than the one being compared. A
// silent mismatch would resolve zero symbols per task and read as a total
// behavior divergence rather than a configuration error.
func checkNormalized(views BridgeViews) error {
	if len(views.Symbols) == 0 {
		return fmt.Errorf("crgbehavior: the bridge graph holds no symbols to compare")
	}
	for _, s := range views.Symbols {
		if !filepath.IsAbs(s.FilePath) {
			return nil
		}
	}
	return fmt.Errorf("crgbehavior: every bridge symbol path is still absolute (e.g. %q) — "+
		"the graph was built under a different root than the repository being compared",
		views.Symbols[0].FilePath)
}

// withDefaults fills the unset knobs.
func (c Config) withDefaults() Config {
	if c.Depth <= 0 {
		c.Depth = DefaultDepth
	}
	if c.MaxResults <= 0 {
		c.MaxResults = DefaultMaxResults
	}
	if c.SpearmanTau <= 0 {
		c.SpearmanTau = graphstore.DefaultSpearmanTau
	}
	return c
}

// tasks returns the corpus tasks this run executes, honoring MaxTasks.
func (c Config) tasks() []Task {
	if c.MaxTasks > 0 && c.MaxTasks < len(c.Manifest.Tasks) {
		return c.Manifest.Tasks[:c.MaxTasks]
	}
	return c.Manifest.Tasks
}

// bootstrapNative ingests the bridge's symbol graph through the kg-native
// adapter and computes its derived views from the STORE READBACK — never from
// the ingestion input, so a dropped write is visible to the comparison.
func bootstrapNative(store sdk.Store, views BridgeViews, commit string) (nativeSide, error) {
	s := sdk.For(crg.Name, store)
	if _, err := crg.Bootstrap(s, store, views.Corpus(commit), nil); err != nil {
		return nativeSide{}, fmt.Errorf("crgbehavior: native bootstrap: %w", err)
	}
	notes, err := store.Notes(sdk.OwnReadToken(crg.Name, "behavior-gate"), crg.Name)
	if err != nil {
		return nativeSide{}, fmt.Errorf("crgbehavior: native readback: %w", err)
	}
	fileByID := make(map[string]string, len(notes))
	for _, n := range notes {
		path, _ := n.Fields[noteFieldFilePath].(string)
		fileByID[n.ID] = path
	}
	pp, err := crg.PostprocessFromStore(store, crg.Name)
	if err != nil {
		return nativeSide{}, fmt.Errorf("crgbehavior: native derived views: %w", err)
	}
	return nativeSide{store: store, fileByID: fileByID, postproc: pp}, nil
}

// evaluateTask replays one review task against both sides.
func evaluateTask(cfg Config, task Task, views BridgeViews, native nativeSide, impact ImpactQuerier) (TaskReport, error) {
	tr := TaskReport{Commit: task.Commit, Subject: task.Subject, ChangedFiles: task.ChangedFiles}
	bridge, err := impact.ImpactRadius(task.ChangedFiles, cfg.Depth, cfg.MaxResults)
	if err != nil {
		return TaskReport{}, fmt.Errorf("crgbehavior: bridge impact query for %s: %w", short(task.Commit), err)
	}
	seeds := native.seedsFor(task.ChangedFiles)
	if len(seeds) == 0 && len(bridge.ChangedIDs) == 0 {
		tr.Skipped = true
		tr.SkipReason = "no symbol in either graph for the changed files (commit outside the built graph)"
		return tr, nil
	}
	tr.Surfaces = taskSurfaces(cfg, task, views, native, seeds, bridge)
	return tr, nil
}

// taskSurfaces runs every review-relevant query comparison for one task.
func taskSurfaces(cfg Config, task Task, views BridgeViews, native nativeSide, seeds []string, bridge BridgeImpact) []Surface {
	return []Surface{
		changedNodesSurface(seeds, bridge),
		impactSurface(cfg, native, seeds, bridge),
		flowsSurface(native, views, seeds),
		flowOrderSurface(native, views, seeds),
		communitiesSurface(native, views, seeds),
		riskSurface(cfg, native, views, seeds),
		ftsSurface(native, views, task.Identifiers),
	}
}

// seedsFor resolves the changed files to native symbol ids, from the PERSISTED
// namespace readback (not the ingestion corpus).
func (n nativeSide) seedsFor(files []string) []string {
	want := map[string]bool{}
	for _, f := range files {
		want[f] = true
	}
	var out []string
	for id, path := range n.fileByID {
		if want[path] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// changedNodesSurface compares which symbols each side resolved for the changed
// files — the seed set every downstream review query starts from.
func changedNodesSurface(seeds []string, bridge BridgeImpact) Surface {
	s := surfaceFrom(SurfaceChangedNodes, compareIDs(SurfaceChangedNodes, seeds, bridge.ChangedIDs))
	s.Metric = fmt.Sprintf("native=%d bridge=%d", len(seeds), len(bridge.ChangedIDs))
	return s
}

// impactSurface compares the blast radius each side reports for the changed
// files: the bridge's own query answer against the kg-native BFS over the
// persisted edge graph.
func impactSurface(cfg Config, native nativeSide, seeds []string, bridge BridgeImpact) Surface {
	rows, err := crg.ImpactRadiusFromStore(native.store, crg.Name, seeds, cfg.Depth)
	if err != nil {
		return failedSurface(SurfaceImpactRadius, err)
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.NodeID)
	}
	s := surfaceFrom(SurfaceImpactRadius, compareIDs(SurfaceImpactRadius, ids, bridge.ImpactedIDs))
	s.Metric = fmt.Sprintf("native=%d bridge=%d depth=%d truncated=%v",
		len(ids), len(bridge.ImpactedIDs), cfg.Depth, bridge.Truncated)
	return s
}

// flowsSurface compares the MEMBERSHIP of every flow the changed symbols
// participate in, under the §11.6 flow_memberships set-equality oracle. The
// rows are position-normalized first: the review question is "which flows does
// this change touch, and which symbols are in them" — step ordering is compared
// separately by flowOrderSurface, because the legacy flow_memberships table is
// keyed (flow_id, node_id) and numbers its steps along its own path order.
func flowsSurface(native nativeSide, views BridgeViews, seeds []string) Surface {
	nativeRows, bridgeRows, ok := touchedFlowRows(native, views, seeds)
	if !ok {
		return skippedSurface(SurfaceFlows, "no execution flow touches the changed symbols")
	}
	s := surfaceFrom(SurfaceFlows, crg.CompareFlowMemberships(withoutPositions(nativeRows), withoutPositions(bridgeRows)))
	s.Metric = fmt.Sprintf("native=%d members bridge=%d members", len(nativeRows), len(bridgeRows))
	return s
}

// flowOrderSurface compares the step ORDER of the touched flows under the same
// oracle with positions kept.
func flowOrderSurface(native nativeSide, views BridgeViews, seeds []string) Surface {
	nativeRows, bridgeRows, ok := touchedFlowRows(native, views, seeds)
	if !ok {
		return skippedSurface(SurfaceFlowOrder, "no execution flow touches the changed symbols")
	}
	s := surfaceFrom(SurfaceFlowOrder, crg.CompareFlowMemberships(nativeRows, bridgeRows))
	s.Metric = fmt.Sprintf("native=%d rows bridge=%d rows (position-sensitive)", len(nativeRows), len(bridgeRows))
	return s
}

// touchedFlowRows returns both sides' membership rows for the flows the changed
// symbols participate in. ok is false when neither side has such a flow.
func touchedFlowRows(native nativeSide, views BridgeViews, seeds []string) (nativeRows, bridgeRows []crg.FlowMembership, ok bool) {
	seedSet := setOf(seeds)
	nativeRows = flowsTouching(native.postproc.FlowMemberships, seedSet)
	bridgeRows = flowsTouching(views.FlowMemberships, seedSet)
	return nativeRows, bridgeRows, len(nativeRows) > 0 || len(bridgeRows) > 0
}

// withoutPositions projects membership rows onto (flow_id, member_id) so the
// oracle compares membership rather than step numbering.
func withoutPositions(rows []crg.FlowMembership) []crg.FlowMembership {
	out := make([]crg.FlowMembership, 0, len(rows))
	for _, r := range rows {
		out = append(out, crg.FlowMembership{FlowID: r.FlowID, MemberID: r.MemberID})
	}
	return out
}

// flowsTouching returns every membership row of every flow that contains at
// least one changed symbol — the "flows touched by this review" query.
func flowsTouching(rows []crg.FlowMembership, seeds map[string]bool) []crg.FlowMembership {
	touched := map[string]bool{}
	for _, r := range rows {
		if seeds[r.MemberID] {
			touched[r.FlowID] = true
		}
	}
	var out []crg.FlowMembership
	for _, r := range rows {
		if touched[r.FlowID] {
			out = append(out, r)
		}
	}
	return out
}

// communitiesSurface compares the community membership of the changed symbols
// under the parity gate's partition-equivalence oracle (cluster ids may differ;
// only the co-membership relation is compared).
func communitiesSurface(native nativeSide, views BridgeViews, seeds []string) Surface {
	a := restrictStrings(native.postproc.Communities, seeds)
	b := restrictStrings(views.Communities, seeds)
	if len(a) < 2 || len(b) < 2 {
		return skippedSurface(SurfaceCommunities, "fewer than two changed symbols carry a community assignment")
	}
	agree, ok := graphstore.PartitionAgreement(a, b)
	rep := graphstore.ParityReport{Row: SurfaceCommunities, Pass: ok && agree == 1.0}
	if !rep.Pass {
		rep.Detail = append(rep.Detail, partitionDetail(a, b, agree, ok))
	}
	s := surfaceFrom(SurfaceCommunities, rep)
	s.Metric = fmt.Sprintf("agreement=%.3f (want 1.000) over %d changed symbols", agree, len(a))
	return s
}

// partitionDetail explains a community divergence in review terms: how the two
// sides cluster the changed symbols.
func partitionDetail(a, b map[string]string, agree float64, ok bool) string {
	if !ok {
		return "the two sides do not cover the same changed-symbol set"
	}
	return fmt.Sprintf("pairwise co-membership agreement %.3f: native groups the changed symbols into %d cluster(s), bridge into %d",
		agree, distinctValues(a), distinctValues(b))
}

// riskSurface compares the risk ranking of the changed symbols under the
// parity gate's Spearman oracle.
func riskSurface(cfg Config, native nativeSide, views BridgeViews, seeds []string) Surface {
	// The bridge does not score every node it stores, so the ranking is
	// compared over the changed symbols BOTH sides score; the coverage gap is
	// reported rather than silently treated as agreement.
	scored := scoredBySides(native.postproc.RiskIndex, views.RiskIndex, seeds)
	a := restrictFloats(native.postproc.RiskIndex, scored)
	b := restrictFloats(views.RiskIndex, scored)
	if len(scored) < 2 {
		return skippedSurface(SurfaceRiskIndex, "fewer than two changed symbols are scored by both sides")
	}
	tau, ok := graphstore.SpearmanTau(a, b)
	rep := graphstore.ParityReport{Row: SurfaceRiskIndex, Pass: ok && tau >= cfg.SpearmanTau}
	if !rep.Pass {
		rep.Detail = append(rep.Detail, fmt.Sprintf(
			"rank correlation %.3f over %d changed symbols (floor %.2f, same-key-set=%v)", tau, len(a), cfg.SpearmanTau, ok))
	}
	s := surfaceFrom(SurfaceRiskIndex, rep)
	s.Metric = fmt.Sprintf("spearman=%.3f (floor %.2f) over %d of %d changed symbols scored by both sides",
		tau, cfg.SpearmanTau, len(scored), len(seeds))
	return s
}

// scoredBySides returns the changed symbols both risk indexes score.
func scoredBySides(a, b map[string]float64, seeds []string) []string {
	var out []string
	for _, id := range seeds {
		if _, inA := a[id]; !inA {
			continue
		}
		if _, inB := b[id]; inB {
			out = append(out, id)
		}
	}
	return out
}

// ftsSurface compares the searchable token set each side exposes for the
// declaration identifiers this commit added or removed.
func ftsSurface(native nativeSide, views BridgeViews, identifiers []string) Surface {
	if len(identifiers) == 0 {
		return skippedSurface(SurfaceFTS, "the commit changed no declaration identifier")
	}
	nativeTokens := tokensFor(native.postproc.FTS, identifiers)
	bridgeTokens := tokensFor(views.FTS, identifiers)
	if len(nativeTokens) == 0 && len(bridgeTokens) == 0 {
		return skippedSurface(SurfaceFTS, "no indexed symbol matches the changed identifiers")
	}
	s := surfaceFrom(SurfaceFTS, crg.CompareFTS(nativeTokens, bridgeTokens))
	s.Metric = fmt.Sprintf("native=%d tokens bridge=%d tokens for %d identifier(s)",
		len(nativeTokens), len(bridgeTokens), len(identifiers))
	return s
}

// tokensFor is the FTS query: the indexed tokens whose symbol name is one of
// the changed identifiers. Bridge and native tokens share the
// "<file>::<name>" qualified-name spelling after normalization.
func tokensFor(tokens, identifiers []string) []string {
	want := setOf(identifiers)
	var out []string
	for _, tok := range tokens {
		if want[symbolNameOf(tok)] {
			out = append(out, tok)
		}
	}
	sort.Strings(out)
	return out
}

// symbolNameOf returns the bare symbol name of a qualified-name token.
func symbolNameOf(token string) string {
	if i := strings.LastIndex(token, "::"); i >= 0 {
		return token[i+2:]
	}
	return token
}

// compareIDs applies the parity gate's impact-radius set-equality oracle to two
// id sets and relabels the report for the surface under comparison. A = the
// kg-native side, B = the legacy bridge.
func compareIDs(row string, native, bridge []string) graphstore.ParityReport {
	rep := graphstore.CompareImpactRadius(impactRowsOf(native), impactRowsOf(bridge))
	rep.Row = row
	return rep
}

// impactRowsOf lifts bare ids into the oracle's row shape.
func impactRowsOf(ids []string) []graphstore.ImpactRow {
	rows := make([]graphstore.ImpactRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, graphstore.ImpactRow{NodeID: id})
	}
	return rows
}

// surfaceFrom turns an oracle verdict into a reported surface, classifying it
// as gating or advisory and capping its structural diff.
func surfaceFrom(name string, rep graphstore.ParityReport) Surface {
	reason, advisory := advisoryReasons[name]
	return Surface{
		Name:           name,
		Advisory:       advisory,
		AdvisoryReason: reason,
		Pass:           rep.Pass,
		Detail:         capDetail(readableDetail(rep.Detail)),
	}
}

// detailReplacer makes an oracle's raw diagnostics readable in the gate report:
// the oracles name their two inputs "a"/"A" and "b"/"B", and the
// flow_memberships oracle keys rows with a NUL separator. The gate's reader is
// deciding a decommission, so the report names the SIDES and prints no control
// characters.
var detailReplacer = strings.NewReplacer(
	"only in a", "only in NATIVE",
	"only in b", "only in BRIDGE",
	"only in A", "only in NATIVE",
	"only in B", "only in BRIDGE",
	"\x00", " | ",
)

// readableDetail applies detailReplacer to every diagnostic line.
func readableDetail(detail []string) []string {
	out := make([]string, 0, len(detail))
	for _, d := range detail {
		out = append(out, detailReplacer.Replace(d))
	}
	return out
}

// failedSurface reports a surface that could not be computed at all.
func failedSurface(name string, err error) Surface {
	return surfaceFrom(name, graphstore.ParityReport{Row: name, Pass: false, Detail: []string{err.Error()}})
}

// skippedSurface reports a surface this task cannot exercise.
func skippedSurface(name, reason string) Surface {
	_, advisory := advisoryReasons[name]
	return Surface{Name: name, Advisory: advisory, Pass: true, Skipped: true, SkipReason: reason}
}

// capDetail bounds a structural diff, keeping the report readable.
func capDetail(detail []string) []string {
	sort.Strings(detail)
	if len(detail) <= maxDetailLines {
		return detail
	}
	out := append([]string{}, detail[:maxDetailLines]...)
	return append(out, fmt.Sprintf("... and %d more difference(s)", len(detail)-maxDetailLines))
}

// restrictStrings narrows a partition map to the given ids present in it.
func restrictStrings(m map[string]string, ids []string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if v, ok := m[id]; ok {
			out[id] = v
		}
	}
	return out
}

// restrictFloats narrows a score map to the given ids present in it.
func restrictFloats(m map[string]float64, ids []string) map[string]float64 {
	out := map[string]float64{}
	for _, id := range ids {
		if v, ok := m[id]; ok {
			out[id] = v
		}
	}
	return out
}

// distinctValues counts the distinct cluster ids in a partition.
func distinctValues(m map[string]string) int {
	seen := map[string]bool{}
	for _, v := range m {
		seen[v] = true
	}
	return len(seen)
}

// setOf builds a lookup set.
func setOf(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[x] = true
	}
	return out
}

// short abbreviates a commit SHA for messages.
func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
