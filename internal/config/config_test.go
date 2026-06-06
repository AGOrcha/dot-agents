package config

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestConfigLoadSave(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)

	// Load from empty dir → returns default
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if cfg.Projects == nil {
		t.Error("expected non-nil Projects map")
	}

	// Add project and save
	cfg.AddProject("myproject", "/home/user/myproject")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload and verify
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	path := cfg2.GetProjectPath("myproject")
	want := filepath.Clean(filepath.FromSlash("/home/user/myproject"))
	if path != want {
		t.Errorf("expected %q, got %q", want, path)
	}
}

func TestConfigAddRemoveProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)

	cfg := &Config{
		Version:  1,
		Projects: make(map[string]Project),
		Agents:   make(map[string]Agent),
	}

	cfg.AddProject("alpha", "/projects/alpha")
	cfg.AddProject("beta", "/projects/beta")

	if len(cfg.Projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(cfg.Projects))
	}

	// Verify Added timestamp is set
	if cfg.Projects["alpha"].Added.IsZero() {
		t.Error("expected non-zero Added time")
	}
	if cfg.Projects["alpha"].Added.After(time.Now().Add(time.Second)) {
		t.Error("Added time is in the future")
	}

	cfg.RemoveProject("alpha")
	if _, ok := cfg.Projects["alpha"]; ok {
		t.Error("alpha should have been removed")
	}
	if _, ok := cfg.Projects["beta"]; !ok {
		t.Error("beta should still be present")
	}
}

func TestConfigPlatformEnabled(t *testing.T) {
	cfg := &Config{
		Version:  1,
		Projects: make(map[string]Project),
		Agents: map[string]Agent{
			"cursor": {Enabled: true, Version: "1.0"},
			"claude": {Enabled: false, Version: ""},
		},
	}

	if !cfg.IsPlatformEnabled("cursor") {
		t.Error("cursor should be enabled")
	}
	if cfg.IsPlatformEnabled("claude") {
		t.Error("claude should be disabled")
	}
	// Unknown platforms default to enabled
	if !cfg.IsPlatformEnabled("unknown") {
		t.Error("unknown platform should default to enabled")
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	tests := []struct {
		input    string
		expected string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"~", home},
		{"./foo/.", filepath.Clean(filepath.Join(cwd, "foo"))},
	}
	for _, tt := range tests {
		got := ExpandPath(tt.input)
		if got != tt.expected {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestConfigGetProjectPathCleansStoredPath(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Projects: map[string]Project{
			"proj": {
				Path: filepath.FromSlash("/tmp/example/."),
			},
		},
	}
	want := filepath.Clean(filepath.FromSlash("/tmp/example"))
	if got := cfg.GetProjectPath("proj"); got != want {
		t.Fatalf("GetProjectPath returned %q, want %q", got, want)
	}
}

func TestDisplayPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	got := DisplayPath(filepath.Join(home, "foo", "bar"))
	if got != "~/foo/bar" {
		t.Errorf("expected ~/foo/bar, got %q", got)
	}
}

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

// TestConfigLoad_ReadErrorNotNotExist covers the non-NotExist read-error branch:
// a config.json that is a directory yields a read error that is not IsNotExist.
func TestConfigLoad_ReadErrorNotNotExist(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	if err := os.Mkdir(filepath.Join(tmp, "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected read error when config.json is a directory")
	}
}

// TestConfigSave_MkdirError covers Save's MkdirAll-failure branch: AGENTS_HOME
// nested under a regular file makes the config dir uncreatable.
func TestConfigSave_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", filepath.Join(blocker, "sub"))
	c := &Config{Version: 1}
	if err := c.Save(); err == nil {
		t.Fatal("expected MkdirAll error when AGENTS_HOME is under a regular file")
	}
}
