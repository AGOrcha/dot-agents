package crgrelease

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// covEnvPayload is a fixed-order structured payload, so the compact JSON
// rendering a client reads out of the content block is fully determined.
type covEnvPayload struct {
	Query string `json:"query"`
	Hits  []int  `json:"hits"`
}

// covEnvWire marshals an envelope the way a JSON-RPC transport would, which is
// where `isError` and the optional `structuredContent` become observable.
func covEnvWire(t *testing.T, result CallResult) string {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return string(encoded)
}

// covEnvText returns the single text block a release envelope always carries.
func covEnvText(t *testing.T, result CallResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("envelope carries %d content blocks, release emits exactly 1", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Fatalf("content block type %q, release emits only %q", result.Content[0].Type, "text")
	}
	return result.Content[0].Text
}

// A success carries BOTH the structured dict and a text block holding the
// compact JSON rendering of that same dict, with isError:false on the wire.
func TestCovEnvSuccessResultCarriesTextAndStructuredContent(t *testing.T) {
	result, err := SuccessResult(covEnvPayload{Query: "main", Hits: []int{1}})
	if err != nil {
		t.Fatalf("success result: %v", err)
	}

	const wantText = `{"query":"main","hits":[1]}`
	if got := covEnvText(t, result); got != wantText {
		t.Errorf("content text = %s, want %s", got, wantText)
	}
	if got := string(result.StructuredContent); got != wantText {
		t.Errorf("structuredContent = %s, want %s", got, wantText)
	}
	if result.IsError {
		t.Error("success envelope reports isError:true")
	}

	const wantWire = `{"content":[{"type":"text","text":"{\"query\":\"main\",\"hits\":[1]}"}],` +
		`"structuredContent":{"query":"main","hits":[1]},"isError":false}`
	if got := covEnvWire(t, result); got != wantWire {
		t.Errorf("wire envelope =\n %s\nwant\n %s", got, wantWire)
	}
}

// The rendering is the literal text a client reads, so both of its two
// non-default properties are contract: compact separators (no spaces after
// `:`/`,`), and no HTML escaping of <, > or &.
func TestCovEnvSuccessResultRendersCompactUnescapedJSON(t *testing.T) {
	result, err := SuccessResult(covEnvPayload{Query: `a<b> & "c"`, Hits: []int{1, 2}})
	if err != nil {
		t.Fatalf("success result: %v", err)
	}

	wantText := `{"query":"a<b> & \"c\"","hits":[1,2]}`
	got := covEnvText(t, result)
	if got != wantText {
		t.Fatalf("content text = %s, want %s", got, wantText)
	}
	if strings.Contains(got, `\u003c`) || strings.Contains(got, `\u0026`) {
		t.Errorf("content text HTML-escaped: %s", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Error("content text keeps the encoder's trailing newline")
	}
	if strings.Contains(got, `", "`) || strings.Contains(got, `": `) {
		t.Errorf("content text uses non-compact separators: %s", got)
	}
}

// An unencodable payload is a caller error, not an envelope: SuccessResult
// must surface the wrapped encoder failure and no half-built result.
func TestCovEnvSuccessResultPropagatesEncodeFailure(t *testing.T) {
	result, err := SuccessResult(map[string]any{"chan": make(chan int)})
	if err == nil {
		t.Fatalf("encoding a channel succeeded, envelope = %+v", result)
	}
	if !strings.HasPrefix(err.Error(), "crgrelease: encode tool result: ") {
		t.Errorf("error = %q, want the crgrelease encode prefix", err.Error())
	}
	var unsupported *json.UnsupportedTypeError
	if !errors.As(err, &unsupported) {
		t.Errorf("error %q does not wrap *json.UnsupportedTypeError", err.Error())
	}
	if result.Content != nil || result.StructuredContent != nil || result.IsError {
		t.Errorf("failed encode returned a populated envelope: %+v", result)
	}
}

// A tool failure is a SUCCESSFUL response carrying isError:true, the message
// in one text block, and no structured content at all.
func TestCovEnvErrorResultIsSuccessfulResponseWithIsError(t *testing.T) {
	const message = "max_results must be at least 1"
	result := ErrorResult(message)

	if got := covEnvText(t, result); got != message {
		t.Errorf("content text = %q, want %q", got, message)
	}
	if !result.IsError {
		t.Error("failure envelope reports isError:false")
	}
	if result.StructuredContent != nil {
		t.Errorf("failure envelope carries structuredContent %s", result.StructuredContent)
	}

	const wantWire = `{"content":[{"type":"text","text":"max_results must be at least 1"}],` +
		`"isError":true}`
	if got := covEnvWire(t, result); got != wantWire {
		t.Errorf("wire envelope =\n %s\nwant\n %s", got, wantWire)
	}
}

// An exception escaping a tool gets the release's uniform prefix, the
// exception's own message, and stays free of structured content.
func TestCovEnvToolExceptionResultUsesReleasePrefix(t *testing.T) {
	cases := []struct {
		name string
		tool string
		err  error
		want string
	}{
		{
			name: "bare error",
			tool: "get_hub_nodes_tool",
			err:  errors.New("max_results must be >= 1"),
			want: "Error calling tool 'get_hub_nodes_tool': max_results must be >= 1",
		},
		{
			name: "wrapped error reports the full chain",
			tool: "query_graph_tool",
			err:  fmt.Errorf("open graph: %w", errors.New("no such file")),
			want: "Error calling tool 'query_graph_tool': open graph: no such file",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ToolExceptionResult(testCase.tool, testCase.err)
			if got := covEnvText(t, result); got != testCase.want {
				t.Errorf("exception text = %q, want %q", got, testCase.want)
			}
			if !result.IsError {
				t.Error("exception envelope reports isError:false")
			}
			if result.StructuredContent != nil {
				t.Errorf("exception envelope carries structuredContent %s", result.StructuredContent)
			}
		})
	}
}

// covEnvSwapReleaseJSON replaces the embedded release descriptor for one test.
// The decode-failure branch is only reachable through the embedded bytes, and
// top-level serial tests never overlap this package's parallel ones.
func covEnvSwapReleaseJSON(t *testing.T, data []byte) {
	t.Helper()
	original := releaseJSON
	releaseJSON = data
	t.Cleanup(func() { releaseJSON = original })
}

// Metadata maps every generated field, including fastmcp_version, which no
// exported constant mirrors.
func TestCovEnvMetadataMapsGeneratedDescriptor(t *testing.T) {
	meta, err := Metadata()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	want := Release{
		Version:         "2.3.8",
		TagCommit:       TagCommit,
		SchemaVersion:   9,
		FastMCPVersion:  "3.4.7",
		ProtocolVersion: "2025-06-18",
		ToolCount:       30,
	}
	if meta != want {
		t.Errorf("metadata = %+v, want %+v", meta, want)
	}
}

// A corrupt descriptor must be reported, not silently answered with a zero
// release that would claim schema version 0 and no tools.
func TestCovEnvMetadataReportsDecodeFailure(t *testing.T) {
	covEnvSwapReleaseJSON(t, []byte(`{"version":`))

	meta, err := Metadata()
	if err == nil {
		t.Fatalf("decoding a truncated descriptor succeeded, metadata = %+v", meta)
	}
	if !strings.HasPrefix(err.Error(), "crgrelease: decode release metadata: ") {
		t.Errorf("error = %q, want the crgrelease decode prefix", err.Error())
	}
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Errorf("error %q does not wrap *json.SyntaxError", err.Error())
	}
	if meta != (Release{}) {
		t.Errorf("failed decode returned %+v, want the zero Release", meta)
	}
}

// covEnvSwapCapabilities installs a mutated copy of the routing table so the
// table-versus-surface consistency failures are observable.
func covEnvSwapCapabilities(t *testing.T, mutate func(map[string]ToolCapability)) {
	t.Helper()
	original := toolCapabilities
	replacement := make(map[string]ToolCapability, len(original)+1)
	for name, capability := range original {
		replacement[name] = capability
	}
	mutate(replacement)
	toolCapabilities = replacement
	t.Cleanup(func() { toolCapabilities = original })
}

// Capability answers with the tool's own name attached, and reports a miss
// rather than defaulting an unknown tool onto a backend.
func TestCovEnvCapabilityLooksUpPublishedTools(t *testing.T) {
	native, ok := Capability("query_graph_tool")
	if !ok {
		t.Fatal("query_graph_tool has no routing decision")
	}
	if native.Tool != "query_graph_tool" || !native.NativeBackend ||
		native.BridgeOnlyReason != "" || native.Backend() != BackendNative {
		t.Errorf("query_graph_tool = %+v, want a native decision", native)
	}
	if toolCapabilities["query_graph_tool"].Tool != "" {
		t.Error("Capability wrote the tool name back into the shared routing table")
	}

	bridge, ok := Capability("embed_graph_tool")
	if !ok {
		t.Fatal("embed_graph_tool has no routing decision")
	}
	if bridge.Tool != "embed_graph_tool" || bridge.NativeBackend ||
		bridge.Backend() != BackendBridge {
		t.Errorf("embed_graph_tool = %+v, want a bridge decision", bridge)
	}
	if !strings.Contains(bridge.BridgeOnlyReason, "no embedding pipeline") {
		t.Errorf("embed_graph_tool reason = %q, want the embedding-pipeline reason",
			bridge.BridgeOnlyReason)
	}
}

func TestCovEnvCapabilityReportsUnknownTool(t *testing.T) {
	for _, name := range []string{"", "no_such_tool", "query_graph"} {
		capability, ok := Capability(name)
		if ok {
			t.Errorf("Capability(%q) resolved to %+v, want a miss", name, capability)
		}
		if capability != (ToolCapability{}) {
			t.Errorf("Capability(%q) = %+v, want the zero decision", name, capability)
		}
	}
}

// The cutover's headline claim: of the 30 published tools, 18 are served
// natively and 12 stay on the bridge, each with a named reason, in the
// release's published order.
func TestCovEnvCapabilitiesSplitMatchesPublishedSurface(t *testing.T) {
	capabilities, err := Capabilities()
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	tools := Surface()
	if len(capabilities) != len(tools) {
		t.Fatalf("%d decisions for %d published tools", len(capabilities), len(tools))
	}

	native, bridge := 0, 0
	for i, capability := range capabilities {
		if capability.Tool != tools[i].Name {
			t.Fatalf("decision %d is %q, published order has %q",
				i, capability.Tool, tools[i].Name)
		}
		if capability.NativeBackend {
			native++
			continue
		}
		bridge++
		if capability.BridgeOnlyReason == "" {
			t.Errorf("%s: bridge-only with no named reason", capability.Tool)
		}
	}
	if native != 18 || bridge != 12 {
		t.Errorf("split = %d native / %d bridge, want 18 native / 12 bridge", native, bridge)
	}
}

// Adding a tool to the release without deciding where it is served must be a
// hard failure, not a silent default onto either backend.
func TestCovEnvCapabilitiesFailWhenPublishedToolHasNoDecision(t *testing.T) {
	covEnvSwapCapabilities(t, func(table map[string]ToolCapability) {
		delete(table, "get_flow_tool")
	})

	capabilities, err := Capabilities()
	if err == nil {
		t.Fatalf("undecided tool accepted, got %d decisions", len(capabilities))
	}
	if capabilities != nil {
		t.Errorf("failed lookup returned %d decisions, want none", len(capabilities))
	}
	const want = `crgrelease: tool "get_flow_tool" has no routing decision; ` +
		`every published tool must be assigned a backend`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// A decision must be exactly one of native or bridge-with-a-reason: both and
// neither are contradictions the table must reject.
func TestCovEnvCapabilitiesFailOnContradictoryDecision(t *testing.T) {
	cases := []struct {
		name       string
		capability ToolCapability
	}{
		{"native with a bridge reason", ToolCapability{NativeBackend: true, BridgeOnlyReason: "why"}},
		{"bridge-only with no reason", ToolCapability{}},
	}
	const want = `crgrelease: tool "get_flow_tool" must be either native or bridge-only with a reason`
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			covEnvSwapCapabilities(t, func(table map[string]ToolCapability) {
				table["get_flow_tool"] = testCase.capability
			})
			capabilities, err := Capabilities()
			if err == nil {
				t.Fatalf("contradictory decision accepted, got %d decisions", len(capabilities))
			}
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err.Error(), want)
			}
		})
	}
}

// A routing decision for a tool the release does not publish is equally a
// drift signal: the counts must agree, not merely cover the surface.
func TestCovEnvCapabilitiesFailOnUnpublishedDecision(t *testing.T) {
	covEnvSwapCapabilities(t, func(table map[string]ToolCapability) {
		table["covenv_retired_tool"] = ToolCapability{NativeBackend: true}
	})

	capabilities, err := Capabilities()
	if err == nil {
		t.Fatalf("unpublished decision accepted, got %d decisions", len(capabilities))
	}
	want := fmt.Sprintf("crgrelease: %d routing decisions for %d published tools",
		len(Surface())+1, len(Surface()))
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// A bridge-only tool with no bridge installed must fail explicitly, naming the
// missing capability and the exact release to install — never answer empty.
func TestCovEnvBridgeUnavailableMessageNamesCapabilityAndRelease(t *testing.T) {
	const head = "get_bridge_nodes_tool is not available from the kg-native code-graph " +
		"backend: upstream ranks by betweenness centrality"
	const tail = ". Install code-review-graph==2.3.8 " +
		"(or select the crg-bridge backend) to use this tool."
	const served = ". It is served by the retained code-review-graph 2.3.8 bridge, " +
		"which is not available here"

	cases := []struct {
		name   string
		detail string
		want   string
	}{
		{"no detail", "", head + served + tail},
		{"detail explains why", "python3 not found on PATH",
			head + served + ": python3 not found on PATH" + tail},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := BridgeUnavailableMessage("get_bridge_nodes_tool",
				"upstream ranks by betweenness centrality", testCase.detail)
			if got != testCase.want {
				t.Errorf("message =\n %q\nwant\n %q", got, testCase.want)
			}
		})
	}
}

// The sources-unsupported reason has to name the offending languages, because
// the routing decision it justifies is per repository, not per tool.
func TestCovEnvSourcesUnsupportedReasonListsLanguages(t *testing.T) {
	const prefix = "the repository contains sources the kg-native scanner does not extract ("
	const suffix = "), so a native answer would be computed from an incomplete graph"

	cases := []struct {
		name      string
		languages []string
		want      string
	}{
		{"several languages", []string{"kotlin", "scala"}, prefix + "[kotlin scala]" + suffix},
		{"no languages", nil, prefix + "[]" + suffix},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SourcesUnsupportedReason(testCase.languages); got != testCase.want {
				t.Errorf("reason =\n %q\nwant\n %q", got, testCase.want)
			}
		})
	}
}
