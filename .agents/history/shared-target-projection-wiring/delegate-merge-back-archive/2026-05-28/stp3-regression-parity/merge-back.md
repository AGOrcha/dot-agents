---
schema_version: 1
task_id: stp3-regression-parity
parent_plan_id: shared-target-projection-wiring
title: 'Regression: import-relink + doctor + refresh-skill-mirror produce projection; dry-run/idempotent'
summary: 'Added 8 behavior-asserting regression tests for SharedTargetPlan wiring across the three newly-wired projection paths: 2 in commands/import_test.go (dry-run no-mutation + idempotence on relinkImportedProjects), 3 in commands/refresh_test.go (codex toml materialization + dry-run no-mutation + idempotence on runSharedTargetsForRefresh), and 3 in commands/doctor_test.go (codex toml materialization + dry-run no-mutation + idempotence on repairManagedProject via lifecycle.RunDoctor entry point). All tests assert the projection''s exclusive observable effect (.codex/agents/<n>.toml) which CreateLinks cannot produce, so regressions dropping the projection call or breaking dry-run/idempotence semantics fail here. PR #134 READY: 11 CI checks pass on linux/macos/windows, mergeStateStatus=CLEAN, SonarCloud green, coverage gate green, zero new coverage-exceptions entries (test-only change). Pre-existing TestCRGBridgeFreshBuildRealCRG env failure (missing code_review_graph Python module) confirmed unrelated — also fails on master HEAD.'
files_changed:
    - .agents/workflow/plans/coverage-gate-per-file/PLAN.yaml
    - .agents/workflow/plans/coverage-gate-per-file/TASKS.yaml
    - .agents/workflow/plans/cross-platform-test-skips-audit/PLAN.yaml
    - .agents/workflow/plans/cross-platform-test-skips-audit/TASKS.yaml
    - .agents/workflow/plans/loop-discipline-stop-hooks/PLAN.yaml
    - .agents/workflow/plans/loop-discipline-stop-hooks/TASKS.yaml
    - .agents/workflow/plans/production-code-helper-extraction/PLAN.yaml
    - .agents/workflow/plans/production-code-helper-extraction/TASKS.yaml
    - .agents/workflow/plans/r1-5-hook-enforcement-telemetry/PLAN.yaml
    - .agents/workflow/plans/r1-5-hook-enforcement-telemetry/TASKS.yaml
    - .agents/workflow/plans/r3-background-worker-service/PLAN.yaml
    - .agents/workflow/plans/r3-background-worker-service/TASKS.yaml
    - .agents/workflow/plans/refresh-skill-relink/PLAN.yaml
    - .agents/workflow/plans/refresh-skill-relink/TASKS.yaml
    - .agents/workflow/plans/shared-target-projection-wiring/TASKS.yaml
    - .agents/workflow/specs/coverage-gate-per-file/design.md
    - .agents/workflow/specs/production-code-helper-extraction/design.md
verification_result:
    status: pass
    summary: 'Parent: PR #134 is mergeable and CLEAN. After merge, run delegation closeout --decision accept per verify-task-status-vs-pr-history. Unblocks stp4-doc-reconcile-close.'
integration_notes: 'Parent: PR #134 is mergeable and CLEAN. After merge, run delegation closeout --decision accept per verify-task-status-vs-pr-history. Unblocks stp4-doc-reconcile-close.'
created_at: "2026-05-27T23:28:27Z"
---

## Summary

Added 8 behavior-asserting regression tests for SharedTargetPlan wiring across the three newly-wired projection paths: 2 in commands/import_test.go (dry-run no-mutation + idempotence on relinkImportedProjects), 3 in commands/refresh_test.go (codex toml materialization + dry-run no-mutation + idempotence on runSharedTargetsForRefresh), and 3 in commands/doctor_test.go (codex toml materialization + dry-run no-mutation + idempotence on repairManagedProject via lifecycle.RunDoctor entry point). All tests assert the projection's exclusive observable effect (.codex/agents/<n>.toml) which CreateLinks cannot produce, so regressions dropping the projection call or breaking dry-run/idempotence semantics fail here. PR #134 READY: 11 CI checks pass on linux/macos/windows, mergeStateStatus=CLEAN, SonarCloud green, coverage gate green, zero new coverage-exceptions entries (test-only change). Pre-existing TestCRGBridgeFreshBuildRealCRG env failure (missing code_review_graph Python module) confirmed unrelated — also fails on master HEAD.

## Integration Notes

Parent: PR #134 is mergeable and CLEAN. After merge, run delegation closeout --decision accept per verify-task-status-vs-pr-history. Unblocks stp4-doc-reconcile-close.
