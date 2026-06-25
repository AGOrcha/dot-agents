package dsl_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// staleView models a control with a structured environmental stale tag and a
// derives_from ref to a policy, plus a fresh control. It exercises the §7.3
// stale-tag selectors (T19–T22) and the depth-1 ref + stale selector (T20).
func staleView() sdk.NamespaceView {
	staleTag := map[string]any{"reason": vEnvironmental, "because": []any{"trig-1"}, "fired_at": "2026-06-01T00:00:00Z"}
	return sdk.NamespaceView{
		Notes: []sdk.Note{
			{ID: idPol1, Type: tPolicy, Fields: map[string]any{"version": "2", fStale: staleTag}},
			{ID: "ctl-1", Type: tControl, Fields: map[string]any{fStatus: vEffective, fDerivesFrom: idPol1, fStale: staleTag}},
			{ID: "ctl-2", Type: tControl, Fields: map[string]any{fStatus: vEffective, fDerivesFrom: idPol1}}, // fresh
		},
	}
}

func staleSchema(t *testing.T) dsl.SchemaInfo {
	t.Helper()
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: tControl, Fields: []dsl.FieldDecl{
			{Name: fStatus, Type: tString},
			{Name: fDerivesFrom, Type: "ref<policy>", Derivation: true},
		}},
		{Name: tPolicy, Fields: []dsl.FieldDecl{{Name: "version", Type: tString}}},
	}, nil, 2)
	if err != nil {
		t.Fatalf("staleSchema: %v", err)
	}
	return info
}

func evalStale(t *testing.T, src string) []sdk.Row {
	t.Helper()
	q, err := dsl.ParseWithSchema(src, staleSchema(t))
	if err != nil {
		t.Fatalf("ParseWithSchema(%q): %v", src, err)
	}
	rows, err := dsl.Eval(q, staleView(), nil)
	if err != nil {
		t.Fatalf("Eval(%q): %v", src, err)
	}
	return rows
}

// T19 — WHERE n.stale.reason = 'environmental' reads the stale tag on the
// primary alias; only the tagged control matches.
func TestConformanceT19StaleReasonPrimary(t *testing.T) {
	rows := evalStale(t, "MATCH (c:control) WHERE c.stale.reason = 'environmental' RETURN c.id")
	if len(rows) != 1 || rows[0]["c.id"] != "ctl-1" {
		t.Fatalf("T19: expected only ctl-1, got %v", rows)
	}
}

// T20 — ref-traversal then stale tag: c.derives_from.stale.reason resolves the
// derives_from ref (depth-1) and reads the policy's stale reason.
func TestConformanceT20StaleThroughRef(t *testing.T) {
	rows := evalStale(t, "MATCH (c:control) WHERE c.derives_from.stale.reason = 'environmental' RETURN c.id")
	if len(rows) != 2 {
		t.Fatalf("T20: both controls derive from the stale policy; expected 2, got %d", len(rows))
	}
}

// T21 — fresh notes return with stale = NULL.
func TestConformanceT21FreshStaleNull(t *testing.T) {
	rows := evalStale(t, "MATCH (c:control) RETURN c.id, c.stale.reason")
	for _, r := range rows {
		if r["c.id"] == "ctl-2" && r[colStaleReason] != nil {
			t.Fatalf("T21: fresh ctl-2 should have NULL stale.reason, got %v", r[colStaleReason])
		}
	}
}

// T22 — stale notes return the structured payload subfields.
func TestConformanceT22StalePayload(t *testing.T) {
	rows := evalStale(t, "MATCH (c:control) WHERE c.stale.reason = 'environmental' RETURN c.id, c.stale.reason, c.stale.fired_at")
	if len(rows) != 1 {
		t.Fatalf("T22: expected 1 stale control, got %d", len(rows))
	}
	r := rows[0]
	if r[colStaleReason] != vEnvironmental || r["c.stale.fired_at"] != "2026-06-01T00:00:00Z" {
		t.Fatalf("T22: structured stale payload not surfaced: %v", r)
	}
}
