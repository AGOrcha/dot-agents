---
schema_version: 1
task_id: se10-p3-diagnostics-batch
parent_plan_id: swallowed-errors-loud-atomic
title: 'Slice 10: P3 diagnostics/telemetry batch (lowest priority)'
summary: 'Implemented se10-p3-diagnostics-batch (Slice 10, P3 lowest priority): distinguish os.IsNotExist from real errors + log on the real-error branch across 4 diagnostics/telemetry sites, keeping legitimate-absence behavior unchanged. scoring/signal_backfill.go:391 backfillAcrossDirs (transcript-root Stat), dashboard/store/store.go:138-141 readDirState (now a DiskStore method using s.logger, matching decodeYAML/resilientRecords siblings), adapters/builtin/sdd-register/ingest.go:349-352 fileExists, globalflagcov/static.go:77-80 loadCommandPackages (packages.Load per-package errors). Tests per site (3-way absent/permission-denied/present table where applicable), reusing internal/testutil/perms* (MakeDirUnreadable) for the permission cases. go build ./... clean; go test ./internal/scoring/... ./internal/dashboard/... ./internal/adapters/... ./internal/globalflagcov/... all green. Independent worktree /tmp/da-swallowed-10, branch swallowed/10-p3-diagnostics-batch off origin/master. PR #358 opened base master, stopped before merge, no AI trailer.'
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
    summary: 'PR #358: https://github.com/AGOrcha/dot-agents/pull/358. Ready for review/merge; parent should run workflow advance + delegation closeout after accepting.'
integration_notes: 'PR #358: https://github.com/AGOrcha/dot-agents/pull/358. Ready for review/merge; parent should run workflow advance + delegation closeout after accepting.'
created_at: "2026-07-09T03:10:54Z"
---

## Summary

Implemented se10-p3-diagnostics-batch (Slice 10, P3 lowest priority): distinguish os.IsNotExist from real errors + log on the real-error branch across 4 diagnostics/telemetry sites, keeping legitimate-absence behavior unchanged. scoring/signal_backfill.go:391 backfillAcrossDirs (transcript-root Stat), dashboard/store/store.go:138-141 readDirState (now a DiskStore method using s.logger, matching decodeYAML/resilientRecords siblings), adapters/builtin/sdd-register/ingest.go:349-352 fileExists, globalflagcov/static.go:77-80 loadCommandPackages (packages.Load per-package errors). Tests per site (3-way absent/permission-denied/present table where applicable), reusing internal/testutil/perms* (MakeDirUnreadable) for the permission cases. go build ./... clean; go test ./internal/scoring/... ./internal/dashboard/... ./internal/adapters/... ./internal/globalflagcov/... all green. Independent worktree /tmp/da-swallowed-10, branch swallowed/10-p3-diagnostics-batch off origin/master. PR #358 opened base master, stopped before merge, no AI trailer.

## Integration Notes

PR #358: https://github.com/AGOrcha/dot-agents/pull/358. Ready for review/merge; parent should run workflow advance + delegation closeout after accepting.
