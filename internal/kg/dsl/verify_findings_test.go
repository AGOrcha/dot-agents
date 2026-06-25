package dsl_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// Literals reused across the both-bound / edge-validation regression tests,
// hoisted so no file repeats one often enough to trip S1192.
const (
	edgeConnects = "connects_to"
	errSchemaFmt = "schema: %v"
)

// locSchema is a single-type location graph with a self edge, used by the
// both-endpoints-bound join regression tests.
func locSchema(t *testing.T) dsl.SchemaInfo {
	t.Helper()
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: tLocation, Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
	}, []dsl.EdgeTypeDecl{{Name: edgeConnects, From: tLocation, To: tLocation}}, 3)
	if err != nil {
		t.Fatalf(errSchemaFmt, err)
	}
	return info
}

// triLocView is three locations with a single l1->l2 edge.
func triLocView() sdk.NamespaceView {
	return sdk.NamespaceView{
		Notes: []sdk.Note{
			{ID: "l1", Type: tLocation}, {ID: "l2", Type: tLocation}, {ID: "l3", Type: tLocation},
		},
		Edges: []sdk.Edge{{Type: edgeConnects, From: "l1", To: "l2"}},
	}
}

// TestBothBoundEdgeFilters is the regression for the both-endpoints-bound join:
// binding A and B independently then requiring an edge A->B must FILTER the
// pre-bound pairs (keep only connected ones), never overwrite an endpoint or
// multiply rows. Only (l1,l2) has an edge → exactly one row.
func TestBothBoundEdgeFilters(t *testing.T) {
	q, err := dsl.ParseWithSchema("MATCH (a:location) MATCH (b:location) MATCH (a)-[:connects_to]->(b) RETURN a.id, b.id", locSchema(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows, err := dsl.Eval(q, triLocView(), nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("both-bound: expected exactly 1 connected pair, got %d rows: %v", len(rows), rows)
	}
	if rows[0]["a.id"] != "l1" || rows[0]["b.id"] != "l2" {
		t.Fatalf("both-bound: expected a=l1,b=l2 (the only edge), got %v", rows[0])
	}
}

// TestBothBoundVarLength covers the both-bound filter for a variable-length edge
// pattern: l1 reaches l3 via l1->l2->l3 only when the edge l2->l3 exists.
func TestBothBoundVarLength(t *testing.T) {
	q, err := dsl.ParseWithSchema("MATCH (a:location) MATCH (b:location) MATCH (a)-[:connects_to*1..3]->(b) RETURN a.id, b.id", locSchema(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	view := triLocView()
	view.Edges = append(view.Edges, sdk.Edge{Type: edgeConnects, From: "l2", To: "l3"})
	rows, err := dsl.Eval(q, view, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	// Count each (a,b) pair to assert EXACT multiplicity, not just set
	// membership: a broken forward-rebind that yields the right pairs WITH
	// duplicate rows (cross-product artifact) must fail here. The only
	// reachable pairs within 3 hops are l1->l2, l1->l3, l2->l3 — each exactly
	// once (both endpoints are pre-bound, so the join filters, never multiplies).
	counts := map[string]int{}
	for _, r := range rows {
		counts[r["a.id"].(string)+"->"+r["b.id"].(string)]++
	}
	want := map[string]int{"l1->l2": 1, "l1->l3": 1, "l2->l3": 1}
	if len(rows) != len(want) {
		t.Fatalf("both-bound var-length: expected exactly %d rows (no duplicates/cross-product), got %d: %v", len(want), len(rows), rows)
	}
	for pair, n := range want {
		if counts[pair] != n {
			t.Fatalf("both-bound var-length: pair %q expected %d row(s), got %d (counts=%v)", pair, n, counts[pair], counts)
		}
	}
	for pair := range counts {
		if want[pair] == 0 {
			t.Fatalf("both-bound var-length: spurious/unconnected pair %q present (counts=%v)", pair, counts)
		}
	}
}

// TestEdgeTypeUnknownRejected rejects an undeclared edge type at load (§5.1).
func TestEdgeTypeUnknownRejected(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: tControl, Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
	}, nil, 2)
	if err != nil {
		t.Fatalf(errSchemaFmt, err)
	}
	if _, err := dsl.ParseWithSchema("MATCH (c:control)-[:nonexistent]->(d:control) RETURN c.id", info); err == nil {
		t.Fatal("unknown edge type must be rejected at load")
	}
}

// TestEdgeWrongDirectionRejected rejects a wrong-direction edge MATCH at load:
// affects is finding->control, so control->finding must fail.
func TestEdgeWrongDirectionRejected(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: tControl, Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
		{Name: tFinding, Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
	}, []dsl.EdgeTypeDecl{{Name: "affects", From: tFinding, To: tControl}}, 2)
	if err != nil {
		t.Fatalf(errSchemaFmt, err)
	}
	if _, err := dsl.ParseWithSchema("MATCH (c:control)-[:affects]->(f:finding) RETURN c.id", info); err == nil {
		t.Fatal("wrong-direction edge MATCH must be rejected at load")
	}
	// The correct direction loads.
	if _, err := dsl.ParseWithSchema("MATCH (f:finding)-[:affects]->(c:control) RETURN c.id", info); err != nil {
		t.Fatalf("correct-direction edge MATCH should load: %v", err)
	}
}

// dirSchema declares affects: finding->control plus a risk type, for the
// bound-alias direction tests.
func dirSchema(t *testing.T) dsl.SchemaInfo {
	t.Helper()
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: tControl, Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
		{Name: tFinding, Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
		{Name: "risk", Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
	}, []dsl.EdgeTypeDecl{{Name: "affects", From: tFinding, To: tControl}}, 2)
	if err != nil {
		t.Fatalf(errSchemaFmt, err)
	}
	return info
}

// TestEdgeUntypedEndpointAccepted confirms a re-referenced bound alias with no
// `:type` (e.g. `(changed)` in the impact_radius reverse join) is accepted when
// its binding type matches the declared edge endpoint — affects.to == control.
func TestEdgeUntypedEndpointAccepted(t *testing.T) {
	if _, err := dsl.ParseWithSchema("MATCH (changed:control) OPTIONAL MATCH (f:finding)-[:affects]->(changed) RETURN changed.id, f.id", dirSchema(t)); err != nil {
		t.Fatalf("untyped re-referenced endpoint matching the declared edge type should load: %v", err)
	}
}

// TestEdgeBoundAliasWrongDirectionRejected is the #3-tightening regression: an
// untyped reverse-join endpoint whose BINDING type contradicts the declared
// edge endpoint must be rejected at load. `changed` is bound to `risk` but
// `affects.to` is `control`, so the reverse-join shape is wrong-direction even
// though `(changed)` carries no inline type — resolution via q.aliasType catches
// it.
func TestEdgeBoundAliasWrongDirectionRejected(t *testing.T) {
	if _, err := dsl.ParseWithSchema("MATCH (changed:risk) OPTIONAL MATCH (f:finding)-[:affects]->(changed) RETURN changed.id", dirSchema(t)); err == nil {
		t.Fatal("bound-alias wrong-direction (changed:risk vs affects.to=control) must be rejected at load")
	}
}

// TestEdgeBoundAliasForwardWrongDirRejected covers the symmetric forward case:
// a bound source alias of the wrong type for the edge's `from`.
func TestEdgeBoundAliasForwardWrongDirRejected(t *testing.T) {
	// `changed` is bound to risk; affects.from is finding. The forward shape
	// (changed)-[:affects]->(c:control) is wrong on the `from` side.
	if _, err := dsl.ParseWithSchema("MATCH (changed:risk) MATCH (changed)-[:affects]->(c:control) RETURN c.id", dirSchema(t)); err == nil {
		t.Fatal("bound-alias forward wrong-direction (changed:risk vs affects.from=finding) must be rejected at load")
	}
}
