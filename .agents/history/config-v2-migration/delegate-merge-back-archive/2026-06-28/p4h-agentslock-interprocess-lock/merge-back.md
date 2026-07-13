---
schema_version: 1
task_id: p4h-agentslock-interprocess-lock
parent_plan_id: config-v2-migration
title: Preserve agentslock sibling sections across concurrent flush
summary: Implemented interprocess-safe agentslock flush in commit 212b3c2f on wave/p4h-agentslock-lock. A portable sidecar-directory lock serializes writers, Flush rereads the latest on-disk document while the lock is held, and only this instance's dirty top-level keys are merged before the existing atomic write. This prevents stale concurrent writers from dropping sibling sections or inputs_digest while keeping the public Lockfile API unchanged.
files_changed:
  - internal/agentslock/lockfile.go
  - internal/agentslock/lockfile_test.go
verification_result:
  status: pass
  summary: "go test ./internal/agentslock/... -race -count=2; go test ./internal/agentslock -coverprofile=/tmp/p4h-agentslock.cover (total 97.0%); go build ./...; escalated env GOCACHE=/tmp/dot-agents-go-cache AGENTS_HOME=/tmp/p4h-agents-home HOME=/tmp/p4h-home go test ./internal/config/... ./commands/... all passed."
integration_notes: "Parent should review commit 212b3c2f, then advance p4h-agentslock-interprocess-lock if accepted. No canonical workflow advance was run by this worker. workflow merge-back could not be used because this worktree has no .agents/active/delegation/p4h-agentslock-interprocess-lock.yaml contract. KG self-review context degraded because code_review_graph failed under the local Python environment."
created_at: "2026-06-23T01:20:22Z"
---

## Summary

Implemented interprocess-safe agentslock flush in commit 212b3c2f on wave/p4h-agentslock-lock. A portable sidecar-directory lock serializes writers, Flush rereads the latest on-disk document while the lock is held, and only this instance's dirty top-level keys are merged before the existing atomic write. This prevents stale concurrent writers from dropping sibling sections or inputs_digest while keeping the public Lockfile API unchanged.

## Integration Notes

Parent should review commit 212b3c2f, then advance p4h-agentslock-interprocess-lock if accepted. No canonical workflow advance was run by this worker. `workflow merge-back` could not be used because this worktree has no `.agents/active/delegation/p4h-agentslock-interprocess-lock.yaml` contract. KG self-review context degraded because `code_review_graph` failed under the local Python environment.
