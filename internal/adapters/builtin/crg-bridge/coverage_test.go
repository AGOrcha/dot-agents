package crgbridge

import (
	"errors"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// readFailStore is a crg.StoreReader whose reads fail, so MirrorSnapshot's
// readback error path is reachable.
type readFailStore struct{}

func (readFailStore) Notes(_ sdk.Token, _ string) ([]sdk.Note, error) {
	return nil, errors.New("store: injected read failure")
}
func (readFailStore) Edges(_ sdk.Token, _ string) ([]sdk.Edge, error) {
	return nil, errors.New("store: injected read failure")
}

func TestMirrorSnapshot_PropagatesReadError(t *testing.T) {
	if _, err := MirrorSnapshot(readFailStore{}, "c"); err == nil {
		t.Fatal("MirrorSnapshot must propagate a readback failure")
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
