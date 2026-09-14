package codegraph

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// stubFlows returns two flows whose criticality ties, so both the tie-break
// and the name ordering are reachable.
func stubFlows() []graphstore.FlowInfo {
	return []graphstore.FlowInfo{
		{ID: 2, Name: "z", Criticality: 2},
		{ID: 1, Name: "a", Criticality: 2},
	}
}

func TestSortFlowsOrdersByNameThenID(t *testing.T) {
	for _, sortBy := range []string{"name", "entry_point"} {
		flows := stubFlows()
		sortFlows(flows, sortBy)
		if flows[0].Name != "a" {
			t.Errorf("sortFlows(%q) = %+v, want 'a' first", sortBy, flows)
		}
	}
	flows := stubFlows()
	sortFlows(flows, "")
	if flows[0].ID != 1 {
		t.Fatalf("criticality tie-break = %+v, want the lower id first", flows)
	}
}

// stubCommunities returns two same-size communities so every sort key changes
// the order in an observable way.
func stubCommunities() []graphstore.CommunityInfo {
	return []graphstore.CommunityInfo{
		{Name: "b", Size: 2, Cohesion: 0.1},
		{Name: "a", Size: 2, Cohesion: 0.9},
	}
}

func TestSortCommunitiesSupportsEveryKey(t *testing.T) {
	for _, sortBy := range []string{"cohesion", "name", ""} {
		got := stubCommunities()
		sortCommunities(got, sortBy)
		if got[0].Name != "a" {
			t.Errorf("sortCommunities(%q) = %+v, want 'a' first", sortBy, got)
		}
	}
}

func TestTestGapsSkipTestDeclarationsAndCoveredSymbols(t *testing.T) {
	changed := []graphstore.GraphNode{
		{QualifiedName: "/r/f_test.go::TestX", FilePath: "/r/f_test.go", IsTest: true},
		{QualifiedName: "/r/f.go::X", FilePath: "/r/f.go"},
		{QualifiedName: "/r/f.go::Y", FilePath: "/r/f.go"},
	}
	gaps := testGaps(changed, map[string]bool{"/r/f.go::Y": true})
	if len(gaps) != 1 || gaps[0].QualifiedName != "/r/f.go::X" {
		t.Fatalf("testGaps = %+v, want only the untested production symbol", gaps)
	}
}
