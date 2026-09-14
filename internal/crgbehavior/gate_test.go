package crgbehavior

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// fakeMaterializer stands in for the worktree materializer. It records which
// commits the gate asked for, which is how the same-SHA execution model is
// tested without building a real graph per commit.
type fakeMaterializer struct {
	states map[string]TaskState
	errs   map[string]error
	seen   []string
}

func (f *fakeMaterializer) Materialize(task Task) (TaskState, error) {
	f.seen = append(f.seen, task.Commit)
	if err := f.errs[task.Commit]; err != nil {
		return TaskState{}, err
	}
	state, ok := f.states[task.Commit]
	if !ok {
		return TaskState{}, fmt.Errorf("no staged state for %s", short(task.Commit))
	}
	state.Commit = task.Commit
	return state, nil
}

// goTask is the review task the fixture graph answers.
func goTask(commit string, files ...string) Task {
	if len(files) == 0 {
		files = []string{"pkg/a.go"}
	}
	return Task{Commit: commit, Subject: "seed commit", ChangedFiles: files,
		Identifiers: []string{"Entry"}, Languages: []string{"go"}}
}

// stateFor materializes a TaskState from a seeded release graph, filling the
// release's query answers the way a real run would.
func stateFor(t *testing.T, seed string, task Task) TaskState {
	t.Helper()
	store := openGraph(t, seed)
	views, err := store.Views()
	if err != nil {
		t.Fatalf("read views: %v", err)
	}
	fts := map[string][]string{}
	for _, term := range task.Identifiers {
		hits, err := store.SearchFTS(term)
		if err != nil {
			t.Fatalf("search %q: %v", term, err)
		}
		fts[term] = hits
	}
	changed := symbolsIn(views, task.ChangedFiles)
	return TaskState{
		Commit:  task.Commit,
		Release: store.Release(),
		Schema:  store.Schema(),
		Views:   views,
		Impact:  BridgeImpact{ChangedIDs: changed, ImpactedIDs: changed},
		FTS:     fts,
		Lifecycle: LifecycleObservation{
			Observed: true, SnapshotsConsistentAfterBuild: true, PostprocessRebuiltFlows: true,
			SnapshotsStaleAfterPostprocess: true, PostprocessStampedMetadata: true,
		},
	}
}

// symbolsIn is the release's answer for which symbols a changed file holds.
func symbolsIn(views BridgeViews, files []string) []string {
	want := setOf(files)
	var out []string
	for _, sym := range views.Symbols {
		if want[sym.FilePath] {
			out = append(out, symbolIDOf(sym.QualifiedName, sym.FilePath))
		}
	}
	return out
}

// gateFixture wires a one-task run with a recorded upstream baseline.
func gateFixture(t *testing.T, seed string, task Task) (Config, *fakeMaterializer) {
	t.Helper()
	state := stateFor(t, seed, task)
	dir := t.TempDir()
	if err := NewFixture(state.Release, task.Commit, bridgeRows(state, task), state.Lifecycle).Save(dir); err != nil {
		t.Fatalf("record fixture: %v", err)
	}
	cfg := Config{
		RepoRoot:   "/repo",
		Manifest:   Manifest{SchemaVersion: ManifestSchemaVersion, Head: task.Commit, Release: PinnedVersion, Tasks: []Task{task}},
		Release:    testRelease(),
		Contract:   Contract{SchemaVersion: ContractSchemaVersion, Release: PinnedVersion, RequiredSurfaces: AllSurfaces()},
		FixtureDir: dir,
	}
	return cfg, &fakeMaterializer{states: map[string]TaskState{task.Commit: state}}
}

// surfaceNamed returns one surface of a task report.
func surfaceNamed(t *testing.T, tr TaskReport, name string) Surface {
	t.Helper()
	for _, s := range tr.Surfaces {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("report has no %q surface (has %d)", name, len(tr.Surfaces))
	return Surface{}
}

// The corpus is historical, so each task must be materialized at ITS OWN
// commit. A run that asks for anything else is replaying history against the
// wrong tree and cannot detect a historical-output regression.
func TestRunMaterializesEveryTaskAtItsOwnCommit(t *testing.T) {
	first, second := goTask("1111111111111111"), goTask("2222222222222222")
	cfg, mat := gateFixture(t, twoFlowSeed, first)
	cfg.Manifest.Tasks = []Task{first, second}
	mat.states[second.Commit] = mat.states[first.Commit]
	if _, err := Run(cfg, mat); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{first.Commit, second.Commit}; !equalStrings(mat.seen, want) {
		t.Fatalf("materialized %v, want one materialization per pinned commit %v", mat.seen, want)
	}
}

// MaxTasks caps the run, and the cap is honored in commit order — a capped run
// must still be judged by the contract over what it actually executed.
func TestRunHonorsTheTaskCap(t *testing.T) {
	first, second := goTask("1111111111111111"), goTask("2222222222222222")
	cfg, mat := gateFixture(t, twoFlowSeed, first)
	cfg.Manifest.Tasks = []Task{first, second}
	cfg.MaxTasks = 1
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Tasks) != 1 || report.CorpusTasks != 2 {
		t.Fatalf("executed %d of %d, want 1 of 2", len(report.Tasks), report.CorpusTasks)
	}
	if mat.seen[len(mat.seen)-1] != first.Commit {
		t.Fatalf("materialized %v, want only the first pinned commit", mat.seen)
	}
}

// A release surface the kg-native adapter cannot answer AT ALL is the strongest
// negative answer for that surface. Reporting it as "not exercised" would hide
// a product gap behind an environment-shaped excuse.
func TestReleaseSurfacesWithoutANativeCounterpartDiverge(t *testing.T) {
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, twoFlowSeed, task)
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tr := report.Tasks[0]
	for _, name := range []string{
		SurfaceFlowMetrics, SurfaceFlowSnapshots, SurfaceCommunitySummaries,
		SurfaceRiskDetail, SurfaceFTSSearch, SurfaceEdgeConfidence,
	} {
		s := surfaceNamed(t, tr, name)
		if s.Status != StatusDiverge {
			t.Fatalf("%s = %s, want a divergence", name, s.Status)
		}
		if !strings.Contains(s.Reason, "no counterpart") {
			t.Fatalf("%s reason = %q, want the unimplemented explanation", name, s.Reason)
		}
		if len(s.Detail) == 0 {
			t.Fatalf("%s carries no release rows, so the report does not say what is missing", name)
		}
	}
	if report.Pass() {
		t.Fatal("a run with unimplemented release surfaces reported PASS")
	}
}

// The surfaces both sides can answer are compared exactly, and the fixture
// graph is built so they genuinely agree — otherwise this test could not tell
// an exact oracle from a broken one.
func TestSurfacesBothSidesImplementAgreeOnTheFixtureGraph(t *testing.T) {
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, twoFlowSeed, task)
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tr := report.Tasks[0]
	for _, name := range []string{
		SurfaceUpstreamFixture, SurfaceChangedNodes, SurfaceFlows,
		SurfaceCommunities, SurfaceFTSIndex, SurfaceLifecycle,
	} {
		if s := surfaceNamed(t, tr, name); s.Status != StatusAgree {
			t.Fatalf("%s = %s (%s) %v, want agreement", name, s.Status, s.Metric, s.Detail)
		}
	}
}

// risk_index is a deterministic function of the graph, so it is compared by
// exact equality. A rank-correlation oracle would accept these scores: the two
// sides rank the changed symbols identically and differ only in value.
func TestRiskIndexIsComparedExactlyNotByRank(t *testing.T) {
	// Native degree centrality scores Entry 2 and Step 1. Rank-agreeing but
	// numerically different release scores must still be a divergence.
	seed := twoFlowSeed + "\nUPDATE risk_index SET risk_score=0.9 WHERE node_id=1;" +
		"\nUPDATE risk_index SET risk_score=0.3 WHERE node_id=2;\n"
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, seed, task)
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s := surfaceNamed(t, report.Tasks[0], SurfaceRiskIndex)
	if s.Status != StatusDiverge {
		t.Fatalf("risk_index = %s, want a divergence on rank-agreeing but unequal scores", s.Status)
	}
}

// The community partition is compared by exact canonical-cluster equality. A
// partition-agreement score computed over the shared key set would call these
// two identical: the release leaves Widget unassigned while the native
// derivation places it in a cluster.
func TestCommunitiesAreComparedExactlyNotByAgreement(t *testing.T) {
	task := goTask("1111111111111111", "pkg/a.go", "pkg/b.go")
	cfg, mat := gateFixture(t, twoFlowSeed, task)
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s := surfaceNamed(t, report.Tasks[0], SurfaceCommunities)
	if s.Status != StatusDiverge {
		t.Fatalf("communities = %s, want a divergence when one side leaves a changed symbol unassigned", s.Status)
	}
	if !strings.Contains(strings.Join(s.Detail, "\n"), "<unassigned>") {
		t.Fatalf("the diff must name the unassigned symbol: %v", s.Detail)
	}
}

// An uncomputed optional view disables exactly the surfaces it backs, with the
// detected release named — and it is never reported as agreement.
func TestUncomputedViewsAreAttributedNotAssumed(t *testing.T) {
	seed := twoFlowSeed + "\nDELETE FROM flows;\nDELETE FROM flow_snapshots;\n"
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, seed, task)
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, name := range []string{SurfaceFlows, SurfaceFlowMetrics, SurfaceFlowSnapshots} {
		s := surfaceNamed(t, report.Tasks[0], name)
		if s.Status != StatusNotExercised {
			t.Fatalf("%s = %s, want not_exercised", name, s.Status)
		}
		if !strings.Contains(s.Reason, PinnedVersion) {
			t.Fatalf("%s reason %q does not name the detected release", name, s.Reason)
		}
	}
}

// A required surface no task exercised fails the run. Without this, a run that
// compared nothing reported the same verdict as one that compared everything.
func TestUnexercisedRequiredSurfaceFailsTheRun(t *testing.T) {
	seed := twoFlowSeed + "\nDELETE FROM flows;\nDELETE FROM flow_snapshots;\n"
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, seed, task)
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !containsString(report.UnsatisfiedSurfaces(), SurfaceFlows) {
		t.Fatalf("unsatisfied surfaces = %v, want flows among them", report.UnsatisfiedSurfaces())
	}
	if report.Verdict() != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL", report.Verdict())
	}
}

// A ratified, attributed exception is the ONLY way an unexercised required
// surface stops failing.
func TestRatifiedExceptionSatisfiesCoverage(t *testing.T) {
	seed := twoFlowSeed + "\nDELETE FROM flows;\nDELETE FROM flow_snapshots;\n"
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, seed, task)
	cfg.Contract.RatifiedExceptions = []RatifiedException{
		{Surface: SurfaceFlows, Reason: "this corpus pins no commit with a traced flow", RatifiedBy: "t6-bridge-decommission"},
	}
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if containsString(report.UnsatisfiedSurfaces(), SurfaceFlows) {
		t.Fatalf("a ratified surface is still unsatisfied: %v", report.UnsatisfiedSurfaces())
	}
}

// Without a recorded release-pinned baseline there is nothing proving the live
// bridge still behaves like the release the gate certifies against.
func TestMissingUpstreamFixtureFailsConformance(t *testing.T) {
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, twoFlowSeed, task)
	cfg.FixtureDir = t.TempDir() // no recording
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s := surfaceNamed(t, report.Tasks[0], SurfaceUpstreamFixture)
	if s.Status != StatusFailed || !strings.Contains(s.Reason, "-record") {
		t.Fatalf("conformance = %s %q, want a failure naming the recording command", s.Status, s.Reason)
	}
}

// Upstream drift must show up as a conformance divergence rather than silently
// redefining what "correct" means.
func TestUpstreamDriftFailsConformance(t *testing.T) {
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, twoFlowSeed, task)
	drifted := stateFor(t, twoFlowSeed+"\nUPDATE flows SET criticality=0.9 WHERE id=41;\n", task)
	drifted.Commit = task.Commit
	mat.states[task.Commit] = drifted
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s := surfaceNamed(t, report.Tasks[0], SurfaceUpstreamFixture); s.Status != StatusDiverge {
		t.Fatalf("conformance = %s, want a divergence against the recorded baseline", s.Status)
	}
}

// An absent, off-release or schema-incompatible bridge invalidates the WHOLE
// run. None of those is a behavior fact and none is a pass.
func TestRunPropagatesFatalBridgeErrors(t *testing.T) {
	for name, fatal := range map[string]error{
		"unavailable": ErrBridgeUnavailable,
		"off release": ErrReleaseMismatch,
		"bad schema":  ErrSchemaIncompatible,
	} {
		t.Run(name, func(t *testing.T) {
			task := goTask("1111111111111111")
			cfg, mat := gateFixture(t, twoFlowSeed, task)
			mat.errs = map[string]error{task.Commit: fmt.Errorf("probe: %w", fatal)}
			if _, err := Run(cfg, mat); !errors.Is(err, fatal) {
				t.Fatalf("Run error = %v, want %v", err, fatal)
			}
		})
	}
}

// A task-local failure is recorded against that task and fails the run; it does
// not silently shrink the corpus.
func TestRunRecordsATaskLocalFailure(t *testing.T) {
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, twoFlowSeed, task)
	mat.errs = map[string]error{task.Commit: errors.New("worktree build failed")}
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Tasks[0].Failure == "" {
		t.Fatal("a failed task recorded no failure")
	}
	if report.Verdict() != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL", report.Verdict())
	}
}

// A run that executed nothing produced no evidence. That is not a pass.
func TestEmptyRunIsInconclusive(t *testing.T) {
	report := Report{Coverage: []SurfaceCoverage{{Surface: SurfaceFlows, Satisfied: true}}}
	if report.Verdict() != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", report.Verdict())
	}
	if report.Pass() {
		t.Fatal("an empty run reported PASS")
	}
}

// equalStrings compares two string slices.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// grTFile is the changed file every hand-built state in these tests describes.
const grTFile = "pkg/a.go"

// grTQualified is the release's spelling of a symbol in grTFile.
func grTQualified(symbol string) string { return grTFile + qualifiedSep + symbol }

// grTRefusingStore is a kg-native store whose readback fails. It drives the
// comparison paths that cannot compute an answer at all, which must be reported
// as a failed surface rather than as an empty agreement.
type grTRefusingStore struct{}

func (grTRefusingStore) Notes(sdk.Token, string) ([]sdk.Note, error) {
	return nil, errors.New("readback refused")
}

func (grTRefusingStore) Edges(sdk.Token, string) ([]sdk.Edge, error) {
	return nil, errors.New("readback refused")
}

// grTNative builds a kg-native side with hand-supplied derivations, so one
// surface's oracle can be driven without materializing a whole graph.
func grTNative(post crg.Postprocess, flows []crg.Flow) nativeSide {
	return nativeSide{store: sdk.NewMemStore(), fileByID: map[string]string{}, flows: flows, post: post}
}

// grTConfig is a run config over the given corpus with NO recorded baseline on
// disk; it is for the paths that do not exercise upstream conformance.
func grTConfig(t *testing.T, tasks ...Task) Config {
	t.Helper()
	var head string
	if len(tasks) > 0 {
		head = tasks[0].Commit
	}
	return Config{
		RepoRoot: "/repo",
		Manifest: Manifest{SchemaVersion: ManifestSchemaVersion, Head: head,
			Release: PinnedVersion, Tasks: tasks},
		Release: testRelease(),
		Contract: Contract{SchemaVersion: ContractSchemaVersion, Release: PinnedVersion,
			RequiredSurfaces: AllSurfaces()},
		FixtureDir: t.TempDir(),
	}
}

// grTReleaseState is the smallest hand-built dual-read state the kg-native side
// can ingest, carrying the release identity the caller wants recorded.
func grTReleaseState(commit, version string) TaskState {
	return TaskState{
		Commit:  commit,
		Release: ReleaseInfo{Package: PackageName, Version: version, SchemaVersion: PinnedSchemaVersion},
	}
}

// grTWantNotExercised asserts a helper returned exactly the named surfaces, all
// not-exercised with the measured reason and no comparison output — an
// unexercised surface must never read as agreement.
func grTWantNotExercised(t *testing.T, got []Surface, reason string, names ...string) {
	t.Helper()
	if len(got) != len(names) {
		t.Fatalf("got %d surface(s) %+v, want exactly %v", len(got), got, names)
	}
	for i, name := range names {
		if got[i].Name != name || got[i].Status != StatusNotExercised {
			t.Fatalf("surface[%d] = %q %s, want %q not_exercised", i, got[i].Name, got[i].Status, name)
		}
		if got[i].Reason != reason {
			t.Fatalf("%s reason = %q, want %q", name, got[i].Reason, reason)
		}
		if got[i].Metric != "" || len(got[i].Detail) != 0 {
			t.Fatalf("%s carries comparison output it never computed: %+v", name, got[i])
		}
	}
}

// grTDiff renders a surface's structural diff for substring assertions.
func grTDiff(s Surface) string { return strings.Join(s.Detail, "\n") }

// An unset knob takes its default; an explicitly chosen one is never silently
// replaced. FixtureDir matters most: overwriting a caller's recording directory
// with the repo default would compare against the wrong baseline.
func TestGrTWithDefaultsFillsOnlyTheUnsetKnobs(t *testing.T) {
	cases := map[string]struct {
		in                Config
		depth, maxResults int
		fixtures          string
	}{
		"every zero knob gets its default": {
			in: Config{}, depth: DefaultDepth, maxResults: DefaultMaxResults, fixtures: DefaultFixtureDir,
		},
		"a nonsensical negative knob gets its default": {
			in: Config{Depth: -1, MaxResults: -1}, depth: DefaultDepth, maxResults: DefaultMaxResults,
			fixtures: DefaultFixtureDir,
		},
		"explicit knobs are left alone": {
			in:    Config{Depth: 7, MaxResults: 11, FixtureDir: "/recorded/elsewhere"},
			depth: 7, maxResults: 11, fixtures: "/recorded/elsewhere",
		},
		"an explicit fixture dir survives default depth": {
			in: Config{FixtureDir: "/recorded/elsewhere"}, depth: DefaultDepth,
			maxResults: DefaultMaxResults, fixtures: "/recorded/elsewhere",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.in.withDefaults()
			if got.Depth != tc.depth || got.MaxResults != tc.maxResults || got.FixtureDir != tc.fixtures {
				t.Fatalf("withDefaults() = depth %d, max_results %d, fixtures %q; want %d, %d, %q",
					got.Depth, got.MaxResults, got.FixtureDir, tc.depth, tc.maxResults, tc.fixtures)
			}
		})
	}
}

// MaxTasks trims the corpus from the FRONT and never grows it. A cap at or
// above the corpus size must not change the run, and it must never reorder the
// pinned tasks — the corpus order is what makes a capped run reproducible.
func TestGrTTasksHonorTheCapAtEveryBoundary(t *testing.T) {
	corpus := []Task{goTask("1111111111111111"), goTask("2222222222222222"), goTask("3333333333333333")}
	cases := map[string]struct {
		max  int
		want []string
	}{
		"no cap runs the whole corpus":       {0, []string{corpus[0].Commit, corpus[1].Commit, corpus[2].Commit}},
		"a negative cap is not a cap":        {-1, []string{corpus[0].Commit, corpus[1].Commit, corpus[2].Commit}},
		"a cap below the corpus trims it":    {2, []string{corpus[0].Commit, corpus[1].Commit}},
		"a cap at the corpus size keeps all": {3, []string{corpus[0].Commit, corpus[1].Commit, corpus[2].Commit}},
		"a cap above the corpus keeps all":   {5, []string{corpus[0].Commit, corpus[1].Commit, corpus[2].Commit}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := Config{Manifest: Manifest{Tasks: corpus}, MaxTasks: tc.max}.tasks()
			commits := make([]string, 0, len(got))
			for _, task := range got {
				commits = append(commits, task.Commit)
			}
			if !equalStrings(commits, tc.want) {
				t.Fatalf("tasks() = %v, want %v", commits, tc.want)
			}
		})
	}
}

// Only release plumbing invalidates the whole run. A task-local failure — a
// broken worktree, a missing baseline — must stay task-local, or one bad commit
// would silently discard every comparison the run already made.
func TestGrTIsFatalClassifiesOnlyReleasePlumbingErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"absent bridge":            {ErrBridgeUnavailable, true},
		"off-release bridge":       {ErrReleaseMismatch, true},
		"incompatible schema":      {ErrSchemaIncompatible, true},
		"wrapped sentinel":         {fmt.Errorf("probe graph.db: %w", ErrSchemaIncompatible), true},
		"doubly wrapped":           {fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", ErrBridgeUnavailable)), true},
		"a broken worktree":        {errors.New("worktree build failed"), false},
		"an unrecorded commit":     {ErrFixtureMissing, false},
		"an unimplemented surface": {ErrNativeUnimplemented, false},
		"no error":                 {nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := isFatal(tc.err); got != tc.want {
				t.Fatalf("isFatal(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// A task-local failure must not shrink the corpus: the later tasks still run,
// the failure is attributed to its own commit, and the run still reaches the
// contract verdict. Aborting here would let one broken commit turn a FAIL into
// an unjudged run.
func TestGrTRunContinuesPastANonFatalTaskFailure(t *testing.T) {
	first, broken, last := goTask("1111111111111111"), goTask("2222222222222222"), goTask("3333333333333333")
	cfg, mat := gateFixture(t, twoFlowSeed, first)
	cfg.Manifest.Tasks = []Task{first, broken, last}
	mat.states[last.Commit] = mat.states[first.Commit]
	mat.errs = map[string]error{broken.Commit: errors.New("worktree build failed")}
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !equalStrings(mat.seen, []string{first.Commit, broken.Commit, last.Commit}) {
		t.Fatalf("materialized %v, want every pinned commit still attempted", mat.seen)
	}
	failed := report.Tasks[1]
	if failed.Failure != "worktree build failed" || failed.Commit != broken.Commit {
		t.Fatalf("task[1] = %q on %s, want the failure attributed to the broken commit",
			failed.Failure, short(failed.Commit))
	}
	if !equalStrings(failed.ChangedFiles, broken.ChangedFiles) || len(failed.Surfaces) != 0 {
		t.Fatalf("a failed task carries %d surface(s) and files %v, want the task's own identity and no comparison",
			len(failed.Surfaces), failed.ChangedFiles)
	}
	if len(report.Tasks) != 3 || len(report.Tasks[2].Surfaces) == 0 {
		t.Fatalf("the run stopped at the failing task: %d task(s) reported", len(report.Tasks))
	}
	if len(report.Coverage) != len(cfg.Contract.RequiredSurfaces) {
		t.Fatalf("coverage has %d entry(ies), want the contract still judged over %d required surface(s)",
			len(report.Coverage), len(cfg.Contract.RequiredSurfaces))
	}
}

// A fatal release error invalidates every comparison the run made, so it must
// return NO report at all. Returning the partial tasks would let a caller
// render evidence produced against an unpinned or absent bridge.
func TestGrTRunAbortsWithAZeroReportOnAFatalError(t *testing.T) {
	good, doomed := goTask("1111111111111111"), goTask("2222222222222222")
	cfg, mat := gateFixture(t, twoFlowSeed, good)
	cfg.Manifest.Tasks = []Task{good, doomed}
	mat.errs = map[string]error{doomed.Commit: fmt.Errorf("probe: %w", ErrBridgeUnavailable)}
	report, err := Run(cfg, mat)
	if !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("Run error = %v, want %v", err, ErrBridgeUnavailable)
	}
	if !reflect.DeepEqual(report, Report{}) {
		t.Fatalf("an aborted run returned partial evidence: %+v", report)
	}
	if !equalStrings(mat.seen, []string{good.Commit, doomed.Commit}) {
		t.Fatalf("materialized %v, want the abort to happen at the doomed commit", mat.seen)
	}
}

// The report records the release the run was PRODUCED against, taken from the
// first task it materialized. A later task observing a different build must not
// silently relabel the whole run's evidence.
func TestGrTRunRecordsTheReleaseObservedAtTheFirstTask(t *testing.T) {
	first, second := goTask("1111111111111111"), goTask("2222222222222222")
	cfg := grTConfig(t, first, second)
	mat := &fakeMaterializer{states: map[string]TaskState{
		first.Commit:  grTReleaseState(first.Commit, PinnedVersion),
		second.Commit: grTReleaseState(second.Commit, "9.9.9-not-the-pinned-build"),
	}}
	report, err := Run(cfg, mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Release.Version != PinnedVersion {
		t.Fatalf("observed release = %q, want the first task's %q", report.Release.Version, PinnedVersion)
	}
	if report.Tasks[1].Release.Version != "9.9.9-not-the-pinned-build" {
		t.Fatalf("task[1] release = %q, want its own observation preserved", report.Tasks[1].Release.Version)
	}
}

// A release graph the kg-native adapter cannot ingest is a task failure, not a
// green task: comparing against a half-ingested namespace would report
// agreement on rows neither side ever produced.
func TestGrTRunRecordsANativeIngestionFailureAgainstTheTask(t *testing.T) {
	task := goTask("1111111111111111")
	state := grTReleaseState(task.Commit, PinnedVersion)
	state.Views = BridgeViews{
		Symbols: []crg.Symbol{{QualifiedName: grTQualified("Entry"), Kind: "Function", FilePath: grTFile}},
		// An edge the release stored with no kind cannot be written as a typed
		// native edge, so ingestion refuses the graph.
		Edges: []BridgeEdge{{From: grTQualified("Entry"), To: grTQualified("Entry"), FilePath: grTFile}},
	}
	mat := &fakeMaterializer{states: map[string]TaskState{task.Commit: state}}
	report, err := Run(grTConfig(t, task), mat)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tr := report.Tasks[0]
	if !strings.Contains(tr.Failure, "native bootstrap") {
		t.Fatalf("task failure = %q, want the native ingestion failure recorded", tr.Failure)
	}
	if len(tr.Surfaces) != 0 {
		t.Fatalf("a task whose graph could not be ingested still reported %d comparison(s)", len(tr.Surfaces))
	}
	if report.Verdict() != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL", report.Verdict())
	}
}

// Recording writes one baseline per corpus task, and what lands on disk must be
// exactly what the release did: it has to load back and conform to the state it
// was recorded from, or the baseline is not evidence about upstream behavior.
func TestGrTRecordFixturesWritesOneRoundTrippableBaselinePerTask(t *testing.T) {
	first, second := goTask("1111111111111111"), goTask("2222222222222222")
	cfg, mat := gateFixture(t, twoFlowSeed, first)
	cfg.Manifest.Tasks = []Task{first, second}
	cfg.FixtureDir = t.TempDir()
	mat.states[second.Commit] = mat.states[first.Commit]
	recorded, err := RecordFixtures(cfg, mat)
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if len(recorded) != 2 {
		t.Fatalf("recorded %d fixture(s), want one per corpus task", len(recorded))
	}
	for i, task := range []Task{first, second} {
		loaded, err := LoadFixture(cfg.FixtureDir, task.Commit)
		if err != nil {
			t.Fatalf("LoadFixture(%s): %v", short(task.Commit), err)
		}
		if loaded.Commit != task.Commit || !reflect.DeepEqual(loaded.Rows, recorded[i].Rows) {
			t.Fatalf("the on-disk baseline for %s does not round-trip", short(task.Commit))
		}
		if !reflect.DeepEqual(loaded.Lifecycle, mat.states[task.Commit].Lifecycle) {
			t.Fatalf("the baseline for %s dropped the release's lifecycle observation", short(task.Commit))
		}
		s := loaded.Conform(bridgeRows(mat.states[task.Commit], task))
		if s.Status != StatusAgree {
			t.Fatalf("the recorded baseline for %s does not conform to the state it came from: %s %v",
				short(task.Commit), s.Status, s.Detail)
		}
	}
}

// Recording is all-or-nothing per run: a task that could not be materialized
// must abort it, so a partially-recorded corpus can never become the baseline a
// later gate run certifies against.
func TestGrTRecordFixturesAbortsOnAMaterializeFailure(t *testing.T) {
	first, second := goTask("1111111111111111"), goTask("2222222222222222")
	cfg, mat := gateFixture(t, twoFlowSeed, first)
	cfg.Manifest.Tasks = []Task{first, second}
	cfg.FixtureDir = t.TempDir()
	mat.errs = map[string]error{first.Commit: errors.New("worktree build failed")}
	recorded, err := RecordFixtures(cfg, mat)
	if err == nil || !strings.Contains(err.Error(), "worktree build failed") {
		t.Fatalf("RecordFixtures error = %v, want the materialize failure", err)
	}
	if recorded != nil {
		t.Fatalf("a failed recording returned %d fixture(s)", len(recorded))
	}
	if !equalStrings(mat.seen, []string{first.Commit}) {
		t.Fatalf("materialized %v, want the recording to stop at the failing task", mat.seen)
	}
	if _, statErr := os.Stat(FixturePath(cfg.FixtureDir, second.Commit)); statErr == nil {
		t.Fatalf("%s was recorded after the run aborted", FixturePath(cfg.FixtureDir, second.Commit))
	}
}

// A baseline that cannot be persisted must fail the command rather than be
// reported as recorded — otherwise `-record` would claim a baseline that does
// not exist and the next gate run would fail conformance for the wrong reason.
func TestGrTRecordFixturesAbortsWhenTheBaselineCannotBeSaved(t *testing.T) {
	task := goTask("1111111111111111")
	cfg, mat := gateFixture(t, twoFlowSeed, task)
	cfg.FixtureDir = grTBlockedDir(t)
	recorded, err := RecordFixtures(cfg, mat)
	if err == nil {
		t.Fatalf("RecordFixtures(%s) = nil error, want the write failure reported", cfg.FixtureDir)
	}
	if recorded != nil {
		t.Fatalf("an unsaved recording returned %d fixture(s)", len(recorded))
	}
	if !equalStrings(mat.seen, []string{task.Commit}) {
		t.Fatalf("materialized %v, want the task materialized before the save attempt", mat.seen)
	}
}

// The release's own impact answer keys the release-side rows. When that query
// resolved nothing, the fallback is the persisted graph's file → symbol mapping,
// so the recorded baseline still describes the task instead of going empty.
func TestGrTTaskSymbolIDsFallBackToTheGraphsOwnFileMapping(t *testing.T) {
	task := goTask("1111111111111111", grTFile)
	state := TaskState{Views: BridgeViews{Symbols: []crg.Symbol{
		{QualifiedName: grTQualified("Step"), FilePath: grTFile},
		{QualifiedName: grTQualified("Entry"), FilePath: grTFile},
		{QualifiedName: "pkg/b.go" + qualifiedSep + "Widget", FilePath: "pkg/b.go"},
	}}}
	want := []string{repoFile(grTFile, "Entry"), repoFile(grTFile, "Step")}
	if got := taskSymbolIDs(state, task); !equalStrings(got, want) {
		t.Fatalf("taskSymbolIDs = %v, want the changed file's symbols, sorted, and nothing else: %v", got, want)
	}
	state.Impact = BridgeImpact{ChangedIDs: []string{repoFile(grTFile, "Entry")}}
	if got := taskSymbolIDs(state, task); !equalStrings(got, state.Impact.ChangedIDs) {
		t.Fatalf("taskSymbolIDs = %v, want the release's own impact answer %v to win",
			got, state.Impact.ChangedIDs)
	}
}

// The fallback has to reach the recorded rows, not just the id list: a baseline
// that recorded no risk row because the impact query came back empty would read
// as "the release scored nothing" forever after.
func TestGrTBridgeRowsUseTheFallbackIDsWhenTheImpactQueryResolvedNothing(t *testing.T) {
	task := goTask("1111111111111111", grTFile)
	entry := repoFile(grTFile, "Entry")
	state := TaskState{Views: BridgeViews{
		Symbols:   []crg.Symbol{{QualifiedName: grTQualified("Entry"), FilePath: grTFile}},
		RiskIndex: map[string]BridgeRisk{entry: {RiskScore: 0.25, CallerCount: 3, TestCoverage: "untested"}},
	}}
	rows := bridgeRows(state, task)
	if !equalStrings(rows[SurfaceRiskIndex], []string{entry + " risk_score=0.250000"}) {
		t.Fatalf("recorded risk_index rows = %v, want the fallback ids scored", rows[SurfaceRiskIndex])
	}
	want := entry + " callers=3 coverage=untested security_relevant=false"
	if !equalStrings(rows[SurfaceRiskDetail], []string{want}) {
		t.Fatalf("recorded risk_detail rows = %v, want %q", rows[SurfaceRiskDetail], want)
	}
}

// A comparison that could not be computed is a FAILED surface. Reporting the
// release's rows against an empty native answer would manufacture either a
// divergence or — when the release also answered nothing — a false agreement.
func TestGrTImpactSurfaceFailsWhenTheNativeQueryCannotRun(t *testing.T) {
	native := nativeSide{store: grTRefusingStore{}}
	got := impactSurface(Config{Depth: DefaultDepth}, native, []string{repoFile(grTFile, "Entry")}, BridgeImpact{})
	if got.Name != SurfaceImpactRadius || got.Status != StatusFailed {
		t.Fatalf("impact surface = %q %s, want %q failed", got.Name, got.Status, SurfaceImpactRadius)
	}
	if !strings.Contains(got.Reason, "readback refused") {
		t.Fatalf("impact_radius reason = %q, want the underlying readback error", got.Reason)
	}
}

// When neither side traces a flow through the change there is nothing to
// compare. Saying so is the point: two empty row sets are trivially equal, and
// recording that as agreement would claim evidence the run never produced.
func TestGrTFlowSurfacesAreNotExercisedWhenNoFlowTouchesTheChange(t *testing.T) {
	state := TaskState{Views: BridgeViews{Flows: []BridgeFlow{
		{EntryPoint: "elsewhere", Path: []string{"elsewhere"}},
	}}}
	got := flowSurfaces(state, grTNative(crg.Postprocess{}, nil), []string{repoFile(grTFile, "Entry")})
	grTWantNotExercised(t, got, "no execution flow touches the changed symbols",
		SurfaceFlows, SurfaceFlowMetrics, SurfaceFlowSnapshots)
}

// Flow identity is the entry-point symbol and the ORDERED path. A reordered
// path is a different flow, and the diff has to name the row on each side.
func TestGrTFlowSurfacesCompareIdentityAndOrderedPath(t *testing.T) {
	entry, step := repoFile(grTFile, "Entry"), repoFile(grTFile, "Step")
	state := TaskState{Views: BridgeViews{
		Flows:         []BridgeFlow{{Name: "Entry", EntryPoint: entry, Path: []string{entry, step}}},
		FlowSnapshots: []BridgeFlowSnapshot{{EntryPoint: entry, Name: "Entry"}},
	}}
	nativeFlow := func(members ...string) nativeSide {
		return grTNative(crg.Postprocess{},
			[]crg.Flow{{ID: entry, EntryPoint: grTQualified("Entry"), Members: members}})
	}
	if got := flowSurfaces(state, nativeFlow(entry, step), []string{entry})[0]; got.Status != StatusAgree {
		t.Fatalf("flows = %s, want agreement on identical identity and path: %v", got.Status, got.Detail)
	}
	got := flowSurfaces(state, nativeFlow(step, entry), []string{entry})[0]
	if got.Status != StatusDiverge {
		t.Fatalf("flows = %s, want a divergence when the sides order the path differently", got.Status)
	}
	for _, want := range []string{
		"only in NATIVE: entry=" + entry + " path=" + step + " > " + entry,
		"only in BRIDGE: entry=" + entry + " path=" + entry + " > " + step,
	} {
		if !strings.Contains(grTDiff(got), want) {
			t.Fatalf("the flows diff does not name %q:\n%s", want, grTDiff(got))
		}
	}
}

// A change no community assignment covers disables both community surfaces.
// Comparing two empty partitions would agree by construction.
func TestGrTCommunitySurfacesAreNotExercisedWithoutAnyAssignment(t *testing.T) {
	state := TaskState{Views: BridgeViews{Communities: map[string]string{"elsewhere": "c1"}}}
	got := communitySurfaces(state, grTNative(crg.Postprocess{}, nil), []string{repoFile(grTFile, "Entry")})
	grTWantNotExercised(t, got, "no changed symbol carries a community assignment in the release graph",
		SurfaceCommunities, SurfaceCommunitySummaries)
}

// An explicitly UNASSIGNED symbol is still a partition answer, so the partition
// is compared — but the release writes no summary for a non-cluster, and that
// surface must be reported unexercised rather than as an empty agreement.
func TestGrTCommunitySummariesAreNotExercisedForAnUnassignedCluster(t *testing.T) {
	entry := repoFile(grTFile, "Entry")
	state := TaskState{Views: BridgeViews{Communities: map[string]string{entry: unassignedCluster}}}
	native := grTNative(crg.Postprocess{Communities: map[string]string{entry: unassignedCluster}}, nil)
	got := communitySurfaces(state, native, []string{entry})
	if len(got) != 2 {
		t.Fatalf("got %d community surface(s), want 2", len(got))
	}
	if got[0].Name != SurfaceCommunities || got[0].Status != StatusAgree {
		t.Fatalf("communities = %q %s, want the partition still compared: %v",
			got[0].Name, got[0].Status, got[0].Detail)
	}
	if got[1].Name != SurfaceCommunitySummaries || got[1].Status != StatusNotExercised {
		t.Fatalf("community_summaries = %q %s, want not_exercised", got[1].Name, got[1].Status)
	}
	if !strings.Contains(got[1].Reason, "community_summaries") {
		t.Fatalf("community_summaries reason = %q, want the missing rows named", got[1].Reason)
	}
}

// The partition is compared by exact canonical-cluster equality: a relabeled
// cluster is a divergence, and the diff names the assignment on each side.
func TestGrTCommunitySurfacesCompareClusterKeysExactly(t *testing.T) {
	entry := repoFile(grTFile, "Entry")
	state := TaskState{Views: BridgeViews{
		Communities:        map[string]string{entry: entry},
		CommunitySummaries: []BridgeCommunitySummary{{Cluster: entry, Name: "pkg"}},
	}}
	nativeAt := func(cluster string) nativeSide {
		return grTNative(crg.Postprocess{Communities: map[string]string{entry: cluster}}, nil)
	}
	if got := communitySurfaces(state, nativeAt(entry), []string{entry})[0]; got.Status != StatusAgree {
		t.Fatalf("communities = %s, want agreement on the same cluster key: %v", got.Status, got.Detail)
	}
	got := communitySurfaces(state, nativeAt("other-cluster"), []string{entry})[0]
	if got.Status != StatusDiverge {
		t.Fatalf("communities = %s, want a divergence on a relabeled cluster", got.Status)
	}
	for _, want := range []string{
		"only in NATIVE: " + entry + " -> other-cluster",
		"only in BRIDGE: " + entry + " -> " + entry,
	} {
		if !strings.Contains(grTDiff(got), want) {
			t.Fatalf("the communities diff does not name %q:\n%s", want, grTDiff(got))
		}
	}
}

// A change the release scored in no risk_index row disables both risk surfaces.
func TestGrTRiskSurfacesAreNotExercisedWhenTheReleaseScoredNothing(t *testing.T) {
	state := TaskState{Views: BridgeViews{
		RiskIndex: map[string]BridgeRisk{"elsewhere": {RiskScore: 1}},
	}}
	got := riskSurfaces(state, grTNative(crg.Postprocess{}, nil), []string{repoFile(grTFile, "Entry")})
	grTWantNotExercised(t, got, "the release scored none of the changed symbols in risk_index",
		SurfaceRiskIndex, SurfaceRiskDetail)
}

// risk_score is a deterministic function of the graph, so it is compared by
// exact value at the pinned precision — not by rank and not by tolerance.
func TestGrTRiskSurfacesCompareScoresExactly(t *testing.T) {
	entry := repoFile(grTFile, "Entry")
	state := TaskState{Views: BridgeViews{
		RiskIndex: map[string]BridgeRisk{entry: {RiskScore: 0.3}},
	}}
	nativeAt := func(score float64) nativeSide {
		return grTNative(crg.Postprocess{RiskIndex: map[string]float64{entry: score}}, nil)
	}
	if got := riskSurfaces(state, nativeAt(0.3), []string{entry})[0]; got.Status != StatusAgree {
		t.Fatalf("risk_index = %s, want agreement on equal scores: %v", got.Status, got.Detail)
	}
	got := riskSurfaces(state, nativeAt(0.30001), []string{entry})[0]
	if got.Status != StatusDiverge {
		t.Fatalf("risk_index = %s, want a divergence on unequal scores", got.Status)
	}
	for _, want := range []string{
		"only in NATIVE: " + entry + " risk_score=0.300010",
		"only in BRIDGE: " + entry + " risk_score=0.300000",
	} {
		if !strings.Contains(grTDiff(got), want) {
			t.Fatalf("the risk_index diff does not name %q:\n%s", want, grTDiff(got))
		}
	}
}

// A commit that changed no declaration identifier the release's extractor
// covers has no search to replay — but its INDEX content is still comparable,
// so only the search surface goes unexercised.
func TestGrTFTSSearchIsNotExercisedWithoutAChangedIdentifier(t *testing.T) {
	task := Task{Commit: "1111111111111111", ChangedFiles: []string{grTFile}}
	token := grTQualified("Entry")
	state := TaskState{Views: BridgeViews{FTSIndex: []string{token}}}
	native := grTNative(crg.Postprocess{FTS: []string{token}}, nil)
	got := ftsSurfaces(task, state, native)
	if len(got) != 2 {
		t.Fatalf("got %d fts surface(s), want 2", len(got))
	}
	if got[0].Name != SurfaceFTSIndex || got[0].Status != StatusAgree {
		t.Fatalf("fts_index = %q %s, want the index content still compared: %v",
			got[0].Name, got[0].Status, got[0].Detail)
	}
	if got[1].Name != SurfaceFTSSearch || got[1].Status != StatusNotExercised {
		t.Fatalf("fts_search = %q %s, want not_exercised", got[1].Name, got[1].Status)
	}
	if !strings.Contains(got[1].Reason, "declaration identifier") {
		t.Fatalf("fts_search reason = %q, want the measured reason", got[1].Reason)
	}
}

// The edge-confidence surface compares the columns the release stored for the
// edges this change touches. With no such edge there is nothing to compare, and
// an empty comparison is not agreement.
func TestGrTEdgeConfidenceIsNotExercisedWithoutAStoredEdge(t *testing.T) {
	task := Task{Commit: "1111111111111111", ChangedFiles: []string{grTFile}}
	state := TaskState{Views: BridgeViews{Edges: []BridgeEdge{
		{Kind: "CALLS", From: "x", To: "y", FilePath: "pkg/b.go", Confidence: 1},
	}}}
	got := edgeConfidenceSurface(task, state)
	if got.Name != SurfaceEdgeConfidence || got.Status != StatusNotExercised {
		t.Fatalf("edge_confidence = %q %s, want not_exercised", got.Name, got.Status)
	}
	if !strings.Contains(got.Reason, "no edge in the changed files") {
		t.Fatalf("edge_confidence reason = %q, want the measured reason", got.Reason)
	}
}
