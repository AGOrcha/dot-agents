---
schema_version: 1
task_id: t10b-create-settings-subpackage
parent_plan_id: root-command-decomposition
title: Move settings command into commands/settings/ (per agents/skills file convention)
summary: 'Extracted commands/settings.go + commands/settings_test.go into commands/settings/ subpackage matching agents/skills convention (cmd.go / deps.go / example.go / list.go / show.go / remove.go / seams.go / seams_test.go / coverage_test.go / settings_test.go). cmdutil helpers from commands/internal/cmdutil; findSettingsSpec stays package-private; UsageError/ErrorWithHints injected via Deps. commands/settings.go is now a thin wiring shim preserving legacy package-private symbols (settingsCommandDeps, newSettings*Cmd, runSettings*) so cross-cutting coverage_test.go + resource_parity_test.go (out of t10b scope) keep compiling until t12 re-homes them and t13 deletes the shim. settings_test.go from t5 PR #49 moved with all testutil call sites preserved. Build/vet/gofmt clean; ./commands/settings/... and ./commands/... PASS; only failing module-wide test is pre-existing internal/graphstore env-only failure (missing code_review_graph python module, unrelated).'
files_changed:
    - .agents/active/delegation/fsops-windows-tests.yaml
    - .agents/active/delegation/t04-move-add-remove.yaml
    - .agents/active/delegation/t06-move-import.yaml
    - .agents/workflow/plans/root-command-decomposition/TASKS.yaml
verification_result:
    status: pass
    summary: 'Worktree: .agents/worktrees/t10b on branch feature/t10b-settings-subpackage. PR https://github.com/NikashPrakash/dot-agents/pull/74 opened (NOT merged). Commit 520e5a69. No overlap with t10a (mcp) or t10c (rules); they own disjoint commands/<resource>/ trees and disjoint root files. Parent should review PR #74 before workflow advance.'
integration_notes: 'Worktree: .agents/worktrees/t10b on branch feature/t10b-settings-subpackage. PR https://github.com/NikashPrakash/dot-agents/pull/74 opened (NOT merged). Commit 520e5a69. No overlap with t10a (mcp) or t10c (rules); they own disjoint commands/<resource>/ trees and disjoint root files. Parent should review PR #74 before workflow advance.'
created_at: "2026-05-25T14:07:01Z"
---

## Summary

Extracted commands/settings.go + commands/settings_test.go into commands/settings/ subpackage matching agents/skills convention (cmd.go / deps.go / example.go / list.go / show.go / remove.go / seams.go / seams_test.go / coverage_test.go / settings_test.go). cmdutil helpers from commands/internal/cmdutil; findSettingsSpec stays package-private; UsageError/ErrorWithHints injected via Deps. commands/settings.go is now a thin wiring shim preserving legacy package-private symbols (settingsCommandDeps, newSettings*Cmd, runSettings*) so cross-cutting coverage_test.go + resource_parity_test.go (out of t10b scope) keep compiling until t12 re-homes them and t13 deletes the shim. settings_test.go from t5 PR #49 moved with all testutil call sites preserved. Build/vet/gofmt clean; ./commands/settings/... and ./commands/... PASS; only failing module-wide test is pre-existing internal/graphstore env-only failure (missing code_review_graph python module, unrelated).

## Integration Notes

Worktree: .agents/worktrees/t10b on branch feature/t10b-settings-subpackage. PR https://github.com/NikashPrakash/dot-agents/pull/74 opened (NOT merged). Commit 520e5a69. No overlap with t10a (mcp) or t10c (rules); they own disjoint commands/<resource>/ trees and disjoint root files. Parent should review PR #74 before workflow advance.
