package codegraph

import "github.com/AGOrcha/dot-agents/internal/graphstore"

// NullProvider is the code-graph backend selected by the `none` adapter
// (graph-backend-adapter-contract §13.1): every operation succeeds and returns
// an empty, well-formed result, and status reports the graph as unbuilt.
//
// It exists so "this project consumes no graph backend" is a first-class,
// non-erroring configuration rather than a nil provider every call site has to
// guard.
type NullProvider struct{}

// Compile-time proof the null backend satisfies the same contract.
var _ graphstore.CodeGraphProvider = NullProvider{}

// nullStatus is the status a disabled backend reports.
func nullStatus() *graphstore.CRGStatus {
	return &graphstore.CRGStatus{
		LastUpdated: "never",
		State:       graphstore.CRGReadinessUnbuilt,
		Message:     "graph backend disabled (none)",
	}
}

// BuildReport reports that no build ran.
func (NullProvider) BuildReport(graphstore.BuildOptions) (*graphstore.CRGOperationReport, error) {
	return &graphstore.CRGOperationReport{
		Operation: "build",
		Outcome:   graphstore.CRGReadinessUnbuilt,
		Summary:   "Graph backend is disabled (none); nothing was built.",
		Status:    nullStatus(),
	}, nil
}

// Build is a no-op.
func (n NullProvider) Build(graphstore.BuildOptions) error { return nil }

// UpdateReport reports that no update ran.
func (NullProvider) UpdateReport(graphstore.UpdateOptions) (*graphstore.CRGOperationReport, error) {
	return &graphstore.CRGOperationReport{
		Operation: "update",
		Outcome:   "no_diff",
		Summary:   "Graph backend is disabled (none); nothing was updated.",
		Status:    nullStatus(),
	}, nil
}

// Update is a no-op.
func (n NullProvider) Update(graphstore.UpdateOptions) error { return nil }

// Status reports the graph as unbuilt.
func (NullProvider) Status() (*graphstore.CRGStatus, error) { return nullStatus(), nil }

// GetImpactRadius returns an empty impact result.
func (NullProvider) GetImpactRadius(opts graphstore.ImpactOptions) (*graphstore.CRGImpactResult, error) {
	return &graphstore.CRGImpactResult{
		Status:       statusOK,
		Summary:      "Graph backend is disabled (none).",
		ChangedFiles: opts.ChangedFiles,
	}, nil
}

// ListFlows returns no flows.
func (NullProvider) ListFlows(int, string) (*graphstore.FlowsResult, error) {
	return &graphstore.FlowsResult{Status: statusOK, Summary: "Graph backend is disabled (none)."}, nil
}

// ListCommunities returns no communities.
func (NullProvider) ListCommunities(int, string) (*graphstore.CommunitiesResult, error) {
	return &graphstore.CommunitiesResult{Status: statusOK, Summary: "Graph backend is disabled (none)."}, nil
}

// Postprocess is a no-op.
func (NullProvider) Postprocess(graphstore.PostprocessOptions) error { return nil }

// DetectChanges returns an empty change report.
func (NullProvider) DetectChanges(graphstore.DetectChangesOptions) (*graphstore.CRGChangeReport, error) {
	return &graphstore.CRGChangeReport{Summary: "Graph backend is disabled (none)."}, nil
}

// ReadNodes returns no nodes.
func (NullProvider) ReadNodes(int) ([]graphstore.GraphNode, error) { return nil, nil }

// ReadEdges returns no edges.
func (NullProvider) ReadEdges(int) ([]graphstore.GraphEdge, error) { return nil, nil }
