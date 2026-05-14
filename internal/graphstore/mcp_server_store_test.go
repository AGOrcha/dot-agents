package graphstore

import (
	"encoding/json"
	"errors"
	"testing"
)

// fakeMCPStore is a minimal in-memory Store double for exercising MCPServer
// branches that depend on s.store returning real data. All non-essential
// methods return zero values / nil errors. Tests should populate only the
// fields they care about.
type fakeMCPStore struct {
	searchNodesResult   []GraphNode
	searchNodesErr      error
	searchKGNotesResult []KGNote
	searchKGNotesErr    error
	impactResult        ImpactResult
	impactErr           error
	stats               GraphStats
	statsErr            error
}

func (f *fakeMCPStore) UpsertNode(node NodeInfo, fileHash string) (int64, error) { return 0, nil }
func (f *fakeMCPStore) UpsertEdge(edge EdgeInfo) (int64, error)                  { return 0, nil }
func (f *fakeMCPStore) RemoveFileData(filePath string) error                     { return nil }
func (f *fakeMCPStore) StoreFileNodesEdges(filePath string, nodes []NodeInfo, edges []EdgeInfo, fileHash string) error {
	return nil
}
func (f *fakeMCPStore) SetMetadata(key, value string) error  { return nil }
func (f *fakeMCPStore) Commit() error                        { return nil }
func (f *fakeMCPStore) GetNode(q string) (*GraphNode, error) { return nil, nil }
func (f *fakeMCPStore) GetNodesByFile(filePath string) ([]GraphNode, error) {
	return nil, nil
}
func (f *fakeMCPStore) GetEdgesBySource(q string) ([]GraphEdge, error) { return nil, nil }
func (f *fakeMCPStore) GetEdgesByTarget(q string) ([]GraphEdge, error) { return nil, nil }
func (f *fakeMCPStore) GetEdgesAmong(qs []string) ([]GraphEdge, error) { return nil, nil }
func (f *fakeMCPStore) GetAllFiles() ([]string, error)                 { return nil, nil }
func (f *fakeMCPStore) SearchNodes(q string, limit int) ([]GraphNode, error) {
	return f.searchNodesResult, f.searchNodesErr
}
func (f *fakeMCPStore) GetMetadata(key string) (string, error) { return "", nil }
func (f *fakeMCPStore) GetStats() (GraphStats, error)          { return f.stats, f.statsErr }
func (f *fakeMCPStore) GetImpactRadius(changed []string, depth, maxNodes int) (ImpactResult, error) {
	return f.impactResult, f.impactErr
}
func (f *fakeMCPStore) UpsertKGNote(note KGNote) error       { return nil }
func (f *fakeMCPStore) GetKGNote(id string) (*KGNote, error) { return nil, nil }
func (f *fakeMCPStore) SearchKGNotes(q string, limit int) ([]KGNote, error) {
	return f.searchKGNotesResult, f.searchKGNotesErr
}
func (f *fakeMCPStore) ListArchivedKGNotes() ([]KGNote, error)               { return nil, nil }
func (f *fakeMCPStore) UpsertNoteSymbolLink(l NoteSymbolLink) (int64, error) { return 0, nil }
func (f *fakeMCPStore) GetLinksForNote(id string) ([]NoteSymbolLink, error)  { return nil, nil }
func (f *fakeMCPStore) GetLinksForSymbol(q string) ([]NoteSymbolLink, error) { return nil, nil }
func (f *fakeMCPStore) DeleteNoteSymbolLink(id int64) error                  { return nil }
func (f *fakeMCPStore) Close() error                                         { return nil }

// resolveImpactFiles

func TestResolveImpactFiles_NoStoreFallsBack(t *testing.T) {
	srv := &MCPServer{}
	got := srv.resolveImpactFiles("pkg.foo")
	if len(got) != 1 || got[0] != "pkg.foo" {
		t.Fatalf("expected single-element fallback, got %v", got)
	}
}

func TestResolveImpactFiles_StoreErrorFallsBack(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{searchNodesErr: errors.New("boom")}}
	got := srv.resolveImpactFiles("pkg.foo")
	if len(got) != 1 || got[0] != "pkg.foo" {
		t.Fatalf("expected fallback on error, got %v", got)
	}
}

func TestResolveImpactFiles_DedupesFilePaths(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{searchNodesResult: []GraphNode{
		{FilePath: "a.go"},
		{FilePath: "b.go"},
		{FilePath: "a.go"}, // dup
		{FilePath: ""},     // skipped
	}}}
	got := srv.resolveImpactFiles("pkg.foo")
	if len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Fatalf("expected ['a.go','b.go'], got %v", got)
	}
}

func TestResolveImpactFiles_AllEmptyFilesFallback(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{searchNodesResult: []GraphNode{
		{FilePath: ""},
	}}}
	got := srv.resolveImpactFiles("pkg.foo")
	if len(got) != 1 || got[0] != "pkg.foo" {
		t.Fatalf("expected fallback when all file paths empty, got %v", got)
	}
}

// reviewImpactNodes

func TestReviewImpactNodes_StoreError(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{impactErr: errors.New("query failed")}}
	got := srv.reviewImpactNodes([]string{"a.go"})
	if len(got) != 0 {
		t.Fatalf("expected empty slice on store error, got %v", got)
	}
}

func TestReviewImpactNodes_StoreReturnsNodes(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{impactResult: ImpactResult{
		ChangedNodes:  []GraphNode{{Name: "a", Kind: "Function", FilePath: "a.go"}},
		ImpactedNodes: []GraphNode{{Name: "b", Kind: "Function", FilePath: "b.go"}},
	}}}
	got := srv.reviewImpactNodes([]string{"a.go"})
	if len(got) != 2 {
		t.Fatalf("expected 2 nodes (1 changed + 1 impacted), got %d: %v", len(got), got)
	}
}

// handleSemanticSearchNodes — store path

func TestHandleSemanticSearchNodes_WithStore(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{searchNodesResult: []GraphNode{
		{Name: "Foo", Kind: "Function", FilePath: "foo.go", QualifiedName: "pkg.Foo"},
	}}}
	out, err := srv.handleSemanticSearchNodes(json.RawMessage(`{"query":"foo","limit":5}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if uerr := json.Unmarshal(out, &payload); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(payload.Results))
	}
	if payload.Results[0]["name"] != "Foo" {
		t.Fatalf("expected name=Foo, got %v", payload.Results[0])
	}
}

func TestHandleSemanticSearchNodes_StoreErrorReturnsEmptyResults(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{searchNodesErr: errors.New("boom")}}
	out, err := srv.handleSemanticSearchNodes(json.RawMessage(`{"query":"foo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if uerr := json.Unmarshal(out, &payload); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	if len(payload.Results) != 0 {
		t.Fatalf("expected empty results on store err, got %v", payload.Results)
	}
}

func TestHandleSemanticSearchNodes_InvalidJSON(t *testing.T) {
	srv := &MCPServer{}
	_, err := srv.handleSemanticSearchNodes(json.RawMessage(`{not-json`))
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

// handleQueryGraph — default branch (unknown intent) with KGNote results

func TestHandleQueryGraph_DefaultBranchWithKGNotes(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{searchKGNotesResult: []KGNote{
		{NoteType: "decision", Title: "T1", Summary: "S1"},
		{NoteType: "concept", Title: "T2", Summary: "S2"},
	}}}
	out, err := srv.handleQueryGraph(json.RawMessage(`{"intent":"","query":"foo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload struct {
		Results  []map[string]any `json:"results"`
		Warnings []string         `json:"warnings"`
	}
	if uerr := json.Unmarshal(out, &payload); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	if len(payload.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(payload.Results))
	}
	if len(payload.Warnings) != 0 {
		t.Fatalf("expected no warnings when intent is empty, got %v", payload.Warnings)
	}
}

func TestHandleQueryGraph_DefaultBranchWithIntentEmitsWarning(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{}}
	out, err := srv.handleQueryGraph(json.RawMessage(`{"intent":"bogus","query":"foo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload struct {
		Warnings []string `json:"warnings"`
	}
	if uerr := json.Unmarshal(out, &payload); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	if len(payload.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %v", payload.Warnings)
	}
}

func TestHandleQueryGraph_DefaultBranchStoreErr(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{searchKGNotesErr: errors.New("boom")}}
	out, err := srv.handleQueryGraph(json.RawMessage(`{"intent":"","query":"foo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if uerr := json.Unmarshal(out, &payload); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	if len(payload.Results) != 0 {
		t.Fatalf("expected no results on store err, got %v", payload.Results)
	}
}

func TestHandleQueryGraph_ImpactIntent(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{{State: string(CRGReadinessReady), Ready: true, Nodes: 1}},
		impact:    &CRGImpactResult{},
	}
	srv := &MCPServer{bridge: bridge}
	out, err := srv.handleQueryGraph(json.RawMessage(`{"intent":"impact_radius","query":"pkg.foo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("expected non-empty output")
	}
}

func TestHandleQueryGraph_ReviewContextIntent(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{{State: string(CRGReadinessReady), Ready: true, Nodes: 1}},
		detect:    &CRGChangeReport{Summary: "ok"},
	}
	srv := &MCPServer{bridge: bridge}
	out, err := srv.handleQueryGraph(json.RawMessage(`{"intent":"review_context","query":"a.go"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("expected non-empty output")
	}
}

func TestHandleQueryGraph_InvalidJSON(t *testing.T) {
	srv := &MCPServer{}
	_, err := srv.handleQueryGraph(json.RawMessage(`not-json`))
	if err == nil {
		t.Fatalf("expected error")
	}
}

// handleListGraphStats — populated path

func TestHandleListGraphStats_WithStore(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{stats: GraphStats{
		TotalNodes: 10,
		TotalEdges: 20,
		Languages:  []string{"go", "python"},
	}}}
	out, err := srv.handleListGraphStats(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload struct {
		Nodes int `json:"nodes"`
		Edges int `json:"edges"`
	}
	if uerr := json.Unmarshal(out, &payload); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	if payload.Nodes != 10 || payload.Edges != 20 {
		t.Fatalf("unexpected: %+v", payload)
	}
}

func TestHandleListGraphStats_StoreError(t *testing.T) {
	srv := &MCPServer{store: &fakeMCPStore{statsErr: errors.New("boom")}}
	_, err := srv.handleListGraphStats(nil)
	if err == nil {
		t.Fatalf("expected error from store")
	}
}

// loadStats branches

func TestLoadStats_StoreErrFallback(t *testing.T) {
	srv := &MCPServer{storeErr: errors.New("open failed")}
	_, err := srv.loadStats()
	if err == nil || err.Error() != "open failed" {
		t.Fatalf("expected open failed err, got %v", err)
	}
}

func TestLoadStats_NoStoreNoErr(t *testing.T) {
	srv := &MCPServer{}
	_, err := srv.loadStats()
	if err == nil {
		t.Fatalf("expected sentinel error when store unavailable")
	}
}

// handleGetImpactRadius — invalid JSON

func TestHandleGetImpactRadius_InvalidJSON(t *testing.T) {
	srv := &MCPServer{}
	_, err := srv.handleGetImpactRadius(json.RawMessage(`not-json`))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleGetImpactRadius_BridgeErrorPropagates(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{{State: string(CRGReadinessReady), Ready: true, Nodes: 1}},
		impactErr: errors.New("bridge boom"),
	}
	srv := &MCPServer{bridge: bridge}
	_, err := srv.handleGetImpactRadius(json.RawMessage(`{"symbol":"foo","depth":2}`))
	if err == nil {
		t.Fatalf("expected bridge error to propagate")
	}
}

// handleGetReviewContext — invalid JSON

func TestHandleGetReviewContext_InvalidJSON(t *testing.T) {
	srv := &MCPServer{}
	_, err := srv.handleGetReviewContext(json.RawMessage(`not-json`))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleGetReviewContext_DetectChangesErrPropagates(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{{State: string(CRGReadinessReady), Ready: true, Nodes: 1}},
		detectErr: errors.New("detect failed"),
	}
	srv := &MCPServer{bridge: bridge}
	_, err := srv.handleGetReviewContext(json.RawMessage(`{"files":["a.go"]}`))
	if err == nil {
		t.Fatalf("expected detect error to propagate")
	}
}

func TestHandleGetReviewContext_EmptyRiskSummaryFallback(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{{State: string(CRGReadinessReady), Ready: true, Nodes: 1}},
		detect:    &CRGChangeReport{Summary: "   "}, // whitespace -> fallback
	}
	srv := &MCPServer{bridge: bridge}
	out, err := srv.handleGetReviewContext(json.RawMessage(`{"files":["a.go"]}`))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	var payload map[string]any
	_ = json.Unmarshal(out, &payload)
	rs, _ := payload["risk_summary"].(string)
	if rs == "" || rs == "   " {
		t.Fatalf("expected synthesized risk_summary, got %q", rs)
	}
}

// handleGetDocsSection — invalid JSON

func TestHandleGetDocsSection_InvalidJSON(t *testing.T) {
	srv := &MCPServer{}
	_, err := srv.handleGetDocsSection(json.RawMessage(`not-json`))
	if err == nil {
		t.Fatalf("expected error")
	}
}

// handleBuildOrUpdateGraph — error branches

func TestHandleBuildOrUpdateGraph_BuildError(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{{}},
		buildErr:  errors.New("build failed"),
	}
	srv := &MCPServer{bridge: bridge}
	_, err := srv.handleBuildOrUpdateGraph(nil)
	if err == nil {
		t.Fatalf("expected build error")
	}
}

func TestHandleBuildOrUpdateGraph_UpdateError(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{{Nodes: 1, Files: 1}},
		updateErr: errors.New("update failed"),
	}
	srv := &MCPServer{bridge: bridge}
	_, err := srv.handleBuildOrUpdateGraph(nil)
	if err == nil {
		t.Fatalf("expected update error")
	}
}

func TestHandleBuildOrUpdateGraph_FinalStatusError(t *testing.T) {
	// Use a custom bridge that returns ok on first 2 status calls then errors.
	bridge := &errOnNthStatusBridge{
		seq:        []*CRGStatus{{Nodes: 1, Files: 1}, nil},
		errOnIndex: 1,
		err:        errors.New("status failed"),
	}
	srv := &MCPServer{bridge: bridge}
	_, err := srv.handleBuildOrUpdateGraph(nil)
	if err == nil {
		t.Fatalf("expected status error")
	}
}

// errOnNthStatusBridge returns errOnIndex-th status as an error and the rest
// from seq. All other bridge methods are no-ops.
type errOnNthStatusBridge struct {
	seq        []*CRGStatus
	errOnIndex int
	err        error
	idx        int
}

func (b *errOnNthStatusBridge) Build(opts BuildOptions) error            { return nil }
func (b *errOnNthStatusBridge) Update(opts UpdateOptions) error          { return nil }
func (b *errOnNthStatusBridge) Postprocess(opts PostprocessOptions) error { return nil }
func (b *errOnNthStatusBridge) Status() (*CRGStatus, error) {
	if b.idx == b.errOnIndex {
		b.idx++
		return nil, b.err
	}
	if b.idx >= len(b.seq) {
		return &CRGStatus{}, nil
	}
	s := b.seq[b.idx]
	b.idx++
	return s, nil
}
func (b *errOnNthStatusBridge) GetImpactRadius(opts ImpactOptions) (*CRGImpactResult, error) {
	return &CRGImpactResult{}, nil
}
func (b *errOnNthStatusBridge) ListFlows(limit int, sortBy string) (*FlowsResult, error) {
	return &FlowsResult{}, nil
}
func (b *errOnNthStatusBridge) ListCommunities(minSize int, sortBy string) (*CommunitiesResult, error) {
	return &CommunitiesResult{}, nil
}
func (b *errOnNthStatusBridge) DetectChanges(opts DetectChangesOptions) (*CRGChangeReport, error) {
	return &CRGChangeReport{}, nil
}

// countStatsCommunities — bridge error path

func TestCountStatsCommunities_ListErr(t *testing.T) {
	bridge := &fakeListCommunitiesBridge{err: errors.New("boom")}
	srv := &MCPServer{bridge: bridge}
	if got := srv.countStatsCommunities(); got != 0 {
		t.Fatalf("expected 0 on err, got %d", got)
	}
}

type fakeListCommunitiesBridge struct {
	fakeMCPBridge
	err error
}

// handleEmbedGraph — no-bridge path returns nil error and propagates
// requireBridge's error.
func TestHandleEmbedGraph_NoBridge(t *testing.T) {
	srv := &MCPServer{bridgeErr: errors.New("bridge missing")}
	_, err := srv.handleEmbedGraph(nil)
	if err == nil {
		t.Fatal("expected bridge error")
	}
}

func (f *fakeListCommunitiesBridge) ListCommunities(minSize int, sortBy string) (*CommunitiesResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &CommunitiesResult{}, nil
}
