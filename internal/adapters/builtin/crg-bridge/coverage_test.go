package crgbridge

import (
	"errors"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// failStore fails writes after failAfter successful calls, so Bootstrap's
// WriteNotes / WriteEdges error paths are reachable.
type failStore struct {
	writeCalls int
	failAfter  int
}

func (s *failStore) WriteNotes(_ sdk.Token, _ string, _ []sdk.Note) error { return s.maybeFail() }
func (s *failStore) WriteEdges(_ sdk.Token, _ string, _ []sdk.Edge) error { return s.maybeFail() }
func (s *failStore) Notes(_ sdk.Token, _ string) ([]sdk.Note, error)      { return nil, nil }
func (s *failStore) Edges(_ sdk.Token, _ string) ([]sdk.Edge, error)      { return nil, nil }
func (s *failStore) maybeFail() error {
	s.writeCalls++
	if s.writeCalls > s.failAfter {
		return errors.New("store: injected write failure")
	}
	return nil
}

func bridgeTestCorpus() crg.Corpus {
	return crg.Corpus{
		Symbols: []crg.Symbol{
			{QualifiedName: "x", Kind: "Function", Language: "go", FilePath: "x.go", LineStart: 1, ContentHash: "h"},
		},
		References: []crg.Reference{{Kind: "CALLS", From: "x", To: "x"}},
	}
}

func TestBootstrap_PropagatesWriteNotesError(t *testing.T) {
	s := sdk.For(Name, &failStore{failAfter: 0})
	if _, err := Bootstrap(s, bridgeTestCorpus(), "c"); err == nil {
		t.Fatal("Bootstrap must propagate a WriteNotes failure")
	}
}

func TestBootstrap_PropagatesWriteEdgesError(t *testing.T) {
	s := sdk.For(Name, &failStore{failAfter: 1})
	if _, err := Bootstrap(s, bridgeTestCorpus(), "c"); err == nil {
		t.Fatal("Bootstrap must propagate a WriteEdges failure")
	}
}

func TestImpactRadius_Identity(t *testing.T) {
	a := New()
	res, err := a.ImpactRadius(registry.ImpactRequest{ChangedIDs: []string{"x", "y"}})
	if err != nil {
		t.Fatalf("impact radius: %v", err)
	}
	if len(res.IDs) != 2 || res.IDs[0] != "x" {
		t.Fatalf("mirror impact radius identity = %v, want [x y]", res.IDs)
	}
	in := []string{"a"}
	res, _ = a.ImpactRadius(registry.ImpactRequest{ChangedIDs: in})
	res.IDs[0] = "mutated"
	if in[0] != "a" {
		t.Fatal("ImpactRadius must not alias the caller's slice")
	}
}

func TestSchema_PanicsOnMalformedEmbed(t *testing.T) {
	orig := schemaYAML
	t.Cleanup(func() { schemaYAML = orig })
	schemaYAML = []byte(":::not yaml")
	defer func() {
		if recover() == nil {
			t.Fatal("Schema must panic on a malformed embedded schema")
		}
	}()
	_ = New().Schema()
}
