---
schema_version: 1
task_id: fanout-evidence-integration
parent_plan_id: planner-evidence-backed-write-scope
title: Add scope-evidence warnings to workflow fanout
summary: 'Implemented scope-evidence warnings for workflow fanout. Added checkFanoutScopeEvidenceWarnings helper in delegation.go: warns to stderr when (1) no sidecar exists and graph adapter is available, (2) sidecar exists but confidence==low. Both warnings non-blocking and suppressible via --skip-evidence-check flag added to cmd.go. Six unit tests in delegation_fanout_test.go cover all paths including suppression. All tests pass (go test ./...).'
files_changed:
    - .agents/workflow/plans/planner-evidence-backed-write-scope/TASKS.yaml
    - .agentsrc.json
verification_result:
    status: pass
    summary: No conflicts with existing fanout behavior. checkFanoutScopeEvidenceWarnings is called after the TDD gate in runWorkflowFanout, before contract creation. New --skip-evidence-check flag registered in cmd.go fanoutCmd block.
integration_notes: No conflicts with existing fanout behavior. checkFanoutScopeEvidenceWarnings is called after the TDD gate in runWorkflowFanout, before contract creation. New --skip-evidence-check flag registered in cmd.go fanoutCmd block.
created_at: "2026-04-21T18:27:00Z"
---

## Summary

Implemented scope-evidence warnings for workflow fanout. Added checkFanoutScopeEvidenceWarnings helper in delegation.go: warns to stderr when (1) no sidecar exists and graph adapter is available, (2) sidecar exists but confidence==low. Both warnings non-blocking and suppressible via --skip-evidence-check flag added to cmd.go. Six unit tests in delegation_fanout_test.go cover all paths including suppression. All tests pass (go test ./...).

## Integration Notes

No conflicts with existing fanout behavior. checkFanoutScopeEvidenceWarnings is called after the TDD gate in runWorkflowFanout, before contract creation. New --skip-evidence-check flag registered in cmd.go fanoutCmd block.
