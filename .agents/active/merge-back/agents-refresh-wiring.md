---
schema_version: 1
task_id: agents-refresh-wiring
parent_plan_id: agent-resource-lifecycle
title: Fix refresh and install to wire .agentsrc.json agents list and sync ~/.agents/agents/<project>/ → repo symlinks
summary: createAgentsLinks syncs ~/.agents/agents/<project>/ to repo .agents/agents and .claude/agents; install links from project-scoped canonical paths and falls back to AgentsHome when manifest has no sources; tests added
files_changed:
    - .agents/active/active.loop.md
    - .agents/active/loop-state.md
    - .agents/workflow/plans/agent-resource-lifecycle/TASKS.yaml
    - commands/install.go
    - commands/refresh.go
    - internal/platform/claude.go
    - internal/platform/stage1_integration_test.go
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-04-17T09:00:48Z"
---

## Summary

createAgentsLinks syncs ~/.agents/agents/<project>/ to repo .agents/agents and .claude/agents; install links from project-scoped canonical paths and falls back to AgentsHome when manifest has no sources; tests added

## Integration Notes


