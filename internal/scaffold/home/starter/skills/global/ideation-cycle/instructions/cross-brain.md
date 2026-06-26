# Step 5: Cross-brain the judgment calls

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
