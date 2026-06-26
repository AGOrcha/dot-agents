# Spec: ideation-system composition — kg-ideate (authoring) ⟷ ideation-cycle (fork-resolution)

**Spec ID:** ideation-system-composition
**Status:** draft (for review) — D1–D4 proposed, several open questions need an owner ruling
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
authoring the lint that would enforce the dispatch-hop bound (plan-tier work).

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

### D2 — Grounding is a single shared stage (`kg-brief`)

Both skills ground on the same primitive: the `kg-brief` molecule (KG / research / lessons
→ the briefing block). `ideation-cycle` does NOT carry its own baseline scan. When
dispatched from `kg-ideate`, it consumes the Phase 1 briefing; standalone, it invokes
`kg-brief` to produce the same briefing. This is the anti-duplication invariant — the
reframe's core requirement. `ideation-cycle`'s step 1 (`ground-via-kg-brief.md`) enforces it.

### D3 — `kg-ideate`'s idea→spec front (Phase 1 + Phase 2) evolves INTO invoking `ideation-cycle`

This is NOT "bolt a dispatcher onto Phase 2." The idea→spec transition — `kg-brief`
grounding (Phase 1) followed by `spec-scaffold` (Phase 2) — *becomes* the `ideation-cycle`
loop. Spec authoring itself is now `ideation-cycle`'s output, folded back into `kg-ideate`'s
pipeline before Phase 3.

Within that evolved segment, triage still applies (not every decision needs the full loop):

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

### D4 — `ideation-cycle` is dual-invocable

- **Dispatched** (from `kg-ideate` Phase 2): resolve one named fork; return decision +
  evidence; do not author the surrounding spec.
- **Standalone** (one-off design question, or a fork surfaced from execution): run the full
  cycle, including seeding/refining the spec at converge.

Both modes ground via `kg-brief` (D2) and gate prototypes via the fidelity gate.

## 4. Requirements (behavioral)

1. A standalone `ideation-cycle` run with no `kg-ideate` in flight invokes `kg-brief` to
   ground, then resolves the fork end-to-end and seeds/refines the spec.
2. A `kg-ideate` Phase 2 run that hits a briefing-decidable decision authors it directly —
   it does NOT dispatch to `ideation-cycle` (no over-dispatch).
3. A `kg-ideate` Phase 2 run that hits a hard fork dispatches to `ideation-cycle`, receives
   a ratified decision + evidence pointer, and folds it into the spec — `ideation-cycle`
   does not write that spec.
4. No prototype result informs any decision until it has cleared the fidelity gate
   (faithful inputs + negative control + real execution + independent cross-harness audit).
5. Neither skill reinvents grounding: there is exactly one briefing shape (`kg-brief`'s),
   consumed by both.

## 5. Done criteria

1. `ideation-cycle/SKILL.md` declares `tier: molecule`, `reuses: [kg-brief]`,
   `dispatchable_from: [kg-ideate]`, and its `calls:` molecule/atom set.
2. `ideation-cycle` step 1 consumes-or-invokes `kg-brief` and never hand-scans the baseline
   (Requirement 5 demonstrated).
3. This composition spec exists and is cross-referenced from both `ideation-cycle/SKILL.md`
   and the `kg-ideate` Phase 2 delta proposal.
4. The `kg-ideate` Phase 2 fork-triage + dispatch delta is captured as a project-local
   proposal (NOT a unilateral edit to `kg-ideate-skill/design.md`) and references this spec.
5. The dispatch-hop ambiguity (§7 OQ1) is recorded as an open question with a recommended
   default, for the owner to rule before the dispatch seam is implemented.

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

## 7. Open questions (need an owner ruling)

**OQ1 — Dispatch-hop bound (the live ambiguity).** The chain
`kg-ideate (compound) → spec-scaffold (molecule) → ideation-cycle (molecule) → kg-brief`
can reach 3 dispatch hops, past the reliable 1–2-hop bound (`skill-tiering-contract` §1.2).
Two in-bound options, **recommend (a)**:
- **(a) Hoist the dispatch to the compound.** `spec-scaffold` *flags* a decision as a hard
  fork and returns control to the `kg-ideate` compound, which dispatches `ideation-cycle`
  as a sibling phase. Every edge stays ≤ 2 hops from the compound.
- **(b) Reuse `kg-brief` by artifact, not dispatch.** When dispatched, `ideation-cycle`
  consumes the already-produced briefing (no `kg-brief` re-dispatch) — the last edge is
  data-passing, not a dispatch hop. Standalone runs are hop-1 to `kg-brief` and fine.
These are not mutually exclusive — (b) is sound regardless; (a) is the structural fix for
the spec-scaffold→ideation-cycle edge. Owner picks whether to require (a), (b), or both.

**OQ2 — Where does the dispatched fork's evidence live?** `ideation-cycle` produces
prototype modules + audits. When dispatched, does that evidence attach to the
`kg-ideate` plan's task notes, to the spec's Related section, or to a per-fork evidence
sidecar? (Standalone mode already records it against the spec it seeds.)

**OQ3 — Triage authority.** Does `spec-scaffold` decide briefing-decidable-vs-hard-fork
autonomously, or surface the triage to the human/orchestrator for confirmation before
dispatching (dispatch is expensive — a full empirical cycle)? Recommend: autonomous triage
with a one-line surfaced rationale, human can veto.

**OQ4 — Standalone entry registration.** `ideation-cycle` is dual-invocable. Is it
registered as a top-level invocable skill (like `kg-ideate`), or only reachable via
`kg-ideate` + an explicit `--question` standalone flag? Recommend: top-level invocable —
forks surface from execution too, not only from authoring.

## 8. Deferred

- Lint enforcing the dispatch-hop bound (plan-tier; `skill-tiering-contract` reserves it).
- Starter promotion of `ideation-cycle` (owner runs `da skills promote` after review; both
  skills are starter candidates under `internal/scaffold/home/starter/skills/global/`).
