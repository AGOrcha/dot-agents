---
schema_version: 1
task_id: p2-archive-handler
parent_plan_id: plan-archive-command
title: Implement runWorkflowPlanArchive() in plan_task.go
summary: 'Implemented runWorkflowPlanArchive in plan_task.go and mergeWorkflowPlanDir in fs.go. Archive function guards on completed status (or --force), stamps status=archived + updated_at before move, calls mergeWorkflowPlanDir, removes source with one retry, handles bulk comma-separated plan IDs with per-plan error continuations. mergeWorkflowPlanDir: os.Rename fast path when history absent; DMA artifact skip (delegation.yaml, merge-back.md, closeout.yaml, delegate-merge-back-archive/ paths); canonical files (PLAN.yaml, TASKS.yaml, <id>.plan.md) always overwrite; all others sha256+mtime compare with newer-wins and warn-if-history-newer; dry-run prints per-file decisions. Helpers: isDMAFile, isCanonicalPlanFile, sha256File, removeAllWithRetry. go test ./commands/workflow/...: pass.'
files_changed:
    - .agents/active/delegation-bundles/del-p1-historybasedir-helper-1776745216.yaml
    - .agents/active/delegation-bundles/del-p2-archive-handler-1776746536.yaml
    - .agents/active/delegation-bundles/del-p3-wire-cmd-1776746947.yaml
    - .agents/active/delegation-bundles/del-p4-drift-extension-1776746947.yaml
    - .agents/active/delegation-bundles/del-p5-sweep-extension-1776746947.yaml
    - .agents/active/delegation/p1-historybasedir-helper.yaml
    - .agents/active/delegation/p2-archive-handler.yaml
    - .agents/active/delegation/p3-wire-cmd.yaml
    - .agents/active/delegation/p4-drift-extension.yaml
    - .agents/active/delegation/p5-sweep-extension.yaml
    - .agents/workflow/plans/plan-archive-command/PLAN.yaml
    - .agents/workflow/plans/plan-archive-command/TASKS.yaml
    - .agents/workflow/plans/planner-evidence-backed-write-scope/PLAN.yaml
    - .agents/workflow/plans/planner-evidence-backed-write-scope/TASKS.yaml
verification_result:
    status: pass
    summary: No conflicts. fs.go and plan_task.go only. archiveSinglePlan exported as runWorkflowPlanArchive ready for p3-wire-cmd to call.
integration_notes: No conflicts. fs.go and plan_task.go only. archiveSinglePlan exported as runWorkflowPlanArchive ready for p3-wire-cmd to call.
created_at: "2026-04-21T12:05:58Z"
---

## Summary

Implemented runWorkflowPlanArchive in plan_task.go and mergeWorkflowPlanDir in fs.go. Archive function guards on completed status (or --force), stamps status=archived + updated_at before move, calls mergeWorkflowPlanDir, removes source with one retry, handles bulk comma-separated plan IDs with per-plan error continuations. mergeWorkflowPlanDir: os.Rename fast path when history absent; DMA artifact skip (delegation.yaml, merge-back.md, closeout.yaml, delegate-merge-back-archive/ paths); canonical files (PLAN.yaml, TASKS.yaml, <id>.plan.md) always overwrite; all others sha256+mtime compare with newer-wins and warn-if-history-newer; dry-run prints per-file decisions. Helpers: isDMAFile, isCanonicalPlanFile, sha256File, removeAllWithRetry. go test ./commands/workflow/...: pass.

## Integration Notes

No conflicts. fs.go and plan_task.go only. archiveSinglePlan exported as runWorkflowPlanArchive ready for p3-wire-cmd to call.
