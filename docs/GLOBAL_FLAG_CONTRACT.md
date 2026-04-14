# Global flag contract (dot-agents CLI)

**Status:** Contract text aligned with **§ Inventory (2026-04-13)** in [`.agents/workflow/plans/global-flag-compliance/global-flag-compliance.plan.md`](../.agents/workflow/plans/global-flag-compliance/global-flag-compliance.plan.md).  
**Scope:** Describes **observed** semantics of persistent root flags and known footguns. Implementation and help-text changes are tracked under the global-flag-compliance plan.

## Persistent globals

The root command registers these **persistent** flags (bound to `commands.Flags` in `cmd/dot-agents/main.go`):

| Long | Short | Purpose (intended) |
|------|-------|-------------------|
| `--dry-run` | `-n` | Preview mutations without applying |
| `--force` | `-f` | Override safety prompts / constraints where applicable |
| `--verbose` | `-v` | More diagnostic output |
| `--yes` | `-y` | Non-interactive assent to prompts |
| `--json` | | Machine-readable output where implemented |

Nested subcommands may define **local** flags with the same long name. Locals **shadow** the root binding for that subcommand only—see [Workflow `status` JSON shadowing](#workflow-status-json-shadowing) and [`kg ingest` dry-run](#kg-ingest-dry-run-vs-global-dry-run).

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

Direct children of `dot-agents`. Unless noted, all five globals are parsed.

| Command family | `--json` | `--dry-run` | `--yes` | `--force` | `--verbose` | Notes |
|----------------|----------|-------------|---------|-----------|-------------|-------|
| `init` | unsupported | supported | supported | supported | unsupported | |
| `add` | unsupported | supported | supported | supported | unsupported | |
| `remove` | unsupported | supported | partial | supported | unsupported | `--yes` / `--force` skip removal prompt |
| `refresh` | unsupported | supported | unsupported | unsupported | unsupported | |
| `import` | unsupported | supported | supported | unsupported | unsupported | |
| `status` | **supported** | unsupported | unsupported | unsupported | unsupported | Structured JSON via `runStatus` |
| `doctor` | unsupported | supported | unsupported | unsupported | supported | `--dry-run` suppresses link repair; verbose expands audits |
| `skills` (all subcommands) | unsupported | unsupported | unsupported | unsupported | unsupported | No global flag reads |
| `agents` | unsupported | unsupported | unsupported | unsupported | unsupported | |
| `hooks` | unsupported | unsupported | unsupported | unsupported | unsupported | |
| `workflow` | unsupported | unsupported | unsupported | unsupported | unsupported | Per-subcommand; see [Workflow](#workflow-subcommands) |
| `review` | unsupported | unsupported | unsupported | unsupported | unsupported | |
| `sync` | unsupported | partial | partial | unsupported | unsupported | `sync init` / `commit` / `push` honor `--dry-run`; `push` honors `--yes`; `pull`, `sync status`, `sync log` do not use these globals |
| `explain` | unsupported | unsupported | unsupported | unsupported | unsupported | |
| `install` | unsupported | supported | unsupported | supported | supported | Large surface; `--dry-run` / `--verbose` used throughout `install.go` |
| `kg` | partial | see [KG](#kg-command-family) | unsupported | unsupported | unsupported | Many handlers check `Flags.JSON`; not every leaf is JSON-first |

### Read-only / doc-style families

For **`explain`**, **`review`**, **`skills`**, **`agents`**, and **`hooks`**, all five globals are effectively **unsupported** (no-op) today while still appearing in root help. Scripts must not rely on them for these families.

### `sync` (partial)

- **`--dry-run`:** **partial**—honored on `sync init`, `sync commit`, `sync push`; not honored on `sync pull`, `sync status`, `sync log`.
- **`--yes`:** **partial**—used where documented for push; not a universal non-interactive switch across `sync`.

**Contract note:** **`sync pull`** does not consult `Flags.DryRun`—global `--dry-run` does **not** prevent `git pull`.

---

## Workflow subcommands

Parent `workflow` does not read globals; behavior is per subcommand.

| Subcommand | `--json` | `--dry-run` | `--yes` | Notes |
|------------|----------|-------------|---------|-------|
| `status` | **defect** | unsupported | unsupported | See [Workflow `status` JSON shadowing](#workflow-status-json-shadowing) |
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

Root `--force` and `--verbose` are not shown in the workflow inventory table; treat as **unsupported** for workflow subcommands unless a future inventory row documents otherwise.

### Workflow `status` JSON shadowing

**Issue:** `workflow status` defines a **local** `--json`/`-j` that **shadows** the root persistent `--json`.

- **`commands.Flags.JSON` (root `--json`):** not effective for this subcommand the way operators expect.
- **Observed behavior:** `dot-agents --json workflow orient` emits JSON; `dot-agents --json workflow status` can still print human UI because the subcommand’s local flag wins the binding for `status`.

**Contract for automation:** Do not assume root `--json` produces JSON for `workflow status`. Until fixed, use the **subcommand’s** `--json` if exposed, or treat output as human-only.

**Classification:** **defect** (automation footgun; highest-impact issue called out in the 2026-04-13 inventory).

---

## KG command family

- **`--json`:** **partial** across `kg`—many handlers check `Flags.JSON` for machine output; some leaves are human-first (e.g. parts of `kg setup`, `kg serve`, maintenance mutations).
- **`--dry-run`:** not one global story for all of `kg`.

### `kg ingest` dry-run vs global dry-run

**Issue:** `kg ingest` uses a **local** `--dry-run` tied to ingest behavior, **not** `Flags.DryRun` from the root persistent `-n` / `--dry-run`.

**Contract for automation:**

| Invocation | Drives ingest dry-run? |
|------------|-------------------------|
| `dot-agents --dry-run kg ingest …` | **No** (global does not drive ingest dry-run) |
| `dot-agents kg ingest --dry-run …` | **Yes** (use this for ingest preview) |

Scripts must pass **`kg ingest --dry-run`** when they need ingest dry-run semantics.

---

## Error paths and `--json`

Per inventory: **`RenderCommandError` / usage** paths render errors in human-oriented form. Root **`--json` does not apply** to CLI error rendering in `commands/ux.go`. Automation should assume failures may be non-JSON even when the successful path supports `--json`.

---

## Summary table: cross-cutting contracts

| Topic | Contract |
|-------|----------|
| Duplicate flag names | Prefer removing shadowing locals or documenting subcommand-specific flags; **`workflow status`** + root `--json` is the documented defect |
| `kg ingest` dry-run | **Local** `--dry-run` only; global `-n` does not substitute |
| Read-only families | `explain`, `review`, `skills`, `agents`, `hooks`: globals **unsupported** (no-op) today |
| `sync pull` + `--dry-run` | **Unsupported**—does not block pull |
| Workflow `sweep` | Plan/run semantics via **`--apply`**; globals `--dry-run` / `--yes` are not the primary contract |

---

## Related documents

- [Generated coverage matrix](../generated/GLOBAL_FLAG_COVERAGE.md) — **machine-generated** table of `commands.Flags` reads per CLI command (`go run ./cmd/globalflag-coverage -markdown -o docs/generated/GLOBAL_FLAG_COVERAGE.md`).
- [Global Flag Compliance plan (inventory)](../.agents/workflow/plans/global-flag-compliance/global-flag-compliance.plan.md) — source matrices for this contract
- [Loop Orchestration Spec](./LOOP_ORCHESTRATION_SPEC.md) — delegation bundles, `workflow graph query` + `--json` forwarding to `kg bridge query`
