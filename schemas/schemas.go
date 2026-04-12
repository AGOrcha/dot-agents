package schemas

import (
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

// Plugin is the embedded schema handle for PLUGIN.yaml.
var Plugin = Schema{
	name: "plugin.schema.json",
	data: pluginSchemaBytes,
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

func validatePluginManifest(jsonBytes []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	allowed := map[string]struct{}{
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
	for key := range payload {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown top-level field %q", key)
		}
	}

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
		switch id {
		case "claude", "cursor", "codex", "copilot", "opencode":
		default:
			return fmt.Errorf("unknown platform %q", id)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("platforms contains duplicate %q", id)
		}
		seen[id] = struct{}{}
	}

	if authors, ok := payload["authors"].([]any); ok {
		for _, raw := range authors {
			if strings.TrimSpace(asString(raw)) == "" {
				return fmt.Errorf("authors contains an empty value")
			}
		}
	}

	if resources, ok := payload["resources"].(map[string]any); ok {
		allowedResources := map[string]struct{}{
			"agents":   {},
			"skills":   {},
			"commands": {},
			"hooks":    {},
			"mcp":      {},
		}
		for key, value := range resources {
			if _, ok := allowedResources[key]; !ok {
				return fmt.Errorf("resources contains unknown field %q", key)
			}
			if err := validateStringArray(value, "resources."+key); err != nil {
				return err
			}
		}
	}

	if marketplace, ok := payload["marketplace"].(map[string]any); ok {
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
	}

	if overrides, ok := payload["platform_overrides"].(map[string]any); ok {
		keys := make([]string, 0, len(overrides))
		for key := range overrides {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			switch key {
			case "claude", "cursor", "codex", "copilot", "opencode":
			default:
				return fmt.Errorf("unknown platform override %q", key)
			}
			if _, ok := overrides[key].(map[string]any); !ok {
				return fmt.Errorf("platform_overrides.%s must be an object", key)
			}
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
