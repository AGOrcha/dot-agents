# Loop Orchestrator Layer

Status: In Progress
Last updated: 2026-04-11
Depends on:
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md`
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md`

## Goal

Add a planner/orchestrator layer above the focused loop agent so work selection, safe parallel fanout, and fold-back happen through deterministic dot-agents artifacts instead of prompt improvisation.

## Decisions

- Build this as a mixed system: command surfaces + skills + existing delegation contracts + light hooks.
- Reuse canonical `PLAN.yaml` / `TASKS.yaml` and derive the dependency graph instead of hand-maintaining another graph file.
- Keep loop agents focused on one bounded slice.
- Keep high-risk shared-behavior changes behind proposal review.

## Current Slice

- [x] Write the orchestrator operating model and command/artifact direction in `docs/LOOP_ORCHESTRATION_SPEC.md`
- [x] Add `workflow next` as the first read-only task-selection primitive
- [x] Create a repo-local `orchestrator-session-start` skill that chains the existing workflow surfaces
- [x] Add `workflow plan graph` so the orchestrator can inspect cross-plan/task dependencies directly
- [ ] Add `SLICES.yaml` support for safe parallel sub-task decomposition
- [ ] Add fanout-from-slice support on top of existing delegation contracts
- [ ] Add fold-back artifacts and reconciliation flow for low-risk observations
- [ ] Extend `workflow graph query` to code-structure intents from the KG spec

## Notes

- `workflow next` should prefer canonical task state over checkpoint text.
- Phase 3 is now in progress: the first derived graph surface exists via `workflow plan graph`; slice artifacts are the next sub-slice.
- Write-scope conflict prevention already exists in `workflow fanout`; the missing layer is task selection and slice derivation.
- Hooks should validate stale or drifting orchestration state, not choose work.
