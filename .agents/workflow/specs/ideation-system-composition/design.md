# Spec: ideation-system composition — kg-ideate (authoring) ⟷ ideation-cycle (fork-resolution)

**Spec ID:** ideation-system-composition
**Status:** **DRAFT — owner-ruled on the OQs; pending human ratification + the v4 experiment.**
The four prior OQs have an owner-ruled direction (§7) folded into the decisions, but no
artifact here claims final ratification: the empirical backing (evidence sidecar v1–v3) is
power-limited and a v4 experiment (referential/stateful constraints × weaker-model power) is
**in flight**. Treat D1–D8 as decided-direction, not ratified contract.
**Created:** 2026-06-26
**Author:** agent-proposed (drafted with agent assist), pending human review
**Related:**
- `.agents/workflow/specs/kg-ideate-skill/design.md` — the authoring front-end (this spec
  proposes a Phase 2 delta to it; the delta itself is routed as a project-local proposal,
  `.agents/proposals/kg-ideate-phase2-fork-triage.md`, not a unilateral spec rewrite).
- `internal/scaffold/home/starter/skills/global/ideation-cycle/SKILL.md` — the
  fork-resolution **compound** this spec formalizes.
- `.agents/workflow/specs/ideation-system-composition/evidence/depth-degradation-dogfood.md`
  — the evidence sidecar (D7): v1–v3 depth/relay experiments + cross-harness audit. **v4 in
  flight.** Source for every depth/relay claim below.
- `.agents/proposals/skill-tiering-molecule-composition.md` — the tiering-contract delta the
  same evidence drives (tier-adjacency dropped; relay discipline added).
- `.agents/rules/dot-agents/workflow-artifact-model.md` — the spec→plan→tasks→history tiers.
- `.agents/workflow/specs/skill-tiering-contract/design.md` — tier vocabulary
  (atom/molecule/compound/cell); its depth-2–3 cliff claim **did not replicate** (sidecar).
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
the idea→spec *fork-resolution* and RETURNS ratified decisions + evidence; `spec-scaffold`
writes the prose; `kg-ideate` still owns the whole pipeline), (b) the **shared grounding**
(`kg-brief`), and (c) the **handoff back** ("ratified decisions → `spec-scaffold` writes the
spec → continue to `plan-scaffold`"), so both get built coherently rather than re-derived.

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
- **Relay discipline**: delegation hand-backs are structured/pointer-based, never retold prose
  — the one positive, reproducible finding of the depth investigation (sidecar v3 family-2).
- **Lineage is an evolution edge** (`derives_from`/`supersedes`), not a sibling edge — noted
  for the later lineage-schema work to inherit.

Non-goals: re-authoring the ideation *profile* (verifiers/reviewers/relevance — owned by
`ideation-execution-profile`); rebuilding `kg-ideate`'s phases (owned by `kg-ideate-skill`);
authoring the tiering lint / relay-discipline lint (owned by `skill-tiering-contract` + its
refinement delta `.agents/proposals/skill-tiering-molecule-composition.md`, plan-tier work).

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
| Tier | T2 compound | **T2 compound** (it orchestrates delegated workers — prototype, audit, cross-brain — with unbounded judgment; see D8) |
| Owns | the WHOLE pipeline: idea → spec → plan → handoff | the idea→spec **fork-resolution** within the segment |
| Verb | AUTHORS the pipeline grounded in the KG | how idea→spec is now DONE: grounded idea → **ratified decision + evidence** |
| Output | spec + plan + handoff | **ratified decision(s) + per-fork evidence sidecar** — NOT the spec prose (that is `spec-scaffold`'s job; see D3) |

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
Phase 1 briefing), `ideation-cycle` MAY consume that briefing **by artifact** (no re-run) —
**but only if it is FRESH**, judged by an `inputs_digest` over a **concrete, canonicalized
input set** plus a **dependency manifest** the brief records.

**The `inputs_digest` input set (exact, ordered, canonicalized):**
1. the idea/proposal text — content hash;
2. the KG snapshot id (or query-engine revision) the brief queried against;
3. the named-query **results** the brief consumed (each query id + its result-set hash);
4. the applicable-lessons set (lesson ids + each lesson file's content hash);
5. the cited-artifact set — every spec/proposal the brief cited, by path + content hash.

These are ordered and canonicalized, then hashed via the **config-v2 `ComputeInputsDigest`
primitive** (`sha256:…`; staleness = digest mismatch — `internal/config/staleness.go`,
`resolver.go`). No parallel scheme.

**Dependency manifest (records what the brief READ):** the brief also records *which* KG
nodes / decisions / lessons it actually read (the entries that fed its conclusions).
**Invalidation rule:** the brief is stale — re-run `kg-brief` — if the `inputs_digest`
mismatches, OR **any dependency-manifest entry changed** (this is the operational definition of
"a prior fork's resolution mutated shared brief state": a prior fork that re-ratified a
decision, wrote a lesson, or changed a KG node the brief read flips that manifest entry → the
brief no longer reflects current state → re-brief). A stale brief must never silently
propagate. Standalone runs (no upstream artifact) always run `kg-brief` fresh.
`ideation-cycle`'s step 1 (`ground-via-kg-brief.md`) enforces this gate.

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
   empirical[fidelity-gate] / cross-brain → ratify).

**Triage guard (closes the silent-skip-the-gate hole):** a **"briefing-decidable" verdict MUST
cite the decisive briefing fact** (the specific prior decision / lesson / query result that
settles it). **If no such fact is citable, the fork defaults to HARD** — the loop runs. And the
**cross-brain pass reviews the TRIAGE decisions themselves**, not only the hard forks: a
different harness checks whether each "briefing-decidable" call was genuinely settled by its
cited fact, so a fork cannot be quietly waved past the gate by mislabeling it easy.

Boundary test: the briefing answers it *with a citable decisive fact* (prior ratified
decision, clear single-resolution gap) → briefing-decidable, inline. The briefing surfaces a
contradiction / unbacked `[PROPOSED]` / unadjudicable trade-off, OR nothing decisive is
citable, AND getting it wrong is costly → hard fork → full loop.

**Segment ownership (resolves the who-writes-the-spec contradiction):** `ideation-cycle`
**RETURNS** the ratified decision(s) + the per-fork evidence sidecar; **`spec-scaffold` WRITES
the spec prose.** `ideation-cycle` does **not** author the spec file. The evolved segment's
output is a set of ratified, evidence-backed decisions; `spec-scaffold` turns them into the
`design.md` text (Phase 2), then control returns to `kg-ideate` to continue at Phase 3
(`plan-scaffold`). `ideation-cycle` owns the idea→spec **fork-resolution method**; `spec-scaffold`
owns the **prose**; `kg-ideate` owns the **surrounding pipeline**. This is the delta routed to
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

- **Dispatched** (from `kg-ideate` Phase 2): resolve one named fork; **return the ratified
  decision + evidence sidecar pointer** to `spec-scaffold`; do not author any spec prose.
- **Standalone** (one-off design question, or a fork surfaced from execution): run the full
  cycle to a ratified decision + evidence sidecar; converge hands those to a spec-drafting
  step (delegated `spec-scaffold`-equivalent) to write the prose — `ideation-cycle` still does
  not type the spec file itself (D3 segment ownership).

Both modes ground via `kg-brief` (D2) and gate prototypes via the fidelity gate.

### D6 — The composition is bounded by ENGINEERING constraints, not a measured fidelity cliff (OQ1 ruling, re-grounded)

OQ1 asked whether the `kg-ideate → spec-scaffold → ideation-cycle` chain breaks a dispatch-hop
bound. We dogfooded the underlying question (evidence sidecar v1–v3, two harnesses) and the
answer reshapes the decision:

- **The depth-2–3 fidelity cliff did NOT replicate.** No same-agent composition degradation was
  **found in the tested regime** — flat constraint-honoring through **depth 10 and ~15k
  accumulated tokens**, including an adversarial buried-constraint placement, on Opus 4.8 and
  codex/GPT. **This null is power-limited** (depth-1 ceiling ~97.6%, never sub-90%): so **no
  cliff was FOUND in the tested regime — NOT "no cliff exists."** The harder referential/stateful
  regime + weaker-model power is under test in **v4 (in flight)**. The tiering contract's "degrades
  past depth ~2–3" premise must **not** be cited as established fact (sidecar §v3.6).
- **Tier-adjacency is dropped anyway** — it was always the wrong lever (a molecule/compound may
  call any tier; composition is governed by judgment-autonomy, not which tier calls which). So
  the chain is legal; no hoist is forced. (Delta:
  `.agents/proposals/skill-tiering-molecule-composition.md`.)
- **The real, evidence-backed bounds are engineering, not cognitive:** (a) a hard **infra
  delegation-nesting ceiling (~hop 4)** — nested `Agent`-tool delegation collapses past ~hop 4
  (reproduced), so deep delegation must be **driver-orchestrated hop-by-hop**, not recursively
  nested; (b) **relay discipline** (D8); (c) hygiene — context isolation, parallelism,
  write-scope, inspectability.

So: the chain is fine, but for the honest reason (adjacency was wrong + the bound is infra +
relay discipline), not because of a 2-hop cognitive cliff we could demonstrate.

### D7 — A hard fork's evidence is a per-fork SIDECAR, linked from the decision (OQ2 ruling)

A hard fork's evidence — the prototype dir, the negative-control result, the cross-brain
audit verdicts — is its **own sidecar artifact**, LINKED from the decision entry. Not inlined
into the spec, not in transient task notes. The founding instance already exists:
`evidence/depth-degradation-dogfood.md` (v1–v3, this spec's own depth fork). This anticipates
lineage schema-v4 (the decision `derives_from` its evidence sidecar — consistent with D5's
evolution-edge lineage). Standalone mode links the sidecar from the decision `spec-scaffold`
writes up; dispatched mode returns the sidecar pointer to `spec-scaffold` to link.

### D8 — `ideation-cycle` is a COMPOUND, and delegation hand-backs MUST be structured (relay discipline)

**Re-tier:** `ideation-cycle` is a **T2 compound**, not a molecule. By the tiering contract's
own definition a compound "orchestrates… with unbounded judgment" — and `ideation-cycle`
orchestrates **delegated workers** (prototype authors, the independent cross-harness auditor,
the cross-brain reviewer) with unbounded judgment over which to run and how to weigh them. That
is a compound, not a bounded molecule. Frontmatter, the composition narrative, and the tiering
proposal's example are corrected to `tier: compound`.

**Relay discipline (the one positive empirical finding):** every delegation hand-back in the
loop (worker → driver, hop → hop) **MUST be structured / pointer-based** — return the artifact
path(s) + a structured constraint/decision checklist — **NOT a retold prose summary.** Lossy
summary relay verifiably drops **non-reconstructable** detail, and that loss **reaches the
deliverable**: sidecar v3 family-2, a summary hand-back compressed away an arbitrary schema
choice and the terminal artifact degraded **16/16 → 13/16**; verbatim/structured relay was
lossless across 8 hops. Reconstructable defaults survive a retell; non-reconstructable
structural detail does not — so it is never retold. This is encoded in the tiering-contract
delta (D-refine-4) and in `ideation-cycle`'s `composition.md` + `empirical-pass.md` hand-back
guidance.

## 4. Requirements (behavioral)

1. A standalone `ideation-cycle` run with no `kg-ideate` in flight runs `kg-brief` fresh to
   ground, resolves the fork to a ratified decision + evidence sidecar, then hands those to a
   spec-drafting step (it does not type the spec file itself).
2. A `kg-ideate` Phase 2 run that hits a briefing-decidable decision (with a **citable
   decisive fact**) authors it directly — it does NOT dispatch to `ideation-cycle`. Triage is
   autonomous with a surfaced rationale; an uncitable "easy" verdict defaults to HARD.
3. A `kg-ideate` Phase 2 run that hits a hard fork dispatches to `ideation-cycle`, receives a
   ratified decision + evidence sidecar pointer, and **`spec-scaffold` writes the spec prose**
   — `ideation-cycle` does not author the spec file.
4. No prototype result informs any decision until it has cleared the fidelity gate
   (faithful inputs + negative control + real execution + independent cross-harness audit).
5. Neither skill reinvents grounding: there is exactly one briefing shape (`kg-brief`'s),
   consumed by both.
6. When dispatched, `ideation-cycle` reuses the upstream briefing by artifact ONLY when its
   `inputs_digest` matches AND no dependency-manifest entry changed; otherwise it re-runs
   `kg-brief`. A stale briefing is never silently reused.
7. A hard fork's evidence is a per-fork sidecar linked from the decision entry (not inlined,
   not in transient task notes).
8. The cross-brain pass reviews the **triage decisions** (not only the hard forks), and every
   delegation hand-back is structured/pointer-based (relay discipline), never retold prose.

## 5. Done criteria

1. `ideation-cycle/SKILL.md` declares `tier: compound`, `calls:` including `kg-brief` (legal
   once tier-adjacency is dropped), and `dispatchable_from: [kg-ideate]`.
2. `ideation-cycle` step 1 runs-or-reuses-if-fresh `kg-brief` and never hand-scans the
   baseline (Requirement 5), with the `inputs_digest` + dependency-manifest freshness gate
   (Requirement 6).
3. This composition spec exists, cites the evidence sidecar for every depth/relay claim, and
   is cross-referenced from `ideation-cycle/SKILL.md` and the `kg-ideate` Phase 2 delta.
4. The `kg-ideate` Phase 2 fork-triage + dispatch delta is captured as a project-local
   proposal (NOT a unilateral edit to `kg-ideate-skill/design.md`) and references this spec.
5. The tiering-contract refinement (tier-adjacency dropped; relay discipline added; §1.2 cliff
   claim corrected) is captured as `.agents/proposals/skill-tiering-molecule-composition.md`.
6. The four OQs (§7) have an owner-ruled direction folded into D2–D8; **no artifact claims
   final ratification** — all stay DRAFT pending human ratification + the v4 experiment.

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

## 7. The OQs — owner-ruled direction (DRAFT, not finally ratified)

The four prior open questions have an **owner-ruled direction** folded into §3. They are NOT
claimed as finally ratified: the empirical backing (sidecar v1–v3) is power-limited and **v4 is
in flight**. Human ratification is still pending. Summary and where each lives:

**OQ1 — Dispatch-hop bound → D6: engineering bounds, not a measured cliff.** The depth-2–3
fidelity cliff **did not replicate** (no same-agent degradation found ≤ depth 10 / ~15k tokens,
two harnesses; **power-limited null** — "not found in the tested regime", not "doesn't exist";
**v4 in flight**). Tier-adjacency is dropped regardless (it was the wrong lever), so the chain
is legal with no hoist. The real bounds are the **infra delegation-nesting ceiling (~hop 4)**,
**relay discipline (D8)**, and hygiene. Evidence: sidecar; delta:
`.agents/proposals/skill-tiering-molecule-composition.md`.

**OQ2 — Fork evidence → D7: per-fork SIDECAR, linked from the decision.** Founding instance:
`evidence/depth-degradation-dogfood.md`. Anticipates lineage schema-v4 (`derives_from` edge).

**OQ3 — Triage authority → D3: AUTONOMOUS + surfaced rationale, with a guard.** Human gate is
spec ratification, not per-fork triage; but a "briefing-decidable" verdict must cite a decisive
fact (else default HARD) and the cross-brain pass reviews the triage calls themselves.

**OQ4 — Registration → D4: BOTH.** Top-level invocable AND dispatchable from `kg-ideate`.

**Plus (not an original OQ, surfaced by the evidence/codex review):** `ideation-cycle` is
re-tiered **molecule → compound** (D8); the `inputs_digest` is given a concrete input set +
dependency manifest (D2); segment ownership is fixed so `spec-scaffold` writes the prose (D3).

## 8. Open / deferred

- **v4 experiment (IN FLIGHT)** — referential/stateful constraints × weaker-model power. May
  surface a real same-agent degradation regime the power-limited v1–v3 null cannot exclude. If
  it does, D6 and the tiering §1.2 correction get revised; D8's relay discipline is independent
  of v4 and stands either way. **No artifact here is final until v4 lands + human ratification.**
- The tiering / relay-discipline lint (plan-tier; owned by `skill-tiering-contract` + its
  refinement delta).
- Lineage schema-v4 (`derives_from` decision→evidence edge; D5/D7 anticipate it, do not build
  it here).
- Starter promotion of `ideation-cycle` (owner runs `da skills promote` after review; both
  skills are starter candidates under `internal/scaffold/home/starter/skills/global/`).
