package sdk

import (
	"reflect"
	"testing"
)

// compile-time proof: MemStore satisfies Store.
var _ Store = (*MemStore)(nil)

func TestBootstrapWriteAndQuery(t *testing.T) {
	store := NewMemStore()
	s := For("ttrpg", store)

	if err := s.WriteNotes([]Note{
		{ID: "c1", Type: "character", Fields: map[string]any{"name": "Mara"}},
		{ID: "loc1", Type: "location", Fields: map[string]any{"name": "Ironhold"}},
	}); err != nil {
		t.Fatalf("WriteNotes: %v", err)
	}
	if err := s.WriteEdges([]Edge{{Type: "present_at", From: "c1", To: "loc1"}}); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}

	rows, err := s.Query(func(v NamespaceView) []Row {
		var out []Row
		for _, n := range v.NotesByType("character") {
			out = append(out, Row{"id": n.ID})
		}
		return out
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "c1" {
		t.Fatalf("Query rows = %v, want one row id=c1", rows)
	}
}

func TestTokenDerivation(t *testing.T) {
	// N1: own query → {A, read} only.
	if got := OwnReadToken("A", "q").Authorized; !reflect.DeepEqual(got, []Grant{{"A", ModeRead}}) {
		t.Fatalf("N1 own-read = %v", got)
	}
	// N2: bootstrap → {A, write} only.
	if got := BootstrapToken("A").Authorized; !reflect.DeepEqual(got, []Grant{{"A", ModeWrite}}) {
		t.Fatalf("N2 bootstrap = %v", got)
	}
	// N3: view reads_from [B] → {A, write} + {B, read}.
	if got := ViewToken("A", "v", []string{"B"}).Authorized; !reflect.DeepEqual(got, []Grant{{"A", ModeWrite}, {"B", ModeRead}}) {
		t.Fatalf("N3 view = %v", got)
	}
	// N4: view reads_from [B, C].
	if got := ViewToken("A", "v", []string{"B", "C"}).Authorized; !reflect.DeepEqual(got, []Grant{{"A", ModeWrite}, {"B", ModeRead}, {"C", ModeRead}}) {
		t.Fatalf("N4 view = %v", got)
	}
}

func TestStoreRejectsUnauthorizedWrite(t *testing.T) {
	store := NewMemStore()
	// N8: a bootstrap token for A cannot write to B's namespace.
	err := store.WriteNotes(BootstrapToken("A"), "B", []Note{{ID: "x", Type: "t"}})
	if err == nil {
		t.Fatal("N8: write to foreign namespace must be rejected")
	}
}

func TestStoreRejectsUnauthorizedRead(t *testing.T) {
	store := NewMemStore()
	// N9: an own-read token for A cannot read C even if a query plan reaches it.
	_, err := store.Notes(OwnReadToken("A", "q"), "C")
	if err == nil {
		t.Fatal("N9: read of unauthorized namespace must be rejected")
	}
}

func TestMaterializeViewCrossNamespaceRead(t *testing.T) {
	store := NewMemStore()
	// Seed a dependency namespace (as if another adapter wrote it).
	if err := store.WriteNotes(BootstrapToken("crg"), "crg", []Note{
		{ID: "f1", Type: "function"},
	}); err != nil {
		t.Fatalf("seed crg: %v", err)
	}

	s := For("ttrpg", store)
	// A view declaring reads_from [crg] CAN read crg; a named query cannot.
	err := s.MaterializeView("dep_view", []string{"crg"}, func(read func(ns string) ([]Note, error)) ([]Note, error) {
		dep, err := read("crg")
		if err != nil {
			return nil, err
		}
		out := make([]Note, 0, len(dep))
		for _, n := range dep {
			out = append(out, Note{ID: "mirror-" + n.ID, Type: "mirror"})
		}
		return out, nil
	})
	if err != nil {
		t.Fatalf("MaterializeView: %v", err)
	}

	// The view's output landed in ttrpg's namespace.
	rows, err := s.Query(func(v NamespaceView) []Row {
		var out []Row
		for _, n := range v.NotesByType("mirror") {
			out = append(out, Row{"id": n.ID})
		}
		return out
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "mirror-f1" {
		t.Fatalf("view output = %v, want mirror-f1", rows)
	}
}

func TestViewRunnerCannotReadUndeclaredNamespace(t *testing.T) {
	store := NewMemStore()
	_ = store.WriteNotes(BootstrapToken("secret"), "secret", []Note{{ID: "s1", Type: "t"}})
	s := For("ttrpg", store)
	// reads_from declares [crg] but the runner tries to read [secret].
	err := s.MaterializeView("v", []string{"crg"}, func(read func(ns string) ([]Note, error)) ([]Note, error) {
		return read("secret")
	})
	if err == nil {
		t.Fatal("view reading an undeclared namespace must be rejected at storage layer")
	}
}

func TestDeclarePredicateFired(t *testing.T) {
	s := For("ttrpg", NewMemStore())
	s.DeclarePredicateFired("session.recorded", map[string]any{"n": 1})
	got := s.FiredPredicates()
	if len(got) != 1 || got[0].Predicate != "session.recorded" {
		t.Fatalf("FiredPredicates = %v", got)
	}
}

func TestWriteValidatesInput(t *testing.T) {
	s := For("ttrpg", NewMemStore())
	if err := s.WriteNotes([]Note{{ID: "", Type: "t"}}); err == nil {
		t.Fatal("empty note id must be rejected")
	}
	if err := s.WriteEdges([]Edge{{Type: "", From: "a", To: "b"}}); err == nil {
		t.Fatal("empty edge type must be rejected")
	}
}
