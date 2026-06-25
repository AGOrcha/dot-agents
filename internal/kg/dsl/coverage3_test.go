package dsl_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// TestResolveEdgeCases drives the remaining eval resolution branches:
//   - a ref field whose value is not a string (followRef non-string path)
//   - a stale selector on a note whose stale field is not a map
//   - a stale selector with no subfield
func TestResolveEdgeCases(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "a", Fields: []dsl.FieldDecl{{Name: "r", Type: "ref<b>"}}},
		{Name: "b", Fields: []dsl.FieldDecl{{Name: "k", Type: "string"}}},
	}, nil, 2)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	view := sdk.NamespaceView{Notes: []sdk.Note{
		// r is a number, not a ref id string → followRef yields NULL.
		{ID: "a1", Type: "a", Fields: map[string]any{"r": float64(7)}},
		// stale is a non-map value → staleSubfield yields NULL.
		{ID: "a2", Type: "a", Fields: map[string]any{fStale: "oops"}},
	}}
	q := mustParseInfo(t, "MATCH (a:a) WHERE a.r.k = $k RETURN a.id", info)
	rows, err := dsl.Eval(q, view, map[string]any{"k": "x"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("non-string ref: expected 0 rows, got %d", len(rows))
	}
	q2 := mustParseInfo(t, "MATCH (a:a) WHERE a.stale.reason = $r RETURN a.id", info)
	rows2, err := dsl.Eval(q2, view, map[string]any{"r": "environmental"})
	if err != nil {
		t.Fatalf("eval2: %v", err)
	}
	if len(rows2) != 0 {
		t.Fatalf("non-map stale: expected 0 rows, got %d", len(rows2))
	}
}

// TestStaleSelectorNoSubfield covers staleSubfield with an empty rest path
// (`alias.stale` with nothing after).
func TestStaleSelectorNoSubfield(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "a", Fields: []dsl.FieldDecl{{Name: "k", Type: "string"}}},
	}, nil, 2)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a1", Type: "a", Fields: map[string]any{fStale: map[string]any{"reason": "x"}}},
	}}
	q := mustParseInfo(t, "MATCH (a:a) RETURN a.id, a.stale", info)
	rows, err := dsl.Eval(q, view, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if rows[0]["a.stale"] != nil {
		t.Fatalf("bare stale selector should resolve NULL, got %v", rows[0]["a.stale"])
	}
}

// TestNumberEqualityIntFloat covers valuesEqual numeric coercion (= on a field
// stored as int vs a float param).
func TestNumberEqualityIntFloat(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "n1", Type: "n", Fields: map[string]any{"v": 3}}, // int
	}}
	q := mustParse2(t, "MATCH (n:n) WHERE n.v = $t RETURN n.id")
	rows, err := dsl.Eval(q, view, map[string]any{"t": float64(3)})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("int/float eq: expected 1 row, got %d", len(rows))
	}
}

// mustParse2 parses with a minimal numeric single-type schema.
func mustParse2(t *testing.T, src string) *dsl.Query {
	t.Helper()
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "n", Fields: []dsl.FieldDecl{{Name: "v", Type: "int"}}},
	}, nil, 2)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	q, err := dsl.ParseWithSchema(src, info)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return q
}

// TestStartsWithMissingParam covers evalStartsWith when the param is absent
// (right side nil → not a string → no match).
func TestStartsWithMissingParam(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "f1", Type: "Function", Fields: map[string]any{"path": "x"}},
	}}
	q := mustParse(t, "MATCH (f:Function) WHERE STARTS_WITH(f.path, $root) RETURN f.id")
	rows, err := dsl.Eval(q, view, nil) // $root absent
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("STARTS_WITH missing param: expected 0 rows, got %d", len(rows))
	}
}

// TestEvalNilFields covers fieldValue's nil-Fields branch and a field whose
// stored value is nil.
func TestEvalNilFields(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "n1", Type: "n", Fields: nil},
		{ID: "n2", Type: "n", Fields: map[string]any{"v": nil}},
	}}
	q := mustParse2(t, "MATCH (n:n) WHERE n.v = $t RETURN n.id")
	rows, err := dsl.Eval(q, view, map[string]any{"t": float64(1)})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("nil fields: expected 0 rows, got %d", len(rows))
	}
}
