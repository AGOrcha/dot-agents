---
schema_version: 1
task_id: p4g-da-install-units-sync
parent_plan_id: config-v2-migration
title: Re-scope da install onto units lock and exact projection
summary: Implemented p4g in commit 37328617 on wave/p4g-da-install-units-sync. da install now runs EnsureResolved before output work, keeps explicit skill/agent materialization for F-007 safety, uses RunSharedTargetProjectionExact for exact/prune shared-target projection, treats projection and platform-link failures as hard errors, and records install output metadata in .agentsrc.lock instead of stamping .agentsrc.json.
files_changed:
  - commands/internal/lifecycle/install.go
  - commands/internal/lifecycle/install_test.go
verification_result:
  status: pass
  summary: "go test ./commands/internal/lifecycle ./internal/platform; escalated go test ./internal/config/...; go build ./...; escalated env GOCACHE=/tmp/dot-agents-go-cache AGENTS_HOME=/tmp/p4g-agents-home HOME=/tmp/p4g-home go test ./commands/... all passed."
integration_notes: "Parent should review commit 37328617, then advance p4g-da-install-units-sync if accepted. No canonical workflow advance was run by this worker. workflow merge-back could not be used because this worktree has no .agents/active/delegation/p4g-da-install-units-sync.yaml contract. The p4g task said to collapse resolveInstallSources/linkInstallResources into EnsureResolved only after proving EnsureResolved materializes every resource type; current code showed install still explicitly links skills/agents, so this implementation keeps that explicit materialization while using EnsureResolved for the lock/currentness seam."
created_at: "2026-06-23T01:29:30Z"
---

## Summary

Implemented p4g in commit 37328617 on wave/p4g-da-install-units-sync. `da install` now runs `EnsureResolved` before output work, keeps explicit skill/agent materialization for F-007 safety, uses `RunSharedTargetProjectionExact` for exact/prune shared-target projection, treats projection and platform-link failures as hard errors, and records install output metadata in `.agentsrc.lock` instead of stamping `.agentsrc.json`.

## Integration Notes

Parent should review commit 37328617, then advance `p4g-da-install-units-sync` if accepted. No canonical workflow advance was run by this worker. `workflow merge-back` could not be used because this worktree has no `.agents/active/delegation/p4g-da-install-units-sync.yaml` contract.

The p4g task said to collapse `resolveInstallSources` / `linkInstallResources` into `EnsureResolved` only after proving `EnsureResolved` materializes every resource type. Current code showed install still explicitly links skills/agents, so this implementation keeps that explicit materialization while using `EnsureResolved` for the lock/currentness seam.
