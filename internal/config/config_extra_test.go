package config

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestConfigLoad_MalformedJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	if err := os.WriteFile(filepath.Join(tmp, "config.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Error("expected parse error for malformed config")
	}
}

func TestConfigLoad_NilMapsBackfilled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	// JSON without projects/agents maps
	if err := os.WriteFile(filepath.Join(tmp, "config.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Projects == nil {
		t.Error("Projects should be initialized")
	}
	if cfg.Agents == nil {
		t.Error("Agents should be initialized")
	}
}

func TestConfigListProjects(t *testing.T) {
	cfg := &Config{
		Projects: map[string]Project{
			"a": {Path: "/a"},
			"b": {Path: "/b"},
			"c": {Path: "/c"},
		},
	}
	got := cfg.ListProjects()
	sort.Strings(got)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("idx %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestConfigSetPlatformState(t *testing.T) {
	cfg := &Config{}
	cfg.SetPlatformState("cursor", true, "1.5")
	if cfg.Agents["cursor"].Enabled != true || cfg.Agents["cursor"].Version != "1.5" {
		t.Errorf("after set: %+v", cfg.Agents["cursor"])
	}
	cfg.SetPlatformState("cursor", false, "")
	if cfg.Agents["cursor"].Enabled != false {
		t.Errorf("after disable: %+v", cfg.Agents["cursor"])
	}
}

func TestConfigSetPlatformState_NilMap(t *testing.T) {
	cfg := &Config{Agents: nil}
	cfg.SetPlatformState("cursor", true, "1.0")
	if cfg.Agents == nil {
		t.Fatal("Agents nil after set")
	}
}

func TestIsPlatformEnabled_LegacyClaude(t *testing.T) {
	cfg := &Config{
		Agents: map[string]Agent{
			"claude-code": {Enabled: false},
		},
	}
	if cfg.IsPlatformEnabled("claude") {
		t.Error("legacy claude-code disabled should make claude disabled")
	}
}

func TestIsPlatformEnabled_LegacyCopilot(t *testing.T) {
	cfg := &Config{
		Agents: map[string]Agent{
			"github-copilot": {Enabled: true},
		},
	}
	if !cfg.IsPlatformEnabled("copilot") {
		t.Error("legacy github-copilot enabled should make copilot enabled")
	}
}

func TestGetProjectPath_Missing(t *testing.T) {
	cfg := &Config{Projects: map[string]Project{}}
	if got := cfg.GetProjectPath("nope"); got != "" {
		t.Errorf("missing project should return empty, got %q", got)
	}
}

func TestAddProject_NilMap(t *testing.T) {
	cfg := &Config{Projects: nil}
	in := filepath.FromSlash("/path/a")
	cfg.AddProject("a", in)
	if cfg.Projects == nil {
		t.Fatal("Projects nil after add")
	}
	if want := filepath.Clean(in); cfg.Projects["a"].Path != want {
		t.Errorf("path: got %q want %q", cfg.Projects["a"].Path, want)
	}
}
