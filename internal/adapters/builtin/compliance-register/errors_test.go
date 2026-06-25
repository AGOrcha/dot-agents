package complianceregister_test

import (
	"errors"
	"strings"
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

// TestNewFromYAMLInvalidSchema covers newFromYAML's invalid-schema error path
// (the build-time guard New panics on): a YAML missing the required name fails
// schema load.
func TestNewFromYAMLInvalidSchema(t *testing.T) {
	bad := []byte("version: 1.0.0\nimpact_radius:\n  query: 'RETURN $changed_ids'\n  max_depth: 0\n")
	if _, err := complianceregister.NewFromYAML(bad); err == nil {
		t.Fatal("expected invalid schema (missing name) to error")
	}
}

// TestNewFromYAMLBadQuery covers newFromYAML's query-compile error path: a
// schema whose impact_radius query references an undeclared edge type fails to
// compile against the schema info.
func TestNewFromYAMLBadQuery(t *testing.T) {
	bad := []byte(`name: x
version: 1.0.0
note_types:
  - name: control
    fields:
      - { name: status, type: string }
impact_radius:
  query: |-
    MATCH (c:control)-[:nonexistent]->(d:control) RETURN c.id
  max_depth: 1
`)
	if _, err := complianceregister.NewFromYAML(bad); err == nil {
		t.Fatal("expected impact_radius query referencing an unknown edge to fail compile")
	}
}

// TestNewSucceeds confirms the embedded-schema path constructs cleanly (New does
// not panic) — the production happy path.
func TestNewSucceeds(t *testing.T) {
	if a := complianceregister.New(); a == nil || a.Name() != complianceregister.Name {
		t.Fatal("New() should construct the compliance-register adapter")
	}
}

// TestNewFromYAMLUntypedRef covers newFromYAML's schema-info error path: a
// schema with an untyped ref field (forbidden, §5.4) fails buildSchemaInfo.
func TestNewFromYAMLUntypedRef(t *testing.T) {
	bad := []byte(`name: x
version: 1.0.0
note_types:
  - name: control
    fields:
      - { name: owner, type: ref }
impact_radius:
  query: 'RETURN $changed_ids'
  max_depth: 0
`)
	if _, err := complianceregister.NewFromYAML(bad); err == nil {
		t.Fatal("expected untyped ref field to fail schema-info build")
	}
}

// TestNewFromYAMLNamedQueryFails covers compileQueries' named-query error path:
// a minimal schema whose impact_radius compiles (a trivial RETURN) but whose
// note/edge vocabulary cannot satisfy the fixed named queries (they reference
// satisfies/cited_by/derives_from) — so a named query fails to compile after
// impact_radius succeeds.
func TestNewFromYAMLNamedQueryFails(t *testing.T) {
	minimal := []byte(`name: x
version: 1.0.0
note_types:
  - name: control
    fields:
      - { name: control_id, type: string }
impact_radius:
  query: 'RETURN $changed_ids'
  max_depth: 0
`)
	_, err := complianceregister.NewFromYAML(minimal)
	if err == nil {
		t.Fatal("expected a named query referencing undeclared types/edges to fail compile")
	}
	if !strings.Contains(err.Error(), "named query") {
		t.Fatalf("expected a named-query compile error, got: %v", err)
	}
}
