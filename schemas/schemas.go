package schemas

import (
	// _ "embed": pull in embedded schema files via go:embed
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Schema is a lightweight embedded schema handle used by repo-local manifest
// loaders. The data is embedded so callers can keep schema documentation and
// validation logic in one place without a runtime dependency on an external
// compiler.
type Schema struct {
	name string
	data []byte
}

//go:embed plugin.schema.json
var pluginSchemaBytes []byte

//go:embed workflow-delegation-bundle.schema.json
var workflowDelegationBundleSchemaBytes []byte

//go:embed agentsrc.schema.json
var agentsRCSchemaBytes []byte

// AgentsRCSchemaJSON returns the canonical AgentsRC (.agentsrc.json) JSON schema
// bytes. It is exported so command code can validate against the single canonical
// schema without copying the file (go:embed cannot reach a sibling directory).
func AgentsRCSchemaJSON() []byte { return agentsRCSchemaBytes }

// Plugin is the embedded schema handle for PLUGIN.yaml.
var Plugin = Schema{
	name: "plugin.schema.json",
	data: pluginSchemaBytes,
}

// WorkflowDelegationBundle is the embedded schema handle for
// .agents/active/delegation-bundles/<delegation-id>.yaml.
var WorkflowDelegationBundle = Schema{
	name: "workflow-delegation-bundle.schema.json",
	data: workflowDelegationBundleSchemaBytes,
}

// Validate checks jsonBytes against the supplied schema handle.
func Validate(schema Schema, jsonBytes []byte) error {
	switch schema.name {
	case "plugin.schema.json":
		return validatePluginManifest(jsonBytes)
	default:
		return nil
	}
}

// pluginAllowedTopLevel lists the top-level fields permitted in PLUGIN.yaml.
var pluginAllowedTopLevel = map[string]struct{}{
	"schema_version":     {},
	"kind":               {},
	"name":               {},
	"version":            {},
	"display_name":       {},
	"description":        {},
	"authors":            {},
	"homepage":           {},
	"license":            {},
	"platforms":          {},
	"resources":          {},
	"marketplace":        {},
	"dependencies":       {},
	"platform_overrides": {},
}

// pluginAllowedResources lists the resource buckets supported under
// `resources` in PLUGIN.yaml.
var pluginAllowedResources = map[string]struct{}{
	"agents":   {},
	"skills":   {},
	"commands": {},
	"hooks":    {},
	"mcp":      {},
}

// pluginAllowedPlatforms lists every platform id accepted by `platforms` and
// `platform_overrides`.
var pluginAllowedPlatforms = map[string]struct{}{
	"claude":   {},
	"cursor":   {},
	"codex":    {},
	"copilot":  {},
	"opencode": {},
}

func validatePluginManifest(jsonBytes []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	for key := range payload {
		if _, ok := pluginAllowedTopLevel[key]; !ok {
			return fmt.Errorf("unknown top-level field %q", key)
		}
	}
	if err := validatePluginCoreFields(payload); err != nil {
		return err
	}
	if err := validatePluginPlatforms(payload); err != nil {
		return err
	}
	if err := validatePluginAuthors(payload); err != nil {
		return err
	}
	if err := validatePluginResources(payload); err != nil {
		return err
	}
	if err := validatePluginMarketplace(payload); err != nil {
		return err
	}
	return validatePluginOverrides(payload)
}

// validatePluginCoreFields enforces the schema_version, kind, and name
// constraints for a plugin manifest.
func validatePluginCoreFields(payload map[string]any) error {
	if v, ok := payload["schema_version"]; !ok || !matchesSchemaVersionOne(v) {
		return fmt.Errorf("schema_version must be 1")
	}
	kind, _ := payload["kind"].(string)
	switch strings.TrimSpace(kind) {
	case "native", "package":
	default:
		return fmt.Errorf("kind must be native or package")
	}
	name, _ := payload["name"].(string)
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

// validatePluginPlatforms ensures `platforms` is a non-empty slice of known,
// unique platform ids.
func validatePluginPlatforms(payload map[string]any) error {
	platforms, ok := payload["platforms"].([]any)
	if !ok || len(platforms) == 0 {
		return fmt.Errorf("platforms must contain at least one platform id")
	}
	seen := map[string]struct{}{}
	for _, raw := range platforms {
		id, _ := raw.(string)
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("platforms contains an empty platform id")
		}
		if _, ok := pluginAllowedPlatforms[id]; !ok {
			return fmt.Errorf("unknown platform %q", id)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("platforms contains duplicate %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// validatePluginAuthors checks the optional authors slice.
func validatePluginAuthors(payload map[string]any) error {
	authors, ok := payload["authors"].([]any)
	if !ok {
		return nil
	}
	for _, raw := range authors {
		if strings.TrimSpace(asString(raw)) == "" {
			return fmt.Errorf("authors contains an empty value")
		}
	}
	return nil
}

// validatePluginResources checks the optional resources block, including the
// allowed bucket keys and string-array shape of each value.
func validatePluginResources(payload map[string]any) error {
	resources, ok := payload["resources"].(map[string]any)
	if !ok {
		return nil
	}
	for key, value := range resources {
		if _, ok := pluginAllowedResources[key]; !ok {
			return fmt.Errorf("resources contains unknown field %q", key)
		}
		if err := validateStringArray(value, "resources."+key); err != nil {
			return err
		}
	}
	return nil
}

// validatePluginMarketplace checks the optional marketplace block (repo/tags).
func validatePluginMarketplace(payload map[string]any) error {
	marketplace, ok := payload["marketplace"].(map[string]any)
	if !ok {
		return nil
	}
	for key := range marketplace {
		if key != "repo" && key != "tags" {
			return fmt.Errorf("marketplace contains unknown field %q", key)
		}
	}
	if tags, ok := marketplace["tags"]; ok {
		if err := validateStringArray(tags, "marketplace.tags"); err != nil {
			return err
		}
	}
	return nil
}

// validatePluginOverrides checks the optional platform_overrides block;
// keys are sorted before iteration to keep error messages deterministic.
func validatePluginOverrides(payload map[string]any) error {
	overrides, ok := payload["platform_overrides"].(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := pluginAllowedPlatforms[key]; !ok {
			return fmt.Errorf("unknown platform override %q", key)
		}
		if _, ok := overrides[key].(map[string]any); !ok {
			return fmt.Errorf("platform_overrides.%s must be an object", key)
		}
	}
	return nil
}

func validateStringArray(value any, path string) error {
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", path)
	}
	for _, raw := range items {
		if strings.TrimSpace(asString(raw)) == "" {
			return fmt.Errorf("%s contains an empty value", path)
		}
	}
	return nil
}

func matchesSchemaVersionOne(v any) bool {
	switch typed := v.(type) {
	case float64:
		return typed == 1
	case float32:
		return typed == 1
	case int:
		return typed == 1
	case int64:
		return typed == 1
	case json.Number:
		return typed.String() == "1"
	default:
		return false
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
