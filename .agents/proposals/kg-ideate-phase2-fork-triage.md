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
| 1 | `kg-brief` | KG/research/lessons → briefing | unchanged as a molecule, BUT its output now carries an `inputs_digest` so `ideation-cycle` can reuse it by artifact when fresh (re-runs `kg-brief` on staleness) |
| 2 | `spec-scaffold` | briefing → decisions/open-questions → `design.md` | **evolved:** **autonomously** triage each decision (surfaced rationale, no per-fork ask) — briefing-decidable → author inline (as today); hard/open fork → run the full `ideation-cycle` loop, fold the ratified decision + its evidence **sidecar pointer** into the spec |
| 3 | `plan-scaffold` | spec → tasks/write-scopes/dep-order | **unchanged** |
| 4 | `staged-execution-handoff` | spec+plan → fanout/ISP | **unchanged** |

### 2. New decision row (proposed D7 in `kg-ideate-skill`)

> **D7 — The idea→spec front is the `ideation-cycle` segment.** Phase 1 (`kg-brief`) +
> Phase 2 (`spec-scaffold`) constitute the idea→spec transition, which is now traversed by
> the `ideation-cycle` molecule (the matured, fidelity-gated form of that transition).
> Within the segment, triage is **autonomous** (surfaced rationale; the human gate is spec
> ratification, not per-fork triage): briefing-decidable decisions resolve inline; hard/open
> forks run the full loop (classify → empirical[fidelity-gate] / cross-brain → ratify). Each
> hard fork's evidence is a **per-fork sidecar linked from the decision**. The segment output
> is a ratified spec; control returns to `kg-ideate` at Phase 3. Phases 3–4 are unchanged.
> `kg-ideate` owns the whole pipeline; `ideation-cycle` owns this segment. Lineage:
> `ideation-cycle derives_from / supersedes` the original `spec-scaffold` idea→spec behavior
> (evolution edge — see composition spec D5/D7).

### 3. Phase-1 → ideation-cycle handoff: briefing digest, consume-or-rebrief

Phase 1 (`kg-brief`) stamps its briefing output with an `inputs_digest` over its inputs (KG
snapshot / query results, research set, applicable lessons, idea/proposal text), using the
config-v2 `inputs_digest` primitive (`ComputeInputsDigest`, `sha256:…`) for coherence. When
Phase 2 dispatches a hard fork to `ideation-cycle`, it passes that briefing + its digest.
`ideation-cycle` **consumes the briefing by artifact only if the digest still matches AND no
prior fork's resolution mutated a brief input**; otherwise it **re-runs `kg-brief`**. This is
the reuse-by-artifact optimization that keeps the dispatch path at 2 hops (`kg-brief` is a
terminal leaf — see composition spec D6) while guaranteeing a stale brief never propagates.

### 4. New plan task (proposed in `kg-ideate-skill` plan)

A task that wires `spec-scaffold` (t2 in the kg-ideate-skill plan) to the **autonomous**
segment-triage + `ideation-cycle` invocation (with the briefing-digest handoff above),
depending on the `ideation-cycle` molecule existing. Verifier: `batch`. Block-scalar notes
(YAML colon-space rule). Cross-plan dep form `ideation-system-composition/<task>` once that
spec has a plan.

## What stays the same (do not change)

- `kg-brief` (Phase 1) — it is the shared grounding; `ideation-cycle` RUNS it (reusing its
  output by artifact when fresh), it does not replace it.
- `plan-scaffold` (Phase 3) and `staged-execution-handoff` (Phase 4) — untouched.
- The single-source boundary: `ideation-execution-profile` still owns the ideation
  *profile*; `kg-ideate-skill` still owns the four-phase skill; the composition spec owns
  only the idea→spec evolution seam.

## Resolved (owner-ruled — inherited from the composition spec §7)

- **Dispatch-hop bound (OQ1) → DEPTH governs, no hoist.** `kg-ideate → spec-scaffold →
  ideation-cycle` is a 2-hop, in-bound path; `kg-brief` is a terminal leaf
  (reuse-by-artifact-when-fresh or leaf re-run). The wiring task above does NOT hoist to the
  compound. Depends on the tiering refinement
  (`.agents/proposals/skill-tiering-molecule-composition.md`).
- **Triage authority (OQ3) → AUTONOMOUS** with surfaced rationale; the human gate is spec
  ratification, not per-fork triage.
- **Evidence location (OQ2) → per-fork SIDECAR** linked from the spec decision entry (not
  task notes, not inline).

## Verification of this delta (once ratified + implemented)

- A `kg-ideate` run whose briefing surfaces a hard fork runs the `ideation-cycle` loop and
  the resulting spec decision links a per-fork evidence sidecar (not a bare assertion).
- A `kg-ideate` run that dispatches a hard fork with a fresh briefing reuses it by artifact
  (no `kg-brief` re-run); mutating a brief input before dispatch forces a re-brief.
- A `kg-ideate` run with only briefing-decidable decisions authors them inline and does NOT
  invoke the full loop (no over-dispatch).
- Phases 3–4 outputs are byte-identical to pre-delta behavior for the same inputs (the
  evolution is confined to the idea→spec segment).
