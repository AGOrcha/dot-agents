package crgrelease

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
)

// covSurfPublishedToolCount pins the size of the pinned release's inventory.
// A tool appearing or disappearing changes what the server advertises, so it
// must be a deliberate contract edit rather than a silent fixture refresh.
const covSurfPublishedToolCount = 30

// covSurfRecover runs fn and returns the value it panicked with, or nil.
func covSurfRecover(t *testing.T, fn func()) (recovered any) {
	t.Helper()
	defer func() { recovered = recover() }()
	fn()
	return nil
}

// covSurfSameStrings compares string slices treating nil and empty as equal,
// which is what "this parameter accepts no types" means.
func covSurfSameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The advertised inventory, and the two answers Lookup owes the server: a
// published tool resolves to its own descriptor, an unpublished name must miss
// so the caller can emit upstream's `Unknown tool` result instead of a schema
// error.
func TestCovSurfPublishedInventory(t *testing.T) {
	tools := Surface()
	if len(tools) != covSurfPublishedToolCount {
		t.Fatalf("surface publishes %d tools, want %d", len(tools), covSurfPublishedToolCount)
	}
	if tools[0].Name != "build_or_update_graph_tool" {
		t.Errorf("published order changed: first tool is %q", tools[0].Name)
	}

	names := ToolNames()
	if len(names) != len(tools) {
		t.Fatalf("ToolNames returned %d names for %d tools", len(names), len(tools))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("ToolNames is not sorted: %v", names)
	}
	ordered := make([]string, 0, len(tools))
	for _, tool := range tools {
		ordered = append(ordered, tool.Name)
	}
	sort.Strings(ordered)
	if !covSurfSameStrings(names, ordered) {
		t.Errorf("ToolNames %v does not match the surface names %v", names, ordered)
	}

	for _, name := range names {
		tool, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) missed a published tool", name)
		}
		if tool.Name != name {
			t.Errorf("Lookup(%q) returned tool %q", name, tool.Name)
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("Lookup(%q) returned an empty input schema", name)
		}
	}
	if tool, ok := Lookup("get_minimal_context_tool_v2"); ok {
		t.Errorf("Lookup reported unpublished tool as published: %q", tool.Name)
	}
}

// A missing embedded contract file means the binary cannot answer tools/list
// at all; failing at init with a named file beats serving an empty inventory.
func TestCovSurfMustContractRejectsMissingFile(t *testing.T) {
	if got := mustContract("contract/" + Version + "/release.json"); len(got) == 0 {
		t.Fatal("mustContract returned no bytes for an embedded contract file")
	}
	recovered := covSurfRecover(t, func() {
		mustContract("contract/" + Version + "/not-shipped.json")
	})
	message, ok := recovered.(string)
	if !ok {
		t.Fatalf("mustContract recovered %#v, want a panic message", recovered)
	}
	if !strings.Contains(message, "not-shipped.json") ||
		!strings.Contains(message, "embedded contract") {
		t.Errorf("panic message does not name the missing contract: %q", message)
	}
}

// covSurfResetSurface drops the memoised surface so the next loadSurface call
// re-decodes toolsListJSON.
func covSurfResetSurface() {
	surfaceOnce = sync.Once{}
	surfaceTools, surfaceIndex, surfaceErr = nil, nil, nil
}

// covSurfSwapToolsList installs a synthetic tools-list document for the
// duration of the test and restores the real, decoded surface afterwards.
// Tests using it must stay sequential (no t.Parallel) because the surface is
// package-level state.
func covSurfSwapToolsList(t *testing.T, raw string) {
	t.Helper()
	original := toolsListJSON
	t.Cleanup(func() {
		toolsListJSON = original
		covSurfResetSurface()
		loadSurface()
		if surfaceErr != nil {
			t.Fatalf("restoring the real surface failed: %v", surfaceErr)
		}
	})
	toolsListJSON = []byte(raw)
	covSurfResetSurface()
}

// covSurfAssertLoadFails installs a broken contract and asserts that every
// accessor refuses to serve a half-decoded surface.
func covSurfAssertLoadFails(t *testing.T, raw, want string) {
	t.Helper()
	covSurfSwapToolsList(t, raw)
	accessors := []struct {
		label string
		call  func()
	}{
		{"Surface", func() { Surface() }},
		{"Lookup", func() { Lookup("build_or_update_graph_tool") }},
		{"ToolNames", func() { ToolNames() }},
	}
	for _, accessor := range accessors {
		recovered := covSurfRecover(t, accessor.call)
		err, ok := recovered.(error)
		if !ok {
			t.Fatalf("%s recovered %#v, want an error", accessor.label, recovered)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s panicked with %q, want it to mention %q", accessor.label, err, want)
		}
	}
}

// A contract the binary cannot decode must take the accessors down loudly
// rather than let the server advertise an empty or partial tool list.
func TestCovSurfBrokenContractFailsLoud(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "tools list is not an array",
			raw:  `{"tools": []}`,
			want: "crgrelease: decode tools-list",
		},
		{
			name: "a tool fails to decode",
			raw: `[{"name": "open_tool", "description": "d",
			  "inputSchema": {"type": "object", "properties": {}},
			  "parameter_order": []}]`,
			want: "crgrelease: open_tool input schema does not set additionalProperties:false",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			covSurfAssertLoadFails(t, tc.raw, tc.want)
		})
	}
}

// covSurfSchema is a synthetic tool descriptor exercising every shape the
// release's generated schemas use.
const covSurfSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["repo"],
  "properties": {
    "repo": {"type": "string", "description": "repository root"},
    "limit": {"anyOf": [{"type": "integer"}, {"type": "null"}], "default": 10},
    "tags": {"type": "array", "items": {"anyOf": [{"type": "string"}, {"type": "null"}]},
             "default": null},
    "mode": {"type": ["string", "boolean"]},
    "budget": {"type": 7}
  }
}`

// covSurfDecodeFixture decodes the synthetic descriptor above.
func covSurfDecodeFixture(t *testing.T) Tool {
	t.Helper()
	tool, err := decodeTool(toolDoc{
		Name:           "covsurf_tool",
		Description:    "synthetic",
		InputSchema:    json.RawMessage(covSurfSchema),
		ParameterOrder: []string{"repo", "limit", "tags", "mode", "budget"},
	})
	if err != nil {
		t.Fatalf("decode synthetic tool: %v", err)
	}
	return tool
}

// The decoded descriptor is what argument binding validates against, so the
// schema's required set, declaration order and verbatim schema bytes all have
// to survive decoding.
func TestCovSurfDecodeToolShape(t *testing.T) {
	tool := covSurfDecodeFixture(t)
	if tool.Name != "covsurf_tool" || tool.Description != "synthetic" {
		t.Errorf("decoded %q/%q, want covsurf_tool/synthetic", tool.Name, tool.Description)
	}
	if string(tool.InputSchema) != covSurfSchema {
		t.Error("input schema bytes were not preserved verbatim")
	}
	if !covSurfSameStrings(tool.Required, []string{"repo"}) {
		t.Errorf("required = %v, want [repo]", tool.Required)
	}
	if !covSurfSameStrings(tool.order, []string{"repo", "limit", "tags", "mode", "budget"}) {
		t.Errorf("declaration order = %v, want the parameter_order", tool.order)
	}
	if len(tool.Params) != 5 {
		t.Fatalf("decoded %d params, want 5", len(tool.Params))
	}
}

// covSurfParamWant is the decoded form one property must take.
type covSurfParamWant struct {
	types      []string
	itemTypes  []string
	optional   bool
	hasDefault bool
	value      any
	desc       string
}

// Each property spelling in the release's schemas has to decode to a distinct,
// meaningful parameter: a bare type, the anyOf-with-null spelling of an
// optional with a default, an explicit null default, a multi-type union, and a
// type keyword the decoder does not understand (which accepts nothing rather
// than everything).
func TestCovSurfDecodeToolParams(t *testing.T) {
	tool := covSurfDecodeFixture(t)
	wants := map[string]covSurfParamWant{
		"repo":   {types: []string{"string"}, desc: "repository root"},
		"limit":  {types: []string{"integer"}, optional: true, hasDefault: true, value: float64(10)},
		"tags":   {types: []string{"array"}, itemTypes: []string{"string"}, hasDefault: true},
		"mode":   {types: []string{"string", "boolean"}},
		"budget": {},
	}
	for name, want := range wants {
		t.Run(name, func(t *testing.T) {
			covSurfCheckParam(t, tool.Params[name], name, want)
		})
	}
}

func covSurfCheckParam(t *testing.T, got Param, name string, want covSurfParamWant) {
	t.Helper()
	if got.Name != name {
		t.Errorf("param name = %q, want %q", got.Name, name)
	}
	if !covSurfSameStrings(got.Types, want.types) {
		t.Errorf("types = %v, want %v", got.Types, want.types)
	}
	if !covSurfSameStrings(got.ItemTypes, want.itemTypes) {
		t.Errorf("item types = %v, want %v", got.ItemTypes, want.itemTypes)
	}
	if got.Optional != want.optional {
		t.Errorf("optional = %v, want %v", got.Optional, want.optional)
	}
	if got.HasDefault != want.hasDefault {
		t.Errorf("hasDefault = %v, want %v", got.HasDefault, want.hasDefault)
	}
	if got.Default != want.value {
		t.Errorf("default = %#v, want %#v", got.Default, want.value)
	}
	if got.Description != want.desc {
		t.Errorf("description = %q, want %q", got.Description, want.desc)
	}
}

// A contract that cannot be trusted must be rejected with a message naming the
// tool and the defect; each of these would otherwise make the server accept
// arguments upstream rejects or report field errors in the wrong order.
func TestCovSurfDecodeToolRejects(t *testing.T) {
	cases := []struct {
		name string
		doc  toolDoc
		want string
	}{
		{
			name: "unparseable input schema",
			doc:  toolDoc{Name: "a", InputSchema: json.RawMessage(`{"type":`)},
			want: "crgrelease: decode a input schema:",
		},
		{
			name: "additionalProperties absent",
			doc:  toolDoc{Name: "b", InputSchema: json.RawMessage(`{"type":"object"}`)},
			want: "crgrelease: b input schema does not set additionalProperties:false",
		},
		{
			name: "additionalProperties open",
			doc: toolDoc{Name: "c", InputSchema: json.RawMessage(
				`{"type":"object","additionalProperties":true}`)},
			want: "crgrelease: c input schema does not set additionalProperties:false",
		},
		{
			name: "property is not a schema object",
			doc: toolDoc{Name: "d", InputSchema: json.RawMessage(
				`{"additionalProperties":false,"properties":{"p":"string"}}`)},
			want: "crgrelease: decode d.p:",
		},
		{
			name: "declared order omits a property",
			doc: toolDoc{Name: "e", InputSchema: json.RawMessage(
				`{"additionalProperties":false,"properties":{"p":{"type":"string"}}}`)},
			want: "crgrelease: e declares 0 parameters but the schema has 1",
		},
		{
			name: "declared order names an unknown property",
			doc: toolDoc{
				Name: "f",
				InputSchema: json.RawMessage(
					`{"additionalProperties":false,"properties":{"p":{"type":"string"}}}`),
				ParameterOrder: []string{"q"},
			},
			want: `crgrelease: f declares parameter "q" with no schema property`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, err := decodeTool(tc.doc)
			if err == nil {
				t.Fatalf("decoded %#v without error", tool)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
			if tool.Name != "" || tool.Params != nil {
				t.Errorf("a rejected tool must decode to the zero Tool, got %#v", tool)
			}
		})
	}
}

// flattenTypes decides both which JSON types an argument may take and whether
// null means "unset" instead of "invalid", so every type spelling upstream
// emits has to reduce correctly.
func TestCovSurfFlattenTypes(t *testing.T) {
	cases := []struct {
		name     string
		prop     string
		types    []string
		optional bool
	}{
		{name: "bare type", prop: `{"type":"string"}`, types: []string{"string"}},
		{name: "null only", prop: `{"type":"null"}`, optional: true},
		{
			name:     "type list with null and a non-string entry",
			prop:     `{"type":["string","null",7]}`,
			types:    []string{"string"},
			optional: true,
		},
		{
			name:     "optional union",
			prop:     `{"anyOf":[{"type":"integer"},{"type":"null"}]}`,
			types:    []string{"integer"},
			optional: true,
		},
		{
			name:  "nested union keeps every member",
			prop:  `{"anyOf":[{"anyOf":[{"type":"string"},{"type":"array"}]},{"type":"object"}]}`,
			types: []string{"string", "array", "object"},
		},
		{name: "unsupported type keyword", prop: `{"type":{"const":"string"}}`},
		{name: "no type at all", prop: `{"description":"d"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var prop propDoc
			if err := json.Unmarshal([]byte(tc.prop), &prop); err != nil {
				t.Fatalf("decode prop: %v", err)
			}
			types, optional := flattenTypes(prop)
			if !covSurfSameStrings(types, tc.types) {
				t.Errorf("types = %v, want %v", types, tc.types)
			}
			if optional != tc.optional {
				t.Errorf("optional = %v, want %v", optional, tc.optional)
			}
		})
	}
}

// hasKey is what separates `default: null` (an explicit unset value) from a
// property with no default at all, so presence — not the value — must decide.
func TestCovSurfHasKey(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "explicit null default", raw: `{"default":null}`, want: true},
		{name: "value default", raw: `{"default":0}`, want: true},
		{name: "no default", raw: `{"type":"string"}`, want: false},
		{name: "empty object", raw: `{}`, want: false},
		{name: "not an object", raw: `["default"]`, want: false},
		{name: "not json", raw: `{"default"`, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasKey(json.RawMessage(tc.raw), "default"); got != tc.want {
				t.Errorf("hasKey(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
