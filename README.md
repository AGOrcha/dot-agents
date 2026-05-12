# dot-agents

**The operational layer for AI coding agents**

One CLI to manage configurations — and soon, workflows — across Cursor, Claude Code, Codex, GitHub Copilot, and more.

```bash
# Install
brew tap AGOrcha/tap && brew install dot-agents

# Set up
da init
da add ~/Github/myproject

# Check status
da status
da doctor

# Refresh after pulling changes
da refresh
```

---

## The Problems

### 1. Config Fragmentation

Every AI coding agent has its own config location and format:

| Agent | Config Location | Format |
|-------|-----------------|--------|
| Cursor | `.cursor/rules/*.mdc` | MDC (Markdown) |
| Claude Code | `CLAUDE.md`, `.claude/` | Markdown, JSON |
| Codex | `AGENTS.md` | Markdown |
| GitHub Copilot | `.github/copilot-instructions.md`, `.github/skills/`, `.github/agents/` | Markdown |

This leads to:
- **Duplicated rules** across every repository
- **No way to share** common configurations
- **Inconsistent setups** between machines

### 2. Workflow Fragmentation

Autonomous agents already behave like a workflow system — resuming work across sessions, persisting plans, verifying as they go — but each platform scatters this state in its own format and location:

- **Context amnesia**: 30-40 minutes per session re-explaining what the agent already knew yesterday
- **Scattered plans**: Plans, tasks, and checkpoints live in different places per platform
- **Repeated verification**: Agents rediscover what's broken vs. what they just caused
- **Lost handoffs**: Session continuity depends on the agent reconstructing state from scratch

## The Solution

**dot-agents** solves both problems in layers:

- **Today**: Unified config management — one source of truth, distributed automatically
- **Next**: Workflow management — agents orient, persist, and propose changes autonomously

### Layer 1: Config Management (Shipped)

**dot-agents** creates a single source of truth at `~/.agents/`:

```
~/.agents/
├── config.json              # Projects, settings, feature flags
├── rules/
│   ├── global/              # Applied to ALL projects
│   │   ├── coding-style.mdc
│   │   └── security.mdc
│   └── myproject/           # Project-specific rules
│       └── api-patterns.mdc
├── skills/                  # Reusable agent skills (procedures)
│   ├── global/
│   │   └── deploy/SKILL.md
│   └── myproject/
├── agents/                  # Subagent definitions
│   ├── global/
│   │   └── reviewer/AGENT.md
│   └── myproject/
├── settings/                # Agent-specific settings
│   └── global/
└── mcp/                     # MCP server configurations
    └── global/
```

Then **symlinks and hard links** distribute configs to your projects automatically:

```
~/Github/myproject/
├── .cursor/rules/
│   ├── global--coding-style.mdc  → ~/.agents/rules/global/...
│   └── myproject--api-patterns.mdc → ~/.agents/rules/myproject/...
├── CLAUDE.md                     → ~/.agents/rules/global/claude-code.mdc
└── (your code)
```

### Layer 2: Workflow Management (Coming)

Agents will manage their own operational infrastructure through three primitives:

| Primitive | What It Does | Who Runs It |
|-----------|-------------|-------------|
| **Orient** | Load active plan, last checkpoint, verification state, recent lessons at session start | Agent (via hook) |
| **Persist** | Save files touched, tests run, blockers, and next action at natural breakpoints | Agent (auto) |
| **Propose** | Queue rule/skill/config changes for human review when patterns emerge | Agent → Human reviews |

The design principle: **agents operate, humans steer.** Zero new commands to learn — the agent handles workflow state and surfaces decisions through `da review`.

See [`research/`](research/) for the full analysis behind this direction.

## Installation

### Homebrew (recommended)

```bash
brew tap AGOrcha/tap
brew install dot-agents
```

### Direct Install (Go CLI, default)

```bash
curl -fsSL https://raw.githubusercontent.com/NikashPrakash/dot-agents/main/scripts/install.sh | bash
```

### Direct Install (TypeScript port subset)

```bash
curl -fsSL https://raw.githubusercontent.com/NikashPrakash/dot-agents/main/scripts/install.sh | bash -s -- --port ts
```

### Windows PowerShell (Go CLI)

```powershell
irm https://raw.githubusercontent.com/NikashPrakash/dot-agents/main/scripts/install-go.ps1 | iex
```

### Manual

```bash
git clone https://github.com/NikashPrakash/dot-agents ~/.dot-agents
cd ~/.dot-agents
go build -o ./bin/da ./cmd/dot-agents
export PATH="$HOME/.dot-agents/bin:$PATH"
```

### TypeScript port (optional, Windows-friendly subset)

The primary CLI is the **Go** `da` binary (Homebrew or `scripts/install.sh` above). For machines where **Node.js 20+** is easier than Go, this repo also ships an experimental **TypeScript** implementation under [`ports/typescript/`](ports/typescript/README.md).

- **Same goal:** Stage 1 config, links, skills, agents, and hooks — not a silent replacement for Go.
- **Different limits:** Knowledge graph commands, workflow **writes**, and loop orchestration stay **Go-only**. Read the boundary once in [`docs/TYPESCRIPT_PORT_BOUNDARY.md`](docs/TYPESCRIPT_PORT_BOUNDARY.md), then run `npm run build` and `node dist/cli.js --help` inside `ports/typescript/` before relying on it.

## Quick Start

```bash
# 1. Initialize ~/.agents/ with starter skills, agents, hooks, and config
da init

# 2. Add a project
da add ~/Github/myproject

# 3. Check what was linked
da status --audit
da doctor

# 4. Create reusable skills and subagents
da skills new deploy
da agents new reviewer

# 5. Or, inside a repo that already committed .agentsrc.json:
da install
```

## Commands

### Core

| Command | Description |
|---------|-------------|
| `init` | Initialize `~/.agents/` directory |
| `add <path>` | Add a project to management |
| `remove <project>` | Remove a project |
| `install` | Set up project from `.agentsrc.json` manifest (`--generate` to create one) |
| `import [project]` | Import existing configs from a project into `~/.agents/` |
| `status` | Show all managed projects (use `--audit` for details) |
| `doctor` | Health check and diagnostics |
| `refresh [project]` | Re-apply links and config to projects |

### Skills & Agents

| Command | Description |
|---------|-------------|
| `skills list [project]` | List shared or project-scoped skills |
| `skills new <name> [project]` | Create a new skill |
| `skills promote <name>` | Promote a repo-local skill into `~/.agents/skills/` |
| `agents list [project]` | List shared or project-scoped agents |
| `agents new <name> [project]` | Create a new subagent |
| `agents promote <name>` | Promote a repo-local agent into `~/.agents/agents/` |
| `agents import <name>` | Link a canonical agent into the current repo |
| `agents remove <name>` | Remove an imported agent link from the current repo |
| `hooks list [scope]` | List canonical hook bundles in `~/.agents/hooks/` |
| `hooks show <scope> <name>` | Show one canonical hook bundle |
| `hooks remove <scope> <name>` | Remove one canonical hook bundle |

### Sync

| Command | Description |
|---------|-------------|
| `sync init` | Initialize git repo in `~/.agents/` |
| `sync status` | Show git status |
| `sync commit` | Commit all changes |
| `sync push` | Push to remote |
| `sync pull` | Pull from remote |

### Utilities

| Command | Description |
|---------|-------------|
| `explain [topic]` | Self-documenting system descriptions |
| `--help` | Show help for any command |
| `--version` | Show version |

## How It Works

### Cursor Rules (Hard Links)

Cursor doesn't follow symlinks for `.cursor/rules/`, so dot-agents uses **hard links**:

```bash
# In your project
.cursor/rules/global--coding-style.mdc  # Hard link to ~/.agents/rules/global/coding-style.mdc
```

Hard links share the same file content (same inode), so edits in either location are reflected in both.

### Claude Code / Codex (Symlinks)

For `CLAUDE.md` and `AGENTS.md`, standard symlinks work:

```bash
CLAUDE.md  → ~/.agents/rules/global/claude-code.mdc
AGENTS.md  → ~/.agents/rules/global/agents.md
```

Codex also gets agent definitions (rendered to `.codex/agents/*.toml`), settings (`.codex/config.toml`), and hooks (`.codex/hooks.json`).

### Naming Convention

Files in `.cursor/rules/` are prefixed to show their source:
- `global--*.mdc` → From `~/.agents/rules/global/`
- `{project}--*.mdc` → From `~/.agents/rules/{project}/`

## Syncing Across Machines

Your `~/.agents/` directory is designed to be git-tracked:

```bash
# First time setup
da sync init
cd ~/.agents
git remote add origin git@github.com:YOU/agents-config.git
da sync push

# On another machine
git clone git@github.com:YOU/agents-config.git ~/.agents
da add ~/Github/myproject  # Re-link your projects
```

## Supported Agents

| Agent | Status | Config Files |
|-------|--------|--------------|
| **Cursor** | ✅ Full | `.cursor/rules/*.mdc` |
| **Claude Code** | ✅ Full | `CLAUDE.md`, `.claude/` |
| **Codex** | ✅ Full | `AGENTS.md`, `.codex/config.toml`, `.codex/agents/*.toml`, `.codex/hooks.json` |
| **OpenCode** | ⚠️ Basic | `opencode.json`, `.opencode/agent/*.md` |
| **GitHub Copilot** | ✅ Full | `.github/copilot-instructions.md`, `.github/skills/*/SKILL.md`, `.github/agents/*.agent.md` |

## Requirements

- **macOS** or **Linux** for the **Go** CLI via Homebrew, `scripts/install.sh`, or a local `go build`.
- **Windows:** use `scripts/install-go.ps1` for the Go CLI, or the **TypeScript** port under `ports/typescript/` when you only need the Stage 1 subset documented there.
- **git** (for sync features)

## Configuration

### config.json

```json
{
  "schema_version": "1.0",
  "projects": {
    "myproject": {
      "path": "/Users/you/Github/myproject",
      "added": "2026-01-10T10:00:00Z"
    }
  },
  "defaults": {
    "link_type": "auto"
  },
  "features": {
    "tasks": false,
    "history": false
  }
}
```

### Skills

Skills are reusable procedure documents that agents can invoke:

```bash
# Create a new skill
da skills new deploy

# List all skills
da skills list

# Promote a repo-local skill into ~/.agents/
da skills promote deploy
```

Skills live in `~/.agents/skills/global/` with this structure:
- `SKILL.md` - The skill definition with frontmatter
- `scripts/` - Optional helper scripts
- `references/` - Optional additional context

### Subagents

Subagents are directory-based agent definitions:

```bash
# Create a new subagent
da agents new reviewer

# List all subagents
da agents list

# Promote a repo-local subagent into ~/.agents/
da agents promote reviewer
```

Each subagent is a directory containing:
- `AGENT.md` - Required agent definition with frontmatter (name, description, model)
- `scripts/` - Optional helper scripts
- `references/` - Optional additional context documents

### Hooks

Inspect canonical hook bundles stored in `~/.agents/hooks/`:

```bash
# List all hooks
da hooks list

# Inspect one hook bundle
da hooks show global session-orient
```

### Project Manifests (.agentsrc.json)

Commit a `.agentsrc.json` to your repo so any contributor can set up agent configs from the repo itself:

```bash
# Generate manifest from current ~/.agents/ state
da install --generate

# Set up a cloned repo from its manifest
da install
```

### Importing Existing Configs

Already have agent configs scattered across your projects? Import them into `~/.agents/`:

```bash
da import myproject
```

This detects existing rules, skills, agents, and hooks in the project and copies them into the central `~/.agents/` directory.

## Roadmap

### Agent-as-Operator

The next major evolution: agents run `da` autonomously instead of humans operating it manually. The agent manages config, skills, rules, and workflow state — surfacing only decisions that require human judgment.

Changes follow an **approval gradient**:
- **Auto-apply**: Checkpoints, verification results, plan progress, lessons after corrections
- **Propose-and-apply**: New rules, skills, workflow config changes — human confirms
- **Escalate**: Conflicting rules, stale config affecting production, cross-repo drift

### Workflow State

Based on analysis of real session data across Claude Code, Cursor, and Codex ([research](research/AUTONOMOUS_WORKFLOW_MANAGEMENT_RESEARCH.md)), dot-agents will manage six workflow concerns:

1. **Resume context** — collect active plan, last handoff, and likely next step
2. **Plan & task state** — canonical plan artifacts with dependency-aware phases
3. **Verification state** — persist test/lint/build results so agents stop rediscovering what's broken
4. **Approvals & tool health** — surface auth expiry, rate-limit risk, environment readiness
5. **Repo preferences** — persist per-repo habits (test commands, CI expectations, review preferences)
6. **Delegation & handoff** — bounded fan-out with ownership constraints and merge-back summaries

### Multi-Agent Coordination

Drawing from [supervisor patterns](research/openclaw-hermes-supervisor-pattern.md) and [swarm orchestration](research/codex-multi-agent-swarms-playbook.md), dot-agents will support:

- **Context engineering**: Front-load subagents with structured context bundles so they don't waste tokens rediscovering state
- **Structured coordination**: Intent marker protocols to prevent infinite loops and drift between cooperating agents
- **Bounded fan-out**: Spawn workers with clear ownership constraints, collect results into parent continuation artifacts

## FAQ

**Q: Why hard links for Cursor?**

Cursor's rule system doesn't follow symlinks. Hard links share the actual file content, so changes sync automatically.

**Q: Can I use this with existing projects?**

Yes! `da add` won't overwrite existing files unless you use `--force`.

**Q: Is my config private?**

Yes. Everything stays in `~/.agents/` on your machine. Git sync is optional and to your own repo.

**Q: What if I don't use all the agents?**

That's fine! dot-agents only creates config files for agents it detects or that you have rules for.

**Q: What is `da refresh` for?**

After pulling changes to `~/.agents/` from git, run `refresh` to re-apply links and configs to all your projects. This ensures your projects stay in sync with your central config.

**Q: How do skills differ from rules?**

- **Rules** (`.mdc` files) are always-active guidelines applied to all projects
- **Skills** (`SKILL.md` files) are on-demand procedures that agents invoke when needed, like deployment checklists or code review workflows

**Q: Can I sync my config across machines?**

Yes! `da sync` helps you manage `~/.agents/` as a git repository. Clone it on another machine and run `da refresh` to set up all your projects.

## Contributing

Contributions welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.

## License

[MIT](LICENSE)

---

Built for developers who use AI coding agents daily. Designed so agents can operate themselves.
