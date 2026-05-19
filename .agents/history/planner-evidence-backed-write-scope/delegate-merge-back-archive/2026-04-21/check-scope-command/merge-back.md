---
schema_version: 1
task_id: check-scope-command
parent_plan_id: planner-evidence-backed-write-scope
title: Implement workflow plan check-scope command
summary: Implemented 'workflow plan check-scope <plan_id> <task_id>' subcommand. Accepts --changed-file (repeatable) and --from-git-diff flags. Reads .scope.yaml sidecar from evidence/<task_id>.scope.yaml. Reports inside-scope, outside-scope, untouched required_paths, touched excluded_paths. Exits 0=clean, 1=warning (outside-scope or excluded touched), 2=no-sidecar. go test ./commands/workflow/... passes.
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
verification_result:
    status: pass
    summary: New cobra subcommand wired under 'workflow plan' in cmd.go. Implementation entirely in plan_task.go (checkScopeResult type + runWorkflowPlanCheckScope + checkScopeGitDiffFiles helpers). No changes to existing functions. Ready to merge.
integration_notes: New cobra subcommand wired under 'workflow plan' in cmd.go. Implementation entirely in plan_task.go (checkScopeResult type + runWorkflowPlanCheckScope + checkScopeGitDiffFiles helpers). No changes to existing functions. Ready to merge.
created_at: "2026-04-21T12:58:25Z"
---

## Summary

Implemented 'workflow plan check-scope <plan_id> <task_id>' subcommand. Accepts --changed-file (repeatable) and --from-git-diff flags. Reads .scope.yaml sidecar from evidence/<task_id>.scope.yaml. Reports inside-scope, outside-scope, untouched required_paths, touched excluded_paths. Exits 0=clean, 1=warning (outside-scope or excluded touched), 2=no-sidecar. go test ./commands/workflow/... passes.

## Integration Notes

New cobra subcommand wired under 'workflow plan' in cmd.go. Implementation entirely in plan_task.go (checkScopeResult type + runWorkflowPlanCheckScope + checkScopeGitDiffFiles helpers). No changes to existing functions. Ready to merge.
