---
schema_version: 1
task_id: c3-sync-command-decomposition
parent_plan_id: command-surface-decomposition
title: Split sync command by subcommand family
summary: 'Extracted sync into commands/sync subpackage (per-subcommand files, Deps for flags + runRefresh). Thin commands/sync.go + newSyncPullCmd for tests. Tests: CountPorcelainLines + pull dry-run. globalflagcov extended for commands/* subpackages and deps.Flags.*; GLOBAL_FLAG_COVERAGE.md regenerated.'
files_changed: []
verification_result:
    status: pass
    summary: 'go test ./... pass after reset to clean sync-only commit 1fbac47. CLI: sync --help [ok], workflow tasks [ok].'
integration_notes: 'go test ./... pass after reset to clean sync-only commit 1fbac47. CLI: sync --help [ok], workflow tasks [ok].'
created_at: "2026-04-18T19:28:12Z"
---

## Summary

Extracted sync into commands/sync subpackage (per-subcommand files, Deps for flags + runRefresh). Thin commands/sync.go + newSyncPullCmd for tests. Tests: CountPorcelainLines + pull dry-run. globalflagcov extended for commands/* subpackages and deps.Flags.*; GLOBAL_FLAG_COVERAGE.md regenerated.

## Integration Notes

go test ./... pass after reset to clean sync-only commit 1fbac47. CLI: sync --help [ok], workflow tasks [ok].
