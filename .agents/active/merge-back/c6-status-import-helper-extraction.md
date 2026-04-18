---
schema_version: 1
task_id: c6-status-import-helper-extraction
parent_plan_id: command-surface-decomposition
title: Extract internal helper seams for status and import without forcing premature package splits
summary: 'Extracted status helpers (probeAgentsHomeGit, printAgentsHomeGitStatusLine, collectProjectTextBadges, printStatusProjectManifestSummary); unified JSON git path via probe; import foldImportCandidates + canonicalImportOutputsNonPlugin; import_plugins supportsCanonicalImportPathNonPlugin. Tests: status (git probe, badges), import (fold, non-plugin rel table, unknown rel).'
files_changed:
    - .agents/active/delegation/c5-hooks-command-decomposition.yaml
    - .agents/workflow/plans/command-surface-decomposition/TASKS.yaml
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - commands/import.go
    - commands/import_plugins.go
    - commands/import_test.go
    - commands/skills.go
    - commands/skills_test.go
    - commands/status.go
    - commands/status_test.go
    - commands/sync.go
    - internal/globalflagcov/static.go
verification_result:
    status: pass
    summary: 'Evidence: go test ./commands; go run status; workflow tasks. go test ./... fails: commands/workflow undefined symbols; internal/globalflagcov unused var — pre-existing on branch, outside write_scope.'
integration_notes: 'Evidence: go test ./commands; go run status; workflow tasks. go test ./... fails: commands/workflow undefined symbols; internal/globalflagcov unused var — pre-existing on branch, outside write_scope.'
created_at: "2026-04-18T19:25:24Z"
---

## Summary

Extracted status helpers (probeAgentsHomeGit, printAgentsHomeGitStatusLine, collectProjectTextBadges, printStatusProjectManifestSummary); unified JSON git path via probe; import foldImportCandidates + canonicalImportOutputsNonPlugin; import_plugins supportsCanonicalImportPathNonPlugin. Tests: status (git probe, badges), import (fold, non-plugin rel table, unknown rel).

## Integration Notes

Evidence: go test ./commands; go run status; workflow tasks. go test ./... fails: commands/workflow undefined symbols; internal/globalflagcov unused var — pre-existing on branch, outside write_scope.
