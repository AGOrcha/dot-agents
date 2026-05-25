package lifecycle

import (
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/platform"
)

// Repo-relative path constants used by MapResourceRelToDest and
// referenced cross-package by commands/import.go (via aliasing const
// declarations) and commands/add.go. Lifted from commands/import.go in
// plan root-command-decomposition t02b so the resource-relative ↔
// canonical-dest mapping can live alongside its consumers in the
// lifecycle subpackage.
const (
	RelClaudeSettingsJSON    = ".claude/settings.json"
	RelCursorSettingsJSON    = ".cursor/settings.json"
	RelCursorMCPJSON         = ".cursor/mcp.json"
	RelCursorHooksJSON       = ".cursor/hooks.json"
	RelCursorIgnore          = ".cursorignore"
	RelCursorIndexingIgnore  = ".cursorindexingignore"
	RelClaudeSettingsLocal   = ".claude/settings.local.json"
	RelMCPJSON               = ".mcp.json"
	RelVSCodeMCPJSON         = ".vscode/mcp.json"
	RelOpenCodeJSON          = "opencode.json"
	RelAgentsMD              = "AGENTS.md"
	RelCodexInstructionsMD   = ".codex/instructions.md"
	RelCodexRulesMD          = ".codex/rules.md"
	RelCodexConfigTOML       = ".codex/config.toml"
	RelCodexHooksJSON        = ".codex/hooks.json"
	RelCopilotInstructionsMD = ".github/copilot-instructions.md"
	RelCursorCommandsDir     = ".cursor/commands/"
	RelClaudeCommandsDir     = ".claude/commands/"
	RelOpenCodeCommandsDir   = ".opencode/commands/"
	RelClaudeOutputStylesDir = ".claude/output-styles/"
	RelOpenCodeModesDir      = ".opencode/modes/"
	RelOpenCodeThemesDir     = ".opencode/themes/"
	RelGitHubPromptsDir      = ".github/prompts/"
	RelCursorRulesDir        = ".cursor/rules/"
	RelAgentsSkillsDir       = ".agents/skills/"
	RelClaudeSkillsDir       = ".claude/skills/"
	RelGitHubAgentsDir       = ".github/agents/"
	RelCodexAgentsDir        = ".codex/agents/"
	RelOpenCodeAgentsDir     = ".opencode/agent/"
	RelGitHubHooksDir        = ".github/hooks/"
	RelAgentMarkdownSuffix   = ".agent.md"
	RelJSONSuffix            = ".json"
	AgentsHooksPrefix        = "hooks/"
)

// MapResourceRelToDest maps a project-relative input path (e.g.
// ".cursor/rules/global--foo.mdc") to the canonical ~/.agents-relative
// destination (e.g. "rules/global/foo.mdc"). Returns "" when no mapping
// applies. Lifted from commands/refresh.go in
// root-command-decomposition t02b; behavior unchanged from the original.
func MapResourceRelToDest(project, relPath string) string {
	// Explicit repo-relative → ~/.agents-relative mappings.
	// All platform MCP sources normalize into the same canonical mcp.json.
	switch relPath {
	case RelCursorSettingsJSON:
		return "settings/" + project + "/cursor.json"
	case RelCursorMCPJSON:
		return "mcp/" + project + "/mcp.json"
	case RelCursorHooksJSON:
		return AgentsHooksPrefix + project + "/cursor.json"
	case RelCursorIgnore:
		return "settings/" + project + "/cursorignore"
	case RelCursorIndexingIgnore:
		return platform.CanonicalBucketScopePath(platform.CanonicalBucketIgnore, project, "cursorindexingignore")
	case RelClaudeSettingsLocal:
		return "settings/" + project + "/claude-code.json"
	case RelMCPJSON:
		return "mcp/" + project + "/mcp.json"
	case RelVSCodeMCPJSON:
		return "mcp/" + project + "/mcp.json"
	case RelOpenCodeJSON:
		return "settings/" + project + "/opencode.json"
	case RelAgentsMD:
		return "rules/" + project + "/agents.md"
	case RelCodexInstructionsMD, RelCodexRulesMD:
		return "rules/" + project + "/agents.md"
	case RelCodexConfigTOML:
		return "settings/" + project + "/codex.toml"
	case RelCodexHooksJSON:
		return AgentsHooksPrefix + project + "/codex.json"
	case RelCopilotInstructionsMD:
		return "rules/" + project + "/copilot-instructions.md"
	}

	// Directory-bucket mappings. The relPath is a full walked file path like
	// ".cursor/commands/foo.md"; the constants are directory prefixes ending
	// in "/". These MUST be prefix matches (not exact-string switch cases) or
	// the bucket files silently fall through and are dropped from recovery.
	for _, m := range []struct {
		prefix string
		bucket platform.CanonicalBucket
	}{
		{RelCursorCommandsDir, platform.CanonicalBucketCommands},
		{RelClaudeCommandsDir, platform.CanonicalBucketCommands},
		{RelOpenCodeCommandsDir, platform.CanonicalBucketCommands},
		{RelClaudeOutputStylesDir, platform.CanonicalBucketOutputStyles},
		{RelOpenCodeModesDir, platform.CanonicalBucketModes},
		{RelOpenCodeThemesDir, platform.CanonicalBucketThemes},
		{RelGitHubPromptsDir, platform.CanonicalBucketPrompts},
	} {
		if strings.HasPrefix(relPath, m.prefix) {
			return platform.CanonicalBucketScopePath(m.bucket, project, strings.TrimPrefix(relPath, m.prefix))
		}
	}

	// .cursor/rules/ → rules/
	if strings.HasPrefix(relPath, RelCursorRulesDir) {
		name := filepath.Base(relPath)
		if strings.HasPrefix(name, "global--") {
			return "rules/global/" + strings.TrimPrefix(name, "global--")
		} else if strings.HasPrefix(name, project+"--") {
			return "rules/" + project + "/" + strings.TrimPrefix(name, project+"--")
		} else if strings.HasSuffix(name, ".mdc") || strings.HasSuffix(name, ".md") {
			return "rules/" + project + "/" + name
		}
		return ""
	}

	// .agents/skills/<name>/<path> → skills/<project>/<name>/<path>
	if strings.HasPrefix(relPath, RelAgentsSkillsDir) {
		rest := strings.TrimPrefix(relPath, RelAgentsSkillsDir)
		return "skills/" + project + "/" + rest
	}
	// .claude/skills/<name>/<path> → skills/<project>/<name>/<path>
	if strings.HasPrefix(relPath, RelClaudeSkillsDir) {
		rest := strings.TrimPrefix(relPath, RelClaudeSkillsDir)
		return "skills/" + project + "/" + rest
	}

	// .github/agents/<name>.agent.md → agents/<project>/<name>/AGENT.md
	if strings.HasPrefix(relPath, RelGitHubAgentsDir) && strings.HasSuffix(relPath, RelAgentMarkdownSuffix) {
		name := strings.TrimSuffix(filepath.Base(relPath), RelAgentMarkdownSuffix)
		return "agents/" + project + "/" + name + "/AGENT.md"
	}

	// .codex/agents/<name>/<path> → agents/<project>/<name>/<path>
	if strings.HasPrefix(relPath, RelCodexAgentsDir) {
		rest := strings.TrimPrefix(relPath, RelCodexAgentsDir)
		return "agents/" + project + "/" + rest
	}

	// .opencode/agent/<name>.md → agents/<project>/<name>/AGENT.md
	if strings.HasPrefix(relPath, RelOpenCodeAgentsDir) && strings.HasSuffix(relPath, ".md") {
		name := strings.TrimSuffix(filepath.Base(relPath), ".md")
		return "agents/" + project + "/" + name + "/AGENT.md"
	}

	// .github/hooks/<name>.json → hooks/<project>/<name>.json
	if strings.HasPrefix(relPath, RelGitHubHooksDir) && strings.HasSuffix(relPath, RelJSONSuffix) {
		name := strings.TrimSuffix(filepath.Base(relPath), RelJSONSuffix)
		return AgentsHooksPrefix + project + "/" + name + "/HOOK.yaml"
	}

	// Pass-through: paths already under known ~/.agents dirs
	for _, prefix := range []string{
		"rules/",
		"settings/",
		"mcp/",
		"skills/",
		"agents/",
		AgentsHooksPrefix,
		string(platform.CanonicalBucketCommands) + "/",
		string(platform.CanonicalBucketOutputStyles) + "/",
		string(platform.CanonicalBucketIgnore) + "/",
		string(platform.CanonicalBucketModes) + "/",
		string(platform.CanonicalBucketPlugins) + "/",
		string(platform.CanonicalBucketThemes) + "/",
		string(platform.CanonicalBucketPrompts) + "/",
	} {
		if strings.HasPrefix(relPath, prefix) {
			return relPath
		}
	}

	// Root-level flat files → settings/<project>/
	if !strings.Contains(relPath, "/") {
		return "settings/" + project + "/" + relPath
	}
	return ""
}

// IsManagedSymlink reports whether path is a resolvable managed link
// (POSIX symlink / Windows junction) whose resolved target lies under
// agentsHome. A Windows hard-linked managed *file* has no reparse point
// to test against a prefix and is reported false here, matching the
// prior symlink-only behavior on POSIX. Lifted from commands/import.go
// in root-command-decomposition t02b.
func IsManagedSymlink(path, agentsHome string) bool {
	raw, ok := links.ManagedLinkTarget(path)
	if !ok {
		return false
	}
	dest := raw
	if !filepath.IsAbs(dest) {
		dest = filepath.Clean(filepath.Join(filepath.Dir(path), dest))
	}
	agentsHome = filepath.Clean(agentsHome) + string(filepath.Separator)
	return strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator), agentsHome)
}
