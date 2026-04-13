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
- `workflow delegation closeout`
  - after `workflow merge-back`, parent archives delegation + merge-back under `.agents/history/<plan-id>/delegate-merge-back-archive/…`, removes active clutter, and reconciles canonical task/plan state (`accept` completes the task; `reject` blocks the task with an optional note)
  - plan-bundle retirement under `.agents/history/<plan-id>/plan-archive/…` remains a separate follow-up (see plan Phase 7 notes)

### 2. Skill layer

Skills should compose the command surfaces into repeatable behavior:

- `orchestrator-session-start`
  - orient, compute next task, inspect graph context, decide whether to run directly or fan out
- `delegation-lifecycle`
  - bounded fanout, merge-back, and parent closeout flow
  - reads `.agents/active/delegation-bundles/<delegation_id>.yaml` as the persisted prompt/context/verification handoff (not ad hoc chat reconstruction)
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
| `.agents/active/delegation-bundles/<delegation-id>.yaml` | per-delegate worker/profile/prompt/context/verification bundle; inspectable handoff payload paired with the delegation contract |

### Delegation bundle workflow (orchestrator / delegation, Phase 8)

Phase 8 models handoff as three layers (do not collapse into a single ad hoc prompt):

1. **Global worker profile** — under `~/.agents/` (stable habits: honor `write_scope`, trust canonical tasks, verification cadence, trace discipline).
2. **Project overlay** — repo-local files (plan locations, regression matrix, validation queue, project loop guidance).
3. **Per-delegation bundle** — `.agents/active/delegation-bundles/<delegation_id>.yaml`, validated by [`schemas/workflow-delegation-bundle.schema.json`](../schemas/workflow-delegation-bundle.schema.json).

**CLI status:** `workflow fanout` **writes** the delegation bundle to `.agents/active/delegation-bundles/<delegation_id>.yaml` (same `delegation_id` as the contract’s `id` field). Repeatable flags include `--delegate-profile`, `--project-overlay`, `--prompt`, `--prompt-file`, `--context-file`, `--feedback-goal`, `--scenario-tag`, `--regression-artifact`, `--validation-queue`, `--selection-reason`, plus optional `--require-negative-coverage` / `--sandbox-mutations` for `verification.evidence_policy`. File-backed flags must refer to paths inside the repo; `--regression-artifact` may name a not-yet-created file as long as it stays under the project tree.

**Manual workflow (optional):** you can still hand-edit a bundle after fanout if you need content the CLI does not yet model; prefer re-running fanout when possible so the contract and bundle stay aligned.

**Closeout responsibilities** — bundle `closeout.worker_must` / `closeout.parent_must` line up with workflow commands as follows:

| Role | Schema token | Command |
|------|----------------|---------|
| Worker | `workflow_verify_record` | `dot-agents workflow verify record …` |
| Worker | `workflow_checkpoint` | `dot-agents workflow checkpoint …` |
| Worker | `workflow_merge_back` | `dot-agents workflow merge-back …` |
| Parent | `workflow_advance` | `dot-agents workflow advance …` |
| Parent | `workflow_delegation_closeout` | `dot-agents workflow delegation closeout --plan <id> --task <id> --decision accept|reject [--note …]` |

### Plan lifecycle

Canonical plan bundles need a terminal lifecycle, not just task-level completion.

- `draft`, `active`, and `paused` plans live under `.agents/workflow/plans/<plan-id>/`
- `completed` means execution is done but the bundle still lives in `.agents/workflow/plans/<plan-id>/` long enough for final verification, fold-back, and delegation closeout
- `archived` means the canonical bundle has been retired out of `.agents/workflow/plans/` and preserved under plan-owned history

Archive preconditions:

- every canonical task is terminal, and required work is `completed` rather than merely abandoned
- no active delegation contracts, pending merge-backs, or pending fold-back artifacts still point at the plan
- `PLAN.yaml` has already been reconciled to a terminal closeout state: `status: completed`, `current_focus_task: ""`, and final summary/notes written

Archive action:

- write a final archived copy of the bundle under `.agents/history/<plan-id>/plan-archive/<yyyy-mm-dd>/`
- preserve `PLAN.yaml`, `TASKS.yaml`, optional `SLICES.yaml`, and the human narrative plan doc when present
- stamp the archived copy's `PLAN.yaml` with `status: archived` and an updated timestamp
- remove the source bundle from `.agents/workflow/plans/<plan-id>/` so active-plan discovery surfaces no longer treat it as a live canonical plan

This keeps `.agents/workflow/plans/` reserved for live canonical plans while `.agents/history/<plan-id>/` becomes the durable record for completed work.

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

- `workflow graph query` forwards **code-structure intents** (see table below; includes `symbol_lookup`, `impact_radius`, `change_analysis`, `tests_for`, `callers_of`, `callees_of`, `community_context`, `symbol_decisions`, `decision_symbols`) to `kg bridge query` — same behavior as invoking `dot-agents kg bridge query --intent …` from the repo.
- keep `kg changes`, `kg impact`, `kg communities`, and `kg flows` as direct escape hatches

Practical rule:

- use graph-first lookup for symbols, callers/callees, blast radius, tests, and decision links
- fall back to `rg` only when the graph is absent, stale, or the question is raw text shaped

### KG-First Query Routing

`workflow graph query` distinguishes two intent families. Summary:

| Intent | Routing | Backing |
|--------|---------|---------|
| `plan_context`, `decision_lookup`, `entity_context`, `workflow_memory`, `contradictions` | Workflow graph bridge (requires `.agents/workflow/graph-bridge.yaml` with `enabled: true`) | `LocalGraphAdapter` over configured `graph_home` (KG notes tree) |
| `symbol_lookup`, `impact_radius`, `change_analysis`, `tests_for`, `callers_of`, `callees_of`, `community_context`, `symbol_decisions`, `decision_symbols` | Subprocess: same as `dot-agents kg bridge query --intent <intent> …` | CRG / code graph via `kg bridge` |

Details:

1. **Workflow / KG-note intents** — served when the bridge config is enabled, using `graph_home` and `LocalGraphAdapter`.

2. **Code-structure intents** — **not** handled on the workflow-local filesystem bridge path. The CLI forwards to the same entry point as a manual invocation:

   `dot-agents kg bridge query --intent <intent> <query>`

   The child process uses the project working directory as `Dir`, connects stdout and stderr to the parent, and receives the global `--json` flag when the parent was run with `--json` (so JSON output shape matches `kg bridge query`).

   The workflow-local `--scope` flag applies to note-oriented queries on the filesystem bridge only; it is not passed through to the kg subprocess today. If `kg bridge query` gains a compatible `--scope`, the forwarder can pass it through without duplicating semantics here.

Orchestrator agents should prefer `workflow graph query` for both families so dispatch stays centralized. Use `grep` / `glob` only when the graph is absent, stale, or the question is raw text shaped.

This keeps a single implementation for code-structure queries (CRG / structural graph behavior in `kg bridge`) while leaving note-oriented workflow queries on the filesystem bridge.

## Initial Product Slices

Phase 3B/3C correspond to items 4 and 5 below: define the canonical slice artifact first, then gate delegation on fanout readiness checks.

1. Ship `workflow next` as the first deterministic selection primitive.
2. Add `orchestrator-session-start` skill that chains orient -> next -> graph readback -> fanout decision.
3. Add plan/task graph rendering before any auto-slicing.
4. Add read-first `SLICES.yaml` support through `workflow slices` and graph rendering.
5. Add slice artifacts and fanout-from-slice readiness checks.
6. Add fold-back reconciliation for loop observations and testing-matrix updates.
7. Route code-structure questions through `workflow graph query` → `kg bridge` (implemented); keep extending `kg bridge` capabilities as CRG evolves.
8. Add delegation closeout so completed delegation and merge-back artifacts reconcile cleanly into task and plan state.
9. Add per-delegate prompt/context bundle inputs so fanout can hand sub-agents reproducible prompts and files.

### Phase 8: Delegation bundle direction

Phase 8 should formalize delegation handoff as a three-layer model rather than treating one giant prompt as the interface.

#### 1. Global worker profile

Reusable, user-local behavior under `~/.agents/`:

- bounded worker discipline: honor `write_scope`, trust canonical task state, avoid mutating shared workflow state directly
- verification discipline: run focused tests first, then broader regression only as justified
- trace discipline: record a concrete `feedback_goal`, use scenario tags, and classify evidence/results
- closeout discipline:
  - worker records `workflow verify record`
  - worker records `workflow checkpoint`
  - worker returns `workflow merge-back`
  - parent advances canonical task state
  - parent runs delegation closeout / archive once accepted

This profile should be stable across repos. It describes how a loop worker behaves, not what one repo is working on.

#### 2. Project overlay

Repo-local guidance layered on top of the worker profile:

- plan and loop-state locations
- preferred verification surfaces
- quality gates and hook expectations
- regression matrix path
- higher-layer validation queue path
- project-specific scenario families and verification heuristics

This is where files like `.agents/active/active.loop.md` or repo-specific loop prompts belong once trimmed into project overlays instead of full worker definitions.

#### 3. Delegation bundle

Per-delegation persisted payload at `.agents/active/delegation-bundles/<delegation_id>.yaml` (written by `workflow fanout` with Phase 8 flags — see **Delegation bundle workflow** under *Canonical Artifact Direction*):

- chosen plan/task/slice and selection reason
- owner plus worker profile reference
- project overlay file references
- prompt text and prompt files
- context files
- verification plan, feedback goal, scenario tags, and closeout expectations

This bundle is the transport/persistence layer for a specific delegation, not the definition of the worker itself.

### Phase 8: Reusable testing additions

The global loop-worker profile should carry six reusable testing/verification additions that are not repo-specific:

1. `feedback_goal` — every delegated iteration states the concrete question the evidence run must answer.
2. `scenario_tags` with stable coverage families and paired-state guidance.
3. `regression_matrix` support — a repo may point at one or more durable matrix artifacts for scenario/run-variant tracking.
4. `higher_layer_validation_queue` support — queue features that are code-complete and automated-check complete but still deserve manual/live validation.
5. evidence/result `classification` taxonomy such as `ok`, `ok-empty`, `ok-warning`, `retry-recovered`, `impl-bug`, `tool-bug`, `missing-feature`, and `blocked`.
6. `sandbox_policy` for destructive or stateful verification, so a worker can prove mutating behavior without touching the user's live home/project state.

Global loop-worker behavior should also require negative-path coverage whenever the delegated change introduces new failure modes.

### Phase 8: Canonical artifact and schema

Use a sibling artifact rather than overloading the core delegation contract:

- contract file: `.agents/active/delegation/<parent_task_id>.yaml` (unique delegation id is the contract’s `id` field)
- bundle file: `.agents/active/delegation-bundles/<delegation_id>.yaml` (`delegation_id` should match the contract `id`)
- schema: `schemas/workflow-delegation-bundle.schema.json`

The schema should be embedded into the binary alongside the other repo-local schemas so later runtime validation can bind directly to the canonical artifact contract.

### Phase 8: Candidate command shape

```bash
dot-agents workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --owner <delegate-name> \
  --write-scope "commands/,internal/platform/" \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --feedback-goal "Does fold-back create/list persist small and proposal routes cleanly?" \
  --scenario-tag canonical-plan-present \
  --scenario-tag workflow-fold-back-small \
  --regression-artifact .agents/workflow/testing-matrix.yaml \
  --validation-queue .agents/active/live-testing-queue.md \
  --prompt "Implement Phase 6 only; keep write-scope tight" \
  --prompt-file .agents/prompts/loop-worker.project.md \
  --context-file .agents/workflow/plans/loop-orchestrator-layer/TASKS.yaml \
  --context-file docs/LOOP_ORCHESTRATION_SPEC.md
```

### Phase 8: Bundle example

```yaml
schema_version: 1
delegation_id: del-phase-6-20260412T213000Z
plan_id: loop-orchestrator-layer
task_id: phase-6-fold-back-reconciliation
slice_id: ""
owner: worker-a

worker:
  profile: loop-worker
  profile_version: 1
  project_overlay_files:
    - .agents/active/active.loop.md

selection:
  selected_by: orchestrator-session-start
  selected_at: "2026-04-12T21:30:00Z"
  reason: "first pending unblocked canonical task"

scope:
  write_scope:
    - commands/workflow.go
    - commands/workflow_test.go
  constraints:
    - "Do not mutate shared workflow state outside the delegated task"

prompt:
  inline:
    - "Implement only the selected task."
  prompt_files:
    - .agents/prompts/loop-worker.project.md

context:
  required_files:
    - .agents/workflow/plans/loop-orchestrator-layer/PLAN.yaml
    - .agents/workflow/plans/loop-orchestrator-layer/TASKS.yaml
  optional_files:
    - .agents/active/loop-state.md
    - docs/LOOP_ORCHESTRATION_SPEC.md

verification:
  feedback_goal: "Does fold-back create/list persist small and proposal routes cleanly?"
  scenario_tags:
    - canonical-plan-present
    - workflow-fold-back-small
  regression_artifacts:
    - .agents/workflow/testing-matrix.yaml
  higher_layer_validation_queue: .agents/active/live-testing-queue.md
  focused_commands:
    - go test ./commands
  regression_commands:
    - go test ./...
  evidence_policy:
    require_negative_coverage: true
    classification_required: true
    sandbox_mutations: true
    primary_chain_max: 3

closeout:
  worker_must:
    - workflow_verify_record
    - workflow_checkpoint
    - workflow_merge_back
  parent_must:
    - workflow_advance
    - workflow_delegation_closeout
```

### Phase 8: Rules

- the global loop-worker profile stays reusable; project overlays and delegation bundles must not fork that behavior ad hoc
- prompt/context inputs must be delegation-specific so different sub-agents can receive different bundles
- repeatable flags are preferable to comma-separated prompt/context strings
- the bundle must be inspectable after fanout so the handoff can be reproduced and audited
- the worker should read from the persisted bundle rather than reconstructing context from memory
- regression matrix and validation queue references are optional at the schema level but should be supported consistently where a repo uses them
- negative-path coverage is required when the delegated change introduces new failure modes
- worker closeout and parent closeout responsibilities must remain distinct

### Phase 8: Acceptance shape

- a parent can choose a stable worker profile and add one or more repo-local project overlays
- a parent can supply inline prompts, prompt files, and multiple context files
- a delegated worker receives reproducible verification metadata, not just prose instructions
- two different delegated sub-agents can receive different prompt/context/testing bundles without colliding
- the resulting bundle is inspectable from repo artifacts and backed by an embedded schema
