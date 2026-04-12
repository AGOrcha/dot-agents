---
name: review-pr
description: Review a PR or branch diff using the code graph for full structural context. Uses dot-agents kg (CRG embedded); optional MCP via kg serve for tool parity.
argument-hint: "[PR number or branch name]"
---

# Review PR

Perform a structured review of a pull request or branch diff using **`dot-agents kg`** for blast radius, change analysis, and bridge queries.

## Binary

- Prefer `dot-agents` on `PATH`.
- In the **dot-agents** repo, use `go run ./cmd/dot-agents …` when needed.

## Workflow

1. **Load gotchas** — Read `instructions/gotchas.md`.
2. **Follow the workflow** — `instructions/workflow.md`.
3. **Generate output** — Fill `templates/review-output.md`.

## Token Optimization

Never send full files unless explicitly required; use graph-backed snippets and query results.
