---
schema_version: 1
task_id: p2-impl-agent-surface
parent_plan_id: loop-agent-pipeline
title: Separate repo-side impl-agent surface from loop-worker behavior
summary: impl-agent.project.md added; LOOP_ORCHESTRATION_SPEC documents impl-handoff + role split; ralph-cursor-loop logs prompt_surface=loop-worker and impl_agent_prompt_file (not loaded)
files_changed:
    - .agents/active/loop-state.md
    - .agents/workflow/plans/agent-resource-lifecycle/PLAN.yaml
    - .agents/workflow/plans/agent-resource-lifecycle/TASKS.yaml
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - bin/tests/ralph-pipeline
    - commands/agents.go
    - commands/agents_test.go
    - commands/workflow.go
    - commands/workflow_test.go
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-04-17T15:19:05Z"
---

## Summary

impl-agent.project.md added; LOOP_ORCHESTRATION_SPEC documents impl-handoff + role split; ralph-cursor-loop logs prompt_surface=loop-worker and impl_agent_prompt_file (not loaded)

## Integration Notes


