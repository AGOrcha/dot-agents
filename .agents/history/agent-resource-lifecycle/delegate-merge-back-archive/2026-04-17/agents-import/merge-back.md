---
schema_version: 1
task_id: agents-import
parent_plan_id: agent-resource-lifecycle
title: agents import <name> — pull ~/.agents/agents/<project>/<name>/ into repo as symlink
summary: 'Implemented agents import <name>: importAgentIn loads canonical ~/.agents/agents/<project>/<name>, ensures .agents/agents symlink with promote-style safety, applies single ResourceIntent for .claude/agents via BuildResourcePlan+Execute, registers in .agentsrc. Tests: happy path, idempotency, missing canonical, empty project name, real dir conflict, mispointed symlink.'
files_changed:
    - commands/agents.go
    - commands/agents_test.go
verification_result:
    status: pass
    summary: Parent may advance agents-import and run delegation closeout; agents-remove remains pending on same files.
integration_notes: Parent may advance agents-import and run delegation closeout; agents-remove remains pending on same files.
created_at: "2026-04-17T14:03:38Z"
---

## Summary

Implemented agents import <name>: importAgentIn loads canonical ~/.agents/agents/<project>/<name>, ensures .agents/agents symlink with promote-style safety, applies single ResourceIntent for .claude/agents via BuildResourcePlan+Execute, registers in .agentsrc. Tests: happy path, idempotency, missing canonical, empty project name, real dir conflict, mispointed symlink.

## Integration Notes

Parent may advance agents-import and run delegation closeout; agents-remove remains pending on same files.
