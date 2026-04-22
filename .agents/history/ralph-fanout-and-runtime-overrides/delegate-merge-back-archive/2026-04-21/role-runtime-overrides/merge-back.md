---
schema_version: 1
task_id: role-runtime-overrides
parent_plan_id: ralph-fanout-and-runtime-overrides
title: Add per-role runtime overrides
summary: Added per-role runtime override env vars with explicit precedence documentation to all four ralph scripts. ralph-orchestrate and ralph-closeout received precedence comment blocks (role-specific > RALPH_MODEL/AGENT_BIN > implicit default). ralph-worker received full 5-level chain (OVERRIDE > role > generic > AGENT_BIN > hardcoded default). ralph-pipeline received the full precedence table in its header and now explicitly exports all 19 role-specific env vars in run_pipeline_pass so sub-scripts inherit them regardless of parent shell state. 17/17 isolation tests pass; bash -n clean on all scripts.
files_changed:
    - .agents/workflow/plans/ralph-fanout-and-runtime-overrides/PLAN.yaml
    - .agents/workflow/plans/ralph-fanout-and-runtime-overrides/TASKS.yaml
    - .agentsrc.json
verification_result:
    status: pass
    summary: No changes to commands/workflow.go or workflow_test.go required; the task scope was entirely in the ralph shell scripts.
integration_notes: No changes to commands/workflow.go or workflow_test.go required; the task scope was entirely in the ralph shell scripts.
created_at: "2026-04-21T16:47:10Z"
---

## Summary

Added per-role runtime override env vars with explicit precedence documentation to all four ralph scripts. ralph-orchestrate and ralph-closeout received precedence comment blocks (role-specific > RALPH_MODEL/AGENT_BIN > implicit default). ralph-worker received full 5-level chain (OVERRIDE > role > generic > AGENT_BIN > hardcoded default). ralph-pipeline received the full precedence table in its header and now explicitly exports all 19 role-specific env vars in run_pipeline_pass so sub-scripts inherit them regardless of parent shell state. 17/17 isolation tests pass; bash -n clean on all scripts.

## Integration Notes

No changes to commands/workflow.go or workflow_test.go required; the task scope was entirely in the ralph shell scripts.
