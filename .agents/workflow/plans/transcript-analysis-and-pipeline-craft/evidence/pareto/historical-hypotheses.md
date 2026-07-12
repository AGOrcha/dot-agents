# Historical pass — per-cell summaries & candidate routing hypotheses

Hypothesis-generation ONLY (`pareto-measurement-rubric.md:42-46`). The historical corpus is
observational and confounded (task mix, cache state, retries, prompt drift co-vary with model), so
**nothing below is a dominance, frontier, or accuracy conclusion.** Each cell summary describes the
*shape* of what was recorded; each hypothesis is a falsifiable prior that a live paired contrast
(rubric step 4) settles. Rows cited by `row_id` in `rows.jsonl`; prior findings by their index ID.

Anchoring corpus facts (all from `rows.jsonl` / `items/*`):
- **Cache-read dominates $ and volume — unevenly.** OMP/anthropic mega-sessions: cacheRead = **96-98%**
  of tokens and **62-69%** of $ across the 4 large sessions (`omp:019f3cf2/019f3f23/019f4eda/019f4eea`;
  items `omp-cost-totals-*`, `cost-cacheread-dominates-context`). Codex census: cached_input/input median
  **88.8%**, cached/total median **87.7%** — but with a wide spread (**16-98%** of total; 48/120 rows below
  the 85% cache-hot line); cache-cold review turns run 45-82% cached (`codex:*`, item block
  `cx-cost-permodel-*`). The uniform ">95% cached" read holds only for the cache-saturated OMP/CC
  mega-sessions, not the codex census.
- **The model-generated slice is tiny; the uncached/fresh slice is not (and the two were conflated).**
  *Model-generated output* (the accuracy-bearing work; reasoning is a SUBSET of output, never added
  again): codex median **8,567 tok = 0.93%** of total (max 4.05%). *Uncached/fresh volume*
  (`productive_tokens = total − cache_read`, billed at full non-cache rates): codex median **12.3%** of
  total (max ~84% on cache-cold turns); OMP **2-4%**. The superseded figure — codex "productive
  (output+reasoning) median 12,070 tok = 1.26%" — **double-counted reasoning and omitted non-cached
  input**; see `## Erratum (token normalization)`.
- **Fixed per-request tax is large on short tasks.** Copilot trivial query: tool defs **16,290 /
  24,342 ctx tokens = 67%** (`copilot:23e45a55`; item `copilot-tooldef-fixed-overhead`).
- **Wall-clock is tool/turn-heavy.** OMP summed model time **1.5h** (019f3cf2) / **13.2h**
  (019f3f23) over multi-day spans, against **278-826 bash + 187-322 read** tool calls per session
  (items `omp-turn-duration-shape-*`, OMP tool-call table). Stage decomposition is `[INFERENCE]`.

---

## 1. Per-cell summaries (shape only)

20 populated cells (`cells-manifest.json`). Grouped by which axes each can feed:

### openai-codex cells (census; 120 rows, cache_regime derivable, NO $/accuracy axis)
- **`openai-codex / codex-session / cache-hot / unknown` (n=50)** and **`… / cache-warm /…`
  (n=15):** the bulk of the codex census. Feeds **token-volume + cache-regime** priors only
  (median cached_input/input 88.8%; model-generated output median 0.93% of total; uncached/fresh
  volume median 12.3%). No $ (codex has no cost field —
  `no_dollar_cost_axis_codex`), no rubric-matched accuracy. Retry unknown.
- **`openai-codex / experiment{,-gate} / cache-{hot,warm}` (n=13+13+12+8=46):** depth-exp &
  scratchpad-gate trials — short, high-cache, structurally unlike the workflow loop; volume/cache
  only.
- **`openai-codex / review / cache-{warm,cold}` (n=5+3=8):** `codex-auto-review` turns — very low
  **model-generated output (145-1036 tok, median 326**: the lowest generation workload of any codex
  class), but cache-warm/cold (cached_input/input **45-82%**) so their uncached/fresh volume is *high*
  (median 27,090, up to 172,149; 18.5-54.8% of total), NOT low. This is the historical analogue of a
  cheap **review-stage** cell (feeds H6 — leverage is on accuracy risk, not volume).

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
| **C5** | PRIMARY (`sonnet-5`/`terra`) vs SECONDARY (`haiku-4-5`/`sol`), stratified by generated-output fraction | executor |
| **C6** | reviewer/verifier stage → cheap tier (`haiku-4-5` or `gpt-5.6-sol`), executor held at baseline; cross-family gate (RULE 7) | review |

### H1 — token-volume is near-invariant under an executor swap → **C1**
**Prior (effect size):** at a fixed snapshot (same bundle, same tool outputs) an executor model swap
moves **token volume by ≤ ~4%**, because the model directly *generates* only its output (codex median
output **0.93%** of total, max 4.05%); the uncached-input (~11%) and cache-read (codex ~88% / OMP 96-98%)
volume are set by context/pipeline size, not the model. Source: `omp-cost-totals-*`,
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

### H5 — cheap-tier accuracy risk is localized to the generated-output fraction → **C5**
**Prior (effect size):** the model's actual work is the **generated output** (reasoning is a SUBSET of
output, not additive) — codex median **0.93% = 8,567 tok** (max 4.05%); the other ~99% (cache-read ~88% +
uncached input ~11%) is context the model *reads*, not generates — quality-neutral. So a weaker
model degrades accuracy **only** in proportion to a task's generated-output fraction: expect PRIMARY
(`sonnet-5`/`terra`) accuracy within its own CI of baseline on most task classes, and SECONDARY
(`haiku-4-5`/`sol`) accuracy drop to **grow with generated-output fraction**, staying ≈baseline on
low-output (context-shuffling/review) work. Source: codex generated-output census, `OH-A3`
(cheap-tier-on-frontier, untested). **Historical accuracy cannot test this** (2.1.0 only, no model
attribution) — C5 is the first identification.
**Refuted if:** SECONDARY matches baseline accuracy (CI-overlapping) on high-output task classes,
**or** PRIMARY underperforms SECONDARY, **or** the accuracy drop is uniform across generated-output-fraction
strata (would break the localization mechanism).

### H6 — the review/verifier stage is the highest-leverage cheap-routing target → **C6**
**Prior (effect size):** historical review turns (`codex-auto-review`, cell `…/review/…`) carry
**very low model-generated output (145-1036 tok, median 326)** — the lowest generation workload of any
codex class; being cache-cold their uncached *volume* is high, not low — the leverage is a classification, not a
generation, workload. So routing the **review stage** to the cheap tier should change review-stage
$ ≈ by the cheap tier's rate ratio at **near-zero accuracy risk**, more cheaply than swapping the
executor. Source: codex review cells (`cx-craft-autoreview-*`), codex generated-output census. Must
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

---

## Erratum (token normalization) — 2026-07-12

A telemetry-accounting audit found the codex-census (and iter-log) `productive` figure was computed as
**`output + reasoning`**, which is wrong two ways:
1. **Double-counts reasoning.** For every codex row `total == input + output` exactly (verified 120/120,
   0 exceptions); `reasoning_output_tokens` is a **subset of `output_tokens`** (already inside it, never
   added to `total`). Adding it again inflates the figure.
2. **Omits non-cached input.** The uncached (fresh, non-cache-read) input the model actually processes was
   left out entirely. For codex, `input` INCLUDES the cache-read portion, so
   `uncached_input = input − cached_input`.

**Corrected definitions** (applied to `rows.jsonl`, `cells-manifest.json`; `preregistration.md` left frozen):
- `output_tokens` = model-generated tokens (`= total − input` for codex). `reasoning_output_tokens` ⊆ output
  (flag `reasoning_output_tokens_subset_of_output: true`); never added to any sum.
- `uncached_input_tokens` = `input − cached_input` (codex) / `input` (anthropic OMP & iter-log, whose `input`
  already excludes cache).
- `productive_tokens` = `total − cache_read` = uncached_input (+ cache_creation) + output — the uncached/fresh
  volume billed at full (non-cache) rates. OMP's prior `productive_est` already used this (unchanged); codex
  and iter-log did not.
- `model_generated_tokens` = `output_tokens`, reported separately as the accuracy-bearing "model work" slice
  (distinct from productive/uncached volume). Every row now carries `token_components_sum_check`.

Worked example (top codex row, session 019f38ed): total 62,156,886 = input 61,960,105 + output 196,781;
cached_input 59,128,960; uncached_input 2,831,145; reasoning 80,418 (⊆ output). Old productive =
196,781 + 80,418 = **277,199**; corrected productive = 196,781 + 2,831,145 = **3,027,926**.

### Numbers changed (old → corrected)
| figure | old | corrected |
|---|---|---|
| codex "productive" median | 12,070 tok (out+reas) | **124,297 tok** (out+uncached_input) |
| codex "productive" max | 292,878 tok | **3,059,313 tok** |
| codex productive % of total, median | 1.26% | **12.28%** |
| codex productive % of total, max | 5.14% | **83.77%** |
| codex model-generated output, median (new field) | — (hidden inside 1.26%) | **8,567 tok = 0.93%** (max 4.05%) |
| codex cached, median | "88%" | cached/input **88.8%**; cached/total **87.7%** (range 16.2-97.8%) |
| review-cell "productive" | "220-720" (out+reas; true 220-1921) | generated output **145-1036** (median 326); uncached vol **8,540-172,149** (median 27,090) |
| review cached% | "46-63%" | cached/input **45.6-82.0%**; cached/total 45.2-81.5% |
| iter-63 productive | 494,666 (output-only) | **2,101,525** (total−cache_read) |
| iter-64 productive | 62,641 (output-only) | **146,087** |
| cache_regime: cache-hot / cache-warm | 83 / 39 | **82 / 40** (session 019d3bd9: rounded 85% → exact 84.88% = warm) |
| codex-session/cache-hot cell n | 51 | **50** |
| codex-session/cache-warm cell n | 14 | **15** |

OMP figures are **unchanged**: cacheRead 96-98% of tokens (96.20-97.71%) and 62-69% of $; productive 2-4%
(2.29-3.80%) — OMP `productive_est` was already `total − cache_read`.

### Hypothesis priors touched
- **H1** (volume near-invariant): the ≤~4% prior stands, but is now grounded on the **model-generated
  output fraction** (codex median 0.93%, max 4.05%), not the superseded "productive 1.26%". Uncached-input
  volume (~11%) is pipeline-set, not model-set — consistent with "to move volume, change the pipeline, not
  the model."
- **H5** (accuracy risk localized): restated onto **generated output** (codex median 0.93% = 8,567 tok; max
  4.05%), replacing "output+reasoning 1.26% = 12,070 tok". The quality-bearing slice is smaller and cleaner
  than reported — the mechanism strengthens.
- **H6** (review = cheap-routing target): review turns have very low **generated output** (145-1036 tok),
  carrying the accuracy-risk argument; but they are cache-cold, so their **uncached volume is high** (median
  27,090, up to 54.8% of total), **not** low. The "very low productive tokens" framing was doubly misleading
  and is corrected: the review-stage leverage is on accuracy risk, not on token volume.
- **H2, H3, H4**: no numbers shifted (OMP $ shares, wall-clock, copilot fixed-tax are unaffected).

### Headline claims, re-verified honestly
- **"cache-read ~89-99% of volume":** HOLDS for the OMP/CC/anthropic mega-sessions (96-98% of tokens; iter
  63/64 96.1/98.7%). For the **codex census it weakens** — cached/total median 87.7%, range **16.2-97.8%**,
  48/120 rows below the 85% cache-hot line, cache-cold tail (3 review + 1 experiment turn) below 50%. Not a
  single 89-99% band; it is source- and task-dependent.
- **"productive ~1-4%":** TRUE only when "productive" means the **model-generated output** (codex median
  0.93%, max 4.05%; OMP <1%). Under the corrected **uncached/fresh-volume** definition it is **2-4% for OMP
  but ~12% median (up to ~84%) for codex** — materially higher, because codex sessions accumulate less cache
  than the OMP mega-sessions. The old "1.26%" matched neither definition.

**Cross-family review advisory (RULE 7 / `falsification-review-rubric.md:23-25`).** These remain corrected
observational priors — **not** dominance, frontier, or accuracy conclusions; the live paired contrasts
(rubric step 4) are still the only settlement. The correction *widens* the codex uncached-volume prior and
SHOULD be re-checked by the adversarial cross-family reviewer before any live-wave sizing leans on it.

---

## Erratum 2 (Claude Code JSONL field fidelity) — 2026-07-12

External reference (`evidence/prior/external-references-2026-07-12.md` §1; Gille 2026-02-24,
PRELIMINARY per author) reports CC JSONL `usage.input_tokens` as a streaming placeholder
(their corpus: 75% ≤1, input under-recorded 102-174×), `output_tokens` partially placeholder
and thinking-exclusive, with 51-55% duplicate `requestId` entries; only
`cache_read_input_tokens`/`cache_creation_input_tokens` match the accurate statusbar source (~1×).

**Local verification (this machine, 2026-07-12, 50 primary CC project files / 1,680 incl
subagents, 35,515 assistant usage entries):** input≤1 = **12%** (placeholder severity far below
their 75% — version/era-dependent), output≤1 = 0%, but **76% of requestIds appear more than
once**. Two overcount ratios follow: the naive-vs-deduped **entry count is 2.39×**, while the
**per-field token** overcount differs by field — input 3.01×, output 3.02×, cacheRead 2.38×,
cacheCreate 3.04× (the "~2.4×" headline is coincidentally correct only for cacheRead).

**Impact on this corpus:** the CC-derived token figures in inventory/items (`has_tokens` for
claude-code sessions) were summed WITHOUT requestId dedup and with no placeholder exclusion. The
cited CC cache-share item `input=2 / cache_read=28,175` is a **single verified turn** (one usage
entry), so requestId dedup is irrelevant to it — it is quoted to illustrate CC's cache-dominated
field shape, not as an instance of the dedup defect. No rows.jsonl codex/OMP/iter figures
are affected (different harnesses). Field-trust rule going forward:
- CC high-trust fields: `cache_read_input_tokens`, `cache_creation_input_tokens`.
- CC low-trust fields: `input_tokens` (12% placeholders locally; possibly under-recorded),
  `output_tokens` (excludes thinking).
- MANDATORY: dedup by last-entry-per-`requestId` before any CC summation.
- CC rows feeding any live-wave sizing must be recomputed under this rule or excluded; the
  capability matrix entry for claude-code downgrades to "tokens: low-fidelity (cache fields only)".
