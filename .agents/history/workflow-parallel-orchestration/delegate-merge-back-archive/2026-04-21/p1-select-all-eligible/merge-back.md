---
schema_version: 1
task_id: p1-select-all-eligible
parent_plan_id: workflow-parallel-orchestration
title: Implement selectAllEligibleTasks() — cross-plan unblocked task set
summary: 'Implemented selectAllEligibleTasks(projectPath, planFilter) in commands/workflow/plan_task.go. Function returns all unblocked pending/in_progress tasks across active plans. Excludes: active-delegation-locked tasks, tasks with incomplete deps (intra-plan and cross-plan via planID/taskID format), and non-active plan tasks. Plan filter limits scope when provided. in_progress tasks sort before pending. Returns []workflowNextTaskSuggestion (same type as selectNextCanonicalTask). Also implemented incompleteCanonicalDependenciesCrossplan() for cross-plan dep resolution with graceful degradation (missing plan/task treated as unsatisfied, warning emitted). Refactored selectNextCanonicalTask to delegate to selectAllEligibleTasks then apply focus-task priority re-ranking. Added 10 unit tests covering all cases from spec §4.3.'
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
    - .agents/workflow/plans/plan-archive-command/plan-archive-command.plan.md
    - .agents/workflow/plans/planner-evidence-backed-write-scope/PLAN.yaml
    - .agents/workflow/plans/planner-evidence-backed-write-scope/TASKS.yaml
    - .agents/workflow/plans/workflow-parallel-orchestration/TASKS.yaml
verification_result:
    status: pass
    summary: No schema changes. selectNextCanonicalTask behavior preserved — all existing tests pass. New function is additive; p2 and p3 can call it directly.
integration_notes: No schema changes. selectNextCanonicalTask behavior preserved — all existing tests pass. New function is additive; p2 and p3 can call it directly.
created_at: "2026-04-21T13:09:14Z"
---

## Summary

Implemented selectAllEligibleTasks(projectPath, planFilter) in commands/workflow/plan_task.go. Function returns all unblocked pending/in_progress tasks across active plans. Excludes: active-delegation-locked tasks, tasks with incomplete deps (intra-plan and cross-plan via planID/taskID format), and non-active plan tasks. Plan filter limits scope when provided. in_progress tasks sort before pending. Returns []workflowNextTaskSuggestion (same type as selectNextCanonicalTask). Also implemented incompleteCanonicalDependenciesCrossplan() for cross-plan dep resolution with graceful degradation (missing plan/task treated as unsatisfied, warning emitted). Refactored selectNextCanonicalTask to delegate to selectAllEligibleTasks then apply focus-task priority re-ranking. Added 10 unit tests covering all cases from spec §4.3.

## Integration Notes

No schema changes. selectNextCanonicalTask behavior preserved — all existing tests pass. New function is additive; p2 and p3 can call it directly.
