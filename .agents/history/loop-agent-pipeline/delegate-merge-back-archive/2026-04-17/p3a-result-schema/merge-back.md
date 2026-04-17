---
schema_version: 1
task_id: p3a-result-schema
parent_plan_id: loop-agent-pipeline
title: Introduce canonical verification-result artifact contract
summary: Introduced schemas/verification-result.schema.json (draft 2020-12), embedded copy for CLI validation, write merge-back.result.yaml on workflow merge-back with jsonschema validation; table tests for schema + integration test with fanout/merge-back.
files_changed:
    - .agents/active/delegation/agents-remove.yaml
    - .agents/workflow/plans/agent-resource-lifecycle/PLAN.yaml
    - .agents/workflow/plans/agent-resource-lifecycle/TASKS.yaml
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - bin/tests/ralph-pipeline
verification_result:
    status: pass
    summary: go test ./... ; go test ./commands -run 'VerificationResult|MergeBack_Writes|MergeBack_Invalid'
integration_notes: go test ./... ; go test ./commands -run 'VerificationResult|MergeBack_Writes|MergeBack_Invalid'
created_at: "2026-04-17T15:20:09Z"
---

## Summary

Introduced schemas/verification-result.schema.json (draft 2020-12), embedded copy for CLI validation, write merge-back.result.yaml on workflow merge-back with jsonschema validation; table tests for schema + integration test with fanout/merge-back.

## Integration Notes

go test ./... ; go test ./commands -run 'VerificationResult|MergeBack_Writes|MergeBack_Invalid'
