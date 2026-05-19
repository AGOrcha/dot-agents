---
schema_version: 1
task_id: kg-change-impact-impl
parent_plan_id: kg-command-surface-readiness
title: Implement kg changes/impact trustworthiness fixes and add smoke coverage
summary: Added Status() pre-flight to runKGChanges and runKGImpact with WarnBox on unbuilt/busy states; added --require-graph flag to both kg changes and kg impact commands; wrapped CRG JSON output with graph_state field at CLI layer (kgChangesJSONOutput and kgImpactJSONOutput structs embedding existing CRG types); added advisory note in human output when ChangedFunctions or ChangedNodes+ImpactedNodes are empty. 6 new unit tests covering warn-on-unbuilt, require-graph failure, and JSON graph_state presence for both commands. All existing tests pass.
files_changed:
    - .agents/active/delegation-bundles/del-kg-freshness-impl-1776636903.yaml
    - .agents/active/delegation/kg-freshness-impl.yaml
    - .agents/active/verification/kg-freshness-impl/impl-handoff.yaml
    - .agents/workflow/plans/kg-command-surface-readiness/PLAN.yaml
    - .agents/workflow/plans/kg-command-surface-readiness/TASKS.yaml
    - commands/kg/cmd.go
    - commands/kg/kg_test.go
    - commands/kg/sync_code_warm_link.go
verification_result:
    status: pass
    summary: Changes are isolated to commands/kg/sync_code_warm_link.go and commands/kg/cmd.go. No changes to CRGChangeReport/CRGImpactResult (CRG-owned) or to crg.go. No changes to kg.go, bridge.go, or query_lint_maintain.go. Write scope respected.
integration_notes: Changes are isolated to commands/kg/sync_code_warm_link.go and commands/kg/cmd.go. No changes to CRGChangeReport/CRGImpactResult (CRG-owned) or to crg.go. No changes to kg.go, bridge.go, or query_lint_maintain.go. Write scope respected.
created_at: "2026-04-20T01:01:47Z"
---

## Summary

Added Status() pre-flight to runKGChanges and runKGImpact with WarnBox on unbuilt/busy states; added --require-graph flag to both kg changes and kg impact commands; wrapped CRG JSON output with graph_state field at CLI layer (kgChangesJSONOutput and kgImpactJSONOutput structs embedding existing CRG types); added advisory note in human output when ChangedFunctions or ChangedNodes+ImpactedNodes are empty. 6 new unit tests covering warn-on-unbuilt, require-graph failure, and JSON graph_state presence for both commands. All existing tests pass.

## Integration Notes

Changes are isolated to commands/kg/sync_code_warm_link.go and commands/kg/cmd.go. No changes to CRGChangeReport/CRGImpactResult (CRG-owned) or to crg.go. No changes to kg.go, bridge.go, or query_lint_maintain.go. Write scope respected.
