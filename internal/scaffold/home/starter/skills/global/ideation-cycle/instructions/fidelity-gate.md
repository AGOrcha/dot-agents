# Step 4 (load-bearing): The fidelity gate

This is the step that caught **both** KG prototypes proving the wrong thing. A prototype
whose tests **pass** can still give **false confidence** if the experiment itself is
unsound — a strawman model that trivially holds, a non-discriminating assertion any impl
would pass, an input that dodges the hard case, or a self-reported mutation-check that
was never really run. No prototype result informs the spec until it clears this gate.

Canonical reference: lesson `prototype-experiment-fidelity-gate`. This file is the
operational checklist; that lesson is the why. It is the experiment-level lift of
`tests-must-drive-the-production-path` — from "is the code correct?" to "is the
**experiment** valid?"

## The two soundness axes

Every prototype is gated on both:

1. **Inputs / design** — Is the schema faithful? Are the hypotheses the right questions?
   Do the assertions discriminate between the options?
2. **Execution** — Real inputs, real interleavings, independently-verified sensitivity?

## The five checks

1. **Faithful inputs, not toys.** Real schema/data — real enums, real fields, the actual
   failure scenario, the gnarly real files. Not a simplified shape that makes the
   hypothesis trivially true.
2. **Negative control (the load-bearing one).** Don't only show the *right* impl passes —
   build the *broken* version (the exact logic that caused the real bug) and prove it
   **fails** the test. A test that no wrong implementation fails proves nothing.
3. **Real execution.** Concurrency under `-race` × many randomized iterations; the real
   corpus per-item, not an aggregate; deterministic assertions.
4. **Don't hide losses to pass.** A field that can't round-trip, a case that breaks — that
   is a RESULT to surface, never a thing to silently drop for a green check.
5. **Independent post-hoc audit.** A cross-harness (different-model, e.g. codex)
   review whose explicit job is to **invalidate the experiment** — find the strawman, the
   non-discriminating assert, the fake mutation, the hidden loss, the false-pass, the
   model-faithfulness gap — PLUS re-running the negative control itself. Only an
   experiment the second brain can't break informs the decision.

## Running the gate

1. **Brief the gate up front.** The prototype worker is given checks 1–4 as a directive
   before it builds anything (in the empirical-pass bundle). The gate is not a surprise
   on return; it is the spec for the experiment.
2. **Self-audit on return.** The worker's report includes a fidelity self-audit answering
   all five axes. Read it as a separate artifact from the result.
3. **Re-run the negative control yourself.** Don't trust the self-reported mutation-check
   — break the production logic and watch the test go red.
4. **Independent cross-harness audit.** Dispatch the codex adversarial audit. Its job is
   to INVALIDATE, not to confirm. It reports: model faithfulness, whether asserts
   discriminate, any false-pass, any hidden loss.
5. **Re-run until the audit passes.** If the audit breaks the experiment, the experiment
   is fixed and re-run — the *result is not used* until an audited-sound version exists.

## The bright line

> Only an audited-sound experiment is allowed to inform the spec.

A prototype that passes its own tests but fails the gate proves nothing. Report the
result AND the proof the experiment was sound, as **two separate things** — a verdict
without its fidelity audit is not evidence.
