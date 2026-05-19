---
schema_version: 1
task_id: c2-agents-command-decomposition
parent_plan_id: command-surface-decomposition
title: Split agents command by lifecycle surface
summary: Split agents into commands/agents (deps+cmd+list/new/import/promote/remove); commands/agents.go shim; createAgent delegates to agents.CreateAgent; tests in commands/agents/agents_test.go; cobra smoke in commands/agents_test.go. go test ./... green.
files_changed: []
verification_result:
    status: pass
    summary: go test ./commands/agents/... ./commands/... ./... — all pass.
integration_notes: go test ./commands/agents/... ./commands/... ./... — all pass.
created_at: "2026-04-18T21:43:26Z"
---

## Summary

Split agents into commands/agents (deps+cmd+list/new/import/promote/remove); commands/agents.go shim; createAgent delegates to agents.CreateAgent; tests in commands/agents/agents_test.go; cobra smoke in commands/agents_test.go. go test ./... green.

## Integration Notes

go test ./commands/agents/... ./commands/... ./... — all pass.
