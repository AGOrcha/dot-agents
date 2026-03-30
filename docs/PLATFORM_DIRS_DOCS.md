# Platform Resource Locations

This document separates two things that were previously mixed together:

- Official platform behavior, based only on vendor docs
- Current `dot-agents` implementation behavior in this repo

The cross-platform matrix below counts only officially documented project-level read locations when it calls a path the "most common" one.

## Official Platform Locations

Official docs checked on 2026-03-29.

### Cursor

- [Rules](https://cursor.com/docs/rules): project rules live in `.cursor/rules/`. Cursor also documents `AGENTS.md` as a markdown instructions alternative. User rules and team rules exist, but those are settings or dashboard scopes rather than shared repo files.
- [Skills](https://cursor.com/docs/skills): project skills can live in `.cursor/skills/<name>/SKILL.md`. Cursor also documents compatibility discovery for `.agents/skills/<name>/SKILL.md`, `.claude/skills/<name>/SKILL.md`, and `.codex/skills/<name>/SKILL.md`, plus user-level `~/.cursor/skills/`, `~/.claude/skills/`, and `~/.codex/skills/`.
- [Subagents](https://cursor.com/docs/subagents): project subagents can live in `.cursor/agents/`, `.claude/agents/`, or `.codex/agents/`; user-level subagents can live in `~/.cursor/agents/`, `~/.claude/agents/`, or `~/.codex/agents/`. Cursor documents `.cursor/agents/` as the precedence winner when names collide.
- [MCP](https://cursor.com/docs/mcp): project MCP config can live in `.cursor/mcp.json`; user-level config can live in `~/.cursor/mcp.json`.
- [Hooks](https://cursor.com/docs/hooks): hooks live in `.cursor/hooks.json` or `~/.cursor/hooks.json`.

### Claude Code

- [Memory and rules](https://code.claude.com/docs/en/memory): project instructions can live in `CLAUDE.md`, `.claude/CLAUDE.md`, and `.claude/rules/*.md`; user-level instructions can live in `~/.claude/CLAUDE.md` and `~/.claude/rules/`.
- [Skills](https://code.claude.com/docs/en/skills): project skills live in `.claude/skills/<name>/SKILL.md`; user-level skills live in `~/.claude/skills/<name>/SKILL.md`. Claude also documents nested `.claude/skills/` discovery for monorepos.
- [Sub-agents](https://code.claude.com/docs/en/sub-agents): project subagents live in `.claude/agents/`; user-level subagents live in `~/.claude/agents/`.
- [MCP](https://code.claude.com/docs/en/mcp): project MCP config can live in `.mcp.json`; user-level config lives in `~/.claude.json`.
- [Hooks](https://code.claude.com/docs/en/hooks): hooks are configured in `.claude/settings.json`, `.claude/settings.local.json`, and `~/.claude/settings.json`.

### Codex (OpenAI)

- [Instructions](https://developers.openai.com/codex/guides/agents-md/): Codex reads `AGENTS.md` and `AGENTS.override.md` from the repo tree, plus `~/.codex/AGENTS.md` and `~/.codex/AGENTS.override.md` at user scope.
- [Skills](https://developers.openai.com/codex/skills/): project skills live in `.agents/skills/<name>/SKILL.md`; user-level skills live in `~/.agents/skills/<name>/SKILL.md`.
- [Subagents](https://developers.openai.com/codex/subagents): Codex documents subagent definition files under `.codex/agents/*.toml`.
- [Config and MCP](https://developers.openai.com/codex/config-reference/): project config lives in `.codex/config.toml`; user-level config lives in `~/.codex/config.toml`. MCP servers are configured inside that TOML.
- [Hooks](https://developers.openai.com/codex/hooks): hooks live in `.codex/hooks.json` and `~/.codex/hooks.json`.

### OpenCode

- [Rules](https://opencode.ai/docs/rules/): project instructions prefer `AGENTS.md`; OpenCode also documents `CLAUDE.md` compatibility. User-level instructions live in `~/.config/opencode/AGENTS.md`.
- [Skills](https://opencode.ai/docs/skills/): project skills prefer `.opencode/skills/<name>/SKILL.md`; OpenCode also documents `.claude/skills/<name>/SKILL.md` and `.agents/skills/<name>/SKILL.md` compatibility. User-level skills prefer `~/.config/opencode/skills/<name>/SKILL.md`, with `~/.claude/skills/` and `~/.agents/skills/` compatibility.
- [Agents](https://opencode.ai/docs/agents/): local agents are markdown files in `.opencode/agent/`; global agents live in `~/.config/opencode/agent/`.
- [MCP servers](https://opencode.ai/docs/mcp-servers/): project MCP lives in `opencode.json` or `opencode.jsonc`; user-level MCP lives in `~/.config/opencode/opencode.json`.
- [Commands](https://opencode.ai/docs/commands/): project commands live in `.opencode/commands/*.md`; user-level commands live in `~/.config/opencode/commands/*.md`.
- [Custom tools](https://opencode.ai/docs/custom-tools/): project tools live in `.opencode/tools/`; user-level tools live in `~/.config/opencode/tools/`.
- OpenCode does not currently document a separate hooks file in the same style as Cursor, Claude Code, Codex, or GitHub Copilot.

### GitHub Copilot

- [Custom instructions](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-custom-instructions): repository-wide custom instructions live in `.github/copilot-instructions.md`; path-specific instructions live under `.github/instructions/**/*.instructions.md`; local instructions can live in `$HOME/.copilot/copilot-instructions.md`. The same docs also document `AGENTS.md` agent instructions, plus root-level `CLAUDE.md` and `GEMINI.md` alternatives for compatible agents.
- [Agent skills](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-skills) and the [Copilot CLI command reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference): project skills live in `.github/skills/<name>/SKILL.md`. GitHub CLI also documents `.agents/skills/<name>/SKILL.md` and `.claude/skills/<name>/SKILL.md` as project compatibility locations, plus user-level `~/.copilot/skills/<name>/SKILL.md`, `~/.agents/skills/<name>/SKILL.md`, and `~/.claude/skills/<name>/SKILL.md`.
- [Custom agents](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-custom-agents), [Copilot CLI custom agents](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/create-custom-agents-for-cli), and the [Copilot CLI command reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference): repository custom agents live under `.github/agents/`. GitHub CLI also documents `.claude/agents/` compatibility plus user-level `~/.copilot/agents/` and `~/.claude/agents/`.
- [Hooks](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/use-hooks): hook files live in `.github/hooks/*.json`. The same docs note that Copilot CLI loads hooks from the current working directory.
- [Coding-agent MCP](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/extend-coding-agent-with-mcp), [Copilot CLI MCP](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers), and the [Copilot CLI command reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference): coding-agent MCP can be configured in repository settings on GitHub.com. Copilot CLI also documents repository `.github/mcp.json`, workspace `.mcp.json` and `.vscode/mcp.json`, devcontainer `.devcontainer/devcontainer.json`, and user-level `~/.copilot/mcp-config.json`.

## Canonical `~/.agents` Storage Policy

If `dot-agents` should keep exactly one canonical source per resource type inside `~/.agents/`, the wiring policy should be:

1. Store one canonical version in `~/.agents/{resource}/{scope}/...`
2. Wire first to the greatest common compatibility path when one exists
3. If the ecosystem is split, wire to the next most compatible shared path or paths
4. Fall back to platform-native wiring only where no useful compat path exists or formats diverge

This section is a recommended storage model, not a statement that the current code already implements all of it.

| Resource type | Canonical `~/.agents` source | Wiring precedence | Notes |
|---------------|------------------------------|-------------------|-------|
| Instructions and rules | `rules/{scope}/` | 1. Shared `AGENTS.md`-style output where useful 2. Translate to Claude-native rule files and other fragmented instruction targets 3. Platform-native outputs such as `.cursor/rules/` or `.github/copilot-instructions.md` when needed | Instructions are still fragmented. The best shared target is `AGENTS.md`, but it does not cover Claude Code or all Copilot instruction surfaces. |
| Skills | `skills/{scope}/{name}/SKILL.md` | 1. `.claude/skills/` 2. `.agents/skills/` 3. Native `.github/skills/`, `.cursor/skills/`, or `.opencode/skills/` only where needed | Skills are the clearest split-GCD case. `.claude/skills/` and `.agents/skills/` each have 4-platform coverage, so the practical policy is to wire both from one canonical source. |
| Agents and subagents | `agents/{scope}/{name}/AGENT.md` | 1. `.claude/agents/` 2. Translate to `.github/agents/*`, `.codex/agents/*.toml`, and `.opencode/agent/*.md` 3. Use `.cursor/agents/` only when native Cursor precedence matters more than compat reuse | There is no universal shared format. A single canonical source still works, but it requires format-specific emitters. |
| MCP and config | `mcp/{scope}/mcp.json` | 1. No true compat winner 2. Translate outward to `.cursor/mcp.json`, `.mcp.json`, `.github/mcp.json` or `.vscode/mcp.json`, `.codex/config.toml`, and `opencode.json` | The storage can be single-source, but the output formats are platform-specific. |
| Hooks | `hooks/{scope}/hooks.json` or a canonical hook bundle under `hooks/{scope}/` | 1. No true compat winner 2. Translate to `.cursor/hooks.json`, `.codex/hooks.json`, `.github/hooks/*.json`, and Claude settings-backed hooks | Hooks need an internal normalized schema if they are going to stay truly single-source. See `docs/CANONICAL_HOOKS_DESIGN.md` for the proposed `HOOK.yaml` bundle model and shared emitter contract. |
| Commands | `commands/{scope}/*.md` | 1. No compat winner 2. Translate to `.cursor/commands/`, `.claude/commands/`, and `.opencode/commands/` | Claude custom commands are legacy, but the resource type is still useful. |
| Output styles | `output-styles/{scope}/*.md` | 1. No compat winner 2. Wire to `.claude/output-styles/` | Claude-specific today. |
| Ignore files | `ignore/{scope}/cursorignore` and `ignore/{scope}/cursorindexingignore` | 1. No compat winner 2. Wire to Cursor root ignore files | Cursor-specific today. |
| Modes | `modes/{scope}/*.md` | 1. No compat winner 2. Wire to `.opencode/modes/` | OpenCode-specific today. |
| Plugins | `plugins/{scope}/` | 1. No compat winner 2. Wire to `.opencode/plugins/` | OpenCode-specific today. |
| Themes | `themes/{scope}/*.json` | 1. No compat winner 2. Wire to `.opencode/themes/` | OpenCode-specific today. |
| Prompt files | `prompts/{scope}/*.prompt.md` | 1. No compat winner 2. Wire to `.github/prompts/` | GitHub Copilot-specific today. |

## Other Documented Resources Worth Managing

These are additional official resources that `dot-agents` could plausibly manage, but they are either platform-specific or not strong enough candidates for the main cross-platform matrix.

| Platform | Additional resource | Official location(s) | Why it matters |
|----------|---------------------|----------------------|----------------|
| Cursor | Ignore files | [Ignore files](https://docs.cursor.com/en/context/ignore-files): `.cursorignore`, `.cursorindexingignore` | Strong candidate. Cursor explicitly uses these to control agent/indexing visibility, and this repo already wires `.cursorignore`. |
| Cursor | Custom commands | [Commands](https://docs.cursor.com/en/agent/chat/commands): `.cursor/commands/*.md` | Good candidate for reusable `/command` workflows. Not currently wired in this repo. |
| Claude Code | Legacy custom commands | [Skills / slash commands](https://code.claude.com/docs/en/slash-commands): `.claude/commands/*.md` still works, though skills are preferred | Useful if this repo wants explicit `/command` files or backward compatibility with older Claude setups. |
| Claude Code | Output styles | [Output styles](https://code.claude.com/docs/en/output-styles): `.claude/output-styles/*.md`, `~/.claude/output-styles/` | Strong candidate for sharable response modes, teaching styles, or non-coding personas. |
| Claude Code | Status line scripts | [Customize status line](https://code.claude.com/docs/en/statusline): script path configured through `statusLine` in settings; docs examples use `~/.claude/statusline.sh` | Possible, but secondary. This is settings-backed rather than a dedicated resource directory. |
| Codex | No extra standalone repo resource found | Inference from the current official Codex docs reviewed above | I did not find another dedicated repo resource directory beyond AGENTS, skills, subagents, config, and hooks. Current extra behavior appears to live inside `.codex/config.toml` rather than separate resource folders. |
| OpenCode | Modes | [Modes](https://opencode.ai/docs/modes/): `.opencode/modes/*.md`, `~/.config/opencode/modes/*.md` | Strong candidate for sharable plan/review/build presets. |
| OpenCode | Plugins | [Plugins](https://opencode.ai/docs/plugins/): `.opencode/plugins/`, `~/.config/opencode/plugins/` | Strong extension point for event hooks and custom runtime behavior. |
| OpenCode | Themes | [Themes](https://opencode.ai/docs/themes/): `.opencode/themes/*.json`, `~/.config/opencode/themes/*.json` | Worth managing if this repo wants shared terminal theming or branded presets. |
| GitHub Copilot | Prompt files | [Customization cheat sheet](https://docs.github.com/en/copilot/reference/customization-cheat-sheet) and [Prompt files](https://docs.github.com/en/copilot/tutorials/customization-library/prompt-files): `.github/prompts/*.prompt.md` | Best additional Copilot repo resource. Reusable prompt templates fit this project well. |

## Cross-Platform Common-Location Matrix

This matrix compares official project-level locations only.

| Resource | Official native project path(s) | Official compat path(s) | Most common documented project path | Recommendation for `dot-agents` |
|----------|---------------------------------|--------------------------|-------------------------------------|----------------------------------|
| Instructions and rules | Cursor: `.cursor/rules/`; Claude Code: `CLAUDE.md`, `.claude/CLAUDE.md`, `.claude/rules/*.md`; Codex: `AGENTS.md`, `AGENTS.override.md`; OpenCode: `AGENTS.md`; GitHub Copilot: `.github/copilot-instructions.md`, `.github/instructions/**/*.instructions.md`, `AGENTS.md` | Cursor also accepts `AGENTS.md`; OpenCode also accepts `CLAUDE.md`; GitHub Copilot also documents `CLAUDE.md` and `GEMINI.md` for agent instructions | No native-path consensus. `AGENTS.md` is the strongest shared instruction file at 4 platforms: Cursor, Codex, OpenCode, and GitHub Copilot agent instructions. | Keep per-platform rule linking. `AGENTS.md` is a useful bridge, but it is not a universal replacement for Claude Code rules or Copilot repo-wide custom instructions. |
| Skills | Cursor: `.cursor/skills/<name>/SKILL.md`; Claude Code: `.claude/skills/<name>/SKILL.md`; Codex: `.agents/skills/<name>/SKILL.md`; OpenCode: `.opencode/skills/<name>/SKILL.md`; GitHub Copilot: `.github/skills/<name>/SKILL.md` | Cursor also accepts `.agents/skills/`, `.claude/skills/`, `.codex/skills/`; OpenCode also accepts `.claude/skills/` and `.agents/skills/`; GitHub Copilot also accepts `.agents/skills/` and `.claude/skills/` | No single winner. `.claude/skills/<name>/SKILL.md` and `.agents/skills/<name>/SKILL.md` each have 4-platform coverage. | If maximum shared coverage is the goal, keep both `.claude/skills/` and `.agents/skills/` in sync. A single-directory choice would force a tradeoff between Claude/Copilot-native gravity and Codex-native gravity. |
| Agents and subagents | Cursor: `.cursor/agents/`; Claude Code: `.claude/agents/`; Codex: `.codex/agents/*.toml`; OpenCode: `.opencode/agent/*.md`; GitHub Copilot: `.github/agents/*` | Cursor also accepts `.claude/agents/` and `.codex/agents/`; GitHub Copilot CLI also accepts `.claude/agents/` | No consensus. `.claude/agents/` is the only repeated compat path, but only across part of the ecosystem and not with a shared file format. | Keep per-platform agent outputs. Do not normalize this category to one directory. |
| MCP and config | Cursor: `.cursor/mcp.json`; Claude Code: `.mcp.json`; Codex: `.codex/config.toml`; OpenCode: `opencode.json` or `opencode.jsonc`; GitHub Copilot: `.github/mcp.json`, `.mcp.json`, `.vscode/mcp.json`, `.devcontainer/devcontainer.json`, plus coding-agent repository settings on GitHub.com | No meaningful cross-platform compat path is documented | No consensus | Treat MCP as platform-specific config, not a shared repo resource. GitHub Copilot is the broadest here, but its documented locations still do not align with the other tools. |
| Hooks | Cursor: `.cursor/hooks.json`; Claude Code: `.claude/settings.json` or `.claude/settings.local.json`; Codex: `.codex/hooks.json`; OpenCode: no dedicated hooks file documented; GitHub Copilot: `.github/hooks/*.json` | No meaningful cross-platform compat path is documented | No consensus | Keep platform-specific hook wiring. |
| Commands | OpenCode only: `.opencode/commands/*.md` | None | OpenCode only | No cross-platform action needed. |
| Custom tools | OpenCode only: `.opencode/tools/` | None | OpenCode only | No cross-platform action needed. |

## `dot-agents` Implementation Audit

This section is about the current repo implementation, not upstream platform behavior.

### Current Path Strategy by Platform

| Platform | Current project links in this repo | Notable difference from official docs |
|----------|------------------------------------|---------------------------------------|
| Cursor | `.cursor/rules/`, `.cursor/settings.json`, `.cursor/mcp.json`, `.cursor/hooks.json`, `.cursorignore`, `.claude/agents/` | Cursor-native agents would be `.cursor/agents/`, but both Go and bash implementations currently target `.claude/agents/` for compatibility reuse. The repo already manages `.cursorignore`, but not `.cursorindexingignore` or `.cursor/commands/`. |
| Claude Code | `.claude/rules/`, `.claude/settings.local.json`, `.mcp.json`, `.claude/agents/`, `.claude/skills/`, `.agents/skills/` | Official Claude skills docs only mention `.claude/skills/`; this repo also mirrors project skills into `.agents/skills/` for shared-tool compatibility. |
| Codex | `AGENTS.md`, `.codex/config.toml`, `.claude/agents/`, `.agents/skills/` | Codex-native subagents are documented under `.codex/agents/*.toml`, but both Go and bash implementations currently place project agents in `.claude/agents/`. |
| OpenCode | `opencode.json`, `.opencode/agent/`, `.agents/skills/` | OpenCode-native skills are documented under `.opencode/skills/`, but current Go and bash implementations rely on the `.agents/skills/` compatibility path instead. |
| GitHub Copilot | `.github/copilot-instructions.md`, `.github/agents/*.agent.md`, `.agents/skills/`, `.vscode/mcp.json`, `.claude/settings.local.json`, and Go-only `.github/hooks/*.json` | `.agents/skills/` and `.vscode/mcp.json` are officially documented Copilot CLI locations, but this repo still skips other official Copilot locations such as `.github/skills/`, `.claude/skills/`, `.github/mcp.json`, and `.mcp.json`. Bash also still lacks `.github/hooks/*.json` output. |

### Hook Wiring Audit

Validated from the current Go and bash implementations:

| Platform | Official hook location | Go implementation | Bash implementation | Notes |
|----------|------------------------|-------------------|---------------------|-------|
| Claude Code | `.claude/settings*.json` | Yes | Yes | Both wire Claude-compatible hook settings, but the management commands still source from `~/.agents/settings/*/claude-code.json` or `~/.agents/hooks/*/claude-code.json`, not from native Claude files. |
| Cursor | `.cursor/hooks.json` | Yes | No | Go wires `~/.agents/hooks/{scope}/cursor.json` to project and user `hooks.json`; bash has no Cursor hook creation path. |
| Codex | `.codex/hooks.json` | No | No | Both implementations link `AGENTS.md`, `.codex/config.toml`, skills, and agents, but neither creates `.codex/hooks.json`. |
| GitHub Copilot | `.github/hooks/*.json` and CLI current-working-directory hooks | Partial | Partial | Go links project `.github/hooks/*.json` and also wires Claude-compatible settings. Bash only wires Claude-compatible settings and does not create `.github/hooks/*.json`. |
| OpenCode | No dedicated hook file documented | No | No | No OpenCode-specific hook handling is implemented here. |
