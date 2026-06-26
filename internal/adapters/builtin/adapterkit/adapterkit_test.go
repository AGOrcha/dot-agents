package adapterkit_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/adapterkit"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

const (
	ntTask    = "task"
	fTaskKey  = "task_key"
	tString   = "string"
	impactSrc = "MATCH (t:task) RETURN t.task_key"
)

// goodSchema is a minimal valid schema: one note type with a scalar field and a
// one-hop impact query.
func goodSchema() registry.Schema {
	return registry.Schema{
		Name:    "kit",
		Version: "1.0.0",
		NoteTypes: []registry.NoteType{
			{Name: ntTask, Fields: []registry.FieldSpec{{Name: fTaskKey, Type: tString}}},
		},
		// One edge so the edge-translation loop is exercised too.
		EdgeTypes: []registry.EdgeType{
			{Name: "depends_on", From: ntTask, To: ntTask},
		},
		ImpactRadius: registry.ImpactRadius{Query: impactSrc, MaxDepth: 1},
	}
}

func TestBuildSchemaInfo(t *testing.T) {
	info, err := adapterkit.BuildSchemaInfo(goodSchema())
	if err != nil {
		t.Fatalf("BuildSchemaInfo: %v", err)
	}
	if len(info.NoteFields[ntTask]) != 1 {
		t.Fatalf("note fields = %d, want 1", len(info.NoteFields[ntTask]))
	}
	if info.MaxDepth != 1 {
		t.Fatalf("max depth = %d, want 1", info.MaxDepth)
	}
}

// TestBuildSchemaInfoError covers the NewSchemaInfo failure branch: an untyped
// `ref` field is rejected.
func TestBuildSchemaInfoError(t *testing.T) {
	s := goodSchema()
	s.NoteTypes[0].Fields = []registry.FieldSpec{{Name: "ptr", Type: "ref"}}
	if _, err := adapterkit.BuildSchemaInfo(s); err == nil {
		t.Fatal("BuildSchemaInfo(untyped ref) returned nil error")
	}
}

func TestCompileQueries(t *testing.T) {
	info, err := adapterkit.BuildSchemaInfo(goodSchema())
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]string{"all_tasks": impactSrc}
	impact, compiled, err := adapterkit.CompileQueries(impactSrc, named, info)
	if err != nil {
		t.Fatalf("CompileQueries: %v", err)
	}
	if impact == nil {
		t.Fatal("impact query is nil")
	}
	if _, ok := compiled["all_tasks"]; !ok {
		t.Fatal("named query all_tasks not compiled")
	}
}

// validSchemaYAML mirrors goodSchema as on-disk YAML so Load's parse path is
// exercised end-to-end.
const validSchemaYAML = `
name: kit
version: 1.0.0
note_types:
  - name: task
    fields:
      - { name: task_key, type: string }
edge_types:
  - { name: depends_on, from: task, to: task }
impact_radius:
  query: MATCH (t:task) RETURN t.task_key
  max_depth: 1
`

// TestLoad covers the full load+compile happy path.
func TestLoad(t *testing.T) {
	c, err := adapterkit.Load([]byte(validSchemaYAML), map[string]string{"all_tasks": impactSrc})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Schema.Name != "kit" {
		t.Fatalf("schema name = %q, want kit", c.Schema.Name)
	}
	if c.Impact == nil || c.Named["all_tasks"] == nil {
		t.Fatal("Load did not compile impact + named queries")
	}
}

// TestLoadErrors covers Load's three error branches: invalid schema YAML, an
// untyped-ref field (schema-info failure), and a bad named query.
func TestLoadErrors(t *testing.T) {
	cases := map[string]struct {
		yaml  string
		named map[string]string
	}{
		"bad-schema":  {"name: x\n", nil}, // missing version
		"untyped-ref": {"name: x\nversion: 1.0.0\nnote_types:\n  - name: t\n    fields:\n      - { name: p, type: ref }\nimpact_radius:\n  query: RETURN $x\n  max_depth: 1\n", nil},
		"bad-named":   {validSchemaYAML, map[string]string{"broken": "MATCH (x:nope) RETURN x.id"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := adapterkit.Load([]byte(tc.yaml), tc.named); err == nil {
				t.Fatalf("Load(%s) returned nil error", name)
			}
		})
	}
}

// TestCompileQueriesErrors covers both compile-error branches: a bad impact
// query, and a bad named query (after the impact query compiles).
func TestCompileQueriesErrors(t *testing.T) {
	info, err := adapterkit.BuildSchemaInfo(goodSchema())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapterkit.CompileQueries("NOT A QUERY", nil, info); err == nil {
		t.Fatal("CompileQueries with bad impact query returned nil error")
	}
	badNamed := map[string]string{"broken": "MATCH (x:unknown_type) RETURN x.id"}
	if _, _, err := adapterkit.CompileQueries(impactSrc, badNamed, info); err == nil {
		t.Fatal("CompileQueries with bad named query returned nil error")
	}
}
