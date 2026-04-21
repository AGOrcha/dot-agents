---
schema_version: 1
task_id: p5-sweep-extension
parent_plan_id: plan-archive-command
title: 'Extend sweep: SweepActionArchiveCompletedPlans'
summary: Added SweepActionArchiveCompletedPlans constant and PlanID field to SweepActionItem. planSweep() emits one action per CompletedPlanID from drift report with RequiresConfirmation=true. applySweepAction() handles the new case by calling runWorkflowPlanArchive(item.Project.Path, []string{item.PlanID}, false, false). All switch statements on SweepActionType are exhaustive. go test ./commands/workflow/... passes.
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
    summary: No conflicts. Only commands/workflow/sweep.go modified. Compile clean.
integration_notes: No conflicts. Only commands/workflow/sweep.go modified. Compile clean.
created_at: "2026-04-21T12:12:17Z"
---

## Summary

Added SweepActionArchiveCompletedPlans constant and PlanID field to SweepActionItem. planSweep() emits one action per CompletedPlanID from drift report with RequiresConfirmation=true. applySweepAction() handles the new case by calling runWorkflowPlanArchive(item.Project.Path, []string{item.PlanID}, false, false). All switch statements on SweepActionType are exhaustive. go test ./commands/workflow/... passes.

## Integration Notes

No conflicts. Only commands/workflow/sweep.go modified. Compile clean.
