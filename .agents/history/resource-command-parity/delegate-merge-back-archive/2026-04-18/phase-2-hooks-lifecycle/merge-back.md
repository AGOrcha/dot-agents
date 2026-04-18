---
schema_version: 1
task_id: phase-2-hooks-lifecycle
parent_plan_id: resource-command-parity
title: Phase 2 — Add coherent hook lifecycle commands on top of canonical HOOK.yaml bundles
summary: 'Added hooks show/remove; list uses platform.ListHookSpecs (canonical bundles + legacy json); import/remove docs; remove --clean deletes hooks/<project>; exported ListHookSpecs/ResolveHookCommand; synced workflow-iter-log schema v2 embed. Tests: go test ./...'
files_changed:
    - .agents/active/delegation/p9-sources-design-fork.yaml
    - .agents/active/loop-state.md
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - bin/tests/ralph-orchestrate
    - commands/workflow.go
    - commands/workflow_test.go
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-04-18T12:48:11Z"
---

## Summary

Added hooks show/remove; list uses platform.ListHookSpecs (canonical bundles + legacy json); import/remove docs; remove --clean deletes hooks/<project>; exported ListHookSpecs/ResolveHookCommand; synced workflow-iter-log schema v2 embed. Tests: go test ./...

## Integration Notes


