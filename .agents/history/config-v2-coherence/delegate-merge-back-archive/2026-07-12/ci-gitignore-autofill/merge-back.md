---
schema_version: 1
task_id: ci-gitignore-autofill
parent_plan_id: config-v2-coherence
title: Managed .gitignore auto-fill in consuming projects
summary: 'Implemented EnsureManagedGitignore in internal/links/gitignore.go: da-owned idempotent .gitignore block for consuming projects (config-distribution-model D14/R8). Ignores projected/materialized outputs + .agentsrc.local.json overlay; keeps .agentsrc.json/.agentsrc.lock committed (filtered out even if passed). Converges on re-run (sorted/dedup/slash-normalized, no append-duplication); preserves user content outside markers; recovers from truncated block. Distinct marker from local-source provenance block. 14 tests (positive+negative), gitignore.go per-file coverage 96.4 pct (>=95). unit + cli-runner verifiers green.'
files_changed:
    - .agents/workflow/plans/config-v2-coherence/TASKS.yaml
verification_result:
    status: pass
    summary: 'Branch feature/config-v2-coherence-ci-gitignore-autofill off master (deps_in_flight empty; dep cd-ensure-resolved-seam already completed on master). Pushed to org; PR https://github.com/AGOrcha/dot-agents/pull/34 (base master). Pre-push gates passed (build+vet POSIX+windows, coverage>=95, sonar-scanner). Library only — wiring EnsureManagedGitignore into install/refresh/sync is a follow-on task. Note: bundle/contract live in main checkout .agents/active/ (worktree .agents not shared); closeout run from main checkout.'
integration_notes: 'Branch feature/config-v2-coherence-ci-gitignore-autofill off master (deps_in_flight empty; dep cd-ensure-resolved-seam already completed on master). Pushed to org; PR https://github.com/AGOrcha/dot-agents/pull/34 (base master). Pre-push gates passed (build+vet POSIX+windows, coverage>=95, sonar-scanner). Library only — wiring EnsureManagedGitignore into install/refresh/sync is a follow-on task. Note: bundle/contract live in main checkout .agents/active/ (worktree .agents not shared); closeout run from main checkout.'
created_at: "2026-06-06T21:36:47Z"
---

## Summary

Implemented EnsureManagedGitignore in internal/links/gitignore.go: da-owned idempotent .gitignore block for consuming projects (config-distribution-model D14/R8). Ignores projected/materialized outputs + .agentsrc.local.json overlay; keeps .agentsrc.json/.agentsrc.lock committed (filtered out even if passed). Converges on re-run (sorted/dedup/slash-normalized, no append-duplication); preserves user content outside markers; recovers from truncated block. Distinct marker from local-source provenance block. 14 tests (positive+negative), gitignore.go per-file coverage 96.4 pct (>=95). unit + cli-runner verifiers green.

## Integration Notes

Branch feature/config-v2-coherence-ci-gitignore-autofill off master (deps_in_flight empty; dep cd-ensure-resolved-seam already completed on master). Pushed to org; PR https://github.com/AGOrcha/dot-agents/pull/34 (base master). Pre-push gates passed (build+vet POSIX+windows, coverage>=95, sonar-scanner). Library only — wiring EnsureManagedGitignore into install/refresh/sync is a follow-on task. Note: bundle/contract live in main checkout .agents/active/ (worktree .agents not shared); closeout run from main checkout.
