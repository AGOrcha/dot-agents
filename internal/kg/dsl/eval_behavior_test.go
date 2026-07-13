package dsl_test

import (
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// graphView is a small location graph for variable-length and aggregate eval.
func graphView() sdk.NamespaceView {
	return sdk.NamespaceView{
		Notes: []sdk.Note{
			{ID: "l1", Type: tLocation, Fields: map[string]any{fRegion: "a"}},
			{ID: "l2", Type: tLocation, Fields: map[string]any{fRegion: "b"}},
			{ID: "l3", Type: tLocation, Fields: map[string]any{fRegion: "c"}},
			{ID: "c1", Type: tCharacter, Fields: map[string]any{fStatedLocation: "l1"}},
			{ID: "c2", Type: tCharacter, Fields: map[string]any{fStatedLocation: "l1"}},
			{ID: "f1", Type: tFunction, Fields: map[string]any{"path": "apps/web/main.go", "qualified_name": "main"}},
			{ID: "f2", Type: tFunction, Fields: map[string]any{"path": "libs/util/x.go", "qualified_name": "x"}},
		},
		Edges: []sdk.Edge{
			{Type: "connects_to", From: "l1", To: "l2"},
			{Type: "connects_to", From: "l2", To: "l3"},
			{Type: "home", From: "c1", To: "l1"},
			{Type: "home", From: "c2", To: "l1"},
		},
	}
}

func evalGraph(t *testing.T, src string, params map[string]any) []sdk.Row {
	t.Helper()
	q := mustParse(t, src)
	rows, err := dsl.Eval(q, graphView(), params)
	if err != nil {
		t.Fatalf("Eval(%q): %v", src, err)
	}
	return rows
}

// TestEvalVarLengthHopCount exercises the §5.1 variable-length BFS, asserting the
// minimum hop_count to each reachable destination (T3 behavior).
func TestEvalVarLengthHopCount(t *testing.T) {
	rows := evalGraph(t, "MATCH (a:location)-[:connects_to*1..3]->(b:location) RETURN b.id, hop_count", nil)
	got := map[string]int{}
	for _, r := range rows {
		id, _ := r["b.id"].(string)
		hc, _ := r["hop_count"].(int)
		if cur, ok := got[id]; !ok || hc < cur {
			got[id] = hc
		}
	}
	if got["l2"] != 1 || got["l3"] != 1 { // l3 reachable in 1 hop from l2 start
		t.Fatalf("var-length hop counts wrong: %v", got)
	}
}

// TestEvalCountAggregate exercises count(*) over edge-joined rows.
func TestEvalCountAggregate(t *testing.T) {
	rows := evalGraph(t, "MATCH (c:character)-[:home]->(l:location) RETURN count(*)", nil)
	if len(rows) != 1 || rows[0]["count"] != 2 {
		t.Fatalf("count(*): expected single row count=2, got %v", rows)
	}
}

// TestEvalInList exercises WHERE c.id IN $ids.
func TestEvalInList(t *testing.T) {
	rows := evalGraph(t, "MATCH (c:character) WHERE c.id IN $ids RETURN c.id", map[string]any{"ids": []string{"c1"}})
	if len(rows) != 1 || rows[0]["c.id"] != "c1" {
		t.Fatalf("IN: expected only c1, got %v", rows)
	}
}

// TestEvalStartsWith exercises the STARTS_WITH prefix predicate (T26 behavior).
func TestEvalStartsWith(t *testing.T) {
	rows := evalGraph(t, "MATCH (f:Function) WHERE STARTS_WITH(f.path, $root) RETURN f.qualified_name", map[string]any{"root": "apps/web/"})
	if len(rows) != 1 || rows[0]["f.qualified_name"] != "main" {
		t.Fatalf("STARTS_WITH: expected only main, got %v", rows)
	}
}

// TestEvalCoalesceParam exercises coalesce param normalization in WHERE (T23
// behavior): a nil optional param folds to the literal default.
func TestEvalCoalesceParam(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "e1", Type: "event", Fields: map[string]any{"session": float64(5)}},
		{ID: "e2", Type: "event", Fields: map[string]any{"session": float64(0)}},
	}}
	q := mustParse(t, "MATCH (e:event) WHERE e.session >= coalesce($min, 3) RETURN e.id")
	rows, err := dsl.Eval(q, view, nil) // $min absent → folds to 3
	if err != nil {
		t.Fatalf("coalesce eval: %v", err)
	}
	if len(rows) != 1 || rows[0]["e.id"] != "e1" {
		t.Fatalf("coalesce: expected only e1 (session 5 >= 3), got %v", rows)
	}
}

// TestApplyEnvTriggerTimeAfter fires a time_after env predicate and asserts the
// expired evidence is tagged stale (§7.2).
func TestApplyEnvTriggerTimeAfter(t *testing.T) {
	preds := []dsl.EnvPredicate{{NoteType: tEvidence, Kind: dsl.KindTimeAfter, Field: fExpiresAt}}
	notes := []sdk.Note{
		{ID: "ev-old", Type: tEvidence, Fields: map[string]any{fExpiresAt: "2026-01-01"}},
		{ID: "ev-new", Type: tEvidence, Fields: map[string]any{fExpiresAt: "2027-01-01"}},
	}
	now, _ := time.Parse("2006-01-02", "2026-06-01")
	tagged, err := dsl.ApplyEnvTrigger(preds, notes, dsl.EnvTrigger{Kind: dsl.KindTimeAfter, Now: now, TriggerID: "t1"})
	if err != nil {
		t.Fatalf("ApplyEnvTrigger: %v", err)
	}
	if len(tagged) != 1 || tagged[0].ID != "ev-old" {
		t.Fatalf("time_after: expected only ev-old tagged, got %v", tagged)
	}
	stale, _ := tagged[0].Fields[fStale].(map[string]any)
	if stale["reason"] != vEnvironmental {
		t.Fatalf("time_after: stale reason not set, got %v", stale)
	}
}

// TestApplyEnvTriggerWebhook fires a webhook env predicate and asserts the
// matching policy is tagged (§7.2).
func TestApplyEnvTriggerWebhook(t *testing.T) {
	preds := []dsl.EnvPredicate{{NoteType: tPolicy, Kind: dsl.KindWebhook, Endpoint: "policy.review_due"}}
	notes := []sdk.Note{{ID: idPol1, Type: tPolicy, Fields: map[string]any{"version": "2"}}}
	tagged, err := dsl.ApplyEnvTrigger(preds, notes, dsl.EnvTrigger{Kind: dsl.KindWebhook, Endpoint: "policy.review_due", TriggerID: "wh-1"})
	if err != nil {
		t.Fatalf("ApplyEnvTrigger webhook: %v", err)
	}
	if len(tagged) != 1 || tagged[0].ID != idPol1 {
		t.Fatalf("webhook: expected pol-1 tagged, got %v", tagged)
	}
}

// TestApplyEnvTriggerUnsupportedKind asserts an unknown trigger kind fails loud.
func TestApplyEnvTriggerUnsupportedKind(t *testing.T) {
	_, err := dsl.ApplyEnvTrigger(nil, nil, dsl.EnvTrigger{Kind: "module_version"})
	if err == nil {
		t.Fatal("expected unsupported env trigger kind to error")
	}
}
