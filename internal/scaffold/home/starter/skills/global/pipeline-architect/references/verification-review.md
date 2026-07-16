# Deep dive — verification and review spine (§4)

Operational depth behind `instructions/verification-review.md`. Model- and registry-agnostic:
no model, vendor, or closed registry is named, and no plan-scoped evidence anchor appears.
Source of truth: `docs/full-loop-pipeline-craft.md` §4.

---

## The spine

The inner pipeline is a strict, gated sequence:

```
executor
  → bounded verifier slots      (each gated on the prior verifier's PASS)
  → bounded routine review lenses (each gated on the prior lens + all verifiers passed; read-only)
  → the blocking cross-family lens
  → the evidence gate
```

**Cardinality is capped, and the cap is configured, not constant.** Over-cardinality is an
explicit BLOCKED refusal, never a silent truncation — a pipeline that quietly drops the seventh
verifier is lying about its coverage. Verifiers and reviewers **NEVER mutate canonical workflow
state**; only the gate stage pushes the owner-held PR, polls the delivery gate for the task's
app-type, and authors the merge-back draft. Verification-then-review-then-gate is the ordering,
and each stage's route must equal its own declared model and family — a stage may not silently
borrow another stage's route.

The ordering matters: verification proves the change *works* before any reviewer spends tokens
arguing about it, and review completes before the gate spends effort publishing it. Each gate is
a hard precondition on the next stage, so a failure short-circuits the remaining spend.

---

## What real discipline looks like

Separate **live discipline** (what a pipeline actually enforces) from **prescribed-but-dead
prose** (what a prompt merely asks for). Three properties distinguish the two:

- **In-session verification is non-optional, per app-type.** Where tool outcomes persist,
  workers ground on version-control status plus tests/lints *before* claiming completion. That
  grounding is a **stage**, not a suggestion — it is emitted into the pipeline and gates the
  next stage, so it cannot be skipped by an agent in a hurry.
- **Review verdicts are structured and wired in-loop.** A review emits a structured verdict —
  risk level, authorization, outcome, rationale — that **binds to the target harness's native
  quality gate**, not a free-text "LGTM". The structure is what lets the gate act on the verdict
  mechanically. This generalizes beyond loop-workers: the same structured-verdict contract is
  the right shape for advisory and one-off review too.
- **Falsification-first is the review contract, not affirmative render.** A review states
  **pre-registered falsifiable hypotheses**, each *executed* to a verdict (refuted / survived /
  inconclusive) rather than argued. Null results are first-class. A **zero-refutation review is
  returned as not-performed** — a review that only affirms the work has not reviewed it. This is
  the discipline delta over an affirmative case-study render: the reviewer must try to break the
  work, on the record, before it can pass.

---

## Unverifiable signals

On a harness that persists **no** tool result, tool outcomes, exit codes, and errors are
unrecoverable and visible only as narration. A review or verifier signal that leans on
self-reported result/verification text is therefore **unverifiable**: a coarse substring scorer
would rate such a transcript high while nothing in-transcript corroborates that any work
happened. The failure is asymmetric — the narration can claim success the tools never confirmed.

The defense is a hard evidence gate: **require an anchor plus a real tool/verifier record**;
never accept self-report as a verification signal. An anchor without a corroborating tool record
is narration; a tool record without an anchor is unattributed. Both are required together.

---

## Cross-family gate (with §2)

The blocking adversarial review MUST run on a **different model family** than the executor; same
family on both sides makes the review invalid. Bind it to the **named** adversarial lens,
require its family to differ from the executor/default family, and reject on any
blocker-or-high-severity finding. Never bind to a numeric slot index or an assumed list order —
that binding breaks silently when the lens list changes. See the design/loop/routing deep dive
for the binding mechanics.

---

## Rules

- Emit an in-session verification stage on every implementing pipeline, per app-type; a verifier
  PASS gates the next stage, and no verifier/reviewer mutates canonical state.
- Cap stage cardinality (bounded verifiers, bounded routine lenses) and refuse (BLOCK) on
  overflow rather than silently truncating.
- Require structured review verdicts (risk / outcome / rationale) that bind to the target
  harness's native gate, not free-text approval.
- Make review falsification-first: a verdict with zero executed refutation hypotheses is
  not-performed.
- NEVER accept self-reported completion as a verification signal on a harness without persisted
  tool results; require an anchor plus a real tool/verifier record.
