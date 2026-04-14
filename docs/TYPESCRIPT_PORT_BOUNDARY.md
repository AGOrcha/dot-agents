# TypeScript port — Phase 4 boundary (workflow / KG / orchestration)

This document records the **Phase 4** decision for the `ports/typescript` CLI: what advanced surfaces exist beyond Stage 1, and what remains **Go-only**.

## Decision (explicit choice)

Among the three candidates from the `typescript-port` plan:

| Option | Meaning |
|--------|---------|
| **1** | Config-and-resource-only Windows port (narrowest) |
| **2** | Selected **`workflow` read-only** surfaces (e.g. task visibility / health-style readback) may be added in TS **without** implying full CLI parity |
| **3** | Broader parity including **KG bridge** and **orchestration** commands in TS |

**Chosen: option 2.**

Rationale in one line: restricted-machine users still benefit from **read-only workflow visibility** when we can implement it without pulling in graph stores, Postgres, or loop write paths; **KG** and **mutating orchestration** stay aligned with the single supported implementation in Go.

## What the TypeScript port implements today (Stage 1)

- `init`, `add`, `refresh`, `status`, `doctor`, `skills`, `agents`, `hooks`
- Project registry, `.agentsrc.json` / config behavior covered by existing port tests — **not** “config-only” in the narrow sense of option 1, but **no** `workflow` or `kg` commands yet

## In scope for optional **future** TypeScript work (under option 2)

- **Read-only `workflow` subsets** (examples: listing tasks for a plan, read-only health-style summaries), implemented in TS only if they can be done safely **without** duplicating Go graph/Postgres dependencies
- Clear `--help` and docs whenever such commands appear

**Not a commitment:** nothing in Phase 4 requires shipping a `workflow` subcommand in the next release; the decision only **allows** that class of surface and **forbids** pretending the TS port is a full workflow/KG replacement.

## Permanently deferred from the TypeScript port (use Go `dot-agents`)

- **All `kg` / knowledge-graph commands** (query, ingest, bridge, sync, setup, …)
- **Workflow mutating and loop-driving commands** including but not limited to: `workflow checkpoint`, `workflow advance`, `workflow merge-back`, `workflow fanout`, `workflow verify record`, `workflow sweep`, delegation closeout, fold-back create, and similar write paths
- **Full orchestration parity** with the Ralph / loop tooling — the Go CLI remains authoritative

## Verification

- Canonical plan task: `phase-4-advanced-surface-decision` in `.agents/workflow/plans/typescript-port/TASKS.yaml`
- User-facing summary: `ports/typescript` top-level `--help` and `ports/typescript/README.md`
- Automated checks: `ports/typescript/tests/boundary.test.ts`

## Related docs

- `docs/TYPESCRIPT_PORT_TDD_PLAN.md` — overall port strategy and MVP list
- `.agents/workflow/plans/typescript-port/` — phased tasks and write scopes
