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

// ── review.go subcommand RunE coverage ──────────────────────────────────────

func TestNewReviewCmd_RunEDispatches(t *testing.T) {
	setupAgentsHomeAndHome(t)
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	cmd := NewReviewCmd()
	// Top-level RunE → runReviewList()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("review root RunE: %v", err)
	}
	// Subcommand RunE wrappers exist and error on unknown id.
	for _, name := range []string{"show", "approve", "reject"} {
		sub := findSubcommand(t, cmd, name)
		if err := sub.RunE(sub, []string{"nonexistent-id"}); err == nil {
			t.Errorf("review %s RunE should error on unknown id", name)
		}
	}
}

// ── install.go RunE coverage ────────────────────────────────────────────────

func TestNewInstallCmd_RunEGenerate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0o755)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	// Switch CWD to a project dir so install --generate works in dry-run.
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0o755)
	old, _ := os.Getwd()
	os.Chdir(projectPath)
	t.Cleanup(func() {
		os.Chdir(old)
	})

	cmd := NewInstallCmd()
	// Flip --generate by setting flag value before RunE.
	if err := cmd.Flags().Set("generate", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("install --generate RunE dry-run: %v", err)
	}
}

func TestNewInstallCmd_RunEDispatch_NoManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0o755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0o755)
	old, _ := os.Getwd()
	os.Chdir(projectPath)
	t.Cleanup(func() { os.Chdir(old) })

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	cmd := NewInstallCmd()
	// generate=false (default) → invokes runInstall, which errors on missing manifest
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Error("expected error from install RunE when manifest missing")
	}
}

// ── import.go RunE coverage ─────────────────────────────────────────────────

func TestNewImportCmd_RunEDispatches(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0o755)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	cmd := NewImportCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("import RunE no-args: %v", err)
	}
	// With unknown project name → RunE returns an error (still exercises the closure).
	if err := cmd.RunE(cmd, []string{"some-project"}); err == nil {
		t.Error("expected error for unknown project filter")
	}
}

// ── remove.go RunE coverage ─────────────────────────────────────────────────

func TestNewRemoveCmd_RunEDispatchesError(t *testing.T) {
	setupAgentsHomeAndHome(t)
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	cmd := NewRemoveCmd()
	// Unknown project name → runRemove returns an error.
	if err := cmd.RunE(cmd, []string{"ghost-project-name"}); err == nil {
		t.Error("expected remove RunE to error on unknown project")
	}
}
