# Resource Command Parity

## Why This Exists

The loop-worker resource audit exposed a second problem adjacent to agent lifecycle parity: several
resource families are canonically managed by dot-agents, but users can only manipulate them through
indirect side effects in `add`, `import`, `refresh`, `install`, `remove`, `status`, and `doctor`.
That makes the resource model look more complete than the CLI actually is.

This plan is intentionally separate from `agent-resource-lifecycle`:

- Agents need concrete wiring and symlink-healing fixes right now.
- Hooks, rules, MCP, and settings need a command-surface contract first, then implementation.
- Context, memory, and profiles remain out of scope unless a later design says they should become
  lifecycle-managed resources.

## Audit Baseline

- `agents`: `agents list` and `agents new` exist; remaining gaps are tracked in
  `agent-resource-lifecycle`.
- `hooks`: `hooks list`, `hooks show`, and `hooks remove` (phase 2); still coordinated with
  `import` / `refresh` / `install` / `remove` / `status` / `doctor` for full wiring.
- `rules`: no dedicated command family.
- `mcp`: no dedicated command family.
- `settings`: no dedicated command family.
- `context`, `memory`, `profiles`: no lifecycle surface by design.

## Command-surface contract (canonical)

**Source of truth:** [`docs/RESOURCE_COMMAND_CONTRACT.md`](../../../../docs/RESOURCE_COMMAND_CONTRACT.md)

Summary:

- **Per-resource Cobra families** (`hooks`, later `rules`, `mcp`, `settings`) with shared internal
  planner/executor — not a single mega-`resources` command.
- **Agents, context, memory, profiles** stay out of this plan’s scope (agents covered elsewhere).
- **Readback** (`status`, `explain`, `doctor`, …) must not claim dedicated lifecycle where the
  contract says it is still implicit (phase 5 aligned copy to this model).

See the doc for the retrofit table (phases 2–5) and the **TASKS DAG drift** note on phase 5 vs
pending phases 3–4.

## Priority Order

1. Finish `agent-resource-lifecycle` first so agent wiring is no longer manual.
2. Use this plan to define the common lifecycle contract.
3. Land hooks, then rules, then MCP/settings, then align readback surfaces.

## Guardrails

- Reuse the shared planner/executor path wherever possible.
- Do not introduce per-resource ad hoc emitters that bypass the canonical storage model.
- Keep intentional non-lifecycle resources documented as out of scope rather than backfilling
  commands for everything.
