---
schema_version: 1
task_id: sidecar-schema
parent_plan_id: planner-evidence-backed-write-scope
title: Define scope-evidence sidecar schema and Go struct types
summary: 'Implemented sidecar-schema task: (1) created schemas/workflow-scope-evidence.schema.json covering all spec §4.2 fields with additionalProperties:false on all nested objects; (2) added ScopeEvidence and supporting Go structs in commands/workflow/plan_task.go with all list fields initialized to []T{} (not nil) via NewScopeEvidence constructor; (3) added scope_evidence_test.go with unmarshal round-trip test, nil-slice guard test, and negative malformed-YAML test. go test ./commands/workflow/... passes. Commit: 5d8a5c9.'
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
verification_result:
    status: pass
    summary: 'No conflicts. New files only: schemas/workflow-scope-evidence.schema.json and commands/workflow/scope_evidence_test.go. Additive struct fields appended before loadCanonicalPlan in plan_task.go — no existing function signatures changed.'
integration_notes: 'No conflicts. New files only: schemas/workflow-scope-evidence.schema.json and commands/workflow/scope_evidence_test.go. Additive struct fields appended before loadCanonicalPlan in plan_task.go — no existing function signatures changed.'
created_at: "2026-04-21T12:01:22Z"
---

## Summary

Implemented sidecar-schema task: (1) created schemas/workflow-scope-evidence.schema.json covering all spec §4.2 fields with additionalProperties:false on all nested objects; (2) added ScopeEvidence and supporting Go structs in commands/workflow/plan_task.go with all list fields initialized to []T{} (not nil) via NewScopeEvidence constructor; (3) added scope_evidence_test.go with unmarshal round-trip test, nil-slice guard test, and negative malformed-YAML test. go test ./commands/workflow/... passes. Commit: 5d8a5c9.

## Integration Notes

No conflicts. New files only: schemas/workflow-scope-evidence.schema.json and commands/workflow/scope_evidence_test.go. Additive struct fields appended before loadCanonicalPlan in plan_task.go — no existing function signatures changed.
