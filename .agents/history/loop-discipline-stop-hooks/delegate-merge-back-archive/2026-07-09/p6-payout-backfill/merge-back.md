---
schema_version: 1
task_id: p6-payout-backfill
parent_plan_id: loop-discipline-stop-hooks
title: Explicit payout migration and active-loop readback
summary: 'Migrated payout onto the shipped config-architecture (L1 unified-config profile / team execution-profile layer, all 10 app_types verified; L2 distributable-manifest source/extends shape; L3 identity registry verified already correct, no change needed) and adopted loop-discipline hooks/skills (hooks:true, isp+loop-worker declared alongside existing iteration-close/delegation-lifecycle/agent-handoff). Re-derived cleanly on an isolated worktree off payout''s origin/main (not the excluded docs/luma-backend-contracts branch); closed a real skill-resolution gap (isp/loop-worker/iteration-close/delegation-lifecycle were scoped to dot-agents-only on this machine despite shipping as global in the starter, fixed payout-scoped via da skills promote, non-invasive to other projects). Verified: da config lint 2/2, da config verify 6 pass/1 cosmetic warn, da install materializes all 5 required skills + hook gates, da workflow app-types resolves all 10, sandboxed isp-gate/loop-worker-gate dry runs pass plus a negative-control block proof. Zero live-artifact loss confirmed (additive-only diff vs origin/main; live dirty payout tree never touched). PRs opened: payout-wrk#2 (main), dot-agents#372 (master) — both CI-green, open, unmerged.'
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
    summary: 'Both PRs are open and CI-green (payout-wrk#2: SonarCloud quality gate passed, no Actions configured at repo root; dot-agents#372: all 5 CI jobs passed). Stopped before merge per instructions. Known gap flagged for P7 (not silently fixed): plan-wave-picker/provider-consumer-pair skill resolution predates this task. p6-rollback-plan (dependent task) not started - separate task.'
integration_notes: 'Both PRs are open and CI-green (payout-wrk#2: SonarCloud quality gate passed, no Actions configured at repo root; dot-agents#372: all 5 CI jobs passed). Stopped before merge per instructions. Known gap flagged for P7 (not silently fixed): plan-wave-picker/provider-consumer-pair skill resolution predates this task. p6-rollback-plan (dependent task) not started - separate task.'
created_at: "2026-07-09T20:32:34Z"
---

## Summary

Migrated payout onto the shipped config-architecture (L1 unified-config profile / team execution-profile layer, all 10 app_types verified; L2 distributable-manifest source/extends shape; L3 identity registry verified already correct, no change needed) and adopted loop-discipline hooks/skills (hooks:true, isp+loop-worker declared alongside existing iteration-close/delegation-lifecycle/agent-handoff). Re-derived cleanly on an isolated worktree off payout's origin/main (not the excluded docs/luma-backend-contracts branch); closed a real skill-resolution gap (isp/loop-worker/iteration-close/delegation-lifecycle were scoped to dot-agents-only on this machine despite shipping as global in the starter, fixed payout-scoped via da skills promote, non-invasive to other projects). Verified: da config lint 2/2, da config verify 6 pass/1 cosmetic warn, da install materializes all 5 required skills + hook gates, da workflow app-types resolves all 10, sandboxed isp-gate/loop-worker-gate dry runs pass plus a negative-control block proof. Zero live-artifact loss confirmed (additive-only diff vs origin/main; live dirty payout tree never touched). PRs opened: payout-wrk#2 (main), dot-agents#372 (master) — both CI-green, open, unmerged.

## Integration Notes

Both PRs are open and CI-green (payout-wrk#2: SonarCloud quality gate passed, no Actions configured at repo root; dot-agents#372: all 5 CI jobs passed). Stopped before merge per instructions. Known gap flagged for P7 (not silently fixed): plan-wave-picker/provider-consumer-pair skill resolution predates this task. p6-rollback-plan (dependent task) not started - separate task.
