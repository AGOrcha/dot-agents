# Outcome-Scoring Rubric

**Status:** active
**Rubric version:** 1.0.0
**Owners:** dot-agents
**Go source:** [`internal/scoring/rubric.go`](../internal/scoring/rubric.go)
**Related:** [`agent-run-scoring-observability-platform.md`](../.agents/proposals/agent-run-scoring-observability-platform.md) (R1, the requirement this rubric serves); [ADR-0004](adr/0004-execution-telemetry-schema-seed.md) (the execution-telemetry pillar the input signals come from); [`workflow-iter-log.schema.json`](../commands/workflow/static/workflow-iter-log.schema.json) (the iteration-log schema the signals are read from)

## Purpose

R1 of the agent-run scoring proposal asks for an **explainable** quality
score per session and per iteration, computed from already-captured
telemetry. This document is the authoritative, versioned definition of
how that score is computed: which signals feed it, how each is weighted,
and how they combine.

The rubric is split in two:

- **This document** — the human-readable contract. It is the canonical
  spec; the Go code must agree with it.
- **`internal/scoring/rubric.go`** — the same rubric as a versioned Go
  data structure. The `scorer` task consumes it; it does not redefine it.

Changing the rubric — adding a signal, moving a weight, changing the
combination method — means editing **both** in the same commit and
bumping `RubricVersion`. That is the "deliberate, reviewable act" R1
requires: a rubric change is a reviewable diff, not a silent constant
edit buried in scoring logic.

## Versioning policy

`RubricVersion` is semantic:

- **major** — the signal set changed, or the combination method changed.
  Scores across a major boundary are not comparable.
- **minor** — a weight changed, or a sub-score mapping changed. Scores
  shift but the shape is the same.
- **patch** — wording, score-band thresholds, or documentation only.

Every persisted score records the `RubricVersion` it was computed under
(see the `persist` task), so a later rubric change never silently
invalidates historical scores.

## Input signals

Five signals, taken verbatim from the R1 requirement: token/cache
telemetry, verifier results, merge-back status, scope adherence, test
outcomes. Each signal is mapped to a **sub-score in `[0, 1]`** or is
reported **absent** when the telemetry to compute it was never captured.

Absent is a first-class state — see [Combination](#combination). Sub-score
extraction itself is the `signals` and `scorer` tasks; this section is the
contract those tasks implement.

### 1. `verifier` — Verifier results (weight 0.30)

Did the iteration's verification gates pass.

- **Source:** `verifiers[].status` in the iteration log.
- **Mapping:** over verifier entries whose status is one of
  `pass` / `fail` / `partial` (entries with status `unknown` are
  excluded), sub-score = mean of `pass → 1.0`, `partial → 0.5`,
  `fail → 0.0`.
- **Absent when:** there are no verifier entries, or every entry has
  status `unknown`.
- **Why the highest weight:** the verifier gate is the broadest direct
  check that the work is correct. If it fails, the run mostly failed,
  regardless of how efficient or in-scope it was.

### 2. `tests` — Test outcomes (weight 0.25)

Did the iteration's tests pass.

- **Source:** `impl.focused_tests_pass` and `verifiers[].tests_total_pass`
  (each a tri-state: `true` / `false` / unset).
- **Mapping:** sub-score = fraction of the *set* pass-flags that are
  `true`.
- **Absent when:** no pass-flag is set anywhere in the entry.
- **Note:** `*.tests_added` (test volume) is **not** scored in v1 — adding
  tests is good practice but not an outcome. It is carried in the score
  breakdown as context only.

### 3. `merge_back` — Merge-back status (weight 0.20)

Was the iteration's work accepted into the trunk.

- **Source:** the merge-back artifact for the task, resolved by the
  `signals` task (merge-back archive presence; `review.overall_decision`
  as the fallback acceptance signal).
- **Mapping:** `accepted` / `merged → 1.0`; `escalated` /
  `pending → 0.5`; `rejected → 0.0`.
- **Absent when:** the iteration did no delegated work and therefore had
  no merge-back step (direct work is common — see the data note below).
- **Why 0.20:** partly a downstream consequence of `verifier` and
  `tests`, but it adds the independent "a reviewer accepted this"
  dimension, so it is weighted below them, not equal.

### 4. `scope` — Scope adherence (weight 0.15)

Did the iteration stay within its declared write-scope.

- **Source:** `impl.scope_note` in the iteration log.
- **Mapping:** `on-target → 1.0`; `partial → 0.5`; `scope-breach → 0.0`.
- **Absent when:** `scope_note` is empty or a free-text value that does
  not normalize to one of the three canonical states. (Historical
  entries predate the schema enum and carry free-text notes; the
  `signals` task normalizes a leading `on-target` prefix, otherwise
  treats the note as absent.)
- **Why 0.15:** a real quality signal, but a correct run that slightly
  overran its file scope is still mostly a good run.

### 5. `token_efficiency` — Token & cache efficiency (weight 0.10)

How efficiently the iteration used the model.

- **Source:** `session_tokens.cache_hit_rate` in the iteration log.
- **Mapping:** sub-score = `cache_hit_rate` directly (already `[0, 1]`).
- **Absent when:** there is no `session_tokens` block, or it carries no
  cache tokens at all.
- **Why the lowest weight:** this is an efficiency metric, not a
  correctness one. A correct-but-expensive run should still score well;
  efficiency only breaks ties.

### Weight summary

| Signal             | Weight | Kind        |
|--------------------|-------:|-------------|
| `verifier`         |   0.30 | correctness |
| `tests`            |   0.25 | correctness |
| `merge_back`       |   0.20 | correctness |
| `scope`            |   0.15 | process     |
| `token_efficiency` |   0.10 | efficiency  |
| **Total**          | **1.00** |           |

Correctness signals total 0.75; process and efficiency total 0.25. The
weighting is deliberate: a run is scored first on whether it worked.

## Combination

Method: **`weighted_mean_renormalized`**.

```
score = Σ (weightᵢ × sub_scoreᵢ)  /  Σ weightᵢ        for every present signal i
```

Absent signals drop out of **both** sums. The remaining weights
renormalize, so a missing signal neither inflates nor deflates the score
— it simply does not vote. This matters: the captured telemetry is
sparse (see the data note), and a rubric that treated "absent" as 0
would punish every iteration that predates a telemetry field.

If **every** signal is absent the iteration is **unscored** (numeric
score is null, band `unscored`) — the rubric never invents a score from
nothing.

The score is explainable by construction: the breakdown reports, per
signal, its `present` flag, raw input, sub-score, nominal weight,
renormalized effective weight, and contribution (`effective_weight ×
sub_score`). The contributions of present signals sum exactly to the
final score.

## Score bands

A numeric score is also reported as a human-readable band:

| Band        | Range          |
|-------------|----------------|
| `excellent` | `score ≥ 0.85` |
| `good`      | `0.70 ≤ score < 0.85` |
| `fair`      | `0.50 ≤ score < 0.70` |
| `poor`      | `score < 0.50` |
| `unscored`  | no signals present |

## Worked examples

**A clean iteration, no token telemetry.** Verifier passed, tests
passed, work was merged, scope on-target; the entry predates
`session_tokens` so `token_efficiency` is absent.

| Signal             | Present | Sub-score | Weight | Eff. weight | Contribution |
|--------------------|---------|----------:|-------:|------------:|-------------:|
| `verifier`         | yes     | 1.00      | 0.30   | 0.333       | 0.333        |
| `tests`            | yes     | 1.00      | 0.25   | 0.278       | 0.278        |
| `merge_back`       | yes     | 1.00      | 0.20   | 0.222       | 0.222        |
| `scope`            | yes     | 1.00      | 0.15   | 0.167       | 0.167        |
| `token_efficiency` | no      | —         | 0.10   | —           | —            |

Present weights sum to 0.90; `score = 0.90 / 0.90 = 1.00` → **excellent**.

**A failed iteration.** Verifier failed, tests failed, work was
rejected, scope partial, cache hit rate 0.60.

| Signal             | Present | Sub-score | Weight | Contribution |
|--------------------|---------|----------:|-------:|-------------:|
| `verifier`         | yes     | 0.00      | 0.30   | 0.000        |
| `tests`            | yes     | 0.00      | 0.25   | 0.000        |
| `merge_back`       | yes     | 0.00      | 0.20   | 0.000        |
| `scope`            | yes     | 0.50      | 0.15   | 0.075        |
| `token_efficiency` | yes     | 0.60      | 0.10   | 0.060        |

All signals present (weights sum to 1.00); `score = 0.135` → **poor**.

## Data note

The rubric is grounded in the 65 iteration-log entries salvaged into
this branch. Signal population there is uneven: `scope_note` is set in
92% of entries, but `verifiers` in only 11%, `review` in 2%, and
`session_tokens` in 3%. The renormalizing combination is the direct
consequence — most historical iterations will be scored on two or three
present signals, and that is correct behaviour, not a degraded one.

## Changing the rubric

A rubric change is a reviewable act. To change it:

1. Edit this document and `internal/scoring/rubric.go` **in the same
   commit** — they must never disagree.
2. Bump `RubricVersion` per the [versioning policy](#versioning-policy).
3. `internal/scoring` tests assert weights sum to 1.0, signal IDs are
   unique, and the version is pinned — they will fail until the change
   is internally consistent.
