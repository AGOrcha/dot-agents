---
schema_version: 1
task_id: agents-promote
parent_plan_id: agent-resource-lifecycle
title: agents promote <name> — repo .agents/agents/<name>/ → ~/.agents/agents/<project>/; repo dirs become symlinks
summary: 'Added dot-agents agents promote mirroring skills promote: promoteAgentIn copies .agents/agents/<name> to canonical tree, replaces repo path with symlink, registers agents[] in .agentsrc.json, runs BuildSharedAgentMirrorIntents for .claude/agents. Table tests cover happy path, idempotency, --force, manifest preservation, and error paths.'
files_changed: []
verification_result:
    status: pass
    summary: 'go test ./commands -run PromoteAgent; go test ./... — 0 failures. CLI: agents promote --help [ok].'
integration_notes: 'go test ./commands -run PromoteAgent; go test ./... — 0 failures. CLI: agents promote --help [ok].'
created_at: "2026-04-17T09:02:08Z"
---

## Summary

Added dot-agents agents promote mirroring skills promote: promoteAgentIn copies .agents/agents/<name> to canonical tree, replaces repo path with symlink, registers agents[] in .agentsrc.json, runs BuildSharedAgentMirrorIntents for .claude/agents. Table tests cover happy path, idempotency, --force, manifest preservation, and error paths.

## Integration Notes

go test ./commands -run PromoteAgent; go test ./... — 0 failures. CLI: agents promote --help [ok].
