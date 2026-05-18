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
