---
schema_version: 1
task_id: t07-move-refresh
parent_plan_id: root-command-decomposition
title: Move refresh command into commands/lifecycle/
summary: 't07 impl complete: refresh cobra constructor moved to commands/lifecycle/refresh.go (NewRefreshCmd taking lifecycle.Deps); lifecycle.Deps grew RunRefresh + ExampleBlock fields; commands/refresh.go is now a thin shim mirroring the commands/sync.go pattern; internal/globalflagcov/static.go gained the lifecycle package in its allowlist (otherwise TestReportNoUnresolvedHandlers fails on the relocated RunE closure); 4 new constructor tests in commands/lifecycle/refresh_test.go covering metadata, --import flag, RunE dispatch + filter+bool propagation, error propagation, empty-args path. Body of runRefresh and the original two test files stay in commands/ root because they reach into addDeps/importDeps owned by t04/t06 — see fold-back .agents/active/fold-back/t07-refresh-body-deferred.md. PR #76 https://github.com/NikashPrakash/dot-agents/pull/76 opened, not merged.'
files_changed:
    - .agents/active/delegation/fsops-windows-tests.yaml
    - .agents/active/delegation/t04-move-add-remove.yaml
    - .agents/active/delegation/t06-move-import.yaml
    - .agents/active/delegation/t10b-create-settings-subpackage.yaml
    - .agents/workflow/plans/root-command-decomposition/TASKS.yaml
verification_result:
    status: pass
    summary: 'Two fold-backs to triage: (a) t07-refresh-body-deferred.md — body+tests follow-up after t04/t06 merge; (b) t07-globalflagcov-needs-lifecycle.md — documents one-line scope expansion in internal/globalflagcov/static.go which subsequent lifecycle moves t03/t04/t05/t06/t08/t09 inherit. Wave 4 sibling PRs (t03/t04/t05/t06/t08) likely need rebase coordination if they also touch commands/refresh.go or globalflagcov.'
integration_notes: 'Two fold-backs to triage: (a) t07-refresh-body-deferred.md — body+tests follow-up after t04/t06 merge; (b) t07-globalflagcov-needs-lifecycle.md — documents one-line scope expansion in internal/globalflagcov/static.go which subsequent lifecycle moves t03/t04/t05/t06/t08/t09 inherit. Wave 4 sibling PRs (t03/t04/t05/t06/t08) likely need rebase coordination if they also touch commands/refresh.go or globalflagcov.'
created_at: "2026-05-25T14:11:21Z"
---

## Summary

t07 impl complete: refresh cobra constructor moved to commands/lifecycle/refresh.go (NewRefreshCmd taking lifecycle.Deps); lifecycle.Deps grew RunRefresh + ExampleBlock fields; commands/refresh.go is now a thin shim mirroring the commands/sync.go pattern; internal/globalflagcov/static.go gained the lifecycle package in its allowlist (otherwise TestReportNoUnresolvedHandlers fails on the relocated RunE closure); 4 new constructor tests in commands/lifecycle/refresh_test.go covering metadata, --import flag, RunE dispatch + filter+bool propagation, error propagation, empty-args path. Body of runRefresh and the original two test files stay in commands/ root because they reach into addDeps/importDeps owned by t04/t06 — see fold-back .agents/active/fold-back/t07-refresh-body-deferred.md. PR #76 https://github.com/NikashPrakash/dot-agents/pull/76 opened, not merged.

## Integration Notes

Two fold-backs to triage: (a) t07-refresh-body-deferred.md — body+tests follow-up after t04/t06 merge; (b) t07-globalflagcov-needs-lifecycle.md — documents one-line scope expansion in internal/globalflagcov/static.go which subsequent lifecycle moves t03/t04/t05/t06/t08/t09 inherit. Wave 4 sibling PRs (t03/t04/t05/t06/t08) likely need rebase coordination if they also touch commands/refresh.go or globalflagcov.
