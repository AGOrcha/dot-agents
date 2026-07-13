---
schema_version: 1
task_id: se3-agentsrc-detectors
parent_plan_id: swallowed-errors-loud-atomic
title: 'Slice 3: agentsrc GenerateAgentsRC detectors fail-or-full (#350 trigger)'
summary: 'Implemented se3-agentsrc-detectors in dedicated worktree /tmp/da-swallowed-3 off origin/swallowed/2-platform-prune-guard. The 7 GenerateAgentsRC detectors (isDirEntry, collectScopedDirs, hasYAMLHooks, detectSettingsHookEvents, detectMCPServers/readMCPScope, detectPlatformSettings, detectRuleScopes) now return (value, error), built on se0''s fsops.ReadFileAllowMissing/ReadDirAllowMissing/StatAllowMissing to distinguish os.IsNotExist from a real I/O error. GenerateAgentsRC aggregates every detector error via errors.Join and returns (nil, err) on any real failure -- fail-or-full, no external signature break. DeriveRepoIDFromGit keeps its documented "" fallback (spec 5.3) but now emits a structured events.Envelope warning via a new emitConfigWarning seam when the underlying gitRemoteOriginURL error is neither gitremote.ErrNoOrigin nor git.ErrRepositoryNotExists -- i.e. a genuine corrupt .git/config becomes visible. Write scope widened by orchestrator mid-task to include internal/config/agentsrc_test.go (13 existing detector-level tests updated for the new two-value signatures; added TestGenerateAgentsRC_DetectorIOErrors with 6 chmod-unreadable subtests covering every scoped resource dir, TestDeriveRepoIDFromGit_CorruptConfigEmitsWarning using a real git.PlainInit repo with a hand-corrupted .git/config, and two legitimate-absence regression guards TestDeriveRepoIDFromGit_NotACheckoutNoWarning/NoOriginRemoteNoWarning). go build ./... and go test ./internal/config/... both green. Committed (no AI trailer, authored as Nikash Prakash), pushed swallowed/3-agentsrc-detectors, opened stacked PR #354 (base swallowed/2-platform-prune-guard) -- https://github.com/AGOrcha/dot-agents/pull/354. STOPPED before merge per delegation contract; parent owns workflow advance + delegation closeout.'
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
created_at: "2026-07-09T02:54:44Z"
---

## Summary

Implemented se3-agentsrc-detectors in dedicated worktree /tmp/da-swallowed-3 off origin/swallowed/2-platform-prune-guard. The 7 GenerateAgentsRC detectors (isDirEntry, collectScopedDirs, hasYAMLHooks, detectSettingsHookEvents, detectMCPServers/readMCPScope, detectPlatformSettings, detectRuleScopes) now return (value, error), built on se0's fsops.ReadFileAllowMissing/ReadDirAllowMissing/StatAllowMissing to distinguish os.IsNotExist from a real I/O error. GenerateAgentsRC aggregates every detector error via errors.Join and returns (nil, err) on any real failure -- fail-or-full, no external signature break. DeriveRepoIDFromGit keeps its documented "" fallback (spec 5.3) but now emits a structured events.Envelope warning via a new emitConfigWarning seam when the underlying gitRemoteOriginURL error is neither gitremote.ErrNoOrigin nor git.ErrRepositoryNotExists -- i.e. a genuine corrupt .git/config becomes visible. Write scope widened by orchestrator mid-task to include internal/config/agentsrc_test.go (13 existing detector-level tests updated for the new two-value signatures; added TestGenerateAgentsRC_DetectorIOErrors with 6 chmod-unreadable subtests covering every scoped resource dir, TestDeriveRepoIDFromGit_CorruptConfigEmitsWarning using a real git.PlainInit repo with a hand-corrupted .git/config, and two legitimate-absence regression guards TestDeriveRepoIDFromGit_NotACheckoutNoWarning/NoOriginRemoteNoWarning). go build ./... and go test ./internal/config/... both green. Committed (no AI trailer, authored as Nikash Prakash), pushed swallowed/3-agentsrc-detectors, opened stacked PR #354 (base swallowed/2-platform-prune-guard) -- https://github.com/AGOrcha/dot-agents/pull/354. STOPPED before merge per delegation contract; parent owns workflow advance + delegation closeout.

## Integration Notes


