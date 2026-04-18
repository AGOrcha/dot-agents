---
schema_version: 1
task_id: p3b-unit-verifier
parent_plan_id: loop-agent-pipeline
title: Unit verifier surface and result contract
summary: Added .agents/prompts/verifiers/unit.project.md (D12 scoped tests + go test ./... -race -count=1 -timeout=300s, unit.result.yaml contract). Updated LOOP_ORCHESTRATION_SPEC Phase 8 repo prompt list for unit verifier role.
files_changed:
    - .agents/prompts/verifiers/unit.project.md
    - docs/LOOP_ORCHESTRATION_SPEC.md
verification_result:
    status: pass
    summary: go test ./... (full suite) green at commit a16642e. Used ./bin/dot-agents for verify/checkpoint/merge-back because go run fails on local WIP in commands/workflow.go.
integration_notes: go test ./... (full suite) green at commit a16642e. Used ./bin/dot-agents for verify/checkpoint/merge-back because go run fails on local WIP in commands/workflow.go.
created_at: "2026-04-18T12:45:20Z"
---

## Summary

Added .agents/prompts/verifiers/unit.project.md (D12 scoped tests + go test ./... -race -count=1 -timeout=300s, unit.result.yaml contract). Updated LOOP_ORCHESTRATION_SPEC Phase 8 repo prompt list for unit verifier role.

## Integration Notes

go test ./... (full suite) green at commit a16642e. Used ./bin/dot-agents for verify/checkpoint/merge-back because go run fails on local WIP in commands/workflow.go.
