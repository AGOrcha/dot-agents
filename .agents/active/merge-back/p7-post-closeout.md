---
schema_version: 1
task_id: p7-post-closeout
parent_plan_id: loop-agent-pipeline
title: Post-closeout orchestration pass plus fold-back update
summary: 'ralph-closeout: stage plan YAML, archive, verification dir, and per-task git add -u for merge-back/delegation/bundle; drop broad delegation dir + loop-state staging. ralph-pipeline: delegation-bundles fallback requires active delegation contract for bundle task_id. workflow_test: fold-back update task-scoped + missing --task negative.'
files_changed:
    - .agents/active/active.loop.md
    - .agents/active/delegation/phase-5-readback-alignment.yaml
    - .agents/active/loop-state.md
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - .agents/workflow/plans/resource-command-parity/TASKS.yaml
    - commands/doctor.go
    - commands/explain.go
    - commands/explain_test.go
    - commands/install.go
    - commands/install_test.go
    - commands/remove.go
    - commands/status.go
verification_result:
    status: pass
    summary: No commands/workflow.go changes. [ok] workflow tasks loop-agent-pipeline.
integration_notes: No commands/workflow.go changes. [ok] workflow tasks loop-agent-pipeline.
created_at: "2026-04-18T19:01:54Z"
---

## Summary

ralph-closeout: stage plan YAML, archive, verification dir, and per-task git add -u for merge-back/delegation/bundle; drop broad delegation dir + loop-state staging. ralph-pipeline: delegation-bundles fallback requires active delegation contract for bundle task_id. workflow_test: fold-back update task-scoped + missing --task negative.

## Integration Notes

No commands/workflow.go changes. [ok] workflow tasks loop-agent-pipeline.
