---
schema_version: 1
task_id: sidecar-manual-experiment
parent_plan_id: planner-evidence-backed-write-scope
title: Hand-author scope-evidence sidecars for 2 real tasks; validate shape
summary: 'Hand-authored 2 scope-evidence sidecar YAML files using git log/diff as ground truth. implement-graph-bridge-readiness-fixes.scope.yaml covers commit ed0ff9f (31 files, 1289 insertions); kg-freshness-impl.scope.yaml covers commit e09e1ac. Both validated against schema via Go struct test. Five gaps documented in docs/scope-evidence-experiment.md including: multi-task commits break clean attribution, graph queries add little for research tasks, stop_conditions require cross-task reading, and schema validation needs dedicated tooling.'
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
    summary: '3 new files committed (df865fa). Changes are purely in write_scope: .agents/workflow/plans/*/evidence/ and docs/. No Go code changes. No conflicts expected.'
integration_notes: '3 new files committed (df865fa). Changes are purely in write_scope: .agents/workflow/plans/*/evidence/ and docs/. No Go code changes. No conflicts expected.'
created_at: "2026-04-21T12:46:45Z"
---

## Summary

Hand-authored 2 scope-evidence sidecar YAML files using git log/diff as ground truth. implement-graph-bridge-readiness-fixes.scope.yaml covers commit ed0ff9f (31 files, 1289 insertions); kg-freshness-impl.scope.yaml covers commit e09e1ac. Both validated against schema via Go struct test. Five gaps documented in docs/scope-evidence-experiment.md including: multi-task commits break clean attribution, graph queries add little for research tasks, stop_conditions require cross-task reading, and schema validation needs dedicated tooling.

## Integration Notes

3 new files committed (df865fa). Changes are purely in write_scope: .agents/workflow/plans/*/evidence/ and docs/. No Go code changes. No conflicts expected.
