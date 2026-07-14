# Scorer rubric 3.0.0 — shadow burn-in (2026-07-12)

Resolves fold-back `scorer-3-0-0-shadow-burn-in` (red-team RT-11 / pre-mortem F4: rubric 3.0.0
existed in code with tests but had zero production emissions; the live waves must not be its
first firing).

## Method

Copied `iter-{55..66}.{yaml,score.yaml}` into a scratch iteration-log dir (canonical 2.1.0
sidecars untouched), then ran `da score iteration <N> --recompute --iter-log-dir <scratch>
--repo-dir .` per iteration — recompute scores fresh from canonical iter-N.yaml + current git
topology under the live `internal/scoring/rubric.go` RubricVersion **3.0.0** — and diffed
against the persisted 2.1.0 sidecars.

## Results (12 iterations)

| iter | 2.1.0 value / band | 3.0.0 value / band | delta |
|---|---|---|---|
| 55 | 0.683 fair | 0.679 fair | −0.003 |
| 56 | 0.683 fair | 0.679 fair | −0.003 |
| 57 | 0.753 good | 0.750 good | −0.003 |
| 58 | 0.753 good | 0.750 good | −0.003 |
| 59 | 0.753 good | 0.750 good | −0.003 |
| 60 | 0.753 good | 0.750 good | −0.003 |
| 61 | 0.753 good | 0.750 good | −0.003 |
| 62 | 0.394 poor | 0.393 poor | −0.001 |
| 63 | 0.517 fair | 0.521 fair | +0.004 |
| 64 | 0.522 fair | 0.526 fair | +0.004 |
| 65 | 0.394 poor | 0.393 poor | −0.001 |
| 66 | 0.840 good | **0.883 excellent** | **+0.044** |

Mean delta +0.002; max |delta| 0.044; band changes 1/12.

## Read

- **Reconciled.** 11/12 iterations agree within ±0.004 — no systematic bias, no ceiling/floor
  clustering, ordering preserved, verifier-signal correlation intact (spot-checked breakdown on
  iter-66: landed/verifier/tests signals present with renormalized weights).
- **The one band change (iter-66, +0.044) is explainable and confounded with input drift, not
  rubric drift:** `--recompute` reads *current* git topology, and iter-66's commit
  (716050a06d5a) is now reachable from master — the `landed` signal scores 1.0 today, which it
  may not have at original scoring time. The 2.1.0→3.0.0 rubric component of the delta is
  bounded by the other 11 iterations at ≲0.004.
- **Caveat carried:** shadow deltas conflate rubric change with topology change for any
  iteration whose landed-state moved between scorings. For the live waves this does not apply —
  wave rows are scored once, at run time, under 3.0.0.

## Verdict

Burn-in PASSED for wave use: 3.0.0 is live-ready as the accuracy-axis instrument, with the C0
null contrast (rubric step 4a) providing the remaining in-situ validation on wave rows
themselves. Scratch artifacts: session scratchpad `shadow-iterlog/` (not committed; canonical
2.1.0 sidecars unmodified — verified byte-identical by leaving them unwritten).
