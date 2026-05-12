package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillsList_PrintsSkills(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	skillDir := filepath.Join(agentsHome, "skills", "global", "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	manifest := "---\nname: test-skill\ndescription: A test skill\n---\n# Test Skill\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	if err := List("global"); err != nil {
		t.Fatalf("List: %v", err)
	}
}

func TestSkillsList_EmptyScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// No skills dir — should print info message, not error.
	if err := List("global"); err != nil {
		t.Fatalf("List with empty scope: %v", err)
	}
}
