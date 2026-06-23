---
title: Platform Resource Locations
description: Where each AI coding platform reads its config, and how dot-agents maps to those paths.
sidebar:
  order: 1
---

# Platform Resource Locations

This document separates two things that were previously mixed together:

- Official platform behavior, based only on vendor docs
- Current `dot-agents` implementation behavior in this repo

The cross-platform matrix below counts only officially documented project-level read locations when it calls a path the "most common" one.

## Official Platform Locations

Official docs were checked on 2026-03-29.

Session storage section added 2026-05-10 from local file mining and vendor docs (all five platforms).

Spot re-verification on 2026-04-11:

- Codex, Claude Code, OpenCode, and GitHub Copilot locations below were re-checked against current vendor docs and remain directionally correct.
- Cursor compatibility locations still need manual follow-up. The direct docs fetch/search path was inconclusive on 2026-04-11, so Cursor compatibility claims below remain based on the earlier manual doc pass rather than a fresh automated verification.

Partial refresh on 2026-05-25 (hooks topic only — Claude Code, Codex, Cursor, GitHub Copilot):

- Hook **event sets** updated across all four platforms. Codex now documents `SubagentStart` and `SubagentStop` (PascalCase). Cursor confirms `subagentStop` as a documented camelCase event. GitHub Copilot's stop-equivalent is named **`agentStop`** (camelCase, distinct from Cursor's `stop`), and Copilot now exposes `subagentStart` / `subagentStop`. Claude Code's event surface has grown substantially (Setup, UserPromptExpansion, PostToolBatch, PermissionDenied, PostCompact, StopFailure, TaskCreated, TaskCompleted, TeammateIdle, InstructionsLoaded, ConfigChange, CwdChanged, FileChanged, WorktreeCreate, WorktreeRemove, Elicitation, ElicitationResult).
- Hook **file locations** unchanged for Claude, Codex, Cursor. Copilot adds `.github/copilot/settings.json` / `.github/copilot/settings.local.json` as a repo-scope settings target and `~/.copilot/hooks/` (or `$COPILOT_HOME/hooks/`) as a user-scope hook directory in addition to `~/.copilot/hooks/*.json`.
- OpenCode and other (non-hooks) topics were NOT re-checked this pass — leave per-topic checked-on dates untouched.

Release-docs re-verification 2026-06-23 (all five platforms): locations confirmed current; added Claude `MessageDisplay` to the documented event surface; Cursor compatibility skill/subagent paths are now directly vendor-verified.

### Cursor

- [Rules](https://cursor.com/docs/rules): project rules live in `.cursor/rules/`. Cursor also documents `AGENTS.md` as a markdown instructions alternative. User rules and team rules exist, but those are settings or dashboard scopes rather than shared repo files.
- [Skills](https://cursor.com/docs/skills): project skills can live in `.cursor/skills/<name>/SKILL.md`. Cursor compatibility discovery for `.agents/skills/<name>/SKILL.md`, `.claude/skills/<name>/SKILL.md`, and `.codex/skills/<name>/SKILL.md` is kept here from the 2026-03-29 manual doc pass and should be manually re-verified.
- [Subagents](https://cursor.com/docs/subagents): project subagents can live in `.cursor/agents/`, `.claude/agents/`, or `.codex/agents/`; user-level subagents can live in `~/.cursor/agents/`, `~/.claude/agents/`, or `~/.codex/agents/`. These compatibility notes are likewise carried forward from the earlier manual verification and should be manually re-checked.
- [MCP](https://cursor.com/docs/mcp): project MCP config can live in `.cursor/mcp.json`; user-level config can live in `~/.cursor/mcp.json`.
- [Hooks](https://cursor.com/docs/hooks): hooks live in `.cursor/hooks.json` or `~/.cursor/hooks.json`. The documented agent-hook event surface (camelCase) includes `sessionStart`, `sessionEnd`, `preToolUse`, `postToolUse`, `postToolUseFailure`, `subagentStart`, `subagentStop`, `beforeShellExecution`, `afterShellExecution`, `beforeMCPExecution`, `afterMCPExecution`, `beforeReadFile`, `afterFileEdit`, `beforeSubmitPrompt`, `preCompact`, `stop`, `afterAgentResponse`, and `afterAgentThought`, plus tab-completion events (`beforeTabFileRead`, `afterTabFileEdit`) and app-lifecycle `workspaceOpen`. As of early 2026 hook execution moved to an in-process runner (10–20× faster than the previous shell-spawn model).
- [Plugins](https://cursor.com/docs/plugins.md) and the [Cursor Marketplace](https://cursor.com/marketplace/): Cursor now has a first-party plugin system. Plugins bundle rules, skills, agents, commands, MCP servers, and hooks. A plugin package uses a `.cursor-plugin/plugin.json` manifest; multi-plugin repositories can add `.cursor-plugin/marketplace.json`. Cursor also documents local testing from `~/.cursor/plugins/local/<plugin-name>`.

### Claude Code

- [Memory and rules](https://code.claude.com/docs/en/memory): project instructions can live in `CLAUDE.md`, `.claude/CLAUDE.md`, and `.claude/rules/*.md`; user-level instructions can live in `~/.claude/CLAUDE.md` and `~/.claude/rules/`.
- [Skills](https://code.claude.com/docs/en/skills): project skills live in `.claude/skills/<name>/SKILL.md`; user-level skills live in `~/.claude/skills/<name>/SKILL.md`. Claude also documents nested `.claude/skills/` discovery for monorepos.
- [Sub-agents](https://code.claude.com/docs/en/sub-agents): project subagents live in `.claude/agents/`; user-level subagents live in `~/.claude/agents/`.
- [MCP](https://code.claude.com/docs/en/mcp): project MCP config can live in `.mcp.json`; user-level config lives in `~/.claude.json`.
- [Hooks](https://code.claude.com/docs/en/hooks): hooks are configured in `.claude/settings.json`, `.claude/settings.local.json`, and `~/.claude/settings.json`. Plugins ship hooks via `hooks/hooks.json` inside the plugin package; skills and agents can also declare hooks in frontmatter. The documented event surface (PascalCase) includes `SessionStart`, `SessionEnd`, `Setup`, `UserPromptSubmit`, `UserPromptExpansion`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PostToolBatch`, `PermissionRequest`, `PermissionDenied`, `Notification`, `MessageDisplay`, `PreCompact`, `PostCompact`, `Stop`, `StopFailure`, `SubagentStart`, `SubagentStop`, `TaskCreated`, `TaskCompleted`, `TeammateIdle`, `InstructionsLoaded`, `ConfigChange`, `CwdChanged`, `FileChanged`, `WorktreeCreate`, `WorktreeRemove`, `Elicitation`, and `ElicitationResult`. Stop and SubagentStop accept JSON `{"decision":"block","reason":"..."}` on stdout (or exit 2 + stderr) to refuse stop.
- [Plugins](https://code.claude.com/docs/en/plugins.md): Claude Code now has a first-party plugin system. Plugins can bundle custom commands, agents, hooks, Skills, and MCP servers. A plugin package uses a `.claude-plugin/plugin.json` manifest and can include component directories such as `commands/`, `agents/`, `skills/`, and hooks or MCP configuration.
- [Plugin marketplaces](https://code.claude.com/docs/en/plugin-marketplaces.md): Claude Code supports plugin marketplaces. Marketplaces are defined by `.claude-plugin/marketplace.json`, can be added from GitHub, arbitrary git URLs, local paths, direct JSON URLs, and can be configured in `.claude/settings.json` through `extraKnownMarketplaces` and `enabledPlugins` for team rollout.


### Codex (OpenAI)

- [Instructions](https://developers.openai.com/codex/guides/agents-md/): Codex reads `AGENTS.md` and `AGENTS.override.md` from the repo tree, plus `~/.codex/AGENTS.md` and `~/.codex/AGENTS.override.md` at user scope.
- [Skills](https://developers.openai.com/codex/skills/): project skills live in `.agents/skills/<name>/SKILL.md`; user-level skills live in `~/.agents/skills/<name>/SKILL.md`.
- [Subagents](https://developers.openai.com/codex/subagents): Codex documents subagent definition files under `.codex/agents/*.toml`.
- [Config and MCP](https://developers.openai.com/codex/config-reference/): project config lives in `.codex/config.toml`; user-level config lives in `~/.codex/config.toml`. MCP servers are configured inside that TOML.
- [Hooks](https://developers.openai.com/codex/hooks): hooks live in `.codex/hooks.json` and `~/.codex/hooks.json`. Codex also reads layered config from `<repo>/.codex/config.toml` and `~/.codex/config.toml`; higher-precedence layers don't replace lower-precedence hooks — Codex merges them and emits startup warnings when multiple representations exist in one layer. The documented event surface (PascalCase, interoperable with Claude Code naming) is `SessionStart`, `SubagentStart`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `PreCompact`, `PostCompact`, `UserPromptSubmit`, `SubagentStop`, and `Stop`. Stop and SubagentStop require JSON on stdout (or exit 2 + stderr); plain-text output is invalid for these events.
- [Plugins](https://developers.openai.com/codex/plugins.md) and [Build plugins](https://developers.openai.com/codex/plugins/build.md): Codex has a first-party plugin system surfaced in the app and CLI (`/plugins`). Plugin packages use a required `.codex-plugin/plugin.json` manifest and can also include `skills/`, `.app.json`, `.mcp.json`, and `assets/`. For local development and curated catalogs, Codex documents repo marketplaces at `$REPO_ROOT/.agents/plugins/marketplace.json` and personal marketplaces at `~/.agents/plugins/marketplace.json`; the docs use `$REPO_ROOT/plugins/` and `~/.codex/plugins/` as examples, but `source.path` resolves relative to the marketplace root rather than to a fixed plugin directory.

### OpenCode

- [Rules](https://opencode.ai/docs/rules/): project instructions prefer `AGENTS.md`; OpenCode also documents `CLAUDE.md` compatibility. User-level instructions live in `~/.config/opencode/AGENTS.md`.
- [Skills](https://opencode.ai/docs/skills/): project skills prefer `.opencode/skills/<name>/SKILL.md`; OpenCode also documents `.claude/skills/<name>/SKILL.md` and `.agents/skills/<name>/SKILL.md` compatibility. User-level skills prefer `~/.config/opencode/skills/<name>/SKILL.md`, with `~/.claude/skills/` and `~/.agents/skills/` compatibility.
- [Agents](https://opencode.ai/docs/agents/): local agents are markdown files in `.opencode/agent/`; global agents live in `~/.config/opencode/agent/`.
- [MCP servers](https://opencode.ai/docs/mcp-servers/): project MCP lives in `opencode.json` or `opencode.jsonc`; user-level MCP lives in `~/.config/opencode/opencode.json`.
- [Commands](https://opencode.ai/docs/commands/): project commands live in `.opencode/commands/*.md`; user-level commands live in `~/.config/opencode/commands/*.md`.
- [Custom tools](https://opencode.ai/docs/custom-tools/): project tools live in `.opencode/tools/`; user-level tools live in `~/.config/opencode/tools/`.
- [Plugin dependencies](https://opencode.ai/docs/plugins/): local plugins and custom tools can depend on npm packages through `.opencode/package.json` or `~/.config/opencode/package.json`. The docs state that OpenCode runs `bun install` at startup for these config-root manifests.
- OpenCode does not currently document a separate hooks file in the same style as Cursor, Claude Code, Codex, or GitHub Copilot.

### GitHub Copilot

- [Custom instructions](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-custom-instructions): repository-wide custom instructions live in `.github/copilot-instructions.md`; path-specific instructions live under `.github/instructions/**/*.instructions.md`; local instructions can live in `$HOME/.copilot/copilot-instructions.md`. The same docs also document `AGENTS.md` agent instructions, plus root-level `CLAUDE.md` and `GEMINI.md` alternatives for compatible agents.
- [Agent skills](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-skills) and the [Copilot CLI command reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference): project skills live in `.github/skills/<name>/SKILL.md`. GitHub CLI also documents `.agents/skills/<name>/SKILL.md` and `.claude/skills/<name>/SKILL.md` as project compatibility locations, plus user-level `~/.copilot/skills/<name>/SKILL.md`, `~/.agents/skills/<name>/SKILL.md`, and `~/.claude/skills/<name>/SKILL.md`.
- [Custom agents](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-custom-agents), [Copilot CLI custom agents](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/create-custom-agents-for-cli), and the [Copilot CLI command reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference): repository custom agents live under `.github/agents/`. GitHub CLI also documents `.claude/agents/` compatibility plus user-level `~/.copilot/agents/` and `~/.claude/agents/`.
- [Hooks](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/use-hooks) and [Hooks configuration reference](https://docs.github.com/en/copilot/reference/hooks-configuration): hook files live in `.github/hooks/*.json` (project) and `~/.copilot/hooks/` (user; or `$COPILOT_HOME/hooks/` when set). Repo settings can also configure hooks via `.github/copilot/settings.json` and `.github/copilot/settings.local.json`; cross-tool settings via `.claude/settings.json` / `.claude/settings.local.json` are also documented. The Copilot CLI loads hooks from the current working directory. The documented event surface (camelCase) is `sessionStart`, `sessionEnd`, `userPromptSubmitted`, `preToolUse`, `postToolUse`, `postToolUseFailure`, `preCompact`, **`agentStop`** (NOT `stop` — the main-agent stop equivalent), `errorOccurred`, `notification`, `permissionRequest`, `subagentStart`, and `subagentStop`. The `agentStop` naming is a cross-platform footgun for any mapper that assumes `stop` is the universal name.
- [Coding-agent MCP](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/extend-coding-agent-with-mcp), [Copilot CLI MCP](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers), and the [Copilot CLI command reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference): coding-agent MCP can be configured in repository settings on GitHub.com. Copilot CLI also documents repository `.github/mcp.json`, workspace `.mcp.json` and `.vscode/mcp.json`, devcontainer `.devcontainer/devcontainer.json`, and user-level `~/.copilot/mcp-config.json`.
- [Copilot CLI plugins overview](https://docs.github.com/en/copilot/concepts/agents/copilot-cli/about-cli-plugins), [find/install plugins](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/plugins-finding-installing), [create plugins](https://docs.github.com/en/enterprise-cloud@latest/copilot/how-tos/copilot-cli/customize-copilot/plugins-creating), and [create marketplaces](https://docs.github.com/en/enterprise-cloud@latest/copilot/how-tos/copilot-cli/customize-copilot/plugins-marketplace): Copilot CLI now has installable plugins and plugin marketplaces. Plugins can be installed from marketplaces, repositories, or local paths; installed copies live under `~/.copilot/state/installed-plugins/`. A plugin package requires a root `plugin.json` manifest and can contain `agents/`, `skills/`, `hooks.json`, and `.mcp.json`. GitHub also documents direct repository installs when `plugin.json` is at the repository root, in `.github/plugin/`, or in `.claude-plugin/`. Marketplaces use `marketplace.json` as the required file and can live on GitHub, other git hosts, or local/shared filesystems.

## Platform Session Storage

This section covers where each platform stores active session data and whether token usage counts are accessible locally. Checked 2026-05-10 against local file system and vendor docs.

### Claude Code

- **Session files:** `~/.claude/projects/<project-hash>/<session-id>.jsonl`
  - Project hash: `strings.ReplaceAll(absoluteProjectPath, "/", "-")` (e.g., `/Users/x/proj` → `-Users-x-proj`)
  - Session ID: `CLAUDE_CODE_SESSION_ID` env var; format is a UUID
- **Token data:** Yes — per-message in `assistant` type entries under `message.usage`:
  - `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`
- **Model:** `message.model` field on each `assistant` entry
- **Other metadata:** `gitBranch` field on each JSONL entry; usable for branch-scoped session lookup
- **Pre-aggregated stats:** `~/.claude/stats-cache.json` — daily activity, token totals by model, session count (version 3 JSON, maintained by Claude Code)
- **Session ID env var:** `CLAUDE_CODE_SESSION_ID` (confirmed)
- **Entrypoint env var:** `CLAUDE_CODE_ENTRYPOINT` (confirmed: `cli`, `ide`, `web`)

### Codex (OpenAI)

- **Session files:** `~/.codex/sessions/YYYY/MM/DD/rollout-*-<session-id>.jsonl`
  - Session ID format: UUID suffix in the filename after the last `-`
  - Session index: `~/.codex/session_index.jsonl` — one line per session with `id`, `thread_name`, `updated_at`
- **Token data:** Yes — `event_msg` entries where `payload.type == "token_count"`:
  - `payload.info.last_token_usage.{input_tokens, output_tokens, cached_input_tokens, reasoning_output_tokens}`
  - Use `last_token_usage` (per-turn delta), not `total_token_usage` (cumulative)
  - First `token_count` event in a session sometimes has null `info` — guard required
- **Model:** `response_item` entries with `payload.type == "response"` carry `payload.model`
- **Session ID env var:** Not confirmed (`CODEX_SESSION_ID` is a placeholder — verify before using)

### Cursor

- **Session files (native):** `~/.cursor/projects/<slug>/agent-transcripts/<session-uuid>/<session-uuid>.jsonl`
  - Slug: project path with `/` replaced by `-` (e.g., `Users-nikashp-Documents-dot-agents`)
  - Contains conversation turns (role/message pairs) — **no token counts**
- **CLI agent mode:** `cursor agent` (subcommand, not `cursor --help`). Flags: `--print`, `--output-format stream-json`, `--resume <session-id>`, `--worktree`. Each completed agent run writes a result file at:
  - `~/.cursor/projects/<slug>/agent-tools/<uuid>.txt` — stream-json lines, final line is `type=result` with token usage
  - Schema 1 (camelCase): `inputTokens`, `outputTokens`, `cacheReadTokens`, `cacheWriteTokens`
  - `session_id` field present in result lines — use for cross-source deduplication
- **Other native files:** `~/.cursor/projects/<slug>/worker.log` (indexing events only, no token data); `~/.cursor/chats/<workspace-hash>/<session-uuid>/store.db` (SQLite blobs, no token data)
- **Token data:** Available for `cursor agent` CLI runs via `agent-tools/*.txt`. Standard IDE chat sessions write no token data to disk. Note: `.ralph-loop-streams/run-*/` (project-local, created by `bin/tests/ralph-worker`) is general ralph-worker output for any agent binary — not Cursor-specific; each platform scanner reads only its own native files.
- **Session ID env var:** Not confirmed (no documented env var; `--resume` flag uses session IDs but no shell env var injection)

### OpenCode

- **Session database:** `~/.local/share/opencode/opencode.db` (XDG; same path on macOS — does not use `~/Library/Application Support`)
  - Pre-v1.2 (legacy): `~/.local/share/opencode/storage/<project-slug>/session/` JSON files — migrated to SQLite at v1.2.0
- **Token data:** Yes — `part` table, rows where `type = 'step-finish'`, token data in `data` JSON column:
  - `$.tokens.input`, `$.tokens.output`, `$.tokens.cache.read`, `$.tokens.cache.write` (floats)
  - Join to `message` table via `part.message_id = message.id` for `message.created_at` (Unix ms) time-windowing
- **Session table:** `session` table has `id`, `title`, `message_count`, `prompt_tokens`, `completion_tokens`, `cost`, `created_at` (Unix ms), `updated_at` (Unix ms)
- **Model:** `message.model` column — format is `provider/model-name` (e.g., `anthropic/claude-sonnet-4-5`, `openai/gpt-4o`)
- **Session ID env var:** None — OpenCode does not inject a session ID into the shell environment. Session IDs are accessible via the plugin API or by querying the SQLite DB.

### GitHub Copilot

- **Session files (CLI):** `~/.copilot/session-state/<session-id>/events.jsonl`
  - Session metadata: `~/.copilot/session-state/<session-id>/workspace.yaml` (`cwd`, `created_at`, `updated_at`)
  - Session index DB: `~/.copilot/session-store.db` (SQLite — lightweight index only, no token rows)
  - Process logs: `~/.copilot/logs/process-*.log` (connection/auth events, no token data)
- **Token data:** Partial — per-turn counts are ephemeral (memory only, exposed via `/usage` slash command but not written to disk). Session-level aggregate token totals are written to `events.jsonl` on `session.shutdown`:
  ```
  modelMetrics.<model-name>.usage.{inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens}
  ```
  These are camelCase, session-level aggregates per model, not per-message.
- **VS Code extension session files:** `~/Library/Application Support/Code/User/workspaceStorage/<hash>/GitHub.copilot-chat/chatSessions/<uuid>.json` — conversation history only, **no token counts**
- **Session ID env var:** None — no env var is published by the CLI. Session IDs are directory names under `~/.copilot/session-state/`. Use `--resume` or `--continue` flags to reference sessions.
- **Session-level token granularity note:** Only the `session.shutdown` aggregate is persisted. If per-turn granularity is needed, the Copilot SDK streaming events API or the `CompactionProcessor` entries in process logs can be used as a fallback (inference, not official).

### Cross-Platform Session Storage Matrix

| Platform | Session file location | Token data available | Token granularity | Session ID env var |
|----------|-----------------------|---------------------|-------------------|--------------------|
| Claude Code | `~/.claude/projects/<hash>/<session-id>.jsonl` | Yes | Per-message | `CLAUDE_CODE_SESSION_ID` (confirmed) |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-*-<id>.jsonl` | Yes | Per-turn delta (`last_token_usage`) | `CODEX_SESSION_ID` (unconfirmed) |
| Cursor | `~/.cursor/projects/<slug>/agent-tools/*.txt` (`cursor agent` CLI) | Yes (`cursor agent` only; IDE chat = none) | Per agent run (`type=result` line) | None documented |
| OpenCode | `~/.local/share/opencode/opencode.db` (`part` table) | Yes | Per LLM turn (`step-finish` parts) | None |
| GitHub Copilot | `~/.copilot/session-state/<id>/events.jsonl` | Partial (session aggregate only) | Session-level (`session.shutdown`) | None |

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
| Hooks | `hooks/{scope}/hooks.json` or a canonical hook bundle under `hooks/{scope}/` | 1. No true compat winner 2. Translate to `.cursor/hooks.json`, `.codex/hooks.json`, `.github/hooks/*.json`, and Claude settings-backed hooks | Hooks need an internal normalized schema if they are going to stay truly single-source. See [`docs/HOOKS.md`](HOOKS.md) for the user-facing hooks model; the internal design record is archived under `.agents/history/platform-dir-unification/canonical-hooks-design.md`. |
| Commands | `commands/{scope}/*.md` | 1. No compat winner 2. Translate to `.cursor/commands/`, `.claude/commands/`, and `.opencode/commands/` | Claude custom commands are legacy, but the resource type is still useful. |
| Output styles | `output-styles/{scope}/*.md` | 1. No compat winner 2. Wire to `.claude/output-styles/` | Claude-specific today. |
| Ignore files | `ignore/{scope}/cursorignore` and `ignore/{scope}/cursorindexingignore` | 1. No compat winner 2. Wire to Cursor root ignore files | Cursor-specific today. |
| Modes | `modes/{scope}/*.md` | 1. No compat winner 2. Wire to `.opencode/modes/` | OpenCode-specific today. |
| Plugins | `plugins/{scope}/{name}/` | 1. Shared planner/executor owns the canonical bundle 2. OpenCode emitter wired (`.opencode/plugins/`); Cursor, Claude, Codex, and Copilot emitters pending — all five platforms have first-class plugin support | Canonical bundle marker is `PLUGIN.yaml`; sibling files stay bundle-local. See `docs/PLUGIN_CONTRACT.md` for emitter status and per-platform native paths. |
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

The legacy `src/` bash implementation has been retired; the Go CLI (`da`) is
now the sole implementation. Audit notes below describe the Go implementation.

### Current Path Strategy by Platform

| Platform | Current project links in this repo | Notable difference from official docs |
|----------|------------------------------------|---------------------------------------|
| Cursor | `.cursor/rules/`, `.cursor/settings.json`, `.cursor/mcp.json`, `.cursor/hooks.json`, `.cursorignore`, `.claude/agents/` | Cursor-native agents would be `.cursor/agents/`, but the implementation currently targets `.claude/agents/` for compatibility reuse. The repo already manages `.cursorignore`, but not `.cursorindexingignore` or `.cursor/commands/`. |
| Claude Code | `.claude/rules/`, `.claude/settings.local.json`, `.mcp.json`, `.claude/agents/`, `.claude/skills/`, `.agents/skills/` | Official Claude skills docs only mention `.claude/skills/`; this repo also mirrors project skills into `.agents/skills/` for shared-tool compatibility. |
| Codex | `AGENTS.md`, `.codex/config.toml`, `~/.codex/agents/*.toml` (user scope), repo `.codex/agents/*.toml`, `~/.codex/hooks.json` + repo `.codex/hooks.json`, `.agents/skills/` | Codex-native subagents are rendered as `.codex/agents/*.toml` (not `.claude/agents/`). Repo-level `.codex/agents/*.toml` and the Claude shared-skills projection ARE produced by `da refresh` / `install` / `add` (they invoke the shared-target projection before per-platform linking). Known narrow gap: the import-relink path (`relinkImportedProjects`) and the `doctor` broken-link repair path call `CreateLinks` without first running the projection — tracked in `.agents/proposals/codex-hooks-agents-linking-gap.md` and the `shared-target-projection-wiring` plan. |
| OpenCode | `opencode.json`, `.opencode/agent/`, `.agents/skills/` | OpenCode-native skills are documented under `.opencode/skills/`, but the current implementation relies on the `.agents/skills/` compatibility path instead. |
| GitHub Copilot | `.github/copilot-instructions.md`, `.github/agents/*.agent.md`, `.agents/skills/`, `.vscode/mcp.json`, `.claude/settings.local.json`, `.github/hooks/*.json` | `.agents/skills/` and `.vscode/mcp.json` are officially documented Copilot CLI locations, but this repo still skips other official Copilot locations such as `.github/skills/`, `.claude/skills/`, `.github/mcp.json`, and `.mcp.json`. |

### Hook Wiring Audit

Validated from the current Go implementation.

#### File locations

| Platform | Official hook location | Go implementation | Notes |
|----------|------------------------|-------------------|-------|
| Claude Code | `.claude/settings*.json` | Yes | Wires Claude-compatible hook settings, but the management commands still source from `~/.agents/settings/*/claude-code.json` or `~/.agents/hooks/*/claude-code.json`, not from native Claude files. |
| Cursor | `.cursor/hooks.json` | Yes | Wires `~/.agents/hooks/{scope}/cursor.json` to project and user `hooks.json`. |
| Codex | `.codex/hooks.json` | Yes | Renders and writes repo `.codex/hooks.json` and user `~/.codex/hooks.json` via `renderCodexHookConfig` (`internal/platform/codex.go`, `internal/platform/hooks.go`); managed regular file, not a symlink. |
| GitHub Copilot | `.github/hooks/*.json` and CLI current-working-directory hooks | Partial | Links project `.github/hooks/*.json` and also wires Claude-compatible settings. Repo-scope `.github/copilot/settings*.json` (documented 2026-05-25) and user-scope `~/.copilot/hooks/` directory are NOT yet wired. |
| OpenCode | No dedicated hook file documented | No | No OpenCode-specific hook handling is implemented here. |

#### Event coverage (re-verified 2026-06-23 against current code)

Comparison of documented vendor events vs. the per-platform event tables in `internal/platform/hooks.go` (`claudeEventTable`, `codexEventTable`, `cursorEventTable`, `copilotEventTable`). A `dot-agents` `HookSpec.When:` value is "wired" for a platform if that platform's table maps it to the platform's documented event name today. The earlier 2026-05-25 audit's ❌ cells for Codex, Cursor, and Copilot are now **stale** — the current tables map most of those events (Copilot now maps 13 of the canonical events, not 3).

| `HookSpec.When` | Claude (`claudeEventTable`) | Codex (`codexEventTable`) | Cursor (`cursorEventTable`) | Copilot (`copilotEventTable`) |
|------------------|---------------------------|--------------------------|----------------------------|-------------------------------|
| `session_start` | `SessionStart` ✅ | `SessionStart` ✅ | `sessionStart` ✅ | `sessionStart` ✅ |
| `session_end` | `SessionEnd` ✅ | — (vendor event missing) | `sessionEnd` ✅ | `sessionEnd` ✅ |
| `user_prompt_submit` | `UserPromptSubmit` ✅ | `UserPromptSubmit` ✅ | `beforeSubmitPrompt` ✅ | `userPromptSubmitted` ✅ |
| `pre_tool_use` | `PreToolUse` ✅ | `PreToolUse` ✅ | `preToolUse` ✅ | `preToolUse` ✅ |
| `post_tool_use` | `PostToolUse` ✅ | `PostToolUse` ✅ | `postToolUse` ✅ | `postToolUse` ✅ |
| `post_tool_use_failure` | `PostToolUseFailure` ✅ | — (vendor event missing) | `postToolUseFailure` ✅ | `postToolUseFailure` ✅ |
| `notification` | `Notification` ✅ | — | — | `notification` ✅ |
| `permission_request` | `PermissionRequest` ✅ | `PermissionRequest` ✅ | — | `permissionRequest` ✅ |
| `pre_compact` | `PreCompact` ✅ | `PreCompact` ✅ | `preCompact` ✅ | `preCompact` ✅ |
| `stop` | `Stop` ✅ | `Stop` ✅ | `stop` ✅ | `agentStop` ✅ (camelCase, not `stop`) |
| `subagent_start` | `SubagentStart` ✅ | `SubagentStart` ✅ | `subagentStart` ✅ | `subagentStart` ✅ |
| `subagent_stop` | `SubagentStop` ✅ | `SubagentStop` ✅ | `subagentStop` ✅ | `subagentStop` ✅ |
| `message_display` | ❌ (not in `claudeEventTable`) | — | — | — |

Cells: ✅ = mapped today; ❌ = vendor documents the event but the platform's table does not yet map it; — = vendor does not document the event for that platform.

Remaining gaps as of 2026-06-23:

- **Claude `message_display`**: Claude now documents `MessageDisplay`, but it is **not** present in `claudeEventTable` — the only genuine event-mapping gap in this table today. (Wiring it is a separately-routed code change.)
- **Codex** documents the narrowest set; it has no `session_end`, `post_tool_use_failure`, `notification`, or any of the Cursor fine-grained shell/MCP/file events, so those fall through.
- **Cursor** has the widest surface (shell/MCP/file-edit/tab events in `cursorEventTable`) but does not document `notification` or `permission_request`.
- **Copilot** uniquely exposes `error_occurred` → `errorOccurred`. Its terminal event is `agentStop` (camelCase), correctly mapped today.
- **OpenCode**: still has no event table; OpenCode's hook surface is not addressed by `internal/platform/hooks.go` at all.
