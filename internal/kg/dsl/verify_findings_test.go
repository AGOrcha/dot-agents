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
	pairs := map[string]bool{}
	for _, r := range rows {
		pairs[r["a.id"].(string)+"->"+r["b.id"].(string)] = true
	}
	// Reachable pairs within 3 hops: l1->l2, l1->l3, l2->l3.
	if !pairs["l1->l2"] || !pairs["l1->l3"] || !pairs["l2->l3"] {
		t.Fatalf("both-bound var-length: missing reachable pairs, got %v", pairs)
	}
	if pairs["l1->l1"] || pairs["l3->l1"] {
		t.Fatalf("both-bound var-length: spurious unconnected pair, got %v", pairs)
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

// TestEdgeUntypedEndpointAccepted confirms a re-referenced bound alias with no
// `:type` (e.g. `(changed)` in the impact_radius reverse join) is accepted —
// its type was fixed at its binding MATCH.
func TestEdgeUntypedEndpointAccepted(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: tControl, Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
		{Name: tFinding, Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
	}, []dsl.EdgeTypeDecl{{Name: "affects", From: tFinding, To: tControl}}, 2)
	if err != nil {
		t.Fatalf(errSchemaFmt, err)
	}
	if _, err := dsl.ParseWithSchema("MATCH (changed:control) OPTIONAL MATCH (f:finding)-[:affects]->(changed) RETURN changed.id, f.id", info); err != nil {
		t.Fatalf("untyped re-referenced endpoint should load: %v", err)
	}
}
