---
schema_version: 1
task_id: derive-scope-command
parent_plan_id: planner-evidence-backed-write-scope
title: Implement workflow plan derive-scope command
summary: Implemented 'workflow plan derive-scope <plan_id> <task_id>' command in commands/workflow/plan_task.go and cmd.go. Runs scope-lane (symbol_lookup, callers_of, impact_radius via kg bridge) and context-lane (plan_context, decision_lookup) queries; derives task mode from app_type/notes; writes .agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml. Degrades gracefully to confidence:low when graph not ready — no error exit. Does NOT auto-edit TASKS.yaml. Includes tests for mode detection, confidence calculation, and end-to-end graceful degrade.
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
    summary: New function runWorkflowPlanDeriveScope in plan_task.go; new cobra subcommand planDeriveScopeCmd wired into planCmd in cmd.go. Sidecar for this task written to planner-evidence-backed-write-scope/evidence/derive-scope-command.scope.yaml.
integration_notes: New function runWorkflowPlanDeriveScope in plan_task.go; new cobra subcommand planDeriveScopeCmd wired into planCmd in cmd.go. Sidecar for this task written to planner-evidence-backed-write-scope/evidence/derive-scope-command.scope.yaml.
created_at: "2026-04-21T12:51:59Z"
---

## Summary

Implemented 'workflow plan derive-scope <plan_id> <task_id>' command in commands/workflow/plan_task.go and cmd.go. Runs scope-lane (symbol_lookup, callers_of, impact_radius via kg bridge) and context-lane (plan_context, decision_lookup) queries; derives task mode from app_type/notes; writes .agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml. Degrades gracefully to confidence:low when graph not ready — no error exit. Does NOT auto-edit TASKS.yaml. Includes tests for mode detection, confidence calculation, and end-to-end graceful degrade.

## Integration Notes

New function runWorkflowPlanDeriveScope in plan_task.go; new cobra subcommand planDeriveScopeCmd wired into planCmd in cmd.go. Sidecar for this task written to planner-evidence-backed-write-scope/evidence/derive-scope-command.scope.yaml.
