# dot-agents

**The operational layer for AI coding agents**

One CLI (`da`) to manage configuration and workflow state across Cursor, Claude
Code, Codex, GitHub Copilot, and OpenCode — plus a set of composable primitives
(dynamic loops, bounded workflows, agent teams) you assemble into your own
agentic orchestration, not a closed set of commands.

- **New here?** Paste the [one block](#fastest-start-one-paste) below into your
  agent — it installs `da`, onboards you, and tailors a setup to *your* project.
- **Want the hands-on quickstart?** → [Getting Started](docs/GETTING_STARTED.md)
- **Want the whole command surface?** → [Command reference](docs/COMMANDS.md)
- **New to the model?** → [AI development lifecycle](docs/concepts/ai-dev-lifecycle.md)

---

## Fastest start (one paste)

You don't have to read the whole site first. Paste this into your AI coding agent
(Claude Code, Cursor, Codex, Copilot, OpenCode, Antigravity, …). It installs `da`,
runs the guided **onboarding**, and tailors a setup to *your* project — asking you
questions instead of making you read docs.

```text
Set me up with dot-agents (da). Do this in order, and ask before any destructive change:

1. Install the binary: run `brew tap AGOrcha/tap && brew install da` (or, if Homebrew
   isn't available, the Go / install-script method from
   https://github.com/AGOrcha/dot-agents#installation). Confirm with `da --version`.

2. Run the dot-agents `onboard` skill (skill://onboard). Detect my setup path
   — from-home (a shared/team ~/.agents git URL), from-manifest (this repo already
   commits a real .agentsrc.json), or fresh (empty home) — run exactly that path,
   link my active editor with `da refresh`, then verify with `da status --audit`
   and `da doctor`. If the path is ambiguous, ask me the single disambiguating question.

3. Analyze this project and ask me: what am I building, my stack / app_type, my
   editor/harness, and how much automation I want. Then tailor:
   - use the `pipeline-architect` skill to design my execution_profile
     (app_type → verifier/reviewer sequence + topology + review lenses),
   - use the `skill-architect` skill for any repeatable workflow I describe,
   - create bounded subagents for my recurring delegated tasks.

4. Teach me the da primitives I'll actually use, grounded in what you just set up:
   `da workflow orient | checkpoint | verify | advance`, `da workflow fanout` +
   merge-back for bounded delegation, `da review` for proposals, and `da kg` for
   project memory. For each, tell me WHEN to reach for it in my loop.

Keep it to the minimum decisions. Prefer the conservative/standard option and tell me
what you chose.
```

| Step | What happens | Backed by |
|---|---|---|
| 1. Install | Gets the `da` binary onto your machine | Homebrew / Go / script |
| 2. Onboard | Detects your path, scaffolds `~/.agents`, links your editor, health-checks | `onboard` skill (`from-home` / `from-manifest` / `fresh` → verify) |
| 3. Tailor | Designs your execution profile, skills, and subagents around *your* stack | `pipeline-architect`, `skill-architect` |
| 4. Teach | Shows the primitives + when to use each, in context | `da workflow` / `da review` / `da kg` |

Full walkthrough and what each step does: **[Install & onboard](docs/INSTALL.md)**.

### Prefer to drive it yourself?

```bash
brew tap AGOrcha/tap && brew install da   # or: go install …  (see Install below)
da --version
da init                                   # scaffold ~/.agents/
da add ~/Github/myproject                 # register + link a project
da refresh                                # project canonical config into each platform's shape
da status --audit && da doctor            # verify
```

The [Getting Started](docs/GETTING_STARTED.md) guide covers the three setup paths
in full — **adopt** a shared home config, **install** a repo manifest, or **start
fresh** — then the shared verify step.

---

## Installation

### Homebrew (recommended)

```bash
brew tap AGOrcha/tap && brew install da
```

### Install script

Downloads the prebuilt `da` binary onto your `PATH` — no Go toolchain required:

```bash
# macOS / Linux
curl --proto "=https" --tlsv1.2 -fsSL https://raw.githubusercontent.com/AGOrcha/dot-agents/master/scripts/install.sh | bash
```

```powershell
# Windows PowerShell
irm https://raw.githubusercontent.com/AGOrcha/dot-agents/master/scripts/install.ps1 | iex
```

### Go toolchain (`go install`)

With **Go 1.26.2+** (or `GOTOOLCHAIN=auto` to auto-fetch the toolchain):

```bash
go install github.com/AGOrcha/dot-agents/cmd/da@latest
# or a pinned release:
go install github.com/AGOrcha/dot-agents/cmd/da@<release-version>
```

The `da` binary lands in `$(go env GOBIN)` (falling back to `$(go env GOPATH)/bin`);
ensure that directory is on your `PATH`.

### Manual (from source)

Requires **Go 1.26.2+** (or `GOTOOLCHAIN=auto`).

```bash
git clone https://github.com/AGOrcha/dot-agents ~/.dot-agents
cd ~/.dot-agents
go build -ldflags "-s -w" -o ./bin/da ./cmd/da
export PATH="$HOME/.dot-agents/bin:$PATH"
```

Releases are signed with [Cosign](https://github.com/sigstore/cosign) (keyless, via
Sigstore + GitHub OIDC) — see [`docs/RELEASE_VERIFICATION.md`](docs/RELEASE_VERIFICATION.md)
for the `cosign verify-blob` recipe.

---

## Why dot-agents

Every AI coding agent scatters its own state in its own place — and so does the
work those agents do.

- **Config fragmentation.** Each agent has its own instruction/rule files and
  format (`.cursor/rules/*.mdc`, `CLAUDE.md`, `AGENTS.md`,
  `.github/copilot-instructions.md`, …), so rules get duplicated per repo, can't
  be shared, and drift between machines.
- **Workflow fragmentation.** Autonomous agents already behave like a workflow
  system — resuming across sessions, persisting plans, verifying as they go — but
  each platform stores that state differently. The cost: context amnesia at every
  session start, scattered plans, repeated rediscovery of what's broken, and lost
  handoffs.

**dot-agents** answers both with one source of truth and one operational surface.

### The three pillars (all shipping today)

1. **Config management** — a single source of truth at `~/.agents/`, distributed
   to each project via symlinks/hard links in the shape each platform expects. A
   repo's `.agentsrc.json` can `extends` shared layers (git / local / HTTP / OCI);
   resolved layer SHAs pin in `.agentsrc.lock` so every machine projects the same
   effective config.
   → [Layered Configuration](docs/LAYERED_CONFIG_GUIDE.md) ·
   [Config model](docs/concepts/config-model.md) ·
   [Platform projection](docs/concepts/platform-projection.md) ·
   [Platform matrix](docs/PLATFORM_DIRS_DOCS.md)

2. **Workflow management** — composable primitives, not a fixed toolset. Agents
   orient at session start, persist checkpoints/verification, manage canonical
   plans and tasks, delegate bounded fan-out with write-scope constraints and
   merge-back, and queue changes for human review (`da review`). Per `app_type`, an
   *execution profile* declares the executor : verifier : reviewer topology, an
   ordered verifier sequence, and a review-lens set — the CLI resolves, gates, and
   records the topology while the steps run in the agents/skills you compose on
   top. **Agents operate, humans steer.**
   → [Workflow client commands](docs/WORKFLOW_CLIENT_COMMANDS.md) ·
   [Workflow artifact model](docs/concepts/workflow-artifact-model.md) ·
   [Verification & scoring](docs/concepts/verification-and-scoring.md) ·
   [Diagrams](docs/PROJECT_DIAGRAMS.md)

3. **Knowledge graph (`da kg`)** — a local store of structured project memory
   (typed notes about decisions, entities, sessions) plus a code graph and the
   cross-references between them. It backs the `workflow orient` context an agent
   loads at session start, so it resumes with what was already learned. Shipped: a
   hot-filesystem + warm-SQLite store (Postgres backend included), a code graph via
   the code-review-graph bridge, bridge queries by intent, note→symbol links. The
   KG is evolving toward a pluggable-backend, custom-ontology substrate — see the
   [graph backend adapter contract](.agents/workflow/specs/graph-backend-adapter-contract/design.md)
   and the [scoped knowledge-graphs spec](.agents/workflow/specs/scoped-knowledge-graphs/design.md).

A read-only [Observability dashboard](docs/OBSERVABILITY_DASHBOARD.md) surfaces
workflow runs, iteration scores, and rubric detail with live updates.

---

## Command surface

`da` exposes 26 top-level commands. The **[full command reference](docs/COMMANDS.md)**
documents every family and subcommand; the map below is the entry point, and each
row links to the guide for that area. Run `da <command> --help` for the
authoritative flag set, and see the [Global Flag Contract](docs/GLOBAL_FLAG_CONTRACT.md)
for how `--json` / `--dry-run` / `--yes` / `--force` / `--verbose` behave per command.

| Family | For | Deep dive |
|--------|-----|-----------|
| `init` `add` `remove` `refresh` `import` `install` `status` `doctor` | Set up `~/.agents/` and link projects | [Getting Started](docs/GETTING_STARTED.md) |
| `worktree` (`create` `merge-back`) | Managed sub-branch worktrees for isolated / parallel work | [Command reference](docs/COMMANDS.md#worktrees) |
| `config` (`explain` `sync` `lint` `verify` `relevance`) | Inspect/resolve effective config + layers | [Layered config](docs/LAYERED_CONFIG_GUIDE.md), [relevance](docs/CONFIG_RELEVANCE.md) |
| `skills` `agents` | Author + promote reusable skills and subagents | [Skill↔command integration](docs/SKILL_COMMAND_INTEGRATION.md) |
| `rules` `hooks` `mcp` `settings` | Manage canonical resources under `~/.agents/` | [Resource management](docs/RESOURCE_MANAGEMENT_GUIDE.md), [Hooks](docs/HOOKS.md) |
| `workflow` | Orient, plan, verify, checkpoint, delegate, journal | [Workflow commands](docs/COMMANDS.md#workflow-state) |
| `review` | Approve/reject proposed rule/skill/config changes | [Command reference](docs/COMMANDS.md#workflow-proposals) |
| `kg` | Structured project memory + code graph | [Command reference](docs/COMMANDS.md#knowledge-graph) |
| `observability` | Publish iteration/score telemetry to the dashboard | [Dashboard](docs/OBSERVABILITY_DASHBOARD.md) |
| `eval` `run` `score` | Eval harness, recipes, outcome scoring | [Eval](docs/EVAL_HARNESS.md), [Scoring](docs/SCORE_GUIDE.md) |
| `sync` | Git operations on `~/.agents/` | [Command reference](docs/COMMANDS.md#sync) |

---

## How it works

Canonical config lives once under `~/.agents/`; `da` projects it into each
project in the exact shape that platform reads:

- **Cursor** doesn't follow symlinks for `.cursor/rules/`, so dot-agents uses
  **hard links** (`global--coding-style.mdc` → `~/.agents/rules/global/…`). The
  `global--*` / `{project}--*` prefix shows each file's source.
- **Claude Code** links project rules as symlinks under `.claude/rules/` (one per
  canonical rule); global rules load once from the user-scope `~/.claude/CLAUDE.md`.
  **Codex** links a project-root `AGENTS.md` (the global rule by default, a
  project-scoped override when one exists) plus rendered `.codex/` agents/settings/hooks.
  **Copilot** manages `.github/copilot-instructions.md` (+ `.github/agents/*.agent.md`);
  **OpenCode** gets `AGENTS.md` + `.opencode/agent/*.md`. Each platform also gets its
  rendered agent/settings/hook/MCP files.

The full per-platform matrix (files, formats, link types) lives in
[`docs/PLATFORM_DIRS_DOCS.md`](docs/PLATFORM_DIRS_DOCS.md); the model behind it is
[Platform projection](docs/concepts/platform-projection.md).

## Supported agents

| Agent | Status | Config files |
|-------|--------|--------------|
| **Cursor** | ✅ Full | `.cursor/rules/*.mdc`, `.cursor/settings.json`, `.cursor/mcp.json`, `.cursor/hooks.json`, `.cursorignore`, `.claude/agents/` (subagent compat) |
| **Claude Code** | ✅ Full | `.claude/rules/*.md`, `.claude/settings.local.json`, `.mcp.json`, `.claude/agents/`, `.claude/skills/`, `.agents/skills/`, `~/.claude/CLAUDE.md` (user scope) |
| **Codex** | ✅ Full | `AGENTS.md`, `.codex/config.toml`, `.codex/agents/*.toml` (+ `~/.codex/agents/*.toml` user scope), `.codex/hooks.json` (+ `~/.codex/hooks.json` user scope), `.agents/skills/` |
| **OpenCode** | ⚠️ Basic | `opencode.json`, `.opencode/agent/*.md`, `.agents/skills/` |
| **GitHub Copilot** | ✅ Full | `.github/copilot-instructions.md`, `.github/agents/*.agent.md`, `.agents/skills/`, `.vscode/mcp.json`, `.claude/settings.local.json` (hooks compat), `.github/hooks/*.json` |
| **Antigravity** | 🧪 Probe | `.antigravity/settings.json`, `.antigravity/mcp_config.json`, `.antigravity/hooks.json`, `.antigravity/skills/`, `.antigravity/agents/` |

## Requirements

- **macOS** or **Linux** for the CLI via Homebrew, `scripts/install.sh`, or a local `go build`.
- **Windows:** use `scripts/install.ps1`.
- **Go 1.26.2+** — only for `go install` or building from source (Homebrew and the
  install script ship a prebuilt binary; set `GOTOOLCHAIN=auto` to auto-fetch).
- **git** — for sync features and git-sourced config layers.

---

## Documentation

- **Start here:** [Getting Started](docs/GETTING_STARTED.md) ·
  [Install & onboard](docs/INSTALL.md) ·
  [AI development lifecycle](docs/concepts/ai-dev-lifecycle.md)
- **Reference:** [Command reference](docs/COMMANDS.md) ·
  [Global flag contract](docs/GLOBAL_FLAG_CONTRACT.md) ·
  [Platform matrix](docs/PLATFORM_DIRS_DOCS.md)
- **Config:** [Layered configuration](docs/LAYERED_CONFIG_GUIDE.md) ·
  [Config model](docs/concepts/config-model.md) ·
  [Resource management](docs/RESOURCE_MANAGEMENT_GUIDE.md) ·
  [Hooks](docs/HOOKS.md)
- **Workflow:** [Workflow client commands](docs/WORKFLOW_CLIENT_COMMANDS.md) ·
  [Workflow artifact model](docs/concepts/workflow-artifact-model.md) ·
  [Verification & scoring](docs/concepts/verification-and-scoring.md) ·
  [Scoring guide](docs/SCORE_GUIDE.md) · [Diagrams](docs/PROJECT_DIAGRAMS.md)
- **Advanced:** [Config relevance](docs/CONFIG_RELEVANCE.md) ·
  [Eval harness](docs/EVAL_HARNESS.md) ·
  [Observability dashboard](docs/OBSERVABILITY_DASHBOARD.md) ·
  [Release verification](docs/RELEASE_VERIFICATION.md) ·
  [Architecture decision records](docs/adr/)

## Syncing across machines

`~/.agents/` is designed to be git-tracked:

```bash
da sync init && cd ~/.agents
git remote add origin git@github.com:YOU/agents-config.git
da sync push
# On another machine:
da init --from git@github.com:YOU/agents-config.git   # adopt the shared home
da add ~/Github/myproject                             # rebind each project
```

## Roadmap

The foundation ships today; remaining work deepens it.

- **Agent-as-operator** — agents run `da` autonomously along an approval gradient:
  *auto-apply* (checkpoints, verification, plan progress, lessons),
  *propose-and-apply* (new rules/skills/config — human confirms), *escalate*
  (conflicting rules, stale prod config, cross-repo drift).
- **Workflow depth** — the six workflow concerns are partly shipped (orient/next,
  plans/tasks, verify/checkpoint, prefs, fanout/merge-back); approvals & tool-health
  surfacing remain on the roadmap.
- **Multi-agent coordination** — building on bounded fan-out, context bundles, and
  merge-back: richer anti-drift coordination protocols, graph-driven adaptive
  context engineering, and cross-agent scheduling with conflict detection.

## FAQ

**Why hard links for Cursor?** Cursor's rule system doesn't follow symlinks; hard
links share the actual file content, so edits sync automatically.

**Can I use this with existing projects?** Yes — `da add` won't overwrite existing
files unless you pass `--force`.

**Is my config private?** Yes. Everything stays in `~/.agents/` on your machine;
git sync is optional and to your own repo.

**What's `da refresh` for?** After pulling `~/.agents/` changes, `refresh`
re-applies links to all projects. It's **EXACT** by default (prunes managed links
no longer in the resolved set); pass `--inexact` to keep additive behavior.

**Skills vs rules?** Rules (`.mdc`) are always-active guidelines; skills
(`SKILL.md`) are on-demand procedures agents invoke when needed.

## Contributing

Contributions welcome — open an issue or pull request on GitHub.

## License

[Apache License 2.0](LICENSE) — see also [NOTICE](NOTICE). Contributions are
accepted under the Apache 2.0 terms (§5): unless you state otherwise, anything you
intentionally submit for inclusion is licensed under Apache 2.0, with its patent
grant.

---

Built for developers who use AI coding agents daily. Designed so agents can operate themselves.
