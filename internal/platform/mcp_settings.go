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

// resolveCanonicalFileByExt walks the candidate set (name plus
// name+ext for each known ext when name has no dot) under
// agentsHome/<bucket>/<scope>/, returns (foundPath, baseName) for the
// first file that satisfies isValid(). Powers the public Resolve…File
// helpers below.
func resolveCanonicalFileByExt(
	agentsHome, bucket, scope, name string,
	validExts []string,
	isValid func(filename string) bool,
) (foundPath, baseName string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", fmt.Errorf("%s file name is empty", bucket)
	}
	root := filepath.Join(agentsHome, bucket, scope)
	candidates := []string{name}
	if !strings.Contains(name, ".") {
		for _, ext := range validExts {
			candidates = append(candidates, name+ext)
		}
	}
	for _, cand := range candidates {
		p := filepath.Join(root, cand)
		fi, statErr := os.Stat(p)
		if statErr == nil {
			if !fi.IsDir() && isValid(cand) {
				return p, cand, nil
			}
			continue
		}
		if !os.IsNotExist(statErr) {
			// A real Stat failure (permission-denied, I/O error) is not the
			// same as "this candidate doesn't exist" — surface it directly
			// instead of letting the loop fall through to the generic
			// not-found error below, which would mask it as legitimate
			// absence.
			return "", "", fmt.Errorf("checking %s candidate %s: %w", bucket, p, statErr)
		}
	}
	return "", "", fmt.Errorf("%s file not found: %s / %s", bucket, scope, name)
}

// canonicalFileExts is the shared candidate-extension set for both MCP
// and settings file resolution.
var canonicalFileExts = []string{".json", ".toml", ".yaml", ".yml"}

// ResolveCanonicalMCPFile finds an MCP file by scope and name (basename or stem).
func ResolveCanonicalMCPFile(agentsHome, scope, name string) (*MCPFileSpec, error) {
	p, base, err := resolveCanonicalFileByExt(agentsHome, "mcp", scope, name, canonicalFileExts, isMCPFileName)
	if err != nil {
		return nil, err
	}
	return &MCPFileSpec{Scope: scope, BaseName: base, SourcePath: p}, nil
}

// ResolveCanonicalSettingsFile finds a settings file by scope and name (basename or stem).
func ResolveCanonicalSettingsFile(agentsHome, scope, name string) (*SettingsFileSpec, error) {
	p, base, err := resolveCanonicalFileByExt(agentsHome, "settings", scope, name, canonicalFileExts, isSettingsFileName)
	if err != nil {
		return nil, err
	}
	return &SettingsFileSpec{Scope: scope, BaseName: base, SourcePath: p}, nil
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
