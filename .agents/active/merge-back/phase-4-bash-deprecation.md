---
schema_version: 1
task_id: phase-4-bash-deprecation
parent_plan_id: platform-dir-unification
title: Phase 4 — Delete orphaned bash command and platform files
summary: Removed orphaned bash command/platform layer, rewired bootstrap shell entrypoints to direct Go CLI exec, and updated verify smoke coverage for the post-bash surface.
files_changed:
    - .agents/active/delegation/phase-3-plan-graph-and-slices.yaml
    - .agents/active/delegation/phase-5-kg-first-understanding.yaml
    - .agents/active/delegation/phase-e-postgres.yaml
    - .agents/active/merge-back/phase-3-plan-graph-and-slices.md
    - .agents/active/merge-back/phase-5-kg-first-understanding.md
    - .agents/workflow/plans/crg-kg-integration/PLAN.yaml
    - .agents/workflow/plans/crg-kg-integration/TASKS.yaml
    - .agents/workflow/plans/crg-kg-integration/crg-kg-integration.plan.md
    - .agents/workflow/plans/loop-orchestrator-layer/PLAN.yaml
    - .agents/workflow/plans/loop-orchestrator-layer/SLICES.yaml
    - .agents/workflow/plans/loop-orchestrator-layer/TASKS.yaml
    - .agents/workflow/plans/loop-orchestrator-layer/loop-orchestrator-layer.plan.md
    - .agents/workflow/plans/platform-dir-unification/PLAN.yaml
    - .agents/workflow/plans/platform-dir-unification/TASKS.yaml
    - .agents/workflow/plans/platform-dir-unification/platform-dir-unification.plan.md
    - .agentsrc.json
    - .gitignore
    - commands/add.go
    - commands/init.go
    - commands/kg.go
    - commands/kg_test.go
    - commands/workflow.go
    - commands/workflow_test.go
    - docs/LOOP_ORCHESTRATION_SPEC.md
    - docs/PLATFORM_DIRS_DOCS.md
    - docs/SCHEMA_FOLLOWUPS.md
    - internal/platform/opencode.go
    - internal/platform/resource_plan.go
    - internal/platform/resource_plan_test.go
    - scripts/verify.sh
    - src/bin/dot-agents
    - src/lib/commands/add.sh
    - src/lib/commands/explain.sh
    - src/lib/commands/import.sh
    - src/lib/commands/init.sh
    - src/lib/commands/install.sh
    - src/lib/commands/refresh.sh
    - src/lib/commands/status.sh
    - src/lib/platforms/claude-code.sh
    - src/lib/platforms/codex.sh
    - src/lib/platforms/cursor.sh
    - src/lib/platforms/github-copilot.sh
    - src/lib/platforms/opencode.sh
    - src/lib/utils/core.sh
    - src/lib/utils/resource-restore-map.sh
verification_result:
    status: pass
    summary: Worker verified no remaining source references to deleted bash files, bash syntax checks passed, go test ./... passed, and scripts/verify.sh passed; parent should advance canonical task to completed.
integration_notes: Worker verified no remaining source references to deleted bash files, bash syntax checks passed, go test ./... passed, and scripts/verify.sh passed; parent should advance canonical task to completed.
created_at: "2026-04-12T15:21:54Z"
---

## Summary

Removed orphaned bash command/platform layer, rewired bootstrap shell entrypoints to direct Go CLI exec, and updated verify smoke coverage for the post-bash surface.

## Integration Notes

Worker verified no remaining source references to deleted bash files, bash syntax checks passed, go test ./... passed, and scripts/verify.sh passed; parent should advance canonical task to completed.
