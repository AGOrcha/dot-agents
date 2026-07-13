# Step 3: Classify each fork — evidence-type triage

Every fork gets exactly one tag. The tag is its resolution route — it decides whether the
fork goes to the empirical pass (step 4), the cross-brain pass (step 5), or straight to
converge (step 6). Misclassifying is expensive: an empirical fork sent to opinion gets a
guess; a judgment call sent to a prototype burns a cycle proving something un-provable.

The triage is **domain-general**. The scientific-method spine is invariant across domains;
only the **form** the empirical evidence takes changes. So classification answers two
questions: (a) *which resolution route* the fork needs, and — for an empirical fork —
(b) *what evidence form* that route instantiates as in this domain.

## The classes (evidence-type triage)

| Tag | Meaning | Routes to |
|---|---|---|
| **already-settled / briefing-decidable** | A prior ratified decision, a dogfood observation, or a citable briefing fact already answers it. | Step 6 — record the citation; do not re-open. |
| **empirically-testable** | Faithful evidence — in the domain-appropriate form (see the evidence-form table) — can confirm or kill the hypothesis. | Step 4 — empirical pass under the fidelity gate (run the spine: pre-register → GATE 1 → gather → power/regime → GATE 2). |
| **judgment-call** | No single run decides it; it is a trade-off across values (simplicity, ergonomics, blast radius) with no empirical handle. | Step 5 — single cross-brain trade-off pass. |
| **owner-decision** | A preference or product call only the owner can make (naming, scope, priority). | Step 6 — present options + default; owner picks. |
| **unfalsifiable — needs reframe** | As stated, NO evidence could discriminate the options (the claim isn't falsifiable). | **Reframe** the fork until a discriminating test exists, or escalate to the owner. Do not run a spine that cannot fail. |
| **deferred** | Real, but out of scope for this cycle. | Note as deferred in the spec; do not work it now. |

## The evidence-form table (domain-general — the spine dispatches on this)

An `empirically-testable` fork still needs to know *what "evidence" means here*. The spine
is the same; the evidence form and its **negative control** change by domain:

| Fork type | "Empirical evidence" instantiates as | Negative control = |
|---|---|---|
| Algorithmic / software | throwaway prototype (own `go.mod`) + tests | the broken impl must fail the test |
| Agent-behavior | controlled eval across conditions | a condition engineered to fail must fail |
| Research / factual | corpus + KG evidence (`article-extract` → ingest → KG) | a disconfirming source actively sought, not just confirmers |
| Product / UX | user data, A/B, instrumented usage | the variant predicted worse must read worse |
| Ops / infra | a controlled measurement / load test | a known-bad config must trip the metric |
| Pure judgment (no empirical handle) | adversarial cross-brain + explicit trade-off analysis | argue the strongest case for the *rejected* option |

The last row is the `judgment-call` route (step 5). Every other row carries a **real negative
control** — the falsifying case that must actually fail. A fork whose negative control can't
be constructed is not `empirically-testable`; it is either a `judgment-call` or an
`unfalsifiable — needs reframe`.

## Triage guard (do not let a fork slip past the gate)

Classification is autonomous — you tag forks yourself and surface the rationale; the human
gate is spec ratification at converge, not per-fork confirmation. But autonomy needs a guard
so a hard fork can't be quietly relabeled "easy" and skip the empirical/cross-brain gate:

- **An `already-settled` / briefing-decidable verdict MUST cite the decisive fact** — the
  specific prior ratified decision / lesson / query result that settles it. Record the citation
  next to the tag.
- **If no decisive fact is citable, the fork DEFAULTS to a hard route** (empirically-testable
  or judgment-call), not to already-settled. "It feels obvious" is not a citation.
- **The step-5 cross-brain pass reviews the triage decisions themselves**, not only the
  hard forks: hand the different harness the full tag list + each "easy" call's cited fact and
  ask whether any briefing-decidable verdict is actually a disguised hard fork.

## How to tag

Ask, in order:

1. **Is it already answered?** A ratified decision in step 1, or a dogfood result, settles
   it → **already-settled / briefing-decidable** — but only if you can **cite the decisive
   fact** (else default hard, per the triage guard). (A `[PROPOSED]` is NOT settled — it
   still needs step 6.)
2. **Could faithful evidence falsify one option?** If there is a concrete scenario where the
   wrong choice would *fail* — a divergent resolution, data loss, a race, a migration diff, a
   condition that reads worse, a metric that trips — it is **empirically-testable**. The test:
   can you name the **negative control** (the falsifying case that must fail) in this domain's
   evidence form (see the evidence-form table)? If yes, it is empirical — route to the spine.
3. **Is it a trade-off with no single right answer and no empirical handle?** Then it is a
   **judgment-call** → cross-brain trade-off pass. Don't fake-empiricize it with a prototype
   that can only confirm what you already chose.
4. **Is it pure preference / product scope?** → **owner-decision**.
5. **Can NO evidence discriminate the options as stated?** Then the claim isn't falsifiable →
   **unfalsifiable — needs reframe**. Reframe the fork until a discriminating test exists (often
   by sharpening the hypothesis into a measurable prediction), or escalate to the owner. Never
   run a spine that cannot fail — a "test" no wrong answer fails is not evidence.
6. **Real but not now?** → **deferred**.

## The empirical-where-possible-else-cross-brain rule

This is the owner's methodology and the backbone of the classification — and it is
**domain-general**: "empirical" means faithful evidence in the fork's evidence form (a
prototype for software, a controlled eval for agent-behavior, a sought-out disconfirming
source for a factual claim), not a Go prototype specifically.

- **Prefer empirical.** If a fork *can* be decided by faithful evidence, it must be —
  an audited experiment beats an opinion every time.
- **Else cross-brain.** Only forks that genuinely have no empirical handle get the
  adversarial-opinion route. "I couldn't be bothered to gather evidence" is not "un-provable."

## Output

The fork list from step 2, each fork now carrying its tag and a one-line justification
for the tag. This routes the work for the rest of the cycle.
