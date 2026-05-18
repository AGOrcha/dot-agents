# ADR-0004: Execution-telemetry schema seeded by `review-decision.yaml`

**Status:** accepted
**Date:** 2026-05-04
**Owners:** dot-agents
**Related:** [ADR-0001](0001-adopt-architecture-decision-records.md) (ADR conventions); [ADR-0002](0002-self-review-output-schema.md) (the schema being seeded); [ADR-0003](0003-self-review-fire-ordering.md) (when review-decision.yaml gets written); [ADR-0005](0005-restore-kg-context-in-self-review.md) (Step 0 that produces the kg-context fields); [`agent-context-resolution-architecture.md` §1.6](../../.agents/proposals/agent-context-resolution-architecture.md) (the execution-telemetry pillar this ADR seeds); [`self-review-iteration-close-wiring` plan t4](../../.agents/workflow/plans/self-review-iteration-close-wiring/) (this ADR's source)

## Context

The agent-context-resolution architecture note §1.6 names execution
telemetry as the fourth pillar of the dispatch contract. The pillar
specifies a per-resource trace envelope (`resource_type` /
`resource_id` / `invoked_at` / `outcome` / `post_invocation` /
`improvement_signals`) that hooks, subagents, rules, and skills will
emit when invoked. Today, **no such trace exists on disk** — §1.6 is
aspirational.

ADR-0002 chose `.agents/active/verification/<task_id>/review-decision.yaml`
as the path self-review writes its output to, with a forward-compatible
envelope that includes the §1.6 telemetry shape. The shape was chosen
explicitly so the same artifact can serve two purposes: (a) the input
`mergeReviewIterLog` reads to populate the iter-log review block,
and (b) the first concrete trace of the §1.6 execution-telemetry
pillar.

Without this ADR, the relationship is implicit. A future reader
seeing the §1.6 pillar in the architecture note would not know that
review-decision.yaml is its concrete instantiation, and a future
schema migration to ADR-0002's envelope might unintentionally diverge
from the §1.6 pillar's expectations.

## Decision

**Designate the `review-decision.yaml` envelope from ADR-0002 as
the first concrete instance of the §1.6 execution-telemetry trace
schema.**

This means:

1. The §1.6 telemetry envelope and the ADR-0002 review-decision schema
   evolve together. A change to one without the corresponding
   evolution of the other is a contract break.

2. Future hook traces, subagent traces, rule-read traces, and other
   resource invocations that need telemetry follow the same envelope
   shape (top half) — `resource_type` / `resource_id` / `invoked_at` /
   `invoked_by` / `plan_id` / `task_id` / `outcome` / `post_invocation` /
   `improvement_signals`. The middle slice (`overall_decision`,
   `phase_*_decision`, `reviewer_notes`, `failed_gates`,
   `escalation_reason`) is review-kind-specific; other resource kinds
   may add their own kind-specific fields without breaking the shared
   envelope.

3. The score formula in `agent-context-resolution-architecture.md` §3
   reads from this envelope. Specifically:
   - `consumers` axis ← `invoked_by` distinct count over a window
   - `recency-weighted access` ← rate of `invoked_at` over a window
   - `contradiction history` ← `post_invocation.retries_in_loop` and
     `improvement_signals.tooling_gap`/`script_gap`/`instruction_gap`
   - `improvement signals` ← the named `improvement_signals` block
   verbatim

4. `~/.agents/skills/dot-agents/self-review/instructions/output-format.md`
   (produced by t2) is the canonical authoring guide for the envelope.
   New traces written by other resource types reuse the envelope shape
   from that file as the template.

5. The architecture note §1.6 gains a forward reference to this ADR:
   "seeded by review-decision.yaml; see ADR-0004."

**Schema version note** (per t2 worker observation): the on-disk
`schemas/verification-decision.schema.json` uses snake-with-digits
names (`phase_1_decision`, `phase_2_decision`) while ADR-0002's prose
example uses `phase1_decision`/`phase2_decision`. The on-disk schema
is authoritative; ADR-0002 should be amended in a small follow-up
edit (or this ADR's §References can carry the correction). Any future
migration to land §1.6 envelope fields at the *top level* of the
review-decision.yaml (rather than packed inside `reviewer_notes` as
t2's output-format.md currently does) is a real schema migration,
not a docs-only change — it must update
`commands/workflow/review_decision_schema.go`,
`schemas/verification-decision.schema.json`, and the writer in
`commands/workflow/verification.go` together.

## Consequences

**Easier:**

- §1.6 is no longer aspirational. There's a concrete artifact on
  disk that future readers can inspect to understand the trace shape.
- New resource-type traces (hook fire, subagent spawn, rule read)
  copy the envelope and only add their kind-specific fields. No
  re-design required.
- The architecture note's score-formula axes (§3) become computable
  over real data once review-decision.yaml files start accumulating in
  `.agents/active/verification/`.
- Cross-references between the four anchor specs and the architecture
  note collapse to a single source of truth for the trace shape.

**Harder:**

- The schema is now load-bearing for two systems (review-record
  pipeline + telemetry pillar). Schema changes require coordination
  across both consumers.
- The on-disk schema vs ADR-0002 prose mismatch (snake-with-digits
  vs no-underscore) is now visible. ADR-0002 needs a small
  amendment-style edit to align prose with on-disk reality, or this
  ADR's §References note serves as the canonical clarification.

**New risks:**

- Future hooks/subagents emitting traces could drift from the
  envelope if there's no enforcement. Mitigation: when the first
  hook telemetry lands, that plan should reuse `output-format.md`
  verbatim (not re-author the envelope) and a schema test should
  validate the shape against `schemas/verification-decision.schema.json`
  with a generalization to per-resource-type variants.
- The `reviewer_notes`-as-envelope-packing approach (t2's
  output-format.md current implementation) means the §1.6 telemetry
  fields live as packed YAML inside a free-text field rather than at
  top level. A future migration may want to elevate them. This
  migration is named here so it isn't a surprise.

**Locked-in:**

- The envelope shape (resource_type / resource_id / invoked_at /
  invoked_by / plan_id / task_id / outcome / post_invocation /
  improvement_signals). A different envelope for hooks vs subagents
  would defeat the seeding purpose; this ADR forbids that.
- The score-formula axes' read paths into the envelope. Changing
  e.g. how `consumers` is derived requires this ADR be superseded.

## Alternatives considered

- **Don't ADR this — leave the relationship implicit in the
  architecture note** — rejected. ADR-0001's "lessons that mature
  into design choices graduate to ADRs" applies: §1.6's
  pillar-meets-implementation is exactly that kind of choice.
  Without an ADR, future readers can't grep `docs/adr/` to find the
  envelope contract; they'd have to read both the architecture note
  AND ADR-0002 to reconstruct the relationship.

- **Define a separate top-level telemetry schema** — rejected.
  Would create two schemas that drift; defeats the shared-envelope
  benefit. The cost (one ADR-tracked relationship) is worth the
  durable single-shape contract.

- **Wait until the second resource-type trace lands** — rejected.
  Documenting the shape *as* it's seeded is when the contract is
  cheapest to record. Waiting means the next plan author starts from
  blank rather than from a canonical shape.

## References

- `~/.agents/skills/dot-agents/self-review/instructions/output-format.md`
  — canonical authoring guide for the envelope (produced by
  self-review-iteration-close-wiring/t2).
- `schemas/verification-decision.schema.json` — on-disk schema; uses
  snake-with-digits names; authoritative over ADR-0002's prose example.
- `commands/workflow/review_decision_schema.go` — Go-side validator.
- `commands/workflow/verification.go` — writer for `verify record
  --kind review`.
- `commands/workflow/iter_log.go:397-426` — `mergeReviewIterLog`
  reader.
- `agent-context-resolution-architecture.md` §1.6 — execution
  telemetry pillar.
- `agent-context-resolution-architecture.md` §3 — score formula axes
  that read this envelope.
