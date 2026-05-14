package graphstore

import (
	"errors"
	"testing"
)

// erroringImpactStore satisfies impactStoreView but errors on GetEdgesAmong.
type erroringImpactStore struct {
	nodes map[string]*GraphNode
}

func (e *erroringImpactStore) GetNode(qn string) (*GraphNode, error) {
	return e.nodes[qn], nil
}
func (e *erroringImpactStore) GetEdgesAmong(qns []string) ([]GraphEdge, error) {
	return nil, errors.New("edges-among failed")
}

// TestComputeImpactRadius_GetEdgesAmongError verifies that when the store's
// GetEdgesAmong call fails, the error propagates and the result is zeroed.
func TestComputeImpactRadius_GetEdgesAmongError(t *testing.T) {
	store := &erroringImpactStore{nodes: map[string]*GraphNode{"A": {QualifiedName: "A", FilePath: "a.go"}}}
	seeds := map[string]bool{"A": true}
	got, err := computeImpactRadius(seeds, map[string][]string{}, map[string][]string{}, 1, 10, store)
	if err == nil {
		t.Fatal("expected error from GetEdgesAmong")
	}
	if len(got.ChangedNodes) != 0 || len(got.ImpactedNodes) != 0 || len(got.Edges) != 0 {
		t.Errorf("expected zero result on error, got %+v", got)
	}
}
