---
title: Global Flag Contract
description: Observed semantics of the dot-agents CLI's persistent root flags and their footguns.
sidebar:
  order: 2
---

# Global flag contract (dot-agents CLI)

**Status:** Contract text aligned with **§ Inventory (2026-04-13)** in [`.agents/history/global-flag-compliance/global-flag-compliance.plan.md`](../.agents/history/global-flag-compliance/global-flag-compliance.plan.md) (archived; formerly under `.agents/workflow/plans/`).  
**Scope:** Describes **observed** semantics of persistent root flags and known footguns. Implementation and help-text changes are tracked under the global-flag-compliance plan.

## Persistent globals

The root command registers these **persistent** flags in `commands/root.go` (`NewRootCommand`), bound to the `commands.GlobalFlags` struct `commands.Flags` declared in `commands/flags.go`. (`cmd/da/main.go` only calls `commands.NewRootCommand()`; it registers no flags itself.)

| Long | Short | Purpose (intended) |
|------|-------|-------------------|
| `--dry-run` | `-n` | Preview mutations without applying |
| `--force` | `-f` | Override safety prompts / constraints where applicable |
| `--verbose` | `-v` | More diagnostic output |
| `--yes` | `-y` | Non-interactive assent to prompts |
| `--json` | | Machine-readable output where implemented |

Nested subcommands may define **local** flags with the same long name. A local would **shadow** the root binding for that subcommand only; the project's direction is to avoid that (read the global instead — e.g. `da config`/`workflow status` now read `Flags.JSON`). Nine locals intentionally **augment** (OR-merge) a global rather than shadow it — they are not violations:

- **`kg ingest --dry-run`** — OR-merged with the global `-n` (`commands/kg/kg.go`); see [`kg ingest` dry-run](#kg-ingest-dry-run-vs-global-dry-run).
- **`workflow commit --dry-run`** — OR-merged with the global `--dry-run` (`commands/workflow/commit_cmd.go:212-216`): `effectiveDryRun = dryRun || deps.Flags.DryRun()`.
- **`workflow fold-back create --dry-run`** — OR-merged with the global `-n` (`commands/workflow/delegation.go:757-758`): `local || safeDryRun()`.
- **`hook-sentinel --json`** — OR-merged with the global `--json` (`commands/workflow/hook_sentinel.go:858,1050`): emits the sentinel as JSON if either the local `--json` or the global `--json` is set.
- **`workflow task rename --dry-run` / `--json`** — both OR-merged with the globals (`commands/workflow/cmd.go:480,486`; the runner reads `in.JSON || deps.Flags.JSON()` at `commands/workflow/plan_task.go:1562`).
- **`workflow task supersede --dry-run` / `--json`** — both OR-merged (`commands/workflow/cmd.go:505,511`; `asJSON || deps.Flags.JSON()` at `commands/workflow/plan_task.go:3823`).
- **`workflow state-ref reconcile --dry-run` / `--json`** — both OR-merged (`commands/workflow/cmd.go:1221,1226`; `asJSON || deps.Flags.JSON()` at `commands/workflow/state_ref_reconcile.go:283`).
- **`workflow start-task --dry-run`** — OR-merged with the global `-n` (`commands/workflow/start_task.go:99-101`): `local || safeDryRun()`.
- **`workflow pipeline emit --dry-run`** — OR-merged with the global `-n` (`commands/workflow/pipeline_emit.go:94-96`): `local || safeDryRun()`.

## Legend

This document uses the same symbols as the plan inventory:

| Label | Meaning |
|-------|---------|
| **supported** | Implementation reads `commands.Flags.<field>` for this command path and behavior matches the flag’s intent |
| **unsupported** | Flag is accepted (parses) but **not read** for this path—**silent no-op** today unless Cobra rejects the invocation |
| **partial** | Honored on some code paths or only in combination with other flags |
| **local** | Subcommand defines its own flag with the same long name; semantics may differ from globals |
| **defect** | Known incorrect behavior vs. advertised globals—automation must not assume root flag semantics |

**Target direction (plan):** for combinations that are **unsupported**, prefer **explicit rejection** or **narrowed help** over silent no-ops. This document still describes **current** behavior unless a row says *target*.

---

## Top-level command families

Direct children of `da`. Unless noted, all five globals are parsed.

| Command family | `--json` | `--dry-run` | `--yes` | `--force` | `--verbose` | Notes |
|----------------|----------|-------------|---------|-----------|-------------|-------|
| `init` | unsupported | supported | supported | supported | unsupported | |
| `add` | unsupported | supported | supported | supported | unsupported | |
| `config` | supported | partial | unsupported | unsupported | unsupported | Subtree is `explain`/`sync`/`lint`/`verify`/`relevance`/`migrate`; **not** read-only — `config sync` is mutating (rewrites the lock). Honors the global `--json` like every other command |
| `remove` | unsupported | supported | partial | supported | unsupported | `--yes` / `--force` skip removal prompt |
| `refresh` | unsupported | supported | unsupported | unsupported | unsupported | |
| `import` | unsupported | supported | supported | unsupported | unsupported | |
| `status` | **supported** | unsupported | unsupported | unsupported | unsupported | Structured JSON via `runStatus` |
| `doctor` | unsupported | unsupported | unsupported | unsupported | supported | Read-only by design (`doctor.go` `Long` help: "never repairs"; §7A.6) — reads only `Flags.Verbose`, never `Flags.DryRun`, so `--dry-run` parses but has no repair action to suppress. `-v` expands per-project audits |
| `skills` (all subcommands) | unsupported | unsupported | unsupported | unsupported | unsupported | No global flag reads |
| `agents` | unsupported | unsupported | partial | unsupported | unsupported | `agents.GlobalFlags` (`commands/agents/deps.go`) has only a `Yes` field; `commands/agents.go` wires only `Flags.Yes`. `agents remove --purge` reads `deps.Flags.Yes` to skip the delete-confirmation prompt (`commands/agents/remove.go:167`); `--dry-run`/`--force` parse but are never read by any agents subcommand — a known divergence from the sibling `remove` commands, tracked as a code gap. (`agents promote --force` is an unrelated distinct-semantic local, see below.) |
| `hooks` | unsupported | partial | partial | partial | unsupported | `hooks remove` honors `--dry-run`/`--yes`/`--force`; other subcommands unsupported |
| `mcp` | unsupported | partial | partial | partial | unsupported | `mcp remove` honors `--dry-run`/`--yes`/`--force` |
| `rules` | unsupported | partial | partial | partial | unsupported | `rules remove` honors `--dry-run`/`--yes`/`--force` |
| `settings` | unsupported | partial | partial | partial | unsupported | `settings remove` honors `--dry-run`/`--yes`/`--force` |
| `score` | supported | unsupported | unsupported | unsupported | unsupported | Reads `Flags.JSON` (`commands/score.go`); locals `--no-write`/`--recompute` |
| `session` | unsupported | unsupported | unsupported | unsupported | unsupported | `session stats` renders human-only platform usage; reads no globals |
| `workflow` | unsupported | unsupported | unsupported | unsupported | unsupported | Per-subcommand; see [Workflow](#workflow-subcommands) |
| `review` | partial | unsupported | unsupported | unsupported | unsupported | Admin subtree is JSON-capable: `review users` (add/list/remove/set-role) and `review audit` (tail/verify/repair/prune) emit JSON via the global `--json` (`commands/review_admin.go:460-733`). The proposal surface (`review`/`show`/`approve`/`reject`, `commands/review.go`) reads no globals |
| `sync` | unsupported | partial | partial | partial | unsupported | `init` / `commit` / `push` honor `--dry-run`; `pull` rejects `--dry-run` (errors); `push` honors both `--yes` and `--force` to skip its confirmation prompt (`commands/sync/push.go:49`); `pull` (refresh prompt) honors `--yes` only (`commands/sync/helpers.go:42`); `status` / `log` do not use these globals |
| `explain` | unsupported | unsupported | unsupported | unsupported | unsupported | |
| `install` | unsupported | supported | unsupported | supported | supported | Large surface; `--dry-run` / `--verbose` used throughout `install.go` |
| `kg` | partial | see [KG](#kg-command-family) | unsupported | unsupported | unsupported | Many handlers check `Flags.JSON`; not every leaf is JSON-first |
| `eval` | supported | partial | unsupported | unsupported | unsupported | `gen`/`run`/`ls` read the global `--json`; `eval run` also reads the global `--dry-run` (resolve + preview, no agent). Reads live in `commands/root.go` handlers |
| `run` | unsupported | unsupported | unsupported | unsupported | unsupported | Recipe dispatcher (`da run <file>`); dispatches inner `da` commands, reads no globals itself |

### Read-only / doc-style families

For **`explain`** and **`skills`**, all five globals are effectively
**unsupported** (no-op) today while still appearing in root help. **`review`** is
unsupported on its proposal surface (`review`/`show`/`approve`/`reject`), but its
**admin subtree** (`review users …`, `review audit …`) reads the global `--json`.
**`hooks`**, **`mcp`**, **`rules`**, and **`settings`** are unsupported *except*
their `remove` subcommands, which honor `--dry-run`/`--yes`/`--force`. **`agents`**
is the lone exception: `agents.GlobalFlags` has no `DryRun`/`Force` field, so only
`agents remove --purge` honors `--yes` — its global `--dry-run`/`--force` parse but
are never read (a known divergence from the sibling `remove` commands, tracked as a
code gap). Scripts must not rely on globals for the read-only paths.

### `sync` (partial)

- **`--dry-run`:** **partial**—honored on `sync init`, `sync commit`, `sync push`; not honored on `sync status`, `sync log`. **`sync pull` now explicitly rejects `--dry-run`** with an error (`commands/sync/pull.go`) rather than silently pulling.
- **`--yes`:** **partial**—honored for push, and `sync pull` consults `Flags.Yes` to auto-confirm its post-pull refresh prompt (`commands/sync/helpers.go`); not a universal non-interactive switch across `sync`.
- **`--force`:** **partial**—`sync push` reads `Flags.Force` (alongside `Flags.Yes`) to skip its confirmation prompt (`commands/sync/push.go:49`); not read by `init`, `commit`, `status`, or `log`.

---

## Workflow subcommands

Parent `workflow` does not read globals; behavior is per subcommand.

| Subcommand | `--json` | `--dry-run` | `--yes` | Notes |
|------------|----------|-------------|---------|-------|
| `status` | supported | unsupported | unsupported | Reads the global `--json` via `deps.Flags.JSON()` (`commands/workflow/state.go`); the former local-`--json` shadow was fixed |
| `orient` | supported | unsupported | unsupported | |
| `checkpoint` | unsupported | unsupported | unsupported | Writes files; no JSON path |
| `log` | unsupported | unsupported | unsupported | |
| `plan` (list) | supported | unsupported | unsupported | |
| `plan show` | supported | unsupported | unsupported | |
| `plan graph` | supported | unsupported | unsupported | |
| `plan create` / `plan update` | unsupported | unsupported | unsupported | |
| `task add` / `task update` | unsupported | unsupported | unsupported | |
| `tasks` | supported | unsupported | unsupported | |
| `slices` | supported | unsupported | unsupported | |
| `next` | supported | unsupported | unsupported | |
| `advance` | unsupported | unsupported | unsupported | |
| `health` | supported | unsupported | unsupported | |
| `verify record` | unsupported | unsupported | unsupported | |
| `verify log` | supported | unsupported | unsupported | |
| `prefs` | supported | unsupported | unsupported | |
| `prefs set-local` / `set-shared` | unsupported | unsupported | unsupported | |
| `graph query` / `graph health` | supported | unsupported | unsupported | Bridge / KG paths forward JSON where applicable |
| `fanout` | unsupported | unsupported | unsupported | |
| `merge-back` | unsupported | unsupported | unsupported | |
| `fold-back create` / `fold-back list` | supported | unsupported | unsupported | |
| `delegation closeout` | supported | unsupported | unsupported | |
| `drift` | supported | unsupported | unsupported | |
| `sweep` | unsupported | unsupported | partial | Uses **`--apply`** for real runs (default is dry plan). Globals `--dry-run` / `--yes` are not wired; `--yes` skips per-action prompts when `sweep --apply` runs |
| `journal` (snapshot/recover/show/prune/append) | supported | partial | unsupported | Session-handoff journal; subcommands read the global `--json` (`commands/workflow/journal.go:217,268,424,481`); `prune` honors the global `-n` |

Root `--force` and `--verbose` are not shown in the workflow inventory table; treat as **unsupported** for workflow subcommands unless a future inventory row documents otherwise.

This table is a **partial inventory** as of the 0.5.0 cut. Newer subcommands not yet
individually rowed — `eligible`, `slots`, `complete`, `app-types`, `resolve-prompt`,
`contract` (create/list), `delegation gate`, `bundle stages`, `commit`, `close-task`,
`start-task`, `hook-sentinel`, `hook-outcome`, `archive-orphans`, `plan archive`,
`plan schedule`, `plan derive-scope`, `plan check-scope`, `fold-back update` — inherit
the same root-persistent globals; confirm a given leaf's support by its
`deps.Flags.JSON()` / `deps.Flags.DryRun()` reads rather than assuming from this
table. `plan archive` additionally honors the global `--dry-run` for a genuine no-op
preview (threaded as `deps.Flags.DryRun()` at `commands/workflow/cmd.go:318` into
`runWorkflowPlanArchive`, `commands/workflow/plan_task.go:3221`) on top of its local
`--force` (see the distinct-semantic-locals summary row below). A full per-row
re-inventory is tracked for a later pass.

### Workflow `status` JSON shadowing — RESOLVED

The former defect (a local `--json`/`-j` on `workflow status` shadowing the root
persistent `--json`) has been **fixed**: `workflow status` now reads the global
`Flags.JSON` via `deps.Flags.JSON()` (`commands/workflow/state.go`), like `orient`.
`da --json workflow status` emits JSON. This section is retained as the historical
record of the 2026-04-13 inventory's highest-impact issue.

---

## KG command family

- **`--json`:** **partial** across `kg`—many handlers check `Flags.JSON` for machine output; some leaves are human-first (e.g. parts of `kg setup`, `kg serve`, maintenance mutations).
- **`--dry-run`:** not one global story for all of `kg`.

### `kg ingest` dry-run vs global dry-run

`kg ingest` keeps a **local** `--dry-run`, but it is now **OR-merged** with the root
global `-n`/`--dry-run` (`dryRun: deps.Flags.DryRun || localDryRun`,
`commands/kg/kg.go`), so either form drives ingest dry-run:

| Invocation | Drives ingest dry-run? |
|------------|-------------------------|
| `da --dry-run kg ingest …` | **Yes** (global now honored) |
| `da kg ingest --dry-run …` | **Yes** |

The local flag remains for explicitness; it no longer shadows the global.

---

## Error paths and `--json`

Per inventory: **`RenderCommandError` / usage** paths render errors in human-oriented form. Root **`--json` does not apply** to CLI error rendering in `commands/ux.go`. Automation should assume failures may be non-JSON even when the successful path supports `--json`.

---

## Summary table: cross-cutting contracts

| Topic | Contract |
|-------|----------|
| Duplicate flag names | Prefer reading the global over a shadowing local; the historical `workflow status` + `da config` shadows are both **fixed** (now read `Flags.JSON`) |
| `kg ingest` dry-run | Local `--dry-run` **OR-merged** with global `-n`; either form drives ingest dry-run |
| OR-merge augment-locals | The nine locals listed under [Persistent globals](#persistent-globals) — `kg ingest --dry-run`, `workflow commit --dry-run`, `workflow fold-back create --dry-run`, `hook-sentinel --json`, `workflow task rename`/`task supersede`/`state-ref reconcile` (`--dry-run`/`--json`), `workflow start-task --dry-run`, `workflow pipeline emit --dry-run` — each OR-merge their global rather than shadow it (not violations) |
| Distinct-semantic locals | Some locals reuse a global's long name with a **different, command-specific** meaning: `agents promote --force` (`commands/agents/cmd.go:86`, replace an existing real directory at the canonical path), `workflow plan archive --force` (`commands/workflow/cmd.go:318`, skip the completed-status guard), `workflow contract create --force` (`commands/workflow/contract.go:296`, overwrite an existing delegation contract), and `workflow app-types --verbose` (`commands/workflow/cmd.go:1189`, show per-`app_type` source + recommendation detail) |
| Read-only families | `explain`, `skills`: globals **unsupported** (no-op). `review` is unsupported on its proposal surface but its **admin subtree** (`review users`/`review audit`) reads the global `--json`. `hooks`/`mcp`/`rules`/`settings` are unsupported except their `remove` subcommands, which honor `--yes`/`--dry-run`/`--force`. `agents` honors `--yes` only (via `agents remove --purge`); its global `--dry-run`/`--force` parse but are never read (code gap) |
| `sync pull` + `--dry-run` | **Rejected** (errors) — no longer a silent pull; `--yes` auto-confirms the post-pull refresh |
| Workflow `sweep` | Plan/run semantics via **`--apply`**; globals `--dry-run` / `--yes` are not the primary contract |

---

## Related documents

- **Generated coverage matrix** — `docs/generated/GLOBAL_FLAG_COVERAGE.md` is **not checked in**; it is generated on demand by the `cmd/globalflag-coverage` tool, which emits a per-command `commands.Flags` reads matrix. Generate it with `go run ./cmd/globalflag-coverage -markdown -o docs/generated/GLOBAL_FLAG_COVERAGE.md`.
- [Global Flag Compliance plan (inventory)](../.agents/history/global-flag-compliance/global-flag-compliance.plan.md) — source matrices for this contract
- [Graph bridge contract](../.agents/workflow/specs/graph-bridge-contract/design.md) and [workflow parallel orchestration](../.agents/workflow/specs/workflow-parallel-orchestration/design.md) — delegation bundles, `workflow graph query` + `--json` forwarding to `kg bridge query`
