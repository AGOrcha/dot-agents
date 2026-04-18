package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/commands/kg"
)

func TestEnsureProjectKGMCPConfigs(t *testing.T) {
	projectDir := t.TempDir()
	agentsHome := t.TempDir()

	rcPath := filepath.Join(projectDir, ".agentsrc.json")
	if err := os.WriteFile(rcPath, []byte(`{"version":1,"project":"demo","sources":[{"type":"local"}],"kg":{"enabled":true}}`), 0644); err != nil {
		t.Fatalf("write agentsrc: %v", err)
	}

	if err := ensureProjectKGMCPConfigs("demo", projectDir, agentsHome); err != nil {
		t.Fatalf("ensureProjectKGMCPConfigs: %v", err)
	}

	for _, name := range []string{"claude.json", "cursor.json", "mcp.json"} {
		path := filepath.Join(agentsHome, "mcp", "demo", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var payload struct {
			Servers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
				Type    string   `json:"type"`
			} `json:"servers"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("unmarshal %s: %v", name, err)
		}
		server, ok := payload.Servers["dot-agents-kg"]
		if !ok {
			t.Fatalf("%s missing dot-agents-kg entry: %+v", name, payload.Servers)
		}
		if server.Command == "" || server.Type != "stdio" {
			t.Fatalf("%s invalid server config: %+v", name, server)
		}
		if len(server.Args) != 2 || server.Args[0] != "kg" || server.Args[1] != "serve" {
			t.Fatalf("%s unexpected args: %+v", name, server.Args)
		}
	}
}

func TestEnsureGlobalKGMCPConfigs(t *testing.T) {
	kgHomeDir := t.TempDir()
	t.Setenv("KG_HOME", kgHomeDir)
	agentsHome := t.TempDir()

	if err := kg.SaveKGConfig(&kg.KGConfig{SchemaVersion: 1, Name: "kg", CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatalf("SaveKGConfig: %v", err)
	}
	if err := ensureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Fatalf("ensureGlobalKGMCPConfigs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "mcp.json")); err != nil {
		t.Fatalf("expected global kg mcp config: %v", err)
	}
}
