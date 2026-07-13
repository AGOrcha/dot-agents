# Outcome-Scoring Rubric

**Status:** active
**Rubric version:** 3.0.0
**Owners:** dot-agents
**Go source:** [`internal/scoring/rubric.go`](../internal/scoring/rubric.go)
**Related:** [`agent-run-scoring-observability-platform.md`](../.agents/proposals/agent-run-scoring-observability-platform.md) (R1, the requirement this rubric serves); [ADR-0004](./adr/0004-execution-telemetry-schema-seed.md) (the execution-telemetry pillar the input signals come from); [`workflow-iter-log.schema.json`](../commands/workflow/static/workflow-iter-log.schema.json) (the iteration-log schema the signals are read from)

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

## Sources scored under this rubric

This rubric scores **two** kinds of iteration, under the same versioned
definition:

- **Production iterations** — real workflow iterations in
  `.agents/active/iteration-log/`, scored by `da score`.
- **Eval-source iterations** — iterations produced by `da eval run`, the R4
  agent-evaluation harness. Each eval run emits an R1-shaped `iter-1.yaml`
  and is scored with `scoring.DefaultRubric()` — the **same** rubric, same
  `RubricVersion`, no eval-special scoring path. The resulting
  `iter-1.score.yaml` sidecar is loadable by `da score iteration` and carries
  the same explainable breakdown as any production iteration.

An eval run leaves some objective signals **absent by construction** (its
sandbox worktree never lands on trunk, so `landed` is absent; it declares no
`write_scope`, so the objective half of `scope` is absent; v1 captures no
transcript window, so the objective process checks are absent). Absent is
first-class — the [renormalizing combination](#combination) drops those
signals from the vote — so an eval run is scored on the signals it does
carry, exactly as a production iteration with the same sparse telemetry would
be. See [`EVAL_HARNESS.md`](./EVAL_HARNESS.md) — in particular
[How eval outcomes feed R1](./EVAL_HARNESS.md#how-eval-outcomes-feed-r1) — for
the harness, the run-dir layout, and the `da score iteration --iter-log-dir`
invocation that reads an eval run.

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

### RubricVersion ordering for concurrent plans

When more than one in-flight plan changes `RubricVersion`, the two plans
must agree on the version ladder at task-execution time — they must not
both try to claim the same number. The rule, per R1.5 design D5
(`.agents/workflow/plans/r1-5-hook-enforcement-telemetry/design.md`):

1. **Read the current `RubricVersion` constant** in
   `internal/scoring/rubric.go` at the moment the rubric-bump task runs
   (not at plan-authoring time).
2. **Pick the next free version on top of the current value**, sized per
   the [versioning policy](#versioning-policy). R1.5 ships `hook_outcomes`
   as a *minor* bump (weights rebalanced, signal set widened by one —
   combination method unchanged). R5 (`r5-review-labeling-access`) ships
   `human_label` as a *major* bump because it introduces the first signal
   that depends on **external human input** rather than agent-run
   telemetry — a qualitatively new dependency surface.
3. **First to merge wins the bump it planned for; the second rebases.**
   - If the current value is `2.0.2` when R1.5's rubric-bump task runs,
     R1.5 targets `2.1.0`. R5, running later, sees `2.1.0` and targets
     `3.0.0`.
   - If R5 ships first and bumps to `3.0.0`, R1.5 then targets `3.1.0`
     (still a minor bump on top of R5's major).
4. **The shipped value (`2.1.0`) is the result of step 1 + step 2** —
   R1.5's rubric-bump task observed `2.0.2` at execution time and took
   the next free minor.
5. **The same rule applies to any future concurrent rubric change**:
   read the constant at execution, choose the next free minor (or major,
   per the criteria above), document the chosen target in the task notes,
   and coordinate cross-plan if more than one mutator is in flight.

The reason for the lookup-at-execution rule: a plan-authored target
("we will be 2.1.0") becomes stale the moment another plan rebases to a
new base. Reading the constant on the day the task ships keeps the
version ladder monotonic without requiring plans to predict each other,
and avoids a merge-conflict-by-version where two open plans both claim
the same number.

**R1.5 / R5 coordination (resolved).** The two plans that mutated
`RubricVersion` were `r1-5-hook-enforcement-telemetry` (minor —
`hook_outcomes`) and `r5-review-labeling-access` (major — `human_label`).
R1.5 merged first and observed `2.0.2` at execution, so it took the next
free minor, `2.1.0`. R5's `r1-integration` task then read `2.1.0` at
execution time and took the major bump to `3.0.0` (the shipped value);
no rebase of R1.5 was required. The plan-side resolution and rationale
live in R1.5 plan design, section `Q4 / D5 — RubricVersion ordering with
R5` (`.agents/workflow/plans/r1-5-hook-enforcement-telemetry/design.md`).
Any future concurrent rubric mutator follows the same five-step rule
above rather than hard-coding a target number here.

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

## Objective process-discipline checks

A first dogfood-driven evaluation of the iteration-log self-assessment
booleans (`iter-66`) found three of them rubber-stamped — `read_loop_state`,
`committed_after_tests`, and `ran_cli_command` were each true in ~98% of
historical entries, carrying essentially no information. Two more were
dead weight: `tdd_refresh_performed` was true in 0/22 entries, and
`stayed_under_10_files` enforced an arbitrary threshold that
`correction_pressure` already captures from real overload symptoms.

These five fields are **deprecated** in the iteration-log schema. The
three rubber-stamped facts are now computed *objectively* from the agent
transcripts in `internal/scoring/objective_checks.go`:

- `ranCliCommand` — was a Bash tool actually invoked in the iteration window
- `committedAfterTests` — did a test command run in the window before the commit
- `readLoopState` — was `loop-state.md` actually read in the window

These observations live on the assembled `SignalSet.Objective`. They
surface alongside the score as a record of what actually happened — but
because their self-report counterparts have been removed from the
schema, they do **not** enter the integrity track. There is nothing to
pair against.

Four self_assessment booleans survive the evaluation: `one_item_only`
(the only flag with real outcome lift, +0.23 on scope),
`aligned_with_canonical_tasks`, `no_destructive_commands`, and
`scoped_tests_to_write_scope`. They are **tri-stated** in the schema —
their type is `["boolean", "null"]`, so absent stays distinct from a
reported false, and the scorer stops treating "unmeasured" as "no."

## Structured claims: name the artifact, not the box

Two self-assessment booleans were genuinely informative *concepts* but
trivially rubber-stamped as flags — `tests_positive_and_negative` and
`linked_traces_to_outcomes`. Both are replaced with **named-list**
fields on the verifier block. A boolean is a box you can tick; a named
list is a set of references each of which must resolve.

- `verifier.tests_added_by_kind: [{name, kind}]` replaces
  `tests_positive_and_negative`. `kind` is one of
  `positive | negative | edge | regression`. The boolean is *derivable*
  ("≥1 positive AND ≥1 negative"); the value of the new field is that
  every `name` must exist in the diff.
- `verifier.linked_traces: [{trace_ref, outcome_ref}]` replaces
  `linked_traces_to_outcomes`. Each pair names a concrete verification
  trace and the outcome it links to (commit SHA, iteration, review-
  decision path); both refs must resolve.

Two other booleans are deprecated without replacements, because the
information was already structured elsewhere or carried no signal:
`exercised_new_scenario` (redundant with `verifier.scenario_tags`, which
is already a list) and `tests_used_sandbox` (`t.TempDir()` and equivalent
is universal Go test hygiene — the flag never discriminated anything).

The 66 salvaged iteration-log entries predate the new fields and parse
fine — the deprecated booleans remain in the schema (with `deprecated:
true`) so backward-compat holds; the extractor reads the structured
fields first and falls back to the legacy booleans.

## Input signals

Eight signals. Each is mapped to a **sub-score in `[0, 1]`** or is reported
**absent** when the telemetry to compute it was never captured. Absent is
a first-class state — see [Combination](#combination). Sub-score
extraction itself is the `signals` and `scorer` tasks; this section is
the contract those tasks implement.

### 1. `landed` — Landed on master (weight 0.17, two-way)

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

### 2. `verifier` — Verifier results (weight 0.15, two-way)

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

### 3. `tests` — Test outcomes (weight 0.14, two-way)

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

### 4. `human_label` — Human review label (weight 0.15, R5)

Structured reviewer judgement attached to the iteration. Human labels
are the first signal sourced from external human input rather than
agent-run telemetry — the reason 3.0.0 is a major bump. The label model
and sidecar persistence live in
[`internal/review/labels`](../internal/review/labels/label.go) (the R5
label store, which the R5 label-collection endpoints write through); the
extractor lives in `internal/scoring/signal_human_label.go`.

- **Source:** the `iter-N.labels.yaml` sidecar adjacent to `iter-N.yaml`
  in the iteration-log directory (R5 spec D5.1). Each label carries an
  enum-bounded structured judgement (spec D5.7): `correctness` (0–3),
  `scope_judgement` (`on-target` | `partial` | `breach`), and
  `hallucination` (`none` | `minor` | `major`), plus free text.
- **Per-label mapping (spec D5.7):** sub-score = mean of the three
  normalized dimensions — `correctness / 3`; `scope_judgement`
  `on-target → 1.0`, `partial → 0.5`, `breach → 0.0`; `hallucination`
  `none → 1.0`, `minor → 0.5`, `major → 0.0`. The `free_text` field is
  surfaced to humans but never affects the score.
- **Aggregation (spec D5.8 + OQ2):** labels are append-on-edit; a
  label's effective judgement is its latest edit. When one reviewer
  holds several labels, only their most recently updated label counts
  (latest-per-reviewer); the signal is the **mean** across reviewers'
  latest labels. An admin's *own* label marked `admin_override`
  supersedes the reviewer mean entirely; an admin *edit* of a reviewer's
  label remains attributed to the reviewer (the audit log captures who
  edited) and participates as that reviewer's latest state.
- **Schema versioning (spec OQ3):** every label and sidecar records the
  label-schema version, persisted as the `schema_version` field of
  `iter-N.labels.yaml`. The extractor scores schema major 1 only; a
  sidecar or label at any other major degrades to absent rather than
  silently misreading a future field layout.
- **Absent when:** no sidecar exists, the sidecar holds no labels, it is
  unreadable or fails validation, or its schema major is unsupported.
  Absent drops the signal from the vote per the renormalizing
  combination — an unreviewed iteration neither gains nor loses score,
  so shipping this signal changes nothing for label-less iterations.
- **One-way (human-sourced):** the label *is* the judgement; there is no
  self-report counterpart, so this signal does not feed the integrity
  track.
- **Why 0.15:** human judgement is the rubric's only direct measure of
  "did the score get this run right," so it carries more weight than any
  process signal — but less than the combined objective correctness
  signals, so a label refines rather than overrides the telemetry.

### 5. `correction_pressure` — Correction pressure (weight 0.11)

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

### 6. `scope` — Scope adherence (weight 0.11, two-way)

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

### 7. `hook_outcomes` — Hook-gate outcomes (weight 0.09, R1.5)

Did the iteration's hook gates allow, advise, or remediate. Objective
evidence from the per-iteration sidecar
`.agents/active/iteration-log/iter-N.hook-outcomes.yaml` written by
`da workflow hook-outcome write` (R1.5 t1, PR #91); the extractor lives
in [`internal/scoring/signal_hook_outcomes.go`](../internal/scoring/signal_hook_outcomes.go).

- **Source:** `iter-N.hook-outcomes.yaml` records whose
  `intervention_class` is `prevent_before_action` or `remediate_at_stop`.
- **Mapping (per R1.5 design D3):**
  - any collapsed record at `remediate` → sub-score = `0.0`
  - all collapsed records at `advise`, no `remediate` → sub-score = `0.6`
  - all collapsed records at `allow`, no `advise` or `remediate` →
    sub-score = `1.0`
- **D4 dedup:** a `prevent_before_action` and a `remediate_at_stop` record
  sharing the same `correlation_id` and `rule_id` collapse to one record
  at the more severe result (remediate > advise > allow). The persisted
  sidecar keeps both rows (audit value); the scorer collapses them in
  memory so a prevented-then-remediated intent does not double-count.
- **Absent when:** no sidecar exists for the iteration, the sidecar is
  unreadable / malformed, or it contains no in-scope records. Absent
  drops the signal from the vote per the renormalizing combination — an
  iteration with no hook activity neither inflates nor deflates the
  score.
- **One-way (objective only):** the gate is the objective source; there
  is no self-report counterpart, so this signal does not contribute to
  the integrity track.
- **Per-rule contribution:** the rule IDs that drove the band are
  rendered in the signal's `Detail` so the explainable breakdown names
  which gates fired (e.g. `remediate: iteration-close.R1.1`).

#### Approved rules feeding the v1 sub-score (per R1.5 design D6)

Only rule IDs in the following families, when they emit a verifiable
outcome at terminal time, currently contribute to the sub-score:

- **Acceptance-criteria adherence** — `iteration-close.R1.1` /
  `iteration-close.R1.2` (declared expected artifacts present;
  verify-record exists).
- **Scope respect** — `loop-worker.R3.1` (write-scope adherence),
  `loop-worker.R3.3` (loop-state.md untouched).
- **Closeout discipline** — `iteration-close.R1.3` (no merge-back on
  rejected self-review), `loop-worker.R3.4` (merge-back artifact
  exists), `delegation-closeout.R4.1`–`R4.3` (history archive valid).
- **Orchestration boundary** — `iteration-close.R1.8`, `loop-worker.R3.9`,
  `orchestrator-handoff.R3.1`–`R3.3` (forbidden workflow-command
  prevention; bundle/sentinel agreement).

Records persisted but **excluded** from the v1 sub-score (deferred to
R1.5.1 — see the [post-tool observation evaluation](#post-tool-observation-evaluation-r15-t1b)
section below):

- Soft advisories tied to unverified traces (`*.coverage-advisory`
  rule_ids) — they record absence of evidence, not failure.
- All `continuity_advice` records (`pre_compact`, per D4 — observational
  only; deferred to R1.5.1 per t1b PR #97).
- All `observe_tool_result` records (post-tool, per t1b PR #97 — the
  four boundary criteria in R1.5 spec R3.2 did not all clear v1).

The extractor enforces these exclusions in code
(`filterScoredHookOutcomes` in `signal_hook_outcomes.go`): any future
addition of a class to the sub-score is a deliberate edit, not silent
inclusion.

### 8. `token_efficiency` — Token & cache efficiency (weight 0.08)

How efficiently the iteration used the model.

- **Source:** `session_tokens.cache_hit_rate` in the iteration log;
  backfilled from Claude and Codex transcripts where the iteration log
  itself never captured it (see the data note).
- **Mapping:** sub-score = `cache_hit_rate` directly (already `[0, 1]`).
- **Absent when:** no token telemetry exists and none can be backfilled.
- **Why the lowest weight:** this is an efficiency metric, not a
  correctness one. A correct-but-expensive run should still score well.

### Weight summary

| Signal                | Weight | Kind        | Two-way | Notes                                |
|-----------------------|-------:|-------------|:-------:|--------------------------------------|
| `landed`              |   0.17 | correctness | yes     |                                      |
| `verifier`            |   0.15 | correctness | yes     |                                      |
| `tests`               |   0.14 | correctness | yes     |                                      |
| `human_label`         |   0.15 | human judgement | no  | added at 3.0.0 (R5); mean of latest-per-reviewer labels, admin override supersedes — see [`human_label`](#4-human_label--human-review-label-weight-015-r5) |
| `correction_pressure` |   0.11 | process     | no      |                                      |
| `scope`               |   0.11 | process     | yes     |                                      |
| `hook_outcomes`       |   0.09 | process     | no      | added at 2.1.0 (R1.5); scores `prevent_before_action` + `remediate_at_stop` only — see [approved rules](#approved-rules-feeding-the-v1-sub-score-per-r15-design-d6) and [post-tool deferral](#post-tool-observation-evaluation-r15-t1b) |
| `token_efficiency`    |   0.08 | efficiency  | no      |                                      |
| **Total**             | **1.00** |          |         |                                      |

Objective correctness signals total 0.46; the human-judgement signal
carries 0.15; process signals total 0.31; efficiency 0.08. The
2.1.0 → 3.0.0 rebalance introduces `human_label` at 0.15 and scales the
seven existing weights proportionally (×0.85, rounded to two decimals),
so the relative shape among the telemetry signals is preserved:
objective correctness still dominates, and correctness plus human
judgement together (0.61) outweigh everything else. The weighting is
deliberate: a run is scored first on whether it worked and landed, and
a human label refines — rather than overrides — that telemetry.

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

**A clean iteration, no token telemetry, no hook activity, unreviewed.**
Landed on master, verifier passed, tests passed, no corrections, scope
on-target; the entry predates `session_tokens` and no backfill was
possible, so `token_efficiency` is absent; no hook sentinel was active
so `hook_outcomes` is absent; no reviewer has labeled the iteration so
`human_label` is absent too.

| Signal                | Present | Sub-score | Weight | Eff. weight | Contribution |
|-----------------------|---------|----------:|-------:|------------:|-------------:|
| `landed`              | yes     | 1.00      | 0.17   | 0.250       | 0.250        |
| `verifier`            | yes     | 1.00      | 0.15   | 0.221       | 0.221        |
| `tests`               | yes     | 1.00      | 0.14   | 0.206       | 0.206        |
| `human_label`         | no      | —         | 0.15   | —           | —            |
| `correction_pressure` | yes     | 1.00      | 0.11   | 0.162       | 0.162        |
| `scope`               | yes     | 1.00      | 0.11   | 0.162       | 0.162        |
| `hook_outcomes`       | no      | —         | 0.09   | —           | —            |
| `token_efficiency`    | no      | —         | 0.08   | —           | —            |

Present weights sum to 0.68; `score = 0.68 / 0.68 = 1.00` → **excellent**.

**A struggling iteration with a remediation.** Did not land, verifier
failed, tests failed, three retries, scope partial, cache hit rate 0.60,
no human label yet, and the iteration-close gate remediated for a
missing verification artifact (`iteration-close.R1.1` → result
`remediate`).

| Signal                | Present | Sub-score | Weight | Eff. weight | Contribution |
|-----------------------|---------|----------:|-------:|------------:|-------------:|
| `landed`              | yes     | 0.00      | 0.17   | 0.200       | 0.000        |
| `verifier`            | yes     | 0.00      | 0.15   | 0.176       | 0.000        |
| `tests`               | yes     | 0.00      | 0.14   | 0.165       | 0.000        |
| `human_label`         | no      | —         | 0.15   | —           | —            |
| `correction_pressure` | yes     | 0.25      | 0.11   | 0.129       | 0.032        |
| `scope`               | yes     | 0.50      | 0.11   | 0.129       | 0.065        |
| `hook_outcomes`       | yes     | 0.00      | 0.09   | 0.106       | 0.000        |
| `token_efficiency`    | yes     | 0.60      | 0.08   | 0.094       | 0.056        |

Present weights sum to 0.85; `score ≈ 0.154` → **poor**. The
`hook_outcomes = 0.00` row makes the gate remediation visible in the
explainable breakdown without changing the overall band the existing
correctness failures already earned.

**Two reviewers and an admin override.** Same struggling iteration,
later reviewed: alice's latest label scores 1.00 and bob's scores 0.50,
so the reviewer mean would be 0.75 — but an admin submitted an
`admin_override` label scoring 0.00 after investigating, so the
`human_label` sub-score is 0.00 (spec OQ2: the admin's own label
supersedes the reviewer mean entirely). All eight signals are now
present, weights sum to 1.00, and the breakdown's `human_label` row
names the admin in its detail string.

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

## Hook-outcome sidecar retention (R1.5)

The R1.5 plan (`r1-5-hook-enforcement-telemetry`) introduces a new
per-iteration sidecar `.agents/active/iteration-log/iter-N.hook-outcomes.yaml`
that the `hook_outcomes` signal extractor reads
([`signal_hook_outcomes.go`](../internal/scoring/signal_hook_outcomes.go),
shipped at RubricVersion 2.1.0). The retention policy for these sidecars
— locked in by the `t-archival-policy` task — is **indefinite retention,
no automatic pruning**, mirroring R5's audit log policy
(`specs/r5-review-labeling-access/design.md` D5.4).

The rule is identical to R5's reasoning applied to hook outcomes: a
re-score under a future `RubricVersion` must read the original sidecar to
re-derive the signal explainably. Auto-pruning at any age would silently
convert "rescored under new rubric" into "absent signal does not vote" —
the failure mode is invisible in the output and fatal to longitudinal
comparison. The sidecars carry no transcript content (per R1.5 spec D2)
and the per-iteration footprint is in the kilobytes, so neither privacy
nor disk pressure earns automatic expiry.

An admin-only manual pruning command spec is reserved for operator-driven
compaction:

```
da workflow hook-outcome prune --before <YYYY-MM-DD> [--dry-run] [--yes]
```

Implementation is deferred until an operator request emerges. When it
ships, the command will gate on the same R5 admin-role mechanism that
gates `da review audit prune`, will write one record per prune action to
R5's tamper-evident audit chain (`.agents/active/review/audit.log.jsonl`),
will never auto-run on a timer or hook, and will touch hook-outcome
sidecars only — not iteration records, score sidecars, or sentinel
history.

The full design (alternatives rejected, command behavior contract,
audit-trail integration) lives in the resolving plan-side decision
record at
[`r1-5-hook-enforcement-telemetry/design.md`](../.agents/workflow/plans/r1-5-hook-enforcement-telemetry/design.md)
under "Q3 — Hook-outcome sidecar retention and archival policy".

The new-signal spec and weight rebalance table live in the
[`hook_outcomes` section above](#7-hook_outcomes--hook-gate-outcomes-weight-009-r15)
and the [weight summary](#weight-summary); the cross-plan version
coordination rule lives in
[RubricVersion ordering for concurrent plans](#rubricversion-ordering-for-concurrent-plans);
the full approved-rule list lives in
[Approved rules feeding the v1 sub-score](#approved-rules-feeding-the-v1-sub-score-per-r15-design-d6).
This section is authoritative on retention only.

## Post-tool observation evaluation (R1.5 T1b)

The R1.5 plan's `t1b-post-tool-observation-evaluation` task evaluates
whether `PostToolUse` and `PostToolUseFailure` hook events (mapped today
per the upstream `loop-discipline-stop-hooks` D9 as observation
candidates, not gates) qualify as an objective signal feeding the
upcoming `hook_outcomes` sub-score.

**Decision: deferred to R1.5.1. Post-tool observation is NOT admitted
to v1 scoring.** The `hook_outcomes` signal shipped at RubricVersion
2.1.0 is fed **only** by terminal-gate (`remediate_at_stop`) and
pre-action (`prevent_before_action`) records; `pre_compact`
continuity-advice records remain observational per the spec D4. The
post-tool surface emits no `iter-N.hook-outcomes.yaml` records that
score under v1 and contributes nothing to the sub-score. The deferral
is enforced in code by `filterScoredHookOutcomes` in
[`internal/scoring/signal_hook_outcomes.go`](../internal/scoring/signal_hook_outcomes.go),
which drops both `continuity_advice` and `observe_tool_result` records
before sub-score folding so a future schema addition cannot vote silently.

The four boundary criteria the R1.5 spec R3.2 requires resolved
**before** any post-tool observation can contribute to scoring did not
all clear v1:

| Criterion | v1 status |
|---|---|
| Vendor payload stability across Claude, Codex, Copilot, Cursor | Partial — event names mapped on all four platforms; payload field shapes not pinned by golden fixtures, and Codex lacks a dedicated `PostToolUseFailure` event (success/failure conflated in `PostToolUse`). |
| Workflow-command filter regex with named approved commands | Reserved but not enabled — filter requires payload-field stability first. |
| Redaction strategy for failure messages | Not solvable in v1 without either expanding `gate.sh` scope with a new classifier or relaxing the D2 disallowed-fields contract (`stderr`, `tool_output`, `failure_message`, free-text). Required form when reopened: bounded enum `{exit_nonzero, timeout_exceeded, permission_denied, vendor_error, unknown}`. |
| Deduplication against terminal remediation for the same workflow command | Safe (no false double-count: successful post-tool records do not overlap with `remediate_at_stop` records) but low-value — the marginal signal over the existing "no remediate ⇒ allow" path is near zero for successful commands; failure records depend on the prior three criteria. |
| Noise-budget cap | Reserved for R1.5.1: max 20 post-tool records per `iter-N.hook-outcomes.yaml` with silent back-pressure beyond the cap and one stderr advisory. |

The deferral preserves the spec Boundary clause — "a post-tool
observation must not be counted separately when it merely records the
same prevention or terminal remediation outcome" — by ensuring no
post-tool record contributes to scoring until both attribution and
redaction are settled.

The full assessment (criteria, evidence, rejected alternatives, and
the bounded R1.5.1 reopen path) lives in the plan-side decision record
at
[`.agents/history/r1-5-hook-enforcement-telemetry/post-tool-observation-assessment.md`](../.agents/history/r1-5-hook-enforcement-telemetry/post-tool-observation-assessment.md).
The shipped extractor (`signal_hook_outcomes.go` at RubricVersion 2.1.0)
implements this deferral by construction; the next ship of post-tool
observation MUST land the C1–C4 follow-up work documented in the
assessment doc before a single post-tool record enters the sub-score —
the code change required is a single new case in
`filterScoredHookOutcomes` and a matching rubric-version bump.

## Changelog

- **3.0.0** — Adds the `human_label` signal (weight 0.15, one-way,
  human-sourced): structured reviewer judgement read from the R5
  `iter-N.labels.yaml` sidecar (`internal/review/labels`). Major bump —
  the first signal that depends on external human input rather than
  agent-run telemetry widens the signal set's dependency surface
  (see the [ordering policy](#rubricversion-ordering-for-concurrent-plans);
  the constant read at execution time was 2.1.0, so R5 took the planned
  major). Per-label sub-score = mean of the three normalized structured
  dimensions (correctness / scope_judgement / hallucination, spec D5.7);
  aggregation = mean of latest-per-reviewer with admin-override
  precedence (spec D5.8 + OQ2); labels at an unsupported label-schema
  major (the sidecar's `schema_version` field) degrade to absent (OQ3).
  The seven
  existing weights scale proportionally (×0.85, rounded): `landed`
  0.20→0.17, `verifier` 0.18→0.15, `tests` 0.17→0.14,
  `correction_pressure` 0.13→0.11, `scope` 0.13→0.11, `hook_outcomes`
  0.10→0.09, `token_efficiency` 0.09→0.08. Combination method unchanged
  (`weighted_mean_renormalized`); an iteration with no labels scores
  exactly as it would without the signal (absent does not vote), and
  scores persisted under 2.1.0 remain valid under their recorded
  version. The Go `SignalSet` struct gains a `HumanLabel` field and
  `SignalSet.Value` a new switch arm; the extractor lives in
  `signal_human_label.go`.
- **2.1.0** — Adds the `hook_outcomes` signal (weight 0.10, one-way) and
  rebalances every other weight proportionally (`landed` 0.22→0.20,
  `verifier` 0.20→0.18, `tests` 0.18→0.17, `correction_pressure`
  0.15→0.13, `scope` 0.15→0.13, `token_efficiency` 0.10→0.09). Source is
  the R1.5 per-iteration sidecar `iter-N.hook-outcomes.yaml` written by
  `da workflow hook-outcome write` (PR #91). The signal scores only
  `prevent_before_action` and `remediate_at_stop` records per t1b PR #97;
  `continuity_advice` (pre_compact) and `observe_tool_result` (post-tool)
  remain in the sidecar as audit-only observations and are deferred to
  R1.5.1 — see the [post-tool observation evaluation](#post-tool-observation-evaluation-r15-t1b)
  section. Pre-action and terminal-remediation records sharing the same
  `(correlation_id, rule_id)` collapse to one record at the more severe
  result per R1.5 design D4. Combination method unchanged
  (`weighted_mean_renormalized`); existing signal IDs are stable; the
  Go `SignalSet` struct gains a `HookOutcomes` field and `SignalSet.Value`
  gains a new switch arm — minor bump per the policy.
- **2.0.2** — Documents the structured-claims layer: the
  `tests_added_by_kind` and `linked_traces` named-list fields replacing
  two rubber-stamped booleans, and the schema deprecation of two more
  (`exercised_new_scenario`, `tests_used_sandbox`). Signal set, weights,
  and combination unchanged — scores remain comparable with 2.0.x.
- **2.0.1** — Documents the objective process-discipline checks layer and
  the iteration-log self_assessment deprecations from the first
  dogfood-driven boolean-effectiveness evaluation. Signal set, weights,
  and combination unchanged — scores remain comparable with 2.0.0.
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
