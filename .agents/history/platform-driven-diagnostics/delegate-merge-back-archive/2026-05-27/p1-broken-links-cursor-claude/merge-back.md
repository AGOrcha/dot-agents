---
schema_version: 1
task_id: p1-broken-links-cursor-claude
parent_plan_id: platform-driven-diagnostics
title: 'P1: BrokenLinkReporter for cursor + claude'
summary: 'P1 cursor + claude implement BrokenLinkReporter. doctor.collectBrokenLinks delegates via type-assert; claude .mcp.json removed from projectSingleFiles to avoid double-count. PR #130 READY (all CI green). HasMultipleHardLinks move folded back to P3 - see .agents/active/fold-back/p1-hasmultiplehardlinks-move-deferred-to-p3.md.'
files_changed:
    - .agents/workflow/plans/cross-platform-test-skips-audit/PLAN.yaml
    - .agents/workflow/plans/cross-platform-test-skips-audit/TASKS.yaml
    - .agents/workflow/plans/loop-discipline-stop-hooks/PLAN.yaml
    - .agents/workflow/plans/loop-discipline-stop-hooks/TASKS.yaml
    - .agents/workflow/plans/r1-5-hook-enforcement-telemetry/PLAN.yaml
    - .agents/workflow/plans/r1-5-hook-enforcement-telemetry/TASKS.yaml
    - .agents/workflow/plans/refresh-skill-relink/PLAN.yaml
    - .agents/workflow/plans/refresh-skill-relink/TASKS.yaml
    - .agents/workflow/specs/coverage-gate-per-file/design.md
    - .agents/workflow/specs/production-code-helper-extraction/design.md
verification_result:
    status: pass
    summary: 'Parent: accept and advance. P3 (Badge + CountLinks) MUST extend its write_scope to include commands/internal/lifecycle/backup.go, backup_test.go, status_exports_test.go, and commands/internal/lifecycle/linkcount_{unix,windows}.go to accommodate the deferred HasMultipleHardLinks relocation. Bundle authoring note: cite [[validate-bundle-against-head]] when rewriting P3 - the bundle''s premise ''single caller'' was inaccurate at HEAD.'
integration_notes: 'Parent: accept and advance. P3 (Badge + CountLinks) MUST extend its write_scope to include commands/internal/lifecycle/backup.go, backup_test.go, status_exports_test.go, and commands/internal/lifecycle/linkcount_{unix,windows}.go to accommodate the deferred HasMultipleHardLinks relocation. Bundle authoring note: cite [[validate-bundle-against-head]] when rewriting P3 - the bundle''s premise ''single caller'' was inaccurate at HEAD.'
created_at: "2026-05-27T17:13:00Z"
---

## Summary

P1 cursor + claude implement BrokenLinkReporter. doctor.collectBrokenLinks delegates via type-assert; claude .mcp.json removed from projectSingleFiles to avoid double-count. PR #130 READY (all CI green). HasMultipleHardLinks move folded back to P3 - see .agents/active/fold-back/p1-hasmultiplehardlinks-move-deferred-to-p3.md.

## Integration Notes

Parent: accept and advance. P3 (Badge + CountLinks) MUST extend its write_scope to include commands/internal/lifecycle/backup.go, backup_test.go, status_exports_test.go, and commands/internal/lifecycle/linkcount_{unix,windows}.go to accommodate the deferred HasMultipleHardLinks relocation. Bundle authoring note: cite [[validate-bundle-against-head]] when rewriting P3 - the bundle's premise 'single caller' was inaccurate at HEAD.
