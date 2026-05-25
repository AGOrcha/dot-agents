package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- KGConfigPath ----------

func TestKGConfigPath_UsesKGHomeEnv(t *testing.T) {
	t.Setenv("KG_HOME", filepath.FromSlash("/custom/kg"))
	got := KGConfigPath()
	want := filepath.FromSlash("/custom/kg/self/config.yaml")
	if got != want {
		t.Errorf("KGConfigPath() = %q, want %q", got, want)
	}
}

func TestKGConfigPath_FallsBackToHome(t *testing.T) {
	t.Setenv("KG_HOME", "")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	got := KGConfigPath()
	if !strings.HasSuffix(got, filepath.Join("knowledge-graph", "self", "config.yaml")) {
		t.Errorf("unexpected fallback path: %q", got)
	}
}

// ---------- WriteKGMCPConfigs ----------

func TestWriteKGMCPConfigs_WritesThreeFiles(t *testing.T) {
	tmp := t.TempDir()
	if err := WriteKGMCPConfigs(tmp, StdAddDeps{}); err != nil {
		t.Fatalf("WriteKGMCPConfigs: %v", err)
	}
	for _, name := range []string{"claude.json", "cursor.json", "mcp.json"} {
		data, err := os.ReadFile(filepath.Join(tmp, name))
		if err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		servers, _ := parsed["servers"].(map[string]any)
		if servers == nil || servers["dot-agents-kg"] == nil {
			t.Errorf("%s missing dot-agents-kg server entry: %+v", name, parsed)
		}
	}
}

// ---------- WriteKGMCPConfigFile ----------

func TestWriteKGMCPConfigFile_MergesExistingServers(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "claude.json")
	// Pre-existing file with another server
	pre := map[string]any{
		"servers": map[string]any{"other": map[string]any{"command": "foo"}},
	}
	data, _ := json.Marshal(pre)
	_ = os.WriteFile(target, data, 0644)

	server := map[string]any{"command": "exe", "args": []string{"kg", "serve"}, "type": "stdio"}
	if err := WriteKGMCPConfigFile(target, server, StdAddDeps{}); err != nil {
		t.Fatalf("WriteKGMCPConfigFile: %v", err)
	}

	out, _ := os.ReadFile(target)
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	servers, _ := parsed["servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("expected to preserve existing servers.other entry")
	}
	if _, ok := servers["dot-agents-kg"]; !ok {
		t.Error("expected dot-agents-kg server entry")
	}
}

// ---------- EnsureGlobalKGMCPConfigs ----------

func TestEnsureGlobalKGMCPConfigs_NoopWhenKGAbsent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("KG_HOME", filepath.Join(tmp, "no-kg-here"))

	agentsHome := filepath.Join(tmp, ".agents")
	if err := EnsureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Errorf("EnsureGlobalKGMCPConfigs should be no-op when KG missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "claude.json")); err == nil {
		t.Error("expected no MCP config to be written")
	}
}

func TestEnsureGlobalKGMCPConfigs_WritesWhenKGPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	kgHome := filepath.Join(tmp, "kgroot")
	_ = os.MkdirAll(filepath.Join(kgHome, "self"), 0755)
	_ = os.WriteFile(filepath.Join(kgHome, "self", "config.yaml"), []byte("k: v\n"), 0644)
	t.Setenv("KG_HOME", kgHome)

	agentsHome := filepath.Join(tmp, ".agents")
	if err := EnsureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Fatalf("EnsureGlobalKGMCPConfigs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "claude.json")); err != nil {
		t.Errorf("expected claude.json: %v", err)
	}
}
