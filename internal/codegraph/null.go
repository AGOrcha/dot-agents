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

const disabledSummary = "Graph backend is disabled (none)."

// nullStatus is the status a disabled backend reports.
func nullStatus() *graphstore.CRGStatus {
	return &graphstore.CRGStatus{
		Languages: []string{},
		State:     graphstore.CRGReadinessUnbuilt,
		Message:   "graph backend disabled (none)",
		VCS:       graphstore.VCSNone,
	}
}

// BuildReport reports that no build ran. The report keeps upstream's shape —
// a full build with zero of everything — so a caller does not need a
// disabled-backend special case to read it.
func (NullProvider) BuildReport(graphstore.BuildOptions) (*graphstore.CRGOperationReport, error) {
	return &graphstore.CRGOperationReport{
		Status:            statusOK,
		BuildType:         buildTypeFull,
		BaseResolved:      graphstore.NullString(),
		Summary:           "Graph backend is disabled (none); nothing was built.",
		FilesParsed:       new(0),
		TotalNodes:        new(0),
		TotalEdges:        new(0),
		StaleFilesRemoved: new(0),
		Errors:            new([]graphstore.BuildErrorRow{}),
	}, nil
}

// Build is a no-op.
func (n NullProvider) Build(graphstore.BuildOptions) error { return nil }

// UpdateReport reports that no update ran.
func (NullProvider) UpdateReport(graphstore.UpdateOptions) (*graphstore.CRGOperationReport, error) {
	return &graphstore.CRGOperationReport{
		Status:            statusOK,
		BuildType:         buildTypeIncremental,
		BaseResolved:      graphstore.NullString(),
		Summary:           "Graph backend is disabled (none); nothing was updated.",
		FilesUpdated:      new(0),
		TotalNodes:        new(0),
		TotalEdges:        new(0),
		ChangedFiles:      new([]string{}),
		DependentFiles:    new([]string{}),
		StaleFilesRemoved: new(0),
		Errors:            new([]graphstore.BuildErrorRow{}),
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
		Summary:      disabledSummary,
		ChangedFiles: opts.ChangedFiles,
	}, nil
}

// ListFlows returns no flows.
func (NullProvider) ListFlows(int, string) (*graphstore.FlowsResult, error) {
	return &graphstore.FlowsResult{Status: statusOK, Summary: disabledSummary}, nil
}

// ListCommunities returns no communities.
func (NullProvider) ListCommunities(int, string) (*graphstore.CommunitiesResult, error) {
	return &graphstore.CommunitiesResult{Status: statusOK, Summary: disabledSummary}, nil
}

// PostprocessReport reports that no post-processing ran.
func (NullProvider) PostprocessReport(graphstore.PostprocessOptions) (*graphstore.CRGOperationReport, error) {
	return &graphstore.CRGOperationReport{Status: statusOK, Summary: disabledSummary}, nil
}

// Postprocess is a no-op.
func (NullProvider) Postprocess(graphstore.PostprocessOptions) error { return nil }

// DetectChanges returns an empty change report.
func (NullProvider) DetectChanges(graphstore.DetectChangesOptions) (*graphstore.CRGChangeReport, error) {
	return &graphstore.CRGChangeReport{Summary: disabledSummary}, nil
}

// ReadNodes returns no nodes.
func (NullProvider) ReadNodes(int) ([]graphstore.GraphNode, error) { return nil, nil }

// ReadEdges returns no edges.
func (NullProvider) ReadEdges(int) ([]graphstore.GraphEdge, error) { return nil, nil }
