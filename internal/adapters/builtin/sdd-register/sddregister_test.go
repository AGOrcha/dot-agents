package sddregister_test

import (
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	sddregister "github.com/AGOrcha/dot-agents/internal/adapters/builtin/sdd-register"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// Shared test literals hoisted so no single _test.go file repeats a string
// often enough to trip SonarCloud S1192 (the coverage gate's new-issues check
// blocks on duplicated literals in tests too).
const (
	colTaskKey = "t.task_key" // RETURN-column key for task.task_key
	colPlanID  = "p.plan_id"
	colSpecID  = "s.spec_id"
	keyTaskKey = "task_key" // query param name
	keySpecID  = "spec_id"
	ntSpec     = "spec"
	ntPlan     = "plan"
	etBelongs  = "belongs_to_plan"
)

// workflowDir resolves THIS repo's real .agents/workflow/ tree relative to the
// test file, so the hard test ingests the live corpus regardless of the cwd
// `go test` chooses. The package lives at
// internal/adapters/builtin/sdd-register, so the repo root is 4 dirs up.
func workflowDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	return filepath.Join(root, ".agents", "workflow")
}

// ingestRepo runs the production Ingest path over the REAL repo tree and returns
// the result. A failure here is a production bug, not a test-setup convenience.
func ingestRepo(t *testing.T) (*sddregister.Adapter, *sddregister.IngestResult) {
	t.Helper()
	a := sddregister.New()
	res, err := a.Ingest(sdk.NewMemStore(), workflowDir(t))
	if err != nil {
		t.Fatalf("ingest real workflow tree: %v", err)
	}
	return a, res
}

// TestSchemaLoadsAndResolves proves the v4-subset schema validates with the same
// registry loader the built-in adapters use, and that the adapter is
// registry-resolvable by ref.
func TestSchemaLoadsAndResolves(t *testing.T) {
	a := sddregister.New()
	s := a.Schema()
	if s.Name != sddregister.Name {
		t.Fatalf("schema name = %q, want %q", s.Name, sddregister.Name)
	}
	if len(s.NoteTypes) != 3 {
		t.Fatalf("note types = %d, want 3 (spec, plan, task)", len(s.NoteTypes))
	}
	if len(s.EdgeTypes) != 5 {
		t.Fatalf("edge types = %d, want 5", len(s.EdgeTypes))
	}
	reg := registry.New()
	if err := sddregister.Register(reg); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := reg.Resolve("dotagents-builtin:graph/sdd-register@^1.0"); err != nil {
		t.Fatalf("resolve sdd-register@^1.0: %v", err)
	}
}

// TestIngestRealTreeCounts is the §13.3-style hard test: it ingests the REAL
// .agents/workflow/ tree through the production Ingest path and asserts the
// projection is plausible — every spec/plan/task became a node, the
// contains_task and belongs_to_plan edges are 1:1 with tasks, and the trace
// edges (plan_for_spec, implements_spec) are present. It uses lower-bound
// assertions (the live corpus grows) but pins the structural invariants exactly.
func TestIngestRealTreeCounts(t *testing.T) {
	_, res := ingestRepo(t)

	// The repo has many specs and plans; assert a meaningful floor so a broken
	// walk (zero/near-zero nodes) fails loudly.
	if res.SpecsParsed < 20 {
		t.Errorf("specs parsed = %d, want >= 20", res.SpecsParsed)
	}
	if res.PlansParsed < 20 {
		t.Errorf("plans parsed = %d, want >= 20", res.PlansParsed)
	}
	tasks := res.NotesByType["task"]
	if tasks < res.PlansParsed {
		t.Errorf("task nodes = %d, want >= plans (%d)", tasks, res.PlansParsed)
	}

	// Structural invariants that must hold EXACTLY regardless of corpus size.
	if res.NotesByType[ntSpec] != res.SpecsParsed {
		t.Errorf("spec nodes = %d, want = specs parsed %d", res.NotesByType[ntSpec], res.SpecsParsed)
	}
	if res.NotesByType[ntPlan] != res.PlansParsed {
		t.Errorf("plan nodes = %d, want = plans parsed %d", res.NotesByType[ntPlan], res.PlansParsed)
	}
	if res.NodeCount != res.NotesByType[ntSpec]+res.NotesByType[ntPlan]+res.NotesByType["task"] {
		t.Errorf("node count %d != sum of per-type counts", res.NodeCount)
	}
	// Every task has exactly one contains_task and one belongs_to_plan edge.
	if res.EdgesByType["contains_task"] != tasks {
		t.Errorf("contains_task edges = %d, want = task count %d", res.EdgesByType["contains_task"], tasks)
	}
	if res.EdgesByType[etBelongs] != tasks {
		t.Errorf("belongs_to_plan edges = %d, want = task count %d", res.EdgesByType[etBelongs], tasks)
	}
	// The trace edges the spec asked for must actually be populated.
	if res.EdgesByType["plan_for_spec"] == 0 {
		t.Error("plan_for_spec edges = 0; the spec<-plan trace is empty")
	}
	if res.EdgesByType["implements_spec"] == 0 {
		t.Error("implements_spec edges = 0; the spec<-task trace is empty")
	}
	if res.EdgeCount == 0 {
		t.Error("edge count = 0")
	}
}

// traceProbe is a spec known to be present in this repo with a same-named plan,
// so plan_for_spec and implements_spec edges exist for it. If the repo ever
// drops this plan/spec pair, this test should be repointed — but the pair is the
// canonical KG-schema dossier's own neighborhood, so it is stable.
const traceProbeSpec = "docs-starlight-migration"

// TestTraceQueryReturnsRealRows runs the real DSL trace queries over the
// real-ingested namespace and asserts they return real rows. This is the
// "named queries return expected results" half of the hard test — the query is
// run through the PUBLIC RunNamed path, exactly as a caller would.
func TestTraceQueryReturnsRealRows(t *testing.T) {
	a, res := ingestRepo(t)

	// 1) tasks_implementing_spec: the probe spec has implementing tasks.
	impl, err := a.RunNamed(sddregister.QueryTasksImplementingSpec, res.View, map[string]any{keySpecID: traceProbeSpec})
	if err != nil {
		t.Fatalf("tasks_implementing_spec: %v", err)
	}
	if len(impl) == 0 {
		t.Fatalf("tasks_implementing_spec(%s) returned no rows; the implements_spec trace is broken", traceProbeSpec)
	}
	taskKey, ok := impl[0][colTaskKey].(string)
	if !ok || taskKey == "" {
		t.Fatalf("first implementing task has no task_key: %v", impl[0])
	}

	// 2) task_trace: that task traces back to its plan AND spec.
	trace, err := a.RunNamed(sddregister.QueryTaskTrace, res.View, map[string]any{keyTaskKey: taskKey})
	if err != nil {
		t.Fatalf("task_trace: %v", err)
	}
	if len(trace) != 1 {
		t.Fatalf("task_trace(%s) = %d rows, want 1", taskKey, len(trace))
	}
	if got := trace[0][colSpecID]; got != traceProbeSpec {
		t.Errorf("task_trace spec_id = %v, want %s", got, traceProbeSpec)
	}
	if got := trace[0][colPlanID]; got != traceProbeSpec {
		t.Errorf("task_trace plan_id = %v, want %s (same-named plan)", got, traceProbeSpec)
	}
}

// TestTraceQueryMutationVerified is the mutation check the
// tests-must-drive-the-production-path lesson requires: it proves the trace
// query genuinely DEPENDS on the edges the production ingest emits. It ingests
// the real tree, confirms the trace returns a row, then removes the
// plan_for_spec edge the trace walks and confirms the SAME query goes empty. A
// query that still returned a row after the edge was dropped would be hollow.
func TestTraceQueryMutationVerified(t *testing.T) {
	a, res := ingestRepo(t)

	impl, err := a.RunNamed(sddregister.QueryTasksImplementingSpec, res.View, map[string]any{keySpecID: traceProbeSpec})
	if err != nil || len(impl) == 0 {
		t.Fatalf("precondition: tasks_implementing_spec(%s) err=%v rows=%d", traceProbeSpec, err, len(impl))
	}
	taskKey := impl[0][colTaskKey].(string)

	// Baseline: the trace returns exactly one row over the unmutated view.
	base, err := a.RunNamed(sddregister.QueryTaskTrace, res.View, map[string]any{keyTaskKey: taskKey})
	if err != nil {
		t.Fatalf("baseline task_trace: %v", err)
	}
	if len(base) != 1 {
		t.Fatalf("baseline task_trace = %d rows, want 1 (no point mutating a 0-row baseline)", len(base))
	}

	// MUTATION: drop the plan_for_spec edge(s) for the probe plan. The trace
	// query walks task -> plan -> [plan_for_spec] -> spec, so removing that hop
	// must make the trace empty. (We do NOT touch the task or its
	// belongs_to_plan edge, so only the spec hop is severed.)
	mutated := dropEdges(res.View, "plan_for_spec")
	after, err := a.RunNamed(sddregister.QueryTaskTrace, mutated, map[string]any{keyTaskKey: taskKey})
	if err != nil {
		t.Fatalf("post-mutation task_trace: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("mutation check FAILED: task_trace still returned %d rows after dropping plan_for_spec; "+
			"the query does not actually depend on the ingested edge (hollow test)", len(after))
	}

	// Second mutation: drop belongs_to_plan instead — the first hop. The trace
	// must also collapse, proving it depends on that edge too.
	mutated2 := dropEdges(res.View, etBelongs)
	after2, err := a.RunNamed(sddregister.QueryTaskTrace, mutated2, map[string]any{keyTaskKey: taskKey})
	if err != nil {
		t.Fatalf("post-mutation2 task_trace: %v", err)
	}
	if len(after2) != 0 {
		t.Fatalf("mutation check FAILED: task_trace survived dropping belongs_to_plan (%d rows)", len(after2))
	}
}

// TestImpactRadiusOverRealPlan runs the schema's impact_radius query (plan ->
// its tasks) over the real ingest and asserts the probe plan surfaces real
// task_keys.
func TestImpactRadiusOverRealPlan(t *testing.T) {
	a, res := ingestRepo(t)
	rows, err := a.RunImpact(res.View, traceProbeSpec)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("impact_radius(%s) returned no tasks", traceProbeSpec)
	}
	keys := stringColumn(rows, colTaskKey)
	for _, k := range keys {
		if k == "" {
			t.Fatalf("impact row has empty task_key: %v", rows)
		}
	}
}

// TestQueryNamesRegistered confirms the named-query catalog is wired.
func TestQueryNamesRegistered(t *testing.T) {
	a := sddregister.New()
	names := a.QueryNames()
	sort.Strings(names)
	want := []string{
		sddregister.QueryTaskTrace,
		sddregister.QueryTasksBlockedBy,
		sddregister.QueryTasksImplementingSpec,
	}
	sort.Strings(want)
	if len(names) != len(want) {
		t.Fatalf("query names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("query names = %v, want %v", names, want)
		}
	}
}

// TestRunNamedUnknownErrors covers the unknown-query error path.
func TestRunNamedUnknownErrors(t *testing.T) {
	a := sddregister.New()
	if _, err := a.RunNamed("no-such-query", sdk.NamespaceView{}, nil); err == nil {
		t.Fatal("RunNamed with unknown name returned nil error")
	}
}

// TestNewFromYAMLRejectsBadSchema covers the fallible constructor's error paths
// (which New panics on, and which cannot occur for the shipped embed).
func TestNewFromYAMLRejectsBadSchema(t *testing.T) {
	cases := map[string]string{
		"missing-version": "name: x\nimpact_radius:\n  query: RETURN $x\n  max_depth: 1\n",
		"bad-query":       "name: x\nversion: 1.0.0\nimpact_radius:\n  query: NOT A QUERY\n  max_depth: 1\n",
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := sddregister.NewFromYAML([]byte(yaml)); err == nil {
				t.Fatalf("NewFromYAML(%s) returned nil error", name)
			}
		})
	}
}

// dropEdges returns a copy of view with every edge of the given type removed.
// It is a VIEW mutation (severing a production-emitted edge), used to falsify
// the trace query — not a production code path.
func dropEdges(view sdk.NamespaceView, edgeType string) sdk.NamespaceView {
	out := sdk.NamespaceView{Notes: view.Notes}
	for _, e := range view.Edges {
		if e.Type != edgeType {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}

// stringColumn collects the string values of a column across rows.
func stringColumn(rows []sdk.Row, col string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if s, ok := r[col].(string); ok {
			out = append(out, s)
		}
	}
	return out
}
