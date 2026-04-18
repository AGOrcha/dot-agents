# Resource command contract (hooks, rules, MCP, settings)

This document is the **canonical contract** for the
[`resource-command-parity`](../.agents/workflow/plans/resource-command-parity/resource-command-parity.plan.md)
plan. Phases 2–5 and future work should cite this file (and the plan) instead of duplicating scope
rules.

## Goals

- Make the **lifecycle story** for each managed resource family explicit: what users can do from
  dedicated commands versus what remains **implicit** through shared flows.
- Keep **one shared planner/executor path** for mutations (`add`, `import`, `refresh`, `install`,
  `remove`, and related readback). New per-resource commands must delegate into that path — no
  parallel ad hoc emitters.
- Document **out-of-scope** resources so the CLI is not pressured to grow a uniform surface for
  everything.

## Strategic shape: per-resource command families

Parity lands as **per-resource Cobra families** (`hooks`, and later `rules`, `mcp`, `settings`, …)
with **shared internals** (planner, platform projection, manifest updates).

Rationale:

- **User mental model:** `dot-agents hooks …` matches how people already talk about hook bundles;
  burying hooks under a generic `resources` command would add indirection without reducing code
  duplication.
- **Implementation:** shared code lives in `internal/` packages; command files stay thin
  adapters. That satisfies the guardrail above without forcing a single CLI noun for all resources.
- **Phasing:** families can ship incrementally (hooks first) while readback commands (`status`,
  `explain`, `doctor`) stay aligned in later phases.

## Managed resource families (in scope for this plan)

| Family   | Dedicated lifecycle commands (CLI) | Also touched indirectly |
|----------|-------------------------------------|-------------------------|
| **Hooks** | `hooks list`, `hooks show`, `hooks remove` | `import`, `refresh`, `install`, `remove`, `status`, `doctor` |
| **Rules** | *None yet* (phase 3) | `add`, `import`, `refresh`, `install`, `remove`, `status`, `doctor` |
| **MCP** | *None yet* (phase 4) | same as rules |
| **Settings** | *None yet* (phase 4) | same as rules |

Canonical hook storage and bundle layout: `~/.agents/hooks/…` with `HOOK.yaml` bundles (see
`dot-agents hooks --help`).

## Explicitly out of scope here

- **Agents** — tracked under **agent-resource-lifecycle** (`agents list`, `agents new`, …).
- **Context, memory, profiles** — no lifecycle command surface by design unless a future contract
  extends this document.

## Readback and cross-cutting commands

These commands summarize or explain **multiple** resource families. They must stay consistent with
this contract (no implying a dedicated lifecycle where none exists yet):

- `status`, `explain`, `doctor`, `install`, `remove`

Phase **5** aligned readback/install/remove copy with this model; rules/MCP/settings remain
implicit until phases **3** and **4** land their families.

## Retrofit: shipped phases vs this contract

| Phase | Role relative to contract |
|-------|---------------------------|
| **2 — hooks lifecycle** | Shipped `hooks list`, `hooks show`, `hooks remove` on top of canonical `HOOK.yaml` bundles; matches the “per-resource family + shared executor” shape. |
| **5 — readback alignment** | Updated user-visible surfaces so readback and lifecycle wording match the contract (including “implicit until dedicated commands exist”). |
| **3 / 4 — rules, MCP, settings** | **Pending**; contract already reserves dedicated families and forbids duplicate emitters. |

## Canonical task graph note (for maintainers)

`TASKS.yaml` may show **phase 5 completed** while **phase 3 and phase 4** are still **pending**. That
is a known **DAG drift**: readback was aligned early, but upstream lifecycle commands are not all
shipped. Parent orchestration should either adjust `depends_on` / statuses or add a follow-up task —
do not treat “phase 5 completed” as proof that phases 3–4 are done.

## Change process

1. Update this document and `.agents/workflow/plans/resource-command-parity/resource-command-parity.plan.md`.
2. Adjust tests or help text in `commands/` when boundaries or naming change.
3. Run `go test ./commands/...` (or broader) before merge.
