---
schema_version: 1
task_id: c1-kg-command-decomposition
parent_plan_id: command-surface-decomposition
title: Split kg command into narrower files or subpackages
summary: Decomposed kg into commands/kg (kg.go, query_lint_maintain, bridge, sync_code_warm_link, cmd, deps). Thin commands/kg.go shim with kgDeps. SaveKGConfig exported for tests. globalflagcov loads ./commands/kg. MCP tests remain in commands/kg_test.go.
files_changed:
    - commands/add.go
    - commands/agents_test.go
    - commands/kg.go
    - commands/kg_test.go
    - internal/globalflagcov/static.go
verification_result:
    status: pass
    summary: go test ./...
integration_notes: go test ./...
created_at: "2026-04-18T21:37:13Z"
---

## Summary

Decomposed kg into commands/kg (kg.go, query_lint_maintain, bridge, sync_code_warm_link, cmd, deps). Thin commands/kg.go shim with kgDeps. SaveKGConfig exported for tests. globalflagcov loads ./commands/kg. MCP tests remain in commands/kg_test.go.

## Integration Notes

go test ./...
