# Proposal: tiering contract refinement — tier-adjacency is the wrong lever; relay discipline + infra-bounded nesting are the real ones

**ID:** skill-tiering-molecule-composition
**Scope:** project-local (markdown per `proposal-routing` — amends a repo spec, not a shared
`~/.agents/` resource)
**Status:** proposed (delta against `skill-tiering-contract`). Owner-ruled on direction;
**DRAFT pending human ratification + the v4 experiment** (referential/stateful constraints ×
weaker-model power) still in flight. Do not treat as finalized.
**Created:** 2026-06-26
**Author:** agent-proposed, capturing an owner ruling, grounded in the evidence sidecar
**Targets:**
- `.agents/workflow/specs/skill-tiering-contract/design.md` — §3 tier table intent column,
  §4 tier invariants (T1/T2 Composition bullets), §5 done-criteria (lint fixtures), §1.2
  (the cited "degrades past 2–3 hops" reliability claim).
**Evidence:**
- `.agents/workflow/specs/ideation-system-composition/evidence/depth-degradation-dogfood.md`
  — three throwaway experiments (v1 token-recall, v2 multi-constraint drift, v3 regime-valid
  token-mass × depth × lossy-relay) + cross-harness codex replication. **v4 in flight.**
**Motivated by:**
- `.agents/workflow/specs/ideation-system-composition/design.md` — the
  `kg-ideate → spec-scaffold → ideation-cycle` chain crossing tier-adjacency, and the
  empirical investigation that chain triggered.

---

## Why this is a delta, not a rewrite

`skill-tiering-contract` is the canonical tier spec. Per `proposal-routing`, a change to it
is **proposed as a delta**; the owner folds it in after human ratification. This proposal
captures the owner-ruled direction plus the empirical findings; it is not yet final (v4 in
flight).

## The problem with the current contract — and what the evidence actually shows

The current contract restricts `calls:` by **tier-adjacency** (a molecule calls only atoms; a
compound calls only molecules) and justifies a tight depth bound by citing a reliability
**cliff** "past depth ~2–3 hops" (§1.2, shivsakhuja / `the_smart_ape`). Two separate things
are wrong here, and the evidence separates them:

1. **Tier-adjacency is the wrong lever (independent of any cliff).** Whether or not a cognitive
   cliff exists, restricting *which tier may call which* buys no reliability — it only forces
   spurious structural hoists (e.g. bouncing a dispatch up to a compound so a molecule never
   calls a molecule/compound). This refinement stands regardless of the empirical question.

2. **The depth-2–3 fidelity cliff did NOT replicate.** We dogfooded the question with three
   throwaway experiments under the fidelity gate (sidecar v1–v3), two harnesses (Opus 4.8 and
   codex/GPT). **No same-agent composition fidelity cliff was found in the tested regime** —
   constraint-honoring stayed flat through **depth 10 and ~15k accumulated tokens** (v3 reached
   the >10k "lost-in-the-middle" regime v1/v2 dodged), including an adversarial placement (the
   most-driftable constraint buried at the recency-disfavored level 1). **This null is
   power-limited** (the depth-1 ceiling held at ~97.6%, never the sub-90% needed to fully
   separate "robust" from "task too easy" — a fair sub-ceiling on self-checkable constraints may
   not be reachable for frontier models). So: **no cliff was FOUND in the tested regime — NOT
   "no cliff exists."** The harder referential/stateful regime and weaker-model power are under
   test in **v4 (in flight)**. The "degrades past depth ~2–3" premise must therefore **not be
   cited as established fact.**

## The ruling (the delta)

### D-refine-1 — Composition is governed by judgment-autonomy, not tier-adjacency (KEEP)

- **A molecule MAY call molecules and compounds; a compound MAY call anything.** Tier no longer
  constrains WHICH tier you may call.
- **Tier denotes AUTONOMY semantics only:**
  - **atom** — indivisible; declares **no downstream calls** (unchanged — a leaf by definition).
  - **molecule** — bounded runtime judgment over its **declared `calls:` set** (may now include
    molecules/compounds).
  - **compound** — unbounded runtime judgment within its declared `calls:` set.
- This holds **regardless** of the cliff question (justification 1 above). It is the part of the
  original refinement that survives the null.

### D-refine-2 — The depth bound rests on ENGINEERING grounds, not a measured cognitive cliff

The contract should **stop justifying its bound by a same-agent fidelity cliff** (unsupported —
§"what the evidence shows"). Replace that justification with the constraints that ARE
evidence-backed or mechanically real:

- **(a) Infra delegation-nesting ceiling (~hop 4) — a hard harness mechanic.** Nested
  `Agent`-tool delegation **collapses past ~hop 4**: subagents spawned via the `Agent` tool
  lack the dispatcher tool themselves, so a recursive chain degrades to same-agent
  (reproduced across v2 and v3). Bound **delegation NESTING depth by this harness mechanic** —
  not by a cognitive cliff. Deep multi-hop delegation must be **driver-orchestrated hop-by-hop**
  (fresh top-level `Agent` per hop, relay via on-disk artifact), never recursively nested.
- **(b) Relay discipline** — see D-refine-4 (the headline new rule).
- **(c) Hygiene, not fidelity.** Delegation remains worthwhile for **context isolation,
  parallelism, write-scope discipline, and inspectability** — the honest reasons — not a
  measured depth-2–3 fidelity threshold.

Same-agent composition `calls:` depth carries **no evidence-backed cognitive cap** at the
depths tested (≤10); any soft depth signal the lint keeps is for **inspectability**, not a
claimed reliability cliff, and must be labeled as such (and revisited after v4).

### D-refine-3 — Lint change

- **DROP** any "molecule-with-a-molecule-call → error" (and compound-calls-compound error).
  Tier-adjacency is no longer a lint rule.
- **ADD** "**delegation-nesting depth approaching the harness ceiling (~hop 4) → warning**"
  (recursive `Agent`-tool nesting; recommend driver-orchestration past ~hop 3). This is the
  mechanically-real bound, replacing the old cognitive "depth > 2" warning.
- **ADD** "**delegation bundle hand-back not structured/pointer-based → warning**" (relay
  discipline, D-refine-4).
- **KEEP** unchanged: atom-with-downstream-calls → error; molecule/compound-without-verifier →
  error; compound-without-review-or-attendance → error; **per-skill > 10 children → warning**.
- Any same-agent composition-depth signal, if kept at all, is a high-threshold
  **inspectability** warning, explicitly **not** justified by a fidelity cliff.

### D-refine-4 — NEW CONTRACT RULE: relay discipline (the headline, evidence-backed)

**Delegation hand-backs MUST be structured / pointer-based — a returned artifact path plus a
structured constraint/decision checklist — NOT retold prose.**

This is the one positive, reproducible finding of the whole investigation. Lossy **summary**
relay verifiably drops detail, and where the dropped detail is **non-reconstructable** it
**reaches the deliverable**: in sidecar v3 family-2, a summary hand-back compressed away an
arbitrary schema choice ("env as a list of {key,value} mappings" → "4 key/value entries") and
the terminal artifact **degraded to 13/16 vs 16/16 baseline**. **Verbatim / structured relay
was lossless across 8 hops.** Reconstructable defaults (e.g. "cite the version in each section")
survive a lossy retell because a competent author re-derives them; **non-reconstructable
structural detail does not** — so it must be relayed verbatim or by pointer, never retold.

Rule text to add to the contract:
> A delegation hand-back (worker → parent, and hop → hop in a multi-hop chain) MUST return
> **(i) the artifact path(s)** produced and **(ii) a structured checklist** of the constraints
> / decisions the work was accountable to — not a natural-language retelling. Prose summary
> relay is prohibited for any non-reconstructable structural constraint, because it
> demonstrably drops such detail and that loss reaches the deliverable (evidence: sidecar v3).

### D-refine-5 — Edits to the spec text

- **§1.2** — soften the cited reliability claim: the "agents degrade past 1–2 / 2–3 hops"
  finding **did not replicate** in our tests (no same-agent cliff ≤ depth 10 / ~15k tokens, two
  harnesses; null power-limited; v4 pending — cite the sidecar). Keep the *infra* nesting
  ceiling (~hop 4) as the mechanically-real bound.
- **§3 tier table "Intent" (T1)** — "judgment bounded to picking among declared **atoms**" →
  "...among declared **calls (atoms, molecules, or compounds)**."
- **§4 T1 Composition** — "`calls:` lists the **atoms** it invokes" → "`calls:` lists the
  **atoms/molecules/compounds** it invokes"; drop the atom-only judgment phrasing.
- **§4** — keep T0 as-is (atom = leaf, no downstream calls).
- **§4 / new** — add the **relay-discipline** invariant (D-refine-4) to the delegation-bundle
  contract surface.
- **§5 done-criteria lint fixtures** — replace the adjacency fixture with: "lint passes a
  molecule-calls-molecule/compound edge; warns on recursive delegation-nesting past ~hop 4;
  warns on a non-pointer/non-checklist delegation hand-back; still errors on atom-with-calls."

## What stays the same

- Tier still self-declared + lint-verified (contract D3).
- Atom is still a leaf (no downstream calls).
- molecule/compound verifier + compound review-gate requirements unchanged.
- The > 10-children warning is unchanged.
- Additive: no existing artifact's tier changes; the `calls:` adjacency restriction is lifted,
  the lint swaps adjacency-check → (nesting-ceiling + relay-discipline) checks, and the §1.2
  justification is corrected to match the evidence.

## Verification (once folded + the lint exists)

- A fixture molecule whose `calls:` includes a molecule/compound **passes** lint (was an
  adjacency violation).
- A fixture with recursive `Agent`-tool delegation nesting past ~hop 4 raises a **warning**.
- A fixture delegation hand-back that is retold prose (no artifact pointer + checklist) raises a
  **warning**.
- An atom declaring any downstream call still raises an **error**; > 10-children still warns.

## Open / pending (do not finalize without)

- **v4 experiment** (referential/stateful constraints × weaker-model power) — may surface a real
  same-agent degradation regime the current power-limited null cannot exclude. If it does, §1.2
  and D-refine-2 get revised again; the relay-discipline rule (D-refine-4) is independent of v4
  and stands either way.
- **Human ratification** of the contract delta.
