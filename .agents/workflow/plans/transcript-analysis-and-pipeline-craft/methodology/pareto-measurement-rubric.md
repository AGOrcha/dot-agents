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

## Cross-family adversarial gate — validity stage (live-wave amendment 2026-07-12)

The blocking cross-family adversarial gate (RULE 7; `falsification-review-rubric.md:23-25`) is a
**review-validity stage, not a measured routing axis.** This resolves the RULE-7 self-collision that
would otherwise make contrasts C3/C4/C5-gpt-legs/C6-haiku-leg unexecutable (a gpt-family executor with
the historically-fixed gpt-family adversarial lens → `reviewer.family == executor.family` →
`pipeline_projection.go:409` and `cc_pipeline.go:277` hard-refuse the gate). The code gate is correct
and stays unweakened; the fix is in how the live protocol assigns the gate's route:

7. **The adversarial gate's `model_family` is a DEPENDENT variable, pinned OPPOSITE the executor family
   per contrast** — never a constant family, never an independently-swept axis. Concretely:
   claude-family executor → gpt-family adversarial gate (existing `cross-harness-adversarial`,
   `gpt-5.4`/`gpt`); gpt-family executor → claude-family adversarial gate
   (`cross-harness-adversarial-claude`, `claude-opus-4-8`/`claude`). This SATISFIES RULE-7 (families
   differ both sides); it is the correct application of the anti-collusion rule, not an exception to it.
   The opposite-family flip is a *consequence* of the executor swap, so the "swap ONE stage's model,
   unchanged lenses" identity (step 4) still holds for the **measured** stages (executor + verifiers +
   routine review lenses). C6 corollary: when the cheapened stage IS the adversarial gate, the cheap
   model must be from the opposite family (claude executor → `gpt-5.6-sol`); the `haiku-4-5` leg of C6
   is valid only for cheapening a *routine* verifier/lens slot (no family constraint), never the gate.
8. **The adversarial gate's own stage-run (tokens/$/wall-clock) is EXCLUDED from the frontier
   cost/accuracy cell** and reported separately as review-validity overhead. The frontier cell is
   attributed to the **first-pass** executor + verifier stage-runs. Adversarial-gate-INDUCED re-work
   (extra executor iterations triggered by a gate REJECT) is a separate stage-run, reported per-contrast,
   NOT summed into the executor cell.
9. **Known limitation — per-contrast gate-strictness confound.** Because the gate's family flips between
   claude-executor contrasts (gpt gate) and gpt-executor contrasts (claude gate), a family-dependent
   difference in gate strictness (reject/block rate) could induce different re-work rates that would leak
   into the measured cell if re-work were summed in. To keep it visible: **every contrast MUST report the
   gate's verdict distribution (accept/reject/block counts) and induced re-work iteration count alongside
   the cell.** A frontier claim is invalid if the executor-swap delta cannot be separated from the
   gate-strictness delta at the reported block rates. Executable-as-preregistered without the flip:
   C1, C2, C5-claude-legs (`sonnet-5`/`haiku-4-5`), C6-`gpt-5.6-sol`-leg. Requiring the flip: C3, C4,
   C5-gpt-legs (`terra`/`sol`), C6-`haiku-4-5`-leg. Deterministic per-contrast route map:
   `evidence/pareto/live-contrast-lens-map.md`.

## C0 null contrast — gating amendment (2026-07-12)

10. **Step 4a — C0 (A/A null) runs before every other contrast.** Baseline vs baseline
    (`claude-opus-4-8` both arms), identical disposable-task snapshot, **n≥5 repeats**, on the
    cheapest task from `evidence/pareto/disposable-tasks.md`. C0 measures what no historical
    cell can: pure same-config repeat variance (the null distribution all step-4 CIs ride on),
    an empirical per-run $ figure for wave budgeting, production burn-in of rubric 3.0.0
    (shadow-reconciled 2026-07-12: 11/12 historical iters within ±0.004 of 2.1.0, one
    explainable band change — `evidence/pareto/scorer-3.0.0-shadow-burn-in.md`), and a
    resumability exercise (T-b1). **Gate:** if C0's paired delta CI exceeds half the smallest
    active hypothesis threshold (H1 restated: ±7%, see historical-hypotheses Erratum 3 —
    so gate = 3.5% volume), C1-C6 do NOT launch as designed; redesign first (larger thresholds,
    productive-tokens-only volume axis, or a repeat count derived from C0's measured CV).
    Rationale + empirical basis: `reviews/red-team-premortem-2026-07-12.md` RT-7/F1.

## Deliverable

`evidence/pareto/frontier-report.md` + raw rows (`rows.jsonl`) + cell manifest, each row
anchored per the evidence rubric so any point is re-derivable from its transcript.
