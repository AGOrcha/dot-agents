package graphstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// releaseFixture reads one generated release-contract fixture.
func releaseFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "crg-release", "v"+crgrelease.Version, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read release fixture %s: %v", name, err)
	}
	return data
}

// serveRequests runs a batch of JSON-RPC requests through the server and
// returns the decoded responses, keyed by request id.
func serveRequests(t *testing.T, srv *MCPServer, requests ...string) map[float64]map[string]any {
	t.Helper()
	var in bytes.Buffer
	for _, request := range requests {
		in.WriteString(request)
		in.WriteString("\n")
	}
	var out bytes.Buffer
	if err := srv.Serve(&in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	responses := map[float64]map[string]any{}
	decoder := json.NewDecoder(&out)
	for decoder.More() {
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		id, _ := response["id"].(float64)
		responses[id] = response
	}
	return responses
}

// stubNative is a NativeToolSet whose answers and coverage verdict the test
// controls, so routing can be exercised without a built graph.
type stubNative struct {
	result      any
	err         error
	fullyNative bool
	nonNative   []string
	coverageErr error
	calls       []string
	args        []crgrelease.Args
}

func (s *stubNative) CallTool(name string, args crgrelease.Args) (any, error) {
	s.calls = append(s.calls, name)
	s.args = append(s.args, args)
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *stubNative) SourcesFullyNative() (bool, []string, error) {
	return s.fullyNative, s.nonNative, s.coverageErr
}

// stubBridge records what the retained release would have been asked.
type stubBridge struct {
	available   bool
	unavailable string
	result      crgrelease.CallResult
	err         error
	calls       []string
	args        []map[string]any
}

func (s *stubBridge) Available() bool     { return s.available }
func (s *stubBridge) Unavailable() string { return s.unavailable }
func (s *stubBridge) CallTool(name string, args map[string]any) (crgrelease.CallResult, error) {
	s.calls = append(s.calls, name)
	s.args = append(s.args, args)
	return s.result, s.err
}

func newTestServer(t *testing.T, native NativeToolSet, bridge ToolBridge) *MCPServer {
	t.Helper()
	srv := &MCPServer{workDir: t.TempDir()}
	if native != nil {
		srv.WithNativeToolSet(native)
	}
	srv.WithToolBridge(bridge)
	return srv
}

// The server must identify itself as the release does: clients gate features
// on the server name, protocol version and advertised capabilities, so a
// drop-in replacement that renames itself is not a drop-in.
func TestInitializeAdvertisesReleaseIdentity(t *testing.T) {
	srv := newTestServer(t, nil, &stubBridge{})
	responses := serveRequests(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	result, ok := responses[1]["result"].(map[string]any)
	if !ok {
		t.Fatalf("no initialize result: %#v", responses[1])
	}
	if got := result["protocolVersion"]; got != crgrelease.ProtocolVersion {
		t.Errorf("protocolVersion = %v, want %v", got, crgrelease.ProtocolVersion)
	}
	info, _ := result["serverInfo"].(map[string]any)
	if got := info["name"]; got != crgrelease.ServerName {
		t.Errorf("serverInfo.name = %v, want %v", got, crgrelease.ServerName)
	}
	if got := result["instructions"]; got != crgrelease.Instructions {
		t.Errorf("instructions differ from the release")
	}
	capabilities, _ := result["capabilities"].(map[string]any)
	for _, key := range []string{"experimental", "logging", "prompts", "resources", "tools"} {
		if _, ok := capabilities[key]; !ok {
			t.Errorf("capabilities missing %q", key)
		}
	}
}

// tools/list is the contract clients validate their calls against, so it must
// reproduce the release's inventory exactly — every tool, in order, with the
// published schema.
func TestToolsListMatchesRelease(t *testing.T) {
	srv := newTestServer(t, nil, &stubBridge{})
	responses := serveRequests(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result, ok := responses[1]["result"].(map[string]any)
	if !ok {
		t.Fatalf("no tools/list result: %#v", responses[1])
	}
	advertised, _ := result["tools"].([]any)

	var expected []map[string]any
	if err := json.Unmarshal(releaseFixture(t, "tools-list.json"), &expected); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(advertised) != len(expected) {
		t.Fatalf("advertised %d tools, the release publishes %d", len(advertised), len(expected))
	}
	for i, want := range expected {
		got, _ := advertised[i].(map[string]any)
		if got["name"] != want["name"] {
			t.Fatalf("tool %d: advertised %v, release publishes %v", i, got["name"], want["name"])
		}
		if got["description"] != want["description"] {
			t.Errorf("%v: description differs from the release", want["name"])
		}
		gotSchema, _ := json.Marshal(got["inputSchema"])
		wantSchema, _ := json.Marshal(want["inputSchema"])
		if !bytes.Equal(gotSchema, wantSchema) {
			t.Errorf("%v: input schema differs from the release\n got: %s\nwant: %s",
				want["name"], gotSchema, wantSchema)
		}
	}
}

// Prompts are part of the same server contract as tools.
func TestPromptsSurfaceMatchesRelease(t *testing.T) {
	srv := newTestServer(t, nil, &stubBridge{})
	responses := serveRequests(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"review_changes"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"review_changes","arguments":{"base":"origin/main"}}}`)

	list, _ := responses[1]["result"].(map[string]any)
	prompts, _ := list["prompts"].([]any)
	var expected []map[string]any
	if err := json.Unmarshal(releaseFixture(t, "prompts.json"), &expected); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(prompts) != len(expected) {
		t.Fatalf("advertised %d prompts, the release publishes %d", len(prompts), len(expected))
	}
	for i, want := range expected {
		got, _ := prompts[i].(map[string]any)
		if got["name"] != want["name"] {
			t.Errorf("prompt %d: advertised %v, release publishes %v", i, got["name"], want["name"])
		}
	}

	// With no arguments the release's own default rendering is returned...
	defaults, _ := responses[2]["result"].(map[string]any)
	defaultText := promptText(t, defaults)
	wantDefault := expected[0]["rendered_defaults"].([]any)[0].(map[string]any)
	wantText := wantDefault["content"].(map[string]any)["text"].(string)
	if defaultText != wantText {
		t.Errorf("default prompt rendering differs from the release")
	}

	// ...and a supplied argument is interpolated where the release puts it.
	rendered, _ := responses[3]["result"].(map[string]any)
	renderedText := promptText(t, rendered)
	if !strings.Contains(renderedText, "origin/main") {
		t.Errorf("supplied prompt argument not interpolated: %q", renderedText)
	}
	if strings.Contains(renderedText, "${arg:") {
		t.Errorf("prompt template placeholder leaked into the rendering")
	}
}

func promptText(t *testing.T, result map[string]any) string {
	t.Helper()
	messages, _ := result["messages"].([]any)
	if len(messages) == 0 {
		t.Fatalf("no prompt messages in %#v", result)
	}
	message, _ := messages[0].(map[string]any)
	content, _ := message["content"].(map[string]any)
	text, _ := content["text"].(string)
	return text
}

// Invalid arguments are reported the way the release reports them: a
// SUCCESSFUL JSON-RPC response whose result carries isError and the
// validator's message. A client that branches on JSON-RPC errors would never
// see these, so the distinction is load-bearing.
func TestToolsCallValidationMatchesRelease(t *testing.T) {
	fixtures := []string{
		"list_graph_stats_tool__invalid_unknown_argument",
		"query_graph_tool__invalid_missing_required",
		"get_impact_radius_tool__invalid_wrong_type",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			var fixture struct {
				Tool      string          `json:"tool"`
				Arguments json.RawMessage `json:"arguments"`
				Content   []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(
				releaseFixture(t, filepath.Join("calls", name+".json")), &fixture); err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			native := &stubNative{fullyNative: true, result: map[string]any{"status": "ok"}}
			bridge := &stubBridge{available: true}
			srv := newTestServer(t, native, bridge)

			request, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{
					"name":      fixture.Tool,
					"arguments": json.RawMessage(fixture.Arguments),
				},
			})
			responses := serveRequests(t, srv, string(request))
			if _, isRPCError := responses[1]["error"]; isRPCError {
				t.Fatalf("validation failure surfaced as a JSON-RPC error: %#v", responses[1])
			}
			result, _ := responses[1]["result"].(map[string]any)
			if isError, _ := result["isError"].(bool); !isError {
				t.Fatalf("expected isError, got %#v", result)
			}
			content, _ := result["content"].([]any)
			block, _ := content[0].(map[string]any)
			if got := block["text"]; got != fixture.Content[0].Text {
				t.Errorf("message differs from the release\n got: %v\nwant: %v",
					got, fixture.Content[0].Text)
			}
			if len(native.calls) != 0 || len(bridge.calls) != 0 {
				t.Errorf("an invalid call reached a backend: native=%v bridge=%v",
					native.calls, bridge.calls)
			}
		})
	}
}

func TestUnknownToolMatchesRelease(t *testing.T) {
	srv := newTestServer(t, &stubNative{fullyNative: true}, &stubBridge{available: true})
	responses := serveRequests(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope_tool","arguments":{}}}`)
	result, _ := responses[1]["result"].(map[string]any)
	if isError, _ := result["isError"].(bool); !isError {
		t.Fatalf("expected isError, got %#v", result)
	}
	content, _ := result["content"].([]any)
	block, _ := content[0].(map[string]any)
	if got := block["text"]; got != "Unknown tool: 'nope_tool'" {
		t.Errorf("unknown-tool message = %v", got)
	}
}

// A success carries BOTH the structured payload and its compact JSON
// rendering as a text block. Emitting only one is a visible protocol
// difference for clients that read either.
func TestSuccessEnvelopeCarriesStructuredAndText(t *testing.T) {
	native := &stubNative{
		fullyNative: true,
		result:      map[string]any{"status": "ok", "total_nodes": 16},
	}
	srv := newTestServer(t, native, &stubBridge{available: true})
	responses := serveRequests(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_graph_stats_tool","arguments":{}}}`)
	result, _ := responses[1]["result"].(map[string]any)
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("unexpected isError: %#v", result)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	if structured["status"] != "ok" {
		t.Errorf("structuredContent = %#v", structured)
	}
	content, _ := result["content"].([]any)
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	var fromText map[string]any
	if err := json.Unmarshal([]byte(text), &fromText); err != nil {
		t.Fatalf("content text is not the payload's JSON: %v", err)
	}
	structuredJSON, _ := json.Marshal(structured)
	textJSON, _ := json.Marshal(fromText)
	if !bytes.Equal(structuredJSON, textJSON) {
		t.Errorf("content text and structuredContent disagree\n text: %s\nstruct: %s",
			textJSON, structuredJSON)
	}
}

// Routing is the whole point of the Phase-A cutover, and the failure modes
// are asymmetric: answering natively from a graph that is missing files is a
// wrong answer that looks right.
func TestRoutingHonoursCapabilityAndSourceCoverage(t *testing.T) {
	bridgeResult := crgrelease.CallResult{
		Content:           []crgrelease.ContentBlock{{Type: "text", Text: `{"from":"bridge"}`}},
		StructuredContent: json.RawMessage(`{"from":"bridge"}`),
	}

	t.Run("native tool on a fully covered repository stays native", func(t *testing.T) {
		native := &stubNative{fullyNative: true, result: map[string]any{"from": "native"}}
		bridge := &stubBridge{available: true, result: bridgeResult}
		srv := newTestServer(t, native, bridge)
		callTool(t, srv, "list_graph_stats_tool", `{}`)
		if len(native.calls) != 1 || len(bridge.calls) != 0 {
			t.Fatalf("native=%v bridge=%v", native.calls, bridge.calls)
		}
	})

	t.Run("native tool on a partly unsupported repository routes to the bridge", func(t *testing.T) {
		native := &stubNative{fullyNative: false, nonNative: []string{"python", "typescript"}}
		bridge := &stubBridge{available: true, result: bridgeResult}
		srv := newTestServer(t, native, bridge)
		callTool(t, srv, "list_graph_stats_tool", `{}`)
		if len(native.calls) != 0 {
			t.Fatalf("a repository with unextracted sources was answered natively: %v", native.calls)
		}
		if len(bridge.calls) != 1 {
			t.Fatalf("bridge calls = %v", bridge.calls)
		}
	})

	t.Run("bridge-only tool never goes native", func(t *testing.T) {
		native := &stubNative{fullyNative: true, result: map[string]any{"from": "native"}}
		bridge := &stubBridge{available: true, result: bridgeResult}
		srv := newTestServer(t, native, bridge)
		callTool(t, srv, "embed_graph_tool", `{}`)
		if len(native.calls) != 0 {
			t.Fatalf("bridge-only tool answered natively: %v", native.calls)
		}
		if len(bridge.calls) != 1 {
			t.Fatalf("bridge calls = %v", bridge.calls)
		}
	})

	t.Run("an unknown source-coverage verdict routes to the bridge", func(t *testing.T) {
		native := &stubNative{coverageErr: errors.New("walk failed")}
		bridge := &stubBridge{available: true, result: bridgeResult}
		srv := newTestServer(t, native, bridge)
		callTool(t, srv, "list_graph_stats_tool", `{}`)
		if len(native.calls) != 0 {
			t.Fatalf("unknown coverage was answered natively: %v", native.calls)
		}
		if len(bridge.calls) != 1 {
			t.Fatalf("bridge calls = %v", bridge.calls)
		}
	})

	t.Run("bridge-routed calls receive the defaulted argument set", func(t *testing.T) {
		native := &stubNative{fullyNative: false, nonNative: []string{"python"}}
		bridge := &stubBridge{available: true, result: bridgeResult}
		srv := newTestServer(t, native, bridge)
		callTool(t, srv, "list_flows_tool", `{"limit":3}`)
		if len(bridge.args) != 1 {
			t.Fatalf("bridge args = %v", bridge.args)
		}
		forwarded := bridge.args[0]
		if forwarded["sort_by"] != "criticality" {
			t.Errorf("published default not forwarded: %#v", forwarded)
		}
		if forwarded["detail_level"] != "standard" {
			t.Errorf("published default not forwarded: %#v", forwarded)
		}
		if forwarded["limit"] != int64(3) {
			t.Errorf("caller value not forwarded: %#v", forwarded)
		}
	})
}

// An absent bridge must produce an explicit capability error naming what is
// missing. An empty success would be indistinguishable from "the graph really
// is empty", which is exactly the silent regression this cutover must avoid.
func TestBridgeUnavailableReportsTheMissingCapability(t *testing.T) {
	native := &stubNative{fullyNative: true}
	bridge := &stubBridge{available: false, unavailable: "code-review-graph not found on PATH"}
	srv := newTestServer(t, native, bridge)
	result := callTool(t, srv, "embed_graph_tool", `{}`)
	if isError, _ := result["isError"].(bool); !isError {
		t.Fatalf("expected an explicit failure, got %#v", result)
	}
	content, _ := result["content"].([]any)
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	for _, want := range []string{
		"embed_graph_tool",
		"embedding",
		"code-review-graph not found on PATH",
		crgrelease.Version,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("capability error does not mention %q: %s", want, text)
		}
	}
}

// A native handler that declines a tool must fall through to the bridge, not
// fabricate a result.
func TestNativeDeclineFallsThroughToBridge(t *testing.T) {
	native := &stubNative{fullyNative: true, err: ErrToolNotNative}
	bridge := &stubBridge{
		available: true,
		result: crgrelease.CallResult{
			Content:           []crgrelease.ContentBlock{{Type: "text", Text: `{"from":"bridge"}`}},
			StructuredContent: json.RawMessage(`{"from":"bridge"}`),
		},
	}
	srv := newTestServer(t, native, bridge)
	result := callTool(t, srv, "list_graph_stats_tool", `{}`)
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("unexpected failure: %#v", result)
	}
	if len(bridge.calls) != 1 {
		t.Fatalf("bridge was not consulted: %v", bridge.calls)
	}
}

// The routing plan is the diagnostic behind `da kg`'s capability report, so it
// must agree with what the server actually does.
func TestRoutingPlanCoversEveryPublishedTool(t *testing.T) {
	native := &stubNative{fullyNative: true}
	srv := newTestServer(t, native, &stubBridge{available: true})
	plan, err := srv.RoutingPlan()
	if err != nil {
		t.Fatalf("routing plan: %v", err)
	}
	if len(plan) != len(crgrelease.Surface()) {
		t.Fatalf("plan covers %d tools, the release publishes %d",
			len(plan), len(crgrelease.Surface()))
	}
	for _, routing := range plan {
		capability, _ := crgrelease.Capability(routing.Tool)
		wantBackend := crgrelease.BackendNative
		if !capability.NativeBackend {
			wantBackend = crgrelease.BackendBridge
		}
		if routing.Backend != wantBackend {
			t.Errorf("%s routed to %s, want %s", routing.Tool, routing.Backend, wantBackend)
		}
		if routing.Backend == crgrelease.BackendBridge && routing.Reason == "" {
			t.Errorf("%s routed to the bridge with no reason", routing.Tool)
		}
	}

	// A repository the native scanner does not fully cover moves every tool
	// to the bridge, and says which languages forced it.
	partial := newTestServer(t,
		&stubNative{fullyNative: false, nonNative: []string{"ruby"}},
		&stubBridge{available: true})
	partialPlan, err := partial.RoutingPlan()
	if err != nil {
		t.Fatalf("routing plan: %v", err)
	}
	for _, routing := range partialPlan {
		if routing.Backend != crgrelease.BackendBridge {
			t.Fatalf("%s stayed native for a partly unsupported repository", routing.Tool)
		}
	}
	if !strings.Contains(partialPlan[0].Reason, "ruby") {
		t.Errorf("routing reason does not name the unsupported language: %s", partialPlan[0].Reason)
	}
}

func callTool(t *testing.T, srv *MCPServer, name, arguments string) map[string]any {
	t.Helper()
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name +
		`","arguments":` + arguments + `}}`
	responses := serveRequests(t, srv, request)
	result, ok := responses[1]["result"].(map[string]any)
	if !ok {
		t.Fatalf("no tools/call result: %#v", responses[1])
	}
	return result
}

// An exception escaping a native handler must be rendered the way the release
// renders one: the uniform `Error calling tool '<name>': ` prefix, isError,
// and NO structured content.
//
// The distinction from a tool's in-band error payload is observable and easy
// to get wrong. The release validates result bounds BEFORE its try block, so
// `max_results: 0` escapes as an exception rather than returning a
// `status: "error"` payload — a handler that wrapped it would answer
// isError:false with a populated body, both halves wrong while every field
// inside looked plausible.
func TestHandlerExceptionMatchesReleaseEnvelope(t *testing.T) {
	native := &stubNative{
		fullyNative: true,
		err:         errors.New("limit must be an integer greater than or equal to 1"),
	}
	srv := newTestServer(t, native, &stubBridge{available: true})
	result := callTool(t, srv, "list_flows_tool", `{"limit":0}`)

	if isError, _ := result["isError"].(bool); !isError {
		t.Fatalf("expected isError, got %#v", result)
	}
	if _, present := result["structuredContent"]; present {
		t.Error("an escaped exception must carry no structured content")
	}
	content, _ := result["content"].([]any)
	block, _ := content[0].(map[string]any)
	want := "Error calling tool 'list_flows_tool': " +
		"limit must be an integer greater than or equal to 1"
	if got := block["text"]; got != want {
		t.Errorf("exception text\n got: %v\nwant: %v", got, want)
	}
}

// The same envelope, pinned against the release's own recorded output rather
// than against our formatting of it.
func TestHandlerExceptionMatchesRecordedReleaseText(t *testing.T) {
	var fixture struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		IsError   bool            `json:"is_error"`
		Content   []struct {
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structured_content"`
	}
	raw, err := os.ReadFile(filepath.Join(
		"..", "..", "testdata", "crg-release", "v"+crgrelease.Version,
		"calls", "list_flows_tool__bound_limit.json"))
	if err != nil {
		t.Skipf("bound-violation fixture not generated yet: %v", err)
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if !fixture.IsError {
		t.Fatal("fixture is not an error case")
	}
	if len(fixture.Structured) != 0 {
		t.Fatal("fixture unexpectedly carries structured content")
	}

	// Strip the release's prefix to recover the handler-visible message, then
	// prove the server puts it back identically.
	prefix := "Error calling tool '" + fixture.Tool + "': "
	message := strings.TrimPrefix(fixture.Content[0].Text, prefix)
	if message == fixture.Content[0].Text {
		t.Fatalf("recorded text does not carry the release prefix: %q", fixture.Content[0].Text)
	}

	native := &stubNative{fullyNative: true, err: errors.New(message)}
	srv := newTestServer(t, native, &stubBridge{available: true})
	request, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name": fixture.Tool, "arguments": json.RawMessage(fixture.Arguments),
		},
	})
	responses := serveRequests(t, srv, string(request))
	result, _ := responses[1]["result"].(map[string]any)
	content, _ := result["content"].([]any)
	block, _ := content[0].(map[string]any)
	if got := block["text"]; got != fixture.Content[0].Text {
		t.Errorf("exception envelope differs from the release\n got: %v\nwant: %v",
			got, fixture.Content[0].Text)
	}
}
