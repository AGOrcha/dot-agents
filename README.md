# dot-agents

**The operational layer for AI coding agents**

One CLI to manage configurations and workflow state across Cursor, Claude Code, Codex, GitHub Copilot, and OpenCode.

```bash
# Install
brew tap AGOrcha/tap && brew install da

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

| Agent | Instruction / rule files | Format |
|-------|--------------------------|--------|
| Cursor | `.cursor/rules/*.mdc` (also `AGENTS.md`) | MDC / Markdown |
| Claude Code | `CLAUDE.md`, `.claude/CLAUDE.md`, `.claude/rules/*.md` | Markdown, JSON |
| Codex | `AGENTS.md`, `AGENTS.override.md` | Markdown |
| OpenCode | `AGENTS.md` (also `CLAUDE.md`), `.opencode/agent/*.md` | Markdown |
| GitHub Copilot | `.github/copilot-instructions.md`, `.github/instructions/**/*.instructions.md` | Markdown |

Beyond the base instruction file, each agent supports more specific rule files —
Cursor's per-rule `.mdc` files, Claude's nested `.claude/rules/*.md`, and Copilot's
path-scoped `.github/instructions/**/*.instructions.md`; dot-agents projects all of
them from one canonical source. See `docs/PLATFORM_DIRS_DOCS.md` for the full matrix.

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

**dot-agents** solves both problems in layers, both shipping today:

- **Config management** — one source of truth, distributed automatically
- **Workflow management** — agents orient, persist, delegate, and propose
  changes autonomously, backed by a local knowledge graph of structured
  project memory (`da kg`)

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
├── hooks/                   # Canonical hook bundles
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

### Layer 2: Workflow Management (Shipping)

Workflow-state management ships today through `da workflow`: agents orient at
session start, persist checkpoints and verification state as they go, manage
canonical plans and tasks, delegate bounded fan-out work, and queue
rule/skill/config changes for human review through `da review`. Orient and
related context draw on the local knowledge graph (`da kg`) — structured
project memory and a code graph that let agents resume with what was already
learned (see the [Knowledge Graph](#knowledge-graph) section).

| Primitive | What It Does | Status |
|-----------|-------------|--------|
| **Propose** | Agents queue rule/skill/config changes for human review when patterns emerge | Shipped (`da review`) |
| **Orient** | Load active plan, last checkpoint, verification state at session start | Shipped (`da workflow orient`) |
| **Persist** | Save checkpoints, verification results, and plan/task progress at natural breakpoints | Shipped (`da workflow checkpoint`, `verify`, `advance`) |
| **Delegate** | Bounded fan-out to sub-agents with write-scope constraints and merge-back | Shipped (`da workflow fanout`, `merge-back`) |

The design principle: **agents operate, humans steer.** Deeper multi-agent
coordination is on the roadmap; see the Roadmap section below.

## Installation

### Homebrew (recommended)

```bash
brew tap AGOrcha/tap
brew install da
```

### Install script

Downloads the prebuilt `da` binary onto your `PATH` — no Go toolchain required:

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/AGOrcha/dot-agents/master/scripts/install.sh | bash
```

```powershell
# Windows PowerShell
irm https://raw.githubusercontent.com/AGOrcha/dot-agents/master/scripts/install.ps1 | iex
```

### Go toolchain (`go install`)

If you already have **Go 1.26.2+**, install the CLI straight from source with
the Go toolchain (or set `GOTOOLCHAIN=auto` to let Go fetch the required
toolchain automatically):

```bash
go install github.com/AGOrcha/dot-agents/cmd/da@latest
# or
go install github.com/AGOrcha/dot-agents/cmd/da@<release-version>
```

The `da` binary is placed in `$(go env GOBIN)` (falling back to
`$(go env GOPATH)/bin`); ensure that directory is on your `PATH`.

### Manual (from source)

Requires **Go 1.26.2+** (or set `GOTOOLCHAIN=auto`).

```bash
git clone https://github.com/AGOrcha/dot-agents ~/.dot-agents
cd ~/.dot-agents
go build -ldflags "-s -w" -o ./bin/da ./cmd/da
export PATH="$HOME/.dot-agents/bin:$PATH"
```

### Verify a release (optional)

Releases are signed with [Cosign](https://github.com/sigstore/cosign) using
keyless signing via Sigstore + GitHub OIDC. See
[`docs/RELEASE_VERIFICATION.md`](docs/RELEASE_VERIFICATION.md) for the
step-by-step `cosign verify-blob` recipe.

## Quick Start

```bash
# 1. Initialize ~/.agents/ with starter skills, agents, hooks, and config
#    (prompts to confirm; pass -y to run non-interactively)
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

`da` exposes 22 top-level commands (excluding `help` and `completion`).

### Project Management

| Command | Description |
|---------|-------------|
| `init` | Initialize `~/.agents/` directory structure |
| `add <path>` | Add a project to da management |
| `remove <project>` | Remove a project from da management |
| `refresh [project]` | Refresh managed setup from `~/.agents/`; auto-enables newly-installed editors and updates their versions. EXACT by default — prunes managed shared-target links no longer in the resolved set; pass `--inexact` to keep the additive behavior |
| `import [project]` | Import configs from project/global scope into `~/.agents/` |
| `install` | Set up project from `.agentsrc.json` manifest (`--generate` to create one). EXACT by default — prunes managed shared-target links no longer in the resolved set; pass `--inexact` to keep additive behavior, `--strict` to fail on a missing declared resource |
| `status` | Show managed projects and link health; for effective-config detail run `da config explain` (use `--audit` for details) |
| `doctor` | Check installations, validate links, detect issues (read-only — reports problems and the command to fix them; never repairs) |

### Configuration

| Command | Description |
|---------|-------------|
| `config explain [field]` | Show the effective `.agentsrc.json` value of a field and which layer set it (`--all`, `--flags`, `--json`) |
| `config sync` | Re-fetch every declared layer regardless of TTL, re-resolve, and rewrite the config section of `.agentsrc.lock` — the uv `--upgrade` analog (`--layer source-id:path`, `--json`) |
| `config lint` | Validate the repo-local `.agentsrc.json` and each `extends` layer against the AgentsRC layer schema; non-zero exit if invalid (`--json`) |
| `config verify` | Offline setup contract check — manifest parses, declared local source layers exist, integrations ready, and remote `extends` layers are cached at the lockfile's SHA (`--json`; non-zero exit on failure) |
| `config relevance` | Resolve a task's execution profile (units, topology, lenses) by `app_type` (`--filter`, `--app-type`, `--task`, `--stage`, `--recompute`, `--json`; see [docs/CONFIG_RELEVANCE.md](docs/CONFIG_RELEVANCE.md)) |

#### Layered config & the lockfile (`.agentsrc.lock`)

A `.agentsrc.json` manifest may `extends` one or more config layers sourced from
git, local paths, HTTP, or OCI registries, declared as `source:path@version`.
When the layers are resolved, the resolved layer SHAs are pinned in
`.agentsrc.lock` so every machine projects the same effective config.

- `da config sync` re-checks every declared layer upstream (ignoring TTL),
  re-resolves the stack, and rewrites the config section of `.agentsrc.lock`.
  This is the explicit upstream re-check — the uv `--upgrade` analog.
- `da refresh` and `da install` re-project the locked config locally and only
  re-resolve when the lock is stale, so routine relinking never reaches the
  network for an unchanged stack.

### Skills & Agents

| Command | Description |
|---------|-------------|
| `skills list [project]` | List shared or project-scoped skills |
| `skills new <name> [project]` | Create a new skill |
| `skills promote <name>` | Promote a repo-local skill to shared storage |
| `agents list [project]` | List shared or project-scoped agents |
| `agents new <name> [project]` | Create a new agent |
| `agents promote <name>` | Promote a repo-local agent to shared storage |
| `agents import <name>` | Link a canonical agent from `~/.agents/agents/` into this repo |
| `agents remove <name>` | Unlink agent symlinks from this repo and drop the manifest entry |

### Canonical Resource Inspection

These inspect and manage canonical files under `~/.agents/`. Each supports
`list [scope]`, `show <scope> <name>`, and `remove <scope> <name>`.

| Command | Description |
|---------|-------------|
| `rules` | Inspect and manage canonical `~/.agents/rules` files |
| `hooks` | Inspect and manage canonical `~/.agents/hooks` bundles |
| `mcp` | Inspect and manage canonical `~/.agents/mcp` config files |
| `settings` | Inspect and manage canonical `~/.agents/settings` files |

### Workflow Proposals

| Command | Description |
|---------|-------------|
| `review` | Review pending workflow proposals |
| `review show <id>` | Show a pending proposal |
| `review approve <id>` | Approve and apply a pending proposal |
| `review reject <id>` | Reject a pending proposal |

### Workflow State

`da workflow` captures repository-local workflow state — canonical plans,
checkpoints, verification logs, preferences, fanout artifacts, and bridge
queries — so humans and agents can resume work safely.

| Command | Description |
|---------|-------------|
| `workflow status` | Show workflow state for the current project |
| `workflow orient` | Render session orient context for the current project |
| `workflow next` | Suggest the next actionable canonical task |
| `workflow eligible` | List all unblocked eligible tasks across active plans with conflict detection |
| `workflow complete --plan <id>` | Probe scoped plan-completion state |
| `workflow health` | Show workflow health snapshot |
| `workflow app-types` | List available app_type values for the current repo |

#### Plans & Tasks

| Command | Description |
|---------|-------------|
| `workflow plan` | List canonical plans (`show`, `graph`, `schedule` subcommands) |
| `workflow plan create <id> --title <t>` | Create a new canonical plan with PLAN.yaml and TASKS.yaml stubs |
| `workflow plan update <id>` | Update PLAN.yaml metadata fields |
| `workflow plan archive --plan <id>` | Archive one or more completed canonical plans |
| `workflow plan derive-scope <plan> <task>` | Derive a candidate scope-evidence sidecar via KG/CRG queries |
| `workflow plan check-scope <plan> <task>` | Check changed files against a task's scope-evidence sidecar |
| `workflow task add <plan> --id <id> --title <t>` | Append a new task to a plan's TASKS.yaml |
| `workflow task update <plan> --task <id>` | Update notes, write-scope, or title for a task |
| `workflow tasks <plan>` | Show tasks for a canonical plan |
| `workflow slices <plan>` | Show slices for a canonical plan |
| `workflow advance <plan> --task <id> --status <s>` | Advance a task's status within a plan |

#### Persist & Verify

| Command | Description |
|---------|-------------|
| `workflow checkpoint --message <m>` | Write a checkpoint for the current project |
| `workflow log` | Show recent checkpoint log entries |
| `workflow verify record` | Record a verification run (test/lint/build/review) |
| `workflow verify log` | Show verification log entries |
| `workflow prefs` | Show resolved workflow preferences (`set-local`, `set-shared`) |
| `workflow graph query` | Query knowledge graph context by bridge intent (`graph health`) |

#### Delegation

| Command | Description |
|---------|-------------|
| `workflow fanout --plan <id> --task <id>` | Delegate a task to a sub-agent with a bounded write scope |
| `workflow merge-back --task <id> --summary <s>` | Record a sub-agent's completed work as a merge-back artifact |
| `workflow delegation closeout` | Archive merge-back artifacts and reconcile canonical task state |
| `workflow delegation gate --task <id>` | Evaluate task-local review evidence into a parent-gate outcome |
| `workflow fold-back create` | Route loop observations into plan artifacts or proposals (`update`, `list`) |
| `workflow bundle stages <path>` | Expand a delegation bundle into the ordered stage list |

#### Drift (cross-repo, read-only by default)

| Command | Description |
|---------|-------------|
| `workflow drift` | Detect workflow drift across managed repos (read-only) |
| `workflow sweep` | Plan and optionally apply fixes for workflow drift (`--apply`) |

### Knowledge Graph

`da kg` creates, queries, and maintains the local knowledge graph used for
structured project memory, bridge queries, and code-to-note context.

| Command | Description |
|---------|-------------|
| `kg setup` | Initialize the knowledge graph at `KG_HOME` |
| `kg health` | Show knowledge graph health |
| `kg serve` | Start the MCP server (stdio transport, JSON-RPC 2.0) |
| `kg ingest [file]` | Ingest a raw source into the graph (`--all`, `--type`, `--dry-run`) |
| `kg queue` | List pending sources in the inbox |
| `kg query [string] --intent <i>` | Query the knowledge graph by intent |
| `kg lint` | Check graph integrity and knowledge quality (`--check`) |

#### Maintenance

| Command | Description |
|---------|-------------|
| `kg maintain reweave` | Repair broken links and add missing source_ref links |
| `kg maintain mark-stale` | Mark notes not updated beyond threshold as stale (`--days`) |
| `kg maintain compact` | Archive superseded and archived notes |
| `kg sync` | Sync graph via git pull + lint (`--push` to push) |
| `kg warm` | Sync hot filesystem notes into the warm SQLite layer (`stats` subcommand) |

#### Bridge

| Command | Description |
|---------|-------------|
| `kg bridge query --intent <i>` | Execute a bridge intent query |
| `kg bridge health` | Show adapter availability and health |
| `kg bridge mapping` | Show bridge intent to KG intent mapping |

#### Code Graph

| Command | Description |
|---------|-------------|
| `kg build` | Full code graph build (re-parse all files via code-review-graph) |
| `kg update` | Incremental code graph update (changed files only) |
| `kg code-status` | Show code graph stats (nodes, edges, languages) |
| `kg changes` | Detect change impact in the current diff |
| `kg impact [file...]` | Show blast radius for given files (or current diff) |
| `kg flows` | List detected execution flows |
| `kg communities` | List detected code communities |
| `kg postprocess` | Rebuild flows, communities, and FTS index |
| `kg link add\|list\|remove` | Manage note→code symbol cross-references |

### Sync

`da sync` wraps git operations on `~/.agents/`.

| Command | Description |
|---------|-------------|
| `sync init` | Initialize git repo in `~/.agents/` |
| `sync status` | Show git status |
| `sync commit` | Commit all changes |
| `sync push` | Push to remote |
| `sync pull` | Pull from remote |
| `sync log` | Show recent commit log |

### Utilities

| Command | Description |
|---------|-------------|
| `explain [topic]` | Explain da concepts |
| `score run` | Compute and query agent-run outcome scores (`iteration <N>`, `session <id>` subcommands) |
| `session stats` | Show usage statistics from each installed AI platform |
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
| **Claude Code** | ✅ Full | `CLAUDE.md`, `.claude/CLAUDE.md`, `.claude/rules/*.md`, `.claude/` |
| **Codex** | ✅ Full | `AGENTS.md`, `AGENTS.override.md`, `.codex/config.toml`, `.codex/agents/*.toml`, `.codex/hooks.json` |
| **OpenCode** | ⚠️ Basic | `AGENTS.md`, `opencode.json`, `.opencode/agent/*.md` |
| **GitHub Copilot** | ✅ Full | `.github/copilot-instructions.md`, `.github/instructions/**/*.instructions.md`, `.github/agents/*.agent.md` |

## Requirements

- **macOS** or **Linux** for the CLI via Homebrew, `scripts/install.sh`, or a local `go build`.
- **Windows:** use `scripts/install.ps1` for the CLI.
- **Go 1.26.2+** — only for `go install` or building from source. The Homebrew and `scripts/install.sh` paths ship a prebuilt binary and need no Go toolchain. (Set `GOTOOLCHAIN=auto` to auto-fetch the toolchain.)
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

`da install` is **EXACT** by default: the platform link pass prunes managed
shared-target links that are no longer in the resolved set, so the tree
converges to exactly what the lock declares. Pass `--inexact` to keep the
additive behavior and leave stale managed links in place, or `--strict` to
fail if any declared resource is missing. `da refresh` follows the same
EXACT/prune-by-default convention with the same `--inexact` opt-out.

### Importing Existing Configs

Already have agent configs scattered across your projects? Import them into `~/.agents/`:

```bash
da import myproject
```

This detects existing rules, skills, agents, and hooks in the project and copies them into the central `~/.agents/` directory.

### Knowledge Graph

The knowledge graph (`da kg`) is a local store of structured project memory:
durable notes about decisions, entities, concepts, and sessions, plus a
code graph and the cross-references between them. It backs the `workflow
orient` context an agent loads at session start, so the agent resumes with
what was already learned instead of rediscovering it.

Notes carry a type — `source`, `entity`, `concept`, `synthesis`,
`decision`, `repo`, or `session` — and live as hot Markdown files that are
synced into a warm SQLite layer for fast querying. Raw material (Markdown,
text, PDFs, URLs, transcripts) is brought in through an inbox and turned
into typed notes:

```bash
# One-time: initialize the graph at KG_HOME
da kg setup

# Ingest a source (or --all to drain the inbox), then sync to the warm layer
da kg ingest notes.md
da kg warm

# Query by intent (decision_lookup, repo_context, related_notes, ...)
da kg query --intent decision_lookup "why postgres backend"

# Health and integrity
da kg health
da kg lint
```

A code graph is built from the repo with `da kg build` (incremental
updates via `da kg update`). It powers impact analysis — `da kg impact`
shows the blast radius of changed files, and `da kg changes` detects
change impact in the current diff. Notes can be tied to specific code
symbols so context follows the code:

```bash
# Build the code graph, then cross-reference a note to a symbol
da kg build
da kg link add <note-id> <qualified-name>
da kg link list <note-id>
```

Maintenance keeps the graph honest: `da kg maintain reweave` repairs
broken links, `mark-stale` flags notes past a freshness threshold, and
`compact` archives superseded notes. `da kg sync` pulls and lints the
graph (add `--push` to publish), and `da kg serve` exposes the graph to
agents over MCP. See the [Knowledge Graph](#knowledge-graph) command
table above for the full surface.

## Roadmap

### Agent-as-Operator

The next major evolution: agents run `da` autonomously instead of humans operating it manually. The agent manages config, skills, rules, and workflow state — surfacing only decisions that require human judgment.

Changes follow an **approval gradient**:
- **Auto-apply**: Checkpoints, verification results, plan progress, lessons after corrections
- **Propose-and-apply**: New rules, skills, workflow config changes — human confirms
- **Escalate**: Conflicting rules, stale config affecting production, cross-repo drift

### Workflow State

Based on analysis of real session data across Claude Code, Cursor, and Codex, dot-agents manages six workflow concerns. The foundation ships today via `da workflow`; remaining work deepens coverage:

1. **Resume context** — collect active plan, last handoff, and likely next step (shipped: `workflow orient`, `next`)
2. **Plan & task state** — canonical plan artifacts with dependency-aware phases (shipped: `workflow plan`, `task`, `advance`)
3. **Verification state** — persist test/lint/build results so agents stop rediscovering what's broken (shipped: `workflow verify`, `checkpoint`)
4. **Approvals & tool health** — surface auth expiry, rate-limit risk, environment readiness (roadmap)
5. **Repo preferences** — persist per-repo habits like test commands and review preferences (shipped: `workflow prefs`)
6. **Delegation & handoff** — bounded fan-out with ownership constraints and merge-back summaries (shipped: `workflow fanout`, `merge-back`)

### Multi-Agent Coordination

Drawing from supervisor and swarm-orchestration patterns, dot-agents will support:

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

After pulling changes to `~/.agents/` from git, run `refresh` to re-apply links and configs to all your projects. This ensures your projects stay in sync with your central config. `refresh` is **EXACT** by default — it prunes managed shared-target links that are no longer in the resolved set; pass `--inexact` to keep the additive behavior and leave stale managed links in place.

**Q: How do skills differ from rules?**

- **Rules** (`.mdc` files) are always-active guidelines applied to all projects
- **Skills** (`SKILL.md` files) are on-demand procedures that agents invoke when needed, like deployment checklists or code review workflows

**Q: Can I sync my config across machines?**

Yes! `da sync` helps you manage `~/.agents/` as a git repository. Clone it on another machine and run `da refresh` to set up all your projects.

## Contributing

Contributions welcome! Please open an issue or pull request on GitHub.

## License

[Apache License 2.0](LICENSE) — see also [NOTICE](NOTICE).

Contributions are accepted under the Apache 2.0 terms (§5): unless you state
otherwise, anything you intentionally submit for inclusion is licensed under
Apache 2.0, with its patent grant.

---

Built for developers who use AI coding agents daily. Designed so agents can operate themselves.
