# Eval-fidelity verifier — dot-agents repo overlay (meta)

Repo-local committed layer for the `meta` profile, scoped to **eval-backed** deliverables:
run/session evaluation, hypothesis-testing runs, analysis, product-refinement, and KG-content
refinement. The base contract (`verifiers/verifier.base.md`) and kind (`verifiers/eval-fidelity.md`)
do not resolve in this repo, so this overlay is **self-sufficient**. Role: verify the **fidelity** of
the experiment/eval that backs the deliverable per the fidelity-gate discipline
(`.agents/lessons/prototype-experiment-fidelity-gate/LESSON.md`). This is NOT a code-test verifier —
it audits whether the experiment is *sound enough to inform a decision*.

## How to run

Read the deliverable's method + inputs + result, then **re-run its negative control yourself** (never
trust the self-reported one). Score it against the five fidelity axes:

1. **Faithful inputs, not toys.** Real schema/enums/fields, the actual failure scenario, the gnarly
   real files — not a simplified shape that makes the hypothesis trivially true.
2. **Real negative control (load-bearing).** The *broken* version (the exact logic of the real bug) is
   built and PROVEN to FAIL the same test. Re-run it — a test no wrong impl fails proves nothing.
3. **Real execution.** Real corpus per-item (not an aggregate), real interleavings (`-race` ×
   randomized iterations where concurrency is in play), deterministic assertions.
4. **No hidden losses.** A field that can't round-trip / a case that breaks is a RESULT to surface,
   never silently dropped for a green check.
5. **Discrimination + regime + power** (the v1–v4 hardening): the instrument catches a synthetic
   violation, the effect *can* occur, you measured the regime where it actually lives (internal rigor
   ≠ regime validity), and a sub-ceiling baseline exists so a null ≠ "task too easy". No cherry-pick.

## Assert (positive + negative)

- **Positive:** all five axes hold; the negative control FIRES when you re-run it; the deliverable's
  claimed scope matches what the method supports.
- **Negative — reject the wrong proof:** a strawman model that trivially holds; a non-discriminating
  assertion any impl passes; a dodged hard case; a fake / un-run mutation-check; a hollow null
  laundered into a clean headline; a claim over-generalized past the one family actually tested.

## Record + evidence

`da workflow verify record --kind test --status pass|fail --verifier-type eval-fidelity --summary "..."`.
On fail, name the failing axis + the specific defect. Capture: your negative-control re-run result
(you ran it), the faithful-input mapping (deliverable input ↔ real schema), and the honest scope/caveat
vs the headline claimed. Any failed axis ⇒ `--status fail` — an unsound experiment must not inform a spec.
