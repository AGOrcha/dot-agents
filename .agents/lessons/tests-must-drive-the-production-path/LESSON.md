# Lesson: Tests must drive the production path (a green test that doesn't is worse than none)

## Pattern

An implementer ships a change with **passing tests and full coverage**, but the
tests don't actually exercise the production code path a real caller uses — so the
green checkmark certifies nothing. Three forms seen in one wave (graph-chain t2/t4,
all caught by cross-harness review, none by the implementer's own tests or a
same-model review):

1. **Tautological comparison.** A "dual-read parity" test compared the input
   corpus to itself via the same `Snapshot()` function (counting edges that were
   never even ingested), so it could never detect a real bridge-vs-native
   divergence. (t4)
2. **Lifecycle papered over in the test.** A "fire trigger → query returns rows"
   test passed only because it **manually called the second step** (`ApplyDerivation`)
   that the production trigger (`FireEnvTrigger`) never invoked — a real caller got
   zero rows. (t2)
3. **Green helper that nothing calls.** A migration_only safety gate
   (`ValidateReadsFrom`) had correct, well-tested logic, but no load path ever
   invoked it — so the rule never fired in production. Unit-green, integration-absent. (t4 finding 4)

## Root cause

The test was written to **pass**, not to **falsify the production lifecycle**. The
assertion ran against test-constructed state or a manually-driven sequence, not
against the real entry point + readback. Coverage went up (lines executed) while
verification stayed at zero (the behavior a caller depends on was never asserted
end-to-end). Same-model self-review misses this because it reads the test as
"testing X" rather than checking *whether the production path actually does X*.

## Rule

A test must drive the **same entry point a real caller uses** and assert the
**observable end state**, then be **mutation-verified**:

1. **Drive the public path, not the internals.** Call only what a caller calls
   (the trigger, the loader, the adapter bootstrap) — never hand-invoke an
   intermediate step inside the test to make the assertion pass. If the public path
   can't produce the asserted state, that's a production bug, not a test setup step.
2. **Compare independent productions, not shared construction.** For
   parity/equivalence, build the two sides through independent paths and compare
   their **readbacks** (e.g. store-seam reads per namespace) — never compare a value
   to itself or to the inputs it was derived from.
3. **A rule/gate needs an integration test at the real call site.** Unit tests on a
   helper prove the helper; add a test that the gate actually **fires at load /
   dispatch** and rejects the bad case. A callable-but-uncalled gate is unprotected.
4. **Mutation-verify.** Break the production code (remove the drop, delete the gate
   call, skip the propagation) and confirm the test **fails**. A test that still
   passes when production is broken is not a test.

## How to apply

- Before declaring a task done, ask: "If I deleted the production implementation and
  kept the test, would it fail?" If not, the test is hollow — rewrite it.
- For "X then query Y returns Z" lifecycles: the test calls only X and asserts Z.
  If you need to call an intermediate step manually, the production path is broken.
- For parity/diff/equivalence: assert over readbacks from two independent writes,
  not over the shared corpus/function that produced both.
- Cross-harness adversarial review is the reliable catch for this class — a
  different-model reviewer asks "does the production path do what the test claims?"
  where same-model self-review rubber-stamps. Treat it as a **blocking** gate on
  substantive work. See [[sonarcloud-gate-mechanics]] for why green ≠ verified.

## Related

- [[tests-for-each-slice]] — each slice adds coverage; this lesson adds that the
  coverage must be *meaningful* (drives production, mutation-verified), not just line-executing.
- [[gates-must-be-locally-reproducible]] — sibling: a gate's pass must mean what it claims.
