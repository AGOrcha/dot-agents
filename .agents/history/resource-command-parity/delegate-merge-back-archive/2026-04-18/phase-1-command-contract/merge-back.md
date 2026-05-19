---
schema_version: 1
task_id: phase-1-command-contract
parent_plan_id: resource-command-parity
title: Phase 1 — Define command-surface contract and scope boundaries for managed resources
summary: 'Canonical resource command contract: docs/RESOURCE_COMMAND_CONTRACT.md (per-resource Cobra families, shared planner/executor, explicit out-of-scope); resource-command-parity.plan.md links; TASKS phase-1 audit updated for hooks list/show/remove; root --help Long points at doc; TestResourceCommandContractDoc anchors required phrases. Retrofit: phase 2 hooks + phase 5 readback aligned to contract; phases 3–4 still pending per TASKS.'
files_changed: []
verification_result:
    status: pass
    summary: 'Parent: reconcile phase-5 completed vs depends_on on phase-3/4 pending (DAG drift documented in contract + TASKS phase-5 notes); run workflow delegation closeout + advance phase-1-command-contract when accepting.'
integration_notes: 'Parent: reconcile phase-5 completed vs depends_on on phase-3/4 pending (DAG drift documented in contract + TASKS phase-5 notes); run workflow delegation closeout + advance phase-1-command-contract when accepting.'
created_at: "2026-04-18T21:49:01Z"
---

## Summary

Canonical resource command contract: docs/RESOURCE_COMMAND_CONTRACT.md (per-resource Cobra families, shared planner/executor, explicit out-of-scope); resource-command-parity.plan.md links; TASKS phase-1 audit updated for hooks list/show/remove; root --help Long points at doc; TestResourceCommandContractDoc anchors required phrases. Retrofit: phase 2 hooks + phase 5 readback aligned to contract; phases 3–4 still pending per TASKS.

## Integration Notes

Parent: reconcile phase-5 completed vs depends_on on phase-3/4 pending (DAG drift documented in contract + TASKS phase-5 notes); run workflow delegation closeout + advance phase-1-command-contract when accepting.
