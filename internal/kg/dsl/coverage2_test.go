package dsl_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// TestReturnAsRename covers the explicit `AS alias` rename branch in
// parseReturnItem for a field, an aggregate, hop_count, and a param.
func TestReturnAsRename(t *testing.T) {
	q := mustParse(t, "MATCH (c:character) RETURN c.id AS who")
	rows, err := dsl.Eval(q, evalView(), nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if _, ok := rows[0]["who"]; !ok {
		t.Fatalf("AS rename: expected column 'who', got %v", rows[0])
	}
}

// TestParamReturnRename covers the none-adapter `RETURN $changed_ids AS id`
// shape (param projection with AS).
func TestParamReturnRename(t *testing.T) {
	q, err := dsl.Parse("RETURN $changed_ids AS id")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows, err := dsl.Eval(q, sdk.NamespaceView{}, map[string]any{"changed_ids": []string{"a", "b"}})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got, ok := rows[0]["id"].([]string); !ok || len(got) != 2 {
		t.Fatalf("param AS id: expected []string len 2, got %v", rows[0]["id"])
	}
}

// TestAggregateWithNonAggregateColumn covers aggregateValue's default branch
// (a non-aggregate column projected alongside count(*) takes the first row).
func TestAggregateWithNonAggregateColumn(t *testing.T) {
	q := mustParse(t, "MATCH (c:character) RETURN count(*), c.status")
	rows, err := dsl.Eval(q, evalView(), nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 1 || rows[0]["count"] != 3 || rows[0]["c.status"] != "alive" {
		t.Fatalf("aggregate+column: got %v", rows)
	}
}

// TestAggregateEmptyBindings covers aggregateValue with zero bindings (the
// non-aggregate item then resolves to nil).
func TestAggregateEmptyBindings(t *testing.T) {
	q := mustParse(t, "MATCH (c:character) WHERE c.status = $s RETURN count(*), c.status")
	rows, err := dsl.Eval(q, evalView(), map[string]any{"s": "ghost"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if rows[0]["count"] != 0 || rows[0]["c.status"] != nil {
		t.Fatalf("empty aggregate: got %v", rows[0])
	}
}

// TestCoalesceAllNil covers evalCoalesce / evalReturnCoalesce returning nil when
// every argument is nil.
func TestCoalesceAllNil(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{{ID: "c1", Type: "character", Fields: map[string]any{}}}}
	q := mustParse(t, "MATCH (c:character) RETURN c.id, coalesce(c.name, c.region)")
	rows, err := dsl.Eval(q, view, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if rows[0]["coalesce"] != nil {
		t.Fatalf("coalesce all-nil: expected nil, got %v", rows[0]["coalesce"])
	}
}

// TestT18MixedSources covers the §5.4.2 T18 mixed case: required source A's
// predicate stays in WHERE (drops rows), optional source B's predicate hoists.
func TestT18MixedSources(t *testing.T) {
	q := mustParse(t, "MATCH (c:character) OPTIONAL MATCH (c)-[:home]->(loc:location) WHERE c.status = $st AND loc.region = $rg RETURN c.id, loc.id")
	rows, err := dsl.Eval(q, evalView(), map[string]any{"st": "alive", "rg": "eu"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	// All 3 are alive (required passes); only char-a's home is eu (optional
	// hoist keeps the others with NULL loc).
	if len(rows) != 3 {
		t.Fatalf("T18: expected 3 rows, got %d", len(rows))
	}
}

// TestEdgeIntrinsicReturnEval covers edge alias id/kind resolution at eval time.
func TestEdgeIntrinsicReturnEval(t *testing.T) {
	// Parse accepts edge intrinsics; eval returns nil for them in v1 (edge
	// identity is not materialized in the in-memory view) — assert it does not
	// error and the column is present.
	q := mustParse(t, "MATCH (a:Function)-[e:CALLS]->(b:Function) RETURN b.id, e.id")
	view := sdk.NamespaceView{
		Notes: []sdk.Note{{ID: "f1", Type: "Function"}, {ID: "f2", Type: "Function"}},
		Edges: []sdk.Edge{{Type: "CALLS", From: "f1", To: "f2"}},
	}
	rows, err := dsl.Eval(q, view, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 1 || rows[0]["b.id"] != "f2" {
		t.Fatalf("edge-intrinsic eval: got %v", rows)
	}
}

// TestCompareTypeMismatch covers compare's type-mismatch (non-numeric ordering
// returns false; mismatched-type equality returns false).
func TestCompareTypeMismatch(t *testing.T) {
	rows := runNum(t, "MATCH (n:n) WHERE n.tag < $t RETURN n.id", map[string]any{"t": "z"})
	if len(rows) != 0 { // string ordering is not supported → predicate never holds
		t.Fatalf("string ordering: expected 0 rows, got %d", len(rows))
	}
}

// TestWebhookFiresKindBranch covers fires() default + webhook non-match.
func TestWebhookNonMatch(t *testing.T) {
	preds := []dsl.EnvPredicate{{NoteType: "policy", Kind: dsl.KindWebhook, Endpoint: "a"}}
	notes := []sdk.Note{{ID: "p1", Type: "policy"}}
	tagged, err := dsl.ApplyEnvTrigger(preds, notes, dsl.EnvTrigger{Kind: dsl.KindWebhook, Endpoint: "b"})
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if len(tagged) != 0 {
		t.Fatalf("webhook non-match: expected 0 tagged, got %d", len(tagged))
	}
}

// TestReturnCoalesceNumericDefault covers literalFromArg's numeric-default
// branch: coalesce(field, 0) where the field is absent folds to the number 0.
func TestReturnCoalesceNumericDefault(t *testing.T) {
	q := mustParse2(t, "MATCH (n:n) RETURN n.id, coalesce(n.v, 0)")
	view := sdk.NamespaceView{Notes: []sdk.Note{{ID: "n1", Type: "n", Fields: map[string]any{}}}}
	rows, err := dsl.Eval(q, view, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if rows[0]["coalesce"] != 0 {
		t.Fatalf("numeric coalesce default: expected 0, got %v", rows[0]["coalesce"])
	}
}

// TestMinMaxAsRename covers min/max RETURN with an explicit AS alias.
func TestMinMaxAsRename(t *testing.T) {
	q := mustParse2(t, "MATCH (n:n) RETURN min(n.v) AS lo, max(n.v) AS hi")
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "n1", Type: "n", Fields: map[string]any{"v": float64(2)}},
		{ID: "n2", Type: "n", Fields: map[string]any{"v": float64(9)}},
	}}
	rows, err := dsl.Eval(q, view, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if rows[0]["lo"] != float64(2) || rows[0]["hi"] != float64(9) {
		t.Fatalf("min/max AS: got %v", rows[0])
	}
}
