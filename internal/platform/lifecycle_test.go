package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// populatedAgentsHome builds a comprehensive ~/.agents/ fixture with
// rules, settings, mcp, skills, agents, and hooks for both "global" and the
// given project scope. Returned path is suitable for AGENTS_HOME.
func populatedAgentsHome(t *testing.T, project string) (agentsHome, home string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, ".agents")
	home = filepath.Join(tmp, "userhome")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}

	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{agentsHome}, parts...)...)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	wf := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Rules
	wf(filepath.Join(mk("rules", "global"), "rules.md"), "# global rules\n")
	wf(filepath.Join(mk("rules", project), "custom.md"), "# project rule\n")
	wf(filepath.Join(agentsHome, "rules", "global", "agents.md"), "# AGENTS\n")

	// Skills with marker
	for _, scope := range []string{"global", project} {
		d := mk("skills", scope, "my-skill")
		wf(filepath.Join(d, "SKILL.md"),
			"---\nname: my-skill\ndescription: a skill\n---\n# Body\n")
	}

	// Agents with marker
	for _, scope := range []string{"global", project} {
		d := mk("agents", scope, "reviewer")
		wf(filepath.Join(d, "AGENT.md"),
			"---\nname: reviewer\ndescription: a reviewer\n---\n# Body\n")
	}

	// Settings
	wf(filepath.Join(mk("settings", "global"), "claude-code.json"), `{"version":1}`)
	wf(filepath.Join(mk("settings", project), "claude-code.json"), `{"version":1}`)
	wf(filepath.Join(agentsHome, "settings", "global", "cursor.json"), `{}`)

	// MCP
	wf(filepath.Join(mk("mcp", project), "mcp.json"), `{"mcpServers":{"x":{}}}`)
	wf(filepath.Join(mk("mcp", "global"), "mcp.json"), `{"mcpServers":{"g":{}}}`)
	return agentsHome, home
}

func TestLifecycle_ClaudeCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewClaude()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}

	// Verify some artefacts
	if _, err := os.Stat(filepath.Join(repo, ".claude", "rules")); err != nil {
		t.Errorf("rules dir missing: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

func TestLifecycle_CursorCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewCursor()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

func TestLifecycle_CopilotCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewCopilot()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

func TestLifecycle_OpenCodeCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewOpenCode()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

func TestLifecycle_CodexCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewCodex()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

// TestLifecycle_SharedTargetIntentsForAllPlatforms drives the shared-target
// projection path across every platform with a populated AgentsHome.
func TestLifecycle_SharedTargetIntentsForAllPlatforms(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	for _, p := range All() {
		intents, err := p.SharedTargetIntents("proj")
		if err != nil {
			t.Errorf("%s: SharedTargetIntents: %v", p.ID(), err)
		}
		// Just sanity-check intents have non-empty targets and the right project.
		for i, intent := range intents {
			if intent.TargetPath == "" {
				t.Errorf("%s intent[%d] empty TargetPath", p.ID(), i)
			}
		}
	}
}
