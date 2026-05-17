package graphstore

import (
	"testing"
)

// TestBfsImpacted_LinearChain verifies BFS on a simple A->B->C chain.
// Starting from A, B and C should be impacted.
func TestBfsImpacted_LinearChain(t *testing.T) {
	seeds := map[string]bool{"A": true}
	fwd := map[string][]string{
		"A": {"B"},
		"B": {"C"},
	}
	rev := map[string][]string{
		"B": {"A"},
		"C": {"B"},
	}

	impacted := bfsImpacted(seeds, fwd, rev, 10, 100)

	if !impacted["B"] {
		t.Error("B should be impacted (direct neighbor of seed A)")
	}
	if !impacted["C"] {
		t.Error("C should be impacted (2 hops from seed A)")
	}
	if impacted["A"] {
		t.Error("A is a seed and should not be in the impacted set")
	}
}

// TestBfsImpacted_ReverseEdges verifies that BFS follows reverse edges too.
func TestBfsImpacted_ReverseEdges(t *testing.T) {
	seeds := map[string]bool{"B": true}
	fwd := map[string][]string{
		"A": {"B"},
		"B": {"C"},
	}
	rev := map[string][]string{
		"B": {"A"},
		"C": {"B"},
	}

	impacted := bfsImpacted(seeds, fwd, rev, 10, 100)

	if !impacted["A"] {
		t.Error("A should be impacted via reverse edge from B")
	}
	if !impacted["C"] {
		t.Error("C should be impacted via forward edge from B")
	}
}

// TestBfsImpacted_CycleHandling verifies that BFS does not infinite-loop on
// cycles and terminates normally.
func TestBfsImpacted_CycleHandling(t *testing.T) {
	seeds := map[string]bool{"A": true}
	// A -> B -> C -> A (cycle)
	fwd := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"A"},
	}
	rev := map[string][]string{
		"B": {"A"},
		"C": {"B"},
		"A": {"C"},
	}

	impacted := bfsImpacted(seeds, fwd, rev, 10, 100)

	if !impacted["B"] {
		t.Error("B should be impacted")
	}
	if !impacted["C"] {
		t.Error("C should be impacted")
	}
	// Should terminate without hanging — reaching this line is the test.
}

// TestBfsImpacted_MaxDepthLimits verifies that maxDepth bounds BFS hops.
func TestBfsImpacted_MaxDepthLimits(t *testing.T) {
	seeds := map[string]bool{"A": true}
	fwd := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"D"},
	}
	rev := map[string][]string{}

	// maxDepth=1: only B should be found (1 hop from A)
	impacted := bfsImpacted(seeds, fwd, rev, 1, 100)

	if !impacted["B"] {
		t.Error("B should be impacted (1 hop)")
	}
	if impacted["C"] {
		t.Error("C should NOT be impacted (2 hops, maxDepth=1)")
	}
	if impacted["D"] {
		t.Error("D should NOT be impacted (3 hops, maxDepth=1)")
	}
}

// TestBfsImpacted_MaxNodesLimits verifies that maxNodes prevents further BFS
// expansion once the frontier would exceed the cap. The current frontier's
// neighbors are recorded in impacted, but no additional hops occur.
func TestBfsImpacted_MaxNodesLimits(t *testing.T) {
	seeds := map[string]bool{"A": true}
	// A -> B1..B3, each Bx -> Cx (second hop)
	fwd := map[string][]string{
		"A":  {"B1", "B2", "B3"},
		"B1": {"C1"},
		"B2": {"C2"},
		"B3": {"C3"},
	}
	rev := map[string][]string{}

	// maxNodes=3: first hop finds B1,B2,B3 (visited=1 + next=3 = 4 > 3),
	// so BFS stops before the second hop. B1-B3 are in impacted but C1-C3 are not.
	impacted := bfsImpacted(seeds, fwd, rev, 10, 3)

	for _, b := range []string{"B1", "B2", "B3"} {
		if !impacted[b] {
			t.Errorf("%s should be impacted (first hop)", b)
		}
	}
	for _, c := range []string{"C1", "C2", "C3"} {
		if impacted[c] {
			t.Errorf("%s should NOT be impacted (second hop blocked by maxNodes)", c)
		}
	}
}

// TestBfsImpacted_EmptyGraph verifies BFS on empty adjacency maps returns
// empty impacted set.
func TestBfsImpacted_EmptyGraph(t *testing.T) {
	seeds := map[string]bool{"A": true}
	fwd := map[string][]string{}
	rev := map[string][]string{}

	impacted := bfsImpacted(seeds, fwd, rev, 10, 100)

	if len(impacted) != 0 {
		t.Errorf("expected 0 impacted nodes on empty graph, got %d", len(impacted))
	}
}

// TestExpandFrontier_Basic verifies that expandFrontier returns unvisited
// neighbors and marks them in the impacted set.
func TestExpandFrontier_Basic(t *testing.T) {
	fwd := map[string][]string{"A": {"B", "C"}}
	rev := map[string][]string{"A": {"D"}}
	visited := map[string]bool{}
	impacted := map[string]bool{}

	next := expandFrontier([]string{"A"}, fwd, rev, visited, impacted)

	if !visited["A"] {
		t.Error("A should be marked visited")
	}
	if len(next) != 3 {
		t.Errorf("expected 3 next nodes, got %d: %v", len(next), next)
	}
	for _, n := range []string{"B", "C", "D"} {
		if !impacted[n] {
			t.Errorf("%s should be in impacted set", n)
		}
	}
}

// TestExpandFrontier_SkipsVisited verifies that already-visited nodes are
// not added to the next frontier.
func TestExpandFrontier_SkipsVisited(t *testing.T) {
	fwd := map[string][]string{"A": {"B", "C"}}
	rev := map[string][]string{}
	visited := map[string]bool{"B": true} // B already visited
	impacted := map[string]bool{}

	next := expandFrontier([]string{"A"}, fwd, rev, visited, impacted)

	for _, n := range next {
		if n == "B" {
			t.Error("B should not be in next frontier (already visited)")
		}
	}
	if len(next) != 1 || next[0] != "C" {
		t.Errorf("expected [C], got %v", next)
	}
}

// TestBfsImpacted_EmptySeeds returns an empty impacted set when no seeds.
func TestBfsImpacted_EmptySeeds(t *testing.T) {
	impacted := bfsImpacted(map[string]bool{}, map[string][]string{"A": {"B"}}, map[string][]string{}, 10, 100)
	if len(impacted) != 0 {
		t.Errorf("expected empty impacted, got %d", len(impacted))
	}
}

// TestBfsImpacted_ZeroDepth never expands beyond the seeds.
func TestBfsImpacted_ZeroDepth(t *testing.T) {
	seeds := map[string]bool{"A": true}
	fwd := map[string][]string{"A": {"B"}}
	impacted := bfsImpacted(seeds, fwd, map[string][]string{}, 0, 100)
	if len(impacted) != 0 {
		t.Errorf("depth=0 should yield no impacted, got %v", impacted)
	}
}

// TestAllQualifiedNames_Empty returns empty slice for empty input.
func TestAllQualifiedNames_Empty(t *testing.T) {
	all := allQualifiedNames(nil, nil)
	if len(all) != 0 {
		t.Errorf("expected empty, got %v", all)
	}
}

// TestAllQualifiedNames_Union verifies the union of seeds and impacted.
func TestAllQualifiedNames_Union(t *testing.T) {
	seeds := map[string]bool{"A": true, "B": true}
	impacted := map[string]bool{"C": true, "D": true}

	all := allQualifiedNames(seeds, impacted)

	if len(all) != 4 {
		t.Errorf("expected 4 qualified names, got %d", len(all))
	}
	found := map[string]bool{}
	for _, q := range all {
		found[q] = true
	}
	for _, want := range []string{"A", "B", "C", "D"} {
		if !found[want] {
			t.Errorf("missing %q in allQualifiedNames result", want)
		}
	}
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
