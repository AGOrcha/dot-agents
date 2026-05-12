package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunSettingsList_ListsSettings(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	settingsContent := `{"editor.fontSize": 14}`
	if err := os.WriteFile(filepath.Join(settingsDir, "cursor.json"), []byte(settingsContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runSettingsList("global"); err != nil {
		t.Fatalf("runSettingsList: %v", err)
	}
}

func TestRunSettingsList_EmptyScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := runSettingsList("global"); err != nil {
		t.Fatalf("runSettingsList with empty scope: %v", err)
	}
}

func TestRunSettingsList_MissingScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := runSettingsList("nonexistent"); err != nil {
		t.Fatalf("runSettingsList with missing scope: %v", err)
	}
}

func TestRunSettingsShow_ReadsSettingsFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	settingsContent := `{"theme": "dark"}`
	if err := os.WriteFile(filepath.Join(settingsDir, "claude-code.json"), []byte(settingsContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runSettingsShow("global", "claude-code.json"); err != nil {
		t.Fatalf("runSettingsShow: %v", err)
	}
}

func TestRunSettingsShow_NotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	err := runSettingsShow("global", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing settings file")
	}
}

func TestFindSettingsSpec_EmptyName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findSettingsSpec(agentsHome, "global", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}
