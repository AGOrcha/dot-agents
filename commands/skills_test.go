package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// setupSkillsEnv creates a minimal repo+agentsHome fixture for skills tests.
// Returns (agentsHome, projectPath).
func setupSkillsEnv(t *testing.T, projectName string) (agentsHome, projectPath string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, "agents")
	projectPath = filepath.Join(tmp, "repo")

	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	rc := &config.AgentsRC{
		Version: 1,
		Project: projectName,
		Sources: []config.Source{{Type: "local"}},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatalf("rc.Save: %v", err)
	}
	return agentsHome, projectPath
}

func writeSkillMD(t *testing.T, projectPath, skillName string) {
	t.Helper()
	dir := filepath.Join(projectPath, ".agents", "skills", skillName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + skillName + "\ndescription: test skill\n---\n\n# " + skillName + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ── promoteSkillIn success ────────────────────────────────────────────────────

func TestPromoteSkillIn_CreatesSymlinkAndUpdatesManifest(t *testing.T) {
	agentsHome, projectPath := setupSkillsEnv(t, "myprojtest")
	writeSkillMD(t, projectPath, "my-skill")

	if err := promoteSkillIn("my-skill", projectPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Managed symlink created in agentsHome/skills/<project>/<name>
	symlink := filepath.Join(agentsHome, "skills", "myprojtest", "my-skill")
	fi, err := os.Lstat(symlink)
	if err != nil {
		t.Fatalf("symlink not created at %s: %v", symlink, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink at %s, got %v", symlink, fi.Mode())
	}
	// Symlink should point to the repo-local skill dir.
	target, err := os.Readlink(symlink)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	expectedTarget := filepath.Join(projectPath, ".agents", "skills", "my-skill")
	if target != expectedTarget {
		t.Errorf("symlink target = %q, want %q", target, expectedTarget)
	}

	// .agentsrc.json should have the skill registered.
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	found := false
	for _, s := range rc.Skills {
		if s == "my-skill" {
			found = true
		}
	}
	if !found {
		t.Errorf(".agentsrc.json Skills = %v; want 'my-skill' to be present", rc.Skills)
	}
}

func TestPromoteSkillIn_IdempotentOnExistingSymlink(t *testing.T) {
	agentsHome, projectPath := setupSkillsEnv(t, "myprojtest2")
	writeSkillMD(t, projectPath, "idem-skill")

	// First promote.
	if err := promoteSkillIn("idem-skill", projectPath); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	// Second promote should update the symlink without error.
	if err := promoteSkillIn("idem-skill", projectPath); err != nil {
		t.Fatalf("second promote: %v", err)
	}

	symlink := filepath.Join(agentsHome, "skills", "myprojtest2", "idem-skill")
	fi, err := os.Lstat(symlink)
	if err != nil {
		t.Fatalf("symlink missing after second promote: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink after second promote, got %v", fi.Mode())
	}

	// Skills list should not contain duplicates.
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	count := 0
	for _, s := range rc.Skills {
		if s == "idem-skill" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Skills list has %d occurrences of 'idem-skill'; want 1. list=%v", count, rc.Skills)
	}
}

// ── promoteSkillIn error paths ────────────────────────────────────────────────

func TestPromoteSkillIn_ErrorSkillNotFound(t *testing.T) {
	_, projectPath := setupSkillsEnv(t, "myprojtest3")
	// Do NOT write skill files — skill directory does not exist.

	err := promoteSkillIn("nonexistent", projectPath)
	if err == nil {
		t.Fatal("expected error for missing skill, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error message = %q; want 'not found' substring", err.Error())
	}
}

func TestPromoteSkillIn_ErrorNoProjectName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Write manifest with empty project name.
	rc := &config.AgentsRC{Version: 1, Project: ""}
	if err := rc.Save(projectPath); err != nil {
		t.Fatalf("rc.Save: %v", err)
	}
	writeSkillMD(t, projectPath, "some-skill")

	err := promoteSkillIn("some-skill", projectPath)
	if err == nil {
		t.Fatal("expected error for empty project name, got nil")
	}
	if !strings.Contains(err.Error(), "project name") {
		t.Errorf("error message = %q; want 'project name' substring", err.Error())
	}
}

func TestPromoteSkillIn_ErrorExistingNonSymlink(t *testing.T) {
	agentsHome, projectPath := setupSkillsEnv(t, "myprojtest4")
	writeSkillMD(t, projectPath, "clash-skill")

	// Pre-create a real directory at the destination (not a symlink).
	destPath := filepath.Join(agentsHome, "skills", "myprojtest4", "clash-skill")
	if err := os.MkdirAll(destPath, 0755); err != nil {
		t.Fatal(err)
	}

	err := promoteSkillIn("clash-skill", projectPath)
	if err == nil {
		t.Fatal("expected error when destination is a real directory, got nil")
	}
	if !strings.Contains(err.Error(), "not a managed symlink") {
		t.Errorf("error message = %q; want 'not a managed symlink' substring", err.Error())
	}
}
