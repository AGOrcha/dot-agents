package crgbehavior

import (
	"errors"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// Native symbol ids of the fixture graph (qualified_name@file_path).
const (
	idEntry  = "a.go::Entry@a.go"
	idStep   = "a.go::Step@a.go"
	idWidget = "b.go::Widget@b.go"
	idLone   = "b.go::Lone@b.go"
)

// fixtureViews is a legacy-bridge state whose derived views are hand-written to
// match what the kg-native adapter derives from the same graph. Agreement is
// therefore asserted, not copied: perturbing either side must fail an oracle.
func fixtureViews() BridgeViews {
	return BridgeViews{
		Symbols: []crg.Symbol{
			{QualifiedName: "a.go::Entry", Kind: "Function", Language: "go", FilePath: "a.go", ContentHash: "h"},
			{QualifiedName: "a.go::Step", Kind: "Function", Language: "go", FilePath: "a.go", ContentHash: "h"},
			{QualifiedName: "b.go::Widget", Kind: "Type", Language: "go", FilePath: "b.go", ContentHash: "h"},
			{QualifiedName: "b.go::Lone", Kind: "Function", Language: "go", FilePath: "b.go", ContentHash: "h"},
		},
		References: []crg.Reference{
			{Kind: "CALLS", From: "a.go::Entry", To: "a.go::Step"},
			{Kind: "IMPORTS", From: "a.go::Entry", To: "b.go::Widget"},
		},
		FlowMemberships: []crg.FlowMembership{
			{FlowID: idEntry, MemberID: idEntry, Position: 0},
			{FlowID: idEntry, MemberID: idStep, Position: 1},
		},
		Communities: map[string]string{
			idEntry: "c1", idStep: "c1", idWidget: "c1", idLone: "c2",
		},
		RiskIndex:           map[string]float64{idEntry: 2, idStep: 1, idWidget: 1, idLone: 0},
		FTS:                 []string{"a.go::Entry", "a.go::Step", "b.go::Lone", "b.go::Widget"},
		FilesIndexed:        2,
		CommunitiesAssigned: 4,
	}
}

// fakeImpact is a recorded legacy impact-radius answer.
type fakeImpact struct {
	changed   []string
	impacted  []string
	truncated bool
	err       error
	calls     int
}

func (f *fakeImpact) ImpactRadius(_ []string, _, _ int) (BridgeImpact, error) {
	f.calls++
	if f.err != nil {
		return BridgeImpact{}, f.err
	}
	return BridgeImpact{ChangedIDs: f.changed, ImpactedIDs: f.impacted, Truncated: f.truncated}, nil
}

// agreeingImpact is the legacy answer that matches the kg-native traversal.
func agreeingImpact() *fakeImpact {
	return &fakeImpact{changed: []string{idEntry, idStep}, impacted: []string{idWidget}}
}

func fixtureConfig(tasks ...Task) Config {
	if len(tasks) == 0 {
		tasks = []Task{{
			Commit:       "1111111111111111",
			Subject:      "feat: touch a.go",
			ChangedFiles: []string{"a.go"},
			Identifiers:  []string{"Entry"},
		}}
	}
	return Config{
		RepoRoot: "/repo",
		Manifest: Manifest{SchemaVersion: ManifestSchemaVersion, Head: "headsha0", Tasks: tasks},
	}
}

func TestRunAgreesWhenBothSidesMatch(t *testing.T) {
	report, err := Run(fixtureConfig(), fixtureViews(), agreeingImpact())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !report.Pass() {
		t.Fatalf("matching sides must pass; failing surfaces: %v", report.FailingSurfaces())
	}
	if report.ExecutedTasks() != 1 || report.NativeSymbols != 4 {
		t.Fatalf("report = %+v, want one executed task over four ingested symbols", report)
	}
	for _, s := range report.Tasks[0].Surfaces {
		if !s.Pass {
			t.Fatalf("surface %s must pass: %v", s.Name, s.Detail)
		}
	}
}

func TestRunFailsAndLocatesAFlowDivergence(t *testing.T) {
	views := fixtureViews()
	// The legacy side lost a flow member the kg-native derivation keeps.
	views.FlowMemberships = views.FlowMemberships[:1]
	report, err := Run(fixtureConfig(), views, agreeingImpact())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Pass() {
		t.Fatal("a dropped flow member must fail the gate")
	}
	s := surfaceNamed(t, report, SurfaceFlows)
	if s.Pass || len(s.Detail) == 0 {
		t.Fatalf("flows surface must report the structural diff: %+v", s)
	}
	if !strings.Contains(strings.Join(s.Detail, "\n"), idStep) {
		t.Fatalf("the diff must name the missing member, got %v", s.Detail)
	}
}

func TestRunFailsOnChangedNodeDivergence(t *testing.T) {
	impact := agreeingImpact()
	impact.changed = []string{idEntry} // the bridge resolved fewer symbols
	report, err := Run(fixtureConfig(), fixtureViews(), impact)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Pass() {
		t.Fatal("a seed-set divergence must fail the gate")
	}
	s := surfaceNamed(t, report, SurfaceChangedNodes)
	if !strings.Contains(strings.Join(s.Detail, "\n"), "only in NATIVE") {
		t.Fatalf("the diff must say which side has the extra symbol: %v", s.Detail)
	}
}

func TestAdvisorySurfacesDoNotGateUntilStrict(t *testing.T) {
	views := fixtureViews()
	// Communities, impact radius and risk ranking all diverge; none of them
	// gates today, so the run still passes.
	views.Communities = map[string]string{idEntry: "c1", idStep: "c2", idWidget: "c3", idLone: "c4"}
	views.RiskIndex = map[string]float64{idEntry: 0, idStep: 9, idWidget: 1, idLone: 0}
	impact := agreeingImpact()
	impact.impacted = []string{"b.go::Unrelated@b.go"}
	impact.truncated = true

	report, err := Run(fixtureConfig(), views, impact)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !report.Pass() {
		t.Fatalf("advisory divergence must not fail the default gate: %v", report.FailingSurfaces())
	}
	for _, name := range []string{SurfaceCommunities, SurfaceRiskIndex, SurfaceImpactRadius} {
		s := surfaceNamed(t, report, name)
		if s.Pass || !s.Advisory || s.AdvisoryReason == "" {
			t.Fatalf("%s must be reported as an advisory divergence with a reason: %+v", name, s)
		}
	}

	strict := fixtureConfig()
	strict.Strict = true
	strictReport, err := Run(strict, views, impact)
	if err != nil {
		t.Fatalf("strict run: %v", err)
	}
	if strictReport.Pass() {
		t.Fatal("strict mode (§11.4 sign-off) must gate on the advisory surfaces")
	}
}

func TestFlowOrderDivergenceIsAdvisoryWhenMembershipAgrees(t *testing.T) {
	views := fixtureViews()
	// Same members, legacy step numbering — exactly what the real bridge does.
	views.FlowMemberships = []crg.FlowMembership{
		{FlowID: idEntry, MemberID: idEntry, Position: 0},
		{FlowID: idEntry, MemberID: idStep, Position: 4},
	}
	report, err := Run(fixtureConfig(), views, agreeingImpact())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !report.Pass() {
		t.Fatalf("a step-order-only difference must not fail the gate: %v", report.FailingSurfaces())
	}
	if s := surfaceNamed(t, report, SurfaceFlows); !s.Pass {
		t.Fatalf("membership still agrees, so flows must pass: %v", s.Detail)
	}
	if s := surfaceNamed(t, report, SurfaceFlowOrder); s.Pass || !s.Advisory {
		t.Fatalf("flow_order must report the ordering divergence as advisory: %+v", s)
	}
}

func TestRunSkipsTasksOutsideTheBuiltGraph(t *testing.T) {
	cfg := fixtureConfig(Task{Commit: "2222222222222222", Subject: "docs", ChangedFiles: []string{"absent.go"}})
	report, err := Run(cfg, fixtureViews(), &fakeImpact{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !report.Tasks[0].Skipped || report.ExecutedTasks() != 0 {
		t.Fatalf("a commit whose files are not in the graph must SKIP: %+v", report.Tasks[0])
	}
	if report.Pass() {
		t.Fatal("a run with zero executed tasks must not report PASS")
	}
}

func TestSurfacesSkipWhenTheLegacyViewWasNeverComputed(t *testing.T) {
	views := fixtureViews()
	views.FlowMemberships = nil   // the legacy build ran no flow detection
	views.CommunitiesAssigned = 0 // nor community detection
	views.RiskIndex = map[string]float64{}
	views.FTS = nil
	report, err := Run(fixtureConfig(), views, agreeingImpact())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, name := range []string{SurfaceFlows, SurfaceFlowOrder, SurfaceCommunities, SurfaceRiskIndex, SurfaceFTS} {
		s := surfaceNamed(t, report, name)
		if !s.Skipped || !strings.Contains(s.SkipReason, "did not compute that view") {
			t.Fatalf("%s must skip as an uncomputed legacy view, not fail as a divergence: %+v", name, s)
		}
	}
	if !report.Pass() {
		t.Fatalf("an uncomputed legacy view is not a divergence: %v", report.FailingSurfaces())
	}
	out := renderOf(t, report)
	if !strings.Contains(out, "NOT EXERCISED") || !strings.Contains(out, "surface(s) NOT exercised") {
		t.Fatalf("the report must state which surfaces were never exercised:\n%s", out)
	}
}

func TestSurfacesSkipWhenTheTaskCannotExerciseThem(t *testing.T) {
	views := fixtureViews()
	cfg := fixtureConfig(Task{
		Commit:       "3333333333333333",
		ChangedFiles: []string{"b.go"},
		Identifiers:  []string{"NotIndexed"},
	})
	// b.go's symbols participate in no flow; the identifier matches nothing.
	report, err := Run(cfg, views, &fakeImpact{changed: []string{idWidget, idLone}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, name := range []string{SurfaceFlows, SurfaceFlowOrder, SurfaceFTS} {
		if s := surfaceNamed(t, report, name); !s.Skipped || s.SkipReason == "" {
			t.Fatalf("%s must skip with a reason on this task: %+v", name, s)
		}
	}
	if !report.Pass() {
		t.Fatalf("skipped surfaces must not fail the gate: %v", report.FailingSurfaces())
	}
	if out := renderOf(t, report); !strings.Contains(out, "skip  flows") {
		t.Fatalf("a skipped surface must be rendered with its reason:\n%s", out)
	}
}

func TestFTSSkipsWhenTheCommitChangedNoDeclaration(t *testing.T) {
	cfg := fixtureConfig(Task{Commit: "4444444444444444", ChangedFiles: []string{"a.go"}})
	report, err := Run(cfg, fixtureViews(), agreeingImpact())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if s := surfaceNamed(t, report, SurfaceFTS); !s.Skipped {
		t.Fatalf("no changed identifier means no FTS query: %+v", s)
	}
}

func TestCommunitiesSkipWithFewerThanTwoChangedSymbols(t *testing.T) {
	cfg := fixtureConfig(Task{Commit: "5555555555555555", ChangedFiles: []string{"c.go"}})
	views := fixtureViews()
	views.Symbols = append(views.Symbols, crg.Symbol{QualifiedName: "c.go::Only", Kind: "Function", FilePath: "c.go"})
	report, err := Run(cfg, views, &fakeImpact{changed: []string{"c.go::Only@c.go"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if s := surfaceNamed(t, report, SurfaceCommunities); !s.Skipped {
		t.Fatalf("a single changed symbol cannot exercise partition agreement: %+v", s)
	}
}

func TestRunPropagatesABridgeQueryFailure(t *testing.T) {
	_, err := Run(fixtureConfig(), fixtureViews(), &fakeImpact{err: errors.New("python exploded")})
	if err == nil || !strings.Contains(err.Error(), "11111111") {
		t.Fatalf("err = %v, want one naming the failing commit", err)
	}
}

func TestRunHonorsMaxTasksAndDefaults(t *testing.T) {
	cfg := fixtureConfig(
		Task{Commit: "aaaaaaaaaaaaaaaa", ChangedFiles: []string{"a.go"}},
		Task{Commit: "bbbbbbbbbbbbbbbb", ChangedFiles: []string{"a.go"}},
	)
	cfg.MaxTasks = 1
	impact := agreeingImpact()
	report, err := Run(cfg, fixtureViews(), impact)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Tasks) != 1 || impact.calls != 1 {
		t.Fatalf("MaxTasks must bound the run: %d task(s), %d bridge call(s)", len(report.Tasks), impact.calls)
	}
	full := Config{}.withDefaults()
	if full.Depth != DefaultDepth || full.MaxResults != DefaultMaxResults || full.SpearmanTau <= 0 {
		t.Fatalf("defaults not applied: %+v", full)
	}
}

func TestBootstrapNativeSurfacesStoreFailures(t *testing.T) {
	views := fixtureViews()
	views.References = append(views.References, crg.Reference{Kind: "", From: "a.go::Entry", To: "a.go::Step"})
	if _, err := Run(fixtureConfig(), views, agreeingImpact()); err == nil {
		t.Fatal("an un-ingestable graph must fail the run, not silently compare nothing")
	}
	if _, err := bootstrapNative(failingStore{onNotes: true}, fixtureViews(), "sha"); err == nil {
		t.Fatal("a namespace readback failure must propagate")
	}
	if _, err := bootstrapNative(failingStore{}, fixtureViews(), "sha"); err == nil {
		t.Fatal("a derived-view failure must propagate")
	}
}

func TestImpactSurfaceReportsAnUnreadableStore(t *testing.T) {
	s := impactSurface(Config{}.withDefaults(), nativeSide{store: failingStore{}}, []string{idEntry}, BridgeImpact{})
	if s.Pass || len(s.Detail) == 0 {
		t.Fatalf("an unreadable store must be reported, not treated as agreement: %+v", s)
	}
}

// failingStore is a Store whose reads fail: onNotes selects whether the notes
// read or only the edges read fails, so both readback paths are exercised.
type failingStore struct{ onNotes bool }

func (f failingStore) WriteNotes(sdk.Token, string, []sdk.Note) error { return nil }
func (f failingStore) WriteEdges(sdk.Token, string, []sdk.Edge) error { return nil }

func (f failingStore) Notes(sdk.Token, string) ([]sdk.Note, error) {
	if f.onNotes {
		return nil, errors.New("notes unavailable")
	}
	return nil, nil
}

func (f failingStore) Edges(sdk.Token, string) ([]sdk.Edge, error) {
	return nil, errors.New("edges unavailable")
}

// surfaceNamed returns the named surface of the first task.
func surfaceNamed(t *testing.T, r Report, name string) Surface {
	t.Helper()
	for _, s := range r.Tasks[0].Surfaces {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("surface %q not reported", name)
	return Surface{}
}
