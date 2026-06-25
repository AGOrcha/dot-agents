package dsl_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// TestParseErrorBranches drives the parser/lexer error returns that the happy-
// path conformance tests don't reach, so each rejection branch is exercised.
func TestParseErrorBranches(t *testing.T) {
	bad := []string{
		"MATCH n:n RETURN n.id",                                // node not parenthesized
		"MATCH (:n) RETURN n.id",                               // node missing alias
		"MATCH (n RETURN n.id",                                 // node missing close paren
		"MATCH (n:n)-(m:n) RETURN n.id",                        // edge missing brackets
		"MATCH (n:n)-[:e RETURN n.id",                          // edge missing close bracket
		"MATCH (n:n)-[:e]-(m:n) RETURN n.id",                   // edge missing arrow head
		"MATCH (n:n)-[123:e]->(m:n) RETURN n.id",               // edge alias not an ident
		"MATCH (n:n)-[e e]->(m:n) RETURN n.id",                 // edge body missing colon
		"MATCH (n:n)-[:e*x..3]->(m:n) RETURN n.id",             // var-length non-numeric lower
		"MATCH (n:n)-[:e*1.3]->(m:n) RETURN n.id",              // var-length missing second dot
		"MATCH (n:n)-[:e*1..x]->(m:n) RETURN n.id",             // var-length non-numeric upper
		"MATCH (n:n)-[:e*]->(m:n) RETURN n.id",                 // var-length unbounded
		"MATCH (n:n) WHERE RETURN n.id",                        // predicate missing field
		"MATCH (n:n) WHERE n.v ?? $t RETURN n.id",              // unknown operator char
		"MATCH (n:n) WHERE n.v = RETURN n.id",                  // value missing
		"MATCH (n:n) WHERE n.v = coalesce(n.v, 0) RETURN n.id", // coalesce on field (T24 variant)
		"MATCH (n:n) WHERE n.v = nope($t) RETURN n.id",         // unknown value function
		"MATCH (n:n) WHERE STARTS_WITH RETURN n.id",            // STARTS_WITH missing paren
		"MATCH (n:n) WHERE STARTS_WITH(n.v) RETURN n.id",       // STARTS_WITH missing comma/param
		"MATCH (n:n) WHERE n.id IN n.v RETURN n.id",            // IN without param
		"MATCH (n:n) RETURN count(n.v)",                        // count of a field (only count(*))
		"MATCH (n:n) RETURN min()",                             // min missing arg
		"MATCH (n:n) RETURN 123abc",                            // bad return token
	}
	for _, src := range bad {
		t.Run(src, func(t *testing.T) {
			if _, err := dsl.Parse(src); err == nil {
				t.Errorf("expected parse error for %q", src)
			}
		})
	}
}

// TestEvalNullAndOptionalBranches exercises eval paths for NULL bindings: an
// optional join with a missing neighbor, a dangling ref, and a stale selector
// on a fresh note.
func TestEvalNullAndOptionalBranches(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "a", Fields: []dsl.FieldDecl{{Name: "r", Type: "ref<b>"}}},
		{Name: "b", Fields: []dsl.FieldDecl{{Name: "k", Type: "string"}}},
	}, []dsl.EdgeTypeDecl{{Name: "to", From: "a", To: "b"}}, 2)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a1", Type: "a", Fields: map[string]any{"r": "missing"}}, // dangling ref
		{ID: "a2", Type: "a", Fields: map[string]any{}},               // null ref
	}}
	// Dangling ref traversal resolves to NULL → no rows pass the WHERE.
	q := mustParseInfo(t, "MATCH (a:a) WHERE a.r.k = $k RETURN a.id", info)
	rows, err := dsl.Eval(q, view, map[string]any{"k": "x"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("dangling/null ref: expected 0 rows, got %d", len(rows))
	}
	// Optional edge with no neighbor preserves the source row with NULL end.
	q2 := mustParseInfo(t, "MATCH (a:a) OPTIONAL MATCH (a)-[:to]->(b:b) RETURN a.id, b.id", info)
	rows2, err := dsl.Eval(q2, view, nil)
	if err != nil {
		t.Fatalf("eval2: %v", err)
	}
	if len(rows2) != 2 {
		t.Fatalf("optional no-neighbor: expected 2 rows, got %d", len(rows2))
	}
}

// mustParseInfo parses with an explicit schema info.
func mustParseInfo(t *testing.T, src string, info dsl.SchemaInfo) *dsl.Query {
	t.Helper()
	q, err := dsl.ParseWithSchema(src, info)
	if err != nil {
		t.Fatalf("ParseWithSchema(%q): %v", src, err)
	}
	return q
}

// TestVarLengthVarMinFilter covers varNeighbors' VarMin filter (hops < VarMin
// excluded) by requiring a 2-hop minimum.
func TestVarLengthVarMinFilter(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "n", Fields: []dsl.FieldDecl{{Name: "k", Type: "string"}}},
	}, []dsl.EdgeTypeDecl{{Name: "e", From: "n", To: "n"}}, 5)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	view := sdk.NamespaceView{
		Notes: []sdk.Note{
			{ID: "x", Type: "n"}, {ID: "y", Type: "n"}, {ID: "z", Type: "n"},
		},
		Edges: []sdk.Edge{{Type: "e", From: "x", To: "y"}, {Type: "e", From: "y", To: "z"}},
	}
	q := mustParseInfo(t, "MATCH (a:n)-[:e*2..3]->(b:n) RETURN b.id", info)
	rows, err := dsl.Eval(q, view, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	// Only z is ≥2 hops from x; y (1 hop) is filtered out by VarMin.
	if len(rows) != 1 || rows[0]["b.id"] != "z" {
		t.Fatalf("VarMin filter: expected only z, got %v", rows)
	}
}

// TestInvalidVarLengthBounds covers validateVarLength's invalid-bound branch
// (min > max) at schema validation.
func TestInvalidVarLengthBounds(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "n", Fields: []dsl.FieldDecl{{Name: "k", Type: "string"}}},
	}, []dsl.EdgeTypeDecl{{Name: "e", From: "n", To: "n"}}, 5)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := dsl.ParseWithSchema("MATCH (a:n)-[:e*3..2]->(b:n) RETURN b.id", info); err == nil {
		t.Fatal("expected inverted var-length bound to be rejected")
	}
}
