package graphstore

import "path/filepath"

// CodeGraphProvider is the code-graph operation surface every `da kg` code
// command and the MCP server depend on. It is exactly the eight rows of the
// graph-backend-adapter-contract §11.1 parity matrix (build, update, status,
// impact-radius, flows, communities, postprocess, detect-changes) plus the two
// bulk export reads the warm-link sync needs.
//
// Publishing it as an interface is what makes the §11 cutover a provider swap
// rather than a rewrite: the legacy Python subprocess bridge (*CRGBridge) and
// the kg-native engine (internal/codegraph, which cannot live in this package
// because it depends on the crg adapter, which depends on this package) both
// satisfy it, and the backend a command uses is chosen by config at the call
// site instead of being hard-coded to `NewCRGBridge`.
type CodeGraphProvider interface {
	// BuildReport performs a full graph build and returns a structured report.
	BuildReport(opts BuildOptions) (*CRGOperationReport, error)
	// Build performs a full graph build, discarding the report.
	Build(opts BuildOptions) error
	// UpdateReport performs an incremental update and returns a report.
	UpdateReport(opts UpdateOptions) (*CRGOperationReport, error)
	// Update performs an incremental update, discarding the report.
	Update(opts UpdateOptions) error
	// Status reports node/edge/file counts and graph readiness.
	Status() (*CRGStatus, error)
	// GetImpactRadius returns the blast radius of a changed-file set.
	GetImpactRadius(opts ImpactOptions) (*CRGImpactResult, error)
	// ListFlows returns the detected execution flows.
	ListFlows(limit int, sortBy string) (*FlowsResult, error)
	// ListCommunities returns the detected code communities.
	ListCommunities(minSize int, sortBy string) (*CommunitiesResult, error)
	// Postprocess recomputes the derived views (flows/communities/FTS).
	Postprocess(opts PostprocessOptions) error
	// DetectChanges returns the change-impact report for the current diff.
	DetectChanges(opts DetectChangesOptions) (*CRGChangeReport, error)
	// ReadNodes bulk-exports code nodes (limit <= 0 means all).
	ReadNodes(limit int) ([]GraphNode, error)
	// ReadEdges bulk-exports code edges (limit <= 0 means all).
	ReadEdges(limit int) ([]GraphEdge, error)
}

// The legacy Python subprocess bridge is one implementation of the contract.
// It stays registered here (not deleted) because §11.4 keeps the bridge as the
// documented rollback path until the decommissioning gate passes.
var _ CodeGraphProvider = (*CRGBridge)(nil)

// nativeGraphDir is the repo-local directory holding the kg-native code graph.
// It deliberately mirrors the bridge's `.code-review-graph/` layout so the two
// backends never share a database file and a rollback cannot read a graph the
// other backend wrote.
const nativeGraphDir = ".dot-agents"

// NativeGraphDBPath returns the SQLite path the kg-native code-graph backend
// persists repoRoot's graph to. Keeping the path helper next to the bridge's
// CRGDBPath makes the two storage locations reviewable side by side.
func NativeGraphDBPath(repoRoot string) string {
	return filepath.Join(repoRoot, nativeGraphDir, "code-graph.db")
}
