# Historical pass — per-cell summaries & candidate routing hypotheses

Hypothesis-generation ONLY (`pareto-measurement-rubric.md:42-46`). The historical corpus is
observational and confounded (task mix, cache state, retries, prompt drift co-vary with model), so
**nothing below is a dominance, frontier, or accuracy conclusion.** Each cell summary describes the
*shape* of what was recorded; each hypothesis is a falsifiable prior that a live paired contrast
(rubric step 4) settles. Rows cited by `row_id` in `rows.jsonl`; prior findings by their index ID.

Anchoring corpus facts (all from `rows.jsonl` / `items/*`):
- **Cache-read dominates $ and volume.** OMP: cacheRead = **96-98%** of tokens and **62-69%** of $
  across the 4 large sessions (`omp:019f3cf2/019f3f23/019f4eda/019f4eea`; items `omp-cost-totals-*`,
  `cost-cacheread-dominates-context`). Codex: median cached **88%** (`codex:*`, item block
  `cx-cost-permodel-*`).
- **Productive work is a tiny token slice.** Codex productive (output+reasoning) median **12,070
  tok = 1.26%** of total (max 5.14%); OMP productive ≈ **2-4%** of tokens. >95% of every session's
  token/$ footprint is re-sent cached context, not model work.
- **Fixed per-request tax is large on short tasks.** Copilot trivial query: tool defs **16,290 /
  24,342 ctx tokens = 67%** (`copilot:23e45a55`; item `copilot-tooldef-fixed-overhead`).
- **Wall-clock is tool/turn-heavy.** OMP summed model time **1.5h** (019f3cf2) / **13.2h**
  (019f3f23) over multi-day spans, against **278-826 bash + 187-322 read** tool calls per session
  (items `omp-turn-duration-shape-*`, OMP tool-call table). Stage decomposition is `[INFERENCE]`.

---

## 1. Per-cell summaries (shape only)

20 populated cells (`cells-manifest.json`). Grouped by which axes each can feed:

### openai-codex cells (census; 120 rows, cache_regime derivable, NO $/accuracy axis)
- **`openai-codex / codex-session / cache-hot / unknown` (n=51)** and **`… / cache-warm /…`
  (n=14):** the bulk of the codex census. Feeds **token-volume + cache-regime** priors only
  (median cached 88%; productive 1.26% of total). No $ (codex has no cost field —
  `no_dollar_cost_axis_codex`), no rubric-matched accuracy. Retry unknown.
- **`openai-codex / experiment{,-gate} / cache-{hot,warm}` (n=13+13+12+8=46):** depth-exp &
  scratchpad-gate trials — short, high-cache, structurally unlike the workflow loop; volume/cache
  only.
- **`openai-codex / review / cache-{warm,cold}` (n=5+3=8):** `codex-auto-review` turns — very low
  productive tokens (220-720), lower cached% (46-63%). This is the historical analogue of a cheap
  **review-stage** cell (feeds H6).

### mixed / cursor-routed cells (OMP; $ recorded, model blended)
- **`mixed / orchestration-impl / cache-hot / unknown` (n=3):** OMP `019f3cf2`, `019f3f23`,
  `019f4eea` — the only **recorded-$** workflow rows; cacheRead 62-69% of $. Model-blended
  (`model_change` boundaries present) → not model-attributable except via the 019f3cf2 partition.
  Feeds the cost-decomposition prior (H2) as an *observation of the $ shape*, not a routing verdict.
- **`anthropic / orchestration-impl / cache-hot` (019f4eda)** and **`mixed / failure` (019f3cbc):**
  one cutoff anthropic-family session and the zero-output usage-limit failure control.
- **`cursor-routed / orchestration-impl / unknown` (n=1)** + the **`anthropic`/`openai-codex`
  019f3cf2 partitions:** the sole rows isolating one family's $ inside a session —
  gpt-5.4 **$22.03** vs claude-opus-4-8 **$18.72** vs cursor-routed **$0.00** (item
  `omp-cost-cursor-routed-zero-dollar`). Confounded (different turns/tasks per model) → observation
  only.

### unknown-model cells (iter-log; accuracy + retry axes, NO model/token/$)
- **`unknown / impl-slice / unknown / no-retry` (n=62)** + **`… / retried` (n=4:** iters 3,17,52,53):
  the iter-log implementation corpus. Feeds the **accuracy proxy** (66 scored, all `rubric 2.1.0`,
  `accuracy_live_comparable:false`) and **retry-regime** shape only. 65/69 iterations ran clean
  (retries 0). Model unattributed → excluded from model_family cells.
- **`anthropic / impl-slice / cache-hot / no-retry` (n=2:** iters 63,64, `claude-opus-4-7`): the
  only iter rows with a model AND `session_tokens` (cache_hit_rate 0.992). The lone historical point
  joining anthropic-family + impl + measured cache — still 2.1.0-scored, still not live-comparable.

**Read of the cell map:** the historical corpus can price the **cache/volume** axes richly
(openai-codex + OMP), the **$** axis only for blended OMP sessions, and the **accuracy** axis only
on a stale rubric with no model attribution. No cell holds all four axes for a single model family
— which is exactly why the frontier is a **live** artifact and these are priors, not conclusions.

---

## 2. Candidate routing hypotheses (falsifiable, effect-size priors, 1:1 live contrasts)

Live contrasts (rubric step 4 — identical disposable-task snapshots, swap ONE stage's model,
≥3 repeats, per-cell medians + bootstrap CIs; a move smaller than its CI is noise):

| id | swap | stage |
|---|---|---|
| **C1** | `claude-opus-4-8` → `claude-sonnet-5` (PRIMARY, anthropic) | executor |
| **C2** | `claude-opus-4-8` → `claude-haiku-4-5` (SECONDARY, anthropic) | executor |
| **C3** | `claude-opus-4-8` → `gpt-5.6-terra` (PRIMARY, cross-family) | executor |
| **C4** | `claude-opus-4-8` → `gpt-5.6-sol` (SECONDARY, cross-family) | executor |
| **C5** | PRIMARY (`sonnet-5`/`terra`) vs SECONDARY (`haiku-4-5`/`sol`), stratified by productive-fraction | executor |
| **C6** | reviewer/verifier stage → cheap tier (`haiku-4-5` or `gpt-5.6-sol`), executor held at baseline; cross-family gate (RULE 7) | review |

### H1 — token-volume is near-invariant under an executor swap → **C1**
**Prior (effect size):** at a fixed snapshot (same bundle, same tool outputs) an executor model swap
moves **token volume by ≤ ~4%**, because the productive fraction is ≤4% (codex median 1.26%; OMP
2-4%) and the cache-read volume (96-98%) is set by context size, not model. Source: `omp-cost-totals-*`,
codex census, `cost-cacheread-dominates-context`; extends `OH-A2` (context-cost proxy) onto the
volume axis.
**Refuted if:** the C1 paired token-volume delta has a CI whose magnitude exceeds 4% — i.e. the
model choice, not the pipeline, is moving volume. **To move volume you must change the pipeline
(compaction / fewer re-reads), not the model.**

### H2 — achievable $ savings are cache-read-rate-bound, not output-bound → **C2**
**Prior (effect size):** cache-read is **62-69%** of session $ (OMP) and its *volume* is
model-independent. So the **outcome-addressable $ swing** (input+output+cacheWrite) is **≤ ~38%** of
total $; the remaining ≥62% re-prices purely by the swapped tier's **cache-read rate ratio**. A
cheap-tier swap's $ reduction ≈ `0.38·(1−r_prod) + 0.62·(1−r_cacheread)` where `r_*` are the cheap
tier's price ratios vs baseline. Source: `omp-cost-totals-019f3cf2/019f3f23/019f4eda/019f4eea`.
**Refuted if:** the C2 $ reduction **exceeds** the cache-read-rate-ratio prediction (would prove the
swap also shrank context/tool volume — a *pipeline* effect misattributed to the model), **or** falls
to ≈0 when the cheap tier's cache-read rate ≈ baseline (confirming cache-read pricing, not model
work, gates savings).

### H3 — a faster model moves wall-clock sublinearly in tool-heavy stages → **C3**
**Prior (effect size):** in tool-heavy task classes (OMP: 278-826 bash + 187-322 read calls/session;
per-turn model latency median 10-24s, summed model time 1.5-13.2h across multi-day spans), tool
execution + queue/wait sit on the critical path, so wall-clock reduction from a faster executor is
**< its per-turn latency ratio** and **model-latency share of critical path is expected minority
(<50%)**. Source: `omp-turn-duration-shape-*`, OMP tool-call table; addresses `OH-A1` (efficiency
untested for lack of timing). Historical stage decomposition is `[INFERENCE]` — the live wave sets
the share.
**Refuted if:** C3's wall-clock reduction ≈ the model's full per-turn latency ratio (model latency
≈100% of critical path) on a tool-heavy task class.

### H4 — cheap-tier $ savings are smallest on short tasks (fixed-tax dominated) → **C4**
**Prior (effect size):** fixed per-request context (tool defs + system prompt) is **~67%** of a
trivial task's context (copilot) and is model-priced but volume-fixed; codex's 14 token-gap sessions
are the short-task analogue. So a cheap swap saves only the price ratio on that fixed block, and
productive savings are negligible → **cheap-tier fractional $ savings rise with task length**;
shortest tasks yield the least. Source: `copilot-tooldef-fixed-overhead`, `cx-cost-tokengap-*`.
**Refuted if:** C4's cheap-tier fractional $ savings on short disposable tasks ≥ on long/
context-heavy ones (fixed tax would then be scaling with model quality — it does not).

### H5 — cheap-tier accuracy risk is localized to the productive fraction → **C5**
**Prior (effect size):** the model's actual work (output+reasoning) is **≤~4%** of tokens (codex
median 1.26% = 12,070 tok); the other ≥95% is quality-neutral context re-transmission. So a weaker
model degrades accuracy **only** in proportion to a task's productive fraction: expect PRIMARY
(`sonnet-5`/`terra`) accuracy within its own CI of baseline on most task classes, and SECONDARY
(`haiku-4-5`/`sol`) accuracy drop to **grow with productive fraction**, staying ≈baseline on
low-productive (context-shuffling/review) work. Source: codex productive-fraction census, `OH-A3`
(cheap-tier-on-frontier, untested). **Historical accuracy cannot test this** (2.1.0 only, no model
attribution) — C5 is the first identification.
**Refuted if:** SECONDARY matches baseline accuracy (CI-overlapping) on high-productive task classes,
**or** PRIMARY underperforms SECONDARY, **or** the accuracy drop is uniform across productive-fraction
strata (would break the localization mechanism).

### H6 — the review/verifier stage is the highest-leverage cheap-routing target → **C6**
**Prior (effect size):** historical review turns (`codex-auto-review`, cell `…/review/…`) carry
**very low productive tokens (220-720)** and high fixed/cached context — a classification, not a
generation, workload. So routing the **review stage** to the cheap tier should change review-stage
$ ≈ by the cheap tier's rate ratio at **near-zero accuracy risk**, more cheaply than swapping the
executor. Source: codex review cells (`cx-craft-autoreview-*`), productive-fraction census. Must
preserve the cross-family adversarial gate (RULE 7 / `falsification-review-rubric.md:23-25`).
**Refuted if:** the cheap reviewer's verdicts diverge from the baseline reviewer's beyond CI on the
cross-family gate (accuracy cost), **or** the review-stage $/latency saving is within CI of zero
(no leverage).

---

## 3. Mapping back to the plan

- H1, H2, H4, H5 operationalize `OH-A3` (cheap-tier routing) and `OH-A2` (context-cost proxy) with
  numeric priors; H3 operationalizes `OH-A1` (efficiency, untested for lack of timing). All route
  through `pareto-live-waves` → `pareto-live-review` (`open-hypotheses.md:85`).
- Every hypothesis is **falsifiable with a stated numeric prior** and maps **1:1** to a single live
  contrast that swaps exactly one stage's model among the pre-registered candidates. No dominance or
  frontier claim is made here; those are live-only.
