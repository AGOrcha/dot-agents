package settings

import (
	"os"
	"path/filepath"
	"testing"
)

// setupAgentsHome points HOME and AGENTS_HOME at a fresh tempdir and returns
// the agentsHome path. Mirrors commands.setupAgentsHomeAndHome so the moved
// RunE-coverage tests behave identically post-extraction.
func setupAgentsHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	fakeHome := filepath.Join(tmp, "home")
	if err := os.MkdirAll(fakeHome, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatalf("mkdir agentsHome: %v", err)
	}
	t.Setenv("HOME", agentsHome[:len(agentsHome)-len("/.agents")])
	t.Setenv("AGENTS_HOME", agentsHome)
	return agentsHome
}

// ── cmd.go RunE coverage ────────────────────────────────────────────────────

func TestNewListCmd_RunE(t *testing.T) {
	setupAgentsHome(t)
	deps := noOpDeps()
	cmd := NewListCmd(deps)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("settings list RunE: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"some-project"}); err != nil {
		t.Errorf("settings list with scope RunE: %v", err)
	}
}

func TestNewShowCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHome(t)
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "cursor.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := noOpDeps()
	cmd := NewShowCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "cursor.json"}); err != nil {
		t.Errorf("settings show RunE: %v", err)
	}
}

func TestNewRemoveCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHome(t)
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "kill.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := noOpDeps()
	deps.Flags.Yes = true
	cmd := NewRemoveCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "kill.json"}); err != nil {
		t.Errorf("settings remove RunE: %v", err)
	}
}

// ── canonicalFlags projection coverage ──────────────────────────────────────

func TestDeps_CanonicalFlagsProjection(t *testing.T) {
	d := Deps{Flags: GlobalFlags{DryRun: true, Yes: true, Force: true}}
	cf := d.canonicalFlags()
	if !(cf.DryRun && cf.Yes && cf.Force) {
		t.Errorf("canonicalFlags lost a flag: %+v", cf)
	}
	d2 := Deps{}
	cf2 := d2.canonicalFlags()
	if cf2.DryRun || cf2.Yes || cf2.Force {
		t.Errorf("empty Deps should produce zeroed flags: %+v", cf2)
	}
}
