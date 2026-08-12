package graphstore

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// exportingBridge is a backend that also satisfies nodeExporter, i.e. the shape
// every CodeGraphProvider has after the §11 cutover. It drives the warm-store
// fallbacks: with an unmirrored warm store, tools must still answer from the
// backend's own graph.
type exportingBridge struct {
	fakeMCPBridge
	nodes    []GraphNode
	nodesErr error
}

func (e *exportingBridge) ReadNodes(int) ([]GraphNode, error) {
	if e.nodesErr != nil {
		return nil, e.nodesErr
	}
	return e.nodes, nil
}

func (e *exportingBridge) ReadEdges(int) ([]GraphEdge, error) { return nil, nil }

func (e *exportingBridge) BuildReport(BuildOptions) (*CRGOperationReport, error) {
	return &CRGOperationReport{Operation: "build"}, nil
}

func (e *exportingBridge) UpdateReport(UpdateOptions) (*CRGOperationReport, error) {
	return &CRGOperationReport{Operation: "update"}, nil
}

// exportingBridge is a full CodeGraphProvider, which is what lets it stand in
// for either shipped backend in the constructor test below.
var _ CodeGraphProvider = (*exportingBridge)(nil)

// nativeNodes is a small backend graph: one File row plus two symbols.
func nativeNodes() []GraphNode {
	return []GraphNode{
		{Kind: NodeKindFile, Name: "lib.go", QualifiedName: "lib/lib.go::lib.go", FilePath: "lib/lib.go"},
		{Kind: NodeKindFunction, Name: "Greet", QualifiedName: "lib.Greet", FilePath: "lib/lib.go"},
		{Kind: NodeKindType, Name: "Config", QualifiedName: "lib.Config", FilePath: "lib/lib.go"},
	}
}

// serverWithBackend builds a server whose warm store is absent, so every read
// falls through to the backend.
func serverWithBackend(bridge mcpBridge) *MCPServer {
	return &MCPServer{bridge: bridge, workDir: "."}
}

// callTool dispatches one tools/call and decodes the JSON result.
func callTool(t *testing.T, s *MCPServer, name, args string) map[string]any {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name, "arguments": json.RawMessage(args)})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	raw, callErr := s.dispatch("tools/call", nil, params)
	if callErr != nil {
		t.Fatalf("%s: %v", name, callErr)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: decode %s: %v", name, raw, err)
	}
	return out
}

func TestNativeGraphDBPathIsRepoLocal(t *testing.T) {
	got := filepath.ToSlash(NativeGraphDBPath("/repo"))
	if got != "/repo/.dot-agents/code-graph.db" {
		t.Fatalf("NativeGraphDBPath = %q", got)
	}
	if got == filepath.ToSlash(CRGDBPath("/repo")) {
		t.Fatal("the native and bridge graphs must not share a database file")
	}
}

func TestNewMCPServerWithProviderUsesSuppliedBackend(t *testing.T) {
	bridge := &exportingBridge{nodes: nativeNodes()}
	srv := NewMCPServerWithProvider(t.TempDir(), bridge, nil)
	got, err := srv.requireBridge()
	if err != nil || got != bridge {
		t.Fatalf("requireBridge = (%v, %v), want the supplied provider", got, err)
	}
}

func TestNewMCPServerWithProviderSurfacesProviderError(t *testing.T) {
	want := errors.New("backend unavailable")
	srv := NewMCPServerWithProvider(t.TempDir(), nil, want)
	if _, err := srv.requireBridge(); !errors.Is(err, want) {
		t.Fatalf("requireBridge err = %v, want %v", err, want)
	}
}

func TestSemanticSearchFallsBackToBackendNodes(t *testing.T) {
	srv := serverWithBackend(&exportingBridge{nodes: nativeNodes()})
	out := callTool(t, srv, "semantic_search_nodes_tool", `{"query":"greet"}`)
	results, _ := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v, want the one matching symbol", out)
	}
	row, _ := results[0].(map[string]any)
	if row["name"] != "Greet" || row["summary"] != "lib.Greet" {
		t.Fatalf("row = %v", row)
	}
}

func TestSemanticSearchExcludesFileNodes(t *testing.T) {
	srv := serverWithBackend(&exportingBridge{nodes: nativeNodes()})
	out := callTool(t, srv, "semantic_search_nodes_tool", `{"query":"lib"}`)
	results, _ := out["results"].([]any)
	for _, r := range results {
		if row, _ := r.(map[string]any); row["type"] == NodeKindFile {
			t.Fatalf("file node leaked into search results: %v", row)
		}
	}
}

func TestBackendNodesExportsOnceAndToleratesFailure(t *testing.T) {
	failing := &exportingBridge{nodesErr: errors.New("export failed")}
	srv := serverWithBackend(failing)
	if nodes := srv.backendNodes(); nodes != nil {
		t.Fatalf("backendNodes = %v, want nil on export failure", nodes)
	}
	// A second call must not retry the failed export.
	if nodes := srv.backendNodes(); nodes != nil {
		t.Fatalf("backendNodes retried after failure: %v", nodes)
	}
}

func TestBackendNodesIgnoresNonExportingBackend(t *testing.T) {
	srv := serverWithBackend(&fakeMCPBridge{})
	if nodes := srv.backendNodes(); nodes != nil {
		t.Fatalf("backendNodes = %v, want nil for a backend without ReadNodes", nodes)
	}
}

func TestResolveImpactFilesUsesBackendWhenWarmStoreEmpty(t *testing.T) {
	srv := serverWithBackend(&exportingBridge{nodes: nativeNodes()})
	files := srv.resolveImpactFiles("Greet")
	if len(files) != 1 || files[0] != "lib/lib.go" {
		t.Fatalf("resolveImpactFiles = %v, want the symbol's file", files)
	}
}

func TestResolveImpactFilesFallsBackToRawSymbol(t *testing.T) {
	srv := serverWithBackend(&exportingBridge{})
	files := srv.resolveImpactFiles("Nowhere")
	if len(files) != 1 || files[0] != "Nowhere" {
		t.Fatalf("resolveImpactFiles = %v, want the raw symbol", files)
	}
}

func TestListGraphStatsFallsBackToBackendCounts(t *testing.T) {
	bridge := &exportingBridge{}
	bridge.statusSeq = []*CRGStatus{{Nodes: 8, Edges: 5, Languages: "go"}}
	// An empty-but-present warm mirror is exactly the post-cutover default:
	// the code graph lives with the backend until `kg warm --include-code`
	// mirrors it, so the stats tool must not report a zeroed graph.
	srv := serverWithBackend(bridge)
	srv.store = &fakeMCPStore{}
	out := callTool(t, srv, "list_graph_stats_tool", `{}`)
	if out["nodes"] != float64(8) || out["edges"] != float64(5) {
		t.Fatalf("stats = %v, want the backend counts", out)
	}
}

func TestBackendCountsWithoutBridgeIsZero(t *testing.T) {
	srv := &MCPServer{}
	if nodes, edges := srv.backendCounts(); nodes != 0 || edges != 0 {
		t.Fatalf("backendCounts = (%d, %d), want zeros", nodes, edges)
	}
}

func TestBackendCountsToleratesStatusError(t *testing.T) {
	srv := serverWithBackend(&fakeMCPBridge{statusErr: errors.New("boom")})
	if nodes, edges := srv.backendCounts(); nodes != 0 || edges != 0 {
		t.Fatalf("backendCounts = (%d, %d), want zeros on status error", nodes, edges)
	}
}

func TestReviewImpactNodesFallsBackToBackend(t *testing.T) {
	bridge := &exportingBridge{}
	bridge.impact = &CRGImpactResult{
		ChangedNodes:  []ImpactNode{{Name: "Greet", QualifiedName: "lib.Greet", Kind: NodeKindFunction, FilePath: "lib/lib.go"}},
		ImpactedNodes: []ImpactNode{{Name: "Run", QualifiedName: "app.Run", Kind: NodeKindFunction, FilePath: "app/app.go"}},
	}
	srv := serverWithBackend(bridge)
	nodes := srv.reviewImpactNodes([]string{"lib/lib.go"})
	if len(nodes) != 2 || nodes[0]["name"] != "Greet" {
		t.Fatalf("reviewImpactNodes = %v", nodes)
	}
}

func TestBackendImpactNodesEmptyWithoutBridge(t *testing.T) {
	srv := &MCPServer{}
	if nodes := srv.backendImpactNodes([]string{"a.go"}); len(nodes) != 0 {
		t.Fatalf("backendImpactNodes = %v, want empty", nodes)
	}
}

func TestBackendImpactNodesToleratesError(t *testing.T) {
	srv := serverWithBackend(&fakeMCPBridge{impactErr: errors.New("boom")})
	if nodes := srv.backendImpactNodes([]string{"a.go"}); len(nodes) != 0 {
		t.Fatalf("backendImpactNodes = %v, want empty on error", nodes)
	}
}

func TestUniqueFilePathsDropsBlanksAndDuplicates(t *testing.T) {
	got := uniqueFilePaths([]GraphNode{
		{FilePath: "a.go"}, {FilePath: ""}, {FilePath: "a.go"}, {FilePath: "b.go"},
	})
	if strings.Join(got, ",") != "a.go,b.go" {
		t.Fatalf("uniqueFilePaths = %v", got)
	}
}

func TestMatchBackendNodesHonorsLimit(t *testing.T) {
	srv := serverWithBackend(&exportingBridge{nodes: nativeNodes()})
	if got := srv.matchBackendNodes("", 1); len(got) != 1 {
		t.Fatalf("matchBackendNodes limit ignored: %d rows", len(got))
	}
}

func TestNodeMatchesEmptyNeedleMatchesEverything(t *testing.T) {
	if !nodeMatches(GraphNode{Name: "X"}, "") {
		t.Fatal("empty needle must match")
	}
	if nodeMatches(GraphNode{Name: "X", QualifiedName: "p.X"}, "zzz") {
		t.Fatal("non-matching needle must not match")
	}
}
