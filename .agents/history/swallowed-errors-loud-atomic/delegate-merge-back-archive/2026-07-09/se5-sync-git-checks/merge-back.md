---
schema_version: 1
task_id: se5-sync-git-checks
parent_plan_id: swallowed-errors-loud-atomic
title: 'Slice 5: sync push/commit git-error checks'
summary: 'Slice 5: surfaced discarded git add/commit errors in commands/sync/push.go+commit.go (only ''nothing to commit'' remains non-fatal; any other failure aborts before confirm/push) and init.go''s untrackMachineLocalState git rm --cached error. Added negative-path tests via env-stripped git identity (shared stripGitIdentity helper) and corrupted-index forcing for git add/rm failures. go build ./... + go test ./commands/sync/... green. PR #356 (base master, head swallowed/5-sync-git-checks) open, not merged.'
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
    summary: ""
integration_notes: ""
created_at: "2026-07-09T03:03:29Z"
---

## Summary

Slice 5: surfaced discarded git add/commit errors in commands/sync/push.go+commit.go (only 'nothing to commit' remains non-fatal; any other failure aborts before confirm/push) and init.go's untrackMachineLocalState git rm --cached error. Added negative-path tests via env-stripped git identity (shared stripGitIdentity helper) and corrupted-index forcing for git add/rm failures. go build ./... + go test ./commands/sync/... green. PR #356 (base master, head swallowed/5-sync-git-checks) open, not merged.

## Integration Notes


