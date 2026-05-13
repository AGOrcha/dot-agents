package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// findSubcommand returns the named subcommand of root or fails the test.
func findSubcommand(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}

// setupAgentsHome creates a fake AGENTS_HOME and HOME at t.TempDir for tests
// that exercise subcommands which inspect ~/.agents.
func setupAgentsHomeAndHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	fakeHome := filepath.Join(tmp, "home")
	for _, d := range []string{agentsHome, fakeHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)
	return agentsHome
}

// ── newSyncPullCmd ───────────────────────────────────────────────────────────

func TestNewSyncPullCmd_ReturnsPullSubcommand(t *testing.T) {
	cmd := newSyncPullCmd()
	if cmd == nil {
		t.Fatal("newSyncPullCmd returned nil")
	}
	if cmd.Name() != "pull" {
		t.Errorf("name = %q; want 'pull'", cmd.Name())
	}
}

// ── skills.go RunE coverage ─────────────────────────────────────────────────

func TestNewSkillsListCmd_RunE(t *testing.T) {
	setupAgentsHomeAndHome(t)
	cmd := newSkillsListCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("skills list RunE: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"some-project"}); err != nil {
		t.Errorf("skills list with scope RunE: %v", err)
	}
}

func TestNewSkillsNewCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	cmd := newSkillsNewCmd()
	if err := cmd.RunE(cmd, []string{"runE-skill"}); err != nil {
		t.Fatalf("skills new RunE: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "skills", "global", "runE-skill", "SKILL.md")); err != nil {
		t.Errorf("expected SKILL.md: %v", err)
	}
}

func TestNewSkillsPromoteCmd_RunEErrorWhenNoSkill(t *testing.T) {
	setupAgentsHomeAndHome(t)
	// Set CWD to a path without .agents/skills/<name> so PromoteSkillIn errors.
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	cmd := newSkillsPromoteCmd()
	if err := cmd.RunE(cmd, []string{"missing"}); err == nil {
		t.Error("expected error for missing skill")
	}
}

// ── mcp.go RunE coverage ────────────────────────────────────────────────────

func TestNewMCPListCmd_RunE(t *testing.T) {
	setupAgentsHomeAndHome(t)
	deps := makeMCPDeps(false, false, false)
	cmd := newMCPListCmd(deps)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("mcp list RunE: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"some-project"}); err != nil {
		t.Errorf("mcp list with scope RunE: %v", err)
	}
}

func TestNewMCPShowCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	writeMCPConfig(t, filepath.Join(agentsHome, "mcp", "global"), "showme.json", "{}")
	deps := makeMCPDeps(false, false, false)
	cmd := newMCPShowCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "showme.json"}); err != nil {
		t.Errorf("mcp show RunE: %v", err)
	}
}

func TestNewMCPRemoveCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	writeMCPConfig(t, filepath.Join(agentsHome, "mcp", "global"), "rmme.json", "{}")
	deps := makeMCPDeps(false, true, false) // Yes:true to bypass prompt
	cmd := newMCPRemoveCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "rmme.json"}); err != nil {
		t.Errorf("mcp remove RunE: %v", err)
	}
}

// ── rules.go RunE coverage ──────────────────────────────────────────────────

func TestNewRulesListCmd_RunE(t *testing.T) {
	setupAgentsHomeAndHome(t)
	deps := rulesCommandDeps()
	cmd := newRulesListCmd(deps)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("rules list RunE: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"some-project"}); err != nil {
		t.Errorf("rules list with scope RunE: %v", err)
	}
}

func TestNewRulesShowCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	rulesDir := filepath.Join(agentsHome, "rules", "global")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "demo.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := rulesCommandDeps()
	cmd := newRulesShowCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "demo.md"}); err != nil {
		t.Errorf("rules show RunE: %v", err)
	}
}

func TestNewRulesRemoveCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	rulesDir := filepath.Join(agentsHome, "rules", "global")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "kill.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := rulesCommandDeps()
	deps.Flags.Yes = true
	cmd := newRulesRemoveCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "kill.md"}); err != nil {
		t.Errorf("rules remove RunE: %v", err)
	}
}

// ── settings.go RunE coverage ──────────────────────────────────────────────

func TestNewSettingsListCmd_RunE(t *testing.T) {
	setupAgentsHomeAndHome(t)
	deps := settingsCommandDeps()
	cmd := newSettingsListCmd(deps)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("settings list RunE: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"some-project"}); err != nil {
		t.Errorf("settings list with scope RunE: %v", err)
	}
}

func TestNewSettingsShowCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "cursor.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := settingsCommandDeps()
	cmd := newSettingsShowCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "cursor.json"}); err != nil {
		t.Errorf("settings show RunE: %v", err)
	}
}

func TestNewSettingsRemoveCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "kill.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := settingsCommandDeps()
	deps.Flags.Yes = true
	cmd := newSettingsRemoveCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "kill.json"}); err != nil {
		t.Errorf("settings remove RunE: %v", err)
	}
}
