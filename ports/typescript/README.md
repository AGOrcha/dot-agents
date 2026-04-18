# TypeScript port (Stage 1 slice)

This directory holds an experimental **TypeScript** implementation of a subset of dot-agents behavior. Treat it as a **separate variant** with explicit limits: it is **not** a silent or full replacement for the Go `dot-agents` CLI.

## Who this is for

- **Restricted machines** where installing or updating the Go toolchain is painful, but **Node.js 20+** is acceptable.
- **Windows** workflows where you only need Stage 1 config/link/skills/agents/hooks behavior and are fine using **Go `dot-agents`** elsewhere for workflow and KG.

## Phase 4 boundary (workflow / KG / orchestration)

Canonical decision: **`docs/TYPESCRIPT_PORT_BOUNDARY.md`** (repo root).

- **Chosen:** optional future **read-only `workflow`** library surfaces in TypeScript (plan option 2). The **interactive CLI** wired here remains Stage 1 commands only; read-only workflow helpers live under `src/commands/workflow.ts` for tests and embedding.
- **Go-only:** all **`kg/*`**, **workflow writes** (checkpoint, advance, merge-back, fanout, …), and **orchestration** — use the Go `dot-agents` binary.

After build, run **`npm run start -- --help`** or **`node dist/cli.js --help`** to see the same boundary text the tests lock.

## Install and run (from a clone of dot-agents)

Prerequisites: **Node.js 20+** (see `engines` in `package.json`).

```bash
cd ports/typescript
npm ci          # or: npm install
npm run build   # required before dist/cli.js or the npm bin exist
```

Run the CLI:

```bash
# From ports/typescript after build:
npm run start -- status
node dist/cli.js --help

# Optional: expose `dot-agents-ts` on PATH for this clone (still requires build first):
npm link
dot-agents-ts --help
```

On **Windows**, use PowerShell or `cmd.exe` with the same commands; paths are resolved with Node’s `path` APIs. Prefer `npm ci` in CI for reproducible installs.

## Current scope (this vertical slice)

- Stage 1 commands: `init`, `add`, `refresh`, `status`, `doctor`, `skills`, `agents`, `hooks`.
- Load and save `.agentsrc.json` from a project directory.
- Preserve **unknown top-level JSON keys** on parse → mutate → serialize, matching the Go contract in `internal/config/agentsrc.go` (`ExtraFields` / `agentsRCKnown`).

## Out of scope (use Go `dot-agents`)

- **`kg` commands, workflow mutating commands, and full orchestration** — see `docs/TYPESCRIPT_PORT_BOUNDARY.md` and `.agents/workflow/plans/typescript-port/TASKS.yaml`.
- **Full Stage 2 / plugin parity** with the Go CLI (for example Go-only plugin spec listing); Phase 5 aligned what the TS port documents and counts today — not full feature parity.

## Verify

```bash
npm test
npm run build
```
