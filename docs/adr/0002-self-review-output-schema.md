# ADR-0002: Self-review output destination and schema

**Status:** accepted
**Date:** 2026-05-03
**Owners:** dot-agents
**Related:** [ADR-0001](0001-adopt-architecture-decision-records.md) (ADR conventions); [ADR-0003](0003-self-review-fire-ordering.md) (fire ordering); [ADR-0004](0004-execution-telemetry-schema-seed.md) (telemetry seed); [ADR-0005](0005-restore-kg-context-in-self-review.md) (kg-context restoration); [`agent-context-resolution-architecture.md` §1.6](../../.agents/proposals/agent-context-resolution-architecture.md) (execution telemetry pillar); [`self-review-iteration-close-wiring` plan t1](../../.agents/workflow/plans/self-review-iteration-close-wiring/) (this ADR's source)

## Context

The self-review skill currently runs in chat and persists nothing.
The iter-log v2 schema requires a populated `review` block, but no
caller writes the input that `mergeReviewIterLog` reads from
(`commands/workflow/iter_log.go:397-426`). The audit at
`agent-context-resolution-architecture.md` §6.5 identified this as
the dead-coded gap that the `self-review-iteration-close-wiring`
plan exists to close.

Two questions need a load-bearing answer before t2 can extend the
skill or t3 can wire iteration-close:

1. **Where does self-review's structured output live on disk?**
2. **What is the envelope schema?**

The path must be one that `mergeReviewIterLog` already reads (or
trivially can) and that the `verify record --kind review` writer
already produces. Inspection of `commands/workflow/verification.go`
(lines 100-176) shows the existing path:
`.agents/active/verification/<task_id>/review-decision.yaml`. That
file is the input `mergeReviewIterLog` reads. Choosing any other
path would require new writer code in workflow.

The schema must be both:
- compatible with what `mergeReviewIterLog` extracts today
  (overall_decision, phase decisions, reviewer notes);
- forward-compatible with the §1.6 execution-telemetry trace
  envelope (resource_type/resource_id/invoked_at/outcome/
  post_invocation/improvement_signals) so that ADR-0004 can promote
  this artifact to "first concrete telemetry trace" without a
  schema migration.

## Decision

**Output destination:** `.agents/active/verification/<task_id>/review-decision.yaml`.

This is the path the existing `verify record --kind review` writer
produces and `mergeReviewIterLog` reads. Self-review writes to the
same path, using the same schema, so the existing workflow pipeline
absorbs the new caller without any new writer code.

**Schema (envelope):**

```yaml
schema_version: 1

# ── §1.6 telemetry envelope (forward-compatible) ────────────────
resource_type: skill
resource_id: self-review
invoked_at: <RFC3339>
invoked_by: <agent_role>          # e.g. orchestrator | loop-worker
plan_id: <if applicable>
task_id: <required for review-kind>

# ── existing verify-record-kind:review fields ───────────────────
overall_decision: accept | reject | escalate
phase1_decision: accept | reject | escalate
phase2_decision: accept | reject | escalate    # optional
reviewer_notes: <free text; markdown ok>
failed_gates: [<slug>, ...]                    # optional
escalation_reason: <required iff overall == escalate>

# ── §1.6 outcome + post_invocation + improvement_signals ────────
outcome:
  declared: success | failure | partial
  agent_self_assessment: <free text>

post_invocation:
  agent_actions_after_skill_returned: [<text>, ...]
  user_corrections: [<text>, ...]
  retries_in_loop: <int>

improvement_signals:
  missing_in_skill: [<text>, ...]
  redundant_in_skill: [<text>, ...]
  tooling_gap: { present: <bool>, note: <text> }
  script_gap: { present: <bool>, note: <text> }
  instruction_gap: { present: <bool>, note: <text> }
```

The top half (telemetry envelope) is what ADR-0004 elevates to the
"first concrete trace shape." The middle (existing verify-record
fields) is what `mergeReviewIterLog` reads. The bottom half
(improvement signals) is what the future score formula in
agent-context-resolution-architecture.md §3 reads.

**Worked example** (under §1.6 trace seed; minimal review acceptance):

```yaml
schema_version: 1
resource_type: skill
resource_id: self-review
invoked_at: "2026-05-04T10:30:00Z"
invoked_by: orchestrator
plan_id: example-plan
task_id: example-task

overall_decision: accept
phase1_decision: accept
phase2_decision: accept
reviewer_notes: |-
  All seven self-review axes pass on the staged diff.
  Code-quality: clear naming; no anti-patterns. Security: no hardcoded
  secrets. Performance: O(n) substitution; no algorithmic concerns.
failed_gates: []

outcome:
  declared: success
  agent_self_assessment: "Diff is small and mechanical; no surprises."

post_invocation:
  agent_actions_after_skill_returned: []
  user_corrections: []
  retries_in_loop: 0

improvement_signals:
  missing_in_skill: []
  redundant_in_skill: []
  tooling_gap: { present: false, note: "" }
  script_gap: { present: false, note: "" }
  instruction_gap: { present: false, note: "" }
```

**Path resolution:** when self-review fires inside iteration-close
(see ADR-0003 for ordering), the `<task_id>` is the iteration's task
context. When self-review fires standalone (manual invocation, not
inside iteration-close), `<task_id>` may be a synthesized id like
`adhoc-<RFC3339>` and that artifact is not consumed by
`mergeReviewIterLog` (which only fires when iteration-close
triggers it). Standalone self-review still writes the file for
future telemetry mining; it just doesn't drive the iter-log review
block in that mode.

## Consequences

**Easier:**

- Zero new writer code: existing `verify record --kind review` path
  already lands the file at the chosen location with the chosen
  shape. Self-review just emits the same YAML.
- `mergeReviewIterLog` reads the new file unchanged — no
  modifications to `commands/workflow/iter_log.go`.
- ADR-0004 promotes this artifact to "first telemetry trace" without
  schema migration.
- Forward-compatibility: when hooks/subagents/rules need their own
  traces, they reuse this envelope (resource_type/_id distinguishes
  them).

**Harder:**

- Self-review skill must learn the YAML envelope. The `output-format.md`
  instruction module under self-review's instructions/ documents this
  with the worked example above. t2 implements.
- Standalone self-review (manual invocation outside iteration-close)
  produces a file that `mergeReviewIterLog` doesn't read — a future
  telemetry pipeline must learn to mine these orphan files.

**New risks:**

- Schema drift: if `mergeReviewIterLog`'s expected shape changes
  later, self-review's output and verify-record's output drift apart.
  Mitigation: a small schema test (`commands/workflow/review_decision_schema.go`
  exists) — the existing schema validation already covers this for
  verify-record output; self-review's output goes through the same
  validation when iteration-close hands it to the verify-record CLI.
- Name collision: two iteration-close turns within the same plan
  for the same task_id would overwrite the same file. Mitigation:
  iteration-close already routes through advance/merge-back which
  serializes per-task; concurrent same-task self-reviews are not a
  real condition.

**Locked-in:**

- The path `.agents/active/verification/<task_id>/review-decision.yaml`.
  Changing it later requires updating `mergeReviewIterLog`,
  `verify record --kind review`, AND self-review — a coordinated
  three-place change.
- The envelope shape. Forward-compatibility means new fields can
  be added at the end of `improvement_signals` without breaking
  existing readers; reordering or renaming requires a schema
  version bump.

## Alternatives considered

- **New path: `.agents/active/self-review/<task_id>.yaml`** —
  rejected. Would require new writer code AND a new reader path
  in `mergeReviewIterLog`; doubles the integration surface.
- **Inline review block written directly into `iter-N.yaml`** —
  rejected. Skips the verify-record flow that `mergeReviewIterLog`
  expects; would force iteration-close to call a different CLI
  path; loses the standalone-self-review use case.
- **Different envelope shape (no telemetry forward-compat)** —
  rejected. ADR-0004 explicitly elevates this to the telemetry
  seed. A non-forward-compatible shape would require a migration
  the moment hooks/subagents need traces.

## References

- `commands/workflow/iter_log.go:397-426` — `mergeReviewIterLog` reader
- `commands/workflow/verification.go:100-176` — `verify record --kind review` writer
- `commands/workflow/review_decision_schema.go` — existing schema validation
- `schemas/workflow-iter-log.schema.json` lines 8-17 — required review block fields
- `agent-context-resolution-architecture.md` §1.6 — telemetry pillar
- `agent-context-resolution-architecture.md` §3 — score formula axes that read this envelope
