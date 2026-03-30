package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderCodexAgentToml_UsesFrontmatterAndBody(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentMD := filepath.Join(agentDir, "AGENT.md")
	content := `---
name: reviewer
description: reviews changes
model: gpt-5.1-codex
is_background: true
---

# Reviewer

Use "safe" defaults and avoid shell footguns.
`
	if err := os.WriteFile(agentMD, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := renderCodexAgentToml(agentMD)
	if err != nil {
		t.Fatalf("renderCodexAgentToml failed: %v", err)
	}

	out := string(got)
	for _, want := range []string{
		`name = "reviewer"`,
		`description = "reviews changes"`,
		`model = "gpt-5.1-codex"`,
		`is_background = true`,
		`instructions = """`,
		`# Reviewer`,
		`Use "safe" defaults and avoid shell footguns.`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render output missing %q:\n%s", want, out)
		}
	}
}

func TestCodexCreateLinks_WritesNativeAgentTomlAndCleansCompat(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	globalAgentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	projectAgentDir := filepath.Join(agentsHome, "agents", "proj", "implementer")
	for _, dir := range []string{globalAgentDir, projectAgentDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(globalAgentDir, "AGENT.md"), []byte(`---
name: reviewer
description: global reviewer
model: gpt-5.1-codex
---

# Reviewer
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectAgentDir, "AGENT.md"), []byte(`---
name: implementer
description: project implementer
is_background: false
---

# Implementer

Build the feature and keep tests green.
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}

	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	projectToml := filepath.Join(repo, ".codex", "agents", "implementer.toml")
	projectOut, err := os.ReadFile(projectToml)
	if err != nil {
		t.Fatalf("expected project toml at %s: %v", projectToml, err)
	}
	for _, want := range []string{
		`name = "implementer"`,
		`description = "project implementer"`,
		`is_background = false`,
		`Build the feature and keep tests green.`,
	} {
		if !strings.Contains(string(projectOut), want) {
			t.Fatalf("project toml missing %q:\n%s", want, string(projectOut))
		}
	}

	userToml := filepath.Join(home, ".codex", "agents", "reviewer.toml")
	userOut, err := os.ReadFile(userToml)
	if err != nil {
		t.Fatalf("expected user toml at %s: %v", userToml, err)
	}
	for _, want := range []string{
		`name = "reviewer"`,
		`description = "global reviewer"`,
		`model = "gpt-5.1-codex"`,
	} {
		if !strings.Contains(string(userOut), want) {
			t.Fatalf("user toml missing %q:\n%s", want, string(userOut))
		}
	}

	if _, err := os.Stat(filepath.Join(repo, ".claude", "agents")); !os.IsNotExist(err) {
		t.Fatalf("legacy compat path should be cleaned up")
	}

	if err := NewCodex().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks failed: %v", err)
	}

	if _, err := os.Stat(projectToml); !os.IsNotExist(err) {
		t.Fatalf("project native agent should be removed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude", "agents")); !os.IsNotExist(err) {
		t.Fatalf("legacy compat path should stay removed")
	}
}
