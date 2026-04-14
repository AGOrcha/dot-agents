# TypeScript port (Stage 1 slice)

This directory holds an experimental **TypeScript** implementation of a subset of dot-agents behavior. It is **not** a full replacement for the Go CLI.

## Phase 4 boundary (workflow / KG / orchestration)

Canonical decision: **`docs/TYPESCRIPT_PORT_BOUNDARY.md`**.

- **Chosen:** optional future **read-only `workflow`** surfaces in TypeScript (plan option 2).
- **Go-only:** all **`kg/*`**, **workflow writes** (checkpoint, advance, merge-back, fanout, …), and **orchestration** — use the Go `dot-agents` binary.

Run `node dist/cli.js --help` (after `npm run build`) to see the same boundary on the CLI.

## Current scope (this vertical slice)

- Stage 1 commands: `init`, `add`, `refresh`, `status`, `doctor`, `skills`, `agents`, `hooks`.
- Load and save `.agentsrc.json` from a project directory.
- Preserve **unknown top-level JSON keys** on parse → mutate → serialize, matching the Go contract in `internal/config/agentsrc.go` (`ExtraFields` / `agentsRCKnown`).

## Out of scope (later phases)

- **`kg` commands, workflow mutating commands, and full orchestration** — see `docs/TYPESCRIPT_PORT_BOUNDARY.md` and `.agents/workflow/plans/typescript-port/TASKS.yaml`.
- Stage 2 buckets and plugin alignment — phase 5+.

## Commands

```bash
npm install
npm test
npm run build
```
