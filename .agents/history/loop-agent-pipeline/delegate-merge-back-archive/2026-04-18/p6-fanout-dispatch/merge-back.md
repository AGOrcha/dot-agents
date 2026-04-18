---
schema_version: 1
task_id: p6-fanout-dispatch
parent_plan_id: loop-agent-pipeline
title: App-type dispatch and verifier-sequence wiring through plan schema, .agentsrc, and delegation bundles
summary: 'Fanout: resolve verifier_sequence from TASKS app_type or PLAN default_app_type via .agentsrc.json; --verifier-sequence override; bundle verification fields; schemas + ralph-orchestrate RALPH_VERIFIER_SEQUENCE.'
files_changed:
    - .agents/active/loop-state.md
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - bin/tests/ralph-orchestrate
    - commands/workflow.go
    - commands/workflow_test.go
    - schemas/agentsrc.schema.json
    - schemas/workflow-delegation-bundle.schema.json
    - schemas/workflow-plan.schema.json
    - src/share/templates/standard/agentsrc.json
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-04-18T13:00:31Z"
---

## Summary

Fanout: resolve verifier_sequence from TASKS app_type or PLAN default_app_type via .agentsrc.json; --verifier-sequence override; bundle verification fields; schemas + ralph-orchestrate RALPH_VERIFIER_SEQUENCE.

## Integration Notes


