# Loop Agent Pipeline Completed-Bundle Audit

Date: 2026-04-19

Scope: spec-vs-implementation audit of the live `loop-agent-pipeline` workflow bundle after it
was marked `completed` in canonical state. This audit follows the method in
[`completed-plan-audit-analysis`](../../workflow/specs/completed-plan-audit-analysis/design.md).

## Verdict

`reopen-recommended`

The bundle should not be treated as fully verified-complete.

Two behavioral drift points remain current in source:

1. the post-task review gate still defaults to heuristic auto-accept rather than a real
   orchestrator judgment pass
2. replacement-agent retry for usage-limit / rate-limit failures is still not implemented,
   even though the completed `p11-plan-completion-mode` task says it is part of done means

However, one major finding from the earlier implementation audit is now stale:

- the old `p8-orchestrator-awareness` concern about `ralph-orchestrate` passing the same file
  as both `--project-overlay` and `--prompt-file` no longer appears true in current source

So the right disposition is not "everything in the older audit still stands." It is:

- reopen or fork follow-on work for the still-real runtime gaps
- reconcile stale historical claims so the archive stops overstating current incompleteness

That follow-on scope is now captured as two pending canonical tasks in
`loop-agent-pipeline/TASKS.yaml`:

- `p12-review-gate-hardening`
- `p13-replacement-worker-retry`

## Spec Anchors

- [PLAN.yaml](../../workflow/plans/loop-agent-pipeline/PLAN.yaml)
- [TASKS.yaml](../../workflow/plans/loop-agent-pipeline/TASKS.yaml)
- [LOOP_ORCHESTRATION_SPEC.md](../../../docs/LOOP_ORCHESTRATION_SPEC.md)
- [loop-agent-pipeline decisions.1.md](../../workflow/specs/loop-agent-pipeline/decisions.1.md)

## Implementation Anchors

- [bin/tests/ralph-review-gate](../../../bin/tests/ralph-review-gate)
- [bin/tests/ralph-pipeline](../../../bin/tests/ralph-pipeline)
- [bin/tests/ralph-orchestrate](../../../bin/tests/ralph-orchestrate)
- [commands/workflow/plan_task.go](../../../commands/workflow/plan_task.go)

## Verification Anchors

- [loop-agent-pipeline-implementation-audit.md](./loop-agent-pipeline-implementation-audit.md)
- [p7 merge-back archive](./delegate-merge-back-archive/2026-04-18/p7-post-closeout/merge-back.md)
- [p11 merge-back archive](./delegate-merge-back-archive/2026-04-19/p11-plan-completion-mode/merge-back.md)
- [tests/test-ralph-review-gate-auto.sh](../../../tests/test-ralph-review-gate-auto.sh)
- [tests/test-ralph-pipeline-review-gate.sh](../../../tests/test-ralph-pipeline-review-gate.sh)

## Confirmed Drift Points

### 1. `p7-post-closeout` is still over-claimed

The canonical task notes explicitly say the runtime still lacks the stronger spec shape:

- auto mode in `ralph-review-gate` is still artifact-presence based rather than a real
  orchestrator judgment pass
- fold-back / proposal routing is still not applied in the task-scoped review gate
- `ralph-closeout` still owns too much reconciliation logic

Direct evidence:

- [TASKS.yaml:209](../../workflow/plans/loop-agent-pipeline/TASKS.yaml:209)
- [TASKS.yaml:230](../../workflow/plans/loop-agent-pipeline/TASKS.yaml:230)

Current source matches that warning. In auto mode, `ralph-review-gate` defaults to `1` and
returns success unless `review-decision.yaml` explicitly says `reject`:

- [ralph-review-gate:5](../../../bin/tests/ralph-review-gate:5)
- [ralph-review-gate:13](../../../bin/tests/ralph-review-gate:13)
- [ralph-review-gate:49](../../../bin/tests/ralph-review-gate:49)
- [ralph-review-gate:57](../../../bin/tests/ralph-review-gate:57)
- [ralph-review-gate:78](../../../bin/tests/ralph-review-gate:78)

That is a useful guard, but it is not the "real post-task orchestrator review step with
repo-specific judgment rather than auto-accept heuristics" promised by the task's done means.

### 2. `p11-plan-completion-mode` is still over-claimed

The canonical task notes define replacement-worker behavior as part of done means:

- replace resumable failed workers for the same bundle/stage
- switch away from a rate-limited agent bin when fallback runtime options exist

Direct evidence:

- [TASKS.yaml:285](../../workflow/plans/loop-agent-pipeline/TASKS.yaml:285)
- [TASKS.yaml:295](../../workflow/plans/loop-agent-pipeline/TASKS.yaml:295)
- [TASKS.yaml:314](../../workflow/plans/loop-agent-pipeline/TASKS.yaml:314)

The archived merge-back for `p11` explicitly says this was not implemented:

- [p11 merge-back:11](./delegate-merge-back-archive/2026-04-19/p11-plan-completion-mode/merge-back.md:11)
- [p11 merge-back:13](./delegate-merge-back-archive/2026-04-19/p11-plan-completion-mode/merge-back.md:13)

Current source still supports that conclusion. `ralph-pipeline` classifies `usage_limit` and
`workspace_permissions`, but only logs and exits on stage failure. There is no retry loop that
relaunches the same bundle/stage with a different runtime:

- [ralph-pipeline:145](../../../bin/tests/ralph-pipeline:145)
- [ralph-pipeline:149](../../../bin/tests/ralph-pipeline:149)
- [ralph-pipeline:334](../../../bin/tests/ralph-pipeline:334)
- [ralph-pipeline:389](../../../bin/tests/ralph-pipeline:389)

The runtime exposes per-role `*_AGENT_BIN` knobs, but there is no control-plane logic that
changes bins after a terminal usage-limit failure. The knobs exist; the retry behavior does not.

### 3. Canonical plan status is internally contradictory

`PLAN.yaml` says `status: completed`, but the summary itself still embeds unimplemented runtime
gaps and explicitly calls out missing replacement-agent retry:

- [PLAN.yaml:4](../../workflow/plans/loop-agent-pipeline/PLAN.yaml:4)
- [PLAN.yaml:5](../../workflow/plans/loop-agent-pipeline/PLAN.yaml:5)
- [PLAN.yaml:7](../../workflow/plans/loop-agent-pipeline/PLAN.yaml:7)

This is stronger than normal doc drift. The contradiction is inside the canonical status file,
not only in the markdown narrative.

### 4. Prior archive evidence remains noisy

The earlier audit's provenance warning still stands in general. Several tasks have merge-back
history polluted by dirty-state carryover, so archive `files_changed` should not be treated as
authoritative proof of implementation shape.

Direct evidence:

- [implementation audit:15](./loop-agent-pipeline-implementation-audit.md:15)
- [implementation audit:39](./loop-agent-pipeline-implementation-audit.md:39)

This is not itself a reopen trigger, but it weakens closeout confidence.

## Corrected Prior Claim

One important claim from the 2026-04-18 implementation audit is now stale:

- it said `p8-orchestrator-awareness` was still incomplete because `ralph-orchestrate` passed the
  same file as both `--project-overlay` and `--prompt-file`

Current source now explicitly separates those bundle fields and rejects equality:

- [ralph-orchestrate:369](../../../bin/tests/ralph-orchestrate:369)
- [ralph-orchestrate:379](../../../bin/tests/ralph-orchestrate:379)
- [ralph-orchestrate:382](../../../bin/tests/ralph-orchestrate:382)
- [ralph-orchestrate:387](../../../bin/tests/ralph-orchestrate:387)

So the old `p8` finding should not be carried forward unchanged. The role-aware prompt/overlay
separation concern appears addressed in current source.

## Additional Notes

### Scoped completion state is mostly real

The command-side completion probe and pipeline loop do implement the basic scoped completion
shape:

- `workflow complete --json --plan ...` produces `actionable`, `paused`, `locked`, or `drained`
- `ralph-pipeline` loops over that state machine in scoped mode

Direct evidence:

- [plan_task.go:749](../../../commands/workflow/plan_task.go:749)
- [plan_task.go:801](../../../commands/workflow/plan_task.go:801)
- [ralph-pipeline:155](../../../bin/tests/ralph-pipeline:155)
- [ralph-pipeline:541](../../../bin/tests/ralph-pipeline:541)
- [ralph-pipeline:551](../../../bin/tests/ralph-pipeline:551)

This means `p11` is not paper-complete. The scoped completion surface exists. The gap is that
the task's stronger replacement-worker semantics are still absent.

### `locked` state behavior needs a follow-up decision

The pipeline currently treats `locked` the same as `actionable` at the top-level loop:

- [ralph-pipeline:551](../../../bin/tests/ralph-pipeline:551)

That may be intentional if "locked" means "some scoped plans are currently delegated but the
scoped set still has work worth probing." But it is a subtle contract point that should be
validated against the orchestration spec during follow-on work, because task notes often talk
about pausing plans when planning or architectural review is required.

This audit does not treat that as a confirmed defect yet because the command-side completion
state already distinguishes `paused` from `locked`, and the semantic mismatch needs a deliberate
spec readback rather than a snap judgment.

## Disposition

### Behavioral drift

Confirmed.

- `p7-post-closeout` remains behaviorally incomplete relative to its own done means.
- `p11-plan-completion-mode` remains behaviorally incomplete relative to replacement-worker
  retry/fallback requirements.

### Status drift

Confirmed.

- `PLAN.yaml` status remains `completed` while the same file documents still-missing behavior.

### Evidence/provenance drift

Confirmed.

- archive provenance is noisy and should remain advisory rather than authoritative.

### Stale prior-audit claim

Confirmed.

- the earlier `p8` prompt-file/overlay finding does not match current source and should be
  treated as resolved unless a different role-awareness issue is found later.

## Open Questions

1. Should `locked` in scoped completion mode continue to drive another pipeline pass, or should
   some lock classes break/pause earlier?
2. Does the repo want to reopen `p7` and `p11` directly, or create a narrower follow-on plan
   for review-gate hardening and replacement-worker scheduling?
3. Should the stale `p8` claim be corrected in the earlier implementation audit, or left as
   historical context with this memo as the correction artifact?

## Required Follow-Up

1. Reopen or fork follow-on work for:
   - `p12-review-gate-hardening` for real orchestrator review gating in auto/default runtime paths
   - `p13-replacement-worker-retry` for replacement-agent retry with fallback runtime selection after usage-limit exits
2. Reconcile canonical status:
   - either stop calling the bundle `completed`, or explicitly narrow the success criteria and
     split the remaining runtime gaps into a follow-on plan
3. Keep the `p8` prompt-file/overlay issue out of any reopened scope unless new evidence appears,
   because current source already separates those fields.
