package graphstore

import (
	"bytes"
	"encoding/json"
	"io"
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
