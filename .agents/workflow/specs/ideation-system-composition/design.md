# Spec: ideation-system composition — kg-ideate (authoring) ⟷ ideation-cycle (fork-resolution)

**Spec ID:** ideation-system-composition
**Status:** draft (for final review) — D1–D7 decided; the four prior OQs are owner-ruled
(§7) and folded into the decisions; no unresolved open questions remain
**Created:** 2026-06-26
**Author:** agent-proposed (drafted with agent assist), pending human review
**Related:**
- `.agents/workflow/specs/kg-ideate-skill/design.md` — the authoring front-end (this spec
  proposes a Phase 2 delta to it; the delta itself is routed as a project-local proposal,
  `.agents/proposals/kg-ideate-phase2-fork-triage.md`, not a unilateral spec rewrite).
- `internal/scaffold/home/starter/skills/global/ideation-cycle/SKILL.md` — the
  fork-resolution molecule this spec formalizes.
- `.agents/rules/dot-agents/workflow-artifact-model.md` — the spec→plan→tasks→history tiers.
- `.agents/workflow/specs/skill-tiering-contract/design.md` — tier vocabulary
  (atom/molecule/compound/cell) and the 1–2-hop dispatch bound.
- `.agents/lessons/prototype-experiment-fidelity-gate/LESSON.md`,
  `.agents/lessons/tests-must-drive-the-production-path/LESSON.md` — the evidence gates
  `ideation-cycle` enforces.
- `[[isp]]`, `[[orchestrator-session-start]]` — the execution counterpart.

---

## 1. Problem statement

We have run a fidelity-gated design-refinement methodology three times (config-profiles,
KG lease/conflict, KG projection) and kept re-explaining it each time. Independently,
`kg-ideate` exists as the KG-grounded authoring front-end (idea → brief → spec → plan →
staged-execution handoff). The two are easy to conflate, and that conflation produces two
failure modes:

1. **Duplication.** A standalone "design-refinement" skill that re-does the
   KG/research/lessons grounding `kg-ideate`'s Phase 1 (`kg-brief`) already owns — two
   divergent baseline scans, two briefing shapes.
2. **A missing seam in `kg-ideate`.** Phase 2 (`spec-scaffold`) today *cold-authors every
   spec decision from the briefing*. But some decisions are HARD/OPEN forks the briefing
   cannot adjudicate — a surfaced contradiction, a `[PROPOSED]` with no backing, a
   trade-off across competing values. Cold-authoring those produces a guessed decision
   with no evidence behind it.

This spec formalizes the relationship as an **evolution**, not a sibling-split:
`ideation-cycle` is the **matured form of `kg-ideate`'s idea/proposal → spec segment**.
`kg-ideate`'s lifecycle is idea/proposal → spec → plan → handoff; the FRONT of that
lifecycle (`kg-brief` grounding + `spec-scaffold`) is exactly where hard forks live, and
the fidelity-gated loop (hypotheses → empirical prototype → negative-control + independent
cross-brain audit → ratified decision) IS how you now go from a grounded idea/proposal to a
ratified spec. So this spec formalizes (a) the **segment ownership** (`ideation-cycle` owns
idea→spec; `kg-ideate` still owns the whole pipeline), (b) the **shared grounding**
(`kg-brief`), and (c) the **handoff back** ("ratified spec in hand → continue to
`plan-scaffold`"), so both get built coherently rather than re-derived.

## 2. Goals

- One **evolution framing** on paper: `ideation-cycle` is the matured form of `kg-ideate`'s
  idea→spec segment; `kg-ideate` still owns the whole pipeline (idea→spec→plan→handoff).
- One **shared grounding stage** (`kg-brief`) consumed by both — no reinvented baseline scan.
- The idea→spec **segment evolves**: briefing-decidable forks resolve inline; hard/open
  forks run the full `ideation-cycle` loop; the ratified spec folds back into `kg-ideate`
  before Phase 3 (`plan-scaffold`). Phases 3–4 are unchanged.
- `ideation-cycle` is **independently invocable** for a one-off design question (a fork that
  surfaces outside an active `kg-ideate` run, e.g. from execution).
- The **fidelity gate** (`prototype-experiment-fidelity-gate`) is the mandatory evidence
  bar for any prototype-based fork resolution.
- **Lineage is an evolution edge** (`derives_from`/`supersedes`), not a sibling edge — noted
  for the later lineage-schema work to inherit.

Non-goals: re-authoring the ideation *profile* (verifiers/reviewers/relevance — owned by
`ideation-execution-profile`); rebuilding `kg-ideate`'s phases (owned by `kg-ideate-skill`);
authoring the depth-bound lint (owned by `skill-tiering-contract` + its refinement delta,
plan-tier work).

## 3. Decisions

### D1 — `ideation-cycle` is the evolution of `kg-ideate`'s idea→spec segment (not a sibling)

`ideation-cycle` is NOT a parallel engine `kg-ideate` happens to dispatch. It is the
**matured form of the idea/proposal → spec transition** that lives at the front of
`kg-ideate`'s lifecycle. `kg-ideate` still owns the WHOLE pipeline; `ideation-cycle` owns
the idea→spec SEGMENT and the fork-resolution within it.

```
kg-ideate lifecycle:
  idea/proposal ──▶ spec ──▶ plan ──▶ staged-execution handoff
  └──────────────────┘        Phase 3      Phase 4
   idea→spec SEGMENT          (plan-scaffold)  (handoff)
   = Phase 1 (kg-brief) + Phase 2 (spec-scaffold)
   ▼ EVOLVED INTO ▼
  ideation-cycle: ground(kg-brief) → enumerate → classify →
                  empirical[fidelity-gate] → cross-brain → ratify
   handoff back: "ratified spec in hand → continue to plan-scaffold"
```

| | `kg-ideate` | `ideation-cycle` |
|---|---|---|
| Tier | T2 compound | T1 molecule |
| Owns | the WHOLE pipeline: idea → spec → plan → handoff | the idea→spec SEGMENT + fork-resolution within it |
| Verb | AUTHORS the pipeline grounded in the KG | how idea→spec is now DONE: grounded idea → ratified spec |
| Output | spec + plan + handoff | a ratified spec decision + evidence (prototype, audit, cross-brain) |

The idea→spec front is exactly where hard/open forks concentrate (a grounded briefing
surfaces contradictions, unbacked `[PROPOSED]`s, unadjudicable trade-offs). The
fidelity-gated loop is the rigorous way to traverse that front. `kg-ideate` Phases 3–4
(`plan-scaffold`, `staged-execution-handoff`) are unchanged — the evolution is confined to
the idea→spec segment, and control returns to `kg-ideate` with a ratified spec in hand.

### D2 — `ideation-cycle` RUNS `kg-brief`; reuse-by-artifact only if fresh (`inputs_digest`)

`kg-brief` is `ideation-cycle`'s grounding step 1 — it is **run by** `ideation-cycle`, not
handed down pre-baked unconditionally. Both skills ground on the same primitive (the
`kg-brief` molecule: KG / research / lessons → the briefing block); `ideation-cycle` does NOT
carry its own baseline scan (anti-duplication invariant).

**Optimization + invalidation:** when dispatched from `kg-ideate` (which already produced a
Phase 1 briefing), `ideation-cycle` MAY consume that briefing **by artifact** (0 dispatch
hops) — **but only if it is FRESH.** Freshness is an `inputs_digest` over the briefing's
inputs (KG snapshot / query results, research set, applicable lessons, the idea/proposal
text). This **reuses the config-v2 `inputs_digest` primitive** (`ComputeInputsDigest`,
`sha256:…`; staleness = digest mismatch — `internal/config/staleness.go`, `resolver.go`) for
coherence — not a parallel scheme. **Invalidation:** on any digest mismatch, OR if a prior
fork's resolution mutated shared state the brief depended on, `ideation-cycle` **re-runs
`kg-brief`**. A stale brief must never silently propagate. Standalone runs (no upstream
artifact) always run `kg-brief` fresh. `ideation-cycle`'s step 1 (`ground-via-kg-brief.md`)
enforces this gate.

### D3 — `kg-ideate`'s idea→spec front (Phase 1 + Phase 2) evolves INTO invoking `ideation-cycle`

This is NOT "bolt a dispatcher onto Phase 2." The idea→spec transition — `kg-brief`
grounding (Phase 1) followed by `spec-scaffold` (Phase 2) — *becomes* the `ideation-cycle`
loop. Spec authoring itself is now `ideation-cycle`'s output, folded back into `kg-ideate`'s
pipeline before Phase 3.

Within that evolved segment, triage still applies (not every decision needs the full loop).
**Triage is AUTONOMOUS** (OQ3 ruling): `ideation-cycle` classifies each fork itself and
**surfaces its rationale** — it does NOT ask for per-fork confirmation. The human gate is
**spec ratification** at converge, not per-fork triage; always-asking would defeat the
stop-re-explaining-itself purpose.

1. **Briefing-decidable** → resolved inline, exactly as `spec-scaffold` does today. The
   briefing settles it; no prototype, no cross-brain.
2. **Hard / open fork** → runs the full `ideation-cycle` loop (classify →
   empirical[fidelity-gate] / cross-brain → ratify). The ratified decision + evidence is
   folded into the spec being authored.

Boundary test: the briefing answers it (prior ratified decision, clear single-resolution
gap) → briefing-decidable, inline. The briefing surfaces a contradiction / unbacked
`[PROPOSED]` / unadjudicable trade-off AND getting it wrong is costly → hard fork → full loop.

The output of the evolved segment is a **ratified spec in hand**; control returns to
`kg-ideate` to continue at Phase 3 (`plan-scaffold`). `ideation-cycle` owns the idea→spec
method; `kg-ideate` owns the surrounding pipeline. This is the delta routed to
`kg-ideate-skill` (see §6).

### D5 — Lineage is an evolution edge (`derives_from` / `supersedes`), not a sibling edge

For the KG / lineage schema: `ideation-cycle` **`derives_from`** (and partially
**`supersedes`**) `kg-ideate`'s original idea→spec stage — it is the matured form of that
transition, not a `related_to` sibling. The later lineage-schema fork should inherit this:
the edge from `ideation-cycle` to `kg-ideate`'s original `spec-scaffold` behavior is an
*evolution* edge. `related_to` would wrongly model them as independent peers and lose the
"this replaced how we used to do idea→spec" history. Recorded here so the lineage-schema
work picks it up rather than re-deriving it.

### D4 — `ideation-cycle` is registered BOTH ways (top-level invocable AND dispatchable)

`ideation-cycle` is registered as a **top-level invocable skill AND** dispatchable from
`kg-ideate` (OQ4 ruling). It is independently useful — the fork-resolution loop ran
standalone for every config-profiles decision this session — not only a `kg-ideate`
sub-step. The two modes:

- **Dispatched** (from `kg-ideate` Phase 2): resolve one named fork; return decision +
  evidence sidecar pointer; do not author the surrounding spec.
- **Standalone** (one-off design question, or a fork surfaced from execution): run the full
  cycle, including seeding/refining the spec at converge.

Both modes ground via `kg-brief` (D2) and gate prototypes via the fidelity gate.

### D6 — Dispatch DEPTH governs composition; no hoist needed (OQ1 ruling)

The `kg-ideate → spec-scaffold → ideation-cycle → kg-brief` "3-hop" concern is dissolved by
refining the tiering contract so composition is governed by dispatch **DEPTH (≤2 reliable
hops on any runtime path), not tier-adjacency** — a molecule MAY call a molecule (delta:
`.agents/proposals/skill-tiering-molecule-composition.md`). `kg-brief` is a **terminal
leaf** (calls nothing downstream; satisfied by reuse-by-artifact-when-fresh or a leaf
re-run), so it adds no depth. The chain `kg-ideate → spec-scaffold → ideation-cycle` is a
**2-hop, in-bound** path. **No hoist of the dispatch up to the compound is required** —
forcing one purely to satisfy adjacency was redundant indirection. `ideation-cycle`'s
frontmatter carries `kg-brief` in `calls:` (legal under the refined contract), not a special
`reuses:` field.

### D7 — A hard fork's evidence is a per-fork SIDECAR, linked from the decision (OQ2 ruling)

A hard fork's evidence — the prototype dir, the negative-control result, the cross-brain
audit verdicts — is its **own sidecar artifact**, LINKED from the spec's decision entry. Not
inlined into the spec, not in transient task notes. This anticipates lineage schema-v4 (the
decision `derives_from` its evidence sidecar — consistent with D5's evolution-edge lineage).
Standalone mode links the sidecar from the spec it seeds; dispatched mode returns the sidecar
pointer to `spec-scaffold` to link from the decision it folds in.

## 4. Requirements (behavioral)

1. A standalone `ideation-cycle` run with no `kg-ideate` in flight runs `kg-brief` fresh to
   ground, then resolves the fork end-to-end and seeds/refines the spec.
2. A `kg-ideate` Phase 2 run that hits a briefing-decidable decision authors it directly —
   it does NOT dispatch to `ideation-cycle` (no over-dispatch). Triage is autonomous with a
   surfaced rationale.
3. A `kg-ideate` Phase 2 run that hits a hard fork dispatches to `ideation-cycle`, receives
   a ratified decision + evidence sidecar pointer, and folds it into the spec —
   `ideation-cycle` does not write that spec.
4. No prototype result informs any decision until it has cleared the fidelity gate
   (faithful inputs + negative control + real execution + independent cross-harness audit).
5. Neither skill reinvents grounding: there is exactly one briefing shape (`kg-brief`'s),
   consumed by both.
6. When dispatched, `ideation-cycle` reuses the upstream briefing by artifact ONLY when its
   `inputs_digest` matches and no prior fork mutated a brief input; otherwise it re-runs
   `kg-brief`. A stale briefing is never silently reused.
7. A hard fork's evidence is a per-fork sidecar linked from the decision entry (not inlined,
   not in transient task notes).

## 5. Done criteria

1. `ideation-cycle/SKILL.md` declares `tier: molecule`, `calls:` including `kg-brief` (legal
   under the refined depth-governed contract), and `dispatchable_from: [kg-ideate]`.
2. `ideation-cycle` step 1 runs-or-reuses-if-fresh `kg-brief` and never hand-scans the
   baseline (Requirement 5), with the `inputs_digest` freshness gate (Requirement 6).
3. This composition spec exists and is cross-referenced from both `ideation-cycle/SKILL.md`
   and the `kg-ideate` Phase 2 delta proposal.
4. The `kg-ideate` Phase 2 fork-triage + dispatch delta is captured as a project-local
   proposal (NOT a unilateral edit to `kg-ideate-skill/design.md`) and references this spec.
5. The tiering-contract depth-refinement is captured as a project-local proposal
   (`.agents/proposals/skill-tiering-molecule-composition.md`) and resolves OQ1.
6. The four OQs (§7) are owner-ruled and folded into D2–D7; the spec stays `draft` pending
   final review but carries no unresolved open questions.

## 6. Relationship to `kg-ideate-skill`

This spec does **not** rewrite `kg-ideate-skill/design.md`. The Phase 2 change is a
*delta* proposed against that spec, routed as a project-local proposal
(`.agents/proposals/kg-ideate-phase2-fork-triage.md`) per the proposal-routing rule
("propose the delta; don't rewrite the spec unilaterally"). If ratified, the owner folds
it into the `kg-ideate-skill` spec's Phase 2 decision (D-row) and the `kg-ideate-skill`
plan grows a task for the dispatch seam.

The single-source boundary holds: `kg-ideate-skill` owns the four-phase skill;
`ideation-execution-profile` owns the profile; this spec owns only the **composition seam**
between `kg-ideate` and `ideation-cycle`.

## 7. Resolved questions (owner-ruled — RATIFIED, folded into §3)

All four prior open questions are closed by owner ruling. The decisions are **RATIFIED** and
encoded in §3 (the spec stays `draft` pending final review, but these are settled, not
framing-to-apply). Summary of the rulings and where each lives:

**OQ1 — Dispatch-hop bound → RESOLVED (D6): DEPTH governs, no hoist.** The "3-hop" problem
was an artifact of an over-strict tier-adjacency rule. Composition is now governed by
dispatch **DEPTH (≤2 hops on any runtime path)**, not tier-adjacency — a molecule MAY call a
molecule (delta: `.agents/proposals/skill-tiering-molecule-composition.md`). `kg-brief` is a
terminal leaf (reuse-by-artifact = 0 hops, or leaf re-run), so `kg-ideate → spec-scaffold →
ideation-cycle` is 2-hop and in-bound. No hoist required.

**OQ2 — Fork evidence → RESOLVED (D7): per-fork SIDECAR, linked from the decision.** Not
inlined, not in transient task notes. Anticipates lineage schema-v4 (`derives_from` edge).

**OQ3 — Triage authority → RESOLVED (D3): AUTONOMOUS + surfaced rationale.** The human gate
is spec ratification, not per-fork triage.

**OQ4 — Registration → RESOLVED (D4): BOTH.** Top-level invocable AND dispatchable from
`kg-ideate`.

## 8. Deferred

- The depth-bound lint itself (plan-tier; owned by `skill-tiering-contract` + its refinement
  delta `.agents/proposals/skill-tiering-molecule-composition.md`).
- Lineage schema-v4 (`derives_from` decision→evidence edge; D5/D7 anticipate it, do not build
  it here).
- Starter promotion of `ideation-cycle` (owner runs `da skills promote` after review; both
  skills are starter candidates under `internal/scaffold/home/starter/skills/global/`).
