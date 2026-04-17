---
schema_version: 1
task_id: agents-remove
parent_plan_id: agent-resource-lifecycle
title: agents remove <name> — unlink repo symlinks and optionally delete canonical
summary: 'Added agents remove subcommand: removeAgentIn unlinks .agents/agents and .claude/agents symlinks via RemoveIfSymlinkUnder, strips name from .agentsrc.json, optional --purge with ui.Confirm/Flags.Yes and os.RemoveAll on canonical dir. Tests: RemoveAgentIn happy path, drift symlink, not linked, real dir, purge, symlink canonical error.'
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
    summary: 'Parent: advance agents-remove + delegation closeout after review.'
integration_notes: 'Parent: advance agents-remove + delegation closeout after review.'
created_at: "2026-04-17T15:19:31Z"
---

## Summary

Added agents remove subcommand: removeAgentIn unlinks .agents/agents and .claude/agents symlinks via RemoveIfSymlinkUnder, strips name from .agentsrc.json, optional --purge with ui.Confirm/Flags.Yes and os.RemoveAll on canonical dir. Tests: RemoveAgentIn happy path, drift symlink, not linked, real dir, purge, symlink canonical error.

## Integration Notes

Parent: advance agents-remove + delegation closeout after review.
