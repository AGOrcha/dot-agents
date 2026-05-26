package rules

import (
	"os"
	"path/filepath"
	"testing"
)

// setupAgentsHomeAndHome creates fake AGENTS_HOME and HOME dirs and sets the
// env vars for the duration of the test. Mirrors the helper in skills/.
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

// TestNewListCmd_RunE exercises both the no-args and single-scope paths.
func TestNewListCmd_RunE(t *testing.T) {
	setupAgentsHomeAndHome(t)
	cmd := NewListCmd(testDeps(false, false, false))
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("rules list RunE: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"some-project"}); err != nil {
		t.Errorf("rules list with scope RunE: %v", err)
	}
}

func TestNewShowCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	rulesDir := filepath.Join(agentsHome, "rules", "global")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "demo.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewShowCmd(testDeps(false, false, false))
	if err := cmd.RunE(cmd, []string{"global", "demo.md"}); err != nil {
		t.Errorf("rules show RunE: %v", err)
	}
}

func TestNewRemoveCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	rulesDir := filepath.Join(agentsHome, "rules", "global")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "kill.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRemoveCmd(testDeps(false, true, false))
	if err := cmd.RunE(cmd, []string{"global", "kill.md"}); err != nil {
		t.Errorf("rules remove RunE: %v", err)
	}
}
