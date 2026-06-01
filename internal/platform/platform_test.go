package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/links"
)

const (
	platformTestExpectedSymlinkTargetFmt = "expected %s to be a managed link to %s"
)

func TestOpenCodeCreateLinksUsesCanonicalAgents(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	agentDir := filepath.Join(agentsHome, "agents", "proj", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentMD := filepath.Join(agentDir, "AGENT.md")
	if err := os.WriteFile(agentMD, []byte("# Reviewer\n"), 0644); err != nil {
		t.Fatal(err)
	}

	settingsDir := filepath.Join(agentsHome, "settings", "proj")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	opencodeJSON := filepath.Join(settingsDir, "opencode.json")
	if err := os.WriteFile(opencodeJSON, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewOpenCode()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	if err := NewOpenCode().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	gotAgent := filepath.Join(repo, ".opencode", "agent", "reviewer.md")
	if !links.IsManagedLink(gotAgent, agentMD) {
		t.Fatalf(platformTestExpectedSymlinkTargetFmt, gotAgent, agentMD)
	}

	gotConfig := filepath.Join(repo, "opencode.json")
	if !links.IsManagedLink(gotConfig, opencodeJSON) {
		t.Fatalf(platformTestExpectedSymlinkTargetFmt, gotConfig, opencodeJSON)
	}
}

func TestCodexCreateLinksEmitsProjectAndUserHooks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	hooksDir := filepath.Join(agentsHome, "hooks", "proj")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	hooksJSON := filepath.Join(hooksDir, "codex.json")
	if err := os.WriteFile(hooksJSON, []byte("{\"hooks\":[]}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewCodex()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	projectHooks := filepath.Join(repo, ".codex", "hooks.json")
	if !links.IsManagedLink(projectHooks, hooksJSON) {
		t.Fatalf(platformTestExpectedSymlinkTargetFmt, projectHooks, hooksJSON)
	}

	userHooks := filepath.Join(home, ".codex", "hooks.json")
	if !links.IsManagedLink(userHooks, hooksJSON) {
		t.Fatalf(platformTestExpectedSymlinkTargetFmt, userHooks, hooksJSON)
	}
}
