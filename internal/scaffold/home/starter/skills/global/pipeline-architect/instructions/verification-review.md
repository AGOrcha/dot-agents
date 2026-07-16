# Verification + review spine

Operationalizes **§4** of the public guide `docs/full-loop-pipeline-craft.md`.
Load when sizing a verifier chain, choosing lens sets, wiring a review gate, or defining evidence
gates.

**Deep dive:** [`references/verification-review.md`](../references/verification-review.md) carries
the full spine, the live-vs-dead-prose distinction, the unverifiable-signal failure, and the
cross-family binding. This file is a concise loader.

---

## The spine

Strict, gated sequence: **executor → bounded verifier slots** (each gated on the prior's PASS) →
**bounded routine review lenses** (each gated on prior + all verifiers passed, read-only) → **the
blocking cross-family lens** → **the evidence gate**. Verifiers and reviewers NEVER mutate
canonical workflow state; only the gate stage pushes the owner-held PR, polls the app-type
delivery gate, and authors the merge-back draft.

## Architect rules

- **Emit an in-session verification stage on every implementing pipeline, per app-type.** A
  verifier PASS gates the next stage, and no verifier/reviewer mutates canonical state.
  Verification is real only where tool outcomes persist.
- **Cap stage cardinality (bounded verifiers, bounded routine lenses) and refuse (`BLOCKED`) on
  overflow** rather than silently truncating. The cap is configured, not a constant.
- **Require structured review verdicts** (risk / authorization / outcome / rationale) that bind to
  the target harness's native quality gate, not free-text "LGTM".
- **Make review falsification-first:** pre-registered falsifiable hypotheses, each *executed*
  (refuted / survived / inconclusive), null results first-class; a verdict with **zero** executed
  refutation hypotheses is returned as **not-performed**.
- **NEVER accept self-reported completion as a verification signal on a harness without persisted
  tool results;** require an anchor plus a real tool/verifier record.

## Cross-family gate (with §2)

The blocking adversarial review MUST run on a **different model family** than the executor; same
family both sides ⇒ review invalid. Bind it to the **named** adversarial lens, require its family
to differ from the executor/default, and reject on any blocker-or-high-severity finding. Never
bind to a numeric slot index.

## Config/command surface

- Verifier/lens sets and order come from the execution profile's per-app-type map; resolve them
  per app-type.
- Each verifier/reviewer prompt resolves through the resolve seam; assert non-empty model /
  model family and, for the cross-family slug, `family != executor.family`.
- Verifier precondition gating is a precondition policy on the stage profile that names a key in
  the precondition-policy registry.
- After editing, lint the config, then spot-check with the resolve seam before emitting the
  projection.
