package graphstore

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// covMCPNativeProvider is a CodeGraphProvider that is ALSO a NativeToolSet —
// the shape the kg-native engine has. The embedded *CRGBridge only supplies
// the provider methods; the tool answers come from the stub, so the wiring
// assertion does not need a built graph.
type covMCPNativeProvider struct {
	*CRGBridge
	*stubNative
}

// covMCPBridgeProvider is a CodeGraphProvider that is ALSO a ToolBridge — the
// shape the retained Python backend has. Its bridge methods are declared on
// the outer type so they shadow the embedded *CRGBridge's Available().
type covMCPBridgeProvider struct {
	*CRGBridge
	tools *stubBridge
}

func (p covMCPBridgeProvider) Available() bool     { return p.tools.Available() }
func (p covMCPBridgeProvider) Unavailable() string { return p.tools.Unavailable() }

func (p covMCPBridgeProvider) CallTool(
	name string, args map[string]any,
) (crgrelease.CallResult, error) {
	return p.tools.CallTool(name, args)
}

// covMCPResult builds a success envelope tagged with the leg it came from, so
// a test can tell WHICH backend answered rather than only counting calls.
func covMCPResult(origin string) crgrelease.CallResult {
	payload := json.RawMessage(`{"from":"` + origin + `"}`)
	return crgrelease.CallResult{
		Content:           []crgrelease.ContentBlock{{Type: "text", Text: string(payload)}},
		StructuredContent: payload,
	}
}

// covMCPProvider pairs a stub tool set with a provider, so the provider is the
// same object the server must recognise as a NativeToolSet.
func covMCPProvider(t *testing.T, native *stubNative) covMCPNativeProvider {
	t.Helper()
	return covMCPNativeProvider{CRGBridge: &CRGBridge{RepoRoot: t.TempDir()}, stubNative: native}
}

// covMCPStructured returns a tools/call result's decoded structuredContent.
func covMCPStructured(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("no structuredContent in %#v", result)
	}
	return structured
}

// covMCPErrorText returns the message of an isError tools/call result.
func covMCPErrorText(t *testing.T, result map[string]any) string {
	t.Helper()
	if isError, _ := result["isError"].(bool); !isError {
		t.Fatalf("expected an isError result, got %#v", result)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("isError result carries no content: %#v", result)
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	return text
}

// covMCPCallResultText returns the message of an isError CallResult returned
// by route() directly.
func covMCPCallResultText(t *testing.T, result crgrelease.CallResult) string {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected an error result, got %+v", result)
	}
	if len(result.Content) == 0 {
		t.Fatalf("error result carries no content: %+v", result)
	}
	return result.Content[0].Text
}

// covMCPRequireAll fails unless text names every fragment. The error
// vocabulary is the contract here: a message a client cannot act on is a bug
// even when the routing decision behind it is right.
func covMCPRequireAll(t *testing.T, text string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Errorf("message does not mention %q: %s", fragment, text)
		}
	}
}

// covMCPRPCError asserts err is a JSON-RPC error carrying code.
func covMCPRPCError(t *testing.T, err error, code int) *rpcError {
	t.Helper()
	var typed *rpcError
	if !errors.As(err, &typed) {
		t.Fatalf("err = %v, want a JSON-RPC *rpcError", err)
	}
	if typed.Code != code {
		t.Errorf("code = %d, want %d", typed.Code, code)
	}
	return typed
}

// covMCPDispatch routes one JSON-RPC method through the server's router.
func covMCPDispatch(
	t *testing.T, srv *MCPServer, method, params string,
) (json.RawMessage, error) {
	t.Helper()
	var raw json.RawMessage
	if params != "" {
		raw = json.RawMessage(params)
	}
	return srv.dispatch(method, nil, raw)
}

// The backend a call is answered on follows from the provider's own type: a
// provider that implements NativeToolSet answers in-process, and one that
// implements ToolBridge is reused as the bridge instead of a freshly
// discovered one. That is what makes the cutover a provider swap rather than a
// server rewrite, and it is observable in which stub receives the call.
func TestCovMCPProviderTypeSelectsTheAnsweringLeg(t *testing.T) {
	t.Run("a NativeToolSet provider answers in-process", func(t *testing.T) {
		native := &stubNative{fullyNative: true, result: map[string]any{"from": "native"}}
		srv := NewMCPServerWithProvider(t.TempDir(), covMCPProvider(t, native), nil)
		if srv.native == nil {
			t.Fatal("a provider implementing NativeToolSet was not wired as one")
		}
		if srv.bridge == nil {
			t.Fatal("no bridge was wired for a provider that is not itself a bridge")
		}
		result := callTool(t, srv, "list_graph_stats_tool", `{}`)
		if got := covMCPStructured(t, result)["from"]; got != "native" {
			t.Errorf("answer came from %v, want the native leg", got)
		}
		if len(native.calls) != 1 {
			t.Errorf("native calls = %v, want exactly one", native.calls)
		}
	})

	t.Run("a ToolBridge provider is reused as the bridge", func(t *testing.T) {
		tools := &stubBridge{available: true, result: covMCPResult("provider-bridge")}
		provider := covMCPBridgeProvider{CRGBridge: &CRGBridge{RepoRoot: t.TempDir()}, tools: tools}
		srv := NewMCPServerWithProvider(t.TempDir(), provider, nil)
		if srv.native != nil {
			t.Error("a bridge provider must not be wired as a native tool set")
		}
		result := callTool(t, srv, "list_graph_stats_tool", `{}`)
		if got := covMCPStructured(t, result)["from"]; got != "provider-bridge" {
			t.Errorf("answer came from %v, want the provider's own bridge", got)
		}
		if len(tools.calls) != 1 {
			t.Errorf("the provider's own bridge was not used: %v", tools.calls)
		}
	})
}

// covMCPMethodCase is one accepted JSON-RPC method and a key its result must
// carry.
type covMCPMethodCase struct {
	method string
	params string
	key    string
}

// Every method the server accepts must answer, and anything else must be
// method-not-found rather than a silent empty success — a client that probes
// an unsupported method has to be able to tell the difference.
func TestCovMCPDispatchAnswersEveryAcceptedMethod(t *testing.T) {
	srv := newTestServer(t,
		&stubNative{fullyNative: true, result: map[string]any{"status": "ok"}},
		&stubBridge{available: true})

	cases := []covMCPMethodCase{
		{method: "initialize", key: "serverInfo"},
		{method: "tools/list", key: "tools"},
		{method: "prompts/list", key: "prompts"},
		{method: "prompts/get", params: `{"name":"architecture_map"}`, key: "messages"},
		{
			method: "tools/call",
			params: `{"name":"list_graph_stats_tool","arguments":{}}`,
			key:    "content",
		},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			result, err := covMCPDispatch(t, srv, tc.method, tc.params)
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(result, &decoded); err != nil {
				t.Fatalf("%s: result is not an object: %v", tc.method, err)
			}
			if len(decoded[tc.key]) == 0 {
				t.Errorf("%s: result carries no %q: %s", tc.method, tc.key, result)
			}
		})
	}

	t.Run("notifications", func(t *testing.T) {
		for _, method := range []string{"notifications/initialized", "notifications/cancelled"} {
			result, err := covMCPDispatch(t, srv, method, `{}`)
			if err != nil || result != nil {
				t.Errorf("%s: result=%s err=%v, want no payload and no error",
					method, result, err)
			}
		}
	})

	t.Run("ping", func(t *testing.T) {
		result, err := covMCPDispatch(t, srv, "ping", "")
		if err != nil {
			t.Fatalf("ping: %v", err)
		}
		if string(result) != "{}" {
			t.Errorf("ping result = %s, want an empty object", result)
		}
	})

	t.Run("unknown method", func(t *testing.T) {
		result, err := covMCPDispatch(t, srv, "resources/list", "")
		if result != nil {
			t.Errorf("an unsupported method returned a payload: %s", result)
		}
		typed := covMCPRPCError(t, err, -32601)
		if typed.Message != "method not found" {
			t.Errorf("message = %q", typed.Message)
		}
		if typed.Data != "resources/list" {
			t.Errorf("data = %v, want the rejected method name", typed.Data)
		}
	})
}

// prompts/get is a request the client can get wrong in two ways, and both must
// come back as JSON-RPC invalid-params rather than as an empty prompt.
func TestCovMCPPromptsGetRejectsBadRequests(t *testing.T) {
	srv := newTestServer(t, nil, &stubBridge{available: true})

	t.Run("params that are not a prompt call", func(t *testing.T) {
		_, err := covMCPDispatch(t, srv, "prompts/get", `{"name":["review_changes"]}`)
		typed := covMCPRPCError(t, err, -32602)
		if typed.Message != "invalid params" {
			t.Errorf("message = %q", typed.Message)
		}
		if detail, _ := typed.Data.(string); detail == "" {
			t.Error("invalid params carries no decoding detail")
		}
	})

	t.Run("an unpublished prompt", func(t *testing.T) {
		_, err := covMCPDispatch(t, srv, "prompts/get", `{"name":"nope_prompt"}`)
		typed := covMCPRPCError(t, err, -32602)
		if typed.Data != `Unknown prompt: "nope_prompt"` {
			t.Errorf("data = %v, want the release's unknown-prompt text", typed.Data)
		}
	})

	// The release declares no required prompt arguments, so the nearest
	// client mistake is naming one the prompt does not declare. It must not
	// leak into the rendering, and must not suppress the release's own
	// default rendering for the sites it does declare.
	t.Run("an argument the prompt does not declare", func(t *testing.T) {
		withExtra, err := covMCPDispatch(t, srv, "prompts/get",
			`{"name":"review_changes","arguments":{"nonesuch":"zzz"}}`)
		if err != nil {
			t.Fatalf("prompts/get: %v", err)
		}
		plain, err := covMCPDispatch(t, srv, "prompts/get", `{"name":"review_changes"}`)
		if err != nil {
			t.Fatalf("prompts/get: %v", err)
		}
		if string(withExtra) != string(plain) {
			t.Errorf("an undeclared argument changed the rendering\n got: %s\nwant: %s",
				withExtra, plain)
		}
		if strings.Contains(string(withExtra), "zzz") {
			t.Error("an undeclared prompt argument leaked into the rendering")
		}
	})
}

// A tools/call whose params are not a tool call cannot be validated against
// any schema, so it is a protocol-level invalid-params rather than a tool
// error result.
func TestCovMCPToolsCallRejectsMalformedParams(t *testing.T) {
	srv := newTestServer(t, nil, &stubBridge{available: true})
	_, err := covMCPDispatch(t, srv, "tools/call", `{"name":{"tool":"list_graph_stats_tool"}}`)
	typed := covMCPRPCError(t, err, -32602)
	if typed.Message != "invalid params" {
		t.Errorf("message = %q", typed.Message)
	}
}

// Every published tool has a routing decision, and a name without one must say
// so instead of defaulting to a leg. Defaulting either way would be wrong: to
// native it answers from a backend that never claimed the tool, to the bridge
// it hides a contract drift behind a slow path.
func TestCovMCPRouteWithoutARoutingDecision(t *testing.T) {
	srv := newTestServer(t, &stubNative{fullyNative: true}, &stubBridge{available: true})
	text := covMCPCallResultText(t, srv.route("unpublished_tool", crgrelease.Args{}))
	covMCPRequireAll(t, text,
		"unpublished_tool", "no backend routing decision", crgrelease.Version)
}

// A native answer that cannot be rendered into the release's envelope must
// surface as an error result, not as a truncated or empty success that a
// client would read as a real payload.
func TestCovMCPNativeAnswerThatCannotBeEncoded(t *testing.T) {
	native := &stubNative{fullyNative: true, result: make(chan int)}
	srv := newTestServer(t, native, &stubBridge{available: true})
	text := covMCPErrorText(t, callTool(t, srv, "list_graph_stats_tool", `{}`))
	covMCPRequireAll(t, text, "encode tool result")
	if strings.Contains(text, "Error calling tool") {
		t.Errorf("an encoding failure was reported as an escaped handler exception: %s", text)
	}
}

// The source-coverage verdict must change the LEG a native-capable tool is
// answered on, because answering natively from a graph that is missing files
// is a wrong answer that looks right.
func TestCovMCPSourceCoverageChangesTheAnsweringLeg(t *testing.T) {
	const tool = "get_impact_radius_tool"

	t.Run("fully indexed sources are answered in-process", func(t *testing.T) {
		srv := newTestServer(t,
			&stubNative{fullyNative: true, result: map[string]any{"from": "native"}},
			&stubBridge{available: true, result: covMCPResult("bridge")})
		result := callTool(t, srv, tool, `{}`)
		if got := covMCPStructured(t, result)["from"]; got != "native" {
			t.Errorf("answer came from %v, want the native leg", got)
		}
	})

	t.Run("unextracted sources move the same tool to the bridge", func(t *testing.T) {
		srv := newTestServer(t,
			&stubNative{result: map[string]any{"from": "native"}, nonNative: []string{"ruby"}},
			&stubBridge{available: true, result: covMCPResult("bridge")})
		result := callTool(t, srv, tool, `{}`)
		if got := covMCPStructured(t, result)["from"]; got != "bridge" {
			t.Errorf("answer came from %v, want the bridge leg", got)
		}
	})

	t.Run("with no bridge the message names the unextracted languages", func(t *testing.T) {
		srv := newTestServer(t,
			&stubNative{nonNative: []string{"ruby", "kotlin"}},
			&stubBridge{unavailable: "code-review-graph is not installed"})
		text := covMCPErrorText(t, callTool(t, srv, tool, `{}`))
		covMCPRequireAll(t, text, tool, "ruby", "kotlin", "incomplete graph",
			"code-review-graph is not installed", "Install code-review-graph=="+crgrelease.Version)
	})
}

// When the bridge cannot run, the capability error is the only thing the
// client has to act on, so it must name the tool, why the call had to leave
// the native backend, and the concrete detail — falling back to the provider's
// own open failure when the bridge itself has nothing to say.
func TestCovMCPBridgeUnavailableExplainsTheGap(t *testing.T) {
	t.Run("a mute bridge falls back to the provider error", func(t *testing.T) {
		srv := NewMCPServerWithProvider(t.TempDir(), nil, errBackendProbe)
		srv.WithToolBridge(&stubBridge{})
		text := covMCPErrorText(t, callTool(t, srv, "embed_graph_tool", `{}`))
		covMCPRequireAll(t, text, "embed_graph_tool", "embedding pipeline",
			errBackendProbe.Error(), "Install code-review-graph=="+crgrelease.Version)
	})

	t.Run("a native-capable tool with no native backend says so", func(t *testing.T) {
		srv := NewMCPServerWithProvider(t.TempDir(), nil, errBackendProbe)
		srv.WithToolBridge(&stubBridge{unavailable: "no Python interpreter beside the release"})
		text := covMCPErrorText(t, callTool(t, srv, "list_graph_stats_tool", `{}`))
		covMCPRequireAll(t, text, "list_graph_stats_tool",
			"is not the selected backend", "no Python interpreter beside the release")
		if strings.Contains(text, errBackendProbe.Error()) {
			t.Errorf("the bridge's own reason was overwritten by the provider error: %s", text)
		}
	})
}

// A bridge that runs but cannot import the release is, to the caller, the same
// condition as an absent bridge, so it gets the actionable capability message.
// Any other bridge failure is a transport failure and must stay
// distinguishable from it.
func TestCovMCPBridgeFailuresUseOneErrorVocabulary(t *testing.T) {
	t.Run("an import failure is a capability error", func(t *testing.T) {
		bridge := &stubBridge{
			available: true,
			err:       errors.New("code-review-graph import failed: No module named 'crg'"),
		}
		srv := newTestServer(t, nil, bridge)
		text := covMCPErrorText(t, callTool(t, srv, "generate_wiki_tool", `{}`))
		covMCPRequireAll(t, text, "generate_wiki_tool", "markdown wiki",
			"No module named 'crg'", "Install code-review-graph=="+crgrelease.Version)
	})

	t.Run("any other failure is reported as a bridge failure", func(t *testing.T) {
		bridge := &stubBridge{available: true, err: errors.New("exit status 137")}
		srv := newTestServer(t, nil, bridge)
		text := covMCPErrorText(t, callTool(t, srv, "generate_wiki_tool", `{}`))
		covMCPRequireAll(t, text, "generate_wiki_tool",
			"failed in the retained code-review-graph "+crgrelease.Version+" bridge",
			"exit status 137")
		if strings.Contains(text, "Install code-review-graph==") {
			t.Errorf("a transport failure was reported as a missing install: %s", text)
		}
	})
}

// covMCPBridgeOnlyReasons is the fragment each bridge-only tool's reason must
// name. It is keyed by tool, so the set of keys IS the expected bridge-only
// tool set: a tool that silently changed legs fails on either half.
var covMCPBridgeOnlyReasons = map[string]string{
	"get_minimal_context_tool":     "--unified=0",
	"detect_changes_tool":          "--unified=0",
	"get_docs_section_tool":        "LLM-OPTIMIZED-REFERENCE.md",
	"get_suggested_questions_tool": "betweenness centrality",
	"get_bridge_nodes_tool":        "betweenness centrality",
	"embed_graph_tool":             "sentence-transformers",
	"generate_wiki_tool":           "markdown wiki",
	"get_wiki_page_tool":           "markdown wiki",
	"refactor_tool":                "refactor-preview store",
	"apply_refactor_tool":          "refactor-preview store",
	"list_repos_tool":              "registry.json",
	"cross_repo_search_tool":       "registry.json",
}

// The routing plan backs the `da kg` capability diagnostic, so the published
// split has to be an assertion rather than a comment: 18 tools answered
// natively for a fully indexed repository, 12 held on the bridge, each naming
// the upstream capability that holds it there.
func TestCovMCPRoutingPlanSplitsTheSurface(t *testing.T) {
	srv := newTestServer(t, &stubNative{fullyNative: true}, &stubBridge{available: true})
	plan, err := srv.RoutingPlan()
	if err != nil {
		t.Fatalf("routing plan: %v", err)
	}

	nativeCount, bridged := 0, map[string]string{}
	for _, routing := range plan {
		if routing.Backend == crgrelease.BackendNative {
			nativeCount++
			continue
		}
		bridged[routing.Tool] = routing.Reason
	}
	if nativeCount != 18 {
		t.Errorf("%d tools answered natively, want 18", nativeCount)
	}
	if len(bridged) != len(covMCPBridgeOnlyReasons) {
		t.Errorf("%d tools held on the bridge, want %d: %v",
			len(bridged), len(covMCPBridgeOnlyReasons), bridged)
	}
	for tool, fragment := range covMCPBridgeOnlyReasons {
		reason, ok := bridged[tool]
		if !ok {
			t.Errorf("%s is no longer bridge-only", tool)
			continue
		}
		if !strings.Contains(reason, fragment) {
			t.Errorf("%s bridge reason does not name %q: %s", tool, fragment, reason)
		}
	}
}

// Without a native backend the plan must hold every tool on the bridge and
// say that the native backend is not the selected one — a plan that still
// reported `native` would tell an operator the opposite of what a call does.
func TestCovMCPRoutingPlanWithoutANativeBackend(t *testing.T) {
	bridge := &stubBridge{available: true}
	srv := NewMCPServerWithProvider(t.TempDir(), nil, errBackendProbe)
	srv.WithToolBridge(bridge)

	plan, err := srv.RoutingPlan()
	if err != nil {
		t.Fatalf("routing plan: %v", err)
	}
	if len(plan) != len(crgrelease.Surface()) {
		t.Fatalf("plan covers %d tools, the release publishes %d",
			len(plan), len(crgrelease.Surface()))
	}
	for _, routing := range plan {
		if routing.Backend != crgrelease.BackendBridge {
			t.Fatalf("%s reported %s with no native backend", routing.Tool, routing.Backend)
		}
		if !routing.BridgeAvailable {
			t.Errorf("%s reported an unavailable bridge", routing.Tool)
		}
		want, bridgeOnly := covMCPBridgeOnlyReasons[routing.Tool]
		if bridgeOnly {
			if !strings.Contains(routing.Reason, want) {
				t.Errorf("%s reason does not name %q: %s", routing.Tool, want, routing.Reason)
			}
			continue
		}
		if !strings.Contains(routing.Reason, "is not the selected backend") {
			t.Errorf("%s reason does not name the absent native backend: %s",
				routing.Tool, routing.Reason)
		}
	}

	// An unavailable bridge is reported as such, so the diagnostic
	// distinguishes "held on the bridge" from "held on a bridge that cannot
	// run" — the second is unanswerable, the first is merely slow.
	srv.WithToolBridge(&stubBridge{})
	degraded, err := srv.RoutingPlan()
	if err != nil {
		t.Fatalf("routing plan: %v", err)
	}
	for _, routing := range degraded {
		if routing.BridgeAvailable {
			t.Fatalf("%s reported an available bridge", routing.Tool)
		}
	}
}

// encodeResult is the last step before the wire: an envelope that cannot be
// marshalled must fail loudly rather than reaching the client truncated.
func TestCovMCPEncodeResult(t *testing.T) {
	t.Run("a well-formed envelope round-trips", func(t *testing.T) {
		encoded, err := encodeResult(covMCPResult("native"))
		if err != nil {
			t.Fatalf("encodeResult: %v", err)
		}
		var decoded crgrelease.CallResult
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(decoded.StructuredContent) != `{"from":"native"}` {
			t.Errorf("structuredContent = %s", decoded.StructuredContent)
		}
	})

	t.Run("an unmarshallable envelope is an error", func(t *testing.T) {
		encoded, err := encodeResult(crgrelease.CallResult{
			StructuredContent: json.RawMessage(`{"truncated":`),
		})
		if err == nil {
			t.Fatalf("expected an encoding error, got %s", encoded)
		}
		if encoded != nil {
			t.Errorf("a failed encode returned a payload: %s", encoded)
		}
	})
}

// With no KG_HOME override the graph home is resolved under the user's home
// directory. The override case and the unresolvable-home hard failure are
// covered by TestDefaultKGHome_EnvOverride and
// TestDefaultKGHome_UnresolvableHomeHardFails; this is the ordinary path.
func TestCovMCPDefaultKGHomeUsesTheUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KG_HOME", "")
	t.Setenv("HOME", home)
	// os.UserHomeDir reads USERPROFILE on Windows.
	t.Setenv("USERPROFILE", home)

	want := filepath.Join(home, "knowledge-graph")
	if got := defaultKGHome(); got != want {
		t.Errorf("defaultKGHome() = %q, want %q", got, want)
	}
	if got := defaultGraphstoreDBPath(); got != filepath.Join(want, "ops", "graphstore.db") {
		t.Errorf("defaultGraphstoreDBPath() = %q", got)
	}
}
