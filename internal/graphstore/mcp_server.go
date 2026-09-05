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

// mcpMaxImpactDepth / mcpMaxImpactResults bound the impact-radius request the
// MCP server forwards to the CRG Python subprocess, which applies no internal
// ceiling. Without these a single client request (depth: 1e6) could exhaust
// CPU/memory in the subprocess.
const (
	mcpMaxImpactDepth   = 6
	mcpMaxImpactResults = 200
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
	// nodes caches the backend's node export for the fallbacks below, so one
	// session never re-exports the graph per tool call.
	nodes []GraphNode
	// nodesLoaded distinguishes "not exported yet" from "exported, empty".
	nodesLoaded bool
}

// nodeExporter is the slice of the backend used by the warm-store fallbacks:
// the bulk node export. Both the bridge and the kg-native engine provide it.
type nodeExporter interface {
	ReadNodes(limit int) ([]GraphNode, error)
}

// backendNodes returns the backend's nodes, exported once per session.
//
// It exists because several MCP tools historically read the WARM store
// ($KG_HOME/ops/graphstore.db), which is populated only by an explicit
// `da kg warm --include-code` mirror pass. The authoritative code graph now
// lives with the selected backend, so a tool whose warm-store read comes back
// empty falls back here instead of reporting an empty graph.
func (s *MCPServer) backendNodes() []GraphNode {
	if s.nodesLoaded {
		return s.nodes
	}
	s.nodesLoaded = true
	exporter, ok := s.bridge.(nodeExporter)
	if !ok || exporter == nil {
		return nil
	}
	nodes, err := exporter.ReadNodes(0)
	if err != nil {
		return nil
	}
	s.nodes = nodes
	return s.nodes
}

// matchBackendNodes returns backend nodes whose name or qualified name contains
// query (case-insensitive), capped at limit.
func (s *MCPServer) matchBackendNodes(query string, limit int) []GraphNode {
	needle := strings.ToLower(strings.TrimSpace(query))
	var out []GraphNode
	for _, n := range s.backendNodes() {
		if n.Kind == NodeKindFile || !nodeMatches(n, needle) {
			continue
		}
		out = append(out, n)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// nodeMatches reports whether a node's name or qualified name contains needle
// (already lower-cased). An empty needle matches everything.
func nodeMatches(n GraphNode, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(n.Name), needle) ||
		strings.Contains(strings.ToLower(n.QualifiedName), needle)
}

// NewMCPServer builds a server backed by the legacy Python CRG subprocess. It
// is the pre-cutover constructor, kept because the bridge remains a supported
// rollback backend until the §11.4 decommissioning gate passes. Production
// `da kg serve` no longer calls it: commands/kg resolves the configured backend
// and calls NewMCPServerWithProvider, so the eight tools are kg-native-backed
// by default.
func NewMCPServer(workDir string) *MCPServer {
	bridge, err := NewCRGBridge(workDir)
	if err != nil {
		return NewMCPServerWithProvider(workDir, nil, err)
	}
	return NewMCPServerWithProvider(workDir, bridge, nil)
}

// NewMCPServerWithProvider builds a server over an explicitly selected
// code-graph backend. The tool surface — names, arguments and response shapes —
// is identical whichever provider is passed; only the engine behind it changes.
// A nil provider with a non-nil providerErr makes graph-backed tools report
// that error rather than panicking, exactly as an undiscoverable bridge did.
func NewMCPServerWithProvider(workDir string, provider CodeGraphProvider, providerErr error) *MCPServer {
	s := &MCPServer{workDir: workDir, bridgeErr: providerErr}
	if provider != nil {
		s.bridge = provider
	}
	if store, err := OpenSQLite(defaultGraphstoreDBPath()); err == nil {
		s.store = store
	} else {
		s.storeErr = err
	}
	return s
}

// defaultKGHomeExit is invoked by defaultKGHome() when no KG_HOME override
// is set and the process cannot resolve a home directory — same guard
// class as kgHome() (commands/kg) and config.PreflightUserHome
// (internal/config/paths.go): print an actionable message and exit instead
// of degrading to a relative path. Kept as a package var so tests can
// observe the failure without exiting the test binary.
var defaultKGHomeExit = func(err error) {
	fmt.Fprintf(os.Stderr, "error: cannot resolve home directory for the knowledge graph: %v — set $HOME or $KG_HOME and retry\n", err)
	os.Exit(1)
}

func defaultKGHome() string {
	if v := os.Getenv("KG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		defaultKGHomeExit(err)
		return ""
	}
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
	nodes, edges := stats.TotalNodes, stats.TotalEdges
	if nodes == 0 && edges == 0 {
		// The warm store is a mirror, not the source of truth for the code
		// graph; when it has not been mirrored, report the backend's counts.
		nodes, edges = s.backendCounts()
	}
	payload := map[string]any{
		"nodes":       nodes,
		"edges":       edges,
		"languages":   s.collectStatsLanguages(stats),
		"communities": s.countStatsCommunities(),
	}
	return json.Marshal(payload)
}

// backendCounts reads the node/edge totals straight off the backend.
func (s *MCPServer) backendCounts() (int, int) {
	if s.bridge == nil {
		return 0, 0
	}
	status, err := s.bridge.Status()
	if err != nil || status == nil {
		return 0, 0
	}
	return status.Nodes, status.Edges
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
	// Server-side ceiling. This path routes to the CRG Python subprocess
	// (bridge.GetImpactRadius), which has no internal depth bound — an
	// unclamped client-supplied depth would let one MCP call pin CPU/memory
	// in the subprocess. The native BFS caps itself; the subprocess does not.
	if depth > mcpMaxImpactDepth {
		depth = mcpMaxImpactDepth
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
		MaxResults:   mcpMaxImpactResults,
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
	if dedup := uniqueFilePaths(s.warmSearch(symbol, 20)); len(dedup) > 0 {
		return dedup
	}
	// Warm store empty or unmirrored — resolve against the backend's own graph.
	if dedup := uniqueFilePaths(s.matchBackendNodes(symbol, 20)); len(dedup) > 0 {
		return dedup
	}
	return []string{symbol}
}

// warmSearch runs a warm-store node search, tolerating an absent store.
func (s *MCPServer) warmSearch(query string, limit int) []GraphNode {
	if s.store == nil {
		return nil
	}
	nodes, err := s.store.SearchNodes(query, limit)
	if err != nil {
		return nil
	}
	return nodes
}

// uniqueFilePaths returns the distinct, non-empty file paths of nodes, in order.
func uniqueFilePaths(nodes []GraphNode) []string {
	seen := map[string]bool{}
	var out []string
	for _, node := range nodes {
		if node.FilePath == "" || seen[node.FilePath] {
			continue
		}
		seen[node.FilePath] = true
		out = append(out, node.FilePath)
	}
	return out
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
	nodes := s.warmSearch(req.Query, limit)
	if len(nodes) == 0 {
		nodes = s.matchBackendNodes(req.Query, limit)
	}
	results := []map[string]any{}
	for _, node := range nodes {
		results = append(results, map[string]any{
			"name":    node.Name,
			"type":    node.Kind,
			"file":    node.FilePath,
			"summary": node.QualifiedName,
		})
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
	if out := s.warmImpactNodes(files); len(out) > 0 {
		return out
	}
	return s.backendImpactNodes(files)
}

// warmImpactNodes reads the impact radius from the warm mirror.
func (s *MCPServer) warmImpactNodes(files []string) []map[string]any {
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

// backendImpactNodes reads the impact radius from the selected backend, the
// fallback when the warm mirror holds no code rows.
func (s *MCPServer) backendImpactNodes(files []string) []map[string]any {
	out := []map[string]any{}
	if s.bridge == nil {
		return out
	}
	result, err := s.bridge.GetImpactRadius(ImpactOptions{
		ChangedFiles: files, MaxDepth: 2, MaxResults: 50,
	})
	if err != nil || result == nil {
		return out
	}
	return dedupImpactNodes(result.ChangedNodes, result.ImpactedNodes)
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
		filepath.Join(s.workDir, ".agents", "workflow", "specs", "scoped-knowledge-graphs", "design.md"),
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
