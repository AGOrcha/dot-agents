# Outcome-Scoring Rubric

**Status:** active
**Rubric version:** 2.0.0
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

## Two-way checks and the integrity track

A signal can have **two** sources for the same fact:

- a **self-reported** source — the agent's own claim (an iteration-log
  `scope_note`, a `focused_tests_pass` flag, a `persisted_via_workflow_commands`
  note); and
- an **objective** source — something checkable independently of the
  agent (git topology, a verification artifact, the changed file set).

For a signal with both, the rubric scores the run from the **objective**
source. It additionally records the **claimed-vs-observed delta**
(`observed − claimed`) as an **integrity** metric. A negative delta is an
over-claim: the agent reported better than reality.

Integrity deltas are attributed to the role that made the claim —
`impl`, `verifier`, or `review` — because the v2 iteration-log blocks are
role-owned. Aggregated, they form a per-role honesty profile: which role
types over-claim, and therefore where environment helpers and enforcers
are worth adding.

The integrity track is a **separate parallel output**. It never affects
the numeric outcome score — the score answers "was the run good?", the
integrity track answers "was the self-report honest?", and conflating
them would muddy both. Signals marked `TwoWay` in `rubric.go` are the
ones that feed it.

## Input signals

Six signals. Each is mapped to a **sub-score in `[0, 1]`** or is reported
**absent** when the telemetry to compute it was never captured. Absent is
a first-class state — see [Combination](#combination). Sub-score
extraction itself is the `signals` and `scorer` tasks; this section is
the contract those tasks implement.

### 1. `landed` — Landed on master (weight 0.22, two-way)

Did the iteration's work survive into the trunk.

- **Objective source:** git topology — the iteration's `commit` SHA is
  reachable from `master` and has not been reverted or superseded.
- **Self-reported source:** `self_assessment.persisted_via_workflow_commands`
  and `review.overall_decision`.
- **Mapping:** commit reachable from `master` and not reverted → `1.0`;
  reachable but later reverted → `0.0`; orphaned / never landed → `0.0`.
- **Absent when:** the `commit` SHA cannot be resolved at all — early
  entries carry abbreviated SHAs from since-rebased history. Squashed or
  rebased work whose verbatim SHA is gone but whose change did land is a
  known hard case; the `signals` task falls back to patch-id / commit-
  message matching before declaring the signal absent.
- **Why the highest weight:** surviving in `master` is the truest
  outcome there is — it is ground truth, not self-report.

### 2. `verifier` — Verifier results (weight 0.20, two-way)

Did the iteration's verification gates pass.

- **Objective source:** `verifiers[].status` (v2 iteration log), the
  `da workflow verify` log, and `review-decision.yaml` outcomes. For v1
  entries, which have no `verifiers` array, `tests_total_pass` is the
  verifier proxy.
- **Self-reported source:** `self_assessment.ran_cli_command` and the
  related discipline flags.
- **Mapping:** over verifier records whose status is `pass` / `fail` /
  `partial` (status `unknown` excluded), sub-score = mean of
  `pass → 1.0`, `partial → 0.5`, `fail → 0.0`.
- **Absent when:** no verifier evidence of any kind exists for the entry.

### 3. `tests` — Test outcomes (weight 0.18, two-way)

Did the iteration's tests pass.

- **Objective source:** the verification artifact's test result.
- **Self-reported source:** `impl.focused_tests_pass` (v2),
  `verifiers[].tests_total_pass` (v2), and top-level `tests_total_pass`
  (v1) — each a tri-state `true` / `false` / unset.
- **Mapping:** sub-score = fraction of the *set* pass-flags that are
  `true`.
- **Absent when:** no pass-flag is set anywhere in the entry.
- **Note:** test *volume* (`tests_added`) is not scored — adding tests is
  good practice but not an outcome. It rides in the breakdown as context.

### 4. `correction_pressure` — Correction pressure (weight 0.15)

How little the iteration had to be corrected. A new signal: it is the
most informative thing the previous rubric left unweighted.

- **Source:** `retries` (iteration log), `post_invocation.retries_in_loop`
  and `post_invocation.user_corrections` (`review-decision.yaml`), and
  the tool-call error rate from the agent transcript (`is_error` over
  tool calls).
- **Mapping:** sub-score = `1 / (1 + retries + user_corrections +
  2·error_rate)` — `1.0` for a clean run, decaying as corrections
  accumulate. `error_rate` is in `[0, 1]`; its coefficient `2` is a
  rubric constant.
- **Absent when:** none of the three inputs is available.
- **Not two-way:** it is a composite of weakly-self-reported and
  objective inputs with no single clean claimed/observed pair.

### 5. `scope` — Scope adherence (weight 0.15, two-way)

Did the iteration stay within its declared write-scope.

- **Objective source:** the changed file set (`git diff`) checked against
  the task's declared `write_scope` — the same comparison
  `da workflow plan check-scope` performs.
- **Self-reported source:** `impl.scope_note` (v2) / top-level
  `scope_note` (v1): `on-target → 1.0`, `partial → 0.5`,
  `scope-breach → 0.0`. Historical entries predate the schema enum and
  carry free-text notes; a leading `on-target` prefix normalizes,
  otherwise the self-report is treated as absent.
- **Mapping:** objective sub-score = fraction of changed files inside the
  declared scope. Falls back to the normalized `scope_note` when no
  `write_scope` is declared for the task.
- **Absent when:** neither a `write_scope` nor a usable `scope_note`
  exists.

### 6. `token_efficiency` — Token & cache efficiency (weight 0.10)

How efficiently the iteration used the model.

- **Source:** `session_tokens.cache_hit_rate` in the iteration log;
  backfilled from Claude and Codex transcripts where the iteration log
  itself never captured it (see the data note).
- **Mapping:** sub-score = `cache_hit_rate` directly (already `[0, 1]`).
- **Absent when:** no token telemetry exists and none can be backfilled.
- **Why the lowest weight:** this is an efficiency metric, not a
  correctness one. A correct-but-expensive run should still score well.

### Weight summary

| Signal                | Weight | Kind        | Two-way |
|-----------------------|-------:|-------------|:-------:|
| `landed`              |   0.22 | correctness | yes     |
| `verifier`            |   0.20 | correctness | yes     |
| `tests`               |   0.18 | correctness | yes     |
| `correction_pressure` |   0.15 | process     | no      |
| `scope`               |   0.15 | process     | yes     |
| `token_efficiency`    |   0.10 | efficiency  | no      |
| **Total**             | **1.00** |           |         |

Correctness signals total 0.60; process signals total 0.30; efficiency
0.10. The weighting is deliberate: a run is scored first on whether it
worked and landed.

## Combination

Method: **`weighted_mean_renormalized`**.

```
score = Σ (weightᵢ × sub_scoreᵢ)  /  Σ weightᵢ        for every present signal i
```

Absent signals drop out of **both** sums. The remaining weights
renormalize, so a missing signal neither inflates nor deflates the score
— it simply does not vote. This matters: the captured telemetry is
sparse, and a rubric that treated "absent" as 0 would punish every
iteration that predates a telemetry field.

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

**A clean iteration, no token telemetry.** Landed on master, verifier
passed, tests passed, no corrections, scope on-target; the entry predates
`session_tokens` and no backfill was possible, so `token_efficiency` is
absent.

| Signal                | Present | Sub-score | Weight | Eff. weight | Contribution |
|-----------------------|---------|----------:|-------:|------------:|-------------:|
| `landed`              | yes     | 1.00      | 0.22   | 0.244       | 0.244        |
| `verifier`            | yes     | 1.00      | 0.20   | 0.222       | 0.222        |
| `tests`               | yes     | 1.00      | 0.18   | 0.200       | 0.200        |
| `correction_pressure` | yes     | 1.00      | 0.15   | 0.167       | 0.167        |
| `scope`               | yes     | 1.00      | 0.15   | 0.167       | 0.167        |
| `token_efficiency`    | no      | —         | 0.10   | —           | —            |

Present weights sum to 0.90; `score = 0.90 / 0.90 = 1.00` → **excellent**.

**A struggling iteration.** Did not land, verifier failed, tests failed,
three retries, scope partial, cache hit rate 0.60.

| Signal                | Present | Sub-score | Weight | Contribution |
|-----------------------|---------|----------:|-------:|-------------:|
| `landed`              | yes     | 0.00      | 0.22   | 0.000        |
| `verifier`            | yes     | 0.00      | 0.20   | 0.000        |
| `tests`               | yes     | 0.00      | 0.18   | 0.000        |
| `correction_pressure` | yes     | 0.25      | 0.15   | 0.0375       |
| `scope`               | yes     | 0.50      | 0.15   | 0.075        |
| `token_efficiency`    | yes     | 0.60      | 0.10   | 0.060        |

All signals present (weights sum to 1.00); `score ≈ 0.173` → **poor**.

## Data note

The rubric is grounded in the 65 iteration-log entries salvaged into this
branch — **two schemas**: 39 flat v1 entries and 26 nested v2 entries,
both of which the `signals` reader handles. Native signal population is
uneven: `scope_note` is set in ~92% of entries, but `verifiers` in only
~11%, `review` in ~2%, and `session_tokens` in ~3%. The renormalizing
combination is the direct consequence — most historical iterations are
scored on the signals that are present, and that is correct behaviour.

`token_efficiency` is the largest backfill: every entry carries a 100%-
populated `commit` SHA, so a commit-timestamp window over the Claude
(249 transcripts, 2026-04-22 on) and Codex (204 transcripts, 2026-02-28
on) session logs reconstructs token/cache telemetry the iteration log
never recorded.

## Changelog

- **2.0.0** — Signal set reworked after analysis of the salvaged data.
  `merge_back` (recorded in 1/65 entries) replaced by `landed`, scored
  from objective commit-survival. New `correction_pressure` signal.
  `verifier`, `tests`, and `scope` gained objective sources and two-way
  status. Introduced the integrity track. Weights rebalanced across six
  signals. Combination method unchanged.
- **1.0.0** — Initial rubric: five signals (`verifier`, `tests`,
  `merge_back`, `scope`, `token_efficiency`), weighted-mean-renormalized
  combination, score bands.

## Changing the rubric

A rubric change is a reviewable act. To change it:

1. Edit this document and `internal/scoring/rubric.go` **in the same
   commit** — they must never disagree.
2. Bump `RubricVersion` per the [versioning policy](#versioning-policy),
   and add a [changelog](#changelog) entry.
3. `internal/scoring` tests assert weights sum to 1.0, signal IDs are
   unique, and the version is pinned — they will fail until the change is
   internally consistent.
