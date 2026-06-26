# Step 3: Classify each fork

Every fork gets exactly one tag. The tag is its resolution route — it decides whether the
fork goes to the empirical pass (step 4), the cross-brain pass (step 5), or straight to
converge (step 6). Misclassifying is expensive: an empirical fork sent to opinion gets a
guess; a judgment call sent to a prototype burns a cycle proving something un-provable.

## The five classes

| Tag | Meaning | Routes to |
|---|---|---|
| **already-settled** | A prior ratified decision or a dogfood observation already answers it. | Step 6 — record the citation; do not re-open. |
| **empirically-determinable** | A prototype running the real scenario can confirm or kill the hypothesis. | Step 4 — empirical pass under the fidelity gate. |
| **judgment-call** | No single run decides it; it is a trade-off across values (simplicity, ergonomics, blast radius). | Step 5 — cross-brain adversarial opinion. |
| **owner-decision** | A preference or product call only the owner can make (naming, scope, priority). | Step 6 — present options + default; owner picks. |
| **deferred** | Real, but out of scope for this cycle. | Note as deferred in the spec; do not work it now. |

## Triage guard (do not let a fork slip past the gate)

Classification is autonomous — you tag forks yourself and surface the rationale; the human
gate is spec ratification at converge, not per-fork confirmation. But autonomy needs a guard
so a hard fork can't be quietly relabeled "easy" and skip the empirical/cross-brain gate:

- **An `already-settled` / briefing-decidable verdict MUST cite the decisive fact** — the
  specific prior ratified decision / lesson / query result that settles it. Record the citation
  next to the tag.
- **If no decisive fact is citable, the fork DEFAULTS to a hard route** (empirically-determinable
  or judgment-call), not to already-settled. "It feels obvious" is not a citation.
- **The step-5 cross-brain pass reviews the triage decisions themselves**, not only the
  hard forks: hand the different harness the full tag list + each "easy" call's cited fact and
  ask whether any briefing-decidable verdict is actually a disguised hard fork.

## How to tag

Ask, in order:

1. **Is it already answered?** A ratified decision in step 1, or a dogfood result, settles
   it → **already-settled** — but only if you can **cite the decisive fact** (else default
   hard, per the triage guard). (A `[PROPOSED]` is NOT settled — it still needs step 6.)
2. **Could a prototype falsify one option?** If there is a concrete scenario where the
   wrong choice would *fail an assertion* — divergent resolution, data loss, a race, a
   migration diff — it is **empirically-determinable**. The test: can you write a negative
   control (the broken impl that must fail)? If yes, it is empirical.
3. **Is it a trade-off with no single right answer?** Then it is a **judgment-call** →
   cross-brain. Don't fake-empiricize it with a prototype that can only confirm what you
   already chose.
4. **Is it pure preference / product scope?** → **owner-decision**.
5. **Real but not now?** → **deferred**.

## The empirical-where-possible-else-cross-brain rule

This is the owner's methodology and the backbone of the classification:

- **Prefer empirical.** If a fork *can* be decided by a faithful prototype, it must be —
  an audited experiment beats an opinion every time.
- **Else cross-brain.** Only forks that genuinely cannot be settled by a run get the
  adversarial-opinion route. "I couldn't be bothered to prototype" is not "un-provable."

## Output

The fork list from step 2, each fork now carrying its tag and a one-line justification
for the tag. This routes the work for the rest of the cycle.
