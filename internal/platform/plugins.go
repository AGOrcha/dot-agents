package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NikashPrakash/dot-agents/schemas"
	"go.yaml.in/yaml/v3"
)

const PluginManifestName = "PLUGIN.yaml"

type PluginKind string

const (
	PluginKindNative  PluginKind = "native"
	PluginKindPackage PluginKind = "package"
)

type PluginResources struct {
	Agents   []string `yaml:"agents,omitempty"`
	Skills   []string `yaml:"skills,omitempty"`
	Commands []string `yaml:"commands,omitempty"`
	Hooks    []string `yaml:"hooks,omitempty"`
	MCP      []string `yaml:"mcp,omitempty"`
}

type PluginMarketplace struct {
	Repo string   `yaml:"repo,omitempty"`
	Tags []string `yaml:"tags,omitempty"`
}

// PluginSpec is the canonical dot-agents plugin bundle manifest.
// Schema contract: schemas/plugin.schema.json - keep this struct aligned with
// the manifest contract and validate raw YAML bytes via PluginManifestSchema
// before trusting the typed fields.
type PluginSpec struct {
	SchemaVersion     int                       `yaml:"schema_version"`
	Kind              PluginKind                `yaml:"kind"`
	Name              string                    `yaml:"name"`
	Version           string                    `yaml:"version,omitempty"`
	DisplayName       string                    `yaml:"display_name,omitempty"`
	Description       string                    `yaml:"description,omitempty"`
	Authors           []string                  `yaml:"authors,omitempty"`
	Homepage          string                    `yaml:"homepage,omitempty"`
	License           string                    `yaml:"license,omitempty"`
	Platforms         []string                  `yaml:"platforms"`
	Resources         PluginResources           `yaml:"resources,omitempty"`
	Marketplace       PluginMarketplace         `yaml:"marketplace,omitempty"`
	Dependencies      map[string]any            `yaml:"dependencies,omitempty"`
	PlatformOverrides map[string]map[string]any `yaml:"platform_overrides,omitempty"`
	Dir               string                    `yaml:"-"`
	ManifestPath      string                    `yaml:"-"`
	Scope             string                    `yaml:"-"`
}

// PluginManifestSchema is the embedded compiled schema for PLUGIN.yaml.
var PluginManifestSchema = schemas.Plugin

// LoadPluginSpec parses and validates a canonical plugin manifest from pluginDir.
func LoadPluginSpec(pluginDir string) (PluginSpec, error) {
	manifestPath := filepath.Join(pluginDir, PluginManifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return PluginSpec{}, fmt.Errorf("reading plugin manifest %s: %w", manifestPath, err)
	}

	var generic map[string]any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return PluginSpec{}, fmt.Errorf("parsing plugin manifest %s: %w", manifestPath, err)
	}
	jsonBytes, err := json.Marshal(generic)
	if err != nil {
		return PluginSpec{}, fmt.Errorf("encoding plugin manifest %s: %w", manifestPath, err)
	}
	if err := schemas.Validate(PluginManifestSchema, jsonBytes); err != nil {
		return PluginSpec{}, fmt.Errorf("schema validation for %s: %w", manifestPath, err)
	}

	var spec PluginSpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return PluginSpec{}, fmt.Errorf("unmarshal plugin manifest %s: %w", manifestPath, err)
	}

	spec.Dir = pluginDir
	spec.ManifestPath = manifestPath
	spec.Kind = PluginKind(strings.TrimSpace(string(spec.Kind)))
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Version = strings.TrimSpace(spec.Version)
	spec.DisplayName = strings.TrimSpace(spec.DisplayName)
	spec.Description = strings.TrimSpace(spec.Description)
	spec.Authors = sortedUniqueStrings(spec.Authors)
	spec.Homepage = strings.TrimSpace(spec.Homepage)
	spec.License = strings.TrimSpace(spec.License)
	spec.Platforms = sortedUniqueStrings(spec.Platforms)
	spec.Resources.Agents = sortedUniqueStrings(spec.Resources.Agents)
	spec.Resources.Skills = sortedUniqueStrings(spec.Resources.Skills)
	spec.Resources.Commands = sortedUniqueStrings(spec.Resources.Commands)
	spec.Resources.Hooks = sortedUniqueStrings(spec.Resources.Hooks)
	spec.Resources.MCP = sortedUniqueStrings(spec.Resources.MCP)
	spec.Marketplace.Repo = strings.TrimSpace(spec.Marketplace.Repo)
	spec.Marketplace.Tags = sortedUniqueStrings(spec.Marketplace.Tags)
	for platformID, overrides := range spec.PlatformOverrides {
		if len(overrides) == 0 {
			delete(spec.PlatformOverrides, platformID)
		}
	}

	return spec, nil
}

// ListPluginSpecs returns canonical plugin specs for a scope. An empty scope
// scans all scopes under ~/.agents/plugins/.
func ListPluginSpecs(agentsHome, scope string) ([]PluginSpec, error) {
	root := filepath.Join(agentsHome, "plugins")
	if scope != "" {
		return listPluginSpecsInScope(root, scope)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var specs []PluginSpec
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		scopeSpecs, err := listPluginSpecsInScope(root, entry.Name())
		if err != nil {
			return nil, err
		}
		specs = append(specs, scopeSpecs...)
	}
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Scope == specs[j].Scope {
			return specs[i].Name < specs[j].Name
		}
		return specs[i].Scope < specs[j].Scope
	})
	return specs, nil
}

func listPluginSpecsInScope(root, scope string) ([]PluginSpec, error) {
	scopeRoot := filepath.Join(root, scope)
	entries, err := os.ReadDir(scopeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	specs := make([]PluginSpec, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginDir := filepath.Join(scopeRoot, entry.Name())
		spec, err := LoadPluginSpec(pluginDir)
		if err != nil {
			return nil, err
		}
		spec.Scope = scope
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].Name < specs[j].Name
	})
	return specs, nil
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
