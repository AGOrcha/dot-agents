---
schema_version: 1
task_id: c5-hooks-command-decomposition
parent_plan_id: command-surface-decomposition
title: Split hooks command by subcommand family
summary: Split hooks into commands/hooks (cmd, list, show, remove, spec); thin hooks.go shim with Deps UX injection
files_changed:
    - .agents/active/loop-state.md
    - .agents/workflow/plans/command-surface-decomposition/TASKS.yaml
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - commands/import.go
    - commands/import_plugins.go
    - commands/import_test.go
    - commands/skills.go
    - commands/skills_test.go
    - commands/status.go
    - commands/status_test.go
    - commands/sync.go
    - internal/globalflagcov/static.go
verification_result:
    status: pass
    summary: go test ./...
integration_notes: go test ./...
created_at: "2026-04-18T19:24:21Z"
---

## Summary

Split hooks into commands/hooks (cmd, list, show, remove, spec); thin hooks.go shim with Deps UX injection

## Integration Notes

go test ./...
