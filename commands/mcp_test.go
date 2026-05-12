package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunMCPList_ListsMCPConfigs(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mcpDir := filepath.Join(agentsHome, "mcp", "global")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	mcpContent := `{"mcpServers": {"test": {"command": "echo"}}}`
	if err := os.WriteFile(filepath.Join(mcpDir, "test-mcp.json"), []byte(mcpContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runMCPList("global"); err != nil {
		t.Fatalf("runMCPList: %v", err)
	}
}

func TestRunMCPList_EmptyScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mcpDir := filepath.Join(agentsHome, "mcp", "global")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Empty dir — should print info message, not error.
	if err := runMCPList("global"); err != nil {
		t.Fatalf("runMCPList with empty scope: %v", err)
	}
}

func TestRunMCPList_MissingScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := runMCPList("nonexistent"); err != nil {
		t.Fatalf("runMCPList with missing scope: %v", err)
	}
}

func TestRunMCPShow_ReadsMCPConfig(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mcpDir := filepath.Join(agentsHome, "mcp", "global")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	mcpContent := `{"mcpServers": {"demo": {"command": "node"}}}`
	if err := os.WriteFile(filepath.Join(mcpDir, "demo.json"), []byte(mcpContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runMCPShow("global", "demo.json"); err != nil {
		t.Fatalf("runMCPShow: %v", err)
	}
}

func TestRunMCPShow_NotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mcpDir := filepath.Join(agentsHome, "mcp", "global")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	err := runMCPShow("global", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing MCP config")
	}
}

func TestFindMCPSpec_EmptyName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findMCPSpec(agentsHome, "global", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}
