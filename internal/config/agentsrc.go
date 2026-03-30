package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// isDirEntry reports whether the path is a directory, following symlinks.
func isDirEntry(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// AgentsRC represents the .agentsrc.json manifest committed to a project repo.
type AgentsRC struct {
	Schema   string   `json:"$schema,omitempty"`
	Version  int      `json:"version"`
	Project  string   `json:"project,omitempty"`
	Skills   []string `json:"skills,omitempty"`
	Rules    []string `json:"rules,omitempty"`
	Agents   []string `json:"agents,omitempty"`
	Hooks    bool     `json:"hooks"`
	MCP      bool     `json:"mcp"`
	Settings bool     `json:"settings"`
	Sources  []Source `json:"sources"`
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

// GenerateAgentsRC inspects ~/.agents/ and builds a manifest for the given project.
func GenerateAgentsRC(projectName, projectPath string) (*AgentsRC, error) {
	agentsHome := AgentsHome()

	rc := &AgentsRC{
		Schema:   "https://dot-agents.dev/schemas/agentsrc.json",
		Version:  1,
		Project:  projectName,
		Hooks:    false,
		MCP:      false,
		Settings: false,
		Sources:  []Source{{Type: "local"}},
	}

	scopes := []string{"global", projectName}
	rc.Skills = collectScopedResourceNames(agentsHome, "skills", scopes, "SKILL.md")
	rc.Agents = collectScopedResourceNames(agentsHome, "agents", scopes, "AGENT.md")
	rc.Rules = detectRuleScopes(agentsHome, projectName)
	rc.Hooks = hasProjectClaudeHooks(agentsHome, projectName)
	rc.MCP = hasScopedMCPConfigs(agentsHome, projectName)
	rc.Settings = hasScopedCursorSettings(agentsHome, projectName)

	return rc, nil
}

func collectScopedResourceNames(agentsHome, resourceType string, scopes []string, markerFile string) []string {
	names := []string{}
	for _, scope := range scopes {
		dir := filepath.Join(agentsHome, resourceType, scope)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			entryPath := filepath.Join(dir, entry.Name())
			if !isDirEntry(entryPath) {
				continue
			}
			if _, err := os.Stat(filepath.Join(entryPath, markerFile)); err == nil {
				names = append(names, entry.Name())
			}
		}
	}
	return names
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

func hasProjectClaudeHooks(agentsHome, projectName string) bool {
	_, err := os.Stat(filepath.Join(agentsHome, "settings", projectName, "claude-code.json"))
	return err == nil
}

func hasScopedMCPConfigs(agentsHome, projectName string) bool {
	for _, scope := range []string{projectName, "global"} {
		dir := filepath.Join(agentsHome, "mcp", scope)
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

func hasScopedCursorSettings(agentsHome, projectName string) bool {
	for _, scope := range []string{projectName, "global"} {
		if _, err := os.Stat(filepath.Join(agentsHome, "settings", scope, "cursor.json")); err == nil {
			return true
		}
	}
	return false
}
