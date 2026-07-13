package sddregister

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/commands/workflow"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

const (
	tWorkflow = "workflow"
	tSpecs    = "specs"
	tPlans    = "plans"
	tAlpha    = "alpha" // tiny-tree spec/plan id (same name on both sides)
	tReal     = "real"  // listSubdirs test dir name
	tDirX     = "dir-x" // effectivePlanID fallback dir name
)

// writeFile writes content to path, creating parent dirs, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildTinyTree lays out a minimal but valid .agents/workflow/ tree in a temp
// dir: one spec, one same-named plan with two tasks (one depending on the
// other), and one unrelated plan with no matching spec. Returns the workflow dir.
func buildTinyTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	wf := filepath.Join(root, tWorkflow)

	writeFile(t, filepath.Join(wf, tSpecs, tAlpha, specFileName),
		"# Alpha Spec\n\n**Status:** draft v1\n\nbody\n")
	writeFile(t, filepath.Join(wf, tPlans, tAlpha, planFileName),
		"schema_version: 1\nid: alpha\ntitle: Alpha Plan\nstatus: active\nsummary: do alpha\n")
	writeFile(t, filepath.Join(wf, tPlans, tAlpha, tasksFileName),
		"schema_version: 1\nplan_id: alpha\ntasks:\n"+
			"  - id: t1\n    title: First\n    status: completed\n    app_type: go-cli\n    depends_on: []\n"+
			"  - id: t2\n    title: Second\n    status: pending\n    depends_on: [t1, other/x9]\n")
	// A plan with NO matching spec — exercises the linkedSpec "" branch.
	writeFile(t, filepath.Join(wf, tPlans, "orphan", planFileName),
		"schema_version: 1\nid: orphan\ntitle: Orphan\nstatus: active\nsummary: no spec\n")
	writeFile(t, filepath.Join(wf, tPlans, "orphan", tasksFileName),
		"schema_version: 1\nplan_id: orphan\ntasks:\n  - id: o1\n    title: Only\n    status: pending\n    depends_on: []\n")
	return wf
}

// TestIngestTinyTree drives Ingest over a controlled tiny tree so the exact
// node/edge counts and the depends_on cross-plan + same-plan edges are
// assertable, and the loadView readback is exercised.
func TestIngestTinyTree(t *testing.T) {
	a := New()
	res, err := a.Ingest(sdk.NewMemStore(), buildTinyTree(t))
	if err != nil {
		t.Fatalf("ingest tiny tree: %v", err)
	}
	if res.SpecsParsed != 1 || res.PlansParsed != 2 {
		t.Fatalf("specs=%d plans=%d, want 1/2", res.SpecsParsed, res.PlansParsed)
	}
	// 1 spec + 2 plans + 3 tasks = 6 nodes.
	if res.NodeCount != 6 {
		t.Fatalf("node count = %d, want 6", res.NodeCount)
	}
	// plan_for_spec: only alpha (orphan has no spec) = 1.
	if res.EdgesByType[edgePlanForSpec] != 1 {
		t.Errorf("plan_for_spec = %d, want 1", res.EdgesByType[edgePlanForSpec])
	}
	// implements_spec: alpha's 2 tasks = 2 (orphan's task has no spec).
	if res.EdgesByType[edgeImplementsSpec] != 2 {
		t.Errorf("implements_spec = %d, want 2", res.EdgesByType[edgeImplementsSpec])
	}
	// depends_on: t2 -> t1 (same plan) and t2 -> other/x9 (cross-plan) = 2.
	if res.EdgesByType[edgeDependsOn] != 2 {
		t.Errorf("depends_on = %d, want 2", res.EdgesByType[edgeDependsOn])
	}
	// The cross-plan dep must keep the <plan>/<task> form, namespaced to a task node.
	if !hasEdge(res.View, edgeDependsOn, taskNodeID(tAlpha, "t2"), typeTask+":other/x9") {
		t.Error("cross-plan depends_on edge missing or mis-keyed")
	}
}

// TestIngestErrors covers the Ingest failure branches: a missing specs dir and
// a missing plans dir each surface a listing error.
func TestIngestErrors(t *testing.T) {
	a := New()
	if _, err := a.Ingest(sdk.NewMemStore(), filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("Ingest over a non-existent tree returned nil error")
	}

	// specs/ present but plans/ absent -> plans listing error.
	wf := filepath.Join(t.TempDir(), tWorkflow)
	writeFile(t, filepath.Join(wf, tSpecs, "a", specFileName), "# A\n")
	if _, err := a.Ingest(sdk.NewMemStore(), wf); err == nil {
		t.Fatal("Ingest with missing plans dir returned nil error")
	}
}

// TestAddPlanErrors covers addPlan's load-failure branches via malformed files.
func TestAddPlanErrors(t *testing.T) {
	b := &builder{specSet: map[string]bool{}}

	// Missing PLAN.yaml.
	if err := b.addPlan(t.TempDir(), "p"); err == nil {
		t.Fatal("addPlan with no PLAN.yaml returned nil error")
	}

	// Valid PLAN.yaml but missing TASKS.yaml.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, planFileName), "id: p\n")
	if err := b.addPlan(dir, "p"); err == nil {
		t.Fatal("addPlan with no TASKS.yaml returned nil error")
	}

	// Malformed YAML in PLAN.yaml.
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, planFileName), "id: [unclosed\n")
	if err := b.addPlan(dir2, "p"); err == nil {
		t.Fatal("addPlan with malformed PLAN.yaml returned nil error")
	}
}

// TestLoadParseErrors covers loadPlan/loadTasks read + parse error paths.
func TestLoadParseErrors(t *testing.T) {
	if _, err := loadPlan(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("loadPlan missing file: nil error")
	}
	if _, err := loadTasks(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("loadTasks missing file: nil error")
	}
	bad := filepath.Join(t.TempDir(), tasksFileName)
	writeFile(t, bad, "tasks: [unclosed\n")
	if _, err := loadTasks(bad); err == nil {
		t.Fatal("loadTasks malformed: nil error")
	}
}

// TestEffectivePlanIDFallback covers the dir-name fallback when PLAN.yaml omits id.
func TestEffectivePlanIDFallback(t *testing.T) {
	if got := effectivePlanID(&workflow.CanonicalPlan{}, tDirX); got != tDirX {
		t.Fatalf("effectivePlanID fallback = %q, want dir-x", got)
	}
	if got := effectivePlanID(&workflow.CanonicalPlan{ID: tReal}, tDirX); got != tReal {
		t.Fatalf("effectivePlanID = %q, want real", got)
	}
}

// TestParseSpecFrontmatterMissing covers the unreadable-file branch (empty
// values, no error) — the deliberate freeform-frontmatter gap.
func TestParseSpecFrontmatterMissing(t *testing.T) {
	title, status := parseSpecFrontmatter(filepath.Join(t.TempDir(), "nope.md"))
	if title != "" || status != "" {
		t.Fatalf("missing frontmatter = (%q,%q), want empty", title, status)
	}
}

// TestListSubdirsWithFileError covers the unreadable-dir branch.
func TestListSubdirsWithFileError(t *testing.T) {
	if _, err := listSubdirsWithFile(filepath.Join(t.TempDir(), "missing"), specFileName); err == nil {
		t.Fatal("listSubdirsWithFile on missing dir: nil error")
	}
}

// TestFileExists covers the three-way fileExists contract: present (true, no
// log), genuinely absent (false, no log), and permission-denied (false, but
// now logged so the swallow is distinguishable from legitimate absence).
// os.Stat needs execute permission on the PARENT of the target, not on the
// target itself, so the permission-denied case chmods the parent dir.
func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "design.md")
	writeFile(t, present, "# spec\n")
	if !fileExists(present) {
		t.Error("fileExists(present file) = false, want true")
	}

	if fileExists(filepath.Join(dir, "nope.md")) {
		t.Error("fileExists(missing file) = true, want false")
	}

	parent := t.TempDir()
	sub := filepath.Join(parent, "spec-id")
	target := filepath.Join(sub, "design.md")
	writeFile(t, target, "# spec\n")
	testutil.MakeDirUnreadable(t, parent)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if fileExists(target) {
		t.Error("fileExists(permission-denied file) = true, want false")
	}
	if !bytes.Contains(buf.Bytes(), []byte("stat failed")) {
		t.Errorf("expected a warning log for the permission-denied stat, got %q", buf.String())
	}
}

// TestImpactRadiusMethod covers the registry.Adapter ImpactRadius method (the
// identity passthrough) and the SchemaInfo accessor.
func TestImpactRadiusMethod(t *testing.T) {
	a := New()
	res, err := a.ImpactRadius(registry.ImpactRequest{ChangedIDs: []string{"x", "y"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.IDs) != 2 || res.IDs[0] != "x" || res.IDs[1] != "y" {
		t.Fatalf("ImpactRadius ids = %v, want [x y]", res.IDs)
	}
	if len(a.SchemaInfo().NoteFields) != 3 {
		t.Fatalf("SchemaInfo note types = %d, want 3", len(a.SchemaInfo().NoteFields))
	}
}

// failStore is a Store whose writes always fail, to exercise Ingest's
// WriteNotes/WriteEdges error branches without a real backend.
type failStore struct{ failEdges bool }

func (f failStore) WriteNotes(token sdk.Token, ns string, notes []sdk.Note) error {
	if f.failEdges {
		return nil // let notes succeed so the edges-write branch is reached
	}
	return errFailStore
}
func (f failStore) WriteEdges(sdk.Token, string, []sdk.Edge) error { return errFailStore }
func (f failStore) Notes(sdk.Token, string) ([]sdk.Note, error)    { return nil, nil }
func (f failStore) Edges(sdk.Token, string) ([]sdk.Edge, error)    { return nil, nil }

var errFailStore = errStore("write failed")

type errStore string

func (e errStore) Error() string { return string(e) }

// TestIngestWriteErrors covers Ingest's WriteNotes and WriteEdges failure paths.
func TestIngestWriteErrors(t *testing.T) {
	a := New()
	wf := buildTinyTree(t)
	if _, err := a.Ingest(failStore{}, wf); err == nil {
		t.Fatal("Ingest with failing WriteNotes returned nil error")
	}
	if _, err := a.Ingest(failStore{failEdges: true}, wf); err == nil {
		t.Fatal("Ingest with failing WriteEdges returned nil error")
	}
}

// TestListSubdirsSkipsFiles covers the non-directory branch: a stray FILE in the
// specs root is skipped rather than treated as a spec dir.
func TestListSubdirsSkipsFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stray.txt"), "not a dir")
	writeFile(t, filepath.Join(root, tReal, specFileName), "# Real\n")
	got, err := listSubdirsWithFile(root, specFileName)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != tReal {
		t.Fatalf("listSubdirsWithFile = %v, want [real]", got)
	}
}

// TestMustFromYAMLPanics drives the panic seam New uses, with an invalid
// schema, so the panic branch is exercised. New itself can only be called with
// the valid embed, so this is the only way to reach the panic.
func TestMustFromYAMLPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("mustFromYAML did not panic on an invalid schema")
		}
	}()
	mustFromYAML([]byte("name: x\n")) // missing version -> LoadSchema error -> panic
}

// schemaMissingNamedEdges has a valid impact_radius query but omits the edge
// types the named trace queries need, so newFromYAML reaches the named-query
// compile-error branch (after the impact query compiles).
const schemaMissingNamedEdges = `
name: bad
version: 1.0.0
note_types:
  - name: task
    fields:
      - { name: task_key, type: string }
impact_radius:
  query: |-
    MATCH (t:task) RETURN t.task_key
  max_depth: 1
`

// schemaUntypedRef has an untyped ref field, which NewSchemaInfo (via
// buildSchemaInfo) rejects — covering the schema-info error branch.
const schemaUntypedRef = `
name: bad
version: 1.0.0
note_types:
  - name: task
    fields:
      - { name: ptr, type: ref }
impact_radius:
  query: RETURN $x
  max_depth: 1
`

// TestNewFromYAMLDeepErrors covers the buildSchemaInfo and named-query
// compile-error branches of newFromYAML/compileQueries that the shipped embed
// never hits.
func TestNewFromYAMLDeepErrors(t *testing.T) {
	if _, err := newFromYAML([]byte(schemaUntypedRef)); err == nil {
		t.Fatal("newFromYAML(untyped ref) returned nil error")
	}
	if _, err := newFromYAML([]byte(schemaMissingNamedEdges)); err == nil {
		t.Fatal("newFromYAML(missing named-query edges) returned nil error")
	}
}

// hasEdge reports whether view contains an edge of the given type/from/to.
func hasEdge(view sdk.NamespaceView, edgeType, from, to string) bool {
	for _, e := range view.Edges {
		if e.Type == edgeType && e.From == from && e.To == to {
			return true
		}
	}
	return false
}
