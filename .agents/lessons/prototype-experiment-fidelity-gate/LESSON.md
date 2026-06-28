# Lesson: Prototype experiments need a fidelity gate (negative control + independent audit)

## Pattern

We validate design decisions with throwaway prototypes that run real scenarios and
assert hypotheses (config-profiles resolver; KG lease/conflict + projection). A
prototype whose tests **pass** can still give **false confidence** if the experiment
itself is unsound — a strawman model that trivially holds, a non-discriminating
assertion that any implementation would pass, an input that dodges the hard cases, or
a self-reported mutation-check that wasn't really run. This is the
[[tests-must-drive-the-production-path]] failure mode lifted one level up: from "is the
code correct?" to "is the **experiment** valid?"

## Root cause

Trusting a prototype's own green result without checking the two soundness axes the
owner named: **(1) the inputs/design** — is the schema faithful, are the hypotheses the
right questions, do the tests discriminate? — and **(2) the execution** — real inputs,
real interleavings, independently-verified sensitivity? A worker optimizing for "my
proof passes" will, absent a gate, build the experiment that passes, not the experiment
that could fail.

## Rule

Before a prototype's result is allowed to inform a spec, gate it on fidelity:

1. **Faithful inputs, not toys.** Model the REAL schema/data (real enums, real fields,
   the actual failure scenario, the gnarly real files) — not a simplified shape that
   makes the hypothesis trivially true.
2. **Negative control (the load-bearing one).** Don't only show the *right* impl
   passes — build the *broken* version (the exact logic that caused the real bug) and
   prove it **fails** the test. A test no wrong implementation fails proves nothing.
3. **Real execution.** Concurrency under `-race` × many randomized iterations; real
   corpus per-item, not an aggregate; deterministic assertions.
4. **Don't hide losses to pass.** A field that can't round-trip / a case that breaks is
   a RESULT to surface, never a thing to silently drop for a green check.
5. **Independent post-hoc audit.** A cross-harness (different-model) review whose job is
   to **invalidate the experiment** — find the strawman, the non-discriminating assert,
   the fake mutation, the hidden loss — plus re-running the negative control yourself.
   Only an experiment the second brain can't break informs the decision.

## How to apply

- Brief every prototype worker with the fidelity directive up front (faithful inputs +
  negative control + real execution + a fidelity self-audit in the report).
- On return, run the cross-harness fidelity audit BEFORE trusting the verdict; report
  the result AND the proof the experiment was sound, as two separate things.
- This is standing for all prototype-based refinement (the hypothesis→empirical→
  cross-brain→converge methodology), not a one-off.

## Hardened by the depth-degradation experiment arc (v1–v4, 2026-06-26)

Four throwaway agent-behavior experiments on one question (does composition/depth degrade
agent fidelity?), each with a self-audit, each cross-brain-audited. Every single one felt like
"the win" and **every single one was reined in by the independent audit** — v1 confounded
(salient-token recall), v2 wrong-regime (few-KB not the lost-in-the-middle band), v3
underpowered (97.6% ceiling, no headroom), v4 confounded-and-over-generalized (co-varied
five variables; held for one task family). The durable findings that survived were the *small*
ones (relay-loss → structured hand-backs; the infra delegation-nesting ceiling), never the
headline.

1. **The experimenter systematically over-claims its headline.** A self-audit reliably catches
   *internal* faults (does the scorer discriminate? did the negative control fire?) but
   **structurally cannot catch "wrong experiment" / "hollow null" / over-generalized claim** —
   only a *different brain* does. This is the load-bearing reason the audit must be
   cross-harness, not same-model self-review.
2. **Four distinct gates, not one.** Separate (a) **instrument** discrimination (the scorer
   catches a synthetic violation), (b) **experiment** discrimination (the effect *can* occur),
   (c) **regime** validity (you're measuring where the effect actually lives — internal rigor
   ≠ regime validity), (d) **power** (a sub-ceiling baseline exists, so "no effect" can be
   distinguished from "task too easy"). A green self-audit usually only proves (a).
3. **Two-gate cross-brain — audit the DESIGN before the run, the CONCLUSION after.** The
   pre-run design audit is the cheaper, higher-leverage gate: it catches the *wrong experiment*
   (the expensive error class — v1/v2/v3 each paid for a full run a pre-run audit would have
   flagged) before any spend. The post-run audit catches the over-claim. Same independent-brain
   discipline at the two points it pays off.
4. **Null is first-class; the honest scope is narrow.** "Couldn't induce / couldn't reach the
   regime / generalizes to one family only" is a finding, not a failure — fold it *narrow and
   caveated*, never laundered into a clean headline. The contract was already robust without the
   precise mechanism; chasing mechanistic closure past diminishing returns is optional, the
   honest scoping is not.

Operationalized (so it stops being something to remember) in
`.agents/proposals/scientific-method-spine-domain-general.md` — the domain-general
scientific-method spine of the kg-ideate / ideation-cycle pipeline.

## Related

- [[tests-must-drive-the-production-path]] — same class, at the code level; the negative
  control is the experiment-level analog of "mutation-verify the fix."
- [[gates-must-be-locally-reproducible]] — a gate (or proof) you can't trust is worse
  than none.
