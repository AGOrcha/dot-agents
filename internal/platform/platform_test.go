package platform

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	platformTestExpectedSymlinkFmt       = "expected %s to be a symlink: %v"
	platformTestExpectedSymlinkTargetFmt = "expected %s to point to %s, got %s"
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
	if dest, err := os.Readlink(gotAgent); err != nil {
		t.Fatalf(platformTestExpectedSymlinkFmt, gotAgent, err)
	} else if dest != agentMD {
		t.Fatalf(platformTestExpectedSymlinkTargetFmt, gotAgent, agentMD, dest)
	}

	gotConfig := filepath.Join(repo, "opencode.json")
	if dest, err := os.Readlink(gotConfig); err != nil {
		t.Fatalf(platformTestExpectedSymlinkFmt, gotConfig, err)
	} else if dest != opencodeJSON {
		t.Fatalf(platformTestExpectedSymlinkTargetFmt, gotConfig, opencodeJSON, dest)
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
	if dest, err := os.Readlink(projectHooks); err != nil {
		t.Fatalf(platformTestExpectedSymlinkFmt, projectHooks, err)
	} else if dest != hooksJSON {
		t.Fatalf(platformTestExpectedSymlinkTargetFmt, projectHooks, hooksJSON, dest)
	}

	userHooks := filepath.Join(home, ".codex", "hooks.json")
	if dest, err := os.Readlink(userHooks); err != nil {
		t.Fatalf(platformTestExpectedSymlinkFmt, userHooks, err)
	} else if dest != hooksJSON {
		t.Fatalf(platformTestExpectedSymlinkTargetFmt, userHooks, hooksJSON, dest)
	}
}
