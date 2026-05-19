---
schema_version: 1
task_id: phase-4-mcp-settings-lifecycle
parent_plan_id: resource-command-parity
title: Phase 4 — Add explicit MCP and settings lifecycle surfaces without duplicating emitter logic
summary: 'Added mcp/settings CLI (list, show, remove) with platform ListCanonical* / Resolve / EnsureUnder* for ~/.agents/mcp and ~/.agents/settings; updated RESOURCE_COMMAND_CONTRACT.md and contract test. Tests: go test ./... green.'
files_changed: []
verification_result:
    status: pass
    summary: go test ./internal/platform/... ./commands/...; go test ./...
integration_notes: go test ./internal/platform/... ./commands/...; go test ./...
created_at: "2026-04-18T23:54:23Z"
---

## Summary

Added mcp/settings CLI (list, show, remove) with platform ListCanonical* / Resolve / EnsureUnder* for ~/.agents/mcp and ~/.agents/settings; updated RESOURCE_COMMAND_CONTRACT.md and contract test. Tests: go test ./... green.

## Integration Notes

go test ./internal/platform/... ./commands/...; go test ./...
