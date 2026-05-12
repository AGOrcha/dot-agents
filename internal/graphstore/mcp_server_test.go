package graphstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeMCPBridge struct {
	buildCalls  int
	updateCalls int
	postCalls   int
	statusSeq   []*CRGStatus
	statusIdx   int

	buildErr    error
	updateErr   error
	postErr     error
	statusErr   error
	impactErr   error
	impact      *CRGImpactResult
	detectErr   error
	detect      *CRGChangeReport
	communities *CommunitiesResult
}

func (f *fakeMCPBridge) Build(opts BuildOptions) error {
	f.buildCalls++
	return f.buildErr
}

func (f *fakeMCPBridge) Update(opts UpdateOptions) error {
	f.updateCalls++
	return f.updateErr
}

func (f *fakeMCPBridge) Status() (*CRGStatus, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if len(f.statusSeq) == 0 {
		return &CRGStatus{}, nil
	}
	idx := f.statusIdx
	if idx >= len(f.statusSeq) {
		idx = len(f.statusSeq) - 1
	} else {
		f.statusIdx++
	}
	return f.statusSeq[idx], nil
}

func (f *fakeMCPBridge) GetImpactRadius(opts ImpactOptions) (*CRGImpactResult, error) {
	if f.impactErr != nil {
		return nil, f.impactErr
	}
	if f.impact == nil {
		return &CRGImpactResult{}, nil
	}
	return f.impact, nil
}

func (f *fakeMCPBridge) ListFlows(limit int, sortBy string) (*FlowsResult, error) {
	return &FlowsResult{}, nil
}

func (f *fakeMCPBridge) ListCommunities(minSize int, sortBy string) (*CommunitiesResult, error) {
	if f.communities != nil {
		return f.communities, nil
	}
	return &CommunitiesResult{}, nil
}

func (f *fakeMCPBridge) Postprocess(opts PostprocessOptions) error {
	f.postCalls++
	return f.postErr
}

func (f *fakeMCPBridge) DetectChanges(opts DetectChangesOptions) (*CRGChangeReport, error) {
	if f.detectErr != nil {
		return nil, f.detectErr
	}
	if f.detect != nil {
		return f.detect, nil
	}
	return &CRGChangeReport{}, nil
}

func runMCPServeOnce(t *testing.T, srv *MCPServer, req string) rpcResponse {
	t.Helper()
	reader, writer := io.Pipe()
	defer reader.Close()

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(reader, &out)
	}()

	if _, err := io.WriteString(writer, req); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var resp rpcResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v\nraw: %s", err, out.String())
	}
	return resp
}

func TestKGServeToolsList(t *testing.T) {
	srv := &MCPServer{}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal tools/list result: %v", err)
	}
	if err := json.Unmarshal(resultBytes, &payload); err != nil {
		t.Fatalf("unmarshal tools/list payload: %v", err)
	}
	want := []string{
		"build_or_update_graph_tool",
		"embed_graph_tool",
		"list_graph_stats_tool",
		"get_impact_radius_tool",
		"semantic_search_nodes_tool",
		"query_graph_tool",
		"get_review_context_tool",
		"get_docs_section_tool",
	}
	got := map[string]bool{}
	for _, tool := range payload.Tools {
		got[tool.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Fatalf("missing tool %q in list response: %+v", name, payload.Tools)
		}
	}
}

func TestKGServeBuildOrUpdateGraph(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{
			{},
			{Nodes: 12, Edges: 34, Files: 5, Languages: "go, python", LastUpdated: "2026-04-12T00:00:00Z"},
		},
	}
	srv := &MCPServer{bridge: bridge}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"build_or_update_graph_tool","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var payload struct {
		Nodes      int `json:"nodes"`
		Edges      int `json:"edges"`
		Files      int `json:"files"`
		DurationMS int `json:"duration_ms"`
	}
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal build/update result: %v", err)
	}
	if err := json.Unmarshal(resultBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Nodes != 12 || payload.Edges != 34 || payload.Files != 5 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.DurationMS < 0 {
		t.Fatalf("duration must be non-negative: %+v", payload)
	}
	if bridge.buildCalls != 1 || bridge.updateCalls != 0 {
		t.Fatalf("unexpected bridge calls: build=%d update=%d", bridge.buildCalls, bridge.updateCalls)
	}
}

func TestKGServeGetImpactRadius(t *testing.T) {
	bridge := &fakeMCPBridge{
		impact: &CRGImpactResult{
			ChangedNodes: []ImpactNode{
				{Name: "main.run", Kind: "Function", FilePath: "main.go"},
			},
			ImpactedNodes: []ImpactNode{
				{Name: "main.helper", Kind: "Function", FilePath: "helper.go"},
			},
		},
	}
	srv := &MCPServer{bridge: bridge}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_impact_radius_tool","arguments":{"symbol":"main.run","depth":1}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var payload struct {
		Nodes []map[string]any `json:"nodes"`
	}
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal impact result: %v", err)
	}
	if err := json.Unmarshal(resultBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Nodes) == 0 {
		t.Fatalf("expected nodes in impact radius payload: %+v", payload)
	}
}

func TestKGServeUnknownTool(t *testing.T) {
	srv := &MCPServer{}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"does_not_exist","arguments":{}}}`)
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("unexpected error code: %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "method not found") {
		t.Fatalf("unexpected error message: %+v", resp.Error)
	}
}

// runReadinessErrorTest exercises a tool RPC that should produce a
// structured JSON error envelope (not an RPC error) when the graph is
// in the supplied non-ready state. Asserts the payload's state matches.
// requireHint=true also asserts payload["error"] and payload["hint"].
func runReadinessErrorTest(t *testing.T, state, message, toolName, argumentsJSON string, requireHint bool) {
	t.Helper()
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{
			{State: state, Message: message},
		},
	}
	srv := &MCPServer{bridge: bridge}
	requestJSON := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + toolName + `","arguments":` + argumentsJSON + `}}`
	resp := runMCPServeOnce(t, srv, requestJSON)
	if resp.Error != nil {
		t.Fatalf("expected structured result, got RPC error: %+v", resp.Error)
	}
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(resultBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["state"] != state {
		t.Fatalf("expected state=%q, got: %v", state, payload["state"])
	}
	if requireHint {
		if payload["error"] == nil {
			t.Fatalf("expected 'error' field in payload, got: %v", payload)
		}
		if payload["hint"] == nil {
			t.Fatalf("expected 'hint' field in payload, got: %v", payload)
		}
	}
}

// TestHandleGetReviewContext_UnbuiltGraphReturnsError verifies that
// handleGetReviewContext returns a structured JSON error (not a Go/RPC error)
// when the graph is in the unbuilt state.
func TestHandleGetReviewContext_UnbuiltGraphReturnsError(t *testing.T) {
	runReadinessErrorTest(t,
		string(CRGReadinessUnbuilt), "code graph has not been built yet",
		"get_review_context_tool", `{"files":["main.go"]}`,
		true)
}

// TestHandleGetImpactRadius_UnbuiltGraphReturnsError verifies that
// handleGetImpactRadius returns a structured JSON error (not a Go/RPC error)
// when the graph is in the unbuilt state.
func TestHandleGetImpactRadius_UnbuiltGraphReturnsError(t *testing.T) {
	runReadinessErrorTest(t,
		string(CRGReadinessUnbuilt), "code graph has not been built yet",
		"get_impact_radius_tool", `{"symbol":"main.run","depth":2}`,
		true)
}

// TestHandleGetReviewContext_BusyGraphReturnsError verifies the busy_or_locked
// state is handled by handleGetReviewContext.
func TestHandleGetReviewContext_BusyGraphReturnsError(t *testing.T) {
	runReadinessErrorTest(t,
		string(CRGReadinessBusyOrLocked), "database is locked",
		"get_review_context_tool", `{"files":["cmd/main.go"]}`,
		false)
}

// TestKGServeUnknownMethod verifies that unknown JSON-RPC methods produce a
// -32601 method-not-found error.
func TestKGServeUnknownMethod(t *testing.T) {
	srv := &MCPServer{}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"foo/bar","params":{}}`)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("expected -32601, got %+v", resp.Error)
	}
}

// TestKGServeParseError verifies that malformed JSON returns a -32700 parse
// error response.
func TestKGServeParseError(t *testing.T) {
	srv := &MCPServer{}
	reader, writer := io.Pipe()
	defer reader.Close()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- srv.Serve(reader, &out) }()
	_, _ = io.WriteString(writer, "not json{")
	_ = writer.Close()
	<-done

	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var resp rpcResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode parse-error response: %v raw: %s", err, out.String())
	}
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("expected parse error, got %+v", resp.Error)
	}
}

// TestKGServeNotificationNoResponse verifies a JSON-RPC notification (no id)
// produces no response.
func TestKGServeNotificationNoResponse(t *testing.T) {
	srv := &MCPServer{}
	reader, writer := io.Pipe()
	defer reader.Close()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- srv.Serve(reader, &out) }()
	_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","method":"tools/list","params":{}}`)
	_ = writer.Close()
	<-done
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("expected no response for notification, got %q", out.String())
	}
}

// TestKGServeInvalidToolsCallParams verifies a malformed tools/call params
// returns a -32602 invalid params error.
func TestKGServeInvalidToolsCallParams(t *testing.T) {
	srv := &MCPServer{}
	// params is a string instead of a tool-call object
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"oops"}`)
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602, got %+v", resp.Error)
	}
}

// TestKGServeBuildOrUpdateGraph_ChoosesUpdateWhenReady verifies the dispatcher
// calls Update (not Build) when status reports a populated graph.
func TestKGServeBuildOrUpdateGraph_ChoosesUpdateWhenReady(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{
			{Nodes: 1, Files: 1, Ready: true},
			{Nodes: 1, Edges: 0, Files: 1, Ready: true},
		},
	}
	srv := &MCPServer{bridge: bridge}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"build_or_update_graph_tool","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if bridge.updateCalls != 1 || bridge.buildCalls != 0 {
		t.Fatalf("expected update path; build=%d update=%d", bridge.buildCalls, bridge.updateCalls)
	}
}

// TestKGServeBuildOrUpdateGraph_NoBridge verifies the missing-bridge error path.
func TestKGServeBuildOrUpdateGraph_NoBridge(t *testing.T) {
	srv := &MCPServer{bridgeErr: io.EOF}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"build_or_update_graph_tool","arguments":{}}}`)
	if resp.Error == nil {
		t.Fatal("expected error when bridge is unavailable")
	}
}

// TestKGServeEmbedGraph_OK verifies the embed_graph_tool happy path.
func TestKGServeEmbedGraph_OK(t *testing.T) {
	bridge := &fakeMCPBridge{}
	srv := &MCPServer{bridge: bridge}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"embed_graph_tool","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if bridge.postCalls != 1 {
		t.Errorf("expected 1 postprocess call, got %d", bridge.postCalls)
	}
}

// TestKGServeEmbedGraph_Error verifies the embed_graph_tool returns status:error
// when Postprocess fails.
func TestKGServeEmbedGraph_Error(t *testing.T) {
	bridge := &fakeMCPBridge{postErr: io.EOF}
	srv := &MCPServer{bridge: bridge}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"embed_graph_tool","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %+v", resp.Error)
	}
	rb, _ := json.Marshal(resp.Result)
	var p map[string]any
	_ = json.Unmarshal(rb, &p)
	if p["status"] != "error" {
		t.Errorf("expected status=error, got %v", p)
	}
}

// TestKGServeSemanticSearch_MissingQuery verifies the invalid-params error
// path for empty query.
func TestKGServeSemanticSearch_MissingQuery(t *testing.T) {
	srv := &MCPServer{}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"semantic_search_nodes_tool","arguments":{"query":""}}}`)
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602, got %+v", resp.Error)
	}
}

// TestKGServeSemanticSearch_DefaultLimit verifies that limit<=0 falls back to
// the default of 20.
func TestKGServeSemanticSearch_DefaultLimit(t *testing.T) {
	srv := &MCPServer{}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"semantic_search_nodes_tool","arguments":{"query":"foo"}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

// TestKGServeGetImpactRadius_MissingSymbol verifies invalid-params error
// for empty symbol.
func TestKGServeGetImpactRadius_MissingSymbol(t *testing.T) {
	srv := &MCPServer{bridge: &fakeMCPBridge{}}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_impact_radius_tool","arguments":{"symbol":""}}}`)
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602 for empty symbol, got %+v", resp.Error)
	}
}

// TestKGServeGetImpactRadius_DepthDefault verifies depth<=0 falls back to 2.
func TestKGServeGetImpactRadius_DepthDefault(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{{State: string(CRGReadinessReady), Ready: true}},
		impact:    &CRGImpactResult{ChangedNodes: []ImpactNode{{Name: "x"}}},
	}
	srv := &MCPServer{bridge: bridge}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_impact_radius_tool","arguments":{"symbol":"x"}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

// TestKGServeGetReviewContext_MissingFiles verifies invalid-params error
// for empty files list.
func TestKGServeGetReviewContext_MissingFiles(t *testing.T) {
	srv := &MCPServer{bridge: &fakeMCPBridge{}}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_review_context_tool","arguments":{"files":[]}}}`)
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602, got %+v", resp.Error)
	}
}

// TestKGServeGetDocsSection_MissingSection verifies invalid-params error
// for empty section.
func TestKGServeGetDocsSection_MissingSection(t *testing.T) {
	srv := &MCPServer{}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_docs_section_tool","arguments":{"section":""}}}`)
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602, got %+v", resp.Error)
	}
}

// TestKGServeGetDocsSection_NotFound verifies empty payload when section is
// not in any candidate doc.
func TestKGServeGetDocsSection_NotFound(t *testing.T) {
	srv := &MCPServer{workDir: t.TempDir()}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_docs_section_tool","arguments":{"section":"nonexistent"}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	rb, _ := json.Marshal(resp.Result)
	var p map[string]any
	_ = json.Unmarshal(rb, &p)
	if p["content"] != "" || p["source"] != "" {
		t.Errorf("expected empty content/source, got %v", p)
	}
}

// TestKGServeGetDocsSection_Found verifies the section is extracted from a
// markdown file under <workDir>/docs.
func TestKGServeGetDocsSection_Found(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "docs")
	_ = os.MkdirAll(docsDir, 0o755)
	mdPath := filepath.Join(docsDir, "KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md")
	_ = os.WriteFile(mdPath, []byte("# Top\n\n## Target Section\n\nContent here.\n\n## Next\n"), 0o644)

	srv := &MCPServer{workDir: dir}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_docs_section_tool","arguments":{"section":"Target Section"}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	rb, _ := json.Marshal(resp.Result)
	var p map[string]any
	_ = json.Unmarshal(rb, &p)
	if !strings.Contains(p["content"].(string), "Target Section") {
		t.Errorf("expected section to contain heading, got %v", p)
	}
}

// TestKGServeQueryGraph_SemanticIntent routes to semantic search.
func TestKGServeQueryGraph_SemanticIntent(t *testing.T) {
	srv := &MCPServer{}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query_graph_tool","arguments":{"intent":"semantic_search","query":"foo"}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

// TestKGServeQueryGraph_UnknownIntent yields warnings and an empty result.
func TestKGServeQueryGraph_UnknownIntent(t *testing.T) {
	srv := &MCPServer{}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query_graph_tool","arguments":{"intent":"banana","query":"x"}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	rb, _ := json.Marshal(resp.Result)
	var p map[string]any
	_ = json.Unmarshal(rb, &p)
	if p["warnings"] == nil {
		t.Errorf("expected warnings field for unsupported intent, got %v", p)
	}
}

// TestKGServeQueryGraph_DocsIntent routes to get_docs_section_tool.
func TestKGServeQueryGraph_DocsIntent(t *testing.T) {
	srv := &MCPServer{workDir: t.TempDir()}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query_graph_tool","arguments":{"intent":"docs_section","query":"foo"}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

// TestKGServeListGraphStats_NoStore returns an error when the store is absent.
func TestKGServeListGraphStats_NoStore(t *testing.T) {
	srv := &MCPServer{storeErr: io.EOF}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_graph_stats_tool","arguments":{}}}`)
	if resp.Error == nil {
		t.Fatal("expected error when store is unavailable")
	}
}

// TestBuildRPCResponse_TypedError preserves typed *rpcError on the response.
func TestBuildRPCResponse_TypedError(t *testing.T) {
	id := json.RawMessage(`1`)
	re := &rpcError{Code: -32603, Message: "boom"}
	resp := buildRPCResponse(id, nil, re)
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Errorf("expected typed error preserved, got %+v", resp.Error)
	}
}

// TestBuildRPCResponse_PlainError wraps non-rpcError as code -32603.
func TestBuildRPCResponse_PlainError(t *testing.T) {
	id := json.RawMessage(`1`)
	resp := buildRPCResponse(id, nil, io.EOF)
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Errorf("expected -32603 for non-rpc error, got %+v", resp.Error)
	}
}

// TestBuildRPCResponse_OK populates result on success.
func TestBuildRPCResponse_OK(t *testing.T) {
	id := json.RawMessage(`1`)
	resp := buildRPCResponse(id, json.RawMessage(`{"ok":true}`), nil)
	if resp.Error != nil {
		t.Errorf("expected nil error, got %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Errorf("expected non-nil result")
	}
}

// TestRPCError_Error covers the *rpcError.Error stringer (nil and populated).
func TestRPCError_Error(t *testing.T) {
	var nilErr *rpcError
	if nilErr.Error() != "" {
		t.Errorf("nil rpcError.Error must be empty")
	}
	e := &rpcError{Message: "boom"}
	if e.Error() != "boom" {
		t.Errorf("got %q", e.Error())
	}
}

// TestDedupImpactNodes_RemovesDuplicates verifies dedup by qualified name (or
// fallback name) keeps the first occurrence only.
func TestDedupImpactNodes_RemovesDuplicates(t *testing.T) {
	changed := []ImpactNode{{QualifiedName: "a", Name: "a"}, {QualifiedName: "b", Name: "b"}}
	impacted := []ImpactNode{{QualifiedName: "b", Name: "b"}, {QualifiedName: "c", Name: "c"}}
	nodes := dedupImpactNodes(changed, impacted)
	if len(nodes) != 3 {
		t.Errorf("expected 3 unique nodes, got %d", len(nodes))
	}
}

// TestDedupImpactNodes_FallbackToName uses Name when QualifiedName is empty.
func TestDedupImpactNodes_FallbackToName(t *testing.T) {
	changed := []ImpactNode{{Name: "a"}}
	impacted := []ImpactNode{{Name: "a"}, {Name: "b"}}
	nodes := dedupImpactNodes(changed, impacted)
	if len(nodes) != 2 {
		t.Errorf("expected 2 unique nodes (a from changed, b from impacted), got %d", len(nodes))
	}
}

// TestImpactNodeToMCP and TestGraphNodeToMCP cover the small projectors.
func TestImpactNodeToMCP(t *testing.T) {
	got := impactNodeToMCP(ImpactNode{Name: "n", Kind: "Function", FilePath: "f.go"})
	if got["name"] != "n" || got["type"] != "Function" || got["file"] != "f.go" {
		t.Errorf("unexpected projection: %v", got)
	}
}

func TestGraphNodeToMCP(t *testing.T) {
	got := graphNodeToMCP(GraphNode{Name: "n", Kind: "Class", FilePath: "f.go"})
	if got["type"] != "Class" {
		t.Errorf("unexpected projection: %v", got)
	}
}

// TestParseHeading covers heading-detection edge cases.
func TestParseHeading(t *testing.T) {
	cases := []struct {
		line    string
		level   int
		heading string
		ok      bool
	}{
		{"# Top", 1, "Top", true},
		{"## Section", 2, "Section", true},
		{"### Sub", 3, "Sub", true},
		{"not a heading", 0, "", false},
		{"#nospace", 0, "", false},
		{"#", 0, "", false},
		{"#### Deep heading  ", 4, "Deep heading", true},
	}
	for _, c := range cases {
		level, heading, ok := parseHeading(c.line)
		if ok != c.ok || level != c.level || heading != c.heading {
			t.Errorf("parseHeading(%q) = (%d,%q,%v), want (%d,%q,%v)",
				c.line, level, heading, ok, c.level, c.heading, c.ok)
		}
	}
}

// TestNormalizeHeading covers casefold + underscore→space + whitespace collapse.
func TestNormalizeHeading(t *testing.T) {
	if got := normalizeHeading("  Foo_Bar   Baz "); got != "foo bar baz" {
		t.Errorf("got %q", got)
	}
}

// TestExtractMarkdownSection_TopLevel finds top-level section and trims.
func TestExtractMarkdownSection_TopLevel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "doc.md")
	_ = os.WriteFile(p, []byte("# A\nhello\n\n# B\nworld\n"), 0o644)
	got, ok := extractMarkdownSection(p, "B")
	if !ok || !strings.Contains(got, "world") {
		t.Errorf("got=%q ok=%v", got, ok)
	}
}

// TestExtractMarkdownSection_NotFound returns false when heading absent.
func TestExtractMarkdownSection_NotFound(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "doc.md")
	_ = os.WriteFile(p, []byte("# A\nhello\n"), 0o644)
	_, ok := extractMarkdownSection(p, "missing")
	if ok {
		t.Error("expected not found")
	}
}

// TestExtractMarkdownSection_FileMissing returns false when file does not exist.
func TestExtractMarkdownSection_FileMissing(t *testing.T) {
	_, ok := extractMarkdownSection("/no/such/file.md", "x")
	if ok {
		t.Error("expected false when file missing")
	}
}

// TestMustMarshal covers the tiny encoder helper.
func TestMustMarshal(t *testing.T) {
	out := mustMarshal(map[string]any{"a": 1})
	if !bytes.Contains(out, []byte("\"a\"")) {
		t.Errorf("unexpected: %s", out)
	}
}

// TestDefaultKGHome and TestDefaultGraphstoreDBPath cover the path helpers.
func TestDefaultKGHome_EnvOverride(t *testing.T) {
	t.Setenv("KG_HOME", "/tmp/kg-test-home")
	if defaultKGHome() != "/tmp/kg-test-home" {
		t.Errorf("expected env override to win")
	}
}

func TestDefaultGraphstoreDBPath(t *testing.T) {
	t.Setenv("KG_HOME", "/tmp/kg-test-home")
	p := defaultGraphstoreDBPath()
	if !strings.HasSuffix(p, filepath.Join("ops", "graphstore.db")) {
		t.Errorf("unexpected path: %s", p)
	}
}

// TestNewMCPServer constructs and verifies fields are populated (or errors
// recorded) without panicking.
func TestNewMCPServer(t *testing.T) {
	srv := NewMCPServer(t.TempDir())
	if srv == nil {
		t.Fatal("nil server")
	}
	// bridge may be nil if CRG not on PATH — that's fine; bridgeErr should
	// then be non-nil.
	if srv.bridge == nil && srv.bridgeErr == nil {
		t.Error("either bridge or bridgeErr must be set")
	}
}

// TestGraphReadinessGuardJSON covers each readiness branch.
func TestGraphReadinessGuardJSON_Unbuilt(t *testing.T) {
	bridge := &fakeMCPBridge{statusSeq: []*CRGStatus{{State: string(CRGReadinessUnbuilt)}}}
	out, err := graphReadinessGuardJSON(bridge)
	if err != nil || out == nil {
		t.Errorf("unbuilt: got out=%s err=%v", out, err)
	}
}

func TestGraphReadinessGuardJSON_BusyLocked(t *testing.T) {
	bridge := &fakeMCPBridge{statusSeq: []*CRGStatus{{State: string(CRGReadinessBusyOrLocked)}}}
	out, err := graphReadinessGuardJSON(bridge)
	if err != nil || out == nil {
		t.Errorf("busy: got out=%s err=%v", out, err)
	}
}

func TestGraphReadinessGuardJSON_Ready(t *testing.T) {
	bridge := &fakeMCPBridge{statusSeq: []*CRGStatus{{State: string(CRGReadinessReady), Ready: true}}}
	out, err := graphReadinessGuardJSON(bridge)
	if err != nil || out != nil {
		t.Errorf("ready: should pass through, got out=%s err=%v", out, err)
	}
}

func TestGraphReadinessGuardJSON_StatusErr(t *testing.T) {
	bridge := &fakeMCPBridge{statusErr: io.EOF}
	out, err := graphReadinessGuardJSON(bridge)
	if err != nil || out != nil {
		t.Errorf("status error: should pass through, got out=%s err=%v", out, err)
	}
}

// TestCollectStatsLanguages_BridgeFallback covers the bridge-fallback path
// when the warm store has no languages.
func TestCollectStatsLanguages_BridgeFallback(t *testing.T) {
	bridge := &fakeMCPBridge{statusSeq: []*CRGStatus{{Languages: "go, python"}}}
	srv := &MCPServer{bridge: bridge}
	got := srv.collectStatsLanguages(GraphStats{})
	if got["go"] != 1 || got["python"] != 1 {
		t.Errorf("expected go+python from bridge, got %v", got)
	}
}

// TestCollectStatsLanguages_StoreWins prefers stats.Languages.
func TestCollectStatsLanguages_StoreWins(t *testing.T) {
	srv := &MCPServer{}
	got := srv.collectStatsLanguages(GraphStats{Languages: []string{"go", "rust"}})
	if got["go"] != 1 || got["rust"] != 1 {
		t.Errorf("expected go+rust, got %v", got)
	}
}

// TestCollectStatsLanguages_BridgeStatusError yields empty map.
func TestCollectStatsLanguages_BridgeStatusError(t *testing.T) {
	bridge := &fakeMCPBridge{statusErr: io.EOF}
	srv := &MCPServer{bridge: bridge}
	got := srv.collectStatsLanguages(GraphStats{})
	if len(got) != 0 {
		t.Errorf("expected empty map on bridge status error, got %v", got)
	}
}

// TestCollectStatsLanguages_NoBridge yields empty map.
func TestCollectStatsLanguages_NoBridge(t *testing.T) {
	srv := &MCPServer{}
	got := srv.collectStatsLanguages(GraphStats{})
	if len(got) != 0 {
		t.Errorf("expected empty map without bridge, got %v", got)
	}
}

// TestCountStatsCommunities_NoBridge returns 0.
func TestCountStatsCommunities_NoBridge(t *testing.T) {
	srv := &MCPServer{}
	if c := srv.countStatsCommunities(); c != 0 {
		t.Errorf("got %d", c)
	}
}

// TestCountStatsCommunities_WithBridge returns the slice length.
func TestCountStatsCommunities_WithBridge(t *testing.T) {
	bridge := &fakeMCPBridge{communities: &CommunitiesResult{Communities: []CommunityInfo{{Name: "x"}, {Name: "y"}}}}
	srv := &MCPServer{bridge: bridge}
	if c := srv.countStatsCommunities(); c != 2 {
		t.Errorf("got %d", c)
	}
}

// TestReviewChangedSymbols projects CRGChangedNode list into MCP payloads.
func TestReviewChangedSymbols(t *testing.T) {
	out := reviewChangedSymbols([]CRGChangedNode{{QualifiedName: "pkg.f", FilePath: "f.go", RiskScore: 0.5}})
	if len(out) != 1 || out[0]["name"] != "pkg.f" {
		t.Errorf("unexpected: %v", out)
	}
}

// TestReviewImpactNodes_NoStore returns an empty slice when no store.
func TestReviewImpactNodes_NoStore(t *testing.T) {
	srv := &MCPServer{}
	out := srv.reviewImpactNodes([]string{"a.go"})
	if len(out) != 0 {
		t.Errorf("expected empty, got %v", out)
	}
}

// TestRequireBridge_OK returns the bridge.
func TestRequireBridge_OK(t *testing.T) {
	bridge := &fakeMCPBridge{}
	srv := &MCPServer{bridge: bridge}
	got, err := srv.requireBridge()
	if err != nil || got == nil {
		t.Errorf("got err=%v", err)
	}
}

// TestRequireBridge_StoredErr returns the stored bridgeErr.
func TestRequireBridge_StoredErr(t *testing.T) {
	srv := &MCPServer{bridgeErr: io.EOF}
	if _, err := srv.requireBridge(); err == nil {
		t.Error("expected error")
	}
}

// TestRequireBridge_NoBridgeNoErr returns a synthetic error.
func TestRequireBridge_NoBridgeNoErr(t *testing.T) {
	srv := &MCPServer{}
	if _, err := srv.requireBridge(); err == nil {
		t.Error("expected error")
	}
}

// ── CRG internal helpers ─────────────────────────────────────────────────────

func TestParseCRGMutationSummary_typical(t *testing.T) {
	out := []byte("3 files updated, 12 nodes changed, 7 edges changed\n")
	files, nodes, edges, ok := parseCRGMutationSummary(out)
	if !ok || files != 3 || nodes != 12 || edges != 7 {
		t.Errorf("got files=%d nodes=%d edges=%d ok=%v", files, nodes, edges, ok)
	}
}

func TestParseCRGMutationSummary_skipsInfoLines(t *testing.T) {
	out := []byte("INFO: starting\n5 files, 10 nodes, 2 edges\n")
	files, nodes, edges, ok := parseCRGMutationSummary(out)
	if !ok || files != 5 || nodes != 10 || edges != 2 {
		t.Errorf("got files=%d nodes=%d edges=%d ok=%v", files, nodes, edges, ok)
	}
}

func TestParseCRGMutationSummary_noMatch(t *testing.T) {
	out := []byte("nothing here\n")
	_, _, _, ok := parseCRGMutationSummary(out)
	if ok {
		t.Error("expected no match")
	}
}

func TestIsCRGBusyLockedError(t *testing.T) {
	if !isCRGBusyLockedError(errFmt("database is locked")) {
		t.Error("should detect 'database is locked'")
	}
	if !isCRGBusyLockedError(errFmt("server busy")) {
		t.Error("should detect 'busy'")
	}
	if isCRGBusyLockedError(nil) {
		t.Error("nil err should be false")
	}
	if isCRGBusyLockedError(errFmt("other error")) {
		t.Error("non-matching err should be false")
	}
}

func TestIsCRGUnbuiltError(t *testing.T) {
	if !isCRGUnbuiltError(errFmt("no such table: nodes")) {
		t.Error("should detect 'no such table'")
	}
	if !isCRGUnbuiltError(errFmt("missing schema")) {
		t.Error("should detect 'missing'")
	}
	if isCRGUnbuiltError(nil) {
		t.Error("nil err should be false")
	}
	if isCRGUnbuiltError(errFmt("other")) {
		t.Error("non-matching err should be false")
	}
}

func TestClassifyCRGRunError_BusyLocked(t *testing.T) {
	got := classifyCRGRunError("build", errFmt("database is locked"), nil)
	if !strings.Contains(got.Error(), "busy or locked") {
		t.Errorf("got %v", got)
	}
}

func TestClassifyCRGRunError_FallbackToOutput(t *testing.T) {
	got := classifyCRGRunError("build", errFmt("boom"), []byte("the real output"))
	if !strings.Contains(got.Error(), "the real output") {
		t.Errorf("got %v", got)
	}
}

func TestClassifyCRGRunError_EmptyOutput(t *testing.T) {
	got := classifyCRGRunError("update", errFmt("boom"), nil)
	if !strings.Contains(got.Error(), "boom") {
		t.Errorf("got %v", got)
	}
}

func TestNormalizeCRGUpdatedAt(t *testing.T) {
	if got := normalizeCRGUpdatedAt(""); got != "never" {
		t.Errorf("empty: got %q", got)
	}
	if got := normalizeCRGUpdatedAt("2026-04-11T00:49:52"); got != "2026-04-11T00:49:52" {
		t.Errorf("rfc3339-ish passthrough: got %q", got)
	}
	if got := normalizeCRGUpdatedAt("1712797792.5"); !strings.Contains(got, "T") {
		t.Errorf("numeric should normalize, got %q", got)
	}
	if got := normalizeCRGUpdatedAt("garbage"); got != "garbage" {
		t.Errorf("garbage passthrough: got %q", got)
	}
}

func TestApplyCRGStatusError_BusyLocked(t *testing.T) {
	st := &CRGStatus{}
	applyCRGStatusError(st, errFmt("database is locked"))
	if st.State != string(CRGReadinessBusyOrLocked) {
		t.Errorf("got %q", st.State)
	}
}

func TestApplyCRGStatusError_Unbuilt(t *testing.T) {
	st := &CRGStatus{State: string(CRGReadinessUnbuilt)}
	applyCRGStatusError(st, errFmt("no such table: foo"))
	if st.State != string(CRGReadinessUnbuilt) {
		t.Errorf("got %q", st.State)
	}
}

func TestApplyCRGStatusError_Generic(t *testing.T) {
	st := &CRGStatus{}
	applyCRGStatusError(st, errFmt("totally unexpected"))
	if st.State != string(CRGReadinessError) {
		t.Errorf("got %q", st.State)
	}
}

func TestIsPythonEntrypoint_MissingFile(t *testing.T) {
	if isPythonEntrypoint("/no/such/file") {
		t.Error("missing file should return false")
	}
}

func TestIsPythonEntrypoint_Shebang(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bin")
	_ = os.WriteFile(p, []byte("#!/usr/bin/env python3\nprint('hi')\n"), 0o755)
	if !isPythonEntrypoint(p) {
		t.Error("python shebang should return true")
	}
}

func TestIsPythonEntrypoint_NoShebang(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bin")
	_ = os.WriteFile(p, []byte("not a script\n"), 0o644)
	if isPythonEntrypoint(p) {
		t.Error("expected false")
	}
}

func TestUnmarshalSkippingLogPrefix_DirectJSON(t *testing.T) {
	var v map[string]any
	if err := unmarshalSkippingLogPrefix([]byte(`{"a":1}`), &v); err != nil {
		t.Fatalf("err: %v", err)
	}
	if v["a"] == nil {
		t.Errorf("unexpected: %v", v)
	}
}

func TestUnmarshalSkippingLogPrefix_WithLogLines(t *testing.T) {
	raw := []byte("INFO: something\n{\"a\":1}\n")
	var v map[string]any
	if err := unmarshalSkippingLogPrefix(raw, &v); err != nil {
		t.Fatalf("err: %v", err)
	}
	if v["a"] == nil {
		t.Errorf("got %v", v)
	}
}

func TestSplitPGStatements(t *testing.T) {
	got := splitPGStatements("SELECT 1; SELECT 2;; SELECT 3;")
	if len(got) != 3 {
		t.Errorf("expected 3 statements, got %d: %v", len(got), got)
	}
}

func TestPGEncodeExtra_Empty(t *testing.T) {
	got, err := pgEncodeExtra(nil)
	if err != nil || got != "{}" {
		t.Errorf("nil: got %q err=%v", got, err)
	}
}

func TestPGEncodeExtra_Populated(t *testing.T) {
	got, err := pgEncodeExtra(map[string]any{"k": "v"})
	if err != nil || !strings.Contains(got, "\"k\"") {
		t.Errorf("populated: got %q err=%v", got, err)
	}
}

func TestEncodeExtra_Empty(t *testing.T) {
	got, err := encodeExtra(nil)
	if err != nil || got != "{}" {
		t.Errorf("nil: got %q err=%v", got, err)
	}
}

func TestEncodeExtra_Populated(t *testing.T) {
	got, err := encodeExtra(map[string]any{"x": 1})
	if err != nil || !strings.Contains(got, "\"x\"") {
		t.Errorf("populated: got %q err=%v", got, err)
	}
}

func TestDecodeExtra_Empty(t *testing.T) {
	if m := decodeExtra(""); m != nil {
		t.Errorf("empty: got %v", m)
	}
	if m := decodeExtra("{}"); m != nil {
		t.Errorf("empty object: got %v", m)
	}
}

func TestDecodeExtra_Populated(t *testing.T) {
	if m := decodeExtra(`{"k":"v"}`); m["k"] != "v" {
		t.Errorf("got %v", m)
	}
}

func TestMakeQualified_WithParent(t *testing.T) {
	got := makeQualified(NodeInfo{Name: "Bar", ParentName: "Foo"})
	if got != "Foo.Bar" {
		t.Errorf("got %q", got)
	}
}

func TestMakeQualified_FilePath(t *testing.T) {
	got := makeQualified(NodeInfo{Name: "Bar", FilePath: "f.go"})
	if got != "f.go::Bar" {
		t.Errorf("got %q", got)
	}
}

// ── impact internal helpers ──────────────────────────────────────────────────

// fakeImpactStore satisfies impactStoreView for computeImpactRadius tests.
type fakeImpactStore struct {
	nodes map[string]*GraphNode
	edges []GraphEdge
}

func (f *fakeImpactStore) GetNode(qn string) (*GraphNode, error) {
	return f.nodes[qn], nil
}
func (f *fakeImpactStore) GetEdgesAmong(qns []string) ([]GraphEdge, error) {
	return f.edges, nil
}

func TestComputeImpactRadius_AssemblesResult(t *testing.T) {
	store := &fakeImpactStore{
		nodes: map[string]*GraphNode{
			"A": {QualifiedName: "A", FilePath: "a.go"},
			"B": {QualifiedName: "B", FilePath: "b.go"},
		},
		edges: []GraphEdge{{SourceQualified: "A", TargetQualified: "B"}},
	}
	seeds := map[string]bool{"A": true}
	fwd := map[string][]string{"A": {"B"}}
	rev := map[string][]string{"B": {"A"}}
	got, err := computeImpactRadius(seeds, fwd, rev, 2, 100, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ChangedNodes) != 1 || got.ChangedNodes[0].QualifiedName != "A" {
		t.Errorf("changed: %v", got.ChangedNodes)
	}
	if len(got.ImpactedNodes) != 1 || got.ImpactedNodes[0].QualifiedName != "B" {
		t.Errorf("impacted: %v", got.ImpactedNodes)
	}
	if len(got.ImpactedFiles) != 1 || got.ImpactedFiles[0] != "b.go" {
		t.Errorf("files: %v", got.ImpactedFiles)
	}
}

func TestResolveImpactNodes_ExcludeSet(t *testing.T) {
	store := &fakeImpactStore{nodes: map[string]*GraphNode{
		"A": {QualifiedName: "A", FilePath: "a.go"},
		"B": {QualifiedName: "B", FilePath: "b.go"},
	}}
	got := resolveImpactNodes(map[string]bool{"A": true, "B": true}, map[string]bool{"A": true}, store)
	if len(got) != 1 || got[0].QualifiedName != "B" {
		t.Errorf("got %v", got)
	}
}

func TestResolveImpactNodes_SkipsMissing(t *testing.T) {
	store := &fakeImpactStore{nodes: map[string]*GraphNode{}}
	got := resolveImpactNodes(map[string]bool{"missing": true}, nil, store)
	if len(got) != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

func TestUniqueImpactFiles_Dedup(t *testing.T) {
	got := uniqueImpactFiles([]GraphNode{
		{FilePath: "a.go"}, {FilePath: "b.go"}, {FilePath: "a.go"},
	})
	if len(got) != 2 {
		t.Errorf("expected 2, got %v", got)
	}
}

func TestAppendUnvisited_SkipsVisited(t *testing.T) {
	visited := map[string]bool{"a": true}
	impacted := map[string]bool{}
	got := appendUnvisited(nil, []string{"a", "b", "c"}, visited, impacted)
	if len(got) != 2 {
		t.Errorf("got %v", got)
	}
	if !impacted["b"] || !impacted["c"] || impacted["a"] {
		t.Errorf("impacted: %v", impacted)
	}
}

// errFmt is a tiny helper to construct an error from a literal string.
func errFmt(msg string) error {
	return fmt.Errorf("%s", msg)
}

// TestHandleGetReviewContext_ReadyGraphProceedsNormally verifies that when the
// graph is ready, handleGetReviewContext proceeds to call DetectChanges.
func TestHandleGetReviewContext_ReadyGraphProceedsNormally(t *testing.T) {
	bridge := &fakeMCPBridge{
		statusSeq: []*CRGStatus{
			{State: string(CRGReadinessReady), Ready: true, Nodes: 100, Edges: 200, Files: 10},
		},
		detect: &CRGChangeReport{
			Summary:          "1 changed function",
			ChangedFunctions: []CRGChangedNode{{Name: "foo", QualifiedName: "pkg.foo", FilePath: "pkg/foo.go"}},
		},
	}
	srv := &MCPServer{bridge: bridge}
	resp := runMCPServeOnce(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_review_context_tool","arguments":{"files":["pkg/foo.go"]}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %+v", resp.Error)
	}
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(resultBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["error"] != nil {
		t.Fatalf("unexpected error field in ready-graph response: %v", payload)
	}
	if payload["changed_symbols"] == nil {
		t.Fatalf("expected 'changed_symbols' in ready-graph response, got: %v", payload)
	}
}
