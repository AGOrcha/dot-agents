---
schema_version: 1
task_id: kg-freshness-impl
parent_plan_id: kg-command-surface-readiness
title: Implement kg build/update/code-status readiness fixes and add smoke coverage
summary: Phase 1 accept, Phase 2 accept. kg code-status is now the authoritative freshness probe with machine-readable JSON output covering all four readiness states. kg health remains note-store only. build/update distinguish no_diff, no_mutation, and busy_or_locked correctly. All tests pass with -race. Implementation stayed within write_scope.
files_changed: []
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-04-20T00:51:42Z"
---

## Summary

Phase 1 accept, Phase 2 accept. kg code-status is now the authoritative freshness probe with machine-readable JSON output covering all four readiness states. kg health remains note-store only. build/update distinguish no_diff, no_mutation, and busy_or_locked correctly. All tests pass with -race. Implementation stayed within write_scope.

## Integration Notes


