package crgbehavior

import (
	"errors"
	"fmt"
	"sort"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// Config parameterizes a gate run.
type Config struct {
	// RepoRoot is the repository whose history the corpus is pinned from.
	RepoRoot string
	// Manifest is the pinned review-task corpus.
	Manifest Manifest
	// Release is the pinned release capability fixture.
	Release Release
	// Contract is the corpus contract: which surfaces a run must exercise.
	Contract Contract
	// FixtureDir holds the recorded release-pinned upstream behavior fixtures.
	FixtureDir string
	// Depth is the impact-radius hop budget (DefaultDepth when zero).
	Depth int
	// MaxResults bounds the release's impact query (DefaultMaxResults when zero).
	MaxResults int
	// MaxTasks caps how many corpus tasks run (all when zero). A capped run is
	// recorded as capped: the contract's surface coverage is judged over the
	// tasks that actually ran, so trimming the corpus cannot buy a pass.
	MaxTasks int
}

// withDefaults fills the unset knobs.
func (c Config) withDefaults() Config {
	if c.Depth <= 0 {
		c.Depth = DefaultDepth
	}
	if c.MaxResults <= 0 {
		c.MaxResults = DefaultMaxResults
	}
	if c.FixtureDir == "" {
		c.FixtureDir = DefaultFixtureDir
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

// Run executes the behavior-preservation gate.
//
// For every pinned review task it materializes BOTH sides at that task's OWN
// commit, then replays the review-relevant queries against the pinned release's
// persisted state and against the kg-native adapter's derivations of the same
// views, under EXACT oracles. It finishes by judging the run against the corpus
// contract, so a run that never exercised a required surface fails instead of
// reporting the same verdict as one that compared everything.
func Run(cfg Config, mat Materializer) (Report, error) {
	cfg = cfg.withDefaults()
	report := Report{
		RepoRoot:            cfg.RepoRoot,
		Head:                cfg.Manifest.Head,
		GeneratedFrom:       cfg.Manifest.GeneratedFrom,
		PinnedVersion:       PinnedVersion,
		PinnedSchemaVersion: PinnedSchemaVersion,
		CorpusTasks:         len(cfg.Manifest.Tasks),
	}
	for _, task := range cfg.tasks() {
		tr, err := evaluateTask(cfg, mat, task)
		if err != nil {
			if isFatal(err) {
				return Report{}, err
			}
			tr = TaskReport{Commit: task.Commit, Subject: task.Subject,
				ChangedFiles: task.ChangedFiles, Identifiers: task.Identifiers,
				Languages: task.Languages, Failure: err.Error()}
		}
		if report.Release.Version == "" {
			report.Release = tr.Release
		}
		report.Tasks = append(report.Tasks, tr)
	}
	report.Coverage = cfg.Contract.Coverage(report.Tasks)
	return report, nil
}

// isFatal reports whether an error invalidates the WHOLE run rather than one
// task: an absent bridge, an off-release bridge, or an incompatible graph
// schema. None of them is a behavior fact, and none of them is a pass.
func isFatal(err error) bool {
	return errors.Is(err, ErrBridgeUnavailable) ||
		errors.Is(err, ErrReleaseMismatch) ||
		errors.Is(err, ErrSchemaIncompatible)
}

// RecordFixtures materializes every corpus task at its own commit and records
// the pinned release's behavior as this corpus's conformance baseline. It is an
// explicit command, never a side effect of a gate run: a gate that silently
// re-recorded its own baseline could never detect upstream drift.
func RecordFixtures(cfg Config, mat Materializer) ([]UpstreamFixture, error) {
	cfg = cfg.withDefaults()
	out := make([]UpstreamFixture, 0, len(cfg.tasks()))
	for _, task := range cfg.tasks() {
		state, err := mat.Materialize(task)
		if err != nil {
			return nil, err
		}
		fixture := NewFixture(state.Release, task.Commit, bridgeRows(state, task), state.Lifecycle)
		if err := fixture.Save(cfg.FixtureDir); err != nil {
			return nil, err
		}
		out = append(out, fixture)
	}
	return out, nil
}

// evaluateTask materializes one task at its own SHA and compares both sides.
func evaluateTask(cfg Config, mat Materializer, task Task) (TaskReport, error) {
	state, err := mat.Materialize(task)
	if err != nil {
		return TaskReport{}, err
	}
	native, err := bootstrapNative(sdk.NewMemStore(), state.Views, task.Commit)
	if err != nil {
		return TaskReport{}, err
	}
	tr := TaskReport{
		Commit:        task.Commit,
		Subject:       task.Subject,
		ChangedFiles:  task.ChangedFiles,
		Identifiers:   task.Identifiers,
		Languages:     task.Languages,
		Release:       state.Release,
		Schema:        state.Schema,
		GraphSymbols:  len(state.Views.Symbols),
		GraphEdges:    len(state.Views.Edges),
		GraphFiles:    state.Views.FilesIndexed,
		NativeSymbols: len(native.fileByID),
	}
	tr.Surfaces = taskSurfaces(cfg, task, state, native)
	return tr, nil
}

// taskSurfaces runs every comparison for one task and attributes each surface a
// table could not back to the exact capability that disabled it.
func taskSurfaces(cfg Config, task Task, state TaskState, native nativeSide) []Surface {
	seeds := native.seedsFor(task.ChangedFiles)
	surfaces := []Surface{
		conformanceSurface(cfg, task, state),
		compareRows(SurfaceChangedNodes, idRows(seeds), idRows(state.Impact.ChangedIDs)),
		impactSurface(cfg, native, seeds, state.Impact),
	}
	surfaces = append(surfaces, flowSurfaces(state, native, seeds)...)
	surfaces = append(surfaces, communitySurfaces(state, native, seeds)...)
	surfaces = append(surfaces, riskSurfaces(state, native, seeds)...)
	surfaces = append(surfaces, ftsSurfaces(task, state, native)...)
	surfaces = append(surfaces,
		edgeConfidenceSurface(task, state),
		state.Lifecycle.Surface())
	return attributeUncomputed(surfaces, state.Schema)
}

// attributeUncomputed replaces any surface whose backing table the capability
// probe found missing or empty with an explicit not-exercised verdict naming
// the detected release. An unexercised surface must never read as agreement.
func attributeUncomputed(surfaces []Surface, schema SchemaReport) []Surface {
	uncomputed := schema.UncomputedSurfaces()
	for i, s := range surfaces {
		if reason, ok := uncomputed[s.Name]; ok {
			surfaces[i] = notExercised(s.Name, reason)
		}
	}
	return surfaces
}

// conformanceSurface compares the live bridge's behavior at this commit against
// the recorded release-pinned baseline.
func conformanceSurface(cfg Config, task Task, state TaskState) Surface {
	fixture, err := LoadFixture(cfg.FixtureDir, task.Commit)
	if err != nil {
		return failed(SurfaceUpstreamFixture, err)
	}
	return fixture.Conform(bridgeRows(state, task))
}

// bridgeRows renders every release-side surface of one materialized task into
// its canonical row form — the same rendering the live comparison and the
// recorded fixture both use, so conformance is judged by the one exact oracle.
func bridgeRows(state TaskState, task Task) map[string][]string {
	ids := taskSymbolIDs(state, task)
	touched := flowsTouching(state.Views.Flows, setOf(ids))
	risk := restrictScores(state.Views.RiskIndex, ids)
	return map[string][]string{
		SurfaceFlows:              flowRows(touched),
		SurfaceFlowMetrics:        flowMetricRows(touched),
		SurfaceFlowSnapshots:      flowSnapshotRows(snapshotsOf(state.Views, touched)),
		SurfaceCommunities:        communityRows(restrictPartition(state.Views.Communities, ids)),
		SurfaceCommunitySummaries: communitySummaryRows(summariesOf(state.Views, ids)),
		SurfaceRiskIndex:          riskScoreRows(bridgeScores(risk)),
		SurfaceRiskDetail:         riskDetailRows(risk),
		SurfaceFTSIndex:           ftsIndexOf(state.Views, task.ChangedFiles),
		SurfaceFTSSearch:          ftsSearchRows(state.FTS),
		SurfaceEdgeConfidence:     edgeConfidenceRows(edgesOf(state.Views, task.ChangedFiles)),
	}
}

// taskSymbolIDs is the release's OWN answer for which symbols a task's changed
// files hold. Keying the release-side rows off the release's answer (rather
// than off the native seed set) keeps the recorded fixture a pure statement
// about upstream behavior, independent of the adapter under test.
func taskSymbolIDs(state TaskState, task Task) []string {
	if len(state.Impact.ChangedIDs) > 0 {
		return state.Impact.ChangedIDs
	}
	// The impact query resolved nothing: fall back to the persisted graph's own
	// file → symbol mapping so the release-side rows still describe the task.
	want := setOf(task.ChangedFiles)
	var out []string
	for _, sym := range state.Views.Symbols {
		if want[sym.FilePath] {
			out = append(out, symbolIDOf(sym.QualifiedName, sym.FilePath))
		}
	}
	sort.Strings(out)
	return out
}

// impactSurface compares the blast radius each side reports.
func impactSurface(cfg Config, native nativeSide, seeds []string, bridge BridgeImpact) Surface {
	ids, err := native.impact(seeds, cfg.Depth)
	if err != nil {
		return failed(SurfaceImpactRadius, err)
	}
	s := compareRows(SurfaceImpactRadius, idRows(ids), idRows(bridge.ImpactedIDs))
	s.Metric = fmt.Sprintf("%s depth=%d bridge_truncated=%v", s.Metric, cfg.Depth, bridge.Truncated)
	return s
}

// flowSurfaces compares the flows the changed symbols participate in.
func flowSurfaces(state TaskState, native nativeSide, seeds []string) []Surface {
	seedSet := setOf(seeds)
	bridgeFlows := flowsTouching(state.Views.Flows, seedSet)
	nativeFlows := flowsTouching(native.flowRows(), seedSet)
	if len(bridgeFlows) == 0 && len(nativeFlows) == 0 {
		reason := "no execution flow touches the changed symbols"
		return []Surface{
			notExercised(SurfaceFlows, reason),
			notExercised(SurfaceFlowMetrics, reason),
			notExercised(SurfaceFlowSnapshots, reason),
		}
	}
	snapshots := snapshotsOf(state.Views, bridgeFlows)
	metrics := unimplemented(SurfaceFlowMetrics, flowMetricRows(bridgeFlows))
	snapshotSurface := unimplemented(SurfaceFlowSnapshots, flowSnapshotRows(snapshots))
	if len(snapshots) == 0 {
		snapshotSurface = notExercised(SurfaceFlowSnapshots,
			"the release recorded no flow_snapshots row for the flows this change touches")
	}
	return []Surface{
		compareRows(SurfaceFlows, flowRows(nativeFlows), flowRows(bridgeFlows)),
		metrics,
		snapshotSurface,
	}
}

// communitySurfaces compares the community partition and the release's
// community summaries over the changed symbols.
func communitySurfaces(state TaskState, native nativeSide, seeds []string) []Surface {
	bridgePartition := restrictPartition(state.Views.Communities, seeds)
	if len(bridgePartition) == 0 {
		reason := "no changed symbol carries a community assignment in the release graph"
		return []Surface{
			notExercised(SurfaceCommunities, reason),
			notExercised(SurfaceCommunitySummaries, reason),
		}
	}
	summaries := summariesOf(state.Views, seeds)
	summarySurface := unimplemented(SurfaceCommunitySummaries, communitySummaryRows(summaries))
	if len(summaries) == 0 {
		summarySurface = notExercised(SurfaceCommunitySummaries,
			"the release recorded no community_summaries row for the clusters this change touches")
	}
	return []Surface{
		compareRows(SurfaceCommunities,
			communityRows(native.communities(seeds)), communityRows(bridgePartition)),
		summarySurface,
	}
}

// riskSurfaces compares the release's risk_index over the changed symbols.
func riskSurfaces(state TaskState, native nativeSide, seeds []string) []Surface {
	bridgeRisk := restrictScores(state.Views.RiskIndex, seeds)
	if len(bridgeRisk) == 0 {
		reason := "the release scored none of the changed symbols in risk_index"
		return []Surface{
			notExercised(SurfaceRiskIndex, reason),
			notExercised(SurfaceRiskDetail, reason),
		}
	}
	return []Surface{
		compareRows(SurfaceRiskIndex,
			riskScoreRows(native.risk(seeds)), riskScoreRows(bridgeScores(bridgeRisk))),
		unimplemented(SurfaceRiskDetail, riskDetailRows(bridgeRisk)),
	}
}

// ftsSurfaces compares the release's search index content and its own search
// RESULTS for the declaration identifiers this commit changed.
func ftsSurfaces(task Task, state TaskState, native nativeSide) []Surface {
	index := compareRows(SurfaceFTSIndex,
		native.ftsIndex(task.ChangedFiles), ftsIndexOf(state.Views, task.ChangedFiles))
	if len(task.Identifiers) == 0 {
		return []Surface{index, notExercised(SurfaceFTSSearch,
			"the commit changed no declaration identifier in a language the release's extractor covers")}
	}
	return []Surface{index, unimplemented(SurfaceFTSSearch, ftsSearchRows(state.FTS))}
}

// edgeConfidenceSurface compares the schema-v9 edge confidence columns for the
// edges this change touches.
func edgeConfidenceSurface(task Task, state TaskState) Surface {
	edges := edgesOf(state.Views, task.ChangedFiles)
	if len(edges) == 0 {
		return notExercised(SurfaceEdgeConfidence,
			"the release stored no edge in the changed files")
	}
	return unimplemented(SurfaceEdgeConfidence, edgeConfidenceRows(edges))
}

// flowsTouching returns every flow that contains at least one changed symbol —
// the "flows touched by this review" query.
func flowsTouching(flows []BridgeFlow, seeds map[string]bool) []BridgeFlow {
	var out []BridgeFlow
	for _, f := range flows {
		for _, member := range f.Path {
			if seeds[member] {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// snapshotsOf returns the release's snapshot rows for a set of flows.
func snapshotsOf(views BridgeViews, flows []BridgeFlow) []BridgeFlowSnapshot {
	want := map[string]bool{}
	for _, f := range flows {
		want[f.EntryPoint] = true
	}
	var out []BridgeFlowSnapshot
	for _, s := range views.FlowSnapshots {
		if want[s.EntryPoint] {
			out = append(out, s)
		}
	}
	return out
}

// summariesOf returns the release's community summaries for the clusters the
// changed symbols belong to.
func summariesOf(views BridgeViews, ids []string) []BridgeCommunitySummary {
	want := map[string]bool{}
	for _, id := range ids {
		if cluster, ok := views.Communities[id]; ok && cluster != unassignedCluster {
			want[cluster] = true
		}
	}
	var out []BridgeCommunitySummary
	for _, s := range views.CommunitySummaries {
		if want[s.Cluster] {
			out = append(out, s)
		}
	}
	return out
}

// ftsIndexOf returns the release's index content for a task's changed files.
func ftsIndexOf(views BridgeViews, files []string) []string {
	want := setOf(files)
	var out []string
	for _, token := range views.FTSIndex {
		if want[filePartOf(token)] {
			out = append(out, token)
		}
	}
	sort.Strings(out)
	return out
}

// edgesOf returns the release's edges stored against a task's changed files.
func edgesOf(views BridgeViews, files []string) []BridgeEdge {
	want := setOf(files)
	var out []BridgeEdge
	for _, e := range views.Edges {
		if want[e.FilePath] {
			out = append(out, e)
		}
	}
	return out
}
