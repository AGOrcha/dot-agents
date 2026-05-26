package lifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------- MapResourceRelToDest ----------

const canonicalAgentPath = "agents/proj/my-agent/AGENT.md"

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
		got := MapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("MapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
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
		{".github/agents/my-agent.agent.md", canonicalAgentPath},
		{".codex/agents/my-agent/AGENT.md", canonicalAgentPath},
		{".opencode/agent/my-agent.md", canonicalAgentPath},
	}
	for _, c := range cases {
		got := MapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("MapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
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
		got := MapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("MapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
}

func TestMapResourceRelToDest_RootFlatFileFallsBackToSettings(t *testing.T) {
	got := MapResourceRelToDest("proj", "somefile.txt")
	want := "settings/proj/somefile.txt"
	if got != want {
		t.Errorf("MapResourceRelToDest root-flat = %q, want %q", got, want)
	}
}

func TestMapResourceRelToDest_UnknownPathReturnsEmpty(t *testing.T) {
	got := MapResourceRelToDest("proj", "nested/unknown/path.txt")
	if got != "" {
		t.Errorf("MapResourceRelToDest unknown = %q, want empty", got)
	}
}

// ---------- IsManagedSymlink ----------

func TestIsManagedSymlink_PlainFileNotASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: hard-linked files on Windows have no reparse point")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	_ = os.MkdirAll(agentsHome, 0755)

	plain := filepath.Join(tmp, "plain.txt")
	_ = os.WriteFile(plain, []byte("hi"), 0644)

	if IsManagedSymlink(plain, agentsHome) {
		t.Error("plain file should not be managed symlink")
	}
}

func TestIsManagedSymlink_ManagedLinkUnderAgentsHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	_ = os.MkdirAll(agentsHome, 0755)
	target := filepath.Join(agentsHome, "real.txt")
	_ = os.WriteFile(target, []byte("hi"), 0644)

	managed := filepath.Join(tmp, "linked.txt")
	if err := os.Symlink(target, managed); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if !IsManagedSymlink(managed, agentsHome) {
		t.Error("expected managed symlink under agentsHome to be detected")
	}
}

func TestIsManagedSymlink_UnmanagedLinkOutsideAgentsHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	_ = os.MkdirAll(agentsHome, 0755)

	outsideTarget := filepath.Join(tmp, "outside.txt")
	_ = os.WriteFile(outsideTarget, []byte("hi"), 0644)

	unmanaged := filepath.Join(tmp, "linked.txt")
	if err := os.Symlink(outsideTarget, unmanaged); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if IsManagedSymlink(unmanaged, agentsHome) {
		t.Error("link to outside agentsHome should not be managed")
	}
}

func TestIsManagedSymlink_NonexistentPathIsFalse(t *testing.T) {
	tmp := t.TempDir()
	if IsManagedSymlink(filepath.Join(tmp, "nope"), tmp) {
		t.Error("nonexistent path should be false")
	}
}

// ---------- Constants ----------

func TestAgentsHooksPrefix_StableValue(t *testing.T) {
	if AgentsHooksPrefix != "hooks/" {
		t.Errorf("AgentsHooksPrefix = %q, want %q", AgentsHooksPrefix, "hooks/")
	}
}

func TestRelCursorRulesDir_StableValue(t *testing.T) {
	if RelCursorRulesDir != ".cursor/rules/" {
		t.Errorf("RelCursorRulesDir = %q, want %q", RelCursorRulesDir, ".cursor/rules/")
	}
}

// Hooks + ignore aliases live in the explicit switch and must map to
// the hooks/ and ignore-bucket trees respectively. Both names are easy
// to swap accidentally during hook refactors — table-driven coverage
// keeps the mapping pinned.
func TestMapResourceRelToDest_HooksAndIndexingIgnore(t *testing.T) {
	cases := []struct {
		relPath  string
		expected string
	}{
		{RelCursorHooksJSON, AgentsHooksPrefix + "proj/cursor.json"},
	}
	for _, c := range cases {
		got := MapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("MapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
	// .cursorindexingignore goes through platform.CanonicalBucketScopePath;
	// we only assert the result is non-empty and contains the bucket
	// segment so the test stays decoupled from the canonical-bucket
	// path-rendering details (covered in platform tests).
	got := MapResourceRelToDest("proj", RelCursorIndexingIgnore)
	if got == "" || !strings.Contains(got, "ignore") {
		t.Errorf("MapResourceRelToDest(%q) = %q, expected non-empty ignore-bucket path", RelCursorIndexingIgnore, got)
	}
}

// Directory-bucket prefix mappings cover the .cursor/commands/,
// .claude/commands/, .opencode/commands/, .claude/output-styles/,
// .opencode/modes/, .opencode/themes/, and .github/prompts/ buckets.
// Each of these is a strings.HasPrefix branch in the directory-bucket
// loop that, if dropped, silently loses bucket files from restore.
func TestMapResourceRelToDest_DirectoryBuckets(t *testing.T) {
	cases := []struct {
		relPath  string
		contains string
	}{
		{".cursor/commands/foo.md", "commands"},
		{".claude/commands/foo.md", "commands"},
		{".opencode/commands/foo.md", "commands"},
		{".claude/output-styles/foo.md", "output-styles"},
		{".opencode/modes/foo.json", "modes"},
		{".opencode/themes/foo.toml", "themes"},
		{".github/prompts/foo.md", "prompts"},
	}
	for _, c := range cases {
		got := MapResourceRelToDest("proj", c.relPath)
		if got == "" || !strings.Contains(got, c.contains) {
			t.Errorf("MapResourceRelToDest(%q) = %q, expected non-empty path containing %q", c.relPath, got, c.contains)
		}
	}
}

// A .cursor/rules/foo file whose name uses NEITHER the global-- nor
// the <project>-- namespace AND whose extension is not .md/.mdc has no
// valid mapping and must return "" so the caller knows to skip it.
func TestMapResourceRelToDest_CursorRulesUnknownExtReturnsEmpty(t *testing.T) {
	got := MapResourceRelToDest("proj", ".cursor/rules/loose.txt")
	if got != "" {
		t.Errorf("expected empty mapping for unknown rule extension, got %q", got)
	}
}

// Pass-through behavior: a path already under a known ~/.agents-relative
// prefix (rules/, settings/, mcp/, skills/, agents/, hooks/, etc.) is
// returned unchanged so MapResourceRelToDest is idempotent under
// re-execution against an already-canonicalized resources tree.
func TestMapResourceRelToDest_PassThroughKnownPrefixes(t *testing.T) {
	cases := []string{
		"rules/proj/agents.md",
		"settings/proj/codex.toml",
		"mcp/proj/mcp.json",
		"skills/proj/my-skill/SKILL.md",
		"agents/proj/my-agent/AGENT.md",
		"hooks/proj/cursor.json",
	}
	for _, in := range cases {
		got := MapResourceRelToDest("proj", in)
		if got != in {
			t.Errorf("expected pass-through for %q, got %q", in, got)
		}
	}
}

// IsManagedSymlink must resolve a RELATIVE symlink target against the
// directory containing the link (filepath.Dir(path)) before comparing
// against agentsHome. A relative-target link that resolves under
// agentsHome should still be reported as managed.
func TestIsManagedSymlink_RelativeTargetResolvesAgainstLinkDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	_ = os.MkdirAll(agentsHome, 0755)
	target := filepath.Join(agentsHome, "real.txt")
	_ = os.WriteFile(target, []byte("hi"), 0644)

	// Place the link in the SAME directory as the target and use a
	// relative target so the !IsAbs branch fires.
	link := filepath.Join(agentsHome, "linked.txt")
	if err := os.Symlink("real.txt", link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if !IsManagedSymlink(link, agentsHome) {
		t.Error("relative-target symlink resolving under agentsHome should be managed")
	}
}
