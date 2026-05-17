package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewSkillsCmd_ShimReturnsCobraTree verifies that the package-local
// NewSkillsCmd shim delegates to the skills subpackage and returns a working
// cobra tree with the expected three subcommands.
func TestNewSkillsCmd_ShimReturnsCobraTree(t *testing.T) {
	cmd := NewSkillsCmd()
	if cmd == nil {
		t.Fatal("NewSkillsCmd returned nil")
	}
	if cmd.Use != "skills" {
		t.Errorf("Use = %q, want skills", cmd.Use)
	}
	want := map[string]bool{"list": false, "new": false, "promote": false}
	for _, c := range cmd.Commands() {
		want[c.Name()] = true
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

// TestCreateSkillShim_ProducesGlobalScopeSkillDir verifies the createSkill
// shim (preserved for agentsrc mutation tests in this package) delegates to
// skills.CreateSkill.
func TestCreateSkillShim_ProducesGlobalScopeSkillDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	fakeHome := filepath.Join(tmp, "home")
	if err := os.MkdirAll(fakeHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)

	if err := createSkill("shim-skill", "global"); err != nil {
		t.Fatalf("createSkill: %v", err)
	}
	skillDir := filepath.Join(agentsHome, "skills", "global", "shim-skill")
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not created via shim: %v", err)
	}
}
