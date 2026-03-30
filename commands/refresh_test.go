package commands

import (
	"testing"
)

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
		{".github/agents/my-agent.agent.md", "agents/proj/my-agent/AGENT.md"},
		{".codex/agents/my-agent/AGENT.md", "agents/proj/my-agent/AGENT.md"},
		{".opencode/agent/my-agent.md", "agents/proj/my-agent/AGENT.md"},
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
