package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MCPFileSpec describes one canonical MCP config file under ~/.agents/mcp/<scope>/.
type MCPFileSpec struct {
	Scope      string
	BaseName   string
	SourcePath string
}

// SettingsFileSpec describes one canonical settings file under ~/.agents/settings/<scope>/.
type SettingsFileSpec struct {
	Scope      string
	BaseName   string
	SourcePath string
}

func isMCPFileName(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".json", ".toml", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func isSettingsFileName(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	if name == "cursorignore" {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".json", ".toml", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

// ListCanonicalMCPFiles returns non-directory MCP config files under ~/.agents/mcp/<scope>/,
// sorted by basename. If the scope directory is missing, the error satisfies os.IsNotExist.
func ListCanonicalMCPFiles(agentsHome, scope string) ([]MCPFileSpec, error) {
	root := filepath.Join(agentsHome, "mcp", scope)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []MCPFileSpec
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isMCPFileName(name) {
			continue
		}
		out = append(out, MCPFileSpec{
			Scope:      scope,
			BaseName:   name,
			SourcePath: filepath.Join(root, name),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].BaseName) < strings.ToLower(out[j].BaseName)
	})
	return out, nil
}

// ListCanonicalSettingsFiles returns non-directory settings files under ~/.agents/settings/<scope>/.
func ListCanonicalSettingsFiles(agentsHome, scope string) ([]SettingsFileSpec, error) {
	root := filepath.Join(agentsHome, "settings", scope)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []SettingsFileSpec
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isSettingsFileName(name) {
			continue
		}
		out = append(out, SettingsFileSpec{
			Scope:      scope,
			BaseName:   name,
			SourcePath: filepath.Join(root, name),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].BaseName) < strings.ToLower(out[j].BaseName)
	})
	return out, nil
}

// ResolveCanonicalMCPFile finds an MCP file by scope and name (basename or stem).
func ResolveCanonicalMCPFile(agentsHome, scope, name string) (*MCPFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("mcp file name is empty")
	}
	root := filepath.Join(agentsHome, "mcp", scope)
	candidates := []string{name}
	if !strings.Contains(name, ".") {
		for _, ext := range []string{".json", ".toml", ".yaml", ".yml"} {
			candidates = append(candidates, name+ext)
		}
	}
	for _, cand := range candidates {
		p := filepath.Join(root, cand)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && isMCPFileName(cand) {
			return &MCPFileSpec{
				Scope:      scope,
				BaseName:   cand,
				SourcePath: p,
			}, nil
		}
	}
	return nil, fmt.Errorf("mcp file not found: %s / %s", scope, name)
}

// ResolveCanonicalSettingsFile finds a settings file by scope and name (basename or stem).
func ResolveCanonicalSettingsFile(agentsHome, scope, name string) (*SettingsFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("settings file name is empty")
	}
	root := filepath.Join(agentsHome, "settings", scope)
	candidates := []string{name}
	if !strings.Contains(name, ".") {
		for _, ext := range []string{".json", ".toml", ".yaml", ".yml"} {
			candidates = append(candidates, name+ext)
		}
	}
	for _, cand := range candidates {
		p := filepath.Join(root, cand)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && isSettingsFileName(cand) {
			return &SettingsFileSpec{
				Scope:      scope,
				BaseName:   cand,
				SourcePath: p,
			}, nil
		}
	}
	return nil, fmt.Errorf("settings file not found: %s / %s", scope, name)
}

// EnsureUnderMCPScopeTree checks that target is under agentsHome/mcp/scope.
func EnsureUnderMCPScopeTree(agentsHome, scope, target string) error {
	return ensureUnderScopedBucketTree(agentsHome, "mcp", scope, target)
}

// EnsureUnderSettingsScopeTree checks that target is under agentsHome/settings/scope.
func EnsureUnderSettingsScopeTree(agentsHome, scope, target string) error {
	return ensureUnderScopedBucketTree(agentsHome, "settings", scope, target)
}

func ensureUnderScopedBucketTree(agentsHome, bucket, scope, target string) error {
	root := filepath.Join(agentsHome, bucket, scope)
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to touch path outside %s", cleanRoot)
	}
	return nil
}
