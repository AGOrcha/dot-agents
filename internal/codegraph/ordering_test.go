package codegraph

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// stubFlows returns two flows whose criticality ties, so both the tie-break and
// the entry-point ordering are reachable.
func stubFlows() []crg.Flow {
	return []crg.Flow{
		{ID: "z@f.go", EntryPoint: "z", Criticality: 2},
		{ID: "a@f.go", EntryPoint: "a", Criticality: 2},
	}
}

func TestSortFlowsOrdersByEntryPointThenID(t *testing.T) {
	flows := stubFlows()
	sortFlows(flows, "name")
	if flows[0].EntryPoint != "a" {
		t.Fatalf("name sort = %+v", flows)
	}
	flows = stubFlows()
	sortFlows(flows, "entry_point")
	if flows[0].EntryPoint != "a" {
		t.Fatalf("entry_point sort = %+v", flows)
	}
	flows = stubFlows()
	sortFlows(flows, "")
	if flows[0].ID != "a@f.go" {
		t.Fatalf("criticality tie-break = %+v", flows)
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

func TestTestGapsSkipTestDeclarations(t *testing.T) {
	snap := graphSnapshot{nodeByID: map[string]graphstore.GraphNode{
		"t@f": {QualifiedName: "pkg.TestX", FilePath: "f_test.go", IsTest: true},
		"p@f": {QualifiedName: "pkg.X", FilePath: "f.go"},
	}}
	gaps := testGaps(snap, []string{"t@f", "p@f"})
	if len(gaps) != 1 || gaps[0].QualifiedName != "pkg.X" {
		t.Fatalf("testGaps = %+v, want only the production symbol", gaps)
	}
}

func TestDetectChangesRespectsExplicitFileList(t *testing.T) {
	e := builtEngine(t, nil)
	report, err := e.DetectChanges(graphstore.DetectChangesOptions{Files: []string{"nowhere.go"}})
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	if len(report.ChangedFunctions) != 0 {
		t.Fatalf("want no changed symbols for an unknown file: %+v", report)
	}
}

func TestListFlowsAppliesDefaultLimit(t *testing.T) {
	e := builtEngine(t, nil)
	result, err := e.ListFlows(-1, "criticality")
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(result.Flows) > defaultFlowLimit {
		t.Fatalf("default limit not applied: %d flows", len(result.Flows))
	}
}
