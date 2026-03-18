# Platfrom file locations for resources

## Cursor:

### [rules](https://cursor.com/docs/rules)

Cursor supports four types of rules:

Project Rules

Stored in .cursor/rules, version-controlled and scoped to your codebase.

User Rules

Global to your Cursor environment. Used by Agent (Chat).

Team Rules

Team-wide rules managed from the dashboard. Available on Team and Enterprise plans.

AGENTS.md

Agent instructions in markdown format. Simple alternative to .cursor/rules. Nested AGENTS.md support in subdirectories is available. You can place AGENTS.md files in any subdirectory of your project, and they will be automatically applied when working with files in that directory or its children.

### [skills](https://cursor.com/docs/skills):

Skill directories
Skills are automatically loaded from these locations:

Location Scope
.agents/skills/ Project-level
.cursor/skills/ Project-level
~/.cursor/skills/ User-level (global)
For compatibility, Cursor also loads skills from Claude and Codex directories: .claude/skills/, .codex/skills/, ~/.claude/skills/, and ~/.codex/skills/.

Each skill should be a folder containing a SKILL.md file:

.agents/
└── skills/
└── my-skill/
└── SKILL.md
Skills can also include optional directories for scripts, references, and assets:

.agents/
└── skills/
└── deploy-app/
├── SKILL.md
├── scripts/
│ ├── deploy.sh
│ └── validate.py
├── references/
│ └── REFERENCE.md
└── assets/
└── config-template.json

### [agents](https://cursor.com/docs/subagents):

Define custom subagents to encode specialized knowledge, enforce team standards, or automate repetitive workflows.

File locations
|Type| Location| Scope|
|----|----------|--------|
| Project subagents | .cursor/agents/| Current project only|
| | .claude/agents/ | Current project only (Claude compatibility)|
| | .codex/agents/ | Current project only (Codex compatibility)|
| User subagents | ~/.cursor/agents/ | All projects for current user|
| | ~/.claude/agents/ | All projects for current user (Claude compatibility)|
| | ~/.codex/agents/ | All projects for current user (Codex compatibility)|

Project subagents take precedence when names conflict. When multiple locations contain subagents with the same name, .cursor/ takes precedence over .claude/ or .codex/.

### [hooks](https://cursor.com/docs/hooks):

Create a hooks.json file. You can create it at the project level (<project>/.cursor/hooks.json) or in your home directory (~/.cursor/hooks.json). Project-level hooks apply only to that specific project, while home directory hooks apply globally.

---

## Claude Code:

### [rules/memory](https://code.claude.com/docs/en/memory)

| Location | Scope |
|----------|-------|
| `CLAUDE.md` (project root) | Project-level |
| `.claude/CLAUDE.md` | Project-level (alternative) |
| `.claude/rules/*.md` | Project-level (scoped rules via YAML frontmatter) |
| `~/.claude/CLAUDE.md` | User-level (global) |
| `~/.claude/rules/` | User-level |

### [skills](https://code.claude.com/docs/en/skills)

| Location | Scope |
|----------|-------|
| `.claude/skills/<name>/SKILL.md` | Project-level |
| `~/.claude/skills/<name>/SKILL.md` | User-level (global) |

Nested discovery: `.claude/skills/` in subdirectories auto-discovered for monorepos.

### [agents/subagents](https://code.claude.com/docs/en/sub-agents)

| Location | Scope |
|----------|-------|
| `.claude/agents/` | Project-level |
| `~/.claude/agents/` | User-level (global) |

### [MCP servers](https://code.claude.com/docs/en/mcp)

| Location | Scope |
|----------|-------|
| `.mcp.json` (project root) | Project-level (version-controlled) |
| `~/.claude.json` | User-level |

### [hooks](https://code.claude.com/docs/en/hooks)

| Location | Scope |
|----------|-------|
| `.claude/settings.json` | Project-level |
| `.claude/settings.local.json` | Project-level (local only, gitignored) |
| `~/.claude/settings.json` | User-level |

---

## Codex (OpenAI):

### [instructions](https://developers.openai.com/codex/guides/agents-md/)

| Location | Scope |
|----------|-------|
| `AGENTS.md` (project root, walks tree from git root) | Project-level |
| `AGENTS.override.md` | Project-level (takes precedence) |
| `~/.codex/AGENTS.md` | User-level (global) |
| `~/.codex/AGENTS.override.md` | User-level (takes precedence) |

Discovery: Walks from git root down to CWD, merging files root-to-leaf. One file per directory level.

### [skills](https://developers.openai.com/codex/skills/)

| Location | Scope |
|----------|-------|
| `.agents/skills/<name>/SKILL.md` | Project-level (scanned from CWD up to repo root) |
| `~/.agents/skills/<name>/SKILL.md` | User-level (global) |

### [MCP / config](https://developers.openai.com/codex/config-reference/)

| Location | Scope |
|----------|-------|
| `.codex/config.toml` (`[mcp_servers]` section) | Project-level |
| `~/.codex/config.toml` (`[mcp_servers]` section) | User-level |

---

## OpenCode:

### [rules/instructions](https://opencode.ai/docs/rules/)

| Location | Scope |
|----------|-------|
| `AGENTS.md` (project root) | Project-level (preferred) |
| `CLAUDE.md` (project root) | Project-level (Claude Code compat fallback) |
| `~/.config/opencode/AGENTS.md` | User-level |

### [skills](https://opencode.ai/docs/skills/)

| Location | Scope |
|----------|-------|
| `.opencode/skills/<name>/SKILL.md` | Project-level (primary) |
| `.claude/skills/<name>/SKILL.md` | Project-level (Claude compat) |
| `.agents/skills/<name>/SKILL.md` | Project-level (agent compat) |
| `~/.config/opencode/skills/<name>/SKILL.md` | User-level (primary) |
| `~/.claude/skills/<name>/SKILL.md` | User-level (Claude compat) |
| `~/.agents/skills/<name>/SKILL.md` | User-level (agent compat) |

Disable compat: `OPENCODE_DISABLE_CLAUDE_CODE_SKILLS=1`

### [agents](https://opencode.ai/docs/agents/)

| Location | Scope |
|----------|-------|
| `.opencode/agents/<name>.md` | Project-level (markdown with YAML frontmatter) |
| `~/.config/opencode/agents/<name>.md` | User-level |

Note: Agent format is a single `.md` file (not a directory with `AGENT.md`). Filename = agent ID.

### [MCP servers](https://opencode.ai/docs/mcp-servers/)

| Location | Scope |
|----------|-------|
| `opencode.json` / `opencode.jsonc` (`mcp` object) | Project-level |
| `~/.config/opencode/opencode.json` (`mcp` object) | User-level |

### [commands](https://opencode.ai/docs/commands/)

| Location | Scope |
|----------|-------|
| `.opencode/commands/<name>.md` | Project-level |
| `~/.config/opencode/commands/<name>.md` | User-level |

### [tools](https://opencode.ai/docs/custom-tools/)

| Location | Scope |
|----------|-------|
| `.opencode/tools/` (JS/TS files) | Project-level |
| `~/.config/opencode/tools/` (JS/TS files) | User-level |

---

## GitHub Copilot:

### [instructions](https://docs.github.com/en/copilot/customizing-copilot/adding-custom-instructions-for-github-copilot)

| Location | Scope |
|----------|-------|
| `.github/copilot-instructions.md` | Project-level (repository-wide) |
| `.github/instructions/<name>.instructions.md` | Project-level (path-specific, `applyTo` frontmatter) |
| `~/.config/github-copilot/intellij/global-copilot-instructions.md` | User-level (JetBrains, macOS) |

### [skills](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-skills)

| Location | Scope |
|----------|-------|
| `.github/skills/<name>/SKILL.md` | Project-level (primary) |
| `.claude/skills/<name>/SKILL.md` | Project-level (Claude compat) |
| `.agents/skills/<name>/SKILL.md` | Project-level (agent compat) |
| `~/.copilot/skills/<name>/SKILL.md` | User-level (primary) |
| `~/.claude/skills/<name>/SKILL.md` | User-level (Claude compat) |
| `~/.agents/skills/<name>/SKILL.md` | User-level (agent compat) |

### [agents](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-custom-agents)

| Location | Scope |
|----------|-------|
| `.github/agents/<name>.agent.md` | Project-level (primary) |
| `.claude/agents/<name>.agent.md` | Project-level (Claude compat) |
| `.agents/agents/<name>.agent.md` | Project-level (agent compat) |
| `~/.copilot/agents/<name>.agent.md` | User-level |
| `~/.claude/agents/<name>.agent.md` | User-level (Claude compat) |

Note: Agent files use `.agent.md` extension with YAML frontmatter. Must be on default branch to activate.

### [MCP servers](https://docs.github.com/copilot/customizing-copilot/using-model-context-protocol/extending-copilot-chat-with-mcp)

| Location | Scope |
|----------|-------|
| `.vscode/mcp.json` | Project-level (VS Code) |
| VS Code User `settings.json` | User-level (VS Code) |
| `~/.copilot/mcp-config.json` | User-level (CLI) |

### [hooks](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/use-hooks)

| Location | Scope |
|----------|-------|
| `.github/hooks/<name>.json` | Project-level |
| `.github/hooks/<name>/hooks.json` | Project-level (folder-based) |
