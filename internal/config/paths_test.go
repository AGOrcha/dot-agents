package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentsHomeUsesExplicitOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "agents-home")
	t.Setenv("AGENTS_HOME", override)
	if got := AgentsHome(); got != override {
		t.Fatalf("AgentsHome() = %q, want %q", got, override)
	}
}

func TestAgentsHomeDefaultsUnderUserHome(t *testing.T) {
	t.Setenv("AGENTS_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".agents")
	if got := AgentsHome(); got != want {
		t.Fatalf("AgentsHome() = %q, want %q", got, want)
	}
}