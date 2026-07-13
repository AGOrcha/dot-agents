# Full-loop OMP orchestration runtime

## Goal

Implement an OMP-native outer loop that drives conflict-free eligible tasks across all active dot-agents workflow plans while preserving da as the authority for slot accounting, fanout lineage, delegation lifecycle, fold-backs, proposals, iteration records, and scoring.

The runtime composes two layers:

1. **Outer loop:** selects cross-plan waves through `da workflow slots` and `da workflow eligible`, fans tasks out, launches one inner pipeline per selected task, waits at a wave barrier, and serially reconciles outcomes.
2. **Per-task inner pipeline:** resolves the task's execution profile, implements in an isolated worktree, runs configured verifiers and reviewer lenses, and emits READY/FOLD-BACK plus merge-back/refinement artifacts.

## Decisions

### OMP is the only model harness

All Claude- and GPT-family models run through OMP. The runtime must never infer that selecting a GPT-family model means invoking the Codex CLI or inheriting Codex CLI sandbox/permission-whitelist behavior. Direct Codex CLI guidance remains relevant only to code paths that actually launch that binary.

### Model selection is explicit and profile-owned

Every resolved stage profile carries both:

- `model`: concrete OMP model identifier;
- `model_family`: semantic family used to prove diversity constraints.

High-volume implementation and routine verification/review default to Claude-family models to use the available capacity. A blocking cross-family adversarial lens is explicitly GPT-family through OMP. Diversity is attached to the named lens (`cross-harness-adversarial` until a later clean rename), never to numeric reviewer slot 4 or an assumed list order.

### da owns workflow state

Workers emit artifacts only. Main serializes canonical plan/task mutations, delegation readback/closeout, fold-back routing, proposal creation, checkpointing, scoring, and fresh slot/eligibility reads.

### Migration is gated

Do not move active plan/task state to `refs/agents/state` merely because the orchestration runtime validates. Migration additionally requires `git-ref-work-backend/document-and-default-git-ref`, including read-from-ref behavior, CAS writes, per-task state, WorkStore adoption, and rollback. Until then, use temporary transition instructions and keep current canonical workflow state intact.

## Task graph

1. **explicit-model-routing**
   - Extend `StageProfile` and resolved prompt output with `model` and `model_family`.
   - Validate family/model consistency and preserve backward compatibility through explicit defaults.
   - Configure Claude-volume stages and the GPT-family adversarial gate.
   - Correct documentation that equates cross-family review with a different agent harness.

2. **per-task-omp-pipeline**
   - Promote the validated state-ref draft under `.agents/workflow/runtime/full-loop/`.
   - Make every stage's OMP model explicit.
   - Handle the configured verifier/lens cardinalities without silently dropping the fifth daemon/web lens.
   - Emit READY/FOLD-BACK, merge-back, proof, and refinement candidates only.

3. **n-plan-omp-driver**
   - Query slots and cross-plan eligibility from repository-HEAD da.
   - Select `max_batch` within available capacity.
   - Fan out every selected task with da lineage and write-scope checks.
   - Launch one isolated `TASK=<plan>/<task> omp-swarm ...` process per task.
   - Enforce a wave barrier and deterministic result collection.
   - Invoke no model-vendor CLI directly.

4. **lifecycle-refinement-reconcile**
   - Run one serialized OMP reconciliation pipeline after each wave.
   - Convert inner artifacts into da delegation lifecycle transitions.
   - Route task/plan fold-backs idempotently by slug.
   - Produce cross-cutting observation/proposal candidates without self-applying them.
   - Record checkpoint/hook outcomes and score iterations.

5. **controlled-multiplan-validation**
   - Exercise fixture plans and a fake `omp-swarm` to prove slot limits, conflicts, waves, model routing, bounded task-local fold-back re-entry, final-reject slot release, and serialized state writes.
   - Parse and semantically validate both swarm definitions.
   - Run a controlled OMP smoke only after deterministic tests pass.

6. **state-ref-transition-instructions**
   - Add temporary, bounded orchestrator guidance for detecting the active backend, using da-only writes, verifying ref/CAS state, and rolling back.
   - Mark removal conditions explicitly.

7. **migrate-workflow-state-ref**
   - Cross-plan gated on `git-ref-work-backend/document-and-default-git-ref` and controlled runtime validation.
   - Snapshot source state and target ref.
   - Migrate all active plan triads/task state atomically.
   - Compare projected views, prove rollback, then switch the backend in one cutover.

## Acceptance criteria

- One command drives eligible tasks across multiple active plans without exceeding the da slot budget.
- Selection-time and fanout-time write-scope conflict guards remain effective.
- Each inner stage resolves an explicit OMP model and model family.
- Routine volume is Claude-family; at least one blocking adversarial gate is GPT-family.
- No GPT-family stage launches or depends on Codex CLI sandbox/permission flags.
- Every configured verifier and lens is either executed or explicitly skipped with a reason; no list item is lost due to fixed numeric slots.
- Retryable fold-back re-enters the executor inside the active delegation for at most `target_count` iterations and continues holding/re-acquiring its slot; an exhausted final rejection closes the delegation to `blocked` and frees the slot for explicit replan. Proposal emission does not interrupt the active worker.
- Workers do not mutate canonical workflow state concurrently.
- Controlled multi-plan validation proves the wave barrier and serialized reconciliation.
- Active workflow-state migration is not performed before the git-ref backend gate is complete.
- Temporary transition instructions define backend detection, rollback, and their own removal condition.

## Verification

- Focused Go tests for config parsing/schema/defaulting and prompt resolution.
- JSON schema validation of `.agentsrc.json` and model-routing fields.
- Shell tests for the outer driver using fixture da/OMP executables.
- Swarm parser plus semantic validation for inner and reconciliation pipelines.
- Controlled multi-plan smoke with non-overlapping disposable tasks.
- State-ref migration rehearsal comparing source and projected target views before any cutover.
