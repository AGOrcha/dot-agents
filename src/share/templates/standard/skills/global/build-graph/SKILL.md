---
name: build-graph
description: Build or update the code-review knowledge graph via dot-agents kg (CRG embedded in the KG subsystem). Run this first to initialize, or let hooks keep it updated automatically.
argument-hint: "[full]"
---

# Build Graph

Build or incrementally update the persistent **code-structure graph** for this repository using the **`dot-agents kg`** CLI (same store the MCP server uses when you run `kg serve`).

## Binary

- Prefer `dot-agents` on `PATH`.
- In the **dot-agents** repo checkout, use `go run ./cmd/dot-agents …` when `PATH` is stale.

## Steps

1. **Check graph status**

   ```bash
   dot-agents kg code-status
   ```

   If the graph has never been built or stats show it is empty/stale, plan a full build. If it exists and looks current, use an incremental update.

2. **Build or update**

   - First-time or full rebuild from repo root:

     ```bash
     dot-agents kg build
     ```

   - Day-to-day incremental update (typical):

     ```bash
     dot-agents kg update
     ```

   Use `dot-agents kg build --help` / `kg update --help` for flags (`--repo`, `--skip-flows`, etc.).

3. **Verify**

   Run `dot-agents kg code-status` again and report files parsed, node/edge counts, languages, and any errors.

## When to Use

- First-time graph setup for a repository
- After major refactors or branch switches (consider full rebuild if impact is large)
- If the graph seems stale; hooks may also run `kg update` on edit/commit

## MCP Parity (Optional)

Editors or automation that still use **stdio MCP** (`dot-agents kg serve`) may call `build_or_update_graph_tool` / `list_graph_stats_tool`; behavior should match the CLI paths above. Prefer **CLI** in agent sessions so the same commands work everywhere without JSON-RPC.

## Gotchas

Read `instructions/gotchas.md` in this skill — stale graphs after branch switches, large repos, and ignore-file setup.

## Notes

- Default storage: SQLite under `.code-review-graph/graph.db` at the repo root (see project KG / CRG docs).
- Skips generated/binary paths per `.code-review-graphignore` (configure like `.gitignore`).
