package config

import (
	"os"
	"path/filepath"
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
	if path != "/home/user/myproject" {
		t.Errorf("expected /home/user/myproject, got %q", path)
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
	tests := []struct {
		input    string
		expected string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"~", home},
	}
	for _, tt := range tests {
		got := ExpandPath(tt.input)
		if got != tt.expected {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDisplayPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	got := DisplayPath(filepath.Join(home, "foo", "bar"))
	if got != "~/foo/bar" {
		t.Errorf("expected ~/foo/bar, got %q", got)
	}
}
