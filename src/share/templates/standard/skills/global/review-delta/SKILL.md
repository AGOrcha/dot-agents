---
name: review-delta
description: Review only changes since last commit using impact analysis. Token-efficient delta review with automatic blast-radius detection via dot-agents kg (CRG bridge).
argument-hint: "[file or function name]"
---

# Review Delta

Perform a focused, token-efficient code review of changed code and its blast radius using **`dot-agents kg`** (code graph + bridge), not raw MCP calls, unless you are driving **`dot-agents kg serve`** for compatibility.

## Binary

- Prefer `dot-agents` on `PATH`.
- In the **dot-agents** repo, use `go run ./cmd/dot-agents …` when needed.

## Workflow

1. **Load gotchas** — Read `instructions/gotchas.md`.
2. **Follow the workflow** — Execute each step in `instructions/workflow.md`.
3. **Generate output** — Fill `templates/review-output.md`.

## Token Optimization

Use only changed nodes and small neighborhoods — never paste full-repo context. The graph supplies structure (callers, tests, impact) without reading entire files.
