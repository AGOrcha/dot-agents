---
schema_version: 1
task_id: p2-conflict-detection
parent_plan_id: workflow-parallel-orchestration
title: Implement write-scope conflict detection and batch grouping
summary: Implemented ConflictsWith []string on workflowNextTaskSuggestion, writeScopeConflictResult struct (EligibleTasks, MaxBatch []string, ConflictGraph map[string][]string), writeScopesConflict helper, and computeWriteScopeConflicts(). Reused existing scopePathsOverlap from delegation.go. All slice fields initialize to []string{} not nil. 5 tests added covering exact-path conflict, directory-prefix conflict, non-overlapping no-conflict, maximal batch greedy selection, and nil-safety invariant. All pass.
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
    - .agents/workflow/plans/workflow-parallel-orchestration/PLAN.yaml
    - .agents/workflow/plans/workflow-parallel-orchestration/TASKS.yaml
verification_result:
    status: pass
    summary: No merge conflicts. Only write_scope file commands/workflow/plan_task.go was modified plus test file. AnnotatedTask struct added as stub for p3 renderer to build on.
integration_notes: No merge conflicts. Only write_scope file commands/workflow/plan_task.go was modified plus test file. AnnotatedTask struct added as stub for p3 renderer to build on.
created_at: "2026-04-21T13:12:33Z"
---

## Summary

Implemented ConflictsWith []string on workflowNextTaskSuggestion, writeScopeConflictResult struct (EligibleTasks, MaxBatch []string, ConflictGraph map[string][]string), writeScopesConflict helper, and computeWriteScopeConflicts(). Reused existing scopePathsOverlap from delegation.go. All slice fields initialize to []string{} not nil. 5 tests added covering exact-path conflict, directory-prefix conflict, non-overlapping no-conflict, maximal batch greedy selection, and nil-safety invariant. All pass.

## Integration Notes

No merge conflicts. Only write_scope file commands/workflow/plan_task.go was modified plus test file. AnnotatedTask struct added as stub for p3 renderer to build on.
