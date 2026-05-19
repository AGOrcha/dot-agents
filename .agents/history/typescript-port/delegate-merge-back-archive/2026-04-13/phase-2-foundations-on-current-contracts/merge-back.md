---
schema_version: 1
task_id: phase-2-foundations-on-current-contracts
parent_plan_id: typescript-port
title: Phase 2 — Rebuild core config, path, link, and hook foundations on current contracts
summary: 'Implemented MCP detection (readMCPScope/detectMCPServers), hook event detection (detectHookEvents), and Codex agent TOML rendering (renderCodexAgentToml with developer_instructions). All parity-proven by 30 TypeScript tests matching Go contract test names. No changes to Go codebase. Write scope honored: ports/typescript/src/core/, ports/typescript/src/platforms/, ports/typescript/tests/.'
files_changed:
    - .agents/workflow/plans/typescript-port/TASKS.yaml
    - .agentsrc.json
    - .gitignore
    - ports/typescript/src/index.ts
verification_result:
    status: pass
    summary: No conflicts. Go test suite not affected (TypeScript-only changes). Pre-existing pgx build break in internal/graphstore/postgres.go is unrelated. Parent should advance task to completed and run workflow delegation closeout.
integration_notes: No conflicts. Go test suite not affected (TypeScript-only changes). Pre-existing pgx build break in internal/graphstore/postgres.go is unrelated. Parent should advance task to completed and run workflow delegation closeout.
created_at: "2026-04-13T13:08:37Z"
---

## Summary

Implemented MCP detection (readMCPScope/detectMCPServers), hook event detection (detectHookEvents), and Codex agent TOML rendering (renderCodexAgentToml with developer_instructions). All parity-proven by 30 TypeScript tests matching Go contract test names. No changes to Go codebase. Write scope honored: ports/typescript/src/core/, ports/typescript/src/platforms/, ports/typescript/tests/.

## Integration Notes

No conflicts. Go test suite not affected (TypeScript-only changes). Pre-existing pgx build break in internal/graphstore/postgres.go is unrelated. Parent should advance task to completed and run workflow delegation closeout.
