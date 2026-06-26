# Proposal: evolve kg-ideate's idea→spec front into the ideation-cycle loop

**ID:** kg-ideate-phase2-fork-triage
**Scope:** project-local (markdown per `proposal-routing` — targets this repo's
`kg-ideate-skill` spec/plan, not a shared `~/.agents/` resource)
**Status:** proposed (delta against `kg-ideate-skill`; do NOT fold into that spec until ratified)
**Created:** 2026-06-26
**Author:** agent-proposed, pending human review
**Targets:**
- `.agents/workflow/specs/kg-ideate-skill/design.md` (Decisions D1, §3 phase table)
- `.agents/workflow/plans/kg-ideate-skill/` (a new task for the evolved idea→spec segment)
**Depends on / formalized by:**
- `.agents/workflow/specs/ideation-system-composition/design.md` (the composition spec — D1, D3, D5)
- `internal/scaffold/home/starter/skills/global/ideation-cycle/` (the molecule this dispatches to)

---

## Why this is a delta, not a rewrite

`kg-ideate-skill` is a promoted spec with an active plan. Per the `proposal-routing` rule,
a change to its design is **proposed as a delta** and the owner folds it in — it is not
rewritten unilaterally. This proposal captures the delta; ratification + fold-in is the
owner's call.

## The change (framing)

This is **NOT** "add a dispatcher to Phase 2." It is: **`kg-ideate`'s idea→spec front
(Phase 1 `kg-brief` + Phase 2 `spec-scaffold`) evolves INTO invoking `ideation-cycle` for
the idea→spec transition.** Spec authoring itself becomes `ideation-cycle`'s output, folded
back into `kg-ideate`'s pipeline before Phase 3.

Today, `spec-scaffold` (Phase 2) cold-authors every spec decision from the briefing. That
is right for briefing-decidable decisions and wrong for the hard/open forks that the
idea→spec front concentrates — a surfaced contradiction, an unbacked `[PROPOSED]`, an
unadjudicable trade-off. Cold-authoring those produces a guessed decision with no evidence.
The matured idea→spec transition is the fidelity-gated `ideation-cycle` loop (hypotheses →
empirical prototype → negative-control + independent cross-brain audit → ratified decision).

## Proposed delta to `kg-ideate-skill`

### 1. §3 phase table — Phases 1+2 become the `ideation-cycle` segment

| Phase | Molecule | Owns (today) | Owns (proposed) |
|-------|----------|--------------|-----------------|
| 1 | `kg-brief` | KG/research/lessons → briefing | unchanged — `kg-brief` is the shared grounding both skills reuse |
| 2 | `spec-scaffold` | briefing → decisions/open-questions → `design.md` | **evolved:** triage each decision — briefing-decidable → author inline (as today); hard/open fork → run the full `ideation-cycle` loop, fold the ratified decision + evidence into the spec |
| 3 | `plan-scaffold` | spec → tasks/write-scopes/dep-order | **unchanged** |
| 4 | `staged-execution-handoff` | spec+plan → fanout/ISP | **unchanged** |

### 2. New decision row (proposed D7 in `kg-ideate-skill`)

> **D7 — The idea→spec front is the `ideation-cycle` segment.** Phase 1 (`kg-brief`) +
> Phase 2 (`spec-scaffold`) constitute the idea→spec transition, which is now traversed by
> the `ideation-cycle` molecule (the matured, fidelity-gated form of that transition).
> Within the segment, briefing-decidable decisions resolve inline; hard/open forks run the
> full loop (classify → empirical[fidelity-gate] / cross-brain → ratify). The segment
> output is a ratified spec; control returns to `kg-ideate` at Phase 3. Phases 3–4 are
> unchanged. `kg-ideate` owns the whole pipeline; `ideation-cycle` owns this segment.
> Lineage: `ideation-cycle derives_from / supersedes` the original `spec-scaffold`
> idea→spec behavior (evolution edge — see composition spec D5).

### 3. New plan task (proposed in `kg-ideate-skill` plan)

A task that wires `spec-scaffold` (t2 in the kg-ideate-skill plan) to the segment-triage +
`ideation-cycle` invocation, depending on the `ideation-cycle` molecule existing. Verifier:
`batch`. Block-scalar notes (YAML colon-space rule). Cross-plan dep form
`ideation-system-composition/<task>` once that spec has a plan.

## What stays the same (do not change)

- `kg-brief` (Phase 1) — it is the shared grounding; `ideation-cycle` reuses it, does not
  replace it.
- `plan-scaffold` (Phase 3) and `staged-execution-handoff` (Phase 4) — untouched.
- The single-source boundary: `ideation-execution-profile` still owns the ideation
  *profile*; `kg-ideate-skill` still owns the four-phase skill; the composition spec owns
  only the idea→spec evolution seam.

## Open questions (inherit from the composition spec — owner ruling needed)

- **Dispatch-hop bound (composition spec OQ1):** `kg-ideate → spec-scaffold →
  ideation-cycle → kg-brief` can hit 3 hops, past the reliable 1–2-hop bound. Recommend
  hoisting the `ideation-cycle` invocation to the `kg-ideate` compound (so `spec-scaffold`
  flags a hard fork and the compound runs `ideation-cycle` as a sibling phase), and/or
  reusing `kg-brief` by artifact rather than re-dispatch. **This decides how the wiring task
  above is implemented — settle it before that task starts.**
- **Triage authority (composition spec OQ3):** does `spec-scaffold` triage autonomously
  (recommended: yes, with a surfaced one-line rationale, human veto) or always ask first?
- **Evidence location (composition spec OQ2):** where the hard-fork evidence (prototype +
  audits) attaches when produced inside a `kg-ideate` run.

## Verification of this delta (once ratified + implemented)

- A `kg-ideate` run whose briefing surfaces a hard fork runs the `ideation-cycle` loop and
  the resulting spec decision carries an evidence pointer (not a bare assertion).
- A `kg-ideate` run with only briefing-decidable decisions authors them inline and does NOT
  invoke the full loop (no over-dispatch).
- Phases 3–4 outputs are byte-identical to pre-delta behavior for the same inputs (the
  evolution is confined to the idea→spec segment).
