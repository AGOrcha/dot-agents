# Cross-harness review: trust the raw alternate-harness output, not the same-family reconciler's verdict

**Captured:** 2026-07-15
**Triggered by:** t6-oci-consume (package-artifact-install). The `cross-harness-adversarial-reviewer`
agent (Sonnet) dispatched Codex `gpt-5.6-sol` correctly, but its *reconciliation* returned
**"clean pass" twice** while Codex's raw output returned **fail with a BLOCKER + HIGH** each round.
Real, ship-blocking security defects (unpinned-tag MITM, seeded-cache type bypass) were only caught
because the raw Codex file was read directly — the reconciler had rationalized them away.

## The mistake

Running a cross-harness review to get a *different model family's* adversarial read, then trusting
the **reviewer agent's reconciled verdict** as if it were that other brain's finding. The reviewer
is (usually) the same family as the code's author. When it "reconciles" the alternate harness's
findings against its own independent trace, its same-family priors **demote or explain away** the
other brain's real findings:

- On t6 round-1 the reviewer's own trace said "clean pass"; the raw Codex output said "fail — BLOCKER
  digest conflation + HIGH cache bypass." The reviewer never read Codex's text back (a lock blocked
  it) and reported its own clean verdict.
- On t6 round-3 the reviewer *did* read Codex back but **demoted Codex's HIGH to LOW** in
  reconciliation. (That demotion happened to be correct — but the point is the reconciler's verdict
  is a same-family editorial layer over the other brain's findings, not a faithful relay.)

A same-family reconciler laundering a different-family reviewer's findings **defeats the entire
purpose** of going cross-harness — the one check "a lab will never ship" (see
[[tests-must-drive-the-production-path]], caught 3× the same way) is worthless if a sibling model
gets to overrule it silently.

## Why it happens

The reviewer agent is asked to "reconcile," which invites it to apply its own judgment over the
alternate harness's raw findings. Its judgment shares the author model's blind spots. The alternate
harness's value is precisely its *different* priors — collapsing them through a same-family filter
throws that away. Worse, environmental failures (a lock, a timeout) can prevent the reviewer from
reading the raw output at all, and it will still emit a confident verdict from its own trace.

## The rule

When a cross-harness review runs on anything security- or correctness-critical:

1. **Require the raw alternate-harness output as an artifact** — the reviewer must persist Codex's
   verbatim findings to a scratchpad file and cite the path. No raw file → the review did not happen;
   re-run it.
2. **Read the raw output yourself** before accepting the verdict. Treat every BLOCKER/HIGH the *other
   brain* raised as real until you personally disprove it against the diff — do not accept the
   reconciler's demotion on faith.
3. **A "clean pass" from the reviewer's own trace is not a cross-harness pass.** If the reviewer
   couldn't read the alternate harness back (lock, timeout, degraded skip), its verdict is a
   *same-family* review — re-dispatch, don't ship on it.
4. Send findings back to the implementer keyed to the **other brain's** file:line + scenario, not the
   reconciler's summary.

## How to apply

- In the cross-harness reviewer brief: "Save Codex's raw output to a scratchpad file and cite it; I
  will read it directly." Then actually read it (grep for `severity:`/`verdict:` to extract the
  findings block cheaply from a large transcript).
- Budget a round-2/round-3 as the default expectation for a security-sensitive slice — this wave took
  **three** rounds on t6, each surfacing a real defect the same-family pass rated clean.
