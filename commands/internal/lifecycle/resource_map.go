package lifecycle

import (
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
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

	// Canonical ~/.agents/ bucket prefixes used as outputs by
	// MapResourceRelToDest. Extracted from inline string literals to
	// satisfy Sonar S1192 (duplicated-literal lint) and make a future
	// rename of a bucket a one-line edit.
	canonSettingsPrefix = "settings/"
	canonRulesPrefix    = "rules/"
	canonSkillsPrefix   = "skills/"
	canonAgentsPrefix   = "agents/"
	canonMCPRelFile     = "mcp.json"
)

// MapResourceRelToDest maps a project-relative input path (e.g.
// ".cursor/rules/global--foo.mdc") to the canonical ~/.agents-relative
// destination (e.g. "rules/global/foo.mdc"). Returns "" when no mapping
// applies. Lifted from commands/refresh.go in
// root-command-decomposition t02b; behavior unchanged from the original.
//
// Implementation note: the logic is split into focused helpers
// (mapExactRel, mapBucketDirRel, mapCursorRulesRel, mapSkillsRel,
// mapAgentsRel, mapHooksRel, mapPassThroughRel) so each branch has a
// single concern and the top-level function stays inside Sonar's
// cognitive-complexity limit (S3776).
func MapResourceRelToDest(project, relPath string) string {
	if dest, ok := mapExactRel(project, relPath); ok {
		return dest
	}
	if dest, ok := mapBucketDirRel(project, relPath); ok {
		return dest
	}
	if strings.HasPrefix(relPath, RelCursorRulesDir) {
		return mapCursorRulesRel(project, relPath)
	}
	if dest, ok := mapSkillsRel(project, relPath); ok {
		return dest
	}
	if dest, ok := mapAgentsRel(project, relPath); ok {
		return dest
	}
	if dest, ok := mapHooksRel(project, relPath); ok {
		return dest
	}
	if mapPassThroughRel(relPath) {
		return relPath
	}
	// Root-level flat files → settings/<project>/
	if !strings.Contains(relPath, "/") {
		return canonSettingsPrefix + project + "/" + relPath
	}
	return ""
}

// mapExactRel handles repo-relative ↔ canonical-dest mappings that are
// resolved purely by an exact filename match. Returns (dest, true) on
// hit; ("", false) otherwise. All platform MCP sources normalize into
// the same canonical mcp.json.
func mapExactRel(project, relPath string) (string, bool) {
	switch relPath {
	case RelCursorSettingsJSON:
		return canonSettingsPrefix + project + "/cursor.json", true
	case RelCursorMCPJSON, RelMCPJSON, RelVSCodeMCPJSON:
		return "mcp/" + project + "/" + canonMCPRelFile, true
	case RelCursorHooksJSON:
		return AgentsHooksPrefix + project + "/cursor.json", true
	case RelCursorIgnore:
		return canonSettingsPrefix + project + "/cursorignore", true
	case RelCursorIndexingIgnore:
		return platform.CanonicalBucketScopePath(platform.CanonicalBucketIgnore, project, "cursorindexingignore"), true
	case RelClaudeSettingsLocal:
		return canonSettingsPrefix + project + "/claude-code.json", true
	case RelOpenCodeJSON:
		return canonSettingsPrefix + project + "/opencode.json", true
	case RelAgentsMD, RelCodexInstructionsMD, RelCodexRulesMD:
		return canonRulesPrefix + project + "/agents.md", true
	case RelCodexConfigTOML:
		return canonSettingsPrefix + project + "/codex.toml", true
	case RelCodexHooksJSON:
		return AgentsHooksPrefix + project + "/codex.json", true
	case RelCopilotInstructionsMD:
		return canonRulesPrefix + project + "/copilot-instructions.md", true
	}
	return "", false
}

// bucketDirMapping is the static table mapping a repo-relative directory
// prefix to its canonical platform bucket. Defined as a package-level
// variable rather than recreated on each call.
var bucketDirMapping = []struct {
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
}

// mapBucketDirRel handles directory-prefix mappings into canonical
// platform buckets. The relPath is a full walked file path like
// ".cursor/commands/foo.md"; the prefixes end in "/".
func mapBucketDirRel(project, relPath string) (string, bool) {
	for _, m := range bucketDirMapping {
		if strings.HasPrefix(relPath, m.prefix) {
			return platform.CanonicalBucketScopePath(m.bucket, project, strings.TrimPrefix(relPath, m.prefix)), true
		}
	}
	return "", false
}

// mapCursorRulesRel handles .cursor/rules/ → rules/ name extraction.
// Caller has already confirmed the RelCursorRulesDir prefix; this
// function returns "" when no name pattern applies (silently dropped).
func mapCursorRulesRel(project, relPath string) string {
	name := filepath.Base(relPath)
	if strings.HasPrefix(name, "global--") {
		return canonRulesPrefix + "global/" + strings.TrimPrefix(name, "global--")
	}
	if strings.HasPrefix(name, project+"--") {
		return canonRulesPrefix + project + "/" + strings.TrimPrefix(name, project+"--")
	}
	if strings.HasSuffix(name, ".mdc") || strings.HasSuffix(name, ".md") {
		return canonRulesPrefix + project + "/" + name
	}
	return ""
}

// mapSkillsRel handles {.agents,.claude}/skills/ → skills/<project>/.
func mapSkillsRel(project, relPath string) (string, bool) {
	if strings.HasPrefix(relPath, RelAgentsSkillsDir) {
		return canonSkillsPrefix + project + "/" + strings.TrimPrefix(relPath, RelAgentsSkillsDir), true
	}
	if strings.HasPrefix(relPath, RelClaudeSkillsDir) {
		return canonSkillsPrefix + project + "/" + strings.TrimPrefix(relPath, RelClaudeSkillsDir), true
	}
	return "", false
}

// mapAgentsRel handles {.github,.codex,.opencode}/agents/ →
// agents/<project>/. Three variants on the same theme.
func mapAgentsRel(project, relPath string) (string, bool) {
	if strings.HasPrefix(relPath, RelGitHubAgentsDir) && strings.HasSuffix(relPath, RelAgentMarkdownSuffix) {
		name := strings.TrimSuffix(filepath.Base(relPath), RelAgentMarkdownSuffix)
		return canonAgentsPrefix + project + "/" + name + "/AGENT.md", true
	}
	if strings.HasPrefix(relPath, RelCodexAgentsDir) {
		return canonAgentsPrefix + project + "/" + strings.TrimPrefix(relPath, RelCodexAgentsDir), true
	}
	if strings.HasPrefix(relPath, RelOpenCodeAgentsDir) && strings.HasSuffix(relPath, ".md") {
		name := strings.TrimSuffix(filepath.Base(relPath), ".md")
		return canonAgentsPrefix + project + "/" + name + "/AGENT.md", true
	}
	return "", false
}

// mapHooksRel handles .github/hooks/<name>.json → hooks/<project>/<name>/HOOK.yaml.
func mapHooksRel(project, relPath string) (string, bool) {
	if strings.HasPrefix(relPath, RelGitHubHooksDir) && strings.HasSuffix(relPath, RelJSONSuffix) {
		name := strings.TrimSuffix(filepath.Base(relPath), RelJSONSuffix)
		return AgentsHooksPrefix + project + "/" + name + "/HOOK.yaml", true
	}
	return "", false
}

// passThroughPrefixes lists canonical ~/.agents/ bucket prefixes that
// map to themselves (input already in canonical form).
var passThroughPrefixes = []string{
	canonRulesPrefix,
	canonSettingsPrefix,
	"mcp/",
	canonSkillsPrefix,
	canonAgentsPrefix,
	AgentsHooksPrefix,
	string(platform.CanonicalBucketCommands) + "/",
	string(platform.CanonicalBucketOutputStyles) + "/",
	string(platform.CanonicalBucketIgnore) + "/",
	string(platform.CanonicalBucketModes) + "/",
	string(platform.CanonicalBucketPlugins) + "/",
	string(platform.CanonicalBucketThemes) + "/",
	string(platform.CanonicalBucketPrompts) + "/",
}

// mapPassThroughRel reports whether relPath is already under a known
// canonical ~/.agents/ bucket (caller returns relPath unchanged).
func mapPassThroughRel(relPath string) bool {
	for _, prefix := range passThroughPrefixes {
		if strings.HasPrefix(relPath, prefix) {
			return true
		}
	}
	return false
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
