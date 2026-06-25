package complianceregister_test

import (
	"testing"
	"time"

	complianceregister "github.com/AGOrcha/dot-agents/internal/adapters/builtin/compliance-register"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// TestO5SourceMutationContentHash exercises the O5-pinned source_mutation
// semantics: the driver fires on a content-hash CHANGE, not on any re-upsert.
// An identical re-write does NOT fire; a field change does.
func TestO5SourceMutationContentHash(t *testing.T) {
	base := sdk.Note{ID: idCtl1, Type: tControl, Fields: map[string]any{fStatus: vEffective, fFramework: frameworkSOC2}}
	identical := sdk.Note{ID: idCtl1, Type: tControl, Fields: map[string]any{fFramework: frameworkSOC2, fStatus: vEffective}}
	if complianceregister.SourceMutationFires(base, identical) {
		t.Fatal("O5: identical re-upsert (different field order) must NOT fire source_mutation")
	}
	changed := sdk.Note{ID: idCtl1, Type: tControl, Fields: map[string]any{fStatus: "failed", fFramework: frameworkSOC2}}
	if !complianceregister.SourceMutationFires(base, changed) {
		t.Fatal("O5: a status change MUST fire source_mutation")
	}
}

// TestO5SourceHashIgnoresStaleTag confirms the content hash excludes the stale
// tag (driver output), so tagging a note stale does not itself look like a
// source mutation.
func TestO5SourceHashIgnoresStaleTag(t *testing.T) {
	base := sdk.Note{ID: idCtl1, Type: tControl, Fields: map[string]any{fStatus: vEffective}}
	tagged := sdk.Note{ID: idCtl1, Type: tControl, Fields: map[string]any{fStatus: vEffective, fStale: map[string]any{fReason: vDerivation}}}
	if complianceregister.SourceMutationFires(base, tagged) {
		t.Fatal("O5: a stale-tag write must NOT count as source mutation")
	}
}

// TestO5ExplicitRevocation exercises the explicit_revocation driver: a note
// carrying `revokes: <id>` revokes the named note.
func TestO5ExplicitRevocation(t *testing.T) {
	revoker := sdk.Note{ID: "rev-1", Type: tFinding, Fields: map[string]any{"revokes": "ctl-old"}}
	id, ok := complianceregister.RevocationFires(revoker)
	if !ok || id != "ctl-old" {
		t.Fatalf("O5 revocation: expected revokes ctl-old, got (%q, %v)", id, ok)
	}
	plain := sdk.Note{ID: "f-1", Type: tFinding, Fields: map[string]any{}}
	if _, ok := complianceregister.RevocationFires(plain); ok {
		t.Fatal("O5 revocation: a note without revokes must not fire")
	}
}

// TestO5DerivationDepthOneHop confirms the env→derivation propagation is the
// single control→policy ref hop (O5: derivation depth 1) and does not cascade
// further (no note→note unbounded walk without opt-in).
func TestO5DerivationDepthOneHop(t *testing.T) {
	a := complianceregister.New()
	staleTag := map[string]any{fReason: vEnvironmental, "because": []any{"wh-1"}}
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: idPol1, Type: tPolicy, Fields: map[string]any{"version": "1", fStale: staleTag}},
		{ID: idCtl1, Type: tControl, Fields: map[string]any{"control_id": "C1", fDerives: idPol1}},
		{ID: idCtl2, Type: tControl, Fields: map[string]any{"control_id": "C2", fDerives: "pol-other"}},
	}}
	out := a.ApplyDerivation(view)
	got := map[string]string{}
	for _, n := range out.Notes {
		if n.Type == tControl {
			if raw, ok := n.Fields[fStale].(map[string]any); ok {
				got[n.ID], _ = raw[fReason].(string)
			}
		}
	}
	if got[idCtl1] != vDerivation {
		t.Fatalf("ctl-1 should be derivation-stale, got %q", got[idCtl1])
	}
	if got[idCtl2] != "" {
		t.Fatalf("ctl-2 derives from a fresh policy; must stay fresh, got %q", got[idCtl2])
	}
}

// TestApplyDerivationNoStalePolicy covers the early-return when no policy is
// environmentally stale.
func TestApplyDerivationNoStalePolicy(t *testing.T) {
	a := complianceregister.New()
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: idCtl1, Type: tControl, Fields: map[string]any{fDerives: idPol1}},
	}}
	out := a.ApplyDerivation(view)
	if len(out.Notes) != 1 || out.Notes[0].Fields[fStale] != nil {
		t.Fatalf("no stale policy: expected unchanged view, got %v", out.Notes)
	}
}

// TestEnvAndContradictionDriversDeclared confirms all five §13.2 staleness
// drivers are declared in the schema (the driver-binding surface, §7).
func TestEnvAndContradictionDriversDeclared(t *testing.T) {
	a := complianceregister.New()
	want := map[string]bool{
		"source_mutation": false, "derivation_mutation": false, "environmental_trigger": false,
		"explicit_revocation": false, "contradiction_arrival": false,
	}
	for _, d := range a.Schema().StalenessDrivers {
		want[d] = true
	}
	for d, seen := range want {
		if !seen {
			t.Errorf("driver %q not declared in schema", d)
		}
	}
}

// TestImpactRadiusContractMethod covers the registry.Adapter ImpactRadius method
// (the no-corpus contract entry point).
func TestImpactRadiusContractMethod(t *testing.T) {
	a := complianceregister.New()
	res, err := a.ImpactRadius(registry.ImpactRequest{ChangedIDs: []string{idCtl1}})
	if err != nil {
		t.Fatalf("ImpactRadius: %v", err)
	}
	if len(res.IDs) != 1 || res.IDs[0] != idCtl1 {
		t.Fatalf("ImpactRadius: expected passthrough, got %v", res.IDs)
	}
}

// TestUnknownNamedQuery covers RunNamed's unknown-query error path.
func TestUnknownNamedQuery(t *testing.T) {
	a := complianceregister.New()
	if _, err := a.RunNamed("nope", sdk.NamespaceView{}, nil); err == nil {
		t.Fatal("expected error for unknown named query")
	}
}

// TestBootstrapBadCorpus covers the corpus-parse error path.
func TestBootstrapBadCorpus(t *testing.T) {
	a := complianceregister.New()
	if _, err := a.Bootstrap(sdk.NewMemStore(), []byte("{not json")); err == nil {
		t.Fatal("expected bootstrap to reject malformed corpus")
	}
}

// TestAccessorsAndTimeAfter covers SchemaInfo/EnvPredicates/QueryNames accessors
// and the evidence time_after expiry edge (future expiry does not fire).
func TestAccessorsAndTimeAfter(t *testing.T) {
	a := complianceregister.New()
	if len(a.SchemaInfo().NoteFields) == 0 {
		t.Fatal("SchemaInfo should expose note fields")
	}
	if len(a.EnvPredicates()) != 2 {
		t.Fatalf("expected 2 env predicates, got %d", len(a.EnvPredicates()))
	}
	if len(a.QueryNames()) != 3 {
		t.Fatalf("expected 3 named queries, got %d", len(a.QueryNames()))
	}
	// A trigger before the evidence expiry must not tag it.
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "ev-1", Type: tEvidence, Fields: map[string]any{"expires_at": "2030-01-01"}},
	}}
	now, _ := time.Parse("2006-01-02", "2026-01-01")
	out, err := a.FireEnvTrigger(view, dsl.EnvTrigger{Kind: dsl.KindTimeAfter, Now: now})
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if out.Notes[0].Fields[fStale] != nil {
		t.Fatal("evidence expiring in 2030 must not be stale at 2026")
	}
}
