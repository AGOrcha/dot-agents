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

const mcpInvalidParamsMessage = "invalid params"

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
		resp := buildRPCResponse(req.ID, result, rpcErr)
		if len(req.ID) == 0 {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}

// buildRPCResponse assembles a JSON-RPC response from a dispatch result and
// error, normalizing typed *rpcError values and falling back to an internal
// error code for anything else.
func buildRPCResponse(id json.RawMessage, result json.RawMessage, rpcErr error) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: id}
	if rpcErr == nil {
		resp.Result = result
		return resp
	}
	if re, ok := rpcErr.(*rpcError); ok {
		resp.Error = re
	} else {
		resp.Error = &rpcError{Code: -32603, Message: rpcErr.Error()}
	}
	return resp
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
				return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: err.Error()}
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

func toolWithProps(name, desc string, props map[string]any) toolDescriptor {
	return toolDescriptor{
		Name:        name,
		Description: desc,
		InputSchema: map[string]any{"type": "object", "properties": props},
	}
}

func (s *MCPServer) handleToolsList(_ json.RawMessage) (json.RawMessage, error) {
	tools := []toolDescriptor{
		toolWithProps("build_or_update_graph_tool", "Build or update the code graph for the current repository.", map[string]any{}),
		toolWithProps("embed_graph_tool", "Run graph post-processing for downstream queries.", map[string]any{}),
		toolWithProps("list_graph_stats_tool", "Return code graph statistics.", map[string]any{}),
		toolWithProps("get_impact_radius_tool", "Return the impact radius for a symbol.", map[string]any{
			"symbol": map[string]any{"type": "string"},
			"depth":  map[string]any{"type": "integer"},
		}),
		toolWithProps("semantic_search_nodes_tool", "Search the graph for matching code symbols.", map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer"},
		}),
		toolWithProps("query_graph_tool", "Run a higher-level graph query by intent.", map[string]any{
			"intent": map[string]any{"type": "string"},
			"query":  map[string]any{"type": "string"},
			"scope":  map[string]any{"type": "string"},
		}),
		toolWithProps("get_review_context_tool", "Summarize changed symbols and impact radius for a file set.", map[string]any{
			"files": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		}),
		toolWithProps("get_docs_section_tool", "Return a documentation section by heading.", map[string]any{
			"section": map[string]any{"type": "string"},
		}),
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
	payload := map[string]any{
		"nodes":       stats.TotalNodes,
		"edges":       stats.TotalEdges,
		"languages":   s.collectStatsLanguages(stats),
		"communities": s.countStatsCommunities(),
	}
	return json.Marshal(payload)
}

// countStatsCommunities returns the number of communities reported by the
// bridge, or 0 when the bridge is absent or returns an error.
func (s *MCPServer) countStatsCommunities() int {
	if s.bridge == nil {
		return 0
	}
	result, err := s.bridge.ListCommunities(0, "size")
	if err != nil || result == nil {
		return 0
	}
	return len(result.Communities)
}

// collectStatsLanguages returns a {language: count} map preferring the warm
// store's Languages slice and falling back to the bridge status string.
func (s *MCPServer) collectStatsLanguages(stats GraphStats) map[string]int {
	languages := map[string]int{}
	if len(stats.Languages) > 0 {
		for _, lang := range stats.Languages {
			languages[lang]++
		}
		return languages
	}
	if s.bridge == nil {
		return languages
	}
	status, err := s.bridge.Status()
	if err != nil || status == nil {
		return languages
	}
	for _, lang := range strings.Split(status.Languages, ",") {
		lang = strings.TrimSpace(lang)
		if lang != "" {
			languages[lang]++
		}
	}
	return languages
}

func (s *MCPServer) handleGetImpactRadius(params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Symbol string `json:"symbol"`
		Depth  int    `json:"depth"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: err.Error()}
	}
	if strings.TrimSpace(req.Symbol) == "" {
		return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: "symbol is required"}
	}
	depth := req.Depth
	if depth <= 0 {
		depth = 2
	}
	files := s.resolveImpactFiles(req.Symbol)
	bridge, err := s.requireBridge()
	if err != nil {
		return nil, err
	}
	// Freshness guard: return a structured error when the graph is not ready
	// so callers can distinguish "no impact" from "graph not built".
	if guard, gerr := graphReadinessGuardJSON(bridge); gerr != nil || guard != nil {
		return guard, gerr
	}
	result, err := bridge.GetImpactRadius(ImpactOptions{
		ChangedFiles: files,
		MaxDepth:     depth,
	})
	if err != nil {
		return nil, err
	}
	nodes := dedupImpactNodes(result.ChangedNodes, result.ImpactedNodes)
	return json.Marshal(map[string]any{"nodes": nodes})
}

// resolveImpactFiles expands a symbol query into the set of file paths used
// as the impact-radius seed. Falls back to a single-element slice containing
// the raw symbol when the warm store is unavailable or returns nothing.
func (s *MCPServer) resolveImpactFiles(symbol string) []string {
	files := []string{symbol}
	if s.store == nil {
		return files
	}
	nodes, err := s.store.SearchNodes(symbol, 20)
	if err != nil {
		return files
	}
	seen := map[string]bool{}
	var dedup []string
	for _, node := range nodes {
		if node.FilePath == "" || seen[node.FilePath] {
			continue
		}
		seen[node.FilePath] = true
		dedup = append(dedup, node.FilePath)
	}
	if len(dedup) > 0 {
		return dedup
	}
	return files
}

// dedupImpactNodes returns the changed-then-impacted node sequence with
// duplicates by qualified name (or fallback name) removed, in original order.
func dedupImpactNodes(changed, impacted []ImpactNode) []map[string]any {
	nodes := make([]map[string]any, 0, len(changed)+len(impacted))
	seen := map[string]bool{}
	appendIfNew := func(node ImpactNode) {
		key := node.QualifiedName
		if key == "" {
			key = node.Name
		}
		if seen[key] {
			return
		}
		seen[key] = true
		nodes = append(nodes, impactNodeToMCP(node))
	}
	for _, node := range changed {
		appendIfNew(node)
	}
	for _, node := range impacted {
		appendIfNew(node)
	}
	return nodes
}

func (s *MCPServer) handleSemanticSearchNodes(params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: err.Error()}
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: "query is required"}
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
		return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: err.Error()}
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
		return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: err.Error()}
	}
	if len(req.Files) == 0 {
		return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: "files are required"}
	}
	bridge, err := s.requireBridge()
	if err != nil {
		return nil, err
	}
	// Freshness guard: return a structured error when the graph is not ready
	// rather than silently returning empty changed symbols.
	if guard, gerr := graphReadinessGuardJSON(bridge); gerr != nil || guard != nil {
		return guard, gerr
	}
	// Pass req.Files so callers can see the scope, even though the CRG CLI
	// detect-changes subcommand does not yet accept a --files filter (v1.x
	// limitation). The changed_functions section reflects the HEAD~1 diff;
	// req.Files is used below for the warm-store impact-radius query.
	report, err := bridge.DetectChanges(DetectChangesOptions{Files: req.Files})
	if err != nil {
		return nil, err
	}
	riskSummary := report.Summary
	if strings.TrimSpace(riskSummary) == "" {
		riskSummary = fmt.Sprintf("%d changed functions, %d test gaps, %d review priorities", len(report.ChangedFunctions), len(report.TestGaps), len(report.ReviewPriorities))
	}
	return json.Marshal(map[string]any{
		"changed_symbols": reviewChangedSymbols(report.ChangedFunctions),
		"impact_radius":   s.reviewImpactNodes(req.Files),
		"risk_summary":    riskSummary,
	})
}

// reviewChangedSymbols projects each changed function from a CRG report into
// the MCP review-context payload shape.
func reviewChangedSymbols(fns []CRGChangedNode) []map[string]any {
	changed := make([]map[string]any, 0, len(fns))
	for _, fn := range fns {
		changed = append(changed, map[string]any{
			"name":       fn.QualifiedName,
			"type":       "changed_function",
			"file":       fn.FilePath,
			"risk_score": fn.RiskScore,
			"summary":    fn.FilePath,
		})
	}
	return changed
}

// reviewImpactNodes returns the impact-radius node payload for a review
// context request, defaulting to an empty slice when the warm store is
// unavailable or the query fails.
func (s *MCPServer) reviewImpactNodes(files []string) []map[string]any {
	out := []map[string]any{}
	if s.store == nil {
		return out
	}
	impact, err := s.store.GetImpactRadius(files, 2, 50)
	if err != nil {
		return out
	}
	for _, node := range impact.ChangedNodes {
		out = append(out, graphNodeToMCP(node))
	}
	for _, node := range impact.ImpactedNodes {
		out = append(out, graphNodeToMCP(node))
	}
	return out
}

func (s *MCPServer) handleGetDocsSection(params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Section string `json:"section"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: err.Error()}
	}
	section := strings.TrimSpace(req.Section)
	if section == "" {
		return nil, &rpcError{Code: -32602, Message: mcpInvalidParamsMessage, Data: "section is required"}
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

// graphReadinessGuardJSON returns a JSON envelope when the graph is in
// an unbuilt or busy-or-locked state, or (nil, nil) when the graph is
// ready (or status cannot be determined — callers proceed in that
// case). Tool handlers should call this immediately after
// requireBridge and short-circuit when the returned bytes are non-nil.
func graphReadinessGuardJSON(bridge mcpBridge) ([]byte, error) {
	status, stErr := bridge.Status()
	if stErr != nil || status == nil {
		return nil, nil
	}
	switch status.State {
	case string(CRGReadinessUnbuilt):
		return json.Marshal(map[string]any{
			"error": "code graph not built",
			"state": status.State,
			"hint":  "run build_or_update_graph_tool first",
		})
	case string(CRGReadinessBusyOrLocked):
		return json.Marshal(map[string]any{
			"error": "code graph is busy or locked",
			"state": status.State,
			"hint":  "wait for concurrent operation to complete",
		})
	}
	return nil, nil
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
