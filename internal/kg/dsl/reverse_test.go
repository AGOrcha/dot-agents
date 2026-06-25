package dsl_test

import (
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// reverseSchema models the §13.2 finding→control affects shape for reverse-join
// coverage: the new alias is the edge SOURCE and the bound alias is the END.
func reverseSchema(t *testing.T) dsl.SchemaInfo {
	t.Helper()
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: tControl, Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
		{Name: tFinding, Fields: []dsl.FieldDecl{{Name: "severity", Type: tString}}},
	}, []dsl.EdgeTypeDecl{{Name: "affects", From: tFinding, To: tControl}}, 2)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return info
}

func reverseView() sdk.NamespaceView {
	return sdk.NamespaceView{
		Notes: []sdk.Note{
			{ID: "c1", Type: tControl, Fields: map[string]any{"k": "x"}},
			{ID: "c2", Type: tControl, Fields: map[string]any{"k": "y"}}, // no finding
			{ID: "f1", Type: tFinding, Fields: map[string]any{"severity": "high"}},
		},
		Edges: []sdk.Edge{{Type: "affects", From: "f1", To: "c1"}},
	}
}

// TestReverseJoinOptional covers applyReverseMatch / extendReverseRow /
// reverseSources for the OPTIONAL case: all controls preserved, only c1 has a
// finding bound; c2 keeps a NULL finding.
func TestReverseJoinOptional(t *testing.T) {
	q, err := dsl.ParseWithSchema("MATCH (c:control) OPTIONAL MATCH (f:finding)-[:affects]->(c) RETURN c.id, f.id", reverseSchema(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows, err := dsl.Eval(q, reverseView(), nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("reverse optional: expected 2 control rows, got %d", len(rows))
	}
	var withFinding int
	for _, r := range rows {
		if r["f.id"] != nil {
			withFinding++
		}
	}
	if withFinding != 1 {
		t.Fatalf("reverse optional: expected exactly 1 row with a finding, got %d", withFinding)
	}
}

// TestReverseJoinRequired covers the required (non-optional) reverse join: only
// controls with a finding survive.
func TestReverseJoinRequired(t *testing.T) {
	q, err := dsl.ParseWithSchema("MATCH (c:control) MATCH (f:finding)-[:affects]->(c) RETURN c.id, f.id", reverseSchema(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows, err := dsl.Eval(q, reverseView(), nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 1 || rows[0]["c.id"] != "c1" {
		t.Fatalf("reverse required: expected only c1, got %v", rows)
	}
}

// TestEnvTargetScoping covers matchesTarget: a webhook fired with Targets tags
// only the listed note.
func TestEnvTargetScoping(t *testing.T) {
	preds := []dsl.EnvPredicate{{NoteType: tPolicy, Kind: dsl.KindWebhook, Endpoint: "e"}}
	notes := []sdk.Note{
		{ID: "p1", Type: tPolicy}, {ID: "p2", Type: tPolicy},
	}
	tagged, err := dsl.ApplyEnvTrigger(preds, notes, dsl.EnvTrigger{Kind: dsl.KindWebhook, Endpoint: "e", Targets: []string{"p1"}})
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if len(tagged) != 1 || tagged[0].ID != "p1" {
		t.Fatalf("target scoping: expected only p1, got %v", tagged)
	}
	// A target not in the corpus tags nothing.
	none, _ := dsl.ApplyEnvTrigger(preds, notes, dsl.EnvTrigger{Kind: dsl.KindWebhook, Endpoint: "e", Targets: []string{"p9"}})
	if len(none) != 0 {
		t.Fatalf("target scoping: non-matching target should tag nothing, got %v", none)
	}
}

// TestTimeAfterExactBoundary covers timeAfterFires at the exact date boundary
// (date == now fires, since the date has been crossed/reached).
func TestTimeAfterExactBoundary(t *testing.T) {
	preds := []dsl.EnvPredicate{{NoteType: tEvidence, Kind: dsl.KindTimeAfter, Field: fExpiresAt}}
	notes := []sdk.Note{{ID: "e1", Type: tEvidence, Fields: map[string]any{fExpiresAt: "2026-06-01"}}}
	now, _ := time.Parse("2006-01-02", "2026-06-01")
	tagged, err := dsl.ApplyEnvTrigger(preds, notes, dsl.EnvTrigger{Kind: dsl.KindTimeAfter, Now: now})
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if len(tagged) != 1 {
		t.Fatalf("boundary: a date equal to now should fire, got %d", len(tagged))
	}
}
