---
schema_version: 1
task_id: p9-sources-design-fork
parent_plan_id: loop-agent-pipeline
title: Fork external-sources design doc from the main implementation plan
summary: D6.a design fork at .agents/workflow/specs/external-agent-sources/design.md (sections 1–13); TASKS.yaml and loop-state updated; embedded iter-log schema v2 synced; checkpoint --log-to-iter wired to runWorkflowCheckpointLogToIter(n,'',''); TestCheckpointLogToIter asserts v2.
files_changed:
    - .agents/active/loop-state.md
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - bin/tests/ralph-orchestrate
    - commands/hooks.go
    - commands/import.go
    - commands/remove.go
    - commands/static/workflow-iter-log.schema.json
    - commands/workflow.go
    - commands/workflow_test.go
    - internal/platform/copilot.go
    - internal/platform/hooks.go
    - internal/platform/hooks_test.go
    - schemas/workflow-iter-log.schema.json
verification_result:
    status: pass
    summary: 'Uncommitted M bin/tests/ralph-orchestrate left untouched (out of scope). Parent: workflow advance + delegation closeout for p9.'
integration_notes: 'Uncommitted M bin/tests/ralph-orchestrate left untouched (out of scope). Parent: workflow advance + delegation closeout for p9.'
created_at: "2026-04-18T12:47:25Z"
---

## Summary

D6.a design fork at .agents/workflow/specs/external-agent-sources/design.md (sections 1–13); TASKS.yaml and loop-state updated; embedded iter-log schema v2 synced; checkpoint --log-to-iter wired to runWorkflowCheckpointLogToIter(n,'',''); TestCheckpointLogToIter asserts v2.

## Integration Notes

Uncommitted M bin/tests/ralph-orchestrate left untouched (out of scope). Parent: workflow advance + delegation closeout for p9.
