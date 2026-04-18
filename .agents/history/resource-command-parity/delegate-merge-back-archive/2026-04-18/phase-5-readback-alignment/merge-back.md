---
schema_version: 1
task_id: phase-5-readback-alignment
parent_plan_id: resource-command-parity
title: Phase 5 — Align status, explain, doctor, install, and remove with the new resource contract
summary: 'Aligned CLI readback: removed obsolete hooks add from explain manifest; install Long describes skills/agents materialize + platform link pass like refresh; status/doctor/remove Long and remove preview list hooks links; overview lists hooks; active.loop resource readback bullet; tests for explain+install.'
files_changed:
    - .agents/active/active.loop.md
    - .agents/active/loop-state.md
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
    - .agents/workflow/plans/resource-command-parity/TASKS.yaml
    - bin/tests/ralph-closeout
    - bin/tests/ralph-pipeline
    - commands/doctor.go
    - commands/explain.go
    - commands/explain_test.go
    - commands/install.go
    - commands/install_test.go
    - commands/remove.go
    - commands/status.go
    - commands/workflow_test.go
verification_result:
    status: pass
    summary: go test ./... pass. TASKS.yaml still lists phase-3/4 as deps pending — parent reconciles DAG vs merge-back.
integration_notes: go test ./... pass. TASKS.yaml still lists phase-3/4 as deps pending — parent reconciles DAG vs merge-back.
created_at: "2026-04-18T19:01:33Z"
---

## Summary

Aligned CLI readback: removed obsolete hooks add from explain manifest; install Long describes skills/agents materialize + platform link pass like refresh; status/doctor/remove Long and remove preview list hooks links; overview lists hooks; active.loop resource readback bullet; tests for explain+install.

## Integration Notes

go test ./... pass. TASKS.yaml still lists phase-3/4 as deps pending — parent reconciles DAG vs merge-back.
