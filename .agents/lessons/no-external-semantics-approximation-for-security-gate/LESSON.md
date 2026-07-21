# Don't approximate an external tool's semantics to gate a security decision

**Captured:** 2026-07-16
**Triggered by:** the t9 perf pass (package-artifact-install). A fast-path in `EnsureAndVerifyCASIgnore`
tried to SKIP the fail-closed H14 gate (install a `.gitignore` rule + verify the CAS store is
git-ignored before writing fetched content) by **predicting** whether git already ignores the path —
reimplementing git's gitignore matching via go-git plus custom line-trimming. Cross-harness review
found a real H14 bypass in **three consecutive rounds** — a symlinked `.gitignore`, a leading-whitespace
pattern, then a trailing-tab + `!cache/` negation — each a different way the approximation diverged from
real git. The fix that finally held was to **drop the optimization** and always canonicalize+verify.

## The mistake

Making a **security decision** ("is this path safe to write / already protected?") depend on a
**local reimplementation or approximation of an external tool's semantics** (git's gitignore matching,
here — but the same applies to a permission model, a shell/glob parser, a URL/host matcher, a
canonicalizer). The external tool's real semantics have edge cases the approximation won't match, and
each edge is a gate bypass. You cannot enumerate your way to safety — round 2 fixed two variants, round
3 fixed a third, and a fourth was plausible. An approximation used for a *display hint* is fine; used to
*gate a fail-closed security check*, every divergence is a hole.

Two compounding traps this instance exposed:

- **Skipping a "redundant" write can drop a load-bearing SIDE EFFECT.** The gate's install step
  (`WriteFileAtomic` temp+rename) didn't just *add* an ignore rule — it **canonicalized** a symlinked or
  malformed `.gitignore` into a clean regular file. The fast path skipped the write when it *looked*
  already-correct, silently removing that canonicalizing side effect the downstream check depended on.
  Before skipping a write, verify its side effects were redundant too — not just its nominal output.
- **Even careful same-brain inspection misses these.** I personally inspected the fast path and
  concluded it was gate-preserving; the cross-harness brain proved me wrong — twice. See
  [[cross-harness-read-raw-not-reconciler]].

## The rule

When a security/correctness decision depends on an external tool's behavior, do **not** approximate
that behavior to take a shortcut. In order of preference:

1. **Invoke the tool** (or its real library) and trust its answer.
2. **Constrain the input to an unambiguous shape** where the answer is trivially correct — e.g. gate a
   fast return on **byte-identity to a known-canonical form**, or a structural invariant (here: the
   round-3 attempt required the managed block be *terminal*) — not on reproducing the tool's full
   matching semantics. (Even the structural attempt couldn't be *proven* airtight within the review
   budget, which is itself the signal to stop.)
3. **Move the correct-but-costly path so it runs less often** (hoist it out of a hot loop) rather than
   skipping it based on a prediction. The safe perf win here was hoisting the ignore-install to
   once-per-`install`/`refresh` (the pattern is input-independent), tracked as fold-back
   `cas-ignore-install-run-level-hoist` — no semantics reimplementation.
4. **If none of the above is provable, drop the optimization.** The slow, always-correct path beats a
   fast one you can't prove safe. A perf win on one flow is never worth an unclosable security gate.

## How to apply

- Smell test during review/design: *"does this optimization's safety depend on my code matching an
  external tool's edge-case behavior?"* If yes, it's suspect — prefer invoke / constrain-input / hoist /
  drop.
- A perf change that touches a security-adjacent gate is NOT exempt from the cross-harness gate — this
  one shipped through green tests and my own inspection and was caught only cross-harness. "Just perf"
  is not a reason to skip the second brain when the change sits next to a fail-closed check.
- When a gate takes 3+ rounds each finding a new variant of the same class, stop patching variants and
  change the approach (structural) or remove it — that cadence is the signal the approach is wrong.
