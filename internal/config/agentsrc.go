package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// isDirEntry reports whether the path is a directory, following symlinks.
func isDirEntry(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// StringsOrBool holds either a boolean flag (all/none) or a named list.
// It marshals/unmarshals as either a JSON bool or a JSON string array:
//
//	true             → All resources of this type
//	false            → No resources
//	["name1","name2"] → Only the named resources
type StringsOrBool struct {
	All   bool
	Names []string
}

// IsEnabled returns true if any resources are enabled (All or at least one name).
func (s StringsOrBool) IsEnabled() bool {
	return s.All || len(s.Names) > 0
}

// Contains returns true if name is covered (either All=true or name is in Names).
func (s StringsOrBool) Contains(name string) bool {
	if s.All {
		return true
	}
	for _, n := range s.Names {
		if n == name {
			return true
		}
	}
	return false
}

// Add appends name to Names unless All is true (already covers everything).
func (s *StringsOrBool) Add(name string) {
	if s.All {
		return
	}
	for _, n := range s.Names {
		if n == name {
			return // already present
		}
	}
	s.Names = append(s.Names, name)
}

// Remove removes name from Names. No-op if All is true.
func (s *StringsOrBool) Remove(name string) {
	if s.All {
		return
	}
	out := s.Names[:0]
	for _, n := range s.Names {
		if n != name {
			out = append(out, n)
		}
	}
	s.Names = out
}

func (s StringsOrBool) MarshalJSON() ([]byte, error) {
	if len(s.Names) > 0 {
		return json.Marshal(s.Names)
	}
	return json.Marshal(s.All)
}

func (s *StringsOrBool) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		s.All = b
		s.Names = nil
		return nil
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return fmt.Errorf("hooks/mcp field must be bool or string array: %w", err)
	}
	s.All = false
	s.Names = names
	return nil
}

// AgentsRC represents the .agentsrc.json manifest committed to a project repo.
type AgentsRC struct {
	Schema   string        `json:"$schema,omitempty"`
	Version  int           `json:"version"`
	Project  string        `json:"project,omitempty"`
	Skills   []string      `json:"skills,omitempty"`
	Rules    []string      `json:"rules,omitempty"`
	Agents   []string      `json:"agents,omitempty"`
	Hooks    StringsOrBool `json:"hooks"`
	MCP      StringsOrBool `json:"mcp"`
	Settings bool          `json:"settings"`
	Sources  []Source      `json:"sources"`

	// ExtraFields captures unknown JSON keys so Save() can round-trip them
	// instead of silently dropping legacy or custom fields.
	ExtraFields map[string]json.RawMessage `json:"-"`
}

// agentsRCKnown lists all JSON keys owned by AgentsRC's known fields.
var agentsRCKnown = map[string]bool{
	"$schema": true, "version": true, "project": true,
	"skills": true, "rules": true, "agents": true,
	"hooks": true, "mcp": true, "settings": true, "sources": true,
}

// agentsRCCore is an alias used in custom marshal/unmarshal to avoid
// infinite recursion while still using the standard json encoder.
type agentsRCCore struct {
	Schema   string        `json:"$schema,omitempty"`
	Version  int           `json:"version"`
	Project  string        `json:"project,omitempty"`
	Skills   []string      `json:"skills,omitempty"`
	Rules    []string      `json:"rules,omitempty"`
	Agents   []string      `json:"agents,omitempty"`
	Hooks    StringsOrBool `json:"hooks"`
	MCP      StringsOrBool `json:"mcp"`
	Settings bool          `json:"settings"`
	Sources  []Source      `json:"sources"`
}

func (a *AgentsRC) UnmarshalJSON(data []byte) error {
	var core agentsRCCore
	if err := json.Unmarshal(data, &core); err != nil {
		return err
	}
	a.Schema = core.Schema
	a.Version = core.Version
	a.Project = core.Project
	a.Skills = core.Skills
	a.Rules = core.Rules
	a.Agents = core.Agents
	a.Hooks = core.Hooks
	a.MCP = core.MCP
	a.Settings = core.Settings
	a.Sources = core.Sources

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for k, v := range all {
		if !agentsRCKnown[k] {
			if a.ExtraFields == nil {
				a.ExtraFields = make(map[string]json.RawMessage)
			}
			a.ExtraFields[k] = v
		}
	}
	return nil
}

func (a AgentsRC) MarshalJSON() ([]byte, error) {
	core := agentsRCCore{
		Schema:   a.Schema,
		Version:  a.Version,
		Project:  a.Project,
		Skills:   a.Skills,
		Rules:    a.Rules,
		Agents:   a.Agents,
		Hooks:    a.Hooks,
		MCP:      a.MCP,
		Settings: a.Settings,
		Sources:  a.Sources,
	}
	data, err := json.Marshal(core)
	if err != nil {
		return nil, err
	}
	if len(a.ExtraFields) == 0 {
		return data, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for k, v := range a.ExtraFields {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

// Source describes where to find agent resources.
type Source struct {
	Type string `json:"type"`           // "local" | "git"
	Path string `json:"path,omitempty"` // override path for "local"
	URL  string `json:"url,omitempty"`  // repository URL for "git"
	Ref  string `json:"ref,omitempty"`  // branch/tag for "git"
}

const AgentsRCFile = ".agentsrc.json"

// LoadAgentsRC reads .agentsrc.json from the given project directory.
func LoadAgentsRC(projectPath string) (*AgentsRC, error) {
	path := filepath.Join(projectPath, AgentsRCFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rc AgentsRC
	if err := json.Unmarshal(data, &rc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", AgentsRCFile, err)
	}
	// Default to a local source if none declared
	if len(rc.Sources) == 0 {
		rc.Sources = []Source{{Type: "local"}}
	}
	return &rc, nil
}

// Save writes the manifest to .agentsrc.json in projectPath.
func (a *AgentsRC) Save(projectPath string) error {
	path := filepath.Join(projectPath, AgentsRCFile)
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", AgentsRCFile, err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// AgentsCacheDir returns the root directory for cached remote sources.
func AgentsCacheDir() string {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, _ := os.UserHomeDir()
		cacheHome = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheHome, "dot-agents")
}

// GitSourceCacheDir returns the cache directory for a given git URL.
func GitSourceCacheDir(url string) string {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(url)))[:12]
	return filepath.Join(AgentsCacheDir(), "sources", hash)
}

// AppendUnique appends s to slice only if not already present.
func AppendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// GenerateAgentsRC inspects ~/.agents/ and builds a manifest for the given project.
func GenerateAgentsRC(projectName, projectPath string) (*AgentsRC, error) {
	agentsHome := AgentsHome()

	rc := &AgentsRC{
		Schema:  "https://dot-agents.dev/schemas/agentsrc.json",
		Version: 1,
		Project: projectName,
		Sources: []Source{{Type: "local"}},
	}

	scopes := []string{"global", projectName}
	rc.Skills = collectScopedDirs(agentsHome, "skills", scopes, "SKILL.md")
	rc.Agents = collectScopedDirs(agentsHome, "agents", scopes, "AGENT.md")
	rc.Rules = detectRuleScopes(agentsHome, projectName)
	rc.Hooks = detectHookEvents(agentsHome, projectName)
	rc.MCP = detectMCPServers(agentsHome, projectName)
	rc.Settings = detectPlatformSettings(agentsHome, projectName)

	return rc, nil
}

// MergeGenerateAgentsRC overlays a freshly generated manifest onto an existing
// on-disk manifest. Scan-derived lists (skills, rules, agents, hooks, mcp,
// settings) come from generated; an existing non-empty project name, unknown
// JSON keys (ExtraFields), and supplemental sources (e.g. git remotes not
// produced by GenerateAgentsRC) are preserved. Source entries are unioned with
// deduplication so the default local source is not duplicated when merging.
func MergeGenerateAgentsRC(existing, generated *AgentsRC) *AgentsRC {
	if existing == nil {
		return generated
	}
	if generated == nil {
		return existing
	}
	out := *generated
	out.Sources = mergeSourceSlices(generated.Sources, existing.Sources)
	if existing.Project != "" {
		out.Project = existing.Project
	}
	if len(existing.ExtraFields) > 0 {
		out.ExtraFields = cloneExtraFieldsMap(existing.ExtraFields)
	}
	return &out
}

func cloneExtraFieldsMap(m map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func mergeSourceSlices(generated, existing []Source) []Source {
	seen := make(map[string]bool)
	var out []Source
	for _, s := range generated {
		k := sourceMergeKey(s)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	for _, s := range existing {
		k := sourceMergeKey(s)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

func sourceMergeKey(s Source) string {
	switch s.Type {
	case "local":
		return "local:" + s.Path
	case "git":
		return "git:" + s.URL + "\x00" + s.Ref
	default:
		return "type:" + s.Type + "\x00" + s.Path + "\x00" + s.URL + "\x00" + s.Ref
	}
}

// collectScopedDirs returns unique entry names from resource subdirs that contain markerFile.
func collectScopedDirs(agentsHome, resourceType string, scopes []string, markerFile string) []string {
	var names []string
	for _, scope := range scopes {
		dir := filepath.Join(agentsHome, resourceType, scope)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			entryPath := filepath.Join(dir, e.Name())
			if !isDirEntry(entryPath) {
				continue
			}
			if _, err := os.Stat(filepath.Join(entryPath, markerFile)); err == nil {
				names = AppendUnique(names, e.Name())
			}
		}
	}
	return names
}

// detectHookEvents reads the project claude-code.json and returns a StringsOrBool
// listing hook event names that have at least one entry.
func detectHookEvents(agentsHome, projectName string) StringsOrBool {
	for _, scope := range []string{projectName, "global"} {
		hooksDir := filepath.Join(agentsHome, "hooks", scope)
		entries, err := os.ReadDir(hooksDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				if _, err := os.Stat(filepath.Join(hooksDir, entry.Name(), "HOOK.yaml")); err == nil {
					return StringsOrBool{All: true}
				}
			}
		}
	}

	for _, scope := range []string{projectName, "global"} {
		settingsPath := filepath.Join(agentsHome, "settings", scope, "claude-code.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			continue
		}
		var settings map[string]any
		if json.Unmarshal(data, &settings) != nil {
			continue
		}
		hooksVal, ok := settings["hooks"]
		if !ok {
			continue
		}
		hooksMap, ok := hooksVal.(map[string]any)
		if !ok {
			continue
		}
		var hookEvents []string
		for event, val := range hooksMap {
			if list, ok := val.([]any); ok && len(list) > 0 {
				hookEvents = append(hookEvents, event)
			}
		}
		if len(hookEvents) == 0 {
			continue
		}
		sort.Strings(hookEvents)
		return StringsOrBool{Names: hookEvents}
	}
	return StringsOrBool{}
}

// detectMCPServers scans MCP config files for the project and global scopes
// and returns a StringsOrBool listing named server entries.
func detectMCPServers(agentsHome, projectName string) StringsOrBool {
	for _, scope := range []string{projectName, "global"} {
		if result := readMCPScope(agentsHome, scope); result.IsEnabled() {
			return result
		}
	}
	return StringsOrBool{}
}

// readMCPScope tries claude.json then mcp.json for a single scope directory
// and returns the server list from the first readable file.
func readMCPScope(agentsHome, scope string) StringsOrBool {
	for _, fname := range []string{"claude.json", "mcp.json"} {
		mcpPath := filepath.Join(agentsHome, "mcp", scope, fname)
		data, err := os.ReadFile(mcpPath)
		if err != nil {
			continue
		}
		var mcpConfig map[string]any
		if json.Unmarshal(data, &mcpConfig) != nil {
			continue
		}
		servers, ok := mcpConfig["servers"].(map[string]any)
		if !ok {
			break
		}
		var names []string
		for name := range servers {
			names = append(names, name)
		}
		if len(names) > 0 {
			sort.Strings(names)
			return StringsOrBool{Names: names}
		}
		break // found a file, stop trying filenames
	}
	return StringsOrBool{}
}

// detectPlatformSettings returns true if a cursor.json settings file exists
// for the project or global scope.
func detectPlatformSettings(agentsHome, projectName string) bool {
	for _, scope := range []string{projectName, "global"} {
		if _, err := os.Stat(filepath.Join(agentsHome, "settings", scope, "cursor.json")); err == nil {
			return true
		}
	}
	return false
}

func detectRuleScopes(agentsHome, projectName string) []string {
	scopes := []string{"global"}
	projectRulesDir := filepath.Join(agentsHome, "rules", projectName)
	entries, err := os.ReadDir(projectRulesDir)
	if err != nil {
		return scopes
	}
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if ext == ".md" || ext == ".mdc" || ext == ".txt" {
			return append(scopes, "project")
		}
	}
	return scopes
}
