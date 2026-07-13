---
schema_version: 1
task_id: se9-platform-safe-loud
parent_plan_id: swallowed-errors-loud-atomic
title: 'Slice 9 (platform-safe): resources/mcp_settings/rules should-be-LOUD'
summary: 'Implemented Slice 9 (platform-safe) of swallowed-errors-loud-atomic: internal/platform/{resources,mcp_settings,rules}.go should-be-LOUD fixes (proposal group 9e SAFE subset). resolveScopedFile/resolveScopedFileFromBuckets (resources.go) previously returned '''' identically for legitimate absence and a real os.Stat failure (permission-denied, I/O error); their string-only contract can''t change without touching claude.go/codex.go/copilot.go/cursor.go/opencode.go/antigravity.go (all out of scope, deferred to post-se2-merge), so a real Stat error is now logged via slog on the swallow instead of silently falling through identically to absence. resolveCanonicalFileByExt (mcp_settings.go) and ResolveCanonicalRuleFile (rules.go) already returned error but any Stat failure among candidates fell through to the same generic ''not found'' message; both now return the wrapped Stat error directly when it is not os.IsNotExist, keeping the generic not-found message only for genuine absence. Legitimate-absence behavior unchanged in all three. Added a test per site (positive / legitimate-absence / real-error) reusing internal/testutil/perms* (MakeDirUnreadable) to force a Stat failure on the candidate''s immediate parent directory; new resources_test.go created (none existed for resources.go before). go build ./... clean, go test ./internal/platform/... green (95.6%25 coverage), go vet ./... clean. PR #364 open base master head swallowed/9-platform-safe-loud, no AI trailer, STOPPED before merge per contract.'
files_changed:
    - .agents/active/delegation-bundles/del-cg-project-local-overlay-1780781079.yaml
    - .agents/active/delegation-bundles/del-gcc5-verify-close-unblock-1779848143.yaml
    - .agents/active/delegation-bundles/del-l1-followups-1782677806.yaml
    - .agents/active/delegation-bundles/del-p1c-verifier-profile-source-aware-1780784015.yaml
    - .agents/active/delegation-bundles/del-p1e-docs-hooks-consistency-1779841818.yaml
    - .agents/active/delegation-bundles/del-p2-hook-scripts-1779841782.yaml
    - .agents/active/delegation-bundles/del-p3-starter-promotion-1779841783.yaml
    - .agents/active/delegation-bundles/del-t14-import-graph-assertion-1779841818.yaml
    - .agents/active/delegation-bundles/del-t2-config-relevance-resolver-1780554684.yaml
    - .agents/active/delegation-bundles/del-t3-cli-readback-1779841781.yaml
    - .agents/active/delegation-bundles/del-t4-relevance-recompute-1780556883.yaml
    - .agents/active/delegation/gcc5-verify-close-unblock.yaml
    - .agents/active/delegation/l1-followups.yaml
    - .agents/active/delegation/p1c-verifier-profile-source-aware.yaml
    - .agents/active/delegation/p1e-docs-hooks-consistency.yaml
    - .agents/active/delegation/p2-hook-scripts.yaml
    - .agents/active/delegation/p3-starter-promotion.yaml
    - .agents/active/delegation/t14-import-graph-assertion.yaml
    - .agents/active/delegation/t3-cli-readback.yaml
    - .agents/active/merge-back/gcc5-verify-close-unblock.md
    - .agents/active/merge-back/t3-cli-readback.md
    - .agents/active/verification/gcc5-verify-close-unblock/custom.result.yaml
    - .agents/active/verification/gcc5-verify-close-unblock/merge-back.result.yaml
    - .agents/active/verification/t3-cli-readback/merge-back.result.yaml
    - .agents/active/verification/t3-cli-readback/unit.result.yaml
    - .agents/lessons/index.md
    - .agents/workflow/plans/swallowed-errors-loud-atomic/PLAN.yaml
    - .agents/workflow/plans/swallowed-errors-loud-atomic/TASKS.yaml
    - .agentsrc.json
    - commands/import.go
    - commands/import_test.go
    - commands/internal/lifecycle/deps.go
    - commands/internal/lifecycle/project.go
    - commands/internal/lifecycle/project_test.go
    - commands/root.go
    - internal/config/fetcher.go
    - internal/config/fetcher_test.go
    - internal/dashboard/handlers/handlers_test.go
verification_result:
    status: pass
    summary: 'Do NOT merge yet per contract -- parent reviews PR #364 then merges and runs workflow advance + delegation closeout. claude.go/codex.go/copilot.go/cursor.go/opencode.go/antigravity.go detector-shape sites (same resolveScopedFile swallow, just unreachable without touching those files) remain open work, tracked as deferred until se2''s prune-guard PR merges (avoids the file-content conflict).'
integration_notes: 'Do NOT merge yet per contract -- parent reviews PR #364 then merges and runs workflow advance + delegation closeout. claude.go/codex.go/copilot.go/cursor.go/opencode.go/antigravity.go detector-shape sites (same resolveScopedFile swallow, just unreachable without touching those files) remain open work, tracked as deferred until se2''s prune-guard PR merges (avoids the file-content conflict).'
created_at: "2026-07-09T05:24:18Z"
---

## Summary

Implemented Slice 9 (platform-safe) of swallowed-errors-loud-atomic: internal/platform/{resources,mcp_settings,rules}.go should-be-LOUD fixes (proposal group 9e SAFE subset). resolveScopedFile/resolveScopedFileFromBuckets (resources.go) previously returned '' identically for legitimate absence and a real os.Stat failure (permission-denied, I/O error); their string-only contract can't change without touching claude.go/codex.go/copilot.go/cursor.go/opencode.go/antigravity.go (all out of scope, deferred to post-se2-merge), so a real Stat error is now logged via slog on the swallow instead of silently falling through identically to absence. resolveCanonicalFileByExt (mcp_settings.go) and ResolveCanonicalRuleFile (rules.go) already returned error but any Stat failure among candidates fell through to the same generic 'not found' message; both now return the wrapped Stat error directly when it is not os.IsNotExist, keeping the generic not-found message only for genuine absence. Legitimate-absence behavior unchanged in all three. Added a test per site (positive / legitimate-absence / real-error) reusing internal/testutil/perms* (MakeDirUnreadable) to force a Stat failure on the candidate's immediate parent directory; new resources_test.go created (none existed for resources.go before). go build ./... clean, go test ./internal/platform/... green (95.6%25 coverage), go vet ./... clean. PR #364 open base master head swallowed/9-platform-safe-loud, no AI trailer, STOPPED before merge per contract.

## Integration Notes

Do NOT merge yet per contract -- parent reviews PR #364 then merges and runs workflow advance + delegation closeout. claude.go/codex.go/copilot.go/cursor.go/opencode.go/antigravity.go detector-shape sites (same resolveScopedFile swallow, just unreachable without touching those files) remain open work, tracked as deferred until se2's prune-guard PR merges (avoids the file-content conflict).
