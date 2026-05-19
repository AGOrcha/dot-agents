---
schema_version: 1
task_id: t3-sweep-priority-docs
parent_plan_id: binary-rename-da-sweep
title: Sweep README, docs/, ADRs, architecture note
summary: 'Sweep of ''dot-agents <verb>'' -> ''da <verb>'' invocations across README.md, docs/ (specs, ADRs, RFCs, research notes), and .agents/proposals/agent-context-resolution-architecture.md, per ADR-0006 hard cutover. 19 files changed in commit 1f4f024. Hard-test grep returns 15 project-name prose matches (subject/modifier uses), no binary invocations remain. Anti-scope preserved: project name ''dot-agents'', Go module path, cmd/dot-agents/ source dir, .agents/ directory layout untouched. .agents/history/ not touched.'
files_changed:
    - .agents/workflow/plans/binary-rename-da-sweep/PLAN.yaml
    - .agents/workflow/plans/binary-rename-da-sweep/TASKS.yaml
verification_result:
    status: pass
    summary: Parent should run the global cross-scope grep at t6 endgame to confirm coverage across all priority-doc targets. Out-of-scope dirty files at session start (PLAN.yaml, TASKS.yaml, delegation-bundles/, delegation/) were left untouched per loop-worker discipline.
integration_notes: Parent should run the global cross-scope grep at t6 endgame to confirm coverage across all priority-doc targets. Out-of-scope dirty files at session start (PLAN.yaml, TASKS.yaml, delegation-bundles/, delegation/) were left untouched per loop-worker discipline.
created_at: "2026-05-04T02:24:26Z"
---

## Summary

Sweep of 'dot-agents <verb>' -> 'da <verb>' invocations across README.md, docs/ (specs, ADRs, RFCs, research notes), and .agents/proposals/agent-context-resolution-architecture.md, per ADR-0006 hard cutover. 19 files changed in commit 1f4f024. Hard-test grep returns 15 project-name prose matches (subject/modifier uses), no binary invocations remain. Anti-scope preserved: project name 'dot-agents', Go module path, cmd/dot-agents/ source dir, .agents/ directory layout untouched. .agents/history/ not touched.

## Integration Notes

Parent should run the global cross-scope grep at t6 endgame to confirm coverage across all priority-doc targets. Out-of-scope dirty files at session start (PLAN.yaml, TASKS.yaml, delegation-bundles/, delegation/) were left untouched per loop-worker discipline.
