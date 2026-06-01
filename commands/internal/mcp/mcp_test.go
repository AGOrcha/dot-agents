package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestRunList_ListsMCPConfigs, TestRunList_EmptyScope, TestRunList_MissingScope,
// TestRunShow_ReadsMCPConfig, TestRunShow_NotFound, TestRunRemove_DryRun_KeepsFile,
// and TestRunRemove_Force_DeletesFile are the testutil-consuming flow tests
// transplanted verbatim from commands/mcp_test.go as required by the t10a
// bundle. Every testutil.WriteScopeFile call site is preserved 1:1; only the
// package decl and the run* identifiers (now exported in this subpackage)
// changed.

func TestRunList_ListsMCPConfigs(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "test-mcp.json", []byte(`{"mcpServers": {"test": {"command": "echo"}}}`))

	if err := RunList("global"); err != nil {
		t.Fatalf("RunList: %v", err)
	}
}

func TestRunList_EmptyScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mcpDir := filepath.Join(agentsHome, "mcp", "global")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Empty dir — should print info message, not error.
	if err := RunList("global"); err != nil {
		t.Fatalf("RunList with empty scope: %v", err)
	}
}

func TestRunList_MissingScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := RunList("nonexistent"); err != nil {
		t.Fatalf("RunList with missing scope: %v", err)
	}
}

func TestRunShow_ReadsMCPConfig(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "demo.json", []byte(`{"mcpServers": {"demo": {"command": "node"}}}`))

	if err := RunShow(Deps{}, "global", "demo.json"); err != nil {
		t.Fatalf("RunShow: %v", err)
	}
}

func TestRunShow_NotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mcpDir := filepath.Join(agentsHome, "mcp", "global")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	err := RunShow(Deps{}, "global", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing MCP config")
	}
}

// makeDeps builds a Deps for the in-package coverage tests. Mirrors the
// previous package-private makeMCPDeps helper in commands/mcp_test.go.
func makeDeps(dryRun, yes, force bool) Deps {
	return Deps{
		Flags:              cmdutil.CanonicalCmdFlags{DryRun: dryRun, Yes: yes, Force: force},
		MaxArgsWithHints:   stubMaxArgs,
		ExactArgsWithHints: stubExactArgs,
	}
}

func TestRunRemove_DryRun_KeepsFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "dry.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := makeDeps(true, false, false)
	if err := RunRemove(deps, "global", "dry.json"); err != nil {
		t.Fatalf("RunRemove dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "dry.json")); err != nil {
		t.Fatalf("dry-run should preserve file: %v", err)
	}
}

func TestRunRemove_Force_DeletesFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "kill.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := makeDeps(false, true, false)
	if err := RunRemove(deps, "global", "kill.json"); err != nil {
		t.Fatalf("RunRemove force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "kill.json")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed; stat err = %v", err)
	}
}
