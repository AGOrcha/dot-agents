---
schema_version: 1
task_id: se9-periph-loud
parent_plan_id: swallowed-errors-loud-atomic
title: 'Slice 9 (periph): links/projectsync/gitwt should-be-LOUD'
summary: 'Implemented Slice 9 (periph) of swallowed-errors-loud-atomic: internal/links, internal/projectsync, internal/gitwt should-be-LOUD/ATOMIC fixes. links.go handleUnmanagedOccupant now distinguishes os.IsNotExist (legit-vanished, unchanged) from a real Lstat error (now surfaced, fail-safe -- was silently treated as ''nothing to protect'' before an unmanaged-occupant overwrite). IsManagedFileLink now fails safe (reports managed/protected) on a real Lstat error instead of the unsafe ''false = not ours, safe to delete'' direction that platform/hooks.go and platform/claude.go gate destructive decisions on. RemoveIfHardlinkedToAny now surfaces a real AreHardlinked syscall error instead of reading it as ''not hard-linked'', while a missing candidate source is still correctly skipped. projectsync/journal.go readPendingJournalEntry (+ListPendingPromoteJournals) now aborts the pending-recovery listing on a real read/parse failure instead of silently dropping the entry -- feeds crash-recovery, so a dropped in-flight promote would become permanently unrecoverable (should-be-ATOMIC). promote.go clearExistingCanonical now aborts on a real Lstat error instead of proceeding into a destructive CopyTree (should-be-ATOMIC). list.go ListBucket now surfaces a real ReadDir error instead of reporting it identically to ''empty bucket''. gitwt/gogit.go adminDir/worktreeDir now return a typed error (ErrWorktreeNotFound vs a real error) instead of a bare bool; Prune now aborts on a real worktreeDir error instead of silently skipping the entry (should-be-ATOMIC for the destructive prune scan). Added a three-way test per site (legit-absence unchanged / real-error surfaced-or-fail-safe / present-valid unchanged) reusing internal/testutil/perms* for cross-platform permission-fault fixtures; two pre-existing journal tests were split/flipped to match the corrected behavior (corrupt-JSON and unreadable-entry cases now assert a surfaced error instead of a silent skip). go build ./... clean; go vet + go test (incl. -race) green on all 3 packages; 92-100% coverage on every touched function; gocognit <=14 everywhere touched (source+tests), none at/above the 15 Sonar threshold. PR #363 opened base master head swallowed/9-periph-loud, no AI trailer, STOP before merge.'
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
    - .agents/workflow/plans/agent-ops-hardening/PLAN.yaml
    - .agents/workflow/plans/agent-ops-hardening/TASKS.yaml
    - .agents/workflow/plans/graph-backend-adapter-contract/PLAN.yaml
    - .agents/workflow/plans/graph-backend-adapter-contract/TASKS.yaml
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
    summary: 'PR: https://github.com/AGOrcha/dot-agents/pull/363 (base master, head swallowed/9-periph-loud). Independent of the other se9 sub-slices (9a-9c, 9e, 9f) and of every other completed swallowed-errors slice -- no file overlap, safe to merge in any order relative to them. Parent should review the PR, then run workflow advance + workflow delegation closeout for se9-periph-loud (not se9-p2-loud-batch, which stays pending/tracking the remaining 9a/9b/9c/9e/9f sub-slices).'
integration_notes: 'PR: https://github.com/AGOrcha/dot-agents/pull/363 (base master, head swallowed/9-periph-loud). Independent of the other se9 sub-slices (9a-9c, 9e, 9f) and of every other completed swallowed-errors slice -- no file overlap, safe to merge in any order relative to them. Parent should review the PR, then run workflow advance + workflow delegation closeout for se9-periph-loud (not se9-p2-loud-batch, which stays pending/tracking the remaining 9a/9b/9c/9e/9f sub-slices).'
created_at: "2026-07-09T05:10:26Z"
---

## Summary

Implemented Slice 9 (periph) of swallowed-errors-loud-atomic: internal/links, internal/projectsync, internal/gitwt should-be-LOUD/ATOMIC fixes. links.go handleUnmanagedOccupant now distinguishes os.IsNotExist (legit-vanished, unchanged) from a real Lstat error (now surfaced, fail-safe -- was silently treated as 'nothing to protect' before an unmanaged-occupant overwrite). IsManagedFileLink now fails safe (reports managed/protected) on a real Lstat error instead of the unsafe 'false = not ours, safe to delete' direction that platform/hooks.go and platform/claude.go gate destructive decisions on. RemoveIfHardlinkedToAny now surfaces a real AreHardlinked syscall error instead of reading it as 'not hard-linked', while a missing candidate source is still correctly skipped. projectsync/journal.go readPendingJournalEntry (+ListPendingPromoteJournals) now aborts the pending-recovery listing on a real read/parse failure instead of silently dropping the entry -- feeds crash-recovery, so a dropped in-flight promote would become permanently unrecoverable (should-be-ATOMIC). promote.go clearExistingCanonical now aborts on a real Lstat error instead of proceeding into a destructive CopyTree (should-be-ATOMIC). list.go ListBucket now surfaces a real ReadDir error instead of reporting it identically to 'empty bucket'. gitwt/gogit.go adminDir/worktreeDir now return a typed error (ErrWorktreeNotFound vs a real error) instead of a bare bool; Prune now aborts on a real worktreeDir error instead of silently skipping the entry (should-be-ATOMIC for the destructive prune scan). Added a three-way test per site (legit-absence unchanged / real-error surfaced-or-fail-safe / present-valid unchanged) reusing internal/testutil/perms* for cross-platform permission-fault fixtures; two pre-existing journal tests were split/flipped to match the corrected behavior (corrupt-JSON and unreadable-entry cases now assert a surfaced error instead of a silent skip). go build ./... clean; go vet + go test (incl. -race) green on all 3 packages; 92-100% coverage on every touched function; gocognit <=14 everywhere touched (source+tests), none at/above the 15 Sonar threshold. PR #363 opened base master head swallowed/9-periph-loud, no AI trailer, STOP before merge.

## Integration Notes

PR: https://github.com/AGOrcha/dot-agents/pull/363 (base master, head swallowed/9-periph-loud). Independent of the other se9 sub-slices (9a-9c, 9e, 9f) and of every other completed swallowed-errors slice -- no file overlap, safe to merge in any order relative to them. Parent should review the PR, then run workflow advance + workflow delegation closeout for se9-periph-loud (not se9-p2-loud-batch, which stays pending/tracking the remaining 9a/9b/9c/9e/9f sub-slices).
