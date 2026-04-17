package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// setupAgentsEnv creates a minimal repo+agentsHome fixture for agents tests.
// Returns (agentsHome, projectPath).
func setupAgentsEnv(t *testing.T, projectName string) (agentsHome, projectPath string) {
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

func writeAgentMD(t *testing.T, projectPath, agentName string) {
	t.Helper()
	dir := filepath.Join(projectPath, ".agents", "agents", agentName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + agentName + "\ndescription: test agent\n---\n\n# " + agentName + "\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ── promoteAgentIn success ─────────────────────────────────────────────────────

func TestPromoteAgentIn_ConvergesRepoLocalToManagedSymlink(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "myprojtest")
	writeAgentMD(t, projectPath, "my-agent")

	if err := promoteAgentIn("my-agent", projectPath, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	canonicalPath := filepath.Join(agentsHome, "agents", "myprojtest", "my-agent")
	repoLocalPath := filepath.Join(projectPath, ".agents", "agents", "my-agent")

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
	if _, err := os.Stat(filepath.Join(canonicalPath, "AGENT.md")); err != nil {
		t.Errorf("canonical AGENT.md missing: %v", err)
	}

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

	claudePath := filepath.Join(projectPath, ".claude", "agents", "my-agent")
	cfi2, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf(".claude/agents symlink missing: %v", err)
	}
	if cfi2.Mode()&os.ModeSymlink == 0 {
		t.Errorf(".claude/agents/%s should be a symlink, got %v", "my-agent", cfi2.Mode())
	}
	clTarget, err := os.Readlink(claudePath)
	if err != nil {
		t.Fatalf("readlink .claude/agents: %v", err)
	}
	if clTarget != canonicalPath {
		t.Errorf(".claude/agents symlink target = %q, want %q", clTarget, canonicalPath)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	found := false
	for _, a := range rc.Agents {
		if a == "my-agent" {
			found = true
		}
	}
	if !found {
		t.Errorf(".agentsrc.json Agents = %v; want 'my-agent' to be present", rc.Agents)
	}
}

func TestPromoteAgentIn_PreservesManifestUnknownFields(t *testing.T) {
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

	writeAgentMD(t, projectPath, "extra-agent")

	if err := promoteAgentIn("extra-agent", projectPath, false); err != nil {
		t.Fatalf("promoteAgentIn: %v", err)
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
	for _, a := range rc.Agents {
		if a == "extra-agent" {
			found = true
		}
	}
	if !found {
		t.Errorf("Agents should include extra-agent, got %v", rc.Agents)
	}
}

func TestPromoteAgentIn_IdempotentOnExistingSymlink(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "myprojtest2")
	writeAgentMD(t, projectPath, "idem-agent")

	if err := promoteAgentIn("idem-agent", projectPath, false); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	if err := promoteAgentIn("idem-agent", projectPath, false); err != nil {
		t.Fatalf("second promote: %v", err)
	}

	canonical := filepath.Join(agentsHome, "agents", "myprojtest2", "idem-agent")
	fi, err := os.Lstat(canonical)
	if err != nil {
		t.Fatalf("canonical missing after second promote: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("canonical should be a real directory after second promote, got symlink")
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	count := 0
	for _, a := range rc.Agents {
		if a == "idem-agent" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Agents list has %d occurrences of 'idem-agent'; want 1. list=%v", count, rc.Agents)
	}
}

func TestPromoteAgentIn_ForceOverwritesCanonicalDir(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "forceproj")
	writeAgentMD(t, projectPath, "force-agent")

	destPath := filepath.Join(agentsHome, "agents", "forceproj", "force-agent")
	if err := os.MkdirAll(filepath.Join(destPath, "stale"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destPath, "AGENT.md"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := promoteAgentIn("force-agent", projectPath, true); err != nil {
		t.Fatalf("promoteAgentIn with --force: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destPath, "AGENT.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "stale" {
		t.Errorf("expected repo AGENT.md to replace stale canonical file")
	}
	if !strings.Contains(string(data), "test agent") {
		t.Errorf("expected promoted AGENT.md from repo fixture, got %q", string(data))
	}
}

// ── promoteAgentIn error paths ────────────────────────────────────────────────

func TestPromoteAgentIn_ErrorAgentNotFound(t *testing.T) {
	_, projectPath := setupAgentsEnv(t, "myprojtest3")

	err := promoteAgentIn("nonexistent", projectPath, false)
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error message = %q; want 'not found' substring", err.Error())
	}
}

func TestPromoteAgentIn_ErrorNoProjectName(t *testing.T) {
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

	rc := &config.AgentsRC{Version: 1, Project: ""}
	if err := rc.Save(projectPath); err != nil {
		t.Fatalf("rc.Save: %v", err)
	}
	writeAgentMD(t, projectPath, "some-agent")

	err := promoteAgentIn("some-agent", projectPath, false)
	if err == nil {
		t.Fatal("expected error for empty project name, got nil")
	}
	if !strings.Contains(err.Error(), "project name") {
		t.Errorf("error message = %q; want 'project name' substring", err.Error())
	}
}

func TestPromoteAgentIn_ErrorRepoLocalSymlinkMispoints(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "myprojtest5")
	writeAgentMD(t, projectPath, "mis-agent")

	repoLocalPath := filepath.Join(projectPath, ".agents", "agents", "mis-agent")
	if err := os.RemoveAll(repoLocalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(agentsHome, "other"), repoLocalPath); err != nil {
		t.Fatal(err)
	}

	err := promoteAgentIn("mis-agent", projectPath, false)
	if err == nil {
		t.Fatal("expected error when repo-local symlink points elsewhere, got nil")
	}
	if !strings.Contains(err.Error(), "already a symlink but points to") {
		t.Errorf("error message = %q; want 'already a symlink but points to' substring", err.Error())
	}
}

func TestPromoteAgentIn_ErrorExistingCanonicalWithoutForce(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "myprojtest4")
	writeAgentMD(t, projectPath, "clash-agent")

	destPath := filepath.Join(agentsHome, "agents", "myprojtest4", "clash-agent")
	if err := os.MkdirAll(destPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destPath, "AGENT.md"), []byte("---\nname: x\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := promoteAgentIn("clash-agent", projectPath, false)
	if err == nil {
		t.Fatal("expected error when canonical path is a real directory, got nil")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error message = %q; want '--force' substring", err.Error())
	}
}

func TestPromoteAgentIn_ErrorMissingAGENTmd(t *testing.T) {
	_, projectPath := setupAgentsEnv(t, "noagentmd")
	dir := filepath.Join(projectPath, ".agents", "agents", "empty-dir")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	err := promoteAgentIn("empty-dir", projectPath, false)
	if err == nil {
		t.Fatal("expected error without AGENT.md, got nil")
	}
	if !strings.Contains(err.Error(), "AGENT.md") {
		t.Errorf("error message = %q; want 'AGENT.md' substring", err.Error())
	}
}
