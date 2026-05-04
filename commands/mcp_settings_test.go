package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

func TestRunMCPList(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	testutil.WriteScopeFile(t, agentsHome, "mcp", scope, "mcp.json", []byte("{}"))
	if err := runMCPList(scope); err != nil {
		t.Fatalf("runMCPList: %v", err)
	}
}

func TestRunMCPRemove(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	testutil.WriteScopeFile(t, agentsHome, "mcp", scope, "drop.json", []byte("{}"))
	p := filepath.Join(agentsHome, "mcp", scope, "drop.json")
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
	testutil.WriteScopeFile(t, agentsHome, "settings", scope, "cursor.json", []byte("{}"))
	if err := runSettingsList(scope); err != nil {
		t.Fatalf("runSettingsList: %v", err)
	}
}

func TestRunSettingsRemoveCursorignore(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	testutil.WriteScopeFile(t, agentsHome, "settings", scope, "cursorignore", []byte("x\n"))
	p := filepath.Join(agentsHome, "settings", scope, "cursorignore")
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
