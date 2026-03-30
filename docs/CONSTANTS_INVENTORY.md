# Constants Inventory

Generated on 2026-03-30 from this repo checkout.

Scope:
- Go `const` declarations (`*.go`)
- Shell uppercase assignments (`*.sh`) matching `^[A-Z][A-Z0-9_]*=`

Notes:
- This is a declaration inventory, not a full literal-value inventory.
- Counts in this snapshot: `105` Go constants, `53` shell assignments.

## Go Constants

```text
commands/import.go:103|importScopeProject|"project"
commands/import.go:104|importScopeGlobal|"global"
commands/import.go:105|importScopeAll|"all"
commands/import.go:106|importFailedFmt|"Failed to import %s: %v"
commands/import.go:108|relClaudeSettingsJSON|".claude/settings.json"
commands/import.go:109|relCursorSettingsJSON|".cursor/settings.json"
commands/import.go:110|relCursorMCPJSON|".cursor/mcp.json"
commands/import.go:111|relCursorHooksJSON|".cursor/hooks.json"
commands/import.go:112|relCursorIgnore|".cursorignore"
commands/import.go:113|relClaudeSettingsLocal|".claude/settings.local.json"
commands/import.go:114|relMCPJSON|".mcp.json"
commands/import.go:115|relVSCodeMCPJSON|".vscode/mcp.json"
commands/import.go:116|relOpenCodeJSON|"opencode.json"
commands/import.go:117|relAgentsMD|"AGENTS.md"
commands/import.go:118|relCodexInstructionsMD|".codex/instructions.md"
commands/import.go:119|relCodexRulesMD|".codex/rules.md"
commands/import.go:120|relCodexConfigTOML|".codex/config.toml"
commands/import.go:121|relCodexHooksJSON|".codex/hooks.json"
commands/import.go:122|relCopilotInstructionsMD|".github/copilot-instructions.md"
commands/import.go:123|relClaudeREADME|".claude/CLAUDE.md"
commands/import.go:124|relCursorRulesDir|".cursor/rules/"
commands/import.go:125|relAgentsSkillsDir|".agents/skills/"
commands/import.go:126|relClaudeSkillsDir|".claude/skills/"
commands/import.go:127|relGitHubAgentsDir|".github/agents/"
commands/import.go:128|relCodexAgentsDir|".codex/agents/"
commands/import.go:129|relOpenCodeAgentsDir|".opencode/agent/"
commands/import.go:130|relGitHubHooksDir|".github/hooks/"
commands/import.go:131|relAgentMarkdownSuffix|".agent.md"
commands/import.go:132|relJSONSuffix|".json"
commands/import.go:133|agentsHooksPrefix|"hooks/"
commands/import_test.go:12|canonicalImportProject|"proj"
commands/import_test.go:13|promptLogJSON|"prompt-log.json"
commands/import_test.go:14|yamlUnmarshalFailedFmt|"yaml.Unmarshal failed: %v\n%s"
commands/refresh_test.go:7|refreshCanonicalAgentPath|"agents/proj/my-agent/AGENT.md"
commands/status.go:19|statusHooksJSON|"hooks.json"
commands/status.go:20|statusCodexDir|".codex"
commands/status.go:21|statusAgentsDir|".agents"
commands/status.go:22|statusOpenCodeDir|".opencode"
commands/status.go:23|statusGitHubDir|".github"
commands/status.go:24|statusLocalFileFmt|"    %s○%s %s %s(local file)%s\n"
commands/status.go:25|statusCursorDir|".cursor"
commands/status.go:26|statusAgentsMarkdown|"AGENTS.md"
commands/status.go:27|statusCopilotInstructions|"copilot-instructions.md"
internal/config/agentsrc.go:39|AgentsRCFile|".agentsrc.json"
internal/platform/claude.go:16|claudeCodeJSON|"claude-code.json"
internal/platform/claude.go:17|claudeSettingsJSON|"settings.json"
internal/platform/claude.go:18|claudeSettingsLocalJSON|"settings.local.json"
internal/platform/codex.go:17|codexHooksJSON|"hooks.json"
internal/platform/codex_test.go:10|codexAgentMarkdownFile|"AGENT.md"
internal/platform/copilot.go:16|copilotMCPJSON|"mcp.json"
internal/platform/copilot.go:17|copilotClaudeDir|".claude"
internal/platform/copilot.go:18|copilotSettingsLocalJSON|"settings.local.json"
internal/platform/cursor.go:15|cursorHooksFile|"hooks.json"
internal/platform/cursor.go:16|cursorJSON|"cursor.json"
internal/platform/hooks.go:20|HookSourceLegacyFile|"legacy_file"
internal/platform/hooks.go:21|HookSourceCanonicalBundle|"canonical_bundle"
internal/platform/hooks.go:27|HookShapeDirect|"direct"
internal/platform/hooks.go:28|HookShapeRenderSingle|"render_single"
internal/platform/hooks.go:29|HookShapeRenderFanout|"render_fanout"
internal/platform/hooks.go:35|HookTransportSymlink|"symlink"
internal/platform/hooks.go:36|HookTransportHardlink|"hardlink"
internal/platform/hooks.go:37|HookTransportWrite|"write"
internal/platform/hooks_test.go:11|hooksTestAgentsDir|".agents"
internal/platform/hooks_test.go:12|hooksTestClaudeCompatFile|"claude-code.json"
internal/platform/hooks_test.go:13|hooksTestCanonicalHookName|"format-write"
internal/platform/hooks_test.go:14|hooksTestCanonicalMatcherExpr|"Write | Edit"
internal/platform/hooks_test.go:15|hooksTestCanonicalRunCommand|"/tmp/run.sh"
internal/platform/opencode.go:15|opencodeJSON|"opencode.json"
internal/platform/platform_test.go:10|platformTestExpectedSymlinkFmt|"expected %s to be a symlink: %v"
internal/platform/platform_test.go:11|platformTestExpectedSymlinkTargetFmt|"expected %s to point to %s, got %s"
internal/platform/stage1_integration_test.go:15|fixtureProject|"proj"
internal/platform/stage1_integration_test.go:16|hookManifestName|"HOOK.yaml"
internal/platform/stage1_integration_test.go:17|fixtureNoopScriptSh|"#!/bin/sh\nexit 0\n"
internal/platform/stage1_integration_test.go:18|dirAgents|".agents"
internal/platform/stage1_integration_test.go:19|dirClaude|".claude"
internal/platform/stage1_integration_test.go:20|dirCursor|".cursor"
internal/platform/stage1_integration_test.go:21|dirCodex|".codex"
internal/platform/stage1_integration_test.go:22|dirGithub|".github"
internal/platform/stage1_integration_test.go:23|fileSettingsJSON|"settings.json"
internal/platform/stage1_integration_test.go:24|fileSettingsLocalJSON|"settings.local.json"
internal/platform/stage1_integration_test.go:25|fileHooksJSON|"hooks.json"
internal/platform/stage1_integration_test.go:26|fileCursorJSON|"cursor.json"
internal/platform/stage1_integration_test.go:27|fileMCPJSON|"mcp.json"
internal/platform/stage1_integration_test.go:28|fileClaudeCodeJSON|"claude-code.json"
internal/platform/stage1_integration_test.go:29|filePreToolJSON|"pre-tool.json"
internal/platform/stage1_integration_test.go:30|filePostSaveJSON|"post-save.json"
internal/platform/stage1_integration_test.go:31|fileCopilotInstructionsMD|"copilot-instructions.md"
internal/platform/stage1_integration_test.go:32|filePromptLogJSON|"prompt-log.json"
internal/platform/stage1_integration_test.go:33|hookNameFormatWrite|"format-write"
internal/platform/stage1_integration_test.go:34|hookNameSessionBanner|"session-banner"
internal/platform/stage1_integration_test.go:35|hookNameBashGuard|"bash-guard"
internal/platform/stage1_integration_test.go:36|hookNamePromptLog|"prompt-log"
internal/platform/stage1_integration_test.go:37|cmdBannerScript|"./banner.sh"
internal/platform/stage1_integration_test.go:38|cmdGuardScript|"./guard.sh"
internal/platform/stage1_integration_test.go:39|cmdPromptLogScript|"./prompt-log.sh"
internal/ui/output.go:11|Reset|"\033[0m"
internal/ui/output.go:12|Bold|"\033[1m"
internal/ui/output.go:13|Dim|"\033[2m"
internal/ui/output.go:14|Red|"\033[31m"
internal/ui/output.go:15|Green|"\033[32m"
internal/ui/output.go:16|Yellow|"\033[33m"
internal/ui/output.go:17|Blue|"\033[34m"
internal/ui/output.go:18|Cyan|"\033[36m"
internal/ui/output.go:19|White|"\033[37m"
internal/ui/output.go:38|ThreeStringPlaceHolder|"\n%s%s%s\n"
```

## Shell Uppercase Assignments

```text
scripts/verify.sh:8:RED='\033[0;31m'
scripts/verify.sh:9:GREEN='\033[0;32m'
scripts/verify.sh:10:YELLOW='\033[1;33m'
scripts/verify.sh:11:BOLD='\033[1m'
scripts/verify.sh:12:NC='\033[0m'
scripts/install-go.sh:15:RED='\033[0;31m'
scripts/install-go.sh:16:GREEN='\033[0;32m'
scripts/install-go.sh:17:YELLOW='\033[0;33m'
scripts/install-go.sh:18:BLUE='\033[0;34m'
scripts/install-go.sh:19:BOLD='\033[1m'
scripts/install-go.sh:20:NC='\033[0m'
scripts/install-go.sh:22:REPO="dot-agents/dot-agents"
scripts/install-go.sh:23:INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
scripts/install-go.sh:24:VERSION="${DOT_AGENTS_VERSION:-}"
tests/test-claude-configs.sh:10:PASS=0
tests/test-claude-configs.sh:11:FAIL=0
src/lib/utils/paths.sh:138:DOT_AGENTS_WINDOWS_MIRROR="${DOT_AGENTS_WINDOWS_MIRROR:-false}"
src/lib/utils/paths.sh:139:DOT_AGENTS_WINDOWS_HOME="${DOT_AGENTS_WINDOWS_HOME:-}"
src/lib/utils/paths.sh:166:AGENTS_HOME="${AGENTS_HOME:-$HOME/.agents}"
src/lib/utils/paths.sh:167:AGENTS_STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/dot-agents"
src/lib/utils/paths.sh:168:AGENTS_CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/dot-agents"
src/lib/utils/core.sh:13:UTILS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
src/lib/utils/core.sh:14:LIB_DIR="$(dirname "$UTILS_DIR")"
src/lib/utils/core.sh:15:SRC_DIR="$(dirname "$LIB_DIR")"
src/lib/utils/core.sh:29:PLATFORMS_DIR="$LIB_DIR/platforms"
src/lib/utils/core.sh:44:DRY_RUN="${DRY_RUN:-false}"
src/lib/utils/core.sh:45:FORCE="${FORCE:-false}"
src/lib/utils/core.sh:46:VERBOSE="${VERBOSE:-false}"
src/lib/utils/core.sh:47:JSON_OUTPUT="${JSON_OUTPUT:-false}"
src/lib/utils/core.sh:48:YES="${YES:-false}"           # Auto-confirm prompts
src/lib/utils/core.sh:49:INTERACTIVE="${INTERACTIVE:-false}"  # Force interactive mode
src/lib/utils/core.sh:65:DOT_AGENTS_VERSION_DATE="$(date +%Y-%m-%d)"
src/lib/utils/interactive.sh:7:CURRENT_STEP=0
src/lib/utils/interactive.sh:8:TOTAL_STEPS=0
src/lib/platforms/claude-code.sh:125:CLAUDE_USER_AGENTS="${CLAUDE_USER_AGENTS:-$HOME/.claude/agents}"
src/lib/platforms/claude-code.sh:126:CLAUDE_USER_SKILLS="${CLAUDE_USER_SKILLS:-$HOME/.claude/skills}"
scripts/install.sh:16:RED='\033[0;31m'
scripts/install.sh:17:GREEN='\033[0;32m'
scripts/install.sh:18:YELLOW='\033[0;33m'
scripts/install.sh:19:BLUE='\033[0;34m'
scripts/install.sh:20:BOLD='\033[1m'
scripts/install.sh:21:NC='\033[0m' # No Color
scripts/install.sh:24:REPO="dot-agents/dot-agents"
scripts/install.sh:25:INSTALL_DIR="${DOT_AGENTS_INSTALL_DIR:-$HOME/.local/bin}"
scripts/install.sh:26:LIB_DIR="${DOT_AGENTS_LIB_DIR:-$HOME/.local/lib/dot-agents}"
scripts/install.sh:27:SHARE_DIR="${DOT_AGENTS_SHARE_DIR:-$HOME/.local/share/dot-agents}"
scripts/install.sh:28:LOCAL_SRC="${DOT_AGENTS_LOCAL_SRC:-}"  # Set to local src/ directory for testing
src/lib/platforms/opencode.sh:24:OPENCODE_USER_AGENTS="${OPENCODE_USER_AGENTS:-$HOME/.opencode/agent}"
src/lib/platforms/codex.sh:23:CODEX_USER_AGENTS="${CODEX_USER_AGENTS:-$HOME/.codex/agents}"
src/lib/platforms/codex.sh:24:CODEX_USER_SKILLS="${CODEX_USER_SKILLS:-$HOME/.agents/skills}"
src/lib/commands/refresh.sh:49:REFRESH_MARKER_BASENAME=".agents-refresh"
src/lib/utils/platform-registry.sh:6:PLATFORM_IDS=(cursor claude codex opencode copilot)
src/lib/commands/install.sh:16:AGENTSRC_FILE=".agentsrc.json"
```
