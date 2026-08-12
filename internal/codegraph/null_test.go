package codegraph

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// nullReads asserts every read-shaped NullProvider call returns an empty,
// non-erroring, well-formed result.
func nullReads(t *testing.T, n NullProvider) {
	t.Helper()
	impact, err := n.GetImpactRadius(graphstore.ImpactOptions{ChangedFiles: []string{"a.go"}})
	if err != nil || len(impact.ChangedNodes) != 0 || len(impact.ChangedFiles) != 1 {
		t.Errorf("GetImpactRadius = %+v, err %v", impact, err)
	}
	flows, err := n.ListFlows(5, "criticality")
	if err != nil || len(flows.Flows) != 0 {
		t.Errorf("ListFlows = %+v, err %v", flows, err)
	}
	communities, err := n.ListCommunities(0, "size")
	if err != nil || len(communities.Communities) != 0 {
		t.Errorf("ListCommunities = %+v, err %v", communities, err)
	}
	changes, err := n.DetectChanges(graphstore.DetectChangesOptions{})
	if err != nil || changes.Summary == "" {
		t.Errorf("DetectChanges = %+v, err %v", changes, err)
	}
	nodes, err := n.ReadNodes(0)
	if err != nil || len(nodes) != 0 {
		t.Errorf("ReadNodes = %+v, err %v", nodes, err)
	}
	edges, err := n.ReadEdges(0)
	if err != nil || len(edges) != 0 {
		t.Errorf("ReadEdges = %+v, err %v", edges, err)
	}
}

// nullWrites asserts every mutation-shaped NullProvider call is a clean no-op.
func nullWrites(t *testing.T, n NullProvider) {
	t.Helper()
	build, err := n.BuildReport(graphstore.BuildOptions{})
	if err != nil || build.Outcome != graphstore.CRGReadinessUnbuilt {
		t.Errorf("BuildReport = %+v, err %v", build, err)
	}
	update, err := n.UpdateReport(graphstore.UpdateOptions{})
	if err != nil || update.Outcome != "no_diff" {
		t.Errorf("UpdateReport = %+v, err %v", update, err)
	}
	if err := n.Build(graphstore.BuildOptions{}); err != nil {
		t.Errorf("Build: %v", err)
	}
	if err := n.Update(graphstore.UpdateOptions{}); err != nil {
		t.Errorf("Update: %v", err)
	}
	if err := n.Postprocess(graphstore.PostprocessOptions{}); err != nil {
		t.Errorf("Postprocess: %v", err)
	}
}

func TestNullProviderReadsAreEmpty(t *testing.T) {
	nullReads(t, NullProvider{})
}

func TestNullProviderWritesAreNoOps(t *testing.T) {
	nullWrites(t, NullProvider{})
}

func TestNullProviderStatusIsUnbuilt(t *testing.T) {
	status, err := NullProvider{}.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != graphstore.CRGReadinessUnbuilt || status.Ready || status.Message == "" {
		t.Fatalf("status = %+v, want a disabled/unbuilt report", status)
	}
}
