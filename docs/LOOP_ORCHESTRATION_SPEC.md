# Loop Orchestration Spec

Status: Draft
Last updated: 2026-04-12
Related:
- `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md`
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md`

## Purpose

Define the next layer above the focused loop agent: a read-mostly orchestrator that selects the next safe unit of work, derives bounded delegation slices, and folds useful observations back into plans, matrices, and proposal review.

The loop agent stays narrow. The orchestrator decides where to point it.

## Problem

The repo now has the building blocks for bounded coordination:

- canonical plans and tasks
- delegation contracts with `write_scope`
- merge-back artifacts
- workflow orient/status/health surfaces
- KG and CRG read paths

What is still missing is the layer that turns those primitives into a stable operating model:

- choose the next task from canonical state instead of stale checkpoint text
- derive safe slices before fanout so agents do not collide
- route small loop observations into the right durable artifact
- route larger cross-cutting changes into the proposal queue
- prefer graph-backed understanding over broad repo scans when the question is code-structure shaped

## Decision

The orchestrator should be a mixed system, not a single new super-agent.

### 1. Command layer

`dot-agents` owns deterministic read/write surfaces:

- `workflow next`
  - recommend the next actionable canonical task
- `workflow plan graph`
  - derive a dependency graph across plans, tasks, and blockers
- `workflow slices`
  - read canonical `SLICES.yaml` slice artifacts now, then later derive candidate parallel slices for a task
- `workflow fanout`
  - keep as the bounded write-scope contract creator
- `workflow merge-back`
  - keep as the delegate return artifact writer
- `workflow fold-back`
  - fold approved low-risk observations into plan notes, matrices, or lessons
- future: `workflow delegation closeout` / reconciliation surface
  - consume completed merge-backs, archive processed active artifacts, and reconcile canonical task/plan state

### 2. Skill layer

Skills should compose the command surfaces into repeatable behavior:

- `orchestrator-session-start`
  - orient, compute next task, inspect graph context, decide whether to run directly or fan out
- `delegation-lifecycle`
  - remains the bounded fanout and merge-back flow
  - future: consume delegation-specific prompt/context bundle inputs instead of reconstructing handoff text ad hoc
- `iteration-close`
  - remains the persist and proposal closeout flow

### 3. Agent layer

Recommended operating roles:

- Orchestrator / product-owner / architect agent
  - read-mostly
  - chooses task, slices work, decides whether human review is required
- Loop worker agent
  - executes one bounded slice
  - stays focused on implementation and verification
- Optional verifier agent
  - validates merge-back output or higher-risk changes before task completion

### 4. Hook layer

Hooks should stay lightweight and non-authoritative:

- detect stale delegation contracts
- warn on pending merge-backs
- flag canonical task drift versus loop-state
- flag observations that were not folded back anywhere

Hooks should not choose work, mutate plans, or fan out agents.

## Canonical Artifact Direction

The orchestrator should reuse existing canonical artifacts where possible.

### Keep as-is

- `.agents/workflow/plans/<plan-id>/PLAN.yaml`
- `.agents/workflow/plans/<plan-id>/TASKS.yaml`
- `.agents/active/delegation/<task-id>.yaml`
- `.agents/active/merge-back/<task-id>.md`

### Additive artifacts

| Path | Purpose |
|------|---------|
| `.agents/workflow/plans/<plan-id>/SLICES.yaml` | read-first canonical slice artifact for Phase 3B, plus optional sub-task decomposition and fanout-readiness inputs for safe parallel work |
| `.agents/workflow/testing-matrix.yaml` | canonical verification targets and scenario coverage |
| `.agents/active/fold-back/<id>.yaml` | pending low-risk observation to reconcile into plans, matrix, or lessons |
| future: delegation prompt bundle artifact or contract fields | inspectable prompt/context payload handed to one delegated sub-agent |

### Graph model

The spec dependency graph should be derived, not hand-maintained.

Inputs:

- `PLAN.yaml`
- `TASKS.yaml`
- optional `SLICES.yaml`
- active delegations
- merge-back artifacts

Derived graph edges:

- plan -> plan dependency
- plan -> task containment
- task -> task dependency
- task -> slice containment
- slice -> write scope
- slice -> delegation contract
- task -> merge-back artifact

This avoids creating another manual source of truth that would drift.

## Selection Model

`workflow next` is the first orchestrator primitive.

Selection order:

1. active canonical plan with current focus task already `in_progress`
2. active canonical plan with another `in_progress` unblocked task
3. active canonical plan with current focus task `pending` and unblocked
4. active canonical plan with first `pending` unblocked task

Guardrails:

- skip tasks with active delegations
- skip tasks whose dependencies are not completed
- prefer canonical tasks over checkpoint `next_action`

## Slice Model

`SLICES.yaml` should support bounded parallel work below one canonical task.

Suggested slice fields:

- `id`
- `parent_task_id`
- `title`
- `summary`
- `depends_on`
- `write_scope`
- `verification_focus`
- `owner`
- `status`

Slice creation rules:

- derive from disjoint `write_scope`
- prefer file-tree or subsystem boundaries
- allow CRG communities and impact radius to refine boundaries
- treat `SLICES.yaml` as the canonical slice artifact and `workflow fanout` as the readiness gate that decides whether a slice is safe to delegate
- do not slice a task until the command layer can prove scopes do not overlap

## Fold-Back Policy

Loop agents produce useful observations that should not remain stranded in loop-state forever.

### Auto-fold candidates

Small, repo-local, low-risk items:

- testing matrix additions
- plan note clarifications
- lesson updates
- scenario tag or trace hygiene

### Proposal-required candidates

Bigger or shared-behavior changes:

- skill behavior changes
- hook/rule changes
- repo-wide workflow defaults
- cross-repo conventions

Those should become review proposals under `~/.agents/proposals/`.

## KG / CRG Direction

The orchestrator should default to graph-backed understanding when the question is code-structure shaped.

Near-term command direction:

- extend `workflow graph query` to support code intents from the KG spec:
  - `symbol_lookup`
  - `impact_radius`
  - `change_analysis`
  - `tests_for`
  - `community_context`
  - `symbol_decisions`
- keep `kg changes`, `kg impact`, `kg communities`, and `kg flows` as direct escape hatches

Practical rule:

- use graph-first lookup for symbols, callers/callees, blast radius, tests, and decision links
- fall back to `rg` only when the graph is absent, stale, or the question is raw text shaped

## Initial Product Slices

Phase 3B/3C correspond to items 4 and 5 below: define the canonical slice artifact first, then gate delegation on fanout readiness checks.

1. Ship `workflow next` as the first deterministic selection primitive.
2. Add `orchestrator-session-start` skill that chains orient -> next -> graph readback -> fanout decision.
3. Add plan/task graph rendering before any auto-slicing.
4. Add read-first `SLICES.yaml` support through `workflow slices` and graph rendering.
5. Add slice artifacts and fanout-from-slice readiness checks.
6. Add fold-back reconciliation for loop observations and testing-matrix updates.
7. Expand workflow graph bridge to code-structure intents so the orchestrator can ask richer questions without repo spelunking.
8. Add delegation closeout so completed delegation and merge-back artifacts reconcile cleanly into task and plan state.
9. Add per-delegate prompt/context bundle inputs so fanout can hand sub-agents reproducible prompts and files.
