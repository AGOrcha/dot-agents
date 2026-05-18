// Package graphstore — coverage for the encodeExtra / pgEncodeExtra
// JSON-marshal error paths (channels are unmarshallable).
package graphstore

import (
	"testing"
)

func TestEncodeExtra_MarshalError(t *testing.T) {
	// channels are not JSON-encodable.
	bad := map[string]any{"ch": make(chan int)}
	s, err := encodeExtra(bad)
	if err == nil {
		t.Fatalf("expected JSON marshal error, got %q", s)
	}
	if s != "{}" {
		t.Errorf("expected fallback to {} on error, got %q", s)
	}
}

func TestPGEncodeExtra_MarshalError(t *testing.T) {
	bad := map[string]any{"ch": make(chan int)}
	s, err := pgEncodeExtra(bad)
	if err == nil {
		t.Fatalf("expected JSON marshal error, got %q", s)
	}
	if s != "{}" {
		t.Errorf("expected fallback to {} on error, got %q", s)
	}
}

func TestSQLiteStore_UpsertNode_EncodeExtraError(t *testing.T) {
	s := openInternalStore(t)
	_, err := s.UpsertNode(NodeInfo{
		Kind:     "Function",
		Name:     "fn",
		FilePath: "f.go",
		Extra:    map[string]any{"ch": make(chan int)},
	}, "h")
	if err == nil {
		t.Error("expected encodeExtra error from UpsertNode")
	}
}

func TestSQLiteStore_UpsertEdge_EncodeExtraError(t *testing.T) {
	s := openInternalStore(t)
	_, err := s.UpsertEdge(EdgeInfo{
		Kind:     "CALLS",
		Source:   "a",
		Target:   "b",
		FilePath: "f.go",
		Extra:    map[string]any{"ch": make(chan int)},
	})
	if err == nil {
		t.Error("expected encodeExtra error from UpsertEdge")
	}
}
