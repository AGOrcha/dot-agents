package dsl_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// Literals reused across the evaluator-branch tests, hoisted so this file does
// not repeat a string often enough to trip S1192.
const (
	tThing = "thing"
	fScore = "score"
)

// branchSchema is a single-type schema with mixed scalar fields for the
// evaluator-branch tests (string equality, bare-ref WHERE, min over gaps,
// STARTS_WITH on a non-string, stale-through-null-ref).
func branchSchema(t *testing.T) dsl.SchemaInfo {
	t.Helper()
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: tThing, Fields: []dsl.FieldDecl{
			{Name: "tag", Type: tString},
			{Name: fScore, Type: "int"},
			{Name: "ref", Type: "ref<thing>"},
		}},
	}, nil, 2)
	if err != nil {
		t.Fatalf(errSchemaFmt, err)
	}
	return info
}

func evalBranch(t *testing.T, src string, view sdk.NamespaceView, params map[string]any) []sdk.Row {
	t.Helper()
	q, err := dsl.ParseWithSchema(src, branchSchema(t))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	rows, err := dsl.Eval(q, view, params)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return rows
}

// TestStringInequality drives compare.valuesEqual's non-numeric branch:
// `tag != $t` on string-valued fields (asFloats fails → falls to `left==right`).
func TestStringInequality(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a", Type: tThing, Fields: map[string]any{"tag": "x"}},
		{ID: "b", Type: tThing, Fields: map[string]any{"tag": "y"}},
	}}
	rows := evalBranch(t, "MATCH (n:thing) WHERE n.tag != $t RETURN n.id", view, map[string]any{"t": "x"})
	if len(rows) != 1 || rows[0]["n.id"] != "b" {
		t.Fatalf("string !=: expected only b, got %v", rows)
	}
}

// TestBareAliasInWhere drives resolveFieldRef's bare-alias branch: a WHERE
// predicate on the bare alias (no `.field`) reads the node's own id (§5.4.1
// rule 1), so `WHERE n = $id` filters by node identity.
func TestBareAliasInWhere(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a", Type: tThing, Fields: map[string]any{"tag": "x"}},
		{ID: "b", Type: tThing, Fields: map[string]any{"tag": "y"}},
	}}
	rows := evalBranch(t, "MATCH (n:thing) WHERE n = $target RETURN n.id", view, map[string]any{"target": "b"})
	if len(rows) != 1 || rows[0]["n.id"] != "b" {
		t.Fatalf("bare-alias WHERE: expected only b (id match), got %v", rows)
	}
}

// TestMinOverMissingField drives minMax's skip branch: min over a field that is
// absent on some rows skips them and returns the min of the present values.
func TestMinOverMissingField(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a", Type: tThing, Fields: map[string]any{fScore: float64(7)}},
		{ID: "b", Type: tThing, Fields: map[string]any{}}, // no score → skipped
		{ID: "c", Type: tThing, Fields: map[string]any{fScore: float64(3)}},
	}}
	rows := evalBranch(t, "MATCH (n:thing) RETURN min(n.score)", view, nil)
	if len(rows) != 1 || rows[0]["min"] != float64(3) {
		t.Fatalf("min over gaps: expected 3 (skipping the missing-score row), got %v", rows)
	}
}

// TestStartsWithNonStringField drives evalStartsWith's non-string operand
// branch: STARTS_WITH on an int-valued field yields no match (not a string).
func TestStartsWithNonStringField(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a", Type: tThing, Fields: map[string]any{fScore: float64(42)}},
	}}
	rows := evalBranch(t, "MATCH (n:thing) WHERE STARTS_WITH(n.score, $p) RETURN n.id", view, map[string]any{"p": "4"})
	if len(rows) != 0 {
		t.Fatalf("STARTS_WITH on int field: expected 0 rows, got %d", len(rows))
	}
}

// TestStaleSelectorOnFieldlessNote drives staleSubfield's no-fields branch: a
// stale selector on a note with nil Fields resolves to NULL (fresh), so the
// predicate does not hold.
func TestStaleSelectorOnFieldlessNote(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a", Type: tThing, Fields: nil}, // no fields → fresh
		{ID: "b", Type: tThing, Fields: map[string]any{"stale": map[string]any{"reason": vEnvironmental}}}, // stale
	}}
	rows := evalBranch(t, "MATCH (n:thing) WHERE n.stale.reason = $r RETURN n.id", view, map[string]any{"r": vEnvironmental})
	if len(rows) != 1 || rows[0]["n.id"] != "b" {
		t.Fatalf("stale on fieldless note: expected only b, got %v", rows)
	}
}

// TestStaleThroughNullRef drives a stale selector reached through a NULL ref:
// the ref does not resolve, so the whole selector resolves to NULL.
func TestStaleThroughNullRef(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a", Type: tThing, Fields: map[string]any{}}, // ref absent → NULL
	}}
	rows := evalBranch(t, "MATCH (n:thing) WHERE n.ref.stale.reason = $r RETURN n.id", view, map[string]any{"r": vEnvironmental})
	if len(rows) != 0 {
		t.Fatalf("stale through null ref: expected 0 rows, got %d", len(rows))
	}
}

// TestCoalesceAllNilWhere drives evalCoalesce's all-nil return in WHERE: when
// every coalesce arg is an absent param, it folds to nil and the comparison
// against a present field does not hold.
func TestCoalesceAllNilWhere(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a", Type: tThing, Fields: map[string]any{fScore: float64(5)}},
	}}
	// coalesce($x, $y) with both params absent → nil; `score >= nil` is a type
	// mismatch and does not hold, so no rows.
	rows := evalBranch(t, "MATCH (n:thing) WHERE n.score >= coalesce($x, $y) RETURN n.id", view, nil)
	if len(rows) != 0 {
		t.Fatalf("coalesce all-nil: expected 0 rows (nil threshold), got %d", len(rows))
	}
}
