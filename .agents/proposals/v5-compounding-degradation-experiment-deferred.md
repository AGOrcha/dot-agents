# Proposal (DEFERRED): v5 — isolate the compounding-chain-length mechanism

**ID:** v5-compounding-degradation-experiment-deferred
**Scope:** project-local (markdown per `proposal-routing` — a deferred follow-up experiment,
not a shared `~/.agents/` resource)
**Status:** **DEFERRED** — captured so the mechanism question is not lost; not scheduled.
**Created:** 2026-06-26
**Author:** agent-proposed, capturing the v4 GATE-2 audit's deferred follow-up
**Evidence / lineage:**
- `.agents/workflow/specs/ideation-system-composition/evidence/depth-degradation-dogfood.md`
  §v4 (the compounding-constraints × capability run) and **§v4.9** (the GATE-2 audit that ruled
  the broad claim NOT-SOUND and deferred the mechanism question here).
- `.agents/proposals/skill-tiering-molecule-composition.md` (the tiering reframe that folds v4
  narrowly; v5 would harden or revise its decompose/fan-out rationale).

---

## Why this exists (and why it is deferred)

v4 induced same-agent drift cleanly on **one** error-prone compounding family (Family A, DAG
transitive-closure) under a binary whole-artifact metric — strongly on Haiku, partially on Opus.
That closed the v1–v3 power gap (a real sub-ceiling baseline now exists). But the GATE-2 audit
(§v4.9) ruled the **broad** interpretation NOT-SOUND: v4 does **not** establish a clean
"compounding-chain LENGTH" mechanism, nor a "route by tier" law, nor generality beyond the one
family. It supports only a **narrow, preliminary** argument for decomposition + code-execution.

The tiering reframe folds that narrow finding and proceeds to ratification **without** waiting on
the mechanism. v5 is the experiment that would turn the narrow, suggestive result into a
mechanism — deferred, not abandoned.

## The open question v5 must answer

**Is there a clean compounding-chain-LENGTH degradation mechanism, isolated from its confounds?**
v4 confounded, in a single complexity knob (node count), at least: chain depth, transitive-edge
count (~836 at the top cell), output size, and closure density. So "fidelity falls as the
compounding chain gets longer" is *suggested* but not *isolated*.

## What v5 would have to do (design constraints, for when it is scheduled)

1. **Disentangle the confounds.** Vary compounding-chain **length** while holding
   transitive-edge-count, output size, and closure density approximately fixed (and vice-versa),
   so a degradation curve can be attributed to one axis. A factorial or matched-pair design.
2. **More than one error-prone family.** v4's effect rests on Family A alone (Family B / ledger
   did not cascade — integer addition is not error-prone). v5 needs ≥2 *independent* error-prone
   compounding families to claim generality.
3. **Routing-grade capability data.** v4's Haiku ≪ Sonnet ≈ Opus map is suggestive only: small N,
   one Sonnet output-budget-exhaustion failure mixed into the metric, one Opus slip. v5 needs
   larger N per cell, a fix for the output-budget confound (separate incompletion from
   miscalculation), and ideally the missing **fair pure-reasoning GPT arm** (v4's was blocked by
   a usage limit; only the agentic, code-writing observation exists).
4. **Keep the binary no-partial-credit metric and re-seeded non-reconstructable inputs** (these
   were sound in v4 — instrument discrimination + cascade proof PASSED) and the **pure-reasoning
   enforcement** (the drift is conditional on forbidding code; that is the right question for
   "how much compounding work fits in one agent hop," but must stay explicit).
5. **Run the deferred relay arm** — does lossy summary relay amplify loss *more* on weaker tiers /
   compounding constraints? (v3 quantified relay loss; v4 deferred the tier-interaction question.)

## What v5 would let the tiering contract claim (only if it passes its own GATE-2)

- A *mechanism* (chain length → fidelity) rather than a one-family correlate → a principled cap on
  per-agent compounding-chain length.
- A *routing-grade* capability × complexity map → tier-routing as a contract rule, not a caveated
  heuristic.
Until then, the contract folds only the narrow v4 finding (decompose error-prone compounding work
into independently-verifiable sub-artifacts; prefer a code-executing agent for computable
closures) and treats the tier map as suggestive.

## Done criteria (for when v5 runs)

- A degradation curve attributable to compounding-chain length with the v4 confounds controlled,
  replicated on ≥2 error-prone families, surviving an independent GATE-2 audit — OR an honest null
  / "confounds inseparable" report. Either outcome updates the tiering proposal's §1.2 and the
  decompose/fan-out rationale.
