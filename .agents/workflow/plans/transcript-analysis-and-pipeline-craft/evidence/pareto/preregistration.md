# Pareto historical pass — pre-registration (FROZEN before `rows.jsonl`)

Governing doc: `../../methodology/pareto-measurement-rubric.md` (frontier protocol step 1).
Evidence discipline: `../../methodology/evidence-rubric.md` (E1-E5, R1-R5).

## 0. Work order (stated per acceptance)

This document was written **before** `rows.jsonl` and `cells-manifest.json` were emitted. The
proxy definition, cell axes/bins, dominance directions, exclusion rules, and candidate models below
are **frozen here first**; only after this freeze were rows materialized. The only pre-freeze
inspection performed was a **field-availability survey** (which JSONL/YAML keys exist per source) —
no accuracy value was computed by this pass. The accuracy values consumed are the pre-existing
`iter-*.score.yaml` sidecars authored ~1 month ago by the workflow scorer, not scored here.

Order of operations (auditable by file mtime):
`preregistration.md` → `rows.jsonl` → `cells-manifest.json` → `historical-hypotheses.md`.

## 1. Accuracy proxy (FROZEN — no direct accuracy field exists)

Per rubric axis table (`pareto-measurement-rubric.md:19`) the proxy = score sidecar `value/band`
(rubric-version-matched) + verifier pass ratio + review verdict. Concretely, for this corpus:

| component | field used | availability |
|---|---|---|
| primary | `iter-N.score.yaml` → `value` (0-1) + `band` (`poor\|fair\|good\|excellent`) | iter-1..66 only |
| verifier ratio | `iter-N.yaml` → `verifiers[].status==pass \| gate_passed`; else `tests_total_pass` proxy | 8 rows real verifier block; 45 have the score `verifier` signal via the `tests_total_pass` proxy |
| review verdict | `iter-N.yaml` → `review.overall_decision` | 1 row (`accept`) |
| retry pressure | `impl.retries` (0 → clean; ≥1 → correction pressure) | all 69 iter rows |
| outcome/status (fallback for non-iter sources) | session `status` (`complete\|cutoff\|dispose`), `task_complete` presence, copilot code-change count | OMP / codex / copilot |

**Rubric-version freeze (E4, decisive).** Every one of the 66 score sidecars is
`rubric_version: 2.1.0`. The **live** rubric is `3.0.0`. Therefore:
- Within the historical set, 2.1.0 is internally consistent and usable for hypothesis generation.
- Every historical accuracy value carries `accuracy_live_comparable: false` and is **excluded from
  any cross-version pooling** with the future live 3.0.0 rows (E4 forbids mixed-version aggregates
  unless explicitly normalized; no normalization exists). This is the rubric's
  "rows missing rubric-version match" exclusion (`pareto-measurement-rubric.md:56`) applied honestly
  to the whole historical accuracy axis.
- OMP / codex / copilot rows have **no score sidecar at all** → their accuracy proxy is
  status/outcome only (low resolution), flagged as such; never mixed with a numeric band.

The proxy is **frozen**: no re-scoring, re-weighting, or band-normalization is performed downstream
of this document.

## 2. Cell definitions (FROZEN)

Blocked cells over `model_family × task_class × cache_regime × retry_regime`
(`pareto-measurement-rubric.md:9-11`). Rows are never compared across cells.

**model_family** (from the row's recorded model string; mixed only when families co-occur):
`anthropic` (claude-*, incl. fable/opus/sonnet/haiku) · `openai-codex` (gpt-*, *-codex) ·
`cursor-routed` (glm-*, cursor-proxied `-high` turns) · `mixed` (≥2 families in one session that
does carry `model_change` boundaries) · `unknown` (no model recorded).

**task_class** (derived from the row's own metadata, coarse where the source can't resolve finer):
`orchestration-impl` (OMP workflow-loop mega-sessions) · `impl-slice` (iter-log implementation
iteration) · `recovery` (interrupted-worker recovery artifact) · `review`
(codex-auto-review / verifier stage) · `experiment` (depth-exp / hop-chain trials) ·
`experiment-gate` (scratchpad gate/rg trials) · `codex-session` (codex edit-or-investigation,
not separable from the token table alone) · `smoke` (trivial control) · `failure`
(zero-output / usage-limit abort).

**cache_regime** (cache-read as a share of input/total tokens; bins frozen here):
`cache-hot` = ≥85% · `cache-warm` = 50-84% · `cache-cold` = <50% · `unknown` = no cache accounting.

**retry_regime**: `no-retry` = `retries==0` · `retried` = `retries≥1` · `unknown` = not recorded
for the row (codex/OMP session rows do not carry a per-row retry count).

## 3. Dominance directions (FROZEN — for the LIVE frontier only)

Per `pareto-measurement-rubric.md:26-28`: **minimize** token cost ($), **minimize** token volume,
**minimize** wall-clock; **maximize** completion accuracy. A point dominates iff ≤ on all three
minimized axes, ≥ on accuracy, strict on ≥1.

These directions are recorded so the live pass can apply them. **They are NOT applied to any
historical row in this pass**: the historical corpus is observational and confounded, so no
dominance/frontier/accuracy conclusion is drawn from it (`pareto-measurement-rubric.md:42-46`).
`rows.jsonl`, `cells-manifest.json`, and `historical-hypotheses.md` contain **zero dominance or
frontier claims** by construction.

## 4. Wall-clock decomposition rule (FROZEN)

Per rubric: per stage-run split into model latency vs tool execution vs queue/wait; only model
latency attributes to the model; critical-path time with parallel overlap credited, never summed
serially. Historically this is mostly unavailable: session-level span is recorded; per-turn
`message.duration` (OMP) can be summed but **overlaps background jobs → `[INFERENCE]`, not a
critical path**; codex/iter session spans are first/last-timestamp `[INFERENCE]` and not extracted.
Every historical wall-clock value is tagged `source: recorded | inference | na`; the true
stage-level decomposition is deferred to the live waves.

## 5. Cost axis rule (FROZEN — no fabrication)

$ cost is used **only where recorded**: OMP `usage.cost` (5 sessions + the 3 per-model partitions
of 019f3cf2). codex, claude-code/iter-log, and copilot carry **no USD field** (copilot records
premium-request credits, not USD). The rubric's fallback (`tokens × published-rate(model)`) is
**deferred, not executed**: the evidence base contains no authoritative rate-card, and synthesizing
a dollar figure from an invented rate would violate E5/the delivery contract. Synthesized-cost rows
would be flagged `[INFERENCE]`; none are emitted. Cost synthesis is a live-wave input.

## 6. Candidate models (corrected 2026-07-12 registry probe — `pareto-measurement-rubric.md:38-42`)

- **PRIMARY:** `claude-sonnet-5` (anthropic, 1M ctx) + `gpt-5.6-terra` (openai-codex, 372K ctx).
- **SECONDARY (cheap tier):** `claude-haiku-4-5` + `gpt-5.6-sol`.
- **Baseline executor:** `claude-opus-4-8`.

Historical rows may **propose** routings and effect-size priors for these; they may never establish
a frontier or an accuracy verdict (rubric step 3). Every hypothesis in `historical-hypotheses.md`
maps 1:1 to a plannable live contrast that swaps exactly one stage's model between these candidates.

## 7. Exclusion rules (FROZEN — applied in `cells-manifest.json`)

1. **Rubric-version mismatch:** all historical accuracy values are 2.1.0 (not live 3.0.0) →
   `accuracy_live_comparable:false`, excluded from cross-version pooling.
2. **Cutoff sessions:** OMP `019f4eda`/`019f4eea` + codex 7 cutoffs → `cutoff_accuracy_unreliable`;
   token/cost axes retained (recorded), accuracy proxy degraded. Not dropped unless the cutoff is
   itself the finding.
3. **Mixed-model:** retained when `model_change` boundaries exist (OMP mega-sessions →
   `model_family:mixed`, blended $); per-model attribution only where extracted (019f3cf2 sub-rows).
   Codex comma-model rows flagged `mixed_model_no_change_boundary_in_table`.
4. **Model-unattributed:** 67 iter-log rows have no `agent.model` → `model_family:unknown`, excluded
   from model_family cells; retained for accuracy/retry-regime hypothesis generation only.
