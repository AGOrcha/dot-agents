package graphstore

import (
	"errors"
	"testing"
)

// TestLazyStoreDefersOpenUntilFirstUse proves the Path A "open only when a
// command actually needs the graph" property: constructing a LazyStore
// runs zero open work; the thunk fires on the first contract call and
// exactly once thereafter.
func TestLazyStoreDefersOpenUntilFirstUse(t *testing.T) {
	opens := 0
	inner := &fakeStore{}
	ls := NewLazyStore(func() (Store, error) {
		opens++
		return inner, nil
	})

	if opens != 0 {
		t.Fatalf("construction triggered %d opens, want 0 (lazy)", opens)
	}

	if _, err := ls.GetNode("x"); err != nil {
		t.Fatalf("GetNode through lazy store: %v", err)
	}
	if opens != 1 || !inner.gotNode {
		t.Fatalf("first use should open once and dispatch: opens=%d gotNode=%v", opens, inner.gotNode)
	}

	if _, err := ls.GetKGNote("id"); err != nil {
		t.Fatalf("GetKGNote through lazy store: %v", err)
	}
	if opens != 1 {
		t.Fatalf("second use re-opened the backend: opens=%d want 1", opens)
	}
}

// TestLazyStoreOpenErrorIsSticky proves a failed late open does not
// silently degrade to a half-open store: the error is returned on the
// triggering call and on every subsequent call.
func TestLazyStoreOpenErrorIsSticky(t *testing.T) {
	wantErr := errors.New("open failed")
	calls := 0
	ls := NewLazyStore(func() (Store, error) {
		calls++
		return nil, wantErr
	})

	if _, err := ls.GetStats(); !errors.Is(err, wantErr) {
		t.Fatalf("first call err=%v want %v", err, wantErr)
	}
	if _, err := ls.SearchNodes("q", 1); !errors.Is(err, wantErr) {
		t.Fatalf("second call err=%v want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("open thunk ran %d times, want 1 (sticky)", calls)
	}
}

// TestLazyStoreCloseWithoutOpenIsNoop proves Close never triggers a late
// open just to close an unused handle (acquire/release stays cheap).
func TestLazyStoreCloseWithoutOpenIsNoop(t *testing.T) {
	opened := false
	ls := NewLazyStore(func() (Store, error) {
		opened = true
		return &fakeStore{}, nil
	})
	if err := ls.Close(); err != nil {
		t.Fatalf("Close on unused lazy store: %v", err)
	}
	if opened {
		t.Fatal("Close triggered a late open on an unused handle")
	}
}

// lazyMethods is every non-Close Store method exercised through a lazyStore,
// each reduced to its error return. Used by both the happy-path delegation
// test and the sticky-open-error test so each delegator's resolve()+dispatch
// and resolve()-error branches are both covered without per-method tests.
func lazyMethods() []struct {
	name string
	call func(Store) error
} {
	return []struct {
		name string
		call func(Store) error
	}{
		{"GetNode", func(s Store) error { _, e := s.GetNode("q"); return e }},
		{"GetNodesByFile", func(s Store) error { _, e := s.GetNodesByFile("f"); return e }},
		{"GetEdgesBySource", func(s Store) error { _, e := s.GetEdgesBySource("q"); return e }},
		{"GetEdgesByTarget", func(s Store) error { _, e := s.GetEdgesByTarget("q"); return e }},
		{"GetEdgesAmong", func(s Store) error { _, e := s.GetEdgesAmong([]string{"q"}); return e }},
		{"GetAllFiles", func(s Store) error { _, e := s.GetAllFiles(); return e }},
		{"SearchNodes", func(s Store) error { _, e := s.SearchNodes("q", 1); return e }},
		{"GetMetadata", func(s Store) error { _, e := s.GetMetadata("k"); return e }},
		{"GetStats", func(s Store) error { _, e := s.GetStats(); return e }},
		{"GetImpactRadius", func(s Store) error { _, e := s.GetImpactRadius([]string{"f"}, 1, 1); return e }},
		{"UpsertNode", func(s Store) error { _, e := s.UpsertNode(NodeInfo{}, ""); return e }},
		{"UpsertEdge", func(s Store) error { _, e := s.UpsertEdge(EdgeInfo{}); return e }},
		{"RemoveFileData", func(s Store) error { return s.RemoveFileData("f") }},
		{"StoreFileNodesEdges", func(s Store) error { return s.StoreFileNodesEdges("f", nil, nil, "") }},
		{"SetMetadata", func(s Store) error { return s.SetMetadata("k", "v") }},
		{"Commit", func(s Store) error { return s.Commit() }},
		{"UpsertKGNote", func(s Store) error { return s.UpsertKGNote(KGNote{}) }},
		{"GetKGNote", func(s Store) error { _, e := s.GetKGNote("id"); return e }},
		{"SearchKGNotes", func(s Store) error { _, e := s.SearchKGNotes("q", 1); return e }},
		{"ListArchivedKGNotes", func(s Store) error { _, e := s.ListArchivedKGNotes(); return e }},
		{"UpsertNoteSymbolLink", func(s Store) error { _, e := s.UpsertNoteSymbolLink(NoteSymbolLink{}); return e }},
		{"GetLinksForNote", func(s Store) error { _, e := s.GetLinksForNote("id"); return e }},
		{"GetLinksForSymbol", func(s Store) error { _, e := s.GetLinksForSymbol("q"); return e }},
		{"DeleteNoteSymbolLink", func(s Store) error { return s.DeleteNoteSymbolLink(1) }},

		// CodeGraphDerived. The three FTS entries route through
		// covLazyIgnoreFTSUnsupported because ErrFTSUnsupported is the fake
		// backend's successful answer, not a delegation failure.
		{"ReadFlows", func(s Store) error { _, e := s.ReadFlows(); return e }},
		{"ReadFlowMemberships", func(s Store) error { _, e := s.ReadFlowMemberships(); return e }},
		{"ReadCommunities", func(s Store) error { _, e := s.ReadCommunities(); return e }},
		{"ReadCommunitySummaries", func(s Store) error { _, e := s.ReadCommunitySummaries(); return e }},
		{"ReadFlowSnapshots", func(s Store) error { _, e := s.ReadFlowSnapshots(); return e }},
		{"ReadRiskIndex", func(s Store) error { _, e := s.ReadRiskIndex(); return e }},
		{"SearchNodesFTS", func(s Store) error {
			_, e := s.SearchNodesFTS("q", 1)
			return covLazyIgnoreFTSUnsupported(e)
		}},
		{"SearchNodesFTSWords", func(s Store) error {
			_, e := s.SearchNodesFTSWords([]string{"w"}, 1)
			return covLazyIgnoreFTSUnsupported(e)
		}},
		{"CountNodesFTSWords", func(s Store) error {
			_, e := s.CountNodesFTSWords([]string{"w"})
			return covLazyIgnoreFTSUnsupported(e)
		}},
		{"ReplaceFlows", func(s Store) error {
			_, e := s.ReplaceFlows([]FlowRow{{ID: 1}}, [][]int64{{1}})
			return e
		}},
		{"ReplaceCommunities", func(s Store) error {
			_, e := s.ReplaceCommunities([]CommunityRow{{ID: 1}}, [][]string{{"pkg::A"}})
			return e
		}},
		{"ReplaceCommunitySummaries", func(s Store) error {
			_, e := s.ReplaceCommunitySummaries([]CommunitySummaryRow{{CommunityID: 1}})
			return e
		}},
		{"ReplaceFlowSnapshots", func(s Store) error {
			_, e := s.ReplaceFlowSnapshots([]FlowSnapshotRow{{FlowID: 1}})
			return e
		}},
		{"ReplaceRiskIndex", func(s Store) error {
			_, e := s.ReplaceRiskIndex([]RiskIndexRow{{NodeID: 1}})
			return e
		}},
		{"ApplyEdgeRewrites", func(s Store) error {
			_, e := s.ApplyEdgeRewrites([]EdgeRewrite{{EdgeID: 1}})
			return e
		}},
		{"RebuildFTS", func(s Store) error { _, e := s.RebuildFTS(); return e }},
		{"SetNodeSignature", func(s Store) error { return s.SetNodeSignature(1, "sig") }},
		{"SetNodeCommunity", func(s Store) error { return s.SetNodeCommunity(1, 2) }},
		{"NodesWithoutSignature", func(s Store) error { _, e := s.NodesWithoutSignature(); return e }},
		{"ReadNodesByKind", func(s Store) error { _, e := s.ReadNodesByKind([]string{"Function"}); return e }},
		{"ReadNodesByID", func(s Store) error { _, e := s.ReadNodesByID([]int64{1}); return e }},
		{"ReadNodesByCommunity", func(s Store) error { _, e := s.ReadNodesByCommunity(1); return e }},
		{"ReadAllNodes", func(s Store) error { _, e := s.ReadAllNodes(); return e }},
		{"ReadAllEdges", func(s Store) error { _, e := s.ReadAllEdges(); return e }},
	}
}

// TestLazyStoreDelegatesAllMethods proves every contract method resolves the
// backend and dispatches to it (covers each delegator's happy path).
func TestLazyStoreDelegatesAllMethods(t *testing.T) {
	for _, m := range lazyMethods() {
		t.Run(m.name, func(t *testing.T) {
			opens := 0
			ls := NewLazyStore(func() (Store, error) {
				opens++
				return &fakeStore{}, nil
			})
			if err := m.call(ls); err != nil {
				t.Fatalf("%s through lazy store: %v", m.name, err)
			}
			if opens != 1 {
				t.Fatalf("%s: opens=%d want 1", m.name, opens)
			}
		})
	}
}

// TestLazyStoreAllMethodsPropagateOpenError proves the resolve()-error guard
// in every delegator returns the sticky open error (no half-open dispatch).
func TestLazyStoreAllMethodsPropagateOpenError(t *testing.T) {
	wantErr := errors.New("open failed")
	for _, m := range lazyMethods() {
		t.Run(m.name, func(t *testing.T) {
			ls := NewLazyStore(func() (Store, error) { return nil, wantErr })
			if err := m.call(ls); !errors.Is(err, wantErr) {
				t.Fatalf("%s err=%v want %v", m.name, err, wantErr)
			}
		})
	}
}

// TestLazyStoreCloseAfterUseDelegates proves Close releases the backend once
// it has actually been opened (the l.store != nil branch).
func TestLazyStoreCloseAfterUseDelegates(t *testing.T) {
	inner := &fakeStore{}
	ls := NewLazyStore(func() (Store, error) { return inner, nil })
	if _, err := ls.GetNode("x"); err != nil {
		t.Fatalf("prime open: %v", err)
	}
	if err := ls.Close(); err != nil {
		t.Fatalf("Close after use: %v", err)
	}
	if !inner.closed {
		t.Fatal("Close did not delegate to the opened backend")
	}
}
