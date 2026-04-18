---
schema_version: 1
task_id: p7-post-closeout
parent_plan_id: loop-agent-pipeline
title: Post-closeout orchestration pass plus fold-back update
summary: 'Implemented D2.a: fold-back create --slug upsert with (fb:slug) tagged lines; fold-back update; tests; optional RALPH_POST_CLOSEOUT_FOLD_BACK_AUDIT in ralph-closeout (plan dedupe); ralph-pipeline forwards env.'
files_changed:
    - .agents/active/delegation/p3d-ui-verifier.yaml
    - .agents/active/loop-state.md
    - .agents/workflow/plans/loop-agent-pipeline/PLAN.yaml
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
verification_result:
    status: pass
    summary: 'CLI: workflow fold-back create --slug / update; help shows --slug. Evidence: go test ./... ; go run ./cmd/dot-agents workflow fold-back create --help.'
integration_notes: 'CLI: workflow fold-back create --slug / update; help shows --slug. Evidence: go test ./... ; go run ./cmd/dot-agents workflow fold-back create --help.'
created_at: "2026-04-18T13:13:25Z"
---

## Summary

Implemented D2.a: fold-back create --slug upsert with (fb:slug) tagged lines; fold-back update; tests; optional RALPH_POST_CLOSEOUT_FOLD_BACK_AUDIT in ralph-closeout (plan dedupe); ralph-pipeline forwards env.

## Integration Notes

CLI: workflow fold-back create --slug / update; help shows --slug. Evidence: go test ./... ; go run ./cmd/dot-agents workflow fold-back create --help.
