# TypeScript port (Stage 1 slice)

This directory holds an experimental **TypeScript** implementation of a subset of dot-agents behavior. It is **not** a full replacement for the Go CLI.

## Current scope (this vertical slice)

- Load and save `.agentsrc.json` from a project directory.
- Preserve **unknown top-level JSON keys** on parse → mutate → serialize, matching the Go contract in `internal/config/agentsrc.go` (`ExtraFields` / `agentsRCKnown`).

## Out of scope (later phases)

- Full command surface (`init`, `refresh`, `workflow`, knowledge graph, etc.) — see `.agents/workflow/plans/typescript-port/TASKS.yaml`.
- MCP aggregation, Codex TOML, hook rendering, and source-merge behavior beyond what is covered here.

## Commands

```bash
npm install
npm test
npm run build
```
