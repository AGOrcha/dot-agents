# Step 5: Cross-brain — the two-gate independent adversarial audit

Cross-brain is one discipline — *an independent brain whose job is to invalidate* — applied
at the **two points where it pays off**:

- **GATE 1 — invalidate the DESIGN (pre-run).** For an empirically-testable fork, before the
  experiment runs, a different harness audits the *design*: are the hypotheses falsifiable, the
  predictions pre-committed, the power/regime adequate, the negative control real, the inputs
  faithful, the evidence form appropriate? Its job is to **send a flawed design back before the
  spend**. This is the cheaper, higher-leverage gate — it catches the *wrong experiment* (the
  expensive error class), not just the wrong conclusion.
- **GATE 2 — invalidate the CONCLUSION (post-run).** After the run, a different harness audits
  the *result*: does the evidence actually support the claim, what confounds remain, where is it
  over-claimed, is the null hollow? (This is the fidelity-gate audit — `fidelity-gate.md`.)
- **Judgment-call forks (no empirical handle)** still get a **single cross-brain trade-off
  pass** — there is no run to gate, so the independent brain argues the rejected option and
  names the decisive trade-off (the rest of this file).

Same independent-brain discipline at all three; what changes is whether it's invalidating a
design, a conclusion, or a trade-off choice. The two gates are the load-bearing reason the
audit is **cross-harness, not same-model**: a self-audit reliably catches *internal* faults
but structurally cannot catch "wrong experiment" / "hollow null" / over-generalized claim —
only a different brain does (caught us 3× in the depth-degradation arc).

## Quota discipline — serialize the codex calls

Cross-brain calls go to a different harness (codex) with a finite quota. **Serialize them** —
run GATE 1, GATE 2, and judgment-call passes one at a time, not fanned out in parallel — so a
burst of concurrent calls doesn't exhaust the quota mid-cycle. Batch the forks for a given gate
into as few calls as the framing allows.

## The judgment-call trade-off pass

For every fork tagged **judgment-call** in step 3, get a cross-harness (codex) adversarial
opinion. These are the trade-off forks no single run decides — the answer is a defensible
choice across competing values, not an empirical fact. A second brain that argues the
*other* option is the reliable way to pressure-test the choice before it is ratified.

## Why a different harness

Same-model self-review rubber-stamps the option you already lean toward — it reads the
fork as "justifying X" rather than "is X actually better than the alternative?". A
different-model reviewer, briefed to argue against, surfaces the costs you discounted.
This is the same dynamic that makes cross-harness review the catch for hollow tests
(`tests-must-drive-the-production-path`), applied to design choices.

## How to run it

1. **Frame each fork neutrally.** Give the codex reviewer the fork, both (all) options,
   the baseline anchor from step 1, and the stakes — without telegraphing your preference.
2. **Ask it to argue the strongest case for each option**, then name the decisive
   trade-off and recommend one, with the cost of being wrong.
3. **Collect the opinions** and reconcile them with your own analysis. Where you and the
   second brain disagree, that disagreement is itself signal for the owner — surface it.

## Also review the TRIAGE decisions (not only the judgment calls)

The cross-brain pass closes the silent-skip-the-gate hole (triage guard, step 3): hand the
different harness the **full step-3 tag list** plus each `already-settled` / briefing-decidable
verdict's **cited decisive fact**, and ask: *"is any 'easy' call actually a disguised hard
fork — does the cited fact genuinely settle it?"* Any fork the second brain flags as
mis-triaged is re-routed to the empirical or cross-brain gate. A fork must not reach converge
labeled easy on an uncited or unconvincing basis.

## Output: a ranked must-decide list

Produce a single ranked list of must-decide items for the owner. Each item:

- **The fork** (question + options).
- **Recommended default** — the option you and the cross-brain converge on (or your
  pick plus the dissent if you diverge).
- **Rationale** — the decisive trade-off, in one or two sentences.
- **Cost of the wrong call** — what it would take to reverse if the default is wrong
  (cheap-to-reverse forks rank lower; expensive/locked-in forks rank higher and deserve
  more owner attention).

Rank by stakes × reversibility: the irreversible, high-blast-radius forks go to the top
of the owner's attention. This list, plus the empirical verdicts from step 4, is what the
owner ratifies in step 6.
