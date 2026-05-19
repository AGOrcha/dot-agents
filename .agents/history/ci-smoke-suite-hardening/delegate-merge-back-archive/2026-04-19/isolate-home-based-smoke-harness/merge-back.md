---
schema_version: 1
task_id: isolate-home-based-smoke-harness
parent_plan_id: ci-smoke-suite-hardening
title: Move smoke flows to isolated HOME and AGENTS_HOME fixtures
summary: 'Modified .github/workflows/test.yml to add a ''Prepare isolated smoke HOME'' step that runs mktemp -d and exports SMOKE_HOME/SMOKE_AGENTS_HOME to GITHUB_ENV. All 14 built-binary smoke steps (--version, --help, init, status, doctor, add, status --audit, remove, sync, skills list, workflow status/health/plan, cleanup) now set HOME and AGENTS_HOME from those vars. Go test, gofmt, go vet, and build steps are untouched. Cleanup step now removes the tmpdir. Commit: 4081ed3.'
files_changed:
    - .agents/workflow/plans/ci-smoke-suite-hardening/PLAN.yaml
    - .agents/workflow/plans/ci-smoke-suite-hardening/TASKS.yaml
verification_result:
    status: pass
    summary: Change is self-contained to .github/workflows/test.yml. No new scripts created. YAML validates clean. Local smoke simulation with isolated HOME confirms init/status/skills list pass. No conflicts expected.
integration_notes: Change is self-contained to .github/workflows/test.yml. No new scripts created. YAML validates clean. Local smoke simulation with isolated HOME confirms init/status/skills list pass. No conflicts expected.
created_at: "2026-04-19T20:16:06Z"
---

## Summary

Modified .github/workflows/test.yml to add a 'Prepare isolated smoke HOME' step that runs mktemp -d and exports SMOKE_HOME/SMOKE_AGENTS_HOME to GITHUB_ENV. All 14 built-binary smoke steps (--version, --help, init, status, doctor, add, status --audit, remove, sync, skills list, workflow status/health/plan, cleanup) now set HOME and AGENTS_HOME from those vars. Go test, gofmt, go vet, and build steps are untouched. Cleanup step now removes the tmpdir. Commit: 4081ed3.

## Integration Notes

Change is self-contained to .github/workflows/test.yml. No new scripts created. YAML validates clean. Local smoke simulation with isolated HOME confirms init/status/skills list pass. No conflicts expected.
