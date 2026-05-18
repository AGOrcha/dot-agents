# ADR-0003: Self-review fire ordering inside iteration-close

**Status:** accepted
**Date:** 2026-05-03
**Owners:** dot-agents
**Related:** [ADR-0002](0002-self-review-output-schema.md) (output schema); [`self-review-iteration-close-wiring` plan t1/t3](../../.agents/workflow/plans/self-review-iteration-close-wiring/)

## Context

iteration-close currently runs `verify → checkpoint → merge-back|advance`.
Wiring self-review (per ADR-0002 schema) requires deciding *when* in
that chain self-review fires:

1. **Before verify-record-test** — self-review reads diff first; can
   reject the work before tests are even captured. Faster reject;
   loses the option to use test outcome as a review signal.
2. **After verify-record-test** — self-review reads the diff plus the
   verifier's outcome; uses test pass/fail as a stronger signal.
   Slower (review only fires after tests run); decisive.
3. **In parallel** — both fire concurrently; results join before
   checkpoint. Maximizes wall-clock, complicates failure semantics
   (which decides when results disagree?).

The implications cascade:
- Where self-review's `overall_decision` is `reject`, does
  iteration-close still run merge-back/advance (no — review reject
  blocks closeout)? That's identical regardless of ordering.
- Where verify says `fail` and self-review says `accept`, who wins?
  Currently `mergeReviewIterLog` consults both via the iter-log v2
  schema's `verify_record_appended: true` plus
  `review.overall_decision`. Review can override verify only if
  reviewer has signaled escalation; otherwise verify-fail blocks.
- Where the work failed because of an obvious bug visible in the
  diff (no need to run tests), running tests first is wasted time.

## Decision

**Self-review fires AFTER verify-record-test, BEFORE checkpoint.**

Order:

```
verify record (--kind test --status pass|fail|partial)
  ↓
self-review (reads diff + verify-record outcome; writes review-decision.yaml at ADR-0002 path)
  ↓
verify record (--kind review --task <id> --phase1-decision … --phase2-decision …)
  ↓ (or short-circuits to fold-back if review = reject/escalate)
workflow checkpoint --log-to-iter <N> --role review
  ↓
merge-back | advance
```

Rationale:
- Verifier outcome is real signal. Self-review having access to it
  produces a more decisive `overall_decision` than reviewing in the
  blind.
- Pre-verify-record review duplicates judgment; verify already
  decides "do tests pass?" and self-review's job is "is the work
  correct given that tests pass?"
- Parallel mode complicates failure semantics for marginal
  wall-clock gain. Direct sequence keeps reasoning legible.
- Wasted-test-time concern (option 1 advantage) is mitigated by
  the worker's responsibility to write small focused diffs; if
  diffs are large enough that "test would have caught it" matters,
  the decomposition was wrong and that's a separate signal.

## Consequences

**Easier:**

- Self-review's verdict is informed by verifier outcome — fewer
  cases of "review accepts; tests fail; closeout aborts" because
  review didn't see the test signal.
- iteration-close's instruction trail reads sequentially; no
  parallel-results-join logic needed.
- Existing `verify record --kind review` flow lands as the
  natural second step; same CLI path, just a different `--kind`.

**Harder:**

- Self-review fires only after tests have run, so quick-reject
  cases (obviously bad diff caught by review-only) still consume
  test time. Mitigation: small diffs make tests cheap; worker
  decomposition discipline is the upstream fix.
- iteration-close instruction file gains explicit ordering — must
  document the fire point clearly so future skill-architect reworks
  don't reorder it silently (regression-safe per ADR-0005).

**New risks:**

- Reviewer notes referencing test-output details may become brittle
  if test format changes. Mitigation: review-decision.yaml's
  `reviewer_notes` field is free-text; structural test data lives
  in the verify-record artifact, not duplicated.
- Disagreement semantics (verify pass / review reject, or
  verify fail / review accept) require explicit handling in
  iteration-close's chain. The chain document the decision tree:
  any reject from any stage halts closeout.

**Locked-in:**

- The order. Reordering later requires a superseding ADR; the
  iteration-close skill instructions and the merge-back semantics
  both calibrate to this order.

## Alternatives considered

- **(Option 1) Before verify-record-test** — rejected. Faster
  reject is real but small benefit; loses verifier-outcome signal.
- **(Option 3) Parallel** — rejected. Marginal wall-clock gain
  (most plans serialize through advance/merge-back anyway);
  failure-semantics complexity (whose result wins on disagreement)
  is not worth it for a single-skill review chain.

## References

- `commands/workflow/verification.go` — `verify record` writer.
- `commands/workflow/iter_log.go:397-426` — `mergeReviewIterLog`.
- `~/.agents/skills/dot-agents/iteration-close/instructions/workflow.md` —
  the instruction file t3 modifies to add the new fire point.
- ADR-0002 — output schema self-review writes.
- ADR-0005 — kg-context restoration that runs as Step 0 of self-review.
