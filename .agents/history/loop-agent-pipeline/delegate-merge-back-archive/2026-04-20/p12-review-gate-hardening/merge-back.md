---
schema_version: 1
task_id: p12-review-gate-hardening
parent_plan_id: loop-agent-pipeline
title: Replace heuristic auto-accept with real post-task orchestrator review application
summary: Hardened review gating so scripted post-task orchestration only auto-closes explicit accept decisions and preserves reject/escalate outcomes for parent review.
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
    - commands/workflow/cmd.go
    - docs/LOOP_ORCHESTRATION_SPEC.md
    - tests/test-ralph-review-gate-auto.sh
verification_result:
    status: pass
    summary: Added workflow delegation gate for deterministic task-local readback, updated ralph-review-gate/ralph-pipeline/ralph-closeout to distinguish accept, reject, and planning-required outcomes, and covered the new behavior with focused Go and shell tests plus spec updates.
integration_notes: Added workflow delegation gate for deterministic task-local readback, updated ralph-review-gate/ralph-pipeline/ralph-closeout to distinguish accept, reject, and planning-required outcomes, and covered the new behavior with focused Go and shell tests plus spec updates.
created_at: "2026-04-20T03:45:40Z"
---

## Summary

Hardened review gating so scripted post-task orchestration only auto-closes explicit accept decisions and preserves reject/escalate outcomes for parent review.

## Integration Notes

Added workflow delegation gate for deterministic task-local readback, updated ralph-review-gate/ralph-pipeline/ralph-closeout to distinguish accept, reject, and planning-required outcomes, and covered the new behavior with focused Go and shell tests plus spec updates.
