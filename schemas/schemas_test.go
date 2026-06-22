package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPluginAndBundleSchemasEmbedded(t *testing.T) {
	if len(Plugin.data) == 0 {
		t.Fatal("Plugin schema not embedded")
	}
	if Plugin.name != "plugin.schema.json" {
		t.Errorf("Plugin.name = %q", Plugin.name)
	}
	if len(WorkflowDelegationBundle.data) == 0 {
		t.Fatal("WorkflowDelegationBundle schema not embedded")
	}
	if WorkflowDelegationBundle.name != "workflow-delegation-bundle.schema.json" {
		t.Errorf("WorkflowDelegationBundle.name = %q", WorkflowDelegationBundle.name)
	}
}

func TestAgentsRCSchemaJSONEmbeddedAndParses(t *testing.T) {
	data := AgentsRCSchemaJSON()
	if len(data) == 0 {
		t.Fatal("AgentsRC schema not embedded")
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("AgentsRC schema does not parse as JSON: %v", err)
	}
}

func TestValidateSwitchesOnSchemaName(t *testing.T) {
	// Unknown schema -> nil (default branch).
	if err := Validate(Schema{name: "unknown"}, []byte(`{}`)); err != nil {
		t.Errorf("Validate(unknown): got %v; want nil", err)
	}
	// Plugin schema delegates to validatePluginManifest.
	if err := Validate(Plugin, []byte(`not json`)); err == nil {
		t.Error("Validate(plugin, junk): want error")
	}
	// WorkflowDelegationBundle -> nil (no validator wired yet).
	if err := Validate(WorkflowDelegationBundle, []byte(`{}`)); err != nil {
		t.Errorf("Validate(bundle): got %v; want nil", err)
	}
}

func validPluginPayload() map[string]any {
	return map[string]any{
		"schema_version": float64(1),
		"kind":           "native",
		"name":           "p1",
		"platforms":      []any{"claude"},
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestValidatePluginManifest_HappyPath(t *testing.T) {
	payload := validPluginPayload()
	payload["display_name"] = "Plugin One"
	payload["version"] = "0.1.0"
	payload["description"] = "desc"
	payload["homepage"] = "https://example.com"
	payload["license"] = "MIT"
	payload["authors"] = []any{"Alice"}
	payload["resources"] = map[string]any{
		"agents":   []any{"alpha"},
		"skills":   []any{"beta"},
		"commands": []any{"c1"},
		"hooks":    []any{"h1"},
		"mcp":      []any{"m1"},
	}
	payload["marketplace"] = map[string]any{
		"repo": "owner/name",
		"tags": []any{"productivity"},
	}
	payload["platform_overrides"] = map[string]any{
		"claude": map[string]any{"foo": "bar"},
	}
	payload["dependencies"] = map[string]any{"x": "1"}
	if err := Validate(Plugin, mustJSON(t, payload)); err != nil {
		t.Fatalf("happy plugin: %v", err)
	}
}

func TestValidatePluginManifest_UnknownTopLevel(t *testing.T) {
	p := validPluginPayload()
	p["bogus"] = 1
	err := Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "unknown top-level field") {
		t.Errorf("err = %v", err)
	}
}

func TestValidatePluginManifest_InvalidJSON(t *testing.T) {
	if err := Validate(Plugin, []byte(`{`)); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("err = %v", err)
	}
}

func TestValidatePluginManifest_SchemaVersion(t *testing.T) {
	cases := []any{
		float64(2),       // wrong version
		"1",              // string instead of number
		json.Number("2"), // wrong json.Number
	}
	for _, sv := range cases {
		p := validPluginPayload()
		p["schema_version"] = sv
		err := Validate(Plugin, mustJSON(t, p))
		if err == nil || !strings.Contains(err.Error(), "schema_version must be 1") {
			t.Errorf("schema_version=%v: err = %v", sv, err)
		}
	}
	// Missing entirely.
	p := validPluginPayload()
	delete(p, "schema_version")
	if err := Validate(Plugin, mustJSON(t, p)); err == nil {
		t.Error("missing schema_version: want error")
	}
}

func TestMatchesSchemaVersionOne_AllTypes(t *testing.T) {
	if !matchesSchemaVersionOne(float64(1)) {
		t.Error("float64(1)")
	}
	if !matchesSchemaVersionOne(float32(1)) {
		t.Error("float32(1)")
	}
	if !matchesSchemaVersionOne(int(1)) {
		t.Error("int(1)")
	}
	if !matchesSchemaVersionOne(int64(1)) {
		t.Error("int64(1)")
	}
	if !matchesSchemaVersionOne(json.Number("1")) {
		t.Error("json.Number(1)")
	}
	if matchesSchemaVersionOne(true) {
		t.Error("bool true should be false")
	}
}

func TestValidatePluginManifest_Kind(t *testing.T) {
	for _, kind := range []any{"weird", "", 123} {
		p := validPluginPayload()
		p["kind"] = kind
		err := Validate(Plugin, mustJSON(t, p))
		if err == nil || !strings.Contains(err.Error(), "kind must be") {
			t.Errorf("kind=%v: err = %v", kind, err)
		}
	}
	// package kind is allowed.
	p := validPluginPayload()
	p["kind"] = "package"
	if err := Validate(Plugin, mustJSON(t, p)); err != nil {
		t.Errorf("kind=package: %v", err)
	}
}

func TestValidatePluginManifest_Name(t *testing.T) {
	p := validPluginPayload()
	p["name"] = "   "
	err := Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Errorf("blank name: err = %v", err)
	}
}

func TestValidatePluginManifest_Platforms(t *testing.T) {
	// Missing.
	p := validPluginPayload()
	delete(p, "platforms")
	if err := Validate(Plugin, mustJSON(t, p)); err == nil {
		t.Error("missing platforms: want error")
	}
	// Empty.
	p = validPluginPayload()
	p["platforms"] = []any{}
	if err := Validate(Plugin, mustJSON(t, p)); err == nil {
		t.Error("empty platforms: want error")
	}
	// Empty string id.
	p = validPluginPayload()
	p["platforms"] = []any{"  "}
	err := Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "empty platform id") {
		t.Errorf("empty id: err = %v", err)
	}
	// Unknown.
	p = validPluginPayload()
	p["platforms"] = []any{"windows"}
	err = Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "unknown platform") {
		t.Errorf("unknown platform: err = %v", err)
	}
	// Duplicate.
	p = validPluginPayload()
	p["platforms"] = []any{"claude", "claude"}
	err = Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("dup: err = %v", err)
	}
}

func TestValidatePluginManifest_Authors(t *testing.T) {
	p := validPluginPayload()
	p["authors"] = []any{""}
	err := Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "authors") {
		t.Errorf("empty author: err = %v", err)
	}
	// Non-array authors -> ignored.
	p = validPluginPayload()
	p["authors"] = "not-array"
	if err := Validate(Plugin, mustJSON(t, p)); err != nil {
		t.Errorf("non-array authors: want nil; got %v", err)
	}
}

func TestValidatePluginManifest_Resources(t *testing.T) {
	p := validPluginPayload()
	p["resources"] = map[string]any{"weird": []any{"x"}}
	err := Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("unknown resource: err = %v", err)
	}
	// Non-array value.
	p = validPluginPayload()
	p["resources"] = map[string]any{"agents": "nope"}
	err = Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "must be an array") {
		t.Errorf("non-array resource: err = %v", err)
	}
	// Empty item.
	p = validPluginPayload()
	p["resources"] = map[string]any{"agents": []any{""}}
	err = Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "empty value") {
		t.Errorf("empty resource item: err = %v", err)
	}
}

func TestValidatePluginManifest_Marketplace(t *testing.T) {
	p := validPluginPayload()
	p["marketplace"] = map[string]any{"bogus": 1}
	err := Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "marketplace contains unknown") {
		t.Errorf("bad marketplace: err = %v", err)
	}
	// Bad tags.
	p = validPluginPayload()
	p["marketplace"] = map[string]any{"tags": "not-array"}
	err = Validate(Plugin, mustJSON(t, p))
	if err == nil {
		t.Error("non-array marketplace.tags: want error")
	}
	// Non-object marketplace -> ignored.
	p = validPluginPayload()
	p["marketplace"] = "skip"
	if err := Validate(Plugin, mustJSON(t, p)); err != nil {
		t.Errorf("non-object marketplace: %v", err)
	}
}

func TestValidatePluginManifest_PlatformOverrides(t *testing.T) {
	// Unknown override platform.
	p := validPluginPayload()
	p["platform_overrides"] = map[string]any{"windows": map[string]any{}}
	err := Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "unknown platform override") {
		t.Errorf("err = %v", err)
	}
	// Non-object override value.
	p = validPluginPayload()
	p["platform_overrides"] = map[string]any{"claude": "string"}
	err = Validate(Plugin, mustJSON(t, p))
	if err == nil || !strings.Contains(err.Error(), "must be an object") {
		t.Errorf("err = %v", err)
	}
	// Non-object overrides -> ignored.
	p = validPluginPayload()
	p["platform_overrides"] = "x"
	if err := Validate(Plugin, mustJSON(t, p)); err != nil {
		t.Errorf("non-object overrides: %v", err)
	}
}

func TestAsString(t *testing.T) {
	if got := asString("hi"); got != "hi" {
		t.Errorf("asString(string): %q", got)
	}
	if got := asString(123); got != "" {
		t.Errorf("asString(int): %q; want empty", got)
	}
}
