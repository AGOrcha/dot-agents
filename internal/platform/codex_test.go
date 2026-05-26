package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/links"
)

const codexAgentMarkdownFile = "AGENT.md"

func TestRenderCodexAgentTomlUsesFrontmatterAndBody(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentMD := filepath.Join(agentDir, codexAgentMarkdownFile)
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
		`developer_instructions = """`,
		`# Reviewer`,
		`Use "safe" defaults and avoid shell footguns.`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render output missing %q:\n%s", want, out)
		}
	}
}

func TestCodexCreateLinksWritesNativeAgentTomlAndCleansCompat(t *testing.T) {
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
	mustWriteCodexFixtureFile(t, filepath.Join(globalAgentDir, codexAgentMarkdownFile), `---
name: reviewer
description: global reviewer
model: gpt-5.1-codex
---

# Reviewer
`)
	mustWriteCodexFixtureFile(t, filepath.Join(projectAgentDir, codexAgentMarkdownFile), `---
name: implementer
description: project implementer
is_background: false
---

# Implementer

Build the feature and keep tests green.
`)

	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewCodex()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	projectToml := filepath.Join(repo, ".codex", "agents", "implementer.toml")
	assertCodexFileContains(t, "project toml", projectToml, []string{
		`name = "implementer"`,
		`description = "project implementer"`,
		`Build the feature and keep tests green.`,
	})

	userToml := filepath.Join(home, ".codex", "agents", "reviewer.toml")
	assertCodexFileContains(t, "user toml", userToml, []string{
		`name = "reviewer"`,
		`description = "global reviewer"`,
		`model = "gpt-5.1-codex"`,
	})

	assertCodexPathNotExists(t, filepath.Join(repo, ".claude", "agents"), "legacy compat path should be cleaned up")

	if err := NewCodex().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks failed: %v", err)
	}

	assertCodexPathNotExists(t, projectToml, "project native agent should be removed")
	assertCodexPathNotExists(t, filepath.Join(repo, ".claude", "agents"), "legacy compat path should stay removed")
}

func mustWriteCodexFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertCodexFileContains(t *testing.T, label, path string, want []string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s at %s: %v", label, path, err)
	}
	got := string(content)
	for _, snippet := range want {
		if !strings.Contains(got, snippet) {
			t.Fatalf("%s missing %q:\n%s", label, snippet, got)
		}
	}
}

func assertCodexPathNotExists(t *testing.T, path, message string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s, got %v", message, err)
	}
}

// ---------------------------------------------------------------------------
// Codex CreateLinks + SharedTargetIntents + hook rendering coverage
// (relocated from coverage_gap2_test.go).
// ---------------------------------------------------------------------------

// TestCodexCreateLinks_FullRulesAndSettings drives the rules→AGENTS.md and
// settings→config.toml branches of (*codex).CreateLinks.
func TestCodexCreateLinks_FullRulesAndSettings(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed global rules and project override.
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "global"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "global", "agents.md"), []byte("# global\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "proj", "agents.md"), []byte("# proj\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Seed settings/codex.toml.
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "settings", "proj", "codex.toml"), []byte("model = \"x\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}

	// AGENTS.md should be linked (project override wins).
	if !links.IsManagedLink(filepath.Join(repo, "AGENTS.md"), filepath.Join(agentsHome, "rules", "proj", "agents.md")) {
		t.Error("AGENTS.md should be a managed link to the project override")
	}
	if _, err := os.Lstat(filepath.Join(repo, ".codex", "config.toml")); err != nil {
		t.Errorf("config.toml missing: %v", err)
	}
}

// TestCodexSharedTargetIntentsPopulated drives skills + codex-agent-toml intents.
func TestCodexSharedTargetIntents_Populated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	for _, p := range [][]string{
		{"skills", "proj", "alpha", "SKILL.md"},
		{"agents", "proj", "reviewer", "AGENT.md"},
	} {
		dir := filepath.Join(append([]string{agentsHome}, p[:3]...)...)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p[3]), []byte("body"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	intents, err := NewCodex().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) == 0 {
		t.Error("expected non-zero intents")
	}
}

// TestRenderCodexHookConfigMatcherBranches covers session_start/pre_tool_use/
// post_tool_use which take the matcherForSpec branch versus stop which uses
// empty matcher.
func TestRenderCodexHookConfig_MatcherBranches(t *testing.T) {
	specs := []HookSpec{
		{Name: "a", When: "stop", Command: "/bin/x"},
		{Name: "b", When: "pre_tool_use", Command: "/bin/y", MatchExpression: "Bash"},
	}
	content, err := renderCodexHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCodexHookConfig: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "Stop") {
		t.Errorf("expected Stop event: %s", got)
	}
	if !strings.Contains(got, `"matcher": "Bash"`) {
		t.Errorf("expected Bash matcher: %s", got)
	}
}
