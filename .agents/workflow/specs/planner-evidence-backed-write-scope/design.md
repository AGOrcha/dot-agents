# Planner Evidence-Backed Write Scope

**Status:** design artifact

**Purpose:** define the first upgrade to the planning system so canonical workflow tasks stop treating `write_scope` as an unsupported guess and start treating it as a justified, reviewable contract backed by code-graph evidence.

**Dependency:** live graph-backed planner automation from this spec depends on the canonical [Graph Bridge Command Readiness](../../plans/graph-bridge-command-readiness/PLAN.yaml) plan, because the current repo state does not yet provide dependable `workflow graph query` and `kg bridge query` behavior for planning use. New follow-on plan/task work should be recorded through `dot-agents workflow` under `.agents/workflow/plans/` and `.agents/workflow/specs/`.

## 1. Audit Summary

### 1.1 `~/.cursor`

Observed pattern:

- Plans are rich research artifacts with strong prose, decomposition, and file references.
- The best plans often explain architecture and migration order well.
- They do not produce a durable execution contract beyond markdown checklists and narrative intent.

Planning strength:

- good at "what should happen" and "why"

Planning gap:

- no canonical answer to "why are these the exact files we must touch"

### 1.2 `~/.claude`

Observed pattern:

- Plans often go deeper on implementation sequencing and architecture tradeoffs.
- They are useful as analysis records and migration guides.
- They remain prose-first even when they include exact files or commit splits.

Planning strength:

- good at dependency ordering and implementation spine

Planning gap:

- no machine-readable task scope or evidence trail for affected-code completeness

### 1.3 `~/.codex`

Observed pattern:

- Session history and ambient suggestions preserve continuity between threads.
- The system is good at surfacing "what should be worked on next".
- It does not preserve a canonical per-task explanation of why a planned scope is complete.

Planning strength:

- good continuity and backlog hints

Planning gap:

- weak durable plan contract; scope rationale mostly lives in chat history

### 1.4 `.agents/history`

Observed pattern:

- History contains the strongest evidence of prior planning failures and recoveries.
- Several prior efforts already identified the need for better resource safety, graph integration, and canonical plan/task artifacts.
- Repeated failures are usually not "no plan existed"; they are "the plan did not encode enough operational evidence".

Important lineage:

- `planner-resource-write-safety`: planning needs explicit ownership and write-safety rules
- `crg-kg-integration`: the graph surface already exists and is no longer hypothetical
- `workflow-automation-follow-on-spec`: plan/task artifacts and graph bridge work were already promoted into roadmap form
- `loop-agent-pipeline`: canonical tasks with `write_scope` work well as execution units, but scope is still manual

### 1.5 `.agents/workflow/plans`

Observed pattern:

- This is the canonical execution surface today.
- `PLAN.yaml` and `TASKS.yaml` give the repo a real task graph, dependencies, and bounded work units.
- `write_scope` is a plain list of paths with no provenance, confidence level, or evidence of blast-radius review.

Current failure mode:

- tasks can be too broad (`commands/`, `docs/`, `.agents/workflow/plans/`)
- tasks can miss affected callers/tests because nothing ties scope authoring to graph readback
- the TDD gate checks for test-file presence in Go scope, but not whether the planned scope was complete

## 2. Problem Statement

The repo has a canonical task system, but the most important execution-boundary field, `write_scope`, is still authored as human intuition.

That creates three recurring problems:

1. **Under-scoping:** a task changes one file but misses required callers, tests, or mirrored command/docs paths.
2. **Over-scoping:** a task gets broad directory-level scope because the planner cannot prove a smaller safe boundary.
3. **Lost reasoning:** later agents can see the scope list but cannot see why those paths were chosen or what was intentionally excluded.

The graph surface already exists. The gap is not query capability; it is that planning does not preserve query results as first-class evidence.

## 3. Design Direction

### 3.1 Core rule

`write_scope` stays as the canonical execution boundary, but code-oriented tasks should have a durable evidence record explaining how that scope was derived.

### 3.2 Key idea

Back `write_scope` with a task-local evidence artifact derived from:

- code-structure queries from `dot-agents kg`
- workflow and repo-memory queries from `dot-agents workflow graph query` and `dot-agents kg query`
- planner-authored seeds, assumptions, and exclusions

Treat the evidence as two distinct lanes:

1. **Scope lane:** blast-radius and test evidence that justifies which files belong in `write_scope`
2. **Context lane:** decisions, syntheses, repo notes, and contradictions that explain how the task should be implemented and what must not be re-decided

The planner should not treat file scope as the whole contract. A worker can still fail with a perfect `write_scope` if the bundle omits the prior decision or invariant that explains what to do inside that scope.

### 3.2.1 Execution contract rule

For implementation work, the planning artifact should behave like an execution contract, not only a scope note.

At minimum it should capture:

- one concrete task goal
- the locked decisions and invariants the worker must preserve
- the bounded `write_scope`
- the required reads the worker should load before editing
- the verification target that proves the task is done
- the explicit exclusions and stop conditions that prevent adjacent architectural drift

### 3.2.2 Worker-decision boundary

Implementation slices should be delegated only when no major architectural or product decision is still hidden inside the slice.

Permitted worker autonomy should be local and explicit, for example:

- helper extraction within the same file or same bounded scope
- minor test-fixture structure
- exact assertion wording

Disallowed worker autonomy should also be explicit, for example:

- widening `write_scope`
- changing public CLI semantics or schema contracts
- introducing new persistence formats
- altering orchestration flow outside the task contract

### 3.3 Why sidecar first

Do **not** change `TASKS.yaml` schema in the first slice.

Start with a sidecar artifact so the planning upgrade can be adopted incrementally without breaking:

- existing workflow commands
- current `CanonicalTask` parsing
- old plans in history

### 3.4 Operational precondition

This spec separates two concerns that should not be conflated:

1. the **planning model** for how scope evidence should be stored and reviewed
2. the **command readiness** of graph-backed query surfaces

The planning model can be designed now.

Command-driven scope derivation is blocked until the resurrected graph-bridge readiness work proves that the relevant query surfaces return dependable results in this repo.

## 4. Proposed Artifact Model

### 4.1 Canonical location

For plan `<plan_id>` and task `<task_id>`, store evidence at:

`.agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml`

### 4.2 First-pass schema shape

Illustrative only:

```yaml
schema_version: 1
plan_id: loop-agent-pipeline
task_id: p6-fanout-dispatch
status: draft
mode: code
goal: Consume persisted app_type and verifier_sequence at runtime without inventing a second dispatch path
decision_locks:
  - Keep one canonical delegated task per workflow task; do not split impl/verifier/review into separate top-level tasks
  - Extend existing workflow verify record surfaces instead of introducing near-duplicate artifact writers
required_reads:
  - path: .agents/workflow/plans/loop-agent-pipeline/loop-agent-pipeline.plan.md
    why: active canonical plan and runtime boundary
  - path: .agents/workflow/specs/loop-agent-pipeline/decisions.1.md
    why: locked runtime and artifact decisions
seeds:
  symbols:
    - commands.workflow.RunFanout
  paths:
    - commands/workflow/delegation.go
  rationale:
    - fanout metadata exists, but runtime dispatch is still incomplete
queries:
  - tool: kg
    kind: bridge_query
    intent: symbol_lookup
    subject: commands.workflow.RunFanout
    summary:
      files:
        - commands/workflow/delegation.go
  - tool: kg
    kind: bridge_query
    intent: callers_of
    subject: commands.workflow.RunFanout
    summary:
      files:
        - commands/workflow/cmd.go
  - tool: kg
    kind: bridge_query
    intent: tests_for
    subject: commands.workflow.RunFanout
    summary:
      files:
        - commands/workflow/delegation_fanout_test.go
required_paths:
  - path: commands/workflow/delegation.go
    because:
      - symbol definition
  - path: commands/workflow/cmd.go
    because:
      - CLI entry path reaches the same code path
  - path: commands/workflow/delegation_fanout_test.go
    because:
      - graph-linked verification target
optional_paths:
  - path: docs/LOOP_ORCHESTRATION_SPEC.md
    because:
      - contract wording may need alignment if behavior changes
excluded_paths:
  - path: bin/tests/ralph-pipeline
    rationale:
      - related runtime, but not in this slice; should become a separate task if needed
provides:
  - runtime consumption of persisted verifier routing metadata
consumes:
  - task app_type metadata
  - resolved verifier_sequence from delegation bundle generation
final_write_scope:
  - commands/workflow/delegation.go
  - commands/workflow/cmd.go
  - commands/workflow/delegation_fanout_test.go
verification_focus:
  - workflow fanout and bundle tests prove runtime-visible verifier routing metadata is populated and consumed consistently
allowed_local_choices:
  - helper extraction inside commands/workflow/*
stop_conditions:
  - if implementing the task requires a new top-level delegation contract or a new artifact writer command, stop and fold back
confidence: medium
open_gaps:
  - graph has no direct coverage mapping for shell harnesses
```

### 4.3 What the sidecar means

- `required_paths`: planner believes these are in-scope to satisfy task intent
- `optional_paths`: paths likely to need review or update, but not confirmed
- `excluded_paths`: transitive candidates intentionally left out, with rationale
- `final_write_scope`: the normalized bounded set copied into `TASKS.yaml`
- `decision_locks`: already-decided constraints the worker must not reopen
- `required_reads`: exact files, notes, or specs the worker should load before editing
- `provides` / `consumes`: the provider-consumer contract that keeps adjacent slices from being rediscovered mid-implementation
- `verification_focus`: the concrete proof target, not just "run tests"
- `stop_conditions`: the point where the worker must fold back instead of making a bigger decision alone

This keeps `write_scope` concise while preserving the reasoning behind it.

## 5. Query Bundle For Code Tasks

For code-oriented tasks, the first planning pass should prefer a split query bundle.

### 5.1 Scope lane

Use these to derive candidate paths and tests:

1. `dot-agents kg build` or `dot-agents kg update`
2. `dot-agents kg bridge query --intent symbol_lookup <seed>`
3. `dot-agents kg bridge query --intent callers_of <symbol>`
4. `dot-agents kg bridge query --intent callees_of <symbol>` when downstream impact matters
5. `dot-agents kg bridge query --intent tests_for <symbol>`
6. `dot-agents kg impact <path-or-symbol>` when blast radius is broad or ambiguous

### 5.2 Context lane

Use these to derive the non-code context pack that keeps workers from improvising:

1. `dot-agents workflow graph query --intent plan_context <topic>`
2. `dot-agents workflow graph query --intent decision_lookup <topic>`
3. `dot-agents workflow graph query --intent workflow_memory <topic>` when historical task lineage matters
4. `dot-agents workflow graph query --intent contradictions <topic>` before delegation
5. `dot-agents kg query --intent repo_context <topic>`
6. `dot-agents kg query --intent related_notes <topic>`
7. `dot-agents kg query --intent source_lookup <topic>` when upstream sources or research notes matter

Not every task needs every query, but code-task planning should stop at "manual only" only when the planner records why graph or note evidence was unavailable or unhelpful.

## 6. Planner Workflow

### 6.1 Authoring flow

1. Planner identifies the seed symbol, path, or task topic.
2. Planner runs graph/context queries and captures the result summaries.
3. Planner reduces those results into `required`, `optional`, and `excluded` path sets plus `decision_locks`, `required_reads`, and `verification_focus`.
4. Planner checks whether any major implementation decision is still unresolved.
5. Planner writes the sidecar artifact.
6. Planner copies the bounded final set into `TASKS.yaml.write_scope`.
7. If the scope is still broad, planner either:
   - splits the task, or
   - records why broad scope is unavoidable

If unresolved implementation decisions remain, the planner should create or reopen a planning/design slice instead of delegating the code task as if it were ready.

### 6.2 Execution flow

Workers still execute only against `write_scope`.

The evidence artifact is for:

- task authoring
- handoff clarity
- post-change auditing
- future automation that checks whether work escaped or missed the planned blast radius

Workers should not have to reconstruct planner intent from raw query output. The sidecar exists so fanout can hand a cold-start worker a concise context pack instead of a scavenger hunt.

### 6.3 Review flow

Reviewers and orchestrators should be able to ask:

- Did the task change files outside `write_scope`?
- Did the task skip required paths from the evidence record?
- Were excluded paths later proven necessary?
- Did the worker need to reopen a supposedly locked decision?
- Did the task lack enough required reads or stop conditions to execute directly?

That is the first practical path to making planning quality measurable instead of anecdotal.

## 7. Command Surface Proposal

### 7.1 New read-only planner helper

Proposed command:

`dot-agents workflow plan derive-scope <plan_id> <task_id> [flags]`

First responsibilities:

- accept seed symbols and/or paths
- run the graph query bundle
- emit a candidate evidence sidecar
- summarize required/optional/excluded paths
- emit the reduced context pack: `decision_locks`, `required_reads`, `verification_focus`, and `stop_conditions`

Important constraint:

- this command should **not** auto-edit `TASKS.yaml` in its first version
- planner review stays explicit

### 7.2 New read-only checker

Proposed command:

`dot-agents workflow plan check-scope <plan_id> <task_id> [--changed-file ...]`

First responsibilities:

- compare actual changed files to `final_write_scope`
- warn on files changed outside scope
- warn when required paths were not touched for tasks that claimed completion
- surface the recorded exclusions

### 7.3 Future enforcement

Later, `workflow fanout` can warn or block when:

- a code task has no evidence sidecar
- the graph is healthy but no evidence was captured
- the scope is obviously broad and no rationale was recorded
- the task still contains unresolved implementation decisions
- the bundle has no verification target or no required-read context pack

That should be phased in only after read-only adoption proves useful.

## 8. Skill Upgrades

The planning problem is not only command-side. Several skills should explicitly consume the new evidence model.

### 8.1 Skills that should read or produce scope evidence

- `agent-start`
- `orchestrator-session-start`
- `plan-wave-picker`
- `review-pr`
- `review-delta`
- `self-review`

### 8.2 Expected behavior changes

- `agent-start`: when task selection begins, prefer existing scope-evidence sidecars over fresh broad scans
- `orchestrator-session-start`: when selecting the next implementation slice, derive or refresh evidence before fanout
- `plan-wave-picker`: use evidence quality as a tiebreaker when two tasks appear equally ready
- `review-pr` and `review-delta`: compare actual touched files to planned/evidenced scope
- `self-review`: flag missing tests or suspicious untouched required paths before closeout

## 9. Rollout Plan

### Phase 1: spec only

- document the model
- keep `TASKS.yaml` unchanged
- use manual sidecar authoring for early experiments
- treat graph-readiness as an explicit dependency, not an implicit assumption

### Phase 2: read-only command support

- add `workflow plan derive-scope`
- add `workflow plan check-scope`
- add tests for sidecar parsing and candidate generation
- ensure the generated artifact can carry both scope evidence and context-pack fields without forcing `TASKS.yaml` schema churn
- gate `derive-scope` on the graph-bridge readiness resurrection plan, or make it degrade honestly when graph evidence is unavailable

### Phase 3: planner integration

- let plan-authoring flows and fanout helpers reference evidence sidecars
- add warnings when code tasks have no evidence
- teach review/closeout flows to read required and excluded paths
- teach fanout readiness to reject tasks that still leave major decisions to the worker

### Phase 4: enforcement and metrics

- track how often tasks changed files outside scope
- track how often required paths were missed
- decide whether evidence should become required for code tasks

## 10. Non-Goals

- making the graph the only source of truth for planning
- auto-editing every transitive dependent file
- blocking doc-only or research-only tasks on graph availability
- replacing planner judgment with query output dumps

The planner still decides the task boundary. The graph supplies evidence, not authority.

## 11. Open Questions

1. Should the evidence sidecar remain separate long-term, or should `TASKS.yaml` eventually get an `evidence_ref` field?
2. How should confidence be scored: manual, heuristic, or based on query coverage?
3. When a task intentionally excludes a likely affected path, should that create a fold-back automatically?
4. How should shell/runtime files that are weakly represented in the graph be captured without forcing false precision?
5. Should `tests_for` evidence become part of the existing TDD gate, or stay planner-only until later?
6. Should `SLICES.yaml` eventually carry lightweight execution-contract fields directly, or should rich task/slice context remain sidecar-only?

## 12. Recommended First Implementation Slice

Start with a narrow planner-only experiment after the dependency line is clear:

1. pick one active canonical plan
2. add one evidence sidecar by hand for a code task
3. resurrect and close the graph-bridge readiness gap for the query surfaces the planner wants to use
4. add a read-only checker that compares changed files to the sidecar
5. evaluate whether the evidence was useful enough to justify command-level generation

That keeps the first slice small, proves the shape, and avoids another planning system that is more ambitious than operational.
