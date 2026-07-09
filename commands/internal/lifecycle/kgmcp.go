package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/projectsync"
)

// AddDeps is the multi-method collaborator the lifecycle backup /
// restore / KG-MCP-config helpers need (interface-DI per
// docs/TEST_SEAMS.md). Lifted from commands/add.go in plan
// root-command-decomposition t02b so the helpers can live outside the
// root commands package without dragging in the rest of add.go.
//
// The six operations are the add pipeline's fault-injectable touch
// points: filesystem materialization of resource trees and MCP config
// parents (MkdirAll), the MCP config payload itself (WriteFile), the
// destructive removal of an unmanaged config after a successful backup
// (Remove), the dot-agents binary path used to build the KG MCP server
// command (Executable), the resource copy used to back up and restore
// unmanaged configs (CopyFile), and config.json load for project
// registration lookups (LoadConfig).
type AddDeps interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(name string, data []byte, perm os.FileMode) error
	Remove(name string) error
	Executable() (string, error)
	CopyFile(src, dst string) error
	LoadConfig() (*config.Config, error)
}

// StdAddDeps is the production AddDeps backed by direct os /
// projectsync / config calls. Production callers construct StdAddDeps{}
// inline; tests substitute a fake AddDeps implementation.
type StdAddDeps struct{}

func (StdAddDeps) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (StdAddDeps) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (StdAddDeps) Remove(name string) error            { return os.Remove(name) }
func (StdAddDeps) Executable() (string, error)         { return os.Executable() }
func (StdAddDeps) CopyFile(src, dst string) error      { return projectsync.CopyFile(src, dst) }
func (StdAddDeps) LoadConfig() (*config.Config, error) { return config.Load() }

// KGConfigPath returns the path to KG_HOME/self/config.yaml without
// importing the kg subpackage. Used by EnsureGlobalKGMCPConfigs and
// re-aliased into commands/add.go for the legacy kgConfigPath test
// site until commands/init.go and commands/seams_test.go move.
func KGConfigPath() string {
	if v := os.Getenv("KG_HOME"); v != "" {
		return filepath.Join(v, "self", "config.yaml")
	}
	home, _ := config.UserHomeDir()
	return filepath.Join(home, "knowledge-graph", "self", "config.yaml")
}

// EnsureGlobalKGMCPConfigs writes the KG MCP config files into the
// global mcp scope when a KG_HOME/self/config.yaml exists. No-op when
// the KG self config is absent. Lifted from commands/add.go in
// root-command-decomposition t02b.
func EnsureGlobalKGMCPConfigs(agentsHome string) error {
	if _, err := os.Stat(KGConfigPath()); err != nil {
		return nil
	}
	return WriteKGMCPConfigs(filepath.Join(agentsHome, "mcp", "global"), StdAddDeps{})
}

// WriteKGMCPConfigs writes the dot-agents-kg MCP server entry into the
// three canonical platform config files (claude.json, cursor.json,
// mcp.json) under scopeDir. Lifted from commands/add.go in
// root-command-decomposition t02b.
func WriteKGMCPConfigs(scopeDir string, deps AddDeps) error {
	exe, err := deps.Executable()
	if err != nil {
		return err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		exe = resolved
	}
	server := map[string]any{
		"command": exe,
		"args":    []string{"kg", "serve"},
		"type":    "stdio",
	}
	for _, name := range []string{"claude.json", "cursor.json", "mcp.json"} {
		if err := WriteKGMCPConfigFile(filepath.Join(scopeDir, name), server, deps); err != nil {
			return err
		}
	}
	return nil
}

// WriteKGMCPConfigFile merges the dot-agents-kg server entry into the
// JSON file at path, preserving any existing top-level keys and any
// other server entries. If path exists but is not readable or not
// valid JSON, the write is aborted and an error is returned rather
// than overwriting the file with a fresh, partial config — a corrupt
// existing file must never silently lose every other server entry.
// Lifted from commands/add.go in root-command-decomposition t02b.
func WriteKGMCPConfigFile(path string, server map[string]any, deps AddDeps) error {
	configMap := map[string]any{}
	switch data, err := os.ReadFile(path); {
	case err == nil:
		if err := json.Unmarshal(data, &configMap); err != nil {
			return fmt.Errorf("existing MCP config %s is not valid JSON, refusing to overwrite it: %w", path, err)
		}
	case os.IsNotExist(err):
		// No existing config file: start from an empty config map.
	default:
		return fmt.Errorf("reading existing MCP config %s: %w", path, err)
	}
	servers, _ := configMap["servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["dot-agents-kg"] = server
	configMap["servers"] = servers

	data, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := deps.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return deps.WriteFile(path, data, 0644)
}
