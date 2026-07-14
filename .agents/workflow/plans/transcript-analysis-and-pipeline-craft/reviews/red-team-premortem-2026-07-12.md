# Red-team + pre-mortem — transcript-analysis-and-pipeline-craft (2026-07-12)

Owner-requested adversarial pass over the plan's concepts, evidence, and pending execution
(`pareto-live-waves` → `pareto-live-review` → `acceptance-closeout`). Two parts:

- **Part 1 — Red-team (executed refutations).** Every attack was *run*, not argued, per
  `methodology/falsification-review-rubric.md`: independent re-derivation scripts over raw logs,
  a 20-anchor code audit, git/mtime forensics, config/gate resolution probes, and a power
  simulation on the plan's own rows.
- **Part 2 — Pre-mortem (prospective hindsight).** Klein-style: "it is 6 months from now and
  this plan failed" — per failure: story, underlying assumption, early-warning signs
  (pre-sighting), likelihood × impact; then synthesis with a revised-plan delta and a
  pre-launch checklist. Methodology reference: openclaw-share `premortem-SKILL.md`
  (Klein/HBR; frame → raw failures → deep-dive → synthesis).

**Reviewer-family disclosure (RULE-7).** This pass ran on a claude-family reviewer. Most audited
artifacts were claude-family-authored, so **this document is NOT the blocking cross-family
adversarial gate** and does not discharge RULE-7 for any slice; it is an additional red-team
lens. The two re-derivation auditors wrote their own analysis code from scratch (no reuse of
the plan's or Gille's scripts) — code-level independence substitutes for family-level here,
gate-level independence does not.

Scripts and raw outputs: session scratchpad `audit_claim_{a,b,c}.py` (independent token
re-derivations); anchor-audit table reproduced in §1.1.

---

## Part 1 — Red-team: executed refutation attempts

Summary: **12 attacks; 5 survived, 3 refuted-the-work (bounded, correctable), 4 partial.**
The data layer and the gate machinery are solid. The wounds are concentrated in (a) the
external-reference digest's exact numbers, (b) the live-wave *statistical design*, and (c) two
pre-execution gaps (null contrast, disposable-task substrate).

### 1.1 RT-1 — "The craft doc's anchors don't support its claims" → SURVIVED

20-anchor stratified audit of `craft/full-loop-craft.md` against the working tree:
**16 EXACT, 4 DRIFTED (all substantively true), 0 WRONG.** Bonus: `bash
bin/tests/omp-full-loop_test.sh` verified hermetic (sandbox, fake `da`/`omp-swarm`, vendor-CLI
sentinels) and **PASSES** — the deterministic-skeleton claims (2 fanouts, 1 reconcile, no
conflicting dispatch, quiescence, crash/stale-lock/fanout-refusal recovery) are pinned by a
green test, not prose.

Drift detail (correction list): #13 `profile-driven.swarm.yaml:103-113` → actual 105-115;
#14 `target_count` line 6 → 8; #15 `reconcile.swarm.yaml:25-34` → 27-36 (all three: two
generated-header comment lines added after anchoring); #17 `bin/tests/omp-full-loop:241-247` →
269-273 (quiescent break; stale after `recover_incomplete_waves` was inserted above).
**Meta-finding:** three of four drifts are line anchors into *generated* YAML — the craft doc's
own anti-pattern #1 ("treat emitted YAML as a build artifact") applies to its anchors: anchor
generated files by stage name/key, never by line number.

### 1.2 RT-2 — "The CC token 'local verification' numbers won't reproduce" → PARTIAL (core survives, exact numbers wounded)

Independent from-scratch re-derivation over `~/.claude/projects/**/*.jsonl`
(vs `evidence/prior/external-references-2026-07-12.md` §1 / pareto erratum #2):

| claim | verdict | independent result |
|---|---|---|
| corpus "50 primary / 1,680 incl subagents / 35,515 usage entries" | **REFUTED — unreconstructable** | 36 primary + 1,134 subagent files, **93,493** usage entries; no on-disk slice matches; mtime-cutoff (<07-12) shifts metrics <0.5pp, so growth doesn't explain it — the original scan was partial |
| `input_tokens<=1` ≈ 12% | **REFUTED as stated** | input≤1 = **5.3%** full / **0.69%** primary-only. But **output**≤1 = **11.97%** — the claimed "12%" almost certainly measured the *output* field (field swap) |
| `output_tokens<=1` = 0% | **REFUTED** | 11.97% naive full corpus; 0.67% even primary-only naive |
| ~76% of requestIds duplicated | **REPRODUCED** | 74.2% full / 81.4% primary / band 71-81% across projects |
| dedup ratios input 3.01× / output 3.02× / cacheRead 2.38× / cacheCreate 3.04× | **REPRODUCED on the primary-files slice only** | primary-only: 2.98 / 3.02 / 2.49 / 3.01. Full corpus dilutes (output 1.76×) — subagent files behave differently |
| last-entry-wins dedup rule | **REPRODUCED + strengthened** | requestIds never span files (0 cross-file); within dup groups `output_tokens` is non-decreasing and last = max in **100% of 33,194 groups** — the rule is empirically well-defined |

**Consequence:** erratum #2's *direction* is confirmed (dedup mandatory, cache fields highest-trust)
but its quoted stats are slice-dependent and carry one field swap. The evidence-rubric clause
(fold-back `evidence-rubric-token-field-trust`) must pin the corpus definition — "primary session
files (no `subagents/` dirs; 0 `isSidechain`)" — and the digest's §1 numbers need a correction pass.

### 1.3 RT-3 — "OMP cache-read dominance is overstated" → SURVIVED (band nudged)

Independent recomputation over the four mega-session JSONLs: cacheRead token share
**96.63% / 97.71% / 95.48% / 96.08%** vs claimed "96-98%" — one session marginally below band.
Dollar share **57.9-69.0%** vs claimed "62-69%" — lower bound widens ~4pp. T-c1/H2 stand;
quote the bands as **95-98% tokens, 58-69% $**. Bonus context: the four sessions cost
**$40.75 / $491.84 / $94.51 / $439.46** (≈ $1,067 total) — load-bearing for the live-wave
budget discussion (§2, F1).

### 1.4 RT-4 — "The regenerated rows no longer match the hypotheses' cited numbers" → SURVIVED

My own field extraction over regenerated `rows.jsonl` (120 codex census rows): cached_input/input
median **88.8%**, model_generated/total median **0.93%** max **4.05%**, (uncached+generated)/total
median **12.3%** — all three reproduce the `historical-hypotheses.md` citations exactly. The
2026-07-12 "bounded regeneration" preserved the data relationships it claimed to preserve.

### 1.5 RT-5 — "The codex census itself was measured right" → PARTIAL (H1's max is understated)

Independent recomputation from raw `~/.codex/sessions` (538 usable of 597 files): cached median
**87.26%** (claim 88.8% — close), output/total median **0.94%** (claim 0.93% — exact), but
output/total **max = 6.44% even excluding post-claim sessions** (claim ~4.05%); a cluster of
2026-06-26 sessions at 5.6-6.4% predates the claim. **H1's "≤ ~4%" volume-invariance bound rests
on an understated max** — the honest prior is "median ~1%, tail to ~6.5%," and H1's threshold
should be restated (median-based, or ≤ ~7%). Note the census divergence (their 120-row sampled
rows vs my 538-session sweep) also means the rows are a *sample*, not the census the prose
implies.

### 1.6 RT-6 — "RULE-7's opposite-pinning is config fiction" → SURVIVED (strongest section audited)

Empirically verified end-to-end: both lenses registered (`cross-harness-adversarial` =
gpt-5.4/gpt; `cross-harness-adversarial-claude` = claude-opus-4-8/claude); `da --json workflow
resolve-prompt --kind reviewer --slug cross-harness-adversarial-claude` resolves matched=true
with the right model/family; the Go gate (`pipeline_projection.go` — "cross-family lens family
%q must differ from executor family %q") and the CC-runner JS gate (named-slug binding +
family-inequality throw) both exist as cited and hard-refuse same-family. The lens-map's
per-contrast legality table is consistent with the config.

### 1.7 RT-7 — "The live-wave design is underpowered at n≥3" → REFUTED-THE-DESIGN (empirical)

From the plan's own rows: within-cell total-token dispersion for the most-repeatable codex task
classes — `experiment/cache-hot` n=13 IQR/median **64%**, `experiment-gate/cache-hot` n=13
**78%**, `codex-session/cache-hot` n=50 **218%**. Monte-Carlo over those cells: an **n=3 median
carries a ~90% sampling interval of ±~45% of the cell median**. H1's decision threshold is
**4%**; H6's is "saving within CI of zero." Natural trajectory variance is roughly an order of
magnitude above the effect sizes being tested.

Honest caveats: (a) the experiment cells include deliberate parameter variation, so 64-78% is an
*upper bound* on pure repeat variance; (b) the paired design (identical snapshots, delta per
pair) removes between-task variance. But **pure same-config repeat variance has never been
measured** — no cell in the corpus holds same-config repeats, and **no A/A null contrast exists
anywhere in the preregistration** (grep-verified). Unless pairing shrinks variance by ~10×
(unknown), every contrast returns "noise, not signal" by the rubric's own rule, and the stopping
rule (CI excludes dominance reversal) never triggers. This is the single most consequential
design gap. Fix is cheap: see C0 in §2.

### 1.8 RT-8 — "Preregistration ordering is not actually auditable" → REFUTED-THE-CLAIM (integrity intact, mechanism broken)

`preregistration.md` claims its order of operations is "auditable by file mtime:
preregistration.md → rows.jsonl → cells-manifest.json → historical-hypotheses.md." Forensics:
the initial emission landed **in one commit** (c049747d), so git cannot witness the freeze; and
after the bounded regeneration (24c8bdf1), current mtimes read prereg 03:12 → cells 05:24 →
**hypotheses 12:07 → rows 12:16** — rows now *post-date* the hypotheses, inverting the claimed
order. Data integrity is separately fine (RT-4; the 4-audit validation record), but the stated
audit mechanism is broken as written. Fix: an ordering addendum in the prereg (regeneration
history + commit hashes as the witness, not mtimes).

### 1.9 RT-9 — "H6 measures accuracy with the instrument it is treating" → SURVIVED with caveat

The circularity guard exists: H6's refutation clause references the cheap reviewer's verdict
divergence **against the cross-family gate**, an instrument outside the treatment. Residual:
that reference judge is the family-flipped gate, and rubric limitation #9 already concedes
gate strictness may differ by family — so C6's accuracy reference inherits the same confound
the rubric flags for C3/C4. Verdict-distribution reporting (rubric #9) is the mitigation;
it must actually be reported per contrast.

### 1.10 RT-10 — "The live protocol has nothing to run on" → REFUTED-THE-READINESS

"Disposable tasks" appear in the task title and rubric text and **nowhere else**: no definition,
no criteria (write-scope-safe? representative cache regime? task classes matching the cells?),
no list, no generator. Given H4's own finding (savings scale with task length / cache regime),
*which* tasks are chosen determines the answer — small synthetic tasks would sit in the regime
where the fixed tax dominates and cache-hot priors don't apply. The substrate is a blocking
pre-execution artifact that doesn't exist.

### 1.11 RT-11 — "The accuracy instrument is unproven in production" → PARTIAL

Rubric 3.0.0 exists in code (`internal/scoring/rubric.go` RubricVersion="3.0.0", tests + golden
data; r1-5 plan tasks completed) — but **all 66 real score sidecars ever emitted are 2.1.0**
(incl. the newest, iter-64..66). The live waves would be 3.0.0's first production use, *inside*
the experiment whose accuracy axis depends on it. Not a refutation; an instrument-burn-in risk
(and the corpus's own OH-E3 lesson — a scorer confidently rating unverifiable work `high` — is
exactly the class of bug burn-in catches).

### 1.12 RT-12 — "Synthesis routing assumes a queue that drains" → REFUTED-THE-ASSUMPTION (empirical)

The plan's constraint acknowledges "~10 unrouted fold-backs" as queue debt and commits synthesis
output INTO the proposal queue. Current counts: **44** fold-back artifacts in
`.agents/active/fold-back/`, **68** files in project `.agents/proposals/`, **22** in
`~/.agents/proposals/`, plus 7 active delegation YAMLs. O9 (the transformer proposal) and the
five 2026-07-12 fold-backs route into this queue. The routing *mechanism* works (three of five
fold-backs landed as task-note consumers — those get read); the *proposal-queue* leg has no
empirical drain record. Anything routed as a proposal should be treated as "parked" until a
drain process exists.

Additional small findings: H4's "67% fixed tax" prior anchors on a **single copilot session**
(n=1; codex token-gap sessions are the secondary anchor) — fine as a prior, must not survive
into any conclusion without live measurement. The evidence-validation record itself notes all
four audit agents ran claude-family because the GPT reviewer spawns hit `usage_limit_reached` —
T-b1 has already disrupted this plan's own review machinery once.

---

## Part 2 — Pre-mortem (prospective hindsight)

**Frame: it is 2027-01-12. The plan is archived. The live waves either never ran or produced
nothing anyone routes on; the craft doc is drifting. What went wrong?**

Context bar (per the skill's minimum): *What is it* — a rubric-gated evidence pipeline over
local agent transcripts plus a craft extraction (pipeline-architect skill, config→pipeline
transformer, 4-axis Pareto harness) with live A/B waves pending. *Who is affected* — the owner
and every dot-agents-managed project consuming stage_profiles routing decisions; the wave
budget. *Success* — acceptance-closeout green (both projections from one IR, idempotent,
drift-detected) and live waves yielding CI-backed routing decisions or honest, cheap nulls.

### F1 — The cost-power squeeze (waves ran, answered nothing)

**Story.** Wave 1 launched with 3 repeats per cell as preregistered. Every paired delta came
back with a CI spanning ±30-50% against thresholds of 4% (H1) and "CI of zero" (H6); by the
rubric's own rule, all noise. Doubling repeats doubled spend — the mega-sessions had already
shown what full-loop tasks cost ($40-490/session; ≈$1,067 for four) — and the required n for a
4% threshold at ~40% repeat CV was in the hundreds per arm. The user, correctly, declined to
fund wave 3. The plan closed with "inconclusive" on all six contrasts, and the routing decisions
were made on vibes anyway — the exact outcome the plan existed to prevent.
**Underlying assumption.** Pairing on identical snapshots makes agent trajectories similar
enough that n=3 medians resolve single-digit-percent effects.
**Early warnings (pre-sighting).** (1) C0/A-vs-A pilot delta CI > half the smallest hypothesis
threshold. (2) Wave-1 spend > 2× its pre-registered estimate. (3) First contrast's bootstrap CI
width > 10× its threshold.
**Likelihood: HIGH · Impact: HIGH** (empirically grounded: RT-7 dispersion + RT-3 session costs).

### F2 — The disposable-task substrate was improvised (external validity collapse)

**Story.** With waves gated open and no task substrate defined, someone generated a handful of
small synthetic fixes to run the contrasts on. They were short, cache-cold, and low-output —
precisely the regime where H4 says the fixed tax dominates and where the cache-hot mega-session
priors (H1/H2) don't transfer. The frontier measured a task class that doesn't exist in
production. The numbers were CI-clean and wrong.
**Underlying assumption.** "Disposable" and "representative" can be satisfied simultaneously
without designing for it.
**Early warnings.** (1) The substrate artifact still absent at wave-launch review. (2) Chosen
tasks' cache_regime/generated-output profile falls outside the historical cells they're meant
to inform. (3) Task durations an order of magnitude below the workflow-session median.
**Likelihood: HIGH · Impact: HIGH** (RT-10: the artifact does not exist).

### F3 — Gate-strictness asymmetry ate the cross-family contrasts

**Story.** C3/C4 ran with the claude gate over gpt executors; C1/C2 with the gpt gate over
claude executors. Block rates differed by family (they always did — the corpus's own review
cells showed family-distinct verdict styles), inducing different re-work rates. Per rubric
limitation #9's own invalidity rule, no frontier claim could separate executor effect from gate
effect. Half the contrast grid produced un-claimable results — by design, discovered late.
**Underlying assumption.** The gate-strictness delta would be small enough to ignore at the
observed block rates.
**Early warnings.** (1) Wave-1 verdict distributions (mandatory per rubric #9) differ by >2×
between family directions. (2) Any contrast where gate-induced re-work iterations exceed
first-pass iterations.
**Likelihood: MEDIUM · Impact: HIGH** (the rubric names the confound but has no quantitative
tolerance for it).

### F4 — The accuracy axis was noise (instrument burn-in inside the experiment)

**Story.** Rubric 3.0.0 scored its first-ever production runs during wave 1. A weighting quirk
(the kind iter-history found in 2.x, and the kind OH-E3 proved scorers ship with) systematically
favored short completions. Nobody noticed until pareto-live-review, because the scorer was
validated by golden tests, not by shadow-scoring real historical iterations. The accuracy axis
of every cell was quietly biased; the "cheap tier is accuracy-safe" conclusion (H5/H6) was an
artifact.
**Underlying assumption.** A unit-tested scorer is a calibrated scorer.
**Early warnings.** (1) 3.0.0 shadow-scores on iters 60-66 disagree with 2.1.0 bands beyond
re-normalization. (2) Live accuracy scores cluster (ceiling/floor) or anti-correlate with
verifier pass ratio.
**Likelihood: MEDIUM · Impact: HIGH** (RT-11: zero production emissions to date).

### F5 — Rate-limit walls shredded wave integrity

**Story.** Mid-wave, the gpt provider hit `usage_limit_reached` — as it already did once during
this plan's own evidence validation, killing all four reviewer spawns. Paired cells lost one
arm; "snapshot-identical" pairs re-run hours later were no longer cache-identical; partial cells
got quietly pooled. T-b1 was in the corpus as a *finding* and still wasn't in the wave protocol
as a *design input*.
**Underlying assumption.** Resumability rules for the loop (craft §3) transfer automatically to
experiment validity (a resumed run is NOT the same measurement).
**Early warnings.** (1) Any usage-limit event in wave 1. (2) Any pair whose two arms ran >N
hours apart or across a provider incident.
**Likelihood: MEDIUM-HIGH · Impact: MEDIUM** (invalidates cells, not the design; empirically
precedented twice).

### F6 — Completed-with-doc-drift, the sequel (outputs rotted in the queue)

**Story.** The plan archived clean: craft doc written, skill shipped, proposals filed. Six
months later the proposal queue held 90+ items; O9 and the CLI fold-backs were never reviewed;
the craft doc's anchors drifted further with each regeneration (4/20 within the first two days);
stage_profiles evolved past the doc. The plan's own priors predicted this failure class (DA-C2:
"status said done" isn't auditable) — for other plans.
**Underlying assumption.** Routing INTO the queue is equivalent to being consumed.
**Early warnings.** (1) Proposal count still rising 30 days post-archive. (2) First
post-regeneration anchor audit shows >25% drift. (3) `da review` sessions per month = 0.
**Likelihood: HIGH · Impact: MEDIUM** (RT-12: 44/68/22 queue items now; task-note routing
works, proposal routing has no drain record).

### F7 — Single-operator ceiling (transfer claim quietly false)

**Story.** The craft doc's verification discipline findings (T-d1: 0/38 codex sessions skipped
both git and tests) were marketed as transferable methodology. On the second operator's machine
— no CLAUDE.md verification mandate, different habits — the discipline evaporated, and rules
anchored to "the transcripts prove this happens" stopped being true. The doc was correct about
this machine and silent about that scope.
**Underlying assumption.** Operator-shaped behavioral regularities are properties of the
harness/pipeline rather than of the operator's rule set.
**Early warnings.** (1) First cross-operator corpus shows discipline metrics dropping by >2×.
(2) Craft rules cited in other repos without their evidence scope.
**Likelihood: MEDIUM · Impact: MEDIUM** (mitigated by the negative-control analysis's
workflow-scoping, which correctly gates *mechanism* findings; the *discipline* findings carry
no such gate).

### F8 — Reference-contamination compounding (the digest's wrong numbers propagate)

**Story.** The erratum-#2 rubric clause shipped with the digest's numbers as-written. The
"input≤1 = 12%" field swap and the primary-only ratios applied to full-corpus data mis-excluded
rows in the normalization; a later audit found the token axes subtly wrong and every wave-1 cell
had to be re-derived. The correction existed — in a red-team review nobody re-read.
**Underlying assumption.** Externally-sourced numbers verified once ("local verification") stay
pinned to their slice definitions as they move between documents.
**Early warnings.** (1) Rubric clause lands without a corpus-definition sentence. (2) Any
document quoting the 12%/0%/2.4× figures without "primary-files-only" qualification.
**Likelihood: MEDIUM · Impact: MEDIUM** (RT-2 found it pre-execution; the fix is one correction
pass).

### Synthesis

- **Most likely failure:** F1 — the cost-power squeeze. Adequately-powered waves are
  unaffordable; affordable waves are underpowered; the design currently has no way to know
  which side it's on because repeat variance has never been measured.
- **Most dangerous failure:** F2 — a CI-clean frontier measured on an unrepresentative task
  substrate. F1 wastes money and returns honest nulls; F2 returns *confident wrong routing
  decisions* that then propagate through stage_profiles to every project.
- **Hidden assumption (the one nobody wrote down):** *paired snapshots pin trajectories.* The
  entire live protocol's power budget rides on within-pair correlation being high, and no
  artifact in the plan measures, estimates, or even names it.
- **Single key revision:** add **C0 — the A/A null contrast** (baseline vs baseline, identical
  snapshot, n≥5, run FIRST on the cheapest representative disposable task). It measures repeat
  variance directly, prices a wave empirically, validates scorer 3.0.0 in production, exercises
  resumability, and gates every other contrast: if C0's delta CI exceeds half the smallest
  hypothesis threshold, redesign (bigger thresholds, productive-tokens-only volume axis, or
  more repeats) *before* spending on C1-C6.

### Pre-launch checklist (each item maps to failure modes)

1. **[F1,F4,F5] C0 A/A null contrast preregistered and run first**; publish measured repeat CV
   and an empirically-derived per-wave $ estimate before the user gate re-opens.
2. **[F2] Disposable-task substrate artifact** (`evidence/pareto/disposable-tasks.md`):
   selection criteria (cache regime, generated-output fraction, duration matched to target
   cells; write-scope-safe; re-runnable from frozen snapshots) + the concrete task list.
3. **[F8,F1] Digest + erratum correction pass**: pin erratum-#2 stats to the primary-files
   slice, fix the input/output placeholder swap, drop "output≤1=0%", restate H1's max as ~6.5%
   and re-derive its threshold; widen T-c1 bands to 95-98% tokens / 58-69% $.
4. **[F3] Quantitative gate-strictness tolerance**: pre-commit the block-rate delta beyond
   which C3/C4 results are declared unclaimable (don't leave rubric #9 qualitative).
5. **[F4] Shadow-score iters ~55-66 with rubric 3.0.0** and reconcile against 2.1.0 bands
   before any live accuracy number is consumed.
6. **[F6] Anchor hygiene + drain check**: re-anchor the 4 drifted craft anchors; switch
   generated-YAML anchors to key-based; schedule one `da review` drain session for the two
   proposals before plan archive.
7. **[F8] Prereg ordering addendum**: record the regeneration history with commit hashes as
   the ordering witness (mtimes are dead as an audit mechanism).

---

## Review block (falsification-review-rubric schema)

```yaml
review:
  target: transcript-analysis-and-pipeline-craft (plan concepts + evidence + pending live-wave design)
  reviewer_model_family: claude   # NOT the blocking cross-family gate; red-team complement only
  hypotheses:
    - statement: craft-doc anchors do not support their claims
      test: 20-anchor stratified code audit + hermetic driver test run
      outcome: survived
      evidence: ["16 EXACT / 4 DRIFTED / 0 WRONG", "omp-full-loop_test.sh PASS"]
    - statement: CC token local-verification numbers do not reproduce
      test: independent from-scratch rederivation over ~/.claude/projects (93,493 usage entries)
      outcome: refuted-the-work   # bounded: slice/field-swap errors; core direction reproduced
      evidence: ["input<=1 5.3% not 12% (12%≈output field)", "output<=1 11.97% not 0%", "ratios reproduce on primary-only slice", "last=max in 100% of 33,194 dup groups"]
    - statement: OMP cacheRead dominance overstated
      test: independent recomputation over 4 mega-session JSONLs
      outcome: survived
      evidence: ["95.48-97.71% tokens", "57.9-69.0% dollars", "session costs $40.75-$491.84"]
    - statement: regenerated rows diverge from hypotheses' cited medians
      test: independent field extraction over rows.jsonl
      outcome: survived
      evidence: ["88.8% / 0.93% / 4.05% / 12.3% all reproduce exactly"]
    - statement: codex census misestimated
      test: independent sweep of 538/597 raw codex session files
      outcome: refuted-the-work   # bounded: H1 max understated (6.44% pre-claim vs 4.05%)
      evidence: ["medians reproduce (87.26%/0.94%)", "max 6.44% excl. post-claim sessions"]
    - statement: RULE-7 opposite-pinning is config fiction
      test: .agentsrc.json probe + resolve-prompt + Go/JS gate code read
      outcome: survived
      evidence: ["both lenses resolve with correct families", "hard-refusal in pipeline_projection.go + cc_pipeline.go"]
    - statement: live-wave design is underpowered at n>=3
      test: within-cell dispersion + n=3 Monte-Carlo over the plan's own rows
      outcome: refuted-the-work
      evidence: ["IQR/median 64-218%", "n=3 median 90% interval ±~45% vs 4% threshold", "no A/A contrast preregistered (grep-verified)"]
    - statement: preregistration ordering not auditable as claimed
      test: git log --follow + mtime forensics
      outcome: refuted-the-work   # mechanism only; data integrity separately verified
      evidence: ["initial emission single-commit", "post-regeneration rows mtime 12:16 > hypotheses 12:07"]
    - statement: H6 accuracy reference is circular
      test: refutation-clause read + lens-map cross-check
      outcome: survived           # caveat: reference judge inherits gate-strictness confound
      evidence: ["verdict divergence referenced against cross-family gate, outside treatment"]
    - statement: disposable-task substrate exists
      test: repo-wide grep + plan artifact inventory
      outcome: refuted-the-work
      evidence: ["only occurrences: task title + rubric prose; no criteria/list/generator"]
    - statement: live accuracy instrument production-proven
      test: rubric.go read + r1-5 TASKS status + score-sidecar version census
      outcome: inconclusive       # exists+tested in code; zero production emissions (66/66 sidecars 2.1.0)
      evidence: ["internal/scoring/rubric.go RubricVersion=3.0.0", "iter-64..66 all 2.1.0"]
    - statement: synthesis-to-queue routing drains
      test: live queue census
      outcome: refuted-the-work   # for the proposal leg; task-note leg works
      evidence: ["44 fold-backs, 68 project + 22 global proposals currently parked"]
  unrun:
    - hypothesis: pure same-config repeat variance is small enough for n=3 pairing
      reason: requires the C0 A/A run this review recommends; cannot be settled from historical data
    - hypothesis: gate strictness differs materially by family direction
      reason: requires wave-1 verdict distributions; flagged as mandatory reporting, tolerance TBD
    - hypothesis: craft rules transfer to a second operator
      reason: no second-operator corpus exists; scoped as F7
  verdict: reject   # for the pareto-live-waves design AS-PREREGISTERED (C0 + substrate + corrections required);
                    # the evidence/data layer, gate machinery, and craft-doc discipline SURVIVE
```

**Verdict, plainly:** the plan's *evidence discipline is real* — it survived independent
re-derivation at every layer where it controlled its own data. What does not survive is the
live-wave design's readiness: it currently cannot detect the effects it preregistered, on tasks
it hasn't defined, with an accuracy instrument it has never fired, and its two sharpest external
numbers carry slice errors. All five blockers are cheap relative to one wave's cost. Fix, then gate.

---

## Disposition (same day, 2026-07-12 — owner-directed)

Pre-launch checklist items resolved and their fold-backs archived
(initially staged in `.agents/active/fold-back/resolved/`; since migrated to the canonical
`.agents/history/transcript-analysis-and-pipeline-craft/fold-backs/pareto-live-waves/`), so
the waves start on settled decisions:

1. **C0 A/A null contrast** — registered as gating rubric step 4a + lens-map first row
   (design; the run itself is wave 0). ✔
2. **Disposable-task substrate** — `evidence/pareto/disposable-tasks.md` authored (Tier-A
   historical replays / Tier-B calibration-only, empirical cell-matching via C0). ✔
3. **Digest + erratum corrections** — digest §1 corrected in place (+ devforth mitmproxy
   corroboration, `research/articles/devforth-cc-usage-overestimates-output-tokens.md`);
   `historical-hypotheses.md` Erratum 3 (H1 → ±7%, T-c1/H2 bands 95-98% / 58-69%); evidence
   rubric gains clause E6 (per-harness token-field trust, slice pinning). ✔
4. **Gate-strictness tolerance** — NOT yet quantified; carried by the still-active fold-back
   `review-gate-deterministic-pre-llm-tier` on `pareto-live-review` (inherently post-wave-data)
   plus the rubric's mandatory verdict-distribution reporting. ◐
5. **Scorer 3.0.0 shadow burn-in** — EXECUTED: iters 55-66, 11/12 within ±0.004, one
   explainable band change; PASSED (`evidence/pareto/scorer-3.0.0-shadow-burn-in.md`). ✔
6. **Anchor hygiene** — the four drifted craft-doc anchors re-anchored (#13→105-115, #14→8,
   #15→27-36, #17→269-273/277-282); the `da review` drain session for the two parked proposals
   remains an owner action. ◐
7. **Prereg ordering addendum** — added to `preregistration.md` §0 (commit chain replaces
   mtimes as the ordering witness). ✔
