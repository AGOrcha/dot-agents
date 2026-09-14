package crgrelease

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// contractFS holds the generated release contract. It is embedded rather than
// read from testdata because the tool inventory is needed at RUNTIME to answer
// `tools/list`, and a shipped binary has no testdata directory.
//
// The embedded copy is a byte-for-byte duplicate of
// testdata/crg-release/<Version>/{tools-list,release,prompts}.json;
// surface_test.go compares them so the two cannot drift.
//
//go:embed contract/2.3.8/tools-list.json contract/2.3.8/release.json contract/2.3.8/prompts.json
var contractFS embed.FS

var (
	toolsListJSON = mustContract("contract/2.3.8/tools-list.json")
	releaseJSON   = mustContract("contract/2.3.8/release.json")
	promptsJSON   = mustContract("contract/2.3.8/prompts.json")
)

func mustContract(name string) []byte {
	data, err := contractFS.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("crgrelease: embedded contract %s missing: %v", name, err))
	}
	return data
}

// Tool is one advertised MCP tool of the pinned release.
//
// InputSchema keeps the upstream bytes verbatim so `tools/list` reproduces the
// released JSON Schema exactly — including property descriptions, the
// `anyOf: [{type}, {type: null}]` spelling of optional parameters, the default
// values and `additionalProperties: false`. Params is the same schema decoded
// into the form argument binding needs.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Required    []string
	Params      map[string]Param
	// order lists parameter names in the schema's own order, which is what
	// validation error ordering follows.
	order []string
}

// Param is one decoded input-schema property.
//
// Types is the set of accepted JSON types, flattened from either a bare
// `"type"` or the `anyOf` union upstream emits for `Optional[...]`
// parameters. Optional records whether `null` is accepted — the distinction
// that decides whether a null argument is a validation error or an explicit
// "unset".
type Param struct {
	Name        string
	Types       []string
	ItemTypes   []string
	Optional    bool
	Default     any
	HasDefault  bool
	Description string
}

type schemaDoc struct {
	Type                 string                     `json:"type"`
	AdditionalProperties *bool                      `json:"additionalProperties"`
	Required             []string                   `json:"required"`
	Properties           map[string]json.RawMessage `json:"properties"`
}

type propDoc struct {
	Type        any       `json:"type"`
	AnyOf       []propDoc `json:"anyOf"`
	Items       *propDoc  `json:"items"`
	Default     any       `json:"default"`
	Description string    `json:"description"`
}

type toolDoc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	// ParameterOrder is the tool function's declared parameter order. The
	// JSON schema's properties are emitted sorted, so declaration order has
	// to be captured separately — the validator reports field errors in it.
	ParameterOrder []string `json:"parameter_order"`
}

var (
	surfaceOnce  sync.Once
	surfaceTools []Tool
	surfaceIndex map[string]Tool
	surfaceErr   error
)

// Surface returns the pinned release's tool inventory in its published order.
// Callers must not mutate the returned slice's InputSchema bytes.
func Surface() []Tool {
	loadSurface()
	if surfaceErr != nil {
		panic(surfaceErr)
	}
	return surfaceTools
}

// Lookup returns the named tool, or ok=false when the pinned release does not
// publish it. An unknown tool is the one case where the server must answer
// with upstream's `Unknown tool: '<name>'` result rather than a schema error.
func Lookup(name string) (Tool, bool) {
	loadSurface()
	if surfaceErr != nil {
		panic(surfaceErr)
	}
	tool, ok := surfaceIndex[name]
	return tool, ok
}

// ToolNames returns every published tool name, sorted.
func ToolNames() []string {
	names := make([]string, 0, len(Surface()))
	for _, tool := range Surface() {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func loadSurface() {
	surfaceOnce.Do(func() {
		var docs []toolDoc
		if err := json.Unmarshal(toolsListJSON, &docs); err != nil {
			surfaceErr = fmt.Errorf("crgrelease: decode tools-list: %w", err)
			return
		}
		tools := make([]Tool, 0, len(docs))
		index := make(map[string]Tool, len(docs))
		for _, doc := range docs {
			tool, err := decodeTool(doc)
			if err != nil {
				surfaceErr = err
				return
			}
			tools = append(tools, tool)
			index[tool.Name] = tool
		}
		surfaceTools, surfaceIndex = tools, index
	})
}

func decodeTool(doc toolDoc) (Tool, error) {
	var schema schemaDoc
	if err := json.Unmarshal(doc.InputSchema, &schema); err != nil {
		return Tool{}, fmt.Errorf("crgrelease: decode %s input schema: %w", doc.Name, err)
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		// Every released tool closes its argument object; an open schema would
		// silently accept arguments upstream rejects.
		return Tool{}, fmt.Errorf(
			"crgrelease: %s input schema does not set additionalProperties:false", doc.Name)
	}
	params := make(map[string]Param, len(schema.Properties))
	for name, raw := range schema.Properties {
		var prop propDoc
		if err := json.Unmarshal(raw, &prop); err != nil {
			return Tool{}, fmt.Errorf("crgrelease: decode %s.%s: %w", doc.Name, name, err)
		}
		param := Param{Name: name, Description: prop.Description}
		param.Types, param.Optional = flattenTypes(prop)
		if prop.Items != nil {
			param.ItemTypes, _ = flattenTypes(*prop.Items)
		}
		if hasKey(raw, "default") {
			param.Default, param.HasDefault = prop.Default, true
		}
		params[name] = param
	}
	// Validation errors are reported in DECLARATION order, which the sorted
	// schema properties do not preserve — hence the captured order. Verifying
	// the two describe the same parameter set is what catches a fixture that
	// was regenerated only halfway.
	order := append([]string(nil), doc.ParameterOrder...)
	if len(order) != len(params) {
		return Tool{}, fmt.Errorf(
			"crgrelease: %s declares %d parameters but the schema has %d",
			doc.Name, len(order), len(params))
	}
	for _, name := range order {
		if _, ok := params[name]; !ok {
			return Tool{}, fmt.Errorf(
				"crgrelease: %s declares parameter %q with no schema property", doc.Name, name)
		}
	}
	return Tool{
		Name:        doc.Name,
		Description: doc.Description,
		InputSchema: doc.InputSchema,
		Required:    schema.Required,
		Params:      params,
		order:       order,
	}, nil
}

// typeSet accumulates a property's accepted JSON type names, folding `null`
// out of the set and into an "optional" flag — the distinction that decides
// whether a null argument is a validation error or an explicit "unset".
type typeSet struct {
	types    []string
	optional bool
}

// add records one JSON type name.
func (s *typeSet) add(name string) {
	if name == "null" {
		s.optional = true
		return
	}
	s.types = append(s.types, name)
}

// addDeclared records a property's `type` declaration, which upstream spells
// either as a single name or as a list of names. Anything else carries no
// type information and is ignored.
func (s *typeSet) addDeclared(declared any) {
	switch typed := declared.(type) {
	case string:
		s.add(typed)
	case []any:
		for _, item := range typed {
			if name, ok := item.(string); ok {
				s.add(name)
			}
		}
	}
}

// flattenTypes reduces a property's type declaration to the set of accepted
// JSON type names, reporting whether `null` is among them. Upstream spells
// `Optional[...]` as an `anyOf` union, so the alternatives are flattened
// recursively into the same set.
func flattenTypes(prop propDoc) (types []string, optional bool) {
	var set typeSet
	set.addDeclared(prop.Type)
	for _, alt := range prop.AnyOf {
		altTypes, altOptional := flattenTypes(alt)
		set.types = append(set.types, altTypes...)
		set.optional = set.optional || altOptional
	}
	return set.types, set.optional
}

// hasKey reports whether a JSON object literally carries a key, which is how
// "default: null" is distinguished from "no default".
func hasKey(raw json.RawMessage, key string) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	_, ok := probe[key]
	return ok
}

// ToolsListResult returns the exact `tools/list` result payload of the pinned
// release: `{"tools": [...]}` with each descriptor's published name,
// description and verbatim input schema.
func ToolsListResult() (json.RawMessage, error) {
	type descriptor struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	tools := Surface()
	out := make([]descriptor, 0, len(tools))
	for _, tool := range tools {
		out = append(out, descriptor{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return json.Marshal(map[string]any{"tools": out})
}
