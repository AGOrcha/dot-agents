package graphstore

import "testing"

// fakeStore is a minimal in-test Store used to exercise the Handle
// boundary and the narrow-role idiom (declare the role type, assign from
// Store()). It records that a method was reached through a given role so
// the tests can observe that widening actually dispatches to this fake
// (not just type-checks). Each role is represented by one cheap sentinel
// method call.
type fakeStore struct {
	gotNode      bool // CodeGraphReader path observed
	upsertedNode bool // CodeGraphWriter path observed
	gotKGNote    bool // KGNoteStore path observed
	gotLinksNote bool // NoteSymbolLinkStore path observed
	closed       bool // Closer path observed
}

// --- CodeGraphReader ---

func (f *fakeStore) GetNode(string) (*GraphNode, error)           { f.gotNode = true; return nil, nil }
func (f *fakeStore) GetNodesByFile(string) ([]GraphNode, error)   { return nil, nil }
func (f *fakeStore) GetEdgesBySource(string) ([]GraphEdge, error) { return nil, nil }
func (f *fakeStore) GetEdgesByTarget(string) ([]GraphEdge, error) { return nil, nil }
func (f *fakeStore) GetEdgesAmong([]string) ([]GraphEdge, error)  { return nil, nil }
func (f *fakeStore) GetAllFiles() ([]string, error)               { return nil, nil }
func (f *fakeStore) SearchNodes(string, int) ([]GraphNode, error) { return nil, nil }
func (f *fakeStore) GetMetadata(string) (string, error)           { return "", nil }
func (f *fakeStore) GetStats() (GraphStats, error)                { return GraphStats{}, nil }
func (f *fakeStore) GetImpactRadius([]string, int, int) (ImpactResult, error) {
	return ImpactResult{}, nil
}

// --- CodeGraphWriter ---

func (f *fakeStore) UpsertNode(NodeInfo, string) (int64, error)                       { f.upsertedNode = true; return 0, nil }
func (f *fakeStore) UpsertEdge(EdgeInfo) (int64, error)                               { return 0, nil }
func (f *fakeStore) RemoveFileData(string) error                                      { return nil }
func (f *fakeStore) StoreFileNodesEdges(string, []NodeInfo, []EdgeInfo, string) error { return nil }
func (f *fakeStore) SetMetadata(string, string) error                                 { return nil }
func (f *fakeStore) Commit() error                                                    { return nil }

// --- KGNoteStore ---

func (f *fakeStore) UpsertKGNote(KGNote) error                   { return nil }
func (f *fakeStore) GetKGNote(string) (*KGNote, error)           { f.gotKGNote = true; return nil, nil }
func (f *fakeStore) SearchKGNotes(string, int) ([]KGNote, error) { return nil, nil }
func (f *fakeStore) ListArchivedKGNotes() ([]KGNote, error)      { return nil, nil }

// --- NoteSymbolLinkStore ---

func (f *fakeStore) UpsertNoteSymbolLink(NoteSymbolLink) (int64, error) { return 0, nil }
func (f *fakeStore) GetLinksForNote(string) ([]NoteSymbolLink, error) {
	f.gotLinksNote = true
	return nil, nil
}
func (f *fakeStore) GetLinksForSymbol(string) ([]NoteSymbolLink, error) { return nil, nil }
func (f *fakeStore) DeleteNoteSymbolLink(int64) error                   { return nil }

// --- Closer ---

func (f *fakeStore) Close() error { f.closed = true; return nil }

// fakeStore must satisfy the composed contract; if it does, it satisfies
// every role too (Store embeds them).
var _ Store = (*fakeStore)(nil)

func TestNewHandleStoreReturnsWrappedStore(t *testing.T) {
	f := &fakeStore{}
	h := NewHandle(f)

	got := h.Store()
	if got == nil {
		t.Fatal("Store() returned nil for a populated handle")
	}
	if got != Store(f) {
		t.Fatalf("Store() returned a different value than the wrapped store")
	}
	// Widening actually dispatches to the fake.
	if _, err := got.GetNode("x"); err != nil {
		t.Fatalf("unexpected error through Store(): %v", err)
	}
	if !f.gotNode {
		t.Fatal("call through Store() did not reach the fake")
	}
}

// TestStoreNarrowsToEachRoleAndDispatches exercises the documented
// narrow-role idiom: declare the dependency as the narrow role type and
// assign it from Store() (a Store IS each role, since Store embeds them).
// Each narrowed value must dispatch to the underlying fake.
func TestStoreNarrowsToEachRoleAndDispatches(t *testing.T) {
	f := &fakeStore{}
	h := NewHandle(f)

	var r CodeGraphReader = h.Store()
	if _, err := r.GetNode("x"); err != nil {
		t.Fatalf("CodeGraphReader dispatch error: %v", err)
	}
	if !f.gotNode {
		t.Fatal("CodeGraphReader-typed Store() did not widen to the fake")
	}

	var w CodeGraphWriter = h.Store()
	if _, err := w.UpsertNode(NodeInfo{}, ""); err != nil {
		t.Fatalf("CodeGraphWriter dispatch error: %v", err)
	}
	if !f.upsertedNode {
		t.Fatal("CodeGraphWriter-typed Store() did not widen to the fake")
	}

	var k KGNoteStore = h.Store()
	if _, err := k.GetKGNote("id"); err != nil {
		t.Fatalf("KGNoteStore dispatch error: %v", err)
	}
	if !f.gotKGNote {
		t.Fatal("KGNoteStore-typed Store() did not widen to the fake")
	}

	var l NoteSymbolLinkStore = h.Store()
	if _, err := l.GetLinksForNote("id"); err != nil {
		t.Fatalf("NoteSymbolLinkStore dispatch error: %v", err)
	}
	if !f.gotLinksNote {
		t.Fatal("NoteSymbolLinkStore-typed Store() did not widen to the fake")
	}

	var c Closer = h.Store()
	if err := c.Close(); err != nil {
		t.Fatalf("Closer dispatch error: %v", err)
	}
	if !f.closed {
		t.Fatal("Closer-typed Store() did not widen to the fake")
	}
}

// TestZeroHandleStoreIsNilSafe asserts the nil-safety the narrow-role
// idiom relies on: an unset handle's Store() is nil, so any role the
// caller narrows it to is a nil interface value too.
func TestZeroHandleStoreIsNilSafe(t *testing.T) {
	var h Handle // zero/unset handle — no store

	if h.Store() != nil {
		t.Fatal("Store() on a zero Handle must be nil")
	}
	var r CodeGraphReader = h.Store()
	if r != nil {
		t.Fatal("CodeGraphReader narrowed from a zero Handle must be nil")
	}
	var c Closer = h.Store()
	if c != nil {
		t.Fatal("Closer narrowed from a zero Handle must be nil")
	}
}
