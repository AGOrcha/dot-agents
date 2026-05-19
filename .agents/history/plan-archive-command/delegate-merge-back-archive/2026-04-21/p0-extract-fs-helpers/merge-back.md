---
schema_version: 1
task_id: p0-extract-fs-helpers
parent_plan_id: plan-archive-command
title: Extract workflow FS helpers from delegation.go into fs.go
summary: 'Created commands/workflow/fs.go (same package workflow). Moved copyWorkflowArtifact and copyWorkflowDir from delegation.go into fs.go. Added mergeWorkflowPlanDir stub (returns nil, ready for p2 implementation). Removed unused ''io'' import from delegation.go. All call sites in delegation.go are intact — no import changes needed. go build ./commands/... and go test ./commands/workflow/... both pass. Commit: 9657ffb.'
files_changed:
    - .agents/active/delegation-bundles/del-p1-historybasedir-helper-1776745216.yaml
    - .agents/active/delegation-bundles/del-p2-archive-handler-1776746536.yaml
    - .agents/active/delegation-bundles/del-p3-wire-cmd-1776746947.yaml
    - .agents/active/delegation-bundles/del-p5-sweep-extension-1776746947.yaml
    - .agents/active/delegation/p1-historybasedir-helper.yaml
    - .agents/active/delegation/p2-archive-handler.yaml
    - .agents/active/delegation/p3-wire-cmd.yaml
    - .agents/active/delegation/p5-sweep-extension.yaml
    - .agents/workflow/plans/plan-archive-command/PLAN.yaml
    - .agents/workflow/plans/plan-archive-command/TASKS.yaml
    - .agents/workflow/plans/planner-evidence-backed-write-scope/TASKS.yaml
    - commands/workflow/plan_task.go
verification_result:
    status: pass
    summary: No merge conflicts. fs.go is a new file. delegation.go diff is purely removals of the two moved functions plus the unused 'io' import. p2-archive-handler can now implement mergeWorkflowPlanDir in fs.go without touching delegation.go.
integration_notes: No merge conflicts. fs.go is a new file. delegation.go diff is purely removals of the two moved functions plus the unused 'io' import. p2-archive-handler can now implement mergeWorkflowPlanDir in fs.go without touching delegation.go.
created_at: "2026-04-21T12:00:46Z"
---

## Summary

Created commands/workflow/fs.go (same package workflow). Moved copyWorkflowArtifact and copyWorkflowDir from delegation.go into fs.go. Added mergeWorkflowPlanDir stub (returns nil, ready for p2 implementation). Removed unused 'io' import from delegation.go. All call sites in delegation.go are intact — no import changes needed. go build ./commands/... and go test ./commands/workflow/... both pass. Commit: 9657ffb.

## Integration Notes

No merge conflicts. fs.go is a new file. delegation.go diff is purely removals of the two moved functions plus the unused 'io' import. p2-archive-handler can now implement mergeWorkflowPlanDir in fs.go without touching delegation.go.
