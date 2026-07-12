# Pareto Measurement Rubric — model-stage routing frontier

Objective: tune the frontier over **4 axes** — token cost ($ when available), token volume,
task-completion accuracy, **wall-clock** — for stage-level model routing (cheap-tier executors
and other stages vs. current defaults).

## Unit of measurement

One row per `(session_id, iteration)`. Blocked cells over:
`model_family × task_class × cache_regime × retry_regime`.
Never compare rows across cells; the frontier is per-cell first, pooled second.

## Axis definitions

| axis | source | rules |
|---|---|---|
| token volume | iter-log v2 `session_tokens.*`; OMP `message.usage.{input,output,cache_read,cache_creation}` | cache reads counted separately; report raw + cache-adjusted |
| token cost | OMP `usage.cost` when emitted; else `tokens × published-rate(model)` | synthesized cost rows flagged `[INFERENCE]`; never mix silently |
| completion accuracy | proxy = score sidecar `value/band` (rubric-version-matched) + verifier pass ratio + review verdict | no direct field exists; the proxy definition is FROZEN before data collection (pre-registration) |
| wall-clock | OMP record timestamps (session + per-tool `details.wallTimeMs`); iter-log `checkpoint_at` deltas | stage-level timing is reconstructed → `[INFERENCE]`; session-level is recorded |

## Measurement units and dominance

- **Primary unit: stage-run** — one (stage, model, task, wave) execution with its own tokens /
  cost / wall-clock. Task-level rows aggregate stage-runs and carry the outcome proxy.
- **Dominance directions (explicit):** minimize token cost, minimize token volume, minimize
  wall-clock; maximize completion accuracy. A point dominates iff ≤ on all minimized axes,
  ≥ on accuracy, strict on ≥1.
- **Wall-clock decomposition:** per stage-run split into model latency vs tool execution vs
  queue/wait (from OMP `tool_execution_start`/`toolResult.wallTimeMs` vs message timestamps).
  Only model latency attributes to the model; the frontier uses critical-path time for the
  task, with parallel stage overlap credited, never summed serially.

## Frontier protocol

1. **Pre-register** the accuracy proxy, cell definitions, dominance directions, and candidate
   models BEFORE scoring any row (falsification rubric applies to this doc itself).
2. **Candidates (corrected 2026-07-12 via `omp models` registry probe):** PRIMARY =
   `claude-sonnet-5` (anthropic, 1M ctx) + `gpt-5.6-terra` (openai-codex, 372K ctx) — the
   user's originally requested tiers; both resolvable (the earlier "absent" claim only checked
   `config.yml` role aliases, not the registry). SECONDARY cheap tier = `claude-haiku-4-5` +
   `gpt-5.6-sol` (user-confirmed). Baseline: `claude-opus-4-8` executor.
3. **Historical pass is hypothesis-generation ONLY.** Existing transcripts are observational
   and confounded (task mix, cache state, retries, prompt drift co-vary with model). Historical
   cells may propose candidate routings and effect-size priors; they can NEVER establish the
   frontier or an accuracy conclusion.
4. **Live protocol (identification):** paired runs from identical disposable-task snapshots —
   same repo SHA, same bundle, same profile/verifiers/lenses — across candidate routes, swapping
   ONE stage's model per contrast. ≥3 repeats per (route, task) cell; report per-cell medians
   with bootstrap CIs; a frontier move smaller than its CI is noise, not signal. Runs execute
   via the full-loop runtime on non-overlapping disposable tasks (`--max-waves` bounded).
5. **Stopping rule:** stop adding repeats/cells when the non-dominated set is unchanged across
   k=3 consecutive batch additions AND every surviving point's CI excludes dominance reversal
   on all axes. Report dominated/non-dominated sets per cell, then pooled.
6. Exclusions: rows missing rubric-version match, rows from `cutoff` sessions (unless the
   cutoff is itself the finding), mixed-model iterations without `model_change` boundaries.

## Deliverable

`evidence/pareto/frontier-report.md` + raw rows (`rows.jsonl`) + cell manifest, each row
anchored per the evidence rubric so any point is re-derivable from its transcript.
