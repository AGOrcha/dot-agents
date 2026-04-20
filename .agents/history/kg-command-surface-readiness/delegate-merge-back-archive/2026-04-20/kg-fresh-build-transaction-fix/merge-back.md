---
schema_version: 1
task_id: kg-fresh-build-transaction-fix
parent_plan_id: kg-command-surface-readiness
title: Fix fresh KG build transaction failure and add isolated-home regression coverage
summary: Accepted KG fresh-build transaction fix after unit verification
files_changed:
    - .agents/active/fold-back/replacement-agent-retry.yaml
    - .agents/workflow/plans/ci-smoke-suite-hardening/ci-smoke-suite-hardening-reconcile.plan.md
    - .agents/workflow/plans/kg-command-surface-readiness/PLAN.yaml
    - .agents/workflow/plans/kg-command-surface-readiness/TASKS.yaml
    - .agents/workflow/plans/loop-agent-pipeline/PLAN.yaml
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - .agentsrc.json
    - bin/tests/ralph-closeout
    - bin/tests/ralph-pipeline
    - bin/tests/ralph-review-gate
    - bin/tests/ralph-worker
    - commands/kg/kg_test.go
    - commands/workflow/cmd.go
    - docs/LOOP_ORCHESTRATION_SPEC.md
    - internal/graphstore/crg.go
    - tests/test-ralph-review-gate-auto.sh
verification_result:
    status: pass
    summary: Bridge-side sqlite autocommit wrapper is scoped to Python CRG entrypoints; unit verification passed and impl handoff includes fresh-home shell smoke evidence.
integration_notes: Bridge-side sqlite autocommit wrapper is scoped to Python CRG entrypoints; unit verification passed and impl handoff includes fresh-home shell smoke evidence.
created_at: "2026-04-20T12:25:39Z"
---

## Summary

Accepted KG fresh-build transaction fix after unit verification

## Integration Notes

Bridge-side sqlite autocommit wrapper is scoped to Python CRG entrypoints; unit verification passed and impl handoff includes fresh-home shell smoke evidence.
