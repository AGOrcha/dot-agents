# Gotchas: Ideation Cycle

The highest-signal failure points. Read before the empirical pass.

## A green prototype can prove the wrong thing

- This caught **both** KG prototypes (lease/conflict and projection). A prototype whose
  tests pass is NOT evidence until it clears the fidelity gate. The strawman model, the
  non-discriminating assert, the input that dodges the hard case, the fake mutation-check
  — all pass green and all prove nothing.
- The negative control is load-bearing: if no broken implementation fails the test, the
  test certifies nothing. Re-run the negative control yourself; don't trust the
  self-reported mutation-check.
- The independent cross-harness audit's job is to **invalidate**, not confirm. If it
  can't break the experiment, only then is the result usable. Reference:
  `prototype-experiment-fidelity-gate`.

## Don't turn the ideation driver into the prototype author

- The driver scopes, steers, and audits. Dispatch the prototype and spec authoring to
  subagents (Hybrid orchestration model). A driver that hand-builds the experiment loses
  the independence that makes the audit meaningful, and burns the context the framing
  needs.
- Propagate the fidelity directive into every worker bundle — it binds down the whole
  delegation chain, not just the top.

## Don't fake-empiricize a judgment call

- A prototype can only confirm what you set it up to assert. Pointing one at a trade-off
  fork produces a "proof" of whatever option you encoded — not a decision. Trade-offs go
  to cross-brain (step 5), not to a prototype.
- Conversely: don't send an empirically-determinable fork to opinion because prototyping
  felt like work. "Empirical where possible, else cross-brain" — the test is whether you
  can write a negative control.

## Don't skip the baseline sweep

- A fork raised without the prior thinking gets re-litigated from scratch — the exact
  "we keep re-explaining this" the skill exists to kill. Every fork must trace to a
  baseline row. If it doesn't, you're reinventing.
- A `[PROPOSED]` decision is NOT settled — it still needs ratification in step 6. A
  *ratified* decision is a constraint — don't re-open it without new evidence.

## Don't let the spec become a plan

- The spec owns what/why + decisions + open questions + done-criteria. File paths,
  function names, and task breakdowns belong in the plan (execution half). A spec that
  accumulates them has drifted; move that content out. Reference: `workflow-artifact-model`.

## Isolate experiments from the coverage gate

- Each prototype gets its own `go.mod` and lives under `.agents/active/experiments/` (or
  similar), out of the repo's test/coverage gate. It is throwaway evidence, not product
  code — it must never ship or count toward coverage.

## Don't declare the loop closed on a modeled result

- An audited prototype validates a *modeled* scenario. The dogfood pass validates the
  *real corpus* (e.g. zero-diff migration). A clean prototype + a dirty dogfood diff
  means the decision is not yet validated — the diff is a new fork, not a rounding error.
