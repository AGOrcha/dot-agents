# Global Flag Compliance Plan

Status: Active

## Problem

The root command exposes persistent global flags:

- `--json`
- `--dry-run`
- `--yes`
- `--force`
- `--verbose`

In practice, support is uneven across the command tree. Some commands implement a flag fully, some partially, and some inherit the flag in `--help` output without honoring it. The CI review on 2026-04-12 exposed one concrete example: `dot-agents status --json` returned ANSI text instead of JSON while advertising global JSON support.

That inconsistency creates two separate problems:

- operator confusion, because `--help` implies uniform support that does not exist
- brittle automation, because scripts cannot rely on root-level flags behaving consistently

## Immediate Fixes Landed

- `status --json` now emits structured JSON instead of human-formatted output
- `.github/workflows/test.yml` was updated to stop asserting stale `USAGE` text and to stop checking the obsolete `sync status` error string

## Follow-Up Scope

1. Inventory all commands and nested subcommands against each global flag.
2. Decide which flags are intended to be universally supported versus only meaningful on specific commands.
3. For unsupported combinations, prefer explicit rejection or documented non-support over silent no-op behavior.
4. Add regression tests for both supported and unsupported cases.

## Initial Risk Areas

- top-level read-only commands that inherit `--json` without a JSON renderer
- nested command trees where root persistent flags leak into help text but command implementations bypass shared UX paths
- mutation commands that honor `--yes` or `--dry-run` inconsistently between direct and delegated helper paths

## Exit Criteria

- every command has a deliberate contract for each global flag
- automation-facing commands with JSON support have regression tests
- unsupported global flags fail clearly or are no longer advertised ambiguously

## Inventory (2026-04-13)

**Canonical contract (prose):** [`docs/GLOBAL_FLAG_CONTRACT.md`](../../../../docs/GLOBAL_FLAG_CONTRACT.md) — supported / unsupported / partial / defect semantics and the `workflow status` JSON shadowing and `kg ingest` dry-run footguns.

Source: `cmd/dot-agents/main.go` registers **persistent** globals bound to `commands.Flags`: `--dry-run`/`-n`, `--force`/`-f`, `--verbose`/`-v`, `--yes`/`-y`, `--json`. Nested commands may add **local** flags with the same long name; those can shadow or diverge from globals (see `workflow status` below).

### Legend

| Symbol | Meaning |
|--------|---------|
| **yes** | Code reads `commands.Flags.<Field>` for this command path |
| **—** | Not referenced; flag is accepted but has no effect (silent no-op unless Cobra errors) |
| **local** | Subcommand defines its own flag with the same long name (see notes) |
| **partial** | Honored only on some code paths or combined with other flags |

### Top-level commands (direct children of `dot-agents`)

| Command | `--json` | `--dry-run` | `--yes` | `--force` | `--verbose` | Notes |
|---------|----------|-------------|---------|-----------|-------------|-------|
| `init` | — | yes | yes | yes | — | |
| `add` | — | yes | yes | yes | — | |
| `remove` | — | yes | partial | yes | — | `--yes` / `--force` skip removal prompt (`commands/remove.go`) |
| `refresh` | — | yes | — | — | — | |
| `import` | — | yes | yes | — | — | |
| `status` | yes | — | — | — | — | Structured JSON via `runStatus` |
| `doctor` | — | yes | — | — | yes | `--dry-run` suppresses link repair; verbose expands audits |
| `skills` | — | — | — | — | — | All subcommands: no global flag reads |
| `agents` | — | — | — | — | — | |
| `hooks` | — | — | — | — | — | |
| `workflow` | — | — | — | — | — | See **workflow** table (varies per subcommand) |
| `review` | — | — | — | — | — | |
| `sync` | — | partial | partial | — | — | `sync init` / `commit` / `push` honor `--dry-run`; `push` honors `--yes`; `pull` / `sync status` / `sync log` do not use these globals |
| `explain` | — | — | — | — | — | |
| `install` | — | yes | — | yes | yes | Large surface; `--dry-run` / `--verbose` used throughout `install.go` |
| `kg` | partial | see notes | — | — | — | Many handlers check `Flags.JSON` for machine output; **not** every leaf (e.g. `kg setup`, `kg serve`, maintenance mutations are human-first). `kg ingest` uses a **local** `--dry-run`, not `Flags.DryRun` — global `-n` does not drive ingest dry-run |

### `workflow` subcommands

| Subcommand | `--json` | `--dry-run` | `--yes` | Notes |
|------------|----------|-------------|---------|-------|
| `status` | **defect** | — | — | Local `--json`/`-j` on the command **shadows** the root persistent `--json` binding. `runWorkflowStatus` only checks `Flags.JSON`. Verified: `dot-agents --json workflow orient` emits JSON; `dot-agents --json workflow status` still prints human UI. |
| `orient` | yes | — | — | |
| `checkpoint` | — | — | — | Writes files; no JSON path |
| `log` | — | — | — | |
| `plan` (list) | yes | — | — | |
| `plan show` | yes | — | — | |
| `plan graph` | yes | — | — | |
| `plan create` / `plan update` | — | — | — | |
| `task add` / `task update` | — | — | — | |
| `tasks` | yes | — | — | |
| `slices` | yes | — | — | |
| `next` | yes | — | — | |
| `advance` | — | — | — | |
| `health` | yes | — | — | |
| `verify record` | — | — | — | |
| `verify log` | yes | — | — | |
| `prefs` | yes | — | — | |
| `prefs set-local` / `set-shared` | — | — | — | |
| `graph query` / `graph health` | yes | — | — | Bridge / KG paths forward JSON intent where applicable |
| `fanout` | — | — | — | |
| `merge-back` | — | — | — | |
| `fold-back create` / `fold-back list` | yes | — | — | |
| `delegation closeout` | yes | — | — | |
| `drift` | yes | — | — | |
| `sweep` | — | — | partial | Uses **`--apply`** for real runs (default dry plan). Global `--dry-run` / `--yes` are not wired here; `--yes` skips per-action prompts when `sweep --apply` runs (`workflow.go`) |

### Cross-cutting findings (for `define-supported-flag-contract`)

1. **Duplicate flag names**: `workflow status` local `--json` breaks root `--json` for that subcommand — highest-impact automation footgun found in this pass.
2. **`kg ingest --dry-run` vs global `--dry-run`**: Different variables; scripts must use the **subcommand** flag for ingest dry-run.
3. **Read-only command families** (`explain`, `review`, `skills`, `agents`, `hooks`): all five globals are effectively no-ops today (still listed in root help).
4. **`sync pull`**: No `Flags.DryRun` check — global `--dry-run` does not prevent `git pull`.
5. **`RenderCommandError` / usage**: Errors are always human-formatted; root `--json` does not apply to error rendering in `commands/ux.go`.

### Next task

Use this matrix and [`docs/GLOBAL_FLAG_CONTRACT.md`](../../../../docs/GLOBAL_FLAG_CONTRACT.md) in **`define-supported-flag-contract`** / follow-on slices: decide per command whether to implement, reject (fail fast), or narrow advertised globals (e.g. remove shadowing locals, or document “global flags not supported” for pure-doc commands).

## Fanout distribution (slices)

Canonical slices live in `SLICES.yaml` (`gfc-contract` → `gfc-implement` → `gfc-regression`). **Only one active delegation per task**; run the next fanout after `workflow merge-back` and `workflow delegation closeout` on the previous task.

### Active (orchestrator-started)

- **Slice `gfc-contract`** → task `define-supported-flag-contract`  
  - **Bundle:** `.agents/active/delegation-bundles/del-define-supported-flag-contract-1776046022.yaml`  
  - **Contract:** `.agents/active/delegation/define-supported-flag-contract.yaml`  
  - Worker reads the bundle first, then overlay / context files listed inside it.

### Queued — run after contract delegation is accepted

Replace flags/prompts if you refine the contract; keep `--slice` as below.

**Implement slice (`gfc-implement`):**

```bash
dot-agents workflow fanout \
  --plan global-flag-compliance \
  --slice gfc-implement \
  --owner gfc-implement-worker \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --feedback-goal "Do code changes match the approved contract and fix shadowing/ingest dry-run issues where specified?" \
  --scenario-tag "global-flag-implement" \
  --context-file .agents/workflow/plans/global-flag-compliance/global-flag-compliance.plan.md \
  --prompt "Implement implement-command-fixes per approved contract; scope is slice gfc-implement write_scope only." \
  --selection-reason "Contract task completed; implementation slice unblocked."
```

**Regression slice (`gfc-regression`):**

```bash
dot-agents workflow fanout \
  --plan global-flag-compliance \
  --slice gfc-regression \
  --owner gfc-regression-worker \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --feedback-goal "Does go test ./... pass with new global-flag regression cases matching the contract?" \
  --scenario-tag "global-flag-regression" \
  --require-negative-coverage \
  --context-file .agents/workflow/plans/global-flag-compliance/global-flag-compliance.plan.md \
  --prompt "Add regression tests per contract; slice gfc-regression only." \
  --selection-reason "Implementation merged; coverage slice last."
```
