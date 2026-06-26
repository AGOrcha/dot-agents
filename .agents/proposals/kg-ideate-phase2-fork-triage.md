# Proposal: evolve kg-ideate's idea→spec front into the ideation-cycle loop

**ID:** kg-ideate-phase2-fork-triage
**Scope:** project-local (markdown per `proposal-routing` — targets this repo's
`kg-ideate-skill` spec/plan, not a shared `~/.agents/` resource)
**Status:** **DRAFT — proposed delta against `kg-ideate-skill`.** Owner-ruled on direction;
**not finally ratified** (the empirical backing is power-limited and the v4 experiment is in
flight). Do NOT fold into the `kg-ideate-skill` spec until human ratification + v4.
**Created:** 2026-06-26
**Author:** agent-proposed, pending human review
**Targets:**
- `.agents/workflow/specs/kg-ideate-skill/design.md` (Decisions D1, §3 phase table)
- `.agents/workflow/plans/kg-ideate-skill/` (a new task for the evolved idea→spec segment)
**Depends on / formalized by:**
- `.agents/workflow/specs/ideation-system-composition/design.md` (the composition spec — D1–D8)
- `.agents/workflow/specs/ideation-system-composition/evidence/depth-degradation-dogfood.md`
  (the depth/relay evidence; **v4 in flight** — backs the relay-discipline rule below)
- `internal/scaffold/home/starter/skills/global/ideation-cycle/` (the **compound** this dispatches to)

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
| 2 | `spec-scaffold` | briefing → decisions/open-questions → `design.md` | **evolved:** **autonomously** triage each decision (surfaced rationale, no per-fork ask; an uncitable "easy" verdict defaults HARD) — briefing-decidable → author inline (as today); hard/open fork → run the full `ideation-cycle` loop, which RETURNS the ratified decision + evidence sidecar pointer that **`spec-scaffold` then writes into the spec prose** |
| 3 | `plan-scaffold` | spec → tasks/write-scopes/dep-order | **unchanged** |
| 4 | `staged-execution-handoff` | spec+plan → fanout/ISP | **unchanged** |

### 2. New decision row (proposed D7 in `kg-ideate-skill`)

> **D7 — The idea→spec front is the `ideation-cycle` segment.** Phase 1 (`kg-brief`) +
> Phase 2 (`spec-scaffold`) constitute the idea→spec transition, now traversed by the
> `ideation-cycle` **compound** (the matured, fidelity-gated form of that transition).
> Within the segment, triage is **autonomous** with a **guard** (a "briefing-decidable"
> verdict must cite the decisive fact, else default HARD; the cross-brain pass reviews the
> triage calls too): briefing-decidable decisions resolve inline; hard/open forks run the full
> loop (classify → empirical[fidelity-gate] / cross-brain → ratify). `ideation-cycle`
> **RETURNS** the ratified decision + a **per-fork evidence sidecar**; **`spec-scaffold` writes
> the spec prose** (ideation-cycle does not author the spec file). Control returns to
> `kg-ideate` at Phase 3. Phases 3–4 are unchanged. Every hand-back in the loop is
> structured/pointer-based, never retold prose (relay discipline). Lineage:
> `ideation-cycle derives_from / supersedes` the original `spec-scaffold` idea→spec behavior
> (evolution edge — see composition spec D5/D7).

### 3. Phase-1 → ideation-cycle handoff: briefing digest + dependency manifest, consume-or-rebrief

Phase 1 (`kg-brief`) stamps its briefing with an `inputs_digest` over a concrete input set
(idea text hash + KG snapshot id + named-query result hashes + applicable-lessons set +
cited-artifact content hashes, canonicalized/ordered), using the config-v2 `inputs_digest`
primitive (`ComputeInputsDigest`, `sha256:…`). It also records a **dependency manifest** (the
KG nodes / decisions / lessons it read). When Phase 2 dispatches a hard fork, it passes that
briefing. `ideation-cycle` **consumes it by artifact only if the digest matches AND no
dependency-manifest entry changed** (the latter is how "a prior fork mutated shared brief
state" is detected); otherwise it **re-runs `kg-brief`**. Guarantees a stale brief never
propagates. (This is a data-reuse optimization, not a dispatch-depth trick — see composition
spec D6 for why the chain is bounded by engineering constraints, not a measured cliff.)

### 4. New decision: relay discipline on the dispatch hand-back

The hand-back from `ideation-cycle` to `spec-scaffold` (and any hop within the loop) MUST be
**structured/pointer-based — the artifact path(s) + a constraint/decision checklist — NOT
retold prose.** Evidence: lossy summary relay drops non-reconstructable detail that reaches
the deliverable (composition spec evidence sidecar v3 family-2: 16→13). This binds the wiring
task below.

### 5. New plan task (proposed in `kg-ideate-skill` plan)

A task that wires `spec-scaffold` (t2 in the kg-ideate-skill plan) to the **autonomous,
guarded** segment-triage + `ideation-cycle` invocation (with the briefing-digest +
dependency-manifest handoff above, and the structured relay hand-back), depending on the
`ideation-cycle` compound existing. Verifier: `batch`. Block-scalar notes (YAML colon-space
rule). Cross-plan dep form `ideation-system-composition/<task>` once that spec has a plan.

## What stays the same (do not change)

- `kg-brief` (Phase 1) — it is the shared grounding; `ideation-cycle` RUNS it (reusing its
  output by artifact when fresh), it does not replace it.
- `plan-scaffold` (Phase 3) and `staged-execution-handoff` (Phase 4) — untouched.
- The single-source boundary: `ideation-execution-profile` still owns the ideation
  *profile*; `kg-ideate-skill` still owns the four-phase skill; the composition spec owns
  only the idea→spec evolution seam.

## Owner-ruled direction (DRAFT — inherited from the composition spec §7; not finally ratified)

- **Dispatch-hop bound (OQ1) → engineering bounds, not a measured cliff.** The depth-2–3
  fidelity cliff **did not replicate** (sidecar v1–v3, two harnesses; power-limited null; **v4
  in flight**). Tier-adjacency is dropped regardless (the chain is legal, no hoist), and the
  real bounds are the infra delegation-nesting ceiling (~hop 4) + relay discipline + hygiene.
  Depends on the tiering refinement
  (`.agents/proposals/skill-tiering-molecule-composition.md`).
- **Triage authority (OQ3) → AUTONOMOUS with a guard.** Surfaced rationale; human gate is spec
  ratification. A "briefing-decidable" verdict must cite the decisive fact (else default HARD),
  and the cross-brain pass reviews the triage calls themselves.
- **Evidence location (OQ2) → per-fork SIDECAR** linked from the decision entry (not task
  notes, not inline).
- **Re-tier + ownership + relay (from the codex review):** `ideation-cycle` is a **compound**
  (it orchestrates delegated workers with unbounded judgment); it **RETURNS** decisions +
  evidence and **`spec-scaffold` writes the spec prose**; all hand-backs are structured/pointer
  -based (relay discipline).

## Verification of this delta (once ratified + implemented)

- A `kg-ideate` run whose briefing surfaces a hard fork runs the `ideation-cycle` loop;
  `spec-scaffold` writes the decision into the spec with a link to its per-fork evidence
  sidecar (not a bare assertion); `ideation-cycle` itself does not write the spec file.
- A `kg-ideate` run that dispatches a hard fork with a fresh briefing reuses it by artifact
  (no `kg-brief` re-run); changing a dependency-manifest entry before dispatch forces a re-brief.
- A `kg-ideate` run with only briefing-decidable decisions (each with a cited decisive fact)
  authors them inline and does NOT invoke the full loop (no over-dispatch); an uncitable "easy"
  verdict is re-routed to a hard route.
- The dispatch hand-back is a structured artifact-path + checklist, not retold prose.
- Phases 3–4 outputs are byte-identical to pre-delta behavior for the same inputs (the
  evolution is confined to the idea→spec segment).
