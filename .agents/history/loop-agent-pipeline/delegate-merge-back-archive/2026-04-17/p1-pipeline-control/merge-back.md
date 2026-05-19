---
schema_version: 1
task_id: p1-pipeline-control
parent_plan_id: loop-agent-pipeline
title: ralph-pipeline outer loop, plan-scoped break check, verification directory lifecycle, and pre-verifier TDD gate
summary: Implemented plan-scoped workflow next (active delegation plan lock), optional --plan; fanout creates .agents/active/verification/<task_id>/, pre-verifier TDD gate for Go write_scope with --skip-tdd-gate, verifier-retry-max → primary_chain_max; ralph-orchestrate forwards RALPH_VERIFIER_RETRY_MAX.
files_changed:
    - .agents/active/delegation/agents-import.yaml
    - .agents/workflow/plans/agent-resource-lifecycle/PLAN.yaml
    - .agents/workflow/plans/agent-resource-lifecycle/TASKS.yaml
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - bin/tests/ralph-orchestrate
    - bin/tests/ralph-pipeline
    - commands/workflow.go
    - commands/workflow_test.go
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-04-17T14:04:33Z"
---

## Summary

Implemented plan-scoped workflow next (active delegation plan lock), optional --plan; fanout creates .agents/active/verification/<task_id>/, pre-verifier TDD gate for Go write_scope with --skip-tdd-gate, verifier-retry-max → primary_chain_max; ralph-orchestrate forwards RALPH_VERIFIER_RETRY_MAX.

## Integration Notes


