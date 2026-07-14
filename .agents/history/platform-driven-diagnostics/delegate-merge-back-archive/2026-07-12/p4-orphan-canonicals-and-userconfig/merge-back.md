---
schema_version: 1
task_id: p4-orphan-canonicals-and-userconfig
parent_plan_id: platform-driven-diagnostics
title: 'P4: OrphanCanonicalReporter + UserConfigReporter'
summary: 'Implemented OrphanCanonicalReporter (claude owns skills bucket, codex owns agents bucket) and UserConfigReporter (claude/codex/opencode: UserBrokenLinks + UserBadge) in internal/platform. Migrated doctor.collectOrphanCanonicals, doctor.collectBrokenUserLinks, and status.collectUserConfigPlatforms to platform.All() type-assertion iteration, preserving public signatures + output shapes so all existing consumer tests pass unchanged. Added per-platform table-driven tests (positive + negative) and compile-time interface-conformance pins, including a negative pin that opencode does not implement OrphanCanonicalReporter. Committed d1aa6a49 in isolated worktree (not pushed).'
files_changed: []
verification_result:
    status: pass
    summary: 'Disjoint-bucket ownership (claude:skills, codex:agents) keeps doctor reportOrphanCanonicals fan-out double-count free. Write_scope path commands/lifecycle/{doctor,status}.go resolves to the real commands/internal/lifecycle/. Verify: go test ./internal/platform/... ./commands/internal/lifecycle/... — both ok. No push/PR per orchestrator serialization. P5 (AuditPrinter) is next and will fold the remaining inline text-mode user-config audit + per-platform print helpers.'
integration_notes: 'Disjoint-bucket ownership (claude:skills, codex:agents) keeps doctor reportOrphanCanonicals fan-out double-count free. Write_scope path commands/lifecycle/{doctor,status}.go resolves to the real commands/internal/lifecycle/. Verify: go test ./internal/platform/... ./commands/internal/lifecycle/... — both ok. No push/PR per orchestrator serialization. P5 (AuditPrinter) is next and will fold the remaining inline text-mode user-config audit + per-platform print helpers.'
created_at: "2026-06-22T13:33:03Z"
---

## Summary

Implemented OrphanCanonicalReporter (claude owns skills bucket, codex owns agents bucket) and UserConfigReporter (claude/codex/opencode: UserBrokenLinks + UserBadge) in internal/platform. Migrated doctor.collectOrphanCanonicals, doctor.collectBrokenUserLinks, and status.collectUserConfigPlatforms to platform.All() type-assertion iteration, preserving public signatures + output shapes so all existing consumer tests pass unchanged. Added per-platform table-driven tests (positive + negative) and compile-time interface-conformance pins, including a negative pin that opencode does not implement OrphanCanonicalReporter. Committed d1aa6a49 in isolated worktree (not pushed).

## Integration Notes

Disjoint-bucket ownership (claude:skills, codex:agents) keeps doctor reportOrphanCanonicals fan-out double-count free. Write_scope path commands/lifecycle/{doctor,status}.go resolves to the real commands/internal/lifecycle/. Verify: go test ./internal/platform/... ./commands/internal/lifecycle/... — both ok. No push/PR per orchestrator serialization. P5 (AuditPrinter) is next and will fold the remaining inline text-mode user-config audit + per-platform print helpers.
