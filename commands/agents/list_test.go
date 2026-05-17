package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAgents_PrintsAgents(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	agentsDir := filepath.Join(agentsHome, "agents", "global", "test-agent")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	manifest := "---\nname: test-agent\ndescription: A test agent\n---\n# Test Agent\n"
	if err := os.WriteFile(filepath.Join(agentsDir, agentManifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	if err := listAgents("global"); err != nil {
		t.Fatalf("listAgents: %v", err)
	}
}

func TestListAgents_EmptyScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// No agents dir — should print info message, not error.
	if err := listAgents("global"); err != nil {
		t.Fatalf("listAgents with empty scope: %v", err)
	}
}
