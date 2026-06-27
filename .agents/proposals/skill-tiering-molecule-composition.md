# Proposal: tiering contract refinement — tier-adjacency is the wrong lever; relay discipline + infra-bounded nesting are the real ones

**ID:** skill-tiering-molecule-composition
**Scope:** project-local (markdown per `proposal-routing` — amends a repo spec, not a shared
`~/.agents/` resource)
**Status:** proposed (delta against `skill-tiering-contract`). Owner-ruled on direction;
**DRAFT pending human ratification.** **No longer blocked on v4** — v4 returned and is folded
NARROW (its GATE-2 audit ruled the broad claim NOT-SOUND; see §"what the evidence shows" + sidecar
§v4.9); the mechanism question is **deferred to v5**
(`.agents/proposals/v5-compounding-degradation-experiment-deferred.md`). Do not treat as finalized.
**Created:** 2026-06-26
**Author:** agent-proposed, capturing an owner ruling, grounded in the evidence sidecar
**Targets:**
- `.agents/workflow/specs/skill-tiering-contract/design.md` — §3 tier table intent column,
  §4 tier invariants (T1/T2 Composition bullets), §5 done-criteria (lint fixtures), §1.2
  (the cited "degrades past 2–3 hops" reliability claim).
**Evidence:**
- `.agents/workflow/specs/ideation-system-composition/evidence/depth-degradation-dogfood.md`
  — four throwaway experiments (v1 token-recall, v2 multi-constraint drift, v3 regime-valid
  token-mass × depth × lossy-relay, **v4 compounding constraints × capability**) + cross-harness
  codex replication and GATE-2 audits. **v4 folded narrow (§v4.9 audit); v5 deferred.**
**Motivated by:**
- `.agents/workflow/specs/ideation-system-composition/design.md` — the
  `kg-ideate → spec-scaffold → ideation-cycle` chain crossing tier-adjacency, and the
  empirical investigation that chain triggered.

---

## Why this is a delta, not a rewrite

`skill-tiering-contract` is the canonical tier spec. Per `proposal-routing`, a change to it
is **proposed as a delta**; the owner folds it in after human ratification. This proposal
captures the owner-ruled direction plus the empirical findings (v1–v4 folded, v4 narrow per its
GATE-2 audit; v5 mechanism deferred); it is **ready for human ratification, no longer blocked on
v4**, but not yet final.

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
   separate "robust" from "task too easy" on *local self-checkable* constraints). So for that
   regime: **no cliff was FOUND — NOT "no cliff exists."** The "degrades past depth ~2–3 hops"
   premise must therefore **not be cited as established fact.**

3. **v4 found a NARROW degradation — on compounding work, not on hop-depth.** A fourth run
   (sidecar §v4) closed the v1–v3 power gap by switching to **error-prone, referential,
   COMPOUNDING** constraints with a **binary, no-partial-credit** metric: unaided reasoning then
   drifts (Haiku 80%→40%→0%; Opus slips to 80%) on one family. **But its GATE-2 audit (§v4.9)
   ruled the broad reading NOT-SOUND:** it is NOT a clean compounding-chain-LENGTH *mechanism*
   (confounded node-count, depth, ~836 edges, output size, density), NOT a "route by tier" law
   (small N, one family, one budget-exhaustion failure, one Opus slip), and NOT generalizable
   beyond the one family. So v4 supports only the **narrow** argument folded in D-refine-2(d)
   (decompose error-prone compounding work; prefer code execution for computable closures). The
   **mechanism** question is **deferred to v5**
   (`.agents/proposals/v5-compounding-degradation-experiment-deferred.md`). Crucially, even the
   narrow v4 result is about **compounding-chain length, not skill-to-skill hop depth** — it does
   **not** revive the depth-2–3 hop cliff.

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
evidence-backed or mechanically real.

**These bounds are HARNESS-CAPABILITY-DEPENDENT, not universal laws.** The numeric thresholds
below (the ~hop-4 nesting ceiling; the capability map; even what "decompose" needs to buy) were
**observed on the current harness set** (Claude Code's `Agent` tool + `codex exec`). The *tiering
model* is stable — composition is governed by judgment-autonomy + the engineering bounds — but its
**numbers are per-harness** and must be **re-assessed as the harness set grows** (Hermes, Pi-agent,
Aider, Antigravity-CLI, …). The contract records each bound **with the harness it was observed on**,
and treats a new harness's delegation / nesting / capability limits as a **variable to measure, not
assume** (a harness with deeper reliable nesting raises the ceiling; a weaker one lowers it). Do not
read any number below as a constant.

- **(a) Infra delegation-nesting ceiling (~hop 4) — observed on the CURRENT harness, re-assess
  per harness.** On Claude Code's `Agent` tool, nested delegation **collapses past ~hop 4**:
  subagents spawned via the `Agent` tool lack the dispatcher tool themselves, so a recursive chain
  degrades to same-agent (reproduced across v2 and v3). This is a property of *that* harness's
  tool surface, **not a universal constant** — a harness whose subagents can themselves delegate
  raises this bound; record the observed ceiling against the harness. Regardless of the number,
  deep multi-hop delegation should be **driver-orchestrated hop-by-hop** (fresh top-level worker
  per hop, relay via on-disk artifact) rather than relying on recursive nesting, until a given
  harness is *measured* to nest reliably deeper.
- **(b) Relay discipline** — see D-refine-4 (the headline new rule).
- **(c) Hygiene, not fidelity.** Delegation remains worthwhile for **context isolation,
  parallelism, write-scope discipline, and inspectability** — the honest reasons — not a
  measured depth-2–3 fidelity threshold.
- **(d) Decompose error-prone COMPOUNDING work — narrow, preliminary support (v4).** A v4 run
  (sidecar §v4; **GATE-2-audited NOT-SOUND for the broad claim**, see §v4.9) showed that on **one**
  error-prone, referential, *compounding* task family (DAG transitive-closure) under a **binary,
  no-partial-credit** metric, unaided reasoning drifts — strongly on a weak tier (Haiku: 80%→40%→0%
  as the chain grows), and even slips on Opus (80% at the top cell). Because a single early miss
  cascades to the whole artifact, ~2–4% per-element error becomes a 20–60pp whole-artifact failure.
  **Narrow, caveated takeaways:** (i) **decompose** long error-prone compounding chains into
  **independently-verifiable sub-artifacts** — which is exactly what fan-out / delegation with
  bounded write-scopes buys; this is a *capability* argument for decomposition, stated honestly,
  NOT a revival of the depth-2–3 myth. (ii) For **computable** closures (DAG reachability, ledger
  folds, referential invariants), prefer a **code-executing agent** — better-supported than
  tier-routing (a code-writing agent solved the task with a 5-line script). **Do NOT over-read
  this:** v4 did NOT isolate a compounding-chain-LENGTH *mechanism* (it confounded node-count,
  depth, ~836 transitive edges, output size, closure density), is NOT a "route by tier" law (small
  N, one family, one output-budget-exhaustion failure, one Opus slip), and did NOT generalize
  beyond the one family (the ledger family did not cascade). The Haiku ≪ Sonnet ≈ Opus map is
  **suggestive, not routing-grade.** The mechanism question is **deferred to v5**
  (`.agents/proposals/v5-compounding-degradation-experiment-deferred.md`).

Same-agent composition `calls:` depth carries **no evidence-backed cognitive cap** at the
depths tested (≤10) for *local self-checkable* constraints; for *error-prone compounding*
constraints v4 shows whole-artifact risk that grows with chain size (narrow, one family) →
decompose rather than impose a generic depth number. Any soft depth signal the lint keeps is for
**inspectability**, not a claimed reliability cliff, and must be labeled as such (revisited if v5
isolates a mechanism).

### D-refine-3 — Lint change

- **DROP** any "molecule-with-a-molecule-call → error" (and compound-calls-compound error).
  Tier-adjacency is no longer a lint rule.
- **ADD** "**delegation-nesting depth approaching the active harness's ceiling → warning**"
  (recursive `Agent`-tool nesting; on the current harness the ceiling is ~hop 4, so warn /
  recommend driver-orchestration past ~hop 3). The threshold is a **per-harness configured value**,
  not a hardcoded 4 — the lint reads the active harness's observed ceiling; this replaces the old
  cognitive "depth > 2" warning.
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
  harnesses; null power-limited — sidecar v1–v3). v4 found a *narrow* drift on **compounding
  chains** (not hop depth), GATE-2-audited NOT-SOUND for the broad claim; mechanism deferred to
  v5 — cite the sidecar §v4/§v4.9. Keep the *infra* nesting ceiling as the mechanically-real
  bound, but record it as **current-harness-observed (~hop 4 on Claude Code's `Agent` tool),
  re-assessed per harness** — not a universal constant.
- **§3 tier table "Intent" (T1)** — "judgment bounded to picking among declared **atoms**" →
  "...among declared **calls (atoms, molecules, or compounds)**."
- **§4 T1 Composition** — "`calls:` lists the **atoms** it invokes" → "`calls:` lists the
  **atoms/molecules/compounds** it invokes"; drop the atom-only judgment phrasing.
- **§4** — keep T0 as-is (atom = leaf, no downstream calls).
- **§4 / new** — add the **relay-discipline** invariant (D-refine-4) to the delegation-bundle
  contract surface.
- **§5 done-criteria lint fixtures** — replace the adjacency fixture with: "lint passes a
  molecule-calls-molecule/compound edge; warns on recursive delegation-nesting past the **active
  harness's configured ceiling** (~hop 4 on the current harness); warns on a non-pointer/non-checklist
  delegation hand-back; still errors on atom-with-calls."
- **§1.2 / new** — record that the engineering bounds (nesting ceiling, the v4 capability map) are
  **harness-capability-dependent**: each bound is stored with the harness it was observed on, and a
  new harness's limits are a variable to **measure**, not assume.

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
- A fixture with recursive `Agent`-tool delegation nesting past the active harness's configured
  ceiling (~hop 4 on the current harness) raises a **warning**.
- A fixture delegation hand-back that is retold prose (no artifact pointer + checklist) raises a
  **warning**.
- An atom declaring any downstream call still raises an **error**; > 10-children still warns.

## Open / pending

- **v4 — FOLDED (narrow).** v4 returned; its GATE-2 audit ruled the broad claim NOT-SOUND, so it
  is folded only as narrow, preliminary support for decomposition + code-execution (D-refine-2(d)).
  **This proposal is no longer blocked on v4** — it is ready for human ratification.
- **v5 (DEFERRED)** — `.agents/proposals/v5-compounding-degradation-experiment-deferred.md`: would
  isolate the compounding-chain-LENGTH mechanism from v4's confounds, across ≥2 error-prone
  families, with routing-grade N. If v5 passes its own GATE-2, D-refine-2(d) hardens from a
  caveated heuristic to a mechanism/routing rule; otherwise it stays narrow. The relay-discipline
  rule (D-refine-4) is independent of v4/v5 and stands either way.
- **Human ratification** of the contract delta — the only remaining gate.
