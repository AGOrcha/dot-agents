package graphstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type mcpBridge interface {
	Build(opts BuildOptions) error
	Update(opts UpdateOptions) error
	Status() (*CRGStatus, error)
	GetImpactRadius(opts ImpactOptions) (*CRGImpactResult, error)
	ListFlows(limit int, sortBy string) (*FlowsResult, error)
	ListCommunities(minSize int, sortBy string) (*CommunitiesResult, error)
	Postprocess(opts PostprocessOptions) error
	DetectChanges(opts DetectChangesOptions) (*CRGChangeReport, error)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type toolDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

type mcpToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type MCPServer struct {
	bridge    mcpBridge
	store     Store
	bridgeErr error
	storeErr  error
	workDir   string
}

func NewMCPServer(workDir string) *MCPServer {
	s := &MCPServer{workDir: workDir}
	if bridge, err := NewCRGBridge(workDir); err == nil {
		s.bridge = bridge
	} else {
		s.bridgeErr = err
	}
	if store, err := OpenSQLite(defaultGraphstoreDBPath()); err == nil {
		s.store = store
	} else {
		s.storeErr = err
	}
	return s
}

func defaultKGHome() string {
	if v := os.Getenv("KG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "knowledge-graph")
}

func defaultGraphstoreDBPath() string {
	return filepath.Join(defaultKGHome(), "ops", "graphstore.db")
}

func (s *MCPServer) Serve(r io.Reader, w io.Writer) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	for {
		var req rpcRequest
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error: &rpcError{
					Code:    -32700,
					Message: "parse error",
					Data:    err.Error(),
				},
			})
			return err
		}

		result, rpcErr := s.dispatch(req.Method, req.ID, req.Params)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			if re, ok := rpcErr.(*rpcError); ok {
				resp.Error = re
			} else {
				resp.Error = &rpcError{Code: -32603, Message: rpcErr.Error()}
			}
		} else {
			resp.Result = json.RawMessage(result)
		}
		if len(req.ID) == 0 {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}

func (s *MCPServer) dispatch(method string, id json.RawMessage, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "tools/list":
		out, err := s.handleToolsList(params)
		if err != nil {
			return nil, err
		}
		return out, nil
	case "tools/call":
		var call mcpToolCall
		if len(params) > 0 {
			if err := json.Unmarshal(params, &call); err != nil {
				return nil, &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
			}
		}
		switch call.Name {
		case "build_or_update_graph_tool":
			return s.handleBuildOrUpdateGraph(call.Arguments)
		case "embed_graph_tool":
			return s.handleEmbedGraph(call.Arguments)
		case "list_graph_stats_tool":
			return s.handleListGraphStats(call.Arguments)
		case "get_impact_radius_tool":
			return s.handleGetImpactRadius(call.Arguments)
		case "semantic_search_nodes_tool":
			return s.handleSemanticSearchNodes(call.Arguments)
		case "query_graph_tool":
			return s.handleQueryGraph(call.Arguments)
		case "get_review_context_tool":
			return s.handleGetReviewContext(call.Arguments)
		case "get_docs_section_tool":
			return s.handleGetDocsSection(call.Arguments)
		default:
			return nil, &rpcError{Code: -32601, Message: "method not found", Data: fmt.Sprintf("unknown tool %q", call.Name)}
		}
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found", Data: method}
	}
}

func (s *MCPServer) handleToolsList(_ json.RawMessage) (json.RawMessage, error) {
	tools := []toolDescriptor{
		{
			Name:        "build_or_update_graph_tool",
			Description: "Build or update the code graph for the current repository.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "embed_graph_tool",
			Description: "Run graph post-processing for downstream queries.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "list_graph_stats_tool",
			Description: "Return code graph statistics.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "get_impact_radius_tool",
			Description: "Return the impact radius for a symbol.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"symbol": map[string]any{"type": "string"},
					"depth":  map[string]any{"type": "integer"},
				},
			},
		},
		{
			Name:        "semantic_search_nodes_tool",
			Description: "Search the graph for matching code symbols.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer"},
				},
			},
		},
		{
			Name:        "query_graph_tool",
			Description: "Run a higher-level graph query by intent.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent": map[string]any{"type": "string"},
					"query":  map[string]any{"type": "string"},
					"scope":  map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "get_review_context_tool",
			Description: "Summarize changed symbols and impact radius for a file set.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"files": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
			},
		},
		{
			Name:        "get_docs_section_tool",
			Description: "Return a documentation section by heading.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"section": map[string]any{"type": "string"},
				},
			},
		},
	}
	payload := map[string]any{"tools": tools}
	return json.Marshal(payload)
}

func (s *MCPServer) handleBuildOrUpdateGraph(_ json.RawMessage) (json.RawMessage, error) {
	bridge, err := s.requireBridge()
	if err != nil {
		return nil, err
	}
	start := time.Now()
	status, statErr := bridge.Status()
	if statErr != nil || status == nil || (status.Nodes == 0 && status.Files == 0) {
		if err := bridge.Build(BuildOptions{}); err != nil {
			return nil, err
		}
	} else {
		if err := bridge.Update(UpdateOptions{}); err != nil {
			return nil, err
		}
	}
	status, err = bridge.Status()
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"nodes":       status.Nodes,
		"edges":       status.Edges,
		"files":       status.Files,
		"duration_ms": time.Since(start).Milliseconds(),
	}
	return json.Marshal(payload)
}

func (s *MCPServer) handleEmbedGraph(_ json.RawMessage) (json.RawMessage, error) {
	bridge, err := s.requireBridge()
	if err != nil {
		return nil, err
	}
	if err := bridge.Postprocess(PostprocessOptions{}); err != nil {
		return json.Marshal(map[string]any{"status": "error", "message": err.Error()})
	}
	return json.Marshal(map[string]any{"status": "ok", "message": "graph post-processing complete"})
}

func (s *MCPServer) handleListGraphStats(_ json.RawMessage) (json.RawMessage, error) {
	stats, err := s.loadStats()
	if err != nil {
		return nil, err
	}
	communities := 0
	if s.bridge != nil {
		if result, err := s.bridge.ListCommunities(0, "size"); err == nil && result != nil {
			communities = len(result.Communities)
		}
	}
	languages := map[string]int{}
	if len(stats.Languages) > 0 {
		for _, lang := range stats.Languages {
			languages[lang]++
		}
	} else if s.bridge != nil {
		if status, err := s.bridge.Status(); err == nil && status != nil {
			for _, lang := range strings.Split(status.Languages, ",") {
				lang = strings.TrimSpace(lang)
				if lang != "" {
					languages[lang]++
				}
			}
		}
	}
	if len(languages) == 0 {
		languages = map[string]int{}
	}
	payload := map[string]any{
		"nodes":       stats.TotalNodes,
		"edges":       stats.TotalEdges,
		"languages":   languages,
		"communities": communities,
	}
	return json.Marshal(payload)
}

func (s *MCPServer) handleGetImpactRadius(params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Symbol string `json:"symbol"`
		Depth  int    `json:"depth"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
	}
	if strings.TrimSpace(req.Symbol) == "" {
		return nil, &rpcError{Code: -32602, Message: "invalid params", Data: "symbol is required"}
	}
	depth := req.Depth
	if depth <= 0 {
		depth = 2
	}
	files := []string{req.Symbol}
	if s.store != nil {
		if nodes, err := s.store.SearchNodes(req.Symbol, 20); err == nil {
			var dedup []string
			seen := map[string]bool{}
			for _, node := range nodes {
				if node.FilePath == "" || seen[node.FilePath] {
					continue
				}
				seen[node.FilePath] = true
				dedup = append(dedup, node.FilePath)
			}
			if len(dedup) > 0 {
				files = dedup
			}
		}
	}
	bridge, err := s.requireBridge()
	if err != nil {
		return nil, err
	}
	result, err := bridge.GetImpactRadius(ImpactOptions{
		ChangedFiles: files,
		MaxDepth:     depth,
	})
	if err != nil {
		return nil, err
	}
	nodes := make([]map[string]any, 0, len(result.ChangedNodes)+len(result.ImpactedNodes))
	seen := map[string]bool{}
	for _, node := range result.ChangedNodes {
		key := node.QualifiedName
		if key == "" {
			key = node.Name
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		nodes = append(nodes, impactNodeToMCP(node))
	}
	for _, node := range result.ImpactedNodes {
		key := node.QualifiedName
		if key == "" {
			key = node.Name
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		nodes = append(nodes, impactNodeToMCP(node))
	}
	return json.Marshal(map[string]any{"nodes": nodes})
}

func (s *MCPServer) handleSemanticSearchNodes(params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, &rpcError{Code: -32602, Message: "invalid params", Data: "query is required"}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	results := []map[string]any{}
	if s.store != nil {
		nodes, err := s.store.SearchNodes(req.Query, limit)
		if err == nil {
			for _, node := range nodes {
				results = append(results, map[string]any{
					"name":    node.Name,
					"type":    node.Kind,
					"file":    node.FilePath,
					"summary": node.QualifiedName,
				})
			}
		}
	}
	return json.Marshal(map[string]any{"results": results})
}

func (s *MCPServer) handleQueryGraph(params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Intent string `json:"intent"`
		Query  string `json:"query"`
		Scope  string `json:"scope"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
	}
	switch strings.ToLower(strings.TrimSpace(req.Intent)) {
	case "symbol_lookup", "semantic_search", "search":
		return s.handleSemanticSearchNodes(params)
	case "impact_radius":
		return s.handleGetImpactRadius(mustMarshal(map[string]any{"symbol": req.Query, "depth": 2}))
	case "review_context":
		return s.handleGetReviewContext(mustMarshal(map[string]any{"files": []string{req.Query}}))
	case "docs_section":
		return s.handleGetDocsSection(mustMarshal(map[string]any{"section": req.Query}))
	default:
		results := []map[string]any{}
		if req.Query != "" && s.store != nil {
			if notes, err := s.store.SearchKGNotes(req.Query, 10); err == nil {
				for _, note := range notes {
					results = append(results, map[string]any{
						"type":    note.NoteType,
						"title":   note.Title,
						"summary": note.Summary,
					})
				}
			}
		}
		payload := map[string]any{"results": results}
		if req.Intent != "" {
			payload["warnings"] = []string{fmt.Sprintf("unsupported query intent %q", req.Intent)}
		}
		return json.Marshal(payload)
	}
}

func (s *MCPServer) handleGetReviewContext(params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
	}
	if len(req.Files) == 0 {
		return nil, &rpcError{Code: -32602, Message: "invalid params", Data: "files are required"}
	}
	bridge, err := s.requireBridge()
	if err != nil {
		return nil, err
	}
	report, err := bridge.DetectChanges(DetectChangesOptions{})
	if err != nil {
		return nil, err
	}
	riskSummary := report.Summary
	if strings.TrimSpace(riskSummary) == "" {
		riskSummary = fmt.Sprintf("%d changed functions, %d test gaps, %d review priorities", len(report.ChangedFunctions), len(report.TestGaps), len(report.ReviewPriorities))
	}
	impactNodes := []map[string]any{}
	if s.store != nil {
		impact, err := s.store.GetImpactRadius(req.Files, 2, 50)
		if err == nil {
			for _, node := range impact.ChangedNodes {
				impactNodes = append(impactNodes, graphNodeToMCP(node))
			}
			for _, node := range impact.ImpactedNodes {
				impactNodes = append(impactNodes, graphNodeToMCP(node))
			}
		}
	}
	changed := make([]map[string]any, 0, len(report.ChangedFunctions))
	for _, fn := range report.ChangedFunctions {
		changed = append(changed, map[string]any{
			"name":       fn.QualifiedName,
			"type":       "changed_function",
			"file":       fn.FilePath,
			"risk_score": fn.RiskScore,
			"summary":    fn.FilePath,
		})
	}
	return json.Marshal(map[string]any{
		"changed_symbols": changed,
		"impact_radius":   impactNodes,
		"risk_summary":    riskSummary,
	})
}

func (s *MCPServer) handleGetDocsSection(params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Section string `json:"section"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
	}
	section := strings.TrimSpace(req.Section)
	if section == "" {
		return nil, &rpcError{Code: -32602, Message: "invalid params", Data: "section is required"}
	}
	candidates := []string{
		filepath.Join(s.workDir, "docs", "KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md"),
		filepath.Join(s.workDir, "docs", "SKILL_COMMAND_INTEGRATION.md"),
		filepath.Join(s.workDir, ".agents", "workflow", "plans", "crg-kg-integration", "crg-kg-integration.plan.md"),
	}
	for _, candidate := range candidates {
		if content, ok := extractMarkdownSection(candidate, section); ok {
			return json.Marshal(map[string]any{"content": content, "source": candidate})
		}
	}
	return json.Marshal(map[string]any{"content": "", "source": ""})
}

func (s *MCPServer) requireBridge() (mcpBridge, error) {
	if s.bridge != nil {
		return s.bridge, nil
	}
	if s.bridgeErr != nil {
		return nil, s.bridgeErr
	}
	return nil, fmt.Errorf("CRG bridge unavailable")
}

func (s *MCPServer) loadStats() (GraphStats, error) {
	if s.store != nil {
		return s.store.GetStats()
	}
	if s.storeErr != nil {
		return GraphStats{}, s.storeErr
	}
	return GraphStats{}, fmt.Errorf("graph store unavailable")
}

func impactNodeToMCP(node ImpactNode) map[string]any {
	return map[string]any{
		"name":       node.Name,
		"type":       node.Kind,
		"file":       node.FilePath,
		"risk_score": 0.0,
	}
}

func graphNodeToMCP(node GraphNode) map[string]any {
	return map[string]any{
		"name":       node.Name,
		"type":       node.Kind,
		"file":       node.FilePath,
		"risk_score": 0.0,
	}
}

func extractMarkdownSection(path, want string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	lines := strings.Split(string(data), "\n")
	wantNorm := normalizeHeading(want)
	start := -1
	startLevel := 0
	for i, line := range lines {
		level, heading, ok := parseHeading(line)
		if !ok {
			continue
		}
		if normalizeHeading(heading) == wantNorm {
			start = i
			startLevel = level
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		level, _, ok := parseHeading(lines[i])
		if ok && level <= startLevel {
			end = i
			break
		}
	}
	section := strings.Join(lines[start:end], "\n")
	return strings.TrimSpace(section), true
}

func parseHeading(line string) (int, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(line[level:]), true
}

func normalizeHeading(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
