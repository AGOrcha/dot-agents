package graphstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// MCPServer serves the pinned code-review-graph release's MCP surface.
//
// Everything a client can observe about the surface — the advertised tools and
// prompts, their JSON schemas and defaults, argument validation and its error
// strings, and the tools/call response envelope — comes from
// internal/crgrelease, which is generated from the release itself. This server
// only ROUTES: it decides, per call, whether the kg-native backend can answer
// exactly or whether the retained Python bridge must.
//
// Routing is capability-based and deliberately conservative, because the two
// failure modes are not symmetric. Answering from the bridge when native would
// do is merely slow; answering natively when the native backend cannot
// reproduce the release's behaviour returns a WRONG answer that looks right —
// most sharply for a repository whose sources the native scanner does not
// extract, where a native answer is computed from a graph that is missing
// files. So a call goes native only when both are true:
//
//  1. the tool has a native handler that reproduces the release's response,
//     and
//  2. the repository's sources are fully covered by the native scanner.
//
// Otherwise the call is routed to the bridge, and if the bridge is absent the
// server returns an explicit capability error naming the missing capability —
// never an empty success, which a client cannot distinguish from "the graph
// really is empty".
type MCPServer struct {
	workDir string

	// provider is the selected code-graph backend (kg-native engine or the
	// Python bridge), used by the tools that drive build/update lifecycle.
	provider CodeGraphProvider
	// providerErr records why no backend could be opened, so tool calls
	// report the real reason instead of a nil dereference.
	providerErr error

	// native answers tool calls in-process. It is nil when the selected
	// backend is not the kg-native engine.
	native NativeToolSet
	// bridge answers tool calls through the retained Python release.
	bridge ToolBridge

	// routing caches the repository's source-capability verdict, which is a
	// filesystem walk and must not be repeated per tool call.
	routing      *routingVerdict
	routingError error
}

// NativeToolSet is the in-process implementation of the release's tools.
//
// It lives behind an interface because the kg-native engine (internal/codegraph)
// depends on this package, so this package cannot depend on it. Args is
// already validated and defaulted against the release's schema, so a handler
// never re-implements a default or re-checks a type.
type NativeToolSet interface {
	// CallTool runs one release tool natively and returns its structured
	// result. Returning ErrToolNotNative asks the server to route the call to
	// the bridge instead.
	CallTool(name string, args crgrelease.Args) (any, error)
	// SourcesFullyNative reports whether every source file in the repository
	// is one the native scanner extracts, and names the languages that are
	// not when it is false.
	SourcesFullyNative() (bool, []string, error)
}

// ToolBridge runs one release tool through the retained Python release and
// returns its MCP result verbatim.
type ToolBridge interface {
	// Available reports whether the bridge can be executed here.
	Available() bool
	// CallTool forwards a validated, defaulted argument set.
	CallTool(name string, args map[string]any) (crgrelease.CallResult, error)
	// Unavailable explains why Available is false, for the capability error.
	Unavailable() string
}

// ErrToolNotNative is returned by a NativeToolSet for a tool it does not
// implement.
var ErrToolNotNative = errors.New("tool is not implemented by the native backend")

// routingVerdict caches the repository's source-capability answer.
type routingVerdict struct {
	fullyNative bool
	nonNative   []string
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

type mcpToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type mcpPromptCall struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// NewMCPServer builds a server backed by the retained Python bridge. It is the
// pre-cutover constructor, kept because the bridge remains the documented
// rollback backend until the §11.4 decommissioning gate passes.
func NewMCPServer(workDir string) *MCPServer {
	bridge, err := NewCRGBridge(workDir)
	if err != nil {
		return NewMCPServerWithProvider(workDir, nil, err)
	}
	return NewMCPServerWithProvider(workDir, bridge, nil)
}

// NewMCPServerWithProvider builds a server over an explicitly selected
// code-graph backend.
//
// The advertised surface is identical whichever backend is passed — that is
// the point of pinning it to the release contract — but the routing differs: a
// provider that implements NativeToolSet can answer natively, and one that
// does not is served entirely by the bridge. A nil provider with a non-nil
// providerErr makes graph-backed tools report that error rather than panicking.
func NewMCPServerWithProvider(workDir string, provider CodeGraphProvider, providerErr error) *MCPServer {
	s := &MCPServer{workDir: workDir, provider: provider, providerErr: providerErr}
	if native, ok := provider.(NativeToolSet); ok && native != nil {
		s.native = native
	}
	if bridge, ok := provider.(ToolBridge); ok && bridge != nil {
		s.bridge = bridge
	} else {
		s.bridge = NewCRGToolBridge(workDir)
	}
	return s
}

// WithNativeToolSet replaces the in-process tool implementation. The kg-native
// engine is both a CodeGraphProvider and a NativeToolSet, so wiring is
// automatic for it; this exists for a deployment that pairs a provider with a
// separately-constructed native tool set, and for exercising the routing
// decision without a full backend.
func (s *MCPServer) WithNativeToolSet(native NativeToolSet) *MCPServer {
	s.native = native
	s.routing, s.routingError = nil, nil
	return s
}

// WithToolBridge replaces the bridge used for bridge-routed calls. It exists
// so tests can drive the routing decision without a Python installation.
func (s *MCPServer) WithToolBridge(bridge ToolBridge) *MCPServer {
	s.bridge = bridge
	return s
}

// defaultKGHomeExit is invoked by defaultKGHome() when no KG_HOME override is
// set and the process cannot resolve a home directory — the same guard class
// as kgHome() (commands/kg) and config.PreflightUserHome: print an actionable
// message and exit instead of degrading to a relative path. Kept as a package
// var so tests can observe the failure without exiting the test binary.
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

// Serve runs the stdio JSON-RPC loop.
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
		// A notification (no id) gets no response, per JSON-RPC.
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
	var typed *rpcError
	if errors.As(rpcErr, &typed) {
		resp.Error = typed
	} else {
		resp.Error = &rpcError{Code: -32603, Message: rpcErr.Error()}
	}
	return resp
}

func (s *MCPServer) dispatch(method string, _ json.RawMessage, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "initialize":
		return s.handleInitialize()
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "ping":
		return json.Marshal(map[string]any{})
	case "tools/list":
		return crgrelease.ToolsListResult()
	case "tools/call":
		return s.handleToolsCall(params)
	case "prompts/list":
		return crgrelease.PromptsListResult()
	case "prompts/get":
		return s.handlePromptsGet(params)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found", Data: method}
	}
}

// handleInitialize answers with the release's own server identity and
// capability advertisement. Clients gate features on these, so a native server
// that renamed itself or dropped the prompts capability would not be a drop-in.
func (s *MCPServer) handleInitialize() (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"protocolVersion": crgrelease.ProtocolVersion,
		"capabilities": map[string]any{
			"experimental": map[string]any{},
			"logging":      map[string]any{},
			"prompts":      map[string]any{"listChanged": false},
			"resources":    map[string]any{"subscribe": false, "listChanged": false},
			"tools":        map[string]any{"listChanged": true},
		},
		"serverInfo": map[string]any{
			"name":    crgrelease.ServerName,
			"version": crgrelease.Version,
		},
		"instructions": crgrelease.Instructions,
	})
}

func (s *MCPServer) handlePromptsGet(params json.RawMessage) (json.RawMessage, error) {
	var call mcpPromptCall
	if len(params) > 0 {
		if err := json.Unmarshal(params, &call); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
		}
	}
	prompt, ok, err := crgrelease.LookupPrompt(call.Name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &rpcError{
			Code:    -32602,
			Message: "invalid params",
			Data:    fmt.Sprintf("Unknown prompt: %q", call.Name),
		}
	}
	return json.Marshal(map[string]any{
		"description": prompt.Description,
		"messages":    prompt.Render(call.Arguments),
	})
}

// handleToolsCall validates the call against the release schema and routes it.
func (s *MCPServer) handleToolsCall(params json.RawMessage) (json.RawMessage, error) {
	var call mcpToolCall
	if len(params) > 0 {
		if err := json.Unmarshal(params, &call); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
		}
	}
	tool, ok := crgrelease.Lookup(call.Name)
	if !ok {
		// An unknown tool is an isError RESULT in this protocol, not a
		// JSON-RPC error — matching the release exactly.
		return encodeResult(crgrelease.ErrorResult(crgrelease.UnknownToolError(call.Name).Message))
	}
	args, toolErr := tool.Bind(call.Arguments)
	if toolErr != nil {
		return encodeResult(crgrelease.ErrorResult(toolErr.Message))
	}
	return encodeResult(s.route(tool.Name, args))
}

// route executes a validated call on the backend that can answer it exactly.
func (s *MCPServer) route(name string, args crgrelease.Args) crgrelease.CallResult {
	capability, ok := crgrelease.Capability(name)
	if !ok {
		// Capabilities() proves at test time that every published tool has a
		// decision; reaching here means the contract and the map disagree.
		return crgrelease.ErrorResult(fmt.Sprintf(
			"%s has no backend routing decision in the pinned code-review-graph %s contract",
			name, crgrelease.Version))
	}

	if capability.NativeBackend && s.native != nil {
		switch fullyNative, nonNative := s.sourcesFullyNative(); {
		case fullyNative:
			result, err := s.native.CallTool(name, args)
			if err == nil {
				success, encodeErr := crgrelease.SuccessResult(result)
				if encodeErr != nil {
					return crgrelease.ErrorResult(encodeErr.Error())
				}
				return success
			}
			if !errors.Is(err, ErrToolNotNative) {
				// An exception escaping a tool is reported by the release
				// with a uniform prefix and NO structured content. The
				// prefix is part of what clients match on, and it is
				// identical for every tool, so it belongs here rather than
				// in each handler.
				return crgrelease.ToolExceptionResult(name, err)
			}
			// The native backend declined the tool; fall through to the
			// bridge rather than inventing an answer.
			return s.callBridge(name, args, crgrelease.SourcesUnsupportedReason(nil))
		default:
			return s.callBridge(name, args, crgrelease.SourcesUnsupportedReason(nonNative))
		}
	}

	reason := capability.BridgeOnlyReason
	if reason == "" {
		reason = "the kg-native code-graph backend is not the selected backend"
	}
	return s.callBridge(name, args, reason)
}

// callBridge forwards a call to the retained release, or reports exactly which
// capability is missing when it cannot.
func (s *MCPServer) callBridge(name string, args crgrelease.Args, reason string) crgrelease.CallResult {
	if s.bridge == nil || !s.bridge.Available() {
		detail := ""
		if s.bridge != nil {
			detail = s.bridge.Unavailable()
		}
		if detail == "" && s.providerErr != nil {
			detail = s.providerErr.Error()
		}
		return crgrelease.ErrorResult(
			crgrelease.BridgeUnavailableMessage(name, reason, detail))
	}
	result, err := s.bridge.CallTool(name, args.Raw())
	if err != nil {
		// A discovered interpreter that cannot import the release is the
		// same condition as an absent bridge from the caller's point of
		// view, so it gets the actionable capability message rather than a
		// transport error they cannot act on.
		if strings.Contains(err.Error(), "code-review-graph import failed") {
			return crgrelease.ErrorResult(
				crgrelease.BridgeUnavailableMessage(name, reason, err.Error()))
		}
		return crgrelease.ErrorResult(fmt.Sprintf(
			"%s failed in the retained code-review-graph %s bridge: %v",
			name, crgrelease.Version, err))
	}
	return result
}

// sourcesFullyNative reports the repository's cached source-capability
// verdict. A verdict that cannot be computed is treated as NOT fully native:
// routing to the bridge on an unknown repository is recoverable, answering
// from an incomplete graph is not.
func (s *MCPServer) sourcesFullyNative() (bool, []string) {
	if s.routing == nil && s.routingError == nil {
		if s.native == nil {
			s.routingError = errors.New("no native backend")
		} else {
			fullyNative, nonNative, err := s.native.SourcesFullyNative()
			if err != nil {
				s.routingError = err
			} else {
				s.routing = &routingVerdict{fullyNative: fullyNative, nonNative: nonNative}
			}
		}
	}
	if s.routing == nil {
		detail := "the repository's source languages could not be determined"
		if s.routingError != nil {
			detail = s.routingError.Error()
		}
		return false, []string{detail}
	}
	return s.routing.fullyNative, s.routing.nonNative
}

func encodeResult(result crgrelease.CallResult) (json.RawMessage, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// RoutingPlan reports, per published tool, which backend would answer it for
// this repository and why. It backs the `da kg` capability diagnostic, so the
// routing decision is inspectable rather than implicit.
func (s *MCPServer) RoutingPlan() ([]ToolRouting, error) {
	capabilities, err := crgrelease.Capabilities()
	if err != nil {
		return nil, err
	}
	fullyNative, nonNative := s.sourcesFullyNative()
	plan := make([]ToolRouting, 0, len(capabilities))
	for _, capability := range capabilities {
		routing := ToolRouting{Tool: capability.Tool, Backend: crgrelease.BackendBridge}
		switch {
		case !capability.NativeBackend:
			routing.Reason = capability.BridgeOnlyReason
		case s.native == nil:
			routing.Reason = "the kg-native code-graph backend is not the selected backend"
		case !fullyNative:
			routing.Reason = crgrelease.SourcesUnsupportedReason(nonNative)
		default:
			routing.Backend = crgrelease.BackendNative
		}
		if routing.Backend == crgrelease.BackendBridge {
			routing.BridgeAvailable = s.bridge != nil && s.bridge.Available()
		}
		plan = append(plan, routing)
	}
	return plan, nil
}

// ToolRouting is one tool's resolved backend for a repository.
type ToolRouting struct {
	Tool            string             `json:"tool"`
	Backend         crgrelease.Backend `json:"backend"`
	Reason          string             `json:"reason,omitempty"`
	BridgeAvailable bool               `json:"bridge_available,omitempty"`
}
