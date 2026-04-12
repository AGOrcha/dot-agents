package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

const refreshCanonicalAgentPath = "agents/proj/my-agent/AGENT.md"

// ---------- mapResourceRelToDest ----------

func TestMapResourceRelToDest_MCPCanonicalization(t *testing.T) {
	cases := []struct {
		relPath  string
		expected string
	}{
		// All platform MCP files must normalize to the canonical mcp.json
		{".mcp.json", "mcp/proj/mcp.json"},
		{".cursor/mcp.json", "mcp/proj/mcp.json"},
		{".vscode/mcp.json", "mcp/proj/mcp.json"},
		// Other mappings must remain intact
		{".cursor/settings.json", "settings/proj/cursor.json"},
		{".cursorignore", "settings/proj/cursorignore"},
		{".claude/settings.local.json", "settings/proj/claude-code.json"},
		{"opencode.json", "settings/proj/opencode.json"},
		{"AGENTS.md", "rules/proj/agents.md"},
		{".codex/instructions.md", "rules/proj/agents.md"},
		{".codex/rules.md", "rules/proj/agents.md"},
		{".codex/config.toml", "settings/proj/codex.toml"},
		{".codex/hooks.json", "hooks/proj/codex.json"},
		{".github/copilot-instructions.md", "rules/proj/copilot-instructions.md"},
		{".github/hooks/pre-tool.json", "hooks/proj/pre-tool/HOOK.yaml"},
	}
	for _, c := range cases {
		got := mapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("mapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
}

func TestMapResourceRelToDest_SkillsAndAgents(t *testing.T) {
	cases := []struct {
		relPath  string
		expected string
	}{
		{".agents/skills/my-skill/SKILL.md", "skills/proj/my-skill/SKILL.md"},
		{".claude/skills/my-skill/SKILL.md", "skills/proj/my-skill/SKILL.md"},
		{".github/agents/my-agent.agent.md", refreshCanonicalAgentPath},
		{".codex/agents/my-agent/AGENT.md", refreshCanonicalAgentPath},
		{".opencode/agent/my-agent.md", refreshCanonicalAgentPath},
	}
	for _, c := range cases {
		got := mapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("mapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
}

func TestMapResourceRelToDest_CursorRules(t *testing.T) {
	cases := []struct {
		relPath  string
		expected string
	}{
		{".cursor/rules/global--rules.mdc", "rules/global/rules.mdc"},
		{".cursor/rules/proj--rules.mdc", "rules/proj/rules.mdc"},
		{".cursor/rules/some-rule.mdc", "rules/proj/some-rule.mdc"},
	}
	for _, c := range cases {
		got := mapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("mapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
}

func TestMapResourceRelToDest_PassThrough(t *testing.T) {
	cases := []struct {
		relPath  string
		expected string
	}{
		// Already under known ~/.agents dirs — pass through unchanged
		{"rules/proj/rules.mdc", "rules/proj/rules.mdc"},
		{"mcp/proj/mcp.json", "mcp/proj/mcp.json"},
		{"settings/proj/cursor.json", "settings/proj/cursor.json"},
	}
	for _, c := range cases {
		got := mapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("mapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
}

func TestMapResourceRelToDest_UnknownReturnsEmpty(t *testing.T) {
	got := mapResourceRelToDest("proj", ".some/unknown/path.json")
	if got != "" {
		t.Errorf("expected empty for unknown path, got %q", got)
	}
}

func TestRefreshImportsUnmanagedAgentsMarkdownBeforeRelinking(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	agentsHome := filepath.Join(tmp, ".agents")
	repo := filepath.Join(tmp, "repo")
	binDir := filepath.Join(tmp, "bin")

	for _, dir := range []string{home, agentsHome, repo, binDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	t.Setenv("HOME", home)
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	codexBin := filepath.Join(binDir, "codex")
	if err := os.WriteFile(codexBin, []byte("#!/bin/sh\necho codex test\n"), 0755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	globalRules := filepath.Join(agentsHome, "rules", "global", "rules.md")
	if err := os.MkdirAll(filepath.Dir(globalRules), 0755); err != nil {
		t.Fatalf("mkdir global rules dir: %v", err)
	}
	if err := os.WriteFile(globalRules, []byte("global rules\n"), 0644); err != nil {
		t.Fatalf("write global rules: %v", err)
	}

	projectAgents := filepath.Join(repo, "AGENTS.md")
	if err := os.WriteFile(projectAgents, []byte("project agents\n"), 0644); err != nil {
		t.Fatalf("write repo AGENTS.md: %v", err)
	}

	cfg := &config.Config{
		Version: 1,
		Projects: map[string]config.Project{
			"proj": {Path: repo},
		},
		Agents: map[string]config.Agent{
			"cursor":   {Enabled: false},
			"claude":   {Enabled: false},
			"codex":    {Enabled: true},
			"opencode": {Enabled: false},
			"copilot":  {Enabled: false},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	oldFlags := Flags
	oldRefreshImport := refreshImport
	Flags = GlobalFlags{}
	refreshImport = false
	defer func() {
		Flags = oldFlags
		refreshImport = oldRefreshImport
	}()

	if err := runRefresh("proj"); err != nil {
		t.Fatalf("runRefresh: %v", err)
	}

	projectCanonical := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if got, err := os.ReadFile(projectCanonical); err != nil {
		t.Fatalf("read canonical project agents: %v", err)
	} else if string(got) != "project agents\n" {
		t.Fatalf("canonical project agents = %q, want project agents", string(got))
	}

	if linkTarget, err := os.Readlink(projectAgents); err != nil {
		t.Fatalf("repo AGENTS.md should be a symlink after refresh: %v", err)
	} else if linkTarget != projectCanonical {
		t.Fatalf("repo AGENTS.md linked to %q, want %q", linkTarget, projectCanonical)
	}

	if got, err := os.ReadFile(projectAgents); err != nil {
		t.Fatalf("read refreshed repo AGENTS.md: %v", err)
	} else if string(got) != "project agents\n" {
		t.Fatalf("refreshed repo AGENTS.md = %q, want project agents", string(got))
	}

	resourceBackup := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	if got, err := os.ReadFile(resourceBackup); err != nil {
		t.Fatalf("read resource backup: %v", err)
	} else if string(got) != "project agents\n" {
		t.Fatalf("resource backup = %q, want project agents", string(got))
	}
}

func TestRefreshReplacesExistingCanonicalAgentsMarkdownFromUnmanagedRepoFile(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	agentsHome := filepath.Join(tmp, ".agents")
	repo := filepath.Join(tmp, "repo")
	binDir := filepath.Join(tmp, "bin")

	for _, dir := range []string{home, agentsHome, repo, binDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	t.Setenv("HOME", home)
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	codexBin := filepath.Join(binDir, "codex")
	if err := os.WriteFile(codexBin, []byte("#!/bin/sh\necho codex test\n"), 0755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	projectCanonical := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if err := os.MkdirAll(filepath.Dir(projectCanonical), 0755); err != nil {
		t.Fatalf("mkdir project canonical dir: %v", err)
	}
	if err := os.WriteFile(projectCanonical, []byte("old canonical\n"), 0644); err != nil {
		t.Fatalf("write old canonical agents: %v", err)
	}

	projectAgents := filepath.Join(repo, "AGENTS.md")
	if err := os.WriteFile(projectAgents, []byte("new project agents\n"), 0644); err != nil {
		t.Fatalf("write repo AGENTS.md: %v", err)
	}

	cfg := &config.Config{
		Version: 1,
		Projects: map[string]config.Project{
			"proj": {Path: repo},
		},
		Agents: map[string]config.Agent{
			"cursor":   {Enabled: false},
			"claude":   {Enabled: false},
			"codex":    {Enabled: true},
			"opencode": {Enabled: false},
			"copilot":  {Enabled: false},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	oldFlags := Flags
	oldRefreshImport := refreshImport
	Flags = GlobalFlags{}
	refreshImport = false
	defer func() {
		Flags = oldFlags
		refreshImport = oldRefreshImport
	}()

	if err := runRefresh("proj"); err != nil {
		t.Fatalf("runRefresh: %v", err)
	}

	if got, err := os.ReadFile(projectCanonical); err != nil {
		t.Fatalf("read canonical project agents: %v", err)
	} else if string(got) != "new project agents\n" {
		t.Fatalf("canonical project agents = %q, want new project agents", string(got))
	}

	if linkTarget, err := os.Readlink(projectAgents); err != nil {
		t.Fatalf("repo AGENTS.md should be a symlink after refresh: %v", err)
	} else if linkTarget != projectCanonical {
		t.Fatalf("repo AGENTS.md linked to %q, want %q", linkTarget, projectCanonical)
	}
}

// TestRefreshReplacesImportedRepoSkillDirWithManagedSymlink ensures the full refresh
// pipeline (import-from-refresh → RunSharedTargetProjection → per-platform CreateLinks)
// replaces a non-symlink imported skill directory under .agents/skills/ with the managed
// symlink to ~/.agents/skills/<project>/<name>/, matching the shared executor contract.
func TestRefreshReplacesImportedRepoSkillDirWithManagedSymlink(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	agentsHome := filepath.Join(tmp, ".agents")
	repo := filepath.Join(tmp, "repo")

	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	for _, dir := range []string{agentsHome, repo} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	t.Setenv("HOME", home)
	t.Setenv("AGENTS_HOME", agentsHome)

	skillDir := filepath.Join(agentsHome, "skills", "proj", "review")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir canonical skill: %v", err)
	}
	canonicalSkill := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(canonicalSkill, []byte("---\nname: review\ndescription: canonical\n---\n"), 0644); err != nil {
		t.Fatalf("write canonical SKILL.md: %v", err)
	}

	importedSkill := filepath.Join(repo, ".agents", "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(importedSkill), 0755); err != nil {
		t.Fatalf("mkdir imported skill path: %v", err)
	}
	if err := os.WriteFile(importedSkill, []byte("---\nname: review\ndescription: imported copy\n---\n"), 0644); err != nil {
		t.Fatalf("write imported SKILL.md: %v", err)
	}

	cfg := &config.Config{
		Version: 1,
		Projects: map[string]config.Project{
			"proj": {Path: repo},
		},
		Agents: map[string]config.Agent{
			"cursor":   {Enabled: false},
			"claude":   {Enabled: true},
			"codex":    {Enabled: false},
			"opencode": {Enabled: false},
			"copilot":  {Enabled: false},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	oldFlags := Flags
	oldRefreshImport := refreshImport
	Flags = GlobalFlags{}
	Flags.DryRun = false
	refreshImport = false
	defer func() {
		Flags = oldFlags
		refreshImport = oldRefreshImport
	}()

	if err := runRefresh("proj"); err != nil {
		t.Fatalf("runRefresh: %v", err)
	}

	repoSkillLink := filepath.Join(repo, ".agents", "skills", "review")
	got, err := os.Readlink(repoSkillLink)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", repoSkillLink, err)
	}
	if got != skillDir {
		t.Fatalf("symlink target = %q, want %q", got, skillDir)
	}
}
