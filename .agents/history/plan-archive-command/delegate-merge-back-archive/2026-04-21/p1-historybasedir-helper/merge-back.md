---
schema_version: 1
task_id: p1-historybasedir-helper
parent_plan_id: plan-archive-command
title: Add historyBaseDir() helper to state.go
summary: Added historyBaseDir(projectPath string) string to commands/workflow/state.go immediately after plansBaseDir(). Returns filepath.Join(projectPath, '.agents', 'history'). Build passes. Pre-existing redeclaration failure in delegation.go/fs.go prevents go test from running but is unrelated to this change.
files_changed:
    - .agents/active/delegation-bundles/del-p2-archive-handler-1776746536.yaml
    - .agents/active/delegation-bundles/del-p3-wire-cmd-1776746947.yaml
    - .agents/active/delegation-bundles/del-p5-sweep-extension-1776746947.yaml
    - .agents/active/delegation/p2-archive-handler.yaml
    - .agents/active/delegation/p3-wire-cmd.yaml
    - .agents/active/delegation/p5-sweep-extension.yaml
    - .agents/workflow/plans/plan-archive-command/PLAN.yaml
    - .agents/workflow/plans/plan-archive-command/TASKS.yaml
    - .agents/workflow/plans/planner-evidence-backed-write-scope/TASKS.yaml
    - commands/workflow/plan_task.go
verification_result:
    status: partial
    summary: No merge conflicts. Change is purely additive — 4 lines. Dependent tasks p2-archive-handler and p4-drift-extension may now use historyBaseDir().
integration_notes: No merge conflicts. Change is purely additive — 4 lines. Dependent tasks p2-archive-handler and p4-drift-extension may now use historyBaseDir().
created_at: "2026-04-21T12:00:09Z"
---

## Summary

Added historyBaseDir(projectPath string) string to commands/workflow/state.go immediately after plansBaseDir(). Returns filepath.Join(projectPath, '.agents', 'history'). Build passes. Pre-existing redeclaration failure in delegation.go/fs.go prevents go test from running but is unrelated to this change.

## Integration Notes

No merge conflicts. Change is purely additive — 4 lines. Dependent tasks p2-archive-handler and p4-drift-extension may now use historyBaseDir().
