---
title: Command Reference
description: The full da (dot-agents) command surface — every top-level command family and subcommand, with links to the deeper guide for each area.
sidebar:
  order: 1
---

# Command reference

`da` exposes 26 top-level commands (excluding `help` and `completion`). This page
is the exhaustive surface; the [README](../README.md) carries the high-level map,
and each section below links to the guide that explains the area in depth. Run
`da <command> --help` for the authoritative flag set on any command.

> Global flags (`--json`, `--dry-run`, `--yes`, `--force`, `--verbose`) are
> persistent but not uniformly honored — see the
> [Global Flag Contract](GLOBAL_FLAG_CONTRACT.md) for per-command support.

> `--help` is the authority on flag semantics, not this page: every closed-set
> flag lists its allowed values inline, and every command on the agent loop's
> critical path carries worked examples. See the
> [CLI Help Conventions](CLI_HELP_CONVENTIONS.md) for how that text is authored.

## Project Management

| Command | Description |
|---------|-------------|
| `init` | Initialize `~/.agents/` directory structure (`--from <git-url>` adopts a shared home) |
| `add <path>` | Add a project to da management |
| `remove <project>` | Remove a project from da management |
| `refresh [project]` | Refresh managed setup from `~/.agents/` for the **current project** (the one containing the working directory); name a project to refresh that one, or pass `--all` for every project registered on this machine. Auto-enables newly-installed editors and updates their versions. EXACT by default — prunes managed shared-target links no longer in the resolved set; pass `--inexact` to keep the additive behavior |
| `import [project]` | Import configs from project/global scope into `~/.agents/` |
| `install` | Set up project from `.agentsrc.json` manifest (`--generate` to create one). EXACT by default — prunes managed shared-target links no longer in the resolved set; pass `--inexact` to keep additive behavior, `--strict` to fail on a missing declared resource |
| `status` | Show managed projects and link health; for effective-config detail run `da config explain` (use `--audit` for details) |
| `doctor` | Check installations, validate links, detect issues (read-only — reports problems and the command to fix them; never repairs) |

See [Getting Started](GETTING_STARTED.md) for the three setup paths (adopt / install / fresh).

### Refresh scope

`da refresh` mutates every managed project it visits, so its reach is explicit in
the invocation:

| Invocation | Projects refreshed |
|---|---|
| `da refresh` | The current project — the managed project containing the working directory (a subdirectory or worktree beneath it counts) |
| `da refresh <project>` | That one registered project, from anywhere |
| `da refresh --all` | Every project registered on this machine, in deterministic (name) order |

Run outside any managed project without `--all`, refresh refuses and tells you
how to proceed rather than guessing. `--all` cannot be combined with a project
name. The internal refreshes triggered by `da sync pull` and `da review approve`
are current-project-scoped for the same reason; run `da refresh --all` yourself
when you want the pulled or approved change projected everywhere.

### Managed `.gitignore` block

`install` and `refresh` both maintain a delimited, dot-agents-owned block in the
consuming repo's `.gitignore` so the files they project (platform links,
generated configs, and the `*.dot-agents-backup` sidecars install writes when it
displaces a pre-existing user file) do not show up as untracked noise:

```gitignore
# >>> dot-agents managed (project outputs) >>>
*.dot-agents-backup
.agentsrc.local.json
.claude/
.codex/
.mcp.json
AGENTS.md
# <<< dot-agents managed (project outputs) <<<
```

The block's contents come from the enabled platforms themselves, so it lists
exactly what da projects into *that* repo. It is regenerated (not appended) on
every run, sorted and de-duplicated, and everything outside the markers is
preserved verbatim — so re-running is byte-stable and the file is safe to
commit. `.agentsrc.json` and `.agentsrc.lock` are deliberately never ignored:
they are the committed resolved-state contract, the `uv.lock` analog.

**Only untracked files are affected.** `.gitignore` has no effect on a path git
already tracks, so a repo that intentionally commits one of these projections —
a team that keeps `AGENTS.md` in version control, say — is unchanged: the
tracked copy stays tracked and keeps showing up in diffs. That is the intended
behavior, and it is why there is no per-path policy knob.

To opt out entirely, set `"gitignore_projections": false` in `.agentsrc.json`.
The next `install`/`refresh` removes the block (leaving your own ignore rules
untouched) and stops maintaining it. Omitting the key means enabled.

## Worktrees

`da worktree` creates and merges back managed sub-branch worktrees stacked on a
parent branch — for isolated delegation or parallel work. `create` records the
parent-branch tip as an immutable base ref; `merge-back` integrates using that
recorded base (never re-deriving it with `git merge-base`), so a parent that
advanced or was force-pushed is caught loudly instead of silently rebasing onto
the wrong commit.

| Command | Description |
|---------|-------------|
| `worktree create --name <n> --path <p> --base-branch <b>` | Fork a sub-branch worktree from a parent branch and record the base-branch tip as its immutable base ref |
| `worktree merge-back --name <n> --onto <b>` | Integrate the sub-branch into its parent using the recorded base ref (never `git merge-base`), failing loudly if the parent moved |

## Configuration

| Command | Description |
|---------|-------------|
| `config explain [field]` | Show the effective `.agentsrc.json` value of a field and which layer set it (`--all`, `--flags`, `--json`) |
| `config sync` | Re-fetch every declared layer regardless of TTL, re-resolve, and rewrite the `units` section + `inputs_digest` of `.agentsrc.lock` — the uv `--upgrade` analog (`--layer source-id:path`, `--json`) |
| `config lint` | Validate the repo-local `.agentsrc.json` and each `extends` layer against the AgentsRC layer schema; non-zero exit if invalid (`--json`) |
| `config verify` | Offline setup contract check — manifest parses, declared local source layers exist, integrations ready, and remote `extends` layers are cached at the lockfile's SHA (`--json`; non-zero exit on failure) |
| `config relevance` | Resolve a task's execution profile (units, topology, lenses) by `app_type` (`--filter`, `--app-type`, `--task`, `--stage`, `--recompute`, `--json`; see [CONFIG_RELEVANCE.md](CONFIG_RELEVANCE.md)) |

### Layered config & the lockfile (`.agentsrc.lock`)

A `.agentsrc.json` manifest may `extends` one or more config layers sourced from
git, local paths, HTTP, or OCI (any source may supply a layer; the kind is set by the
pulled blob's media type), declared as `source:path@version`. When the layers are
resolved, the resolved layer SHAs are pinned in `.agentsrc.lock` so every machine
projects the same effective config.

See the [**Layered Configuration guide**](LAYERED_CONFIG_GUIDE.md) for the full
model — the manifest, `extends` layers, resolution and precedence, the lockfile, and a
worked walkthrough of `da config sync` / `explain` / `lint`.

- `da config sync` re-checks every declared layer upstream (ignoring TTL),
  re-resolves the stack, and rewrites the `units` section of `.agentsrc.lock`.
  This is the explicit upstream re-check — the uv `--upgrade` analog.
- `da refresh` and `da install` re-project the locked config locally and only
  re-resolve when the lock is stale, so routine relinking never reaches the
  network for an unchanged stack.
- `da config cache prune` garbage-collects the shared config cache
  (`~/.agents/cache/config`, which holds every fetched layer and
  source-qualified prompt file). An entry is prunable when no registered
  project's lockfile references its digest. It lists by default; `--apply`
  deletes.

## Skills & Agents

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

Skills are on-demand procedure documents (`SKILL.md` + optional `scripts/` /
`references/`); subagents are directory-based definitions (`AGENT.md` + optional
helpers). See [SKILL_COMMAND_INTEGRATION.md](SKILL_COMMAND_INTEGRATION.md).

Project-scoped skills/agents resolve the target project through the synced
`config.json` (v2: keyed by `repo_id`) plus the machine-local
`~/.agents/local/bindings.json`, then update that project's `.agentsrc.json`;
`global` writes to the shared `~/.agents/` store. Scope is the positional
`global` / project-name argument (there is no `--scope` flag). See
[Config model](./concepts/config-model.md).

## Canonical Resource Inspection

These inspect and manage canonical files under `~/.agents/`. Each supports
`list [scope]`, `show <scope> <name>`, and `remove <scope> <name>`.

| Command | Description |
|---------|-------------|
| `rules` | Inspect and manage canonical `~/.agents/rules` files |
| `hooks` | Inspect and manage canonical `~/.agents/hooks` bundles |
| `mcp` | Inspect and manage canonical `~/.agents/mcp` config files |
| `settings` | Inspect and manage canonical `~/.agents/settings` files |

See the [**Managing resources guide**](RESOURCE_MANAGEMENT_GUIDE.md) for the
canonical model, per-platform emit, and the `list`/`show`/`remove` surface (hooks have their
own [Hooks guide](HOOKS.md)); the [Resource Command Contract](RESOURCE_COMMAND_CONTRACT.md)
specifies the shared verb behavior.

## Workflow Proposals

| Command | Description |
|---------|-------------|
| `review` | Review pending workflow proposals |
| `review show <id>` | Show a pending proposal |
| `review approve <id>` | Approve and apply a pending proposal |
| `review reject <id>` | Reject a pending proposal |
| `review users <add\|list\|remove\|set-role>` | Admin-only RBAC for the review surface; `add` mints a bearer token printed once, every mutation writes a hash-chained audit record (`--role`, `--token`, `--users-file`) |
| `review audit <tail\|verify\|repair\|prune>` | Inspect and attest the append-only, hash-chained review audit log; `verify` needs no token and exits non-zero on an integrity break (usable as a CI gate) |

## Workflow State

`da workflow` captures repository-local workflow state — canonical plans,
checkpoints, verification logs, preferences, fanout artifacts, and bridge
queries — so humans and agents can resume work safely. See
[WORKFLOW_CLIENT_COMMANDS.md](WORKFLOW_CLIENT_COMMANDS.md) and the workflow-engine
diagram in [PROJECT_DIAGRAMS.md](PROJECT_DIAGRAMS.md) §5.

| Command | Description |
|---------|-------------|
| `workflow status` | Show workflow state for the current project |
| `workflow orient` | Render session orient context for the current project |
| `workflow next` | Suggest the next actionable canonical task |
| `workflow eligible` | List all unblocked eligible tasks across active plans with conflict detection |
| `workflow slots` | Show the slot ledger (occupied / awaiting-owner / blocked) across active plans (`--plan`) |
| `workflow complete --plan <id>` | Probe scoped plan-completion state |
| `workflow health` | Show workflow health snapshot |
| `workflow app-types` | List available app_type values for the current repo |
| `workflow resolve-prompt --kind <k> --slug <s>` | Resolve a stage profile's composed (base-first, scope-resolved) `prompt_files` |

### Plans & Tasks

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

### Persist & Verify

| Command | Description |
|---------|-------------|
| `workflow checkpoint --message <m>` | Write a checkpoint for the current project |
| `workflow log` | Show recent checkpoint log entries |
| `workflow verify record` | Record a verification run (test/lint/build/review) |
| `workflow verify log` | Show verification log entries |
| `workflow prefs` | Show resolved workflow preferences (`set-local`, `set-shared`) |
| `workflow graph query` | Query knowledge graph context by bridge intent (`graph health`) |

### Session-handoff journal

`da workflow journal` is an append-only, crash-survivable event log plus a
deterministic live-state snapshot, kept off the git tree under the XDG state
directory — so a session resumed after a compaction or crash re-injects state
from durable file state, re-verified against current reality, instead of
re-grounding from scratch.

| Command | Description |
|---------|-------------|
| `workflow journal snapshot` | Capture the deterministic live-state snapshot for the current project |
| `workflow journal recover` | Build the verified recovery view (snapshot + replay, re-verified against reality) |
| `workflow journal show` | Show the current snapshot and recent journal events (`--limit`, `--all`) |
| `workflow journal prune` | Drop journal events beyond a bounded retention (safe, atomic; `--keep`) |
| `workflow journal append` | Low-level: append one event to the journal (reasoned-overlay / testing) |

### Delegation

| Command | Description |
|---------|-------------|
| `workflow contract create --task <id>` | Materialize a delegation contract for direct orchestrator work (`list` subcommand; `--plan`, `--mode`, `--write-scope`) |
| `workflow fanout --plan <id> --task <id>` | Delegate a task to a sub-agent with a bounded write scope |
| `workflow merge-back --task <id> --summary <s>` | Record a sub-agent's completed work as a merge-back artifact |
| `workflow delegation closeout` | Archive merge-back artifacts and reconcile canonical task state |
| `workflow delegation gate --task <id>` | Evaluate task-local review evidence into a parent-gate outcome |
| `workflow fold-back create` | Route loop observations into plan artifacts or proposals (`update`, `list`) |
| `workflow bundle stages <path>` | Expand a delegation bundle into the ordered stage list |

### Drift (cross-repo, read-only by default)

| Command | Description |
|---------|-------------|
| `workflow drift` | Detect workflow drift across managed repos (read-only) |
| `workflow sweep` | Plan and optionally apply fixes for workflow drift (`--apply`) |

### Automation / internal commands

These are driven by skills and lifecycle hooks (e.g. iteration-close, loop-worker), not run by
hand. They are listed for completeness; reach for the end-user commands above for day-to-day work.

| Command | Description |
|---------|-------------|
| `workflow hook-sentinel` | Write/read/clear hook sentinels declaring per-skill stop-gate context (`write`/`read`/`clear`) |
| `workflow hook-outcome write` | Append a hook gate outcome record to the active iteration's `iter-N.hook-outcomes.yaml` sidecar |
| `workflow commit` | Stage and commit workflow-state changes (managed roots + declared session paths) |
| `workflow archive-orphans` | Sweep stale active merge-back/delegation artifacts after a plan archive |
| `kg lockfile show` | Inspect adapter lockfile state (`reconcile` subcommand runs fail-closed view reconciliation) |

A task may also carry the parameterized status `blocked-on:<ref>` (set via
`workflow advance --status blocked-on:<ref>`); it is a task *state*, not a standalone command,
and frees its parallelism slot in the `workflow slots` ledger until the blocker auto-resolves.

## Observability

`da observability` publishes workflow iteration/score telemetry to a remote
dashboard backend (the reference deployment is `obs.agorcha.dev`) and inspects its
reachability. The endpoint + credential come from the `.agentsrc.json`
`observability` block; events queue crash-safe in `.agents/active/obs-outbox/` and
drain idempotently, so a publish failure never changes a local command's result.

| Command | Description |
|---------|-------------|
| `observability status` | Report the configured endpoint's reachability + authentication (resolves the credential-ref; refuses a non-HTTPS endpoint before resolving) |
| `observability sync` | Drain the observability outbox to the endpoint (server dedupes); `--full` replays local `.agents/history/` to rebuild the remote read model |
| `observability login --from-env` | Store the service-token credential (`CF_OBS_CLIENT_ID` / `CF_OBS_CLIENT_SECRET`) in the credential store |

Publishing is wired best-effort into `workflow checkpoint` / `verify record`. See
[cf-access-bootstrap.md](cf-access-bootstrap.md) for the Cloudflare Access apps and
the [Observability Dashboard guide](OBSERVABILITY_DASHBOARD.md) for run modes.

## Knowledge Graph

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

### Maintenance

| Command | Description |
|---------|-------------|
| `kg maintain reweave` | Repair broken links and add missing source_ref links |
| `kg maintain mark-stale` | Mark notes not updated beyond threshold as stale (`--days`) |
| `kg maintain compact` | Archive superseded and archived notes |
| `kg sync` | Sync graph via git pull + lint (`--push` to push) |
| `kg warm` | Sync hot filesystem notes into the warm SQLite layer (`stats` subcommand) |

### Bridge

| Command | Description |
|---------|-------------|
| `kg bridge query --intent <i>` | Execute a bridge intent query |
| `kg bridge health` | Show adapter availability and health |
| `kg bridge mapping` | Show bridge intent to KG intent mapping |

### Code Graph

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

## Sync

`da sync` wraps git operations on `~/.agents/`.

| Command | Description |
|---------|-------------|
| `sync init` | Initialize git repo in `~/.agents/` |
| `sync status` | Show git status |
| `sync commit` | Commit all changes |
| `sync push` | Push to remote |
| `sync pull` | Pull from remote |
| `sync log` | Show recent commit log |

## Evaluation & Recipes

`da eval` drives the R4 agent-evaluation harness end to end — synthesise a
reproducible TaskSpec from the knowledge graph, run it in an isolated sandbox,
and score the outcome against the same rubric `da score` uses (see the
[**Eval harness guide**](EVAL_HARNESS.md)). `da run` executes a *recipe*: a
line-oriented sequence of `da` commands.

| Command | Description |
|---------|-------------|
| `eval gen` | Generate a reproducible eval TaskSpec from the knowledge graph (`--language go\|python\|typescript`, `--difficulty`, `--template`, `--out`) |
| `eval run` | Run one eval task end-to-end in an isolated sandbox and score the outcome (`--agent claude\|codex\|copilot`, `--task`, `--language`, `--repo-dir`) |
| `eval ls` | List persisted eval runs under `.agents/eval/runs/` (`--repo-dir`) |
| `run <file>` | Execute a da recipe file — dispatched in order, fail-fast, with `$VAR`/`${VAR}` env-substitution and no shell invoked (shebang-friendly via `#!/usr/bin/env -S da run`) |

## Utilities

| Command | Description |
|---------|-------------|
| `explain [topic]` | Explain da concepts |
| `score run` | Compute and query agent-run outcome scores (`iteration <N>`, `session <id>` subcommands; see the [**Scoring guide**](SCORE_GUIDE.md)) |
| `session stats` | Show usage statistics from each installed AI platform |
| `--help` | Show help for any command |
| `--version` | Show version |
