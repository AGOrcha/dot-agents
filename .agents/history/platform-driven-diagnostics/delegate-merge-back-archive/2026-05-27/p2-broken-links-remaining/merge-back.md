---
schema_version: 1
task_id: p2-broken-links-remaining
parent_plan_id: platform-driven-diagnostics
title: 'P2: BrokenLinkReporter for codex/copilot/opencode'
summary: 'P2 platform-driven-diagnostics: codex/copilot/opencode now implement BrokenLinkReporter; doctor.collectBrokenLinks collapsed to platform.All() loop; projectSingleFiles table + collectSingleFileBrokenLinks helper deleted. PR #133 merged with full CI green (3 OS x 2 runs + Sonar + Coverage gate). 17 new per-platform tests pin migrated semantics; doctor_test.go behavior preserved verbatim.'
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
    summary: 'Parent: run workflow advance + delegation closeout --decision accept. Next task p3-badge-and-countlinks now unblocked.'
integration_notes: 'Parent: run workflow advance + delegation closeout --decision accept. Next task p3-badge-and-countlinks now unblocked.'
created_at: "2026-05-27T23:16:43Z"
---

## Summary

P2 platform-driven-diagnostics: codex/copilot/opencode now implement BrokenLinkReporter; doctor.collectBrokenLinks collapsed to platform.All() loop; projectSingleFiles table + collectSingleFileBrokenLinks helper deleted. PR #133 merged with full CI green (3 OS x 2 runs + Sonar + Coverage gate). 17 new per-platform tests pin migrated semantics; doctor_test.go behavior preserved verbatim.

## Integration Notes

Parent: run workflow advance + delegation closeout --decision accept. Next task p3-badge-and-countlinks now unblocked.
