package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

func TestRunSettingsList_ListsSettings(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "cursor.json", []byte(`{"editor.fontSize": 14}`))

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
	t.Setenv("AGENTS_HOME", agentsHome)

	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "claude-code.json", []byte(`{"theme": "dark"}`))

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

func makeSettingsDeps(dryRun, yes, force bool) settingsDeps {
	return settingsDeps{
		Flags:              canonicalCmdFlags{DryRun: dryRun, Yes: yes, Force: force},
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}
}

func TestFindSettingsSpec_Found(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "cursor.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	spec, err := findSettingsSpec(agentsHome, "global", "cursor.json")
	if err != nil {
		t.Fatalf("findSettingsSpec: %v", err)
	}
	if spec == nil || spec.BaseName != "cursor.json" {
		t.Errorf("unexpected spec: %+v", spec)
	}
}

func TestFindSettingsSpec_NotFoundHintsAtList(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findSettingsSpec(agentsHome, "global", "absent")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if !strings.Contains(strings.Join(cliErr.Hints, " "), "da settings list") {
		t.Errorf("missing hint pointing at `da settings list`: %v", cliErr.Hints)
	}
}

func TestRunSettingsRemove_DryRun_KeepsFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "cursor.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := makeSettingsDeps(true, false, false)
	if err := runSettingsRemove(deps, "global", "cursor.json"); err != nil {
		t.Fatalf("runSettingsRemove dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "settings", "global", "cursor.json")); err != nil {
		t.Fatalf("dry-run should preserve file: %v", err)
	}
}

func TestRunSettingsRemove_Force_DeletesFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "cursor.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := makeSettingsDeps(false, true, false)
	if err := runSettingsRemove(deps, "global", "cursor.json"); err != nil {
		t.Fatalf("runSettingsRemove force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "settings", "global", "cursor.json")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed; stat err = %v", err)
	}
}

func TestNewSettingsCmd_Metadata(t *testing.T) {
	cmd := NewSettingsCmd()
	if cmd.Use != "settings" {
		t.Errorf("Use = %q", cmd.Use)
	}
	wantSubs := map[string]bool{"list": false, "show": false, "remove": false}
	for _, c := range cmd.Commands() {
		if _, ok := wantSubs[c.Name()]; ok {
			wantSubs[c.Name()] = true
		}
	}
	for name, found := range wantSubs {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}
