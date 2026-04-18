package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunMCPList(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	dir := filepath.Join(agentsHome, "mcp", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runMCPList(scope); err != nil {
		t.Fatalf("runMCPList: %v", err)
	}
}

func TestRunMCPRemove(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	dir := filepath.Join(agentsHome, "mcp", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "drop.json")
	if err := os.WriteFile(p, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	deps := mcpCommandDeps()
	deps.Flags.Yes = true
	if err := runMCPRemove(deps, scope, "drop"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("expected file removed")
	}
}

func TestFindMCPSpecNotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if _, err := findMCPSpec(agentsHome, "x", "missing"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunSettingsList(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	dir := filepath.Join(agentsHome, "settings", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cursor.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runSettingsList(scope); err != nil {
		t.Fatalf("runSettingsList: %v", err)
	}
}

func TestRunSettingsRemoveCursorignore(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	dir := filepath.Join(agentsHome, "settings", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "cursorignore")
	if err := os.WriteFile(p, []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	deps := settingsCommandDeps()
	deps.Flags.Yes = true
	if err := runSettingsRemove(deps, scope, "cursorignore"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("expected file removed")
	}
}

func TestFindSettingsSpecNotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if _, err := findSettingsSpec(agentsHome, "x", "missing"); err == nil {
		t.Fatal("expected error")
	}
}
