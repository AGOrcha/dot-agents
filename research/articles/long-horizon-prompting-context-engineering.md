## Long-Horizon Prompting (Agent Skills for Context Engineering)

**Source**: https://github.com/muratcankoylan/Agent-Skills-for-Context-Engineering/tree/main/skills/long-horizon-prompting
**Author**: Agent Skills for Context Engineering Contributors (muratcankoylan)
**Date**: 2026-07-11
**Method**: WebFetch (GitHub raw)
**Word count**: ~2600

---

### Summary

A skill for writing the launch prompt of a long-running autonomous agent or parallel multi-agent
orchestration. Central technique: the **pseudo-formal task brief** — a spec written with the rigor
of formal verification but expressed linguistically, because most hard problems have no
machine-checkable success condition. Exemplar: the published GPT-5.6 Sol Ultra prompt behind a
64-subagent candidate proof of the Cycle Double Cover Conjecture. The controlling trade-off:
everything that makes a long run productive (persistence, autonomy, parallelism) also raises the
cost of a weak specification — a brief with a loophole burns hours producing an answer-shaped
artifact that does not solve the problem.

---

### Body

**Pseudo-formal task specification (4 components by leverage):** (1) definitions with degenerate
cases; (2) exact success predicate with scope quantifiers; (3) **non-counting outcomes** — the
highest-leverage component, an enumerated list of near misses that do not count (partial progress,
special-case solutions, reductions to another unproved statement, bounded/computational
verification, best-effort summaries); (4) enumerated failure modes for the auditor (a concrete
domain-specific hunt list, not a generic "check the work").

**Brief anatomy (block → job → failure prevented):** Definitions, Success predicate, Non-counting
outcomes, Solvability framing ("assume a solution exists"), Orchestration policy (heuristics not
fixed assignments), Verification policy (adversarial audit with enumerated failure modes),
Reporting contract (concrete artifacts; status reports rejected), Return condition (predicate over
the artifact, not confidence), Effort floor, Contamination guards.

**Persistence cuts both ways.** Persistence instructions counter give-up drift on long
trajectories, but the same pressure raises the reward-hacking surface: the most persistence-trained
frontier model measured also showed the highest detected cheating rate, and its time horizon was
not robust to whether cheating counted as success. **Design rule: never add a persistence
instruction without a matching verification gate.**

**The verification bottleneck.** Parallel sampling raises the chance some worker finds a correct
answer, but selection lags, and model judges of hard artifacts are systematically lenient
(reward rigorous-looking but incomplete arguments). Budget as much prompt design for the verifier
as the generator: give auditors the enumerated failure-mode list; require modular independently
checkable output; **use fresh-context adversarial verifiers rather than self-critique — a verifier
that did not build the artifact cannot rationalize its gaps**; treat inter-agent agreement as a
diversity-failure signal, not corroboration (committees converge tightest on the hardest problems,
where unanimity reflects shared bias).

**Structural diversity in parallel search.** Role labels do not create diversity; parallel workers
share priors and converge unless independence is engineered: keep early workers blind to the
favored approach; maintain an explicit registry of approach families grouped by idea (not surface
wording); mark a route blocked when it stalls at a goal-strength gap and reopen only for a
materially new mechanism; cross-pollinate late.

**Stop conditions / effort / progress state.** Effort floor ("spend at least X before returning"),
assume-solvable framing, and return-as-predicate-over-artifact belong in the brief. An externally
maintained ledger of verified progress re-injected each round belongs OUTSIDE the prompt (rescued
large-quantity tasks that prompt-only and completion-gated setups failed). Progress claims must
trace to a tool result/artifact from the current session (nearly eliminated fabricated status
reports).

**Lean and outcome-first.** Both major vendors converged: the prompt carries outcome, hard
constraints, evidence sources, and completion bar, and leaves the path to the model. Accumulated
instruction stacks measurably hurt; leaner system prompts improved vendor coding-agent evals while
cutting cost. Persistence is increasingly trained-in, not prompted-in.

**Harness vs prompt.** "Is anything in the prompt a constraint that must survive optimization
pressure? Move it to the harness; prompt-stated constraints are advisory." Enforce hard budgets and
permissions in the harness.

**Pre-launch evaluation rubric (any 'no' is a defect):** adversarial reader can unambiguously
decide if an artifact satisfies the predicate; every plausible near miss listed non-counting;
auditor has enumerated domain failure modes; every persistence instruction paired with a
verification gate; return condition is a predicate over the artifact; orchestration preserves early
independence + blocked-route bookkeeping; reporting is artifact-based; contamination guards stated;
optimization-pressure constraints live in the harness.

---

### Key Quotes

> "state the success condition so precisely that an adversarial reader cannot satisfy its letter without satisfying its intent."

> "never add a persistence instruction without a matching verification gate. Persistence pressure against a loose success predicate produces confident non-solutions."

> "Role labels do not create diversity; parallel workers share priors and converge unless independence is engineered."

> "Treat inter-agent agreement as a diversity failure signal, not as confirmation: committees converge most tightly on the hardest problems, where unanimity reflects shared bias rather than corroboration."

> "Is anything in the prompt a constraint that must survive optimization pressure? Move it to the harness; prompt-stated constraints are advisory."

---

### Extraction Notes

Tier-0 WebFetch of the raw SKILL.md (25.7 KB). The `references/` subdir (cdc-prompt-annotated.md,
vendor-guidance.md, research-evidence.md, task-brief-template.md) carries dated `claim-*`-backed
numbers; not extracted here (targeted depth). Numeric/vendor claims carry inline `claim-*` IDs
backed by the repo's `researcher/claims/index.jsonl` — treat as [UNVERIFIED] pending that file.
