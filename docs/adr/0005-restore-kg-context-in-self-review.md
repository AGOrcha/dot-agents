# ADR-0005: Restore `da kg changes --brief` + `da kg impact` as Step 0 of self-review

**Status:** accepted
**Date:** 2026-05-03
**Owners:** dot-agents
**Related:** [ADR-0002](0002-self-review-output-schema.md), [ADR-0003](0003-self-review-fire-ordering.md); [`self-review-iteration-close-wiring` plan t1/t2](../../.agents/workflow/plans/self-review-iteration-close-wiring/); [`agent-context-resolution-architecture.md` §6.5](../../.agents/proposals/agent-context-resolution-architecture.md) (audit pattern)

## Context

The audit at `agent-context-resolution-architecture.md` §6.5
identified a regression: the `crg-kg-integration/phase-g-skill-integration`
task on 2026-04-12 added a `da kg changes --brief` invocation as Step 0
of self-review (in
`src/share/templates/standard/skills/global/self-review/SKILL.md` —
the older inline-step format). The skill-architect rework that
produced today's modular orchestrator (in
`~/.agents/skills/dot-agents/self-review/`) did not preserve that
call. The merge-back archive even flagged the missing follow-up:
*"Promote/sync `~/.agents/skills/global` from embedded templates"* —
that step never happened.

This is the exact regression pattern that managed-compounding is
designed to prevent. A graduated capability (KG-context awareness
in review) was lost because no system tracked improvements across
reworks.

The plan (`self-review-iteration-close-wiring`) is fixing this
gap. The decision: restore the lost behavior AND record *why*
explicitly so future skill-architect reworks don't drop it again.

## Decision

**Restore KG-context awareness as Step 0 of self-review's workflow,
documented in a new `instructions/kg-context.md` instruction module
under `~/.agents/skills/dot-agents/self-review/instructions/`.**

Step 0 runs:

1. `da kg changes --brief` — surfaces the structural change set
   (which files / nodes / edges moved) before per-file review.
2. `da kg impact <changed_files>` — surfaces blast-radius (what
   downstream nodes are affected) so review can flag broken
   assumptions in dependent code.

The output of these two CLI calls is captured in the review
narrative (`reviewer_notes` field of `review-decision.yaml`, per
ADR-0002) so the audit trail records what the reviewer saw.

This restores the original phase-G integration's intent and makes
the regression visible in source: any future skill-architect rework
that omits `instructions/kg-context.md` is a deletion, not a silent
miss. The rework would have to actively choose to drop the file,
which surfaces in code review.

## Consequences

**Easier:**

- Per-file review starts with global context (blast-radius +
  change-set) rather than reviewing in the blind.
- Regression discoverability: future reworks that drop kg-context
  must actively delete `instructions/kg-context.md`; deletion is a
  visible diff, not an oversight.
- The lesson is captured in this ADR. A future engineer asking
  "why does self-review have a kg-context step?" can answer from
  this ADR rather than archaeology.

**Harder:**

- Self-review now requires the KG/CRG bridge to be available. When
  it isn't (clean checkout, missing venv), Step 0 must degrade
  gracefully — emit a warning, skip blast-radius, continue with
  per-file review. The instruction module documents this fallback.
- Slightly more wall-clock per review (two extra CLI calls, both
  bounded). Acceptable: blast-radius awareness is what makes review
  meaningfully decisive.

**New risks:**

- KG/CRG output format drift could break the instruction's parsing
  expectations. Mitigation: `instructions/kg-context.md` reads
  `--brief` outputs as opaque text and surfaces them verbatim in
  reviewer_notes; no structured parse required.
- Step 0 fails on a fresh repo without a built KG. Mitigation:
  graceful degradation (skip Step 0, log "KG not available", proceed
  with per-file review). The skill remains useful even without KG.

**Locked-in:**

- Step 0 owns the kg-context responsibility. If the responsibility
  moves later (e.g., to iteration-close itself, or to a separate
  pre-review hook), this ADR must be superseded explicitly.

## Alternatives considered

- **Inline the kg-context calls directly in SKILL.md** — rejected.
  Putting the calls inside the orchestrator file rather than a
  named instruction module makes them harder to spot when reviewing
  the skill's structure; inline calls in SKILL.md are a code-smell
  the skill-architect rework was specifically cleaning up.
- **Move kg-context to iteration-close instead** — rejected.
  Self-review's *job* is informed review; doing the kg-context call
  outside the skill loses the link between the call and the review
  reasoning. Also: standalone self-review (manual invocation
  outside iteration-close) still benefits from kg-context.
- **Don't restore — leave self-review without kg-context** —
  rejected. The original phase-G integration was a deliberate
  graduation; reverting it silently was the regression. This ADR
  records the restoration as load-bearing.

## References

- `.agents/history/crg-kg-integration/` — original phase-G integration history.
- `.agents/history/crg-kg-integration/delegate-merge-back-archive/2026-04-12/phase-g-skill-integration/merge-back.md` — flagged the missing sync follow-up.
- `src/share/templates/standard/skills/global/self-review/SKILL.md` — older inline-step template that had the kg-changes call.
- `~/.agents/skills/dot-agents/self-review/SKILL.md` — current orchestrator that t2 extends with `instructions/kg-context.md`.
- ADR-0002 — output schema where Step 0 results land.
- ADR-0003 — fire ordering that places review after verify-record-test, so the kg-context step runs at the start of self-review's chain.
