package skills

import (
	"encoding/json"
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

// ── PromoteSkillIn success ────────────────────────────────────────────────────

func TestPromoteSkillIn_ConvergesRepoLocalToManagedSymlink(t *testing.T) {
	agentsHome, projectPath := setupSkillsEnv(t, "myprojtest")
	writeSkillMD(t, projectPath, "my-skill")

	if err := PromoteSkillIn("my-skill", projectPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	canonicalPath := filepath.Join(agentsHome, "skills", "myprojtest", "my-skill")
	repoLocalPath := filepath.Join(projectPath, ".agents", "skills", "my-skill")

	// Canonical path must be a real directory containing SKILL.md.
	cfi, err := os.Lstat(canonicalPath)
	if err != nil {
		t.Fatalf("canonical path not created at %s: %v", canonicalPath, err)
	}
	if cfi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("canonical path %s should be a real directory, got symlink", canonicalPath)
	}
	if !cfi.IsDir() {
		t.Errorf("canonical path %s should be a directory, got %v", canonicalPath, cfi.Mode())
	}
	if _, err := os.Stat(filepath.Join(canonicalPath, "SKILL.md")); err != nil {
		t.Errorf("canonical SKILL.md missing: %v", err)
	}

	// Repo-local path must now be a managed symlink pointing to canonical.
	rfi, err := os.Lstat(repoLocalPath)
	if err != nil {
		t.Fatalf("repo-local path missing after promote: %v", err)
	}
	if rfi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("repo-local path %s should be a symlink after promote, got %v", repoLocalPath, rfi.Mode())
	}
	target, err := os.Readlink(repoLocalPath)
	if err != nil {
		t.Fatalf("readlink repo-local: %v", err)
	}
	if target != canonicalPath {
		t.Errorf("repo-local symlink target = %q, want %q", target, canonicalPath)
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

// TestPromoteSkillIn_PreservesManifestUnknownFields regression-tests that promote's
// LoadAgentsRC → append Skills → Save path keeps ExtraFields (custom keys) and known
// multi-source declarations — not only the isolated marshal tests.
func TestPromoteSkillIn_PreservesManifestUnknownFields(t *testing.T) {
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

	manifest := `{
  "version": 1,
  "project": "regproj",
  "sources": [{"type":"local"},{"type":"git","url":"https://example.com/repo.git"}],
  "hooks": false,
  "mcp": false,
  "settings": false,
  "customPolicy": {"interval": "daily", "auto": true},
  "myteam": "platform"
}`
	if err := os.WriteFile(filepath.Join(projectPath, config.AgentsRCFile), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	writeSkillMD(t, projectPath, "extra-skill")

	if err := PromoteSkillIn("extra-skill", projectPath); err != nil {
		t.Fatalf("PromoteSkillIn: %v", err)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	if len(rc.ExtraFields) < 2 {
		t.Fatalf("ExtraFields: got %d keys, want at least 2; keys: %v", len(rc.ExtraFields), rc.ExtraFields)
	}
	if _, ok := rc.ExtraFields["customPolicy"]; !ok {
		t.Error("ExtraFields missing 'customPolicy' after promote")
	}
	if _, ok := rc.ExtraFields["myteam"]; !ok {
		t.Error("ExtraFields missing 'myteam' after promote")
	}
	var policyVal map[string]any
	if err := json.Unmarshal(rc.ExtraFields["customPolicy"], &policyVal); err != nil {
		t.Fatalf("unmarshal customPolicy: %v", err)
	}
	if policyVal["interval"] != "daily" {
		t.Errorf("customPolicy.interval: got %v, want daily", policyVal["interval"])
	}
	if len(rc.Sources) < 2 {
		t.Errorf("Sources: want at least 2 entries preserved, got %+v", rc.Sources)
	}
	found := false
	for _, s := range rc.Skills {
		if s == "extra-skill" {
			found = true
		}
	}
	if !found {
		t.Errorf("Skills should include extra-skill, got %v", rc.Skills)
	}
}

func TestPromoteSkillIn_IdempotentOnExistingSymlink(t *testing.T) {
	agentsHome, projectPath := setupSkillsEnv(t, "myprojtest2")
	writeSkillMD(t, projectPath, "idem-skill")

	// First promote: copies content, repo-local becomes managed symlink.
	if err := PromoteSkillIn("idem-skill", projectPath); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	// Second promote: repo-local is already a symlink to canonical — idempotent.
	if err := PromoteSkillIn("idem-skill", projectPath); err != nil {
		t.Fatalf("second promote: %v", err)
	}

	// Canonical still a real directory.
	canonical := filepath.Join(agentsHome, "skills", "myprojtest2", "idem-skill")
	fi, err := os.Lstat(canonical)
	if err != nil {
		t.Fatalf("canonical missing after second promote: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("canonical should be a real directory after second promote, got symlink")
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

// ── PromoteSkillIn error paths ────────────────────────────────────────────────

func TestPromoteSkillIn_ErrorSkillNotFound(t *testing.T) {
	_, projectPath := setupSkillsEnv(t, "myprojtest3")
	// Do NOT write skill files — skill directory does not exist.

	err := PromoteSkillIn("nonexistent", projectPath)
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

	err := PromoteSkillIn("some-skill", projectPath)
	if err == nil {
		t.Fatal("expected error for empty project name, got nil")
	}
	if !strings.Contains(err.Error(), "project name") {
		t.Errorf("error message = %q; want 'project name' substring", err.Error())
	}
}

func TestPromoteSkillIn_ErrorRepoLocalSymlinkMispoints(t *testing.T) {
	agentsHome, projectPath := setupSkillsEnv(t, "myprojtest5")
	writeSkillMD(t, projectPath, "mis-skill")

	// Manually create repo-local as a symlink pointing somewhere else.
	repoLocalPath := filepath.Join(projectPath, ".agents", "skills", "mis-skill")
	// Remove the real dir first, create symlink to a different location.
	if err := os.RemoveAll(repoLocalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(agentsHome, "other"), repoLocalPath); err != nil {
		t.Fatal(err)
	}

	err := PromoteSkillIn("mis-skill", projectPath)
	if err == nil {
		t.Fatal("expected error when repo-local symlink points elsewhere, got nil")
	}
	if !strings.Contains(err.Error(), "already a symlink but points to") {
		t.Errorf("error message = %q; want 'already a symlink but points to' substring", err.Error())
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

	err := PromoteSkillIn("clash-skill", projectPath)
	if err == nil {
		t.Fatal("expected error when canonical path is a real directory, got nil")
	}
	if !strings.Contains(err.Error(), "real directory") {
		t.Errorf("error message = %q; want 'real directory' substring", err.Error())
	}
}
