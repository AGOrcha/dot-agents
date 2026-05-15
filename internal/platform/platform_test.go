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
	assertPathResolvesToTarget(t, gotAgent, agentMD)

	gotConfig := filepath.Join(repo, "opencode.json")
	assertPathResolvesToTarget(t, gotConfig, opencodeJSON)
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
	assertPathResolvesToTarget(t, projectHooks, hooksJSON)

	userHooks := filepath.Join(home, ".codex", "hooks.json")
	assertPathResolvesToTarget(t, userHooks, hooksJSON)
}

func assertPathResolvesToTarget(t *testing.T, gotPath, wantPath string) {
	t.Helper()
	gotInfo, err := os.Stat(gotPath)
	if err != nil {
		t.Fatalf(platformTestExpectedSymlinkFmt, gotPath, err)
	}
	wantInfo, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("expected target %s to exist: %v", wantPath, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		if gotInfo.Mode().IsRegular() && wantInfo.Mode().IsRegular() {
			gotContent, err := os.ReadFile(gotPath)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", gotPath, err)
			}
			wantContent, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", wantPath, err)
			}
			if string(gotContent) == string(wantContent) {
				return
			}
		}
		t.Fatalf("expected %s to resolve to %s", gotPath, wantPath)
	}
}
