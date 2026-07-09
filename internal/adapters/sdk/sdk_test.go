package sdk

import (
	"errors"
	"reflect"
	"testing"
)

// compile-time proof: MemStore satisfies Store.
var _ Store = (*MemStore)(nil)

// faultStore is a Store whose Notes/Edges/Write calls can be made to fail on
// demand, so the SDK's error-propagation branches are exercised without
// crafting an unauthorized token. It composes a MemStore for the success path.
type faultStore struct {
	inner        *MemStore
	failNotes    bool
	failEdges    bool
	failWriteOn  string // namespace whose writes fail; "" disables
	failReadEdge bool   // Edges fails only after a successful Notes (for Query)
}

var errFault = errors.New("fault")

func (f *faultStore) WriteNotes(token Token, ns string, notes []Note) error {
	if f.failWriteOn != "" && ns == f.failWriteOn {
		return errFault
	}
	return f.inner.WriteNotes(token, ns, notes)
}

func (f *faultStore) WriteEdges(token Token, ns string, edges []Edge) error {
	return f.inner.WriteEdges(token, ns, edges)
}

func (f *faultStore) Notes(token Token, ns string) ([]Note, error) {
	if f.failNotes {
		return nil, errFault
	}
	return f.inner.Notes(token, ns)
}

func (f *faultStore) Edges(token Token, ns string) ([]Edge, error) {
	if f.failEdges || f.failReadEdge {
		return nil, errFault
	}
	return f.inner.Edges(token, ns)
}

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

// allowAllGate is a ReadsFromValidator stub that never rejects. It isolates the
// pre-existing MaterializeView mechanics tests below (token derivation,
// store error propagation) from the reads_from gate under test in
// readsfrom_gate_test.go — those tests exercise the runner/store plumbing,
// not the §11.2 rule, so they opt into "no gate" explicitly rather than
// silently bypassing one.
type allowAllGate struct{}

func (allowAllGate) ValidateReadsFrom(string, []string) error { return nil }

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
	err := s.MaterializeView("dep_view", []string{"crg"}, allowAllGate{}, func(read func(ns string) ([]Note, error)) ([]Note, error) {
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
	err := s.MaterializeView("v", []string{"crg"}, allowAllGate{}, func(read func(ns string) ([]Note, error)) ([]Note, error) {
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
	// note: empty type (second validateNotes branch) and empty id (first).
	if err := s.WriteNotes([]Note{{ID: "", Type: "t"}}); err == nil {
		t.Fatal("empty note id must be rejected")
	}
	if err := s.WriteNotes([]Note{{ID: "n", Type: ""}}); err == nil {
		t.Fatal("empty note type must be rejected")
	}
	// edge: empty type (first validateEdges branch) and missing from/to (second).
	if err := s.WriteEdges([]Edge{{Type: "", From: "a", To: "b"}}); err == nil {
		t.Fatal("empty edge type must be rejected")
	}
	if err := s.WriteEdges([]Edge{{Type: "t", From: "", To: "b"}}); err == nil {
		t.Fatal("edge missing from must be rejected")
	}
	if err := s.WriteEdges([]Edge{{Type: "t", From: "a", To: ""}}); err == nil {
		t.Fatal("edge missing to must be rejected")
	}
}

func TestAdapterName(t *testing.T) {
	if got := For("ttrpg", NewMemStore()).Adapter(); got != "ttrpg" {
		t.Fatalf("Adapter() = %q, want ttrpg", got)
	}
}

func TestNamespaceViewEdgesByType(t *testing.T) {
	v := NamespaceView{Edges: []Edge{
		{Type: "knows", From: "a", To: "b"},
		{Type: "present_at", From: "a", To: "e"},
		{Type: "knows", From: "b", To: "c"},
	}}
	if got := v.EdgesByType("knows"); len(got) != 2 {
		t.Fatalf("EdgesByType(knows) = %d edges, want 2", len(got))
	}
	if got := v.EdgesByType("none"); got != nil {
		t.Fatalf("EdgesByType(none) = %v, want nil", got)
	}
}

func TestMemStoreNamespaces(t *testing.T) {
	store := NewMemStore()
	if got := store.Namespaces(); len(got) != 0 {
		t.Fatalf("empty store Namespaces = %v, want none", got)
	}
	if err := store.WriteNotes(BootstrapToken("b"), "b", []Note{{ID: "n", Type: "t"}}); err != nil {
		t.Fatalf("seed notes: %v", err)
	}
	if err := store.WriteEdges(BootstrapToken("a"), "a", []Edge{{Type: "e", From: "x", To: "y"}}); err != nil {
		t.Fatalf("seed edges: %v", err)
	}
	got := store.Namespaces()
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Namespaces = %v, want [a b] sorted", got)
	}
}

func TestQueryPropagatesNotesError(t *testing.T) {
	s := For("ttrpg", &faultStore{inner: NewMemStore(), failNotes: true})
	if _, err := s.Query(func(NamespaceView) []Row { return nil }); err == nil {
		t.Fatal("Query must propagate a Notes read error")
	}
}

func TestQueryPropagatesEdgesError(t *testing.T) {
	// Notes succeeds, Edges fails — exercises the second Query error branch.
	s := For("ttrpg", &faultStore{inner: NewMemStore(), failReadEdge: true})
	if _, err := s.Query(func(NamespaceView) []Row { return nil }); err == nil {
		t.Fatal("Query must propagate an Edges read error")
	}
}

func TestMaterializeViewPropagatesRunnerError(t *testing.T) {
	s := For("ttrpg", NewMemStore())
	err := s.MaterializeView("v", nil, allowAllGate{}, func(func(string) ([]Note, error)) ([]Note, error) {
		return nil, errFault
	})
	if err == nil {
		t.Fatal("MaterializeView must propagate a runner error")
	}
}

func TestMaterializeViewPropagatesWriteError(t *testing.T) {
	// Runner succeeds but the final write to the adapter namespace fails.
	s := For("ttrpg", &faultStore{inner: NewMemStore(), failWriteOn: "ttrpg"})
	err := s.MaterializeView("v", nil, allowAllGate{}, func(func(string) ([]Note, error)) ([]Note, error) {
		return []Note{{ID: "n", Type: "t"}}, nil
	})
	if err == nil {
		t.Fatal("MaterializeView must propagate the persist write error")
	}
}

func TestWriteEdgesStoreRejectsUnauthorized(t *testing.T) {
	store := NewMemStore()
	// WriteEdges store-level rejection (own-read token cannot write edges).
	if err := store.WriteEdges(OwnReadToken("a", "q"), "a", []Edge{{Type: "t", From: "x", To: "y"}}); err == nil {
		t.Fatal("WriteEdges with a read-only token must be rejected")
	}
}

func TestEdgesStoreRejectsUnauthorized(t *testing.T) {
	store := NewMemStore()
	if _, err := store.Edges(OwnReadToken("a", "q"), "b"); err == nil {
		t.Fatal("Edges read of an unauthorized namespace must be rejected")
	}
}
