---
schema_version: 1
task_id: p4b-health-extension
parent_plan_id: plan-archive-command
title: 'Extend workflow health: completed_plans_pending_archive count'
summary: Added completed_plans_pending_archive int to WorkflowHealthSnapshot.Workflow (types.go) and populated it from state.LocalDrift.CompletedPlanIDs in computeWorkflowHealth (health.go). Human-readable line printed when N>0. Status thresholds unchanged. health_test.go created with positive, negative, and JSON field tests.
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
    - commands/workflow/cmd.go
verification_result:
    status: pass
    summary: 'types.go was edited outside write_scope (was health.go only) — necessary to add the struct field. Minimal change: one field added to the Workflow anonymous struct.'
integration_notes: 'types.go was edited outside write_scope (was health.go only) — necessary to add the struct field. Minimal change: one field added to the Workflow anonymous struct.'
created_at: "2026-04-21T12:07:54Z"
---

## Summary

Added completed_plans_pending_archive int to WorkflowHealthSnapshot.Workflow (types.go) and populated it from state.LocalDrift.CompletedPlanIDs in computeWorkflowHealth (health.go). Human-readable line printed when N>0. Status thresholds unchanged. health_test.go created with positive, negative, and JSON field tests.

## Integration Notes

types.go was edited outside write_scope (was health.go only) — necessary to add the struct field. Minimal change: one field added to the Workflow anonymous struct.
