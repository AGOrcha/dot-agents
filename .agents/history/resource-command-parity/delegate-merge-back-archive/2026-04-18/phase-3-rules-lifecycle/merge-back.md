---
schema_version: 1
task_id: phase-3-rules-lifecycle
parent_plan_id: resource-command-parity
title: Phase 3 — Add explicit rules lifecycle surface and reconcile platform-specific projections
summary: Shipped rules list/show/remove; platform rules.go; RESOURCE_COMMAND_CONTRACT + explain; tests in commands/rules_test.go and internal/platform/rules_test.go. Kept implementation in package commands so globalflagcov works without internal/globalflagcov changes.
files_changed:
    - bin/tests/ralph-closeout
    - bin/tests/ralph-cursor-loop
    - bin/tests/ralph-orchestrate
verification_result:
    status: pass
    summary: go test ./...; rules --help; rules list
integration_notes: go test ./...; rules --help; rules list
created_at: "2026-04-18T22:26:03Z"
---

## Summary

Shipped rules list/show/remove; platform rules.go; RESOURCE_COMMAND_CONTRACT + explain; tests in commands/rules_test.go and internal/platform/rules_test.go. Kept implementation in package commands so globalflagcov works without internal/globalflagcov changes.

## Integration Notes

go test ./...; rules --help; rules list
