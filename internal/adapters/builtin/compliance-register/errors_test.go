package complianceregister_test

import (
	"errors"
	"testing"

	complianceregister "github.com/AGOrcha/dot-agents/internal/adapters/builtin/compliance-register"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// failStore is a Store whose writes always fail, to exercise the Bootstrap
// write-error branches without a real backend.
type failStore struct{ failEdges bool }

func (f failStore) WriteNotes(sdk.Token, string, []sdk.Note) error {
	if f.failEdges {
		return nil // let notes succeed so the edges path is reached
	}
	return errors.New("write notes failed")
}
func (f failStore) WriteEdges(sdk.Token, string, []sdk.Edge) error {
	return errors.New("write edges failed")
}
func (f failStore) Notes(sdk.Token, string) ([]sdk.Note, error) { return nil, nil }
func (f failStore) Edges(sdk.Token, string) ([]sdk.Edge, error) { return nil, nil }

const miniCorpus = `{"notes":[{"id":"n1","type":"control","fields":{}}],"edges":[{"type":"satisfies","from":"n1","to":"n1"}]}`

// TestBootstrapWriteNotesError covers the WriteNotes failure branch.
func TestBootstrapWriteNotesError(t *testing.T) {
	a := complianceregister.New()
	if _, err := a.Bootstrap(failStore{}, []byte(miniCorpus)); err == nil {
		t.Fatal("expected WriteNotes failure to surface")
	}
}

// TestBootstrapWriteEdgesError covers the WriteEdges failure branch.
func TestBootstrapWriteEdgesError(t *testing.T) {
	a := complianceregister.New()
	if _, err := a.Bootstrap(failStore{failEdges: true}, []byte(miniCorpus)); err == nil {
		t.Fatal("expected WriteEdges failure to surface")
	}
}

// TestFireEnvTriggerError covers FireEnvTrigger's error propagation from an
// unsupported trigger kind.
func TestFireEnvTriggerError(t *testing.T) {
	a := complianceregister.New()
	if _, err := a.FireEnvTrigger(sdk.NamespaceView{}, dsl.EnvTrigger{Kind: "module_version"}); err == nil {
		t.Fatal("expected unsupported trigger kind to error")
	}
}

// TestStalenessNilFields covers the nil-Fields guards in staleReasonOf and
// derivesFromStalePolicy.
func TestStalenessNilFields(t *testing.T) {
	a := complianceregister.New()
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: idPol1, Type: tPolicy, Fields: map[string]any{fStale: map[string]any{"reason": vEnvironmental}}},
		{ID: "ctl-nil", Type: tControl, Fields: nil},                             // nil fields
		{ID: "ctl-noref", Type: tControl, Fields: map[string]any{fStatus: "ok"}}, // no derives_from
	}}
	out := a.ApplyDerivation(view)
	for _, n := range out.Notes {
		if n.Type == tControl && n.Fields[fStale] != nil {
			t.Fatalf("control %q without a stale-policy ref must stay fresh", n.ID)
		}
	}
}

// TestRevocationNilFields covers RevocationFires' nil-Fields guard.
func TestRevocationNilFields(t *testing.T) {
	if _, ok := complianceregister.RevocationFires(sdk.Note{ID: "x", Type: tFinding}); ok {
		t.Fatal("nil-fields note must not fire revocation")
	}
}
