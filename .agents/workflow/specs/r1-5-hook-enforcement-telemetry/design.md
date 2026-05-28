# R1.5 Hook Enforcement Telemetry - Design

- spec-id: `r1-5-hook-enforcement-telemetry`
- status: active
- date: 2026-05-25 (expanded 2026-05-25 via Plan-agent review pass)
- predecessor: `r1-outcome-scoring` (completed; ships RubricVersion 2.0.2)
- producer dependency: `loop-discipline-stop-hooks` (D5 sentinel protocol, R5 two-tier output, D8 non-terminal enforcement, D9 post-tool observation candidate)
- adjacent producer: `orchestration-companion-stop-hooks` (D5 mandates emitting R1.5-compatible outcomes when this plan has shipped)
- coordinated consumer: `r5-review-labeling-access` (also bumps RubricVersion; see decision D5 below)
- umbrella spec: `agent-run-scoring-observability-platform`

## Problem

`r1-outcome-scoring` shipped RubricVersion 2.0.2 with six signals scored from already-captured telemetry (`internal/scoring/{rubric,signals,scorer,persist}.go`). The shipped rubric treats process-discipline checks that have no self-reported counterpart (`ran_cli_command`, `read_loop_state`, `committed_after_tests`) as observational facts via `IterationObjectives` — present in the persisted sidecar, never in the numeric score.

The `loop-discipline-stop-hooks` plan introduces a new, *first-class* objective evidence source that the shipped rubric cannot see:

1. **Terminal gate outcomes** at Stop / SubagentStop — `allow`, `advise`, or `remediate` per skill invocation, anchored by an archived sentinel under `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/`.
2. **Pre-action prevention** (`PreToolUse`) — a hard-remediated attempt at a deterministically forbidden workflow command (`workflow advance` in a delegated closeout, `workflow advance|orient|next|status` in a loop-worker subagent).
3. **Continuity advice** (`PreCompact`) — non-blocking advisory text on an active sentinel; not enforcement, but a recorded fact about discipline pressure.
4. **Post-tool observation** (`PostToolUse` / `PostToolUseFailure`) — candidate workflow-command success/failure feedback, deferred to D9 of the upstream plan, owned by this plan.

The orchestration-companion plan adds two more gate surfaces (`orchestrator-handoff-gate`, `delegation-closeout-gate`) whose outcomes share the same vocabulary.

Leaving these signals uncaptured discards the highest-quality discipline evidence the platform now produces. Persisting them privately under `internal/scaffold/hooks/global/*/` without a versioned contract risks each gate inventing its own outcome shape.

The gap R1.5 fills:

- a versioned, durable hook-outcome record linked to archived sentinels;
- a deduplication contract preventing prevention + remediation double-counting;
- a deliberate decision on whether this evidence becomes a **new scoring signal** (rubric bump) or an additional **observational fact track** (no score impact);
- coordinated sequencing with R5's RubricVersion 3.0.0 bump so the two rubric mutations land as a single semantic-version transition or as two cleanly-ordered minor bumps;
- CLI readback so a reviewer can see, for a given iteration, which gate outcomes contributed and why.

This plan is the bridge layer between R1 (shipped scoring) and R5 (human labels). R2/R3 (dashboard / service) read the persisted artifacts this plan writes; they do not constrain its on-disk shape beyond what the existing iteration-log sidecars already establish.

## Decisions

### D1. Persistence shape: sidecar adjacent to `iter-N.score.yaml`, not inside it

**Decision.** Hook outcomes for an iteration are persisted at `.agents/active/iteration-log/iter-N.hook-outcomes.yaml`, a new sidecar alongside `iter-N.yaml` (canonical iteration record) and `iter-N.score.yaml` (R1 score). The file is an ordered list of `HookOutcome` records, one per evaluated sentinel-anchored hook invocation in the iteration window. A successful sentinel produces exactly one terminal record; a prevention event produces one pre-action record; they are paired by a shared `sentinel_id`.

**Rationale.** Mirrors R1's `iter-N.score.yaml` pattern (`internal/scoring/persist.go`) and the broader iteration-log convention (`internal/scoring/iterlog.go`). Keeps R2/R3/R5 reads uniform: every per-iteration artifact is `iter-N.<facet>.yaml`. Avoids mutating the shipped `iter-N.score.yaml` schema mid-plan and prevents an R1 sidecar read from blocking on hook-outcome aggregation.

> Convergence target: the `HookOutcome` / sentinel-anchored record shape is one of
> the surfaces meant to adopt the generic registry-driven envelope in
> `[[unified-pluggable-event-contract]]` (new hook-outcome/sentinel kinds become
> registry entries, not per-type sidecar-schema edits).

**Rejected.**
- *Embed inside `iter-N.score.yaml`.* Couples R1.5 schema to R1's shipped schema; any future hook addition would touch the shipped score sidecar shape. Worse for backward compat with sessions scored under RubricVersion 2.0.2.
- *Single global ledger* (`.agents/active/iteration-log/hook-outcomes.jsonl`). Looks attractive but breaks the iteration-keyed read pattern; CLI rendering would have to scan a growing global file per iteration query.
- *Inside the archived sentinel directory* (`history/.../hook-sentinels/`). History is for the proof artifact; placing scoring inputs there mixes two concerns (audit vs. signal) and forces the scorer to walk history archives.

### D2. Outcome schema is versioned and registered alongside existing workflow schemas

**Decision.** The schema lives at `schemas/workflow-hook-outcome.schema.json` and its bundled twin at `commands/workflow/static/workflow-hook-outcome.schema.json`, registered in `commands/workflow/static/schemas.go` alongside the existing `workflow-iter-log`, `workflow-delegation-bundle`, etc. Top-level field `schema_version: 1`.

Required fields per record:

- `schema_version: int` (= 1 in v1)
- `sentinel_id: string` (`<skill>-<run-id>`, joins to archived sentinel)
- `skill: enum { iteration-close | isp | loop-worker | orchestrator-session-start | delegation-lifecycle }`
- `lifecycle_point: enum { pre_tool_use | stop | subagent_stop | subagent_start | pre_compact | post_tool_use | post_tool_use_failure }`
- `intervention_class: enum { prevent_before_action | remediate_at_stop | continuity_advice | observe_tool_result }`
- `result: enum { allow | advise | remediate }`
- `rule_id: string` (e.g. `iteration-close.R1.1`, `loop-worker.R3.1`)
- `platform: enum { claude | codex | copilot | cursor }`
- `ts: RFC3339 timestamp`
- `archived_sentinel_path: string` (POSIX, relative to repo root) — empty for `pre_tool_use` records written before the sentinel archives
- `correlation_id: string` (groups pre+terminal records for the same intent; defaults to `sentinel_id` when single)

Disallowed fields (boundary): `transcript_excerpt`, `tool_input`, `tool_output`, `stdout`, `stderr`, `command_args`, `failure_message` beyond a bounded enum classification.

**Rationale.** Reuses the project's existing schema-bundling mechanism so validators run in the same pipeline as workflow YAML. Splitting `lifecycle_point` from `intervention_class` resolves the "one event, many classifications" trap (a `Stop` event can carry both a `remediate_at_stop` and a `continuity_advice` record only if they are distinct rules; never two records for the same `(sentinel_id, rule_id, intervention_class)`).

### D3. R1.5 *bumps the rubric to 2.1.0* and adds a new `hook_outcomes` signal — minor bump, not major

**Decision.** R1.5 adds one new `SignalID = "hook_outcomes"` (`internal/scoring/rubric.go`), weight 0.10, sub-score derived from the aggregated hook-outcome record for the iteration:

- All `remediate` rules in the iteration ⇒ sub-score = 0.0
- All `advise` rules, no `remediate` ⇒ sub-score = 0.6
- All `allow` (no advise, no remediate) ⇒ sub-score = 1.0
- Absent if no sentinel was active in the iteration window (preserves the "absent does not vote" invariant from `internal/scoring/scorer.go`).
- `TwoWay: false` (the gate is the objective source; there is no self-report counterpart).

To keep the existing weights summing to 1.0, the rubric re-balances: landed 0.20 (was 0.22), verifier 0.18 (was 0.20), tests 0.17 (was 0.18), correction_pressure 0.13 (was 0.15), scope 0.13 (was 0.15), token_efficiency 0.09 (was 0.10), hook_outcomes 0.10 (new). The correctness triple (landed + verifier + tests = 0.55) still dominates; process (correction + scope + hook = 0.36) is the meaningful secondary; efficiency (0.09) is unchanged in relative position.

This is a **minor** bump (`2.0.2 → 2.1.0`) per the policy in `internal/scoring/rubric.go` lines 18–22: "minor: a weight or sub-score mapping changed." Adding one signal while keeping the combination method and `SignalSet` interface compatible (`SignalSet.Value` gains a new switch arm) is the canonical minor-bump case.

**Rationale.** A separate observational track (the `IterationObjectives` pattern) would not feed the score, defeating the plan's "incorporate approved objective signals into explainable scoring" success criterion. A major bump (`3.0.0`) is reserved for changing the combination method or removing/renaming signals; R1.5 does neither.

**Rejected.**
- *Add as `IterationObjectives` only.* Reasonable safety position, but the success criterion explicitly wants hook outcomes in the score. Demoted to a fallback if T1b's post-tool observation evaluation forces caution.
- *Major bump to 3.0.0.* Forces R5 to wait or rebase its planned 3.0.0 bump; loses cheap forward-compatibility for old sidecars.
- *Reweight only correctness signals to make room.* Process discipline is exactly what the hook signal measures; re-weighting only efficiency would distort intent.

### D4. Pre-action prevention and terminal remediation deduplicate by `correlation_id`

**Decision.** When a `pre_tool_use` record with `intervention_class = prevent_before_action` and a `stop`/`subagent_stop` record with `intervention_class = remediate_at_stop` share the same `correlation_id` and `rule_id`, the scorer counts them as **one** outcome at severity `remediate` (the more severe). The persisted file keeps both records (audit value) but `internal/scoring/signal_hook_outcomes.go` collapses them before sub-scoring.

A `continuity_advice` record from `pre_compact` is **never** counted as an enforcement outcome for scoring; it remains in the persisted file as an observational fact and surfaces in CLI readback only.

A `post_tool_use` / `post_tool_use_failure` record is **not scored** in R1.5 v1. T1b decides whether a future R1.5.1 admits it as a fourth intervention class with its own deduplication rule against terminal outcomes for the same workflow command.

**Rationale.** Honors the boundary clause in the existing spec ("a post-tool observation must not be counted separately when it merely records the same prevention or terminal remediation outcome").

### D5. Coordinate RubricVersion with R5: R1.5 lands first as 2.1.0, R5 then bumps to 3.0.0

**Decision.** This plan ships first and bumps `RubricVersion = "2.1.0"` with the `hook_outcomes` signal. R5 (`r5-review-labeling-access` task `r1-integration`) then bumps to `3.0.0` when adding the `human_label` signal, treating R5's change as the major bump because it introduces the first signal that depends on **external human input** rather than agent-run telemetry — a qualitatively new dependency surface deserving a major version increment.

R5's `design.md` explicitly says "RubricVersion 3.0.0 bump"; this plan must not also bump to 3.0.0, so the two plans agree on the version ladder.

If R5 ships before R1.5: R5 bumps to 3.0.0 with its new signal; R1.5 then ships as 3.1.0 with the hook signal. The decision is **ordering-flexible** but never collides on a single version. The implementation task `t2b-rubric-bump` reads the current `RubricVersion` constant at execution time and chooses 2.1.0 or 3.1.0 accordingly.

**Rationale.** Avoids a merge-conflict-by-version where two open plans both try to be 3.0.0. Records the policy in the rubric godoc so any future plan that bumps the rubric has explicit precedent.

**Rejected.**
- *R1.5 jumps to 3.0.0; R5 to 4.0.0.* Burns major versions for what are individually minor changes.
- *Single combined bump.* Couples two otherwise-independent shipping paths.

### D6. Approved objective signals (the "which signals" question)

**Decision.** The hook_outcomes signal aggregates, but the per-`rule_id` contribution is preserved in the persisted breakdown so explainable readback names which rules fired. Approved rules feeding the v1 sub-score are the rule_ids from `loop-discipline-stop-hooks` R1–R3 and `orchestration-companion-stop-hooks` R3–R4 that produce a verifiable outcome at terminal time:

- **Acceptance-criteria adherence** — surfaced via `iteration-close.R1.1`/`R1.2` (declared expected artifacts present; verify-record exists).
- **Scope respect** — `loop-worker.R3.1` (write-scope adherence), `loop-worker.R3.3` (loop-state.md untouched).
- **Closeout discipline** — `iteration-close.R1.3` (no merge-back on rejected self-review), `loop-worker.R3.4` (merge-back artifact exists), `delegation-closeout.R4.1`–`R4.3` (history archive valid).
- **Orchestration boundary** — `iteration-close.R1.8`, `loop-worker.R3.9`, `orchestrator-handoff.R3.1`–`R3.3` (forbidden workflow-command prevention; bundle/sentinel agreement).

Excluded from v1 sub-score (still persisted as observations):
- Soft advisories tied to unverified traces (`*.coverage-advisory` rule_ids) — they record absence of evidence, not failure.
- All `continuity_advice` records (per D4).
- All `post_tool_use*` records (per T1b).

**Rationale.** A signal that fires on advisory-only outcomes would penalize iterations for the *evidence-availability* problem the upstream plan already chose not to escalate. The included rule set is exactly those that the upstream gates would hard-remediate.

### D7. CLI readback: hook outcomes appear in `da score iteration N` with rule-level detail

**Decision.** `da score iteration N` renders a new section after the existing breakdown:

```
Hook outcomes (sentinel: iteration-close-<run>):
  iteration-close.R1.1  remediate  stop          claude   "missing verify-record"
  loop-worker.R3.1      remediate  subagent_stop codex    "scope escape: docs/X.md"
  iteration-close.R1.4  advise     stop          claude   "trace not available"
```

No transcript fragments, no command bodies. `--json` output includes a `hook_outcomes: [...]` array.

`da score iteration N --recompute` re-reads the `iter-N.hook-outcomes.yaml` sidecar and the archived sentinels, so a post-hoc gate edit (rare, audit-only) can be re-scored against a known RubricVersion.

## Requirements

### R1. Outcome contract

- R1.1 The schema MUST be registered in `commands/workflow/static/schemas.go` and validated by the same validator path as `workflow-iter-log`.
- R1.2 The schema MUST reject records that include any disallowed field listed in D2.
- R1.3 The schema MUST require `lifecycle_point` and `intervention_class` together; neither field alone is a valid classification.

### R2. Persistence

- R2.1 Gates MUST append to the iteration's `iter-N.hook-outcomes.yaml` via the same CLI primitive (a new `da workflow hook-outcome write` subcommand) — never by direct file write from `gate.sh`.
- R2.2 The CLI MUST resolve the current iteration N from `.agents/active/loop-state.md` (existing pattern in `commands/workflow/iter_log.go`); if no active iteration exists, the write MUST exit 0 silently with an stderr advisory (sentinel was active but iteration log was not, treated as a session-only outcome and dropped from scoring).
- R2.3 The write MUST be append-only; existing records MUST NOT be rewritten in-place. Append idempotency is keyed on `(sentinel_id, rule_id, lifecycle_point, intervention_class)` so a re-run of a gate during a recoverable platform retry does not inflate the record list.
- R2.4 The write MUST be bounded by the upstream `timeout_ms: 8000` hook budget (R5.4 in `loop-discipline-stop-hooks`).

### R3. Post-tool observation evaluation (defers to T1b)

- R3.1 Until T1b approves a stable contract, `post_tool_use` and `post_tool_use_failure` events MUST NOT produce `iter-N.hook-outcomes.yaml` records.
- R3.2 T1b's assessment document (`.agents/history/r1-5-hook-enforcement-telemetry/post-tool-observation-assessment.md`) MUST resolve: (a) per-platform payload stability, (b) workflow-command filter regex with named approved commands, (c) redaction strategy for failure messages, (d) deduplication against terminal remediation for the same workflow command and run, and (e) a "noise budget" cap (max records per iteration before back-pressure).

### R4. Scoring integration

- R4.1 A new `internal/scoring/signal_hook_outcomes.go` extractor MUST read the sidecar, apply D4 dedup, and emit `SignalValue` per iteration.
- R4.2 `internal/scoring/signals.go` `AssembleSignalSet`, the `SignalSet` struct, and `SignalSet.Value` MUST gain the new signal.
- R4.3 `internal/scoring/rubric.go` MUST update `DefaultRubric()` weights per D3, add `SignalHookOutcomes` to the `SignalID` constants, and bump `RubricVersion` per D5 (read current value at task time).
- R4.4 `internal/scoring/scorer.go` MUST remain untouched at the combination-method level; only the rubric input changes.
- R4.5 `internal/scoring/persist.go` MUST include the new signal's contribution row in the breakdown without schema changes.

### R5. CLI readback

- R5.1 `commands/score.go` MUST render the new section described in D7 for both YAML/text and `--json` output.
- R5.2 The renderer MUST NOT load archived sentinel bodies for display; only `archived_sentinel_path` is printed.

### R6. Companion-gate compatibility

- R6.1 The schema, CLI primitive, and dedup rules MUST be designed so `orchestrator-handoff-gate` and `delegation-closeout-gate` (from `orchestration-companion-stop-hooks`) emit compatible records without any companion-plan code change beyond enumerating their `rule_id` namespace.

### R7. Rubric coordination

- R7.1 The implementation task MUST read the current `RubricVersion` at task execution time and choose 2.1.0 or 3.1.0 per D5; the task notes record both branches.
- R7.2 `docs/OUTCOME_SCORING_RUBRIC.md` MUST gain a section "RubricVersion ordering for concurrent plans" recording the policy in D5 verbatim.

### R8. Migration & history

- R8.1 Pre-existing iterations with no `iter-N.hook-outcomes.yaml` sidecar MUST score with `SignalHookOutcomes` absent (no retro-penalty). The "absent does not vote" path covers this.
- R8.2 `da score run --recompute` MUST be safe to run against an iteration log that pre-dates R1.5; existing sidecars under RubricVersion 2.0.2 remain valid until explicitly re-scored.

## Done criteria

- DC1 Schema file + bundled twin + `schemas.go` registration; round-trip test in `commands/workflow/static/schemas_test.go`.
- DC2 `da workflow hook-outcome write` CLI primitive with unit tests for append idempotency, no-iteration-active behavior, and disallowed-field rejection.
- DC3 The three upstream gate.sh scripts (`iteration-close-gate`, `isp-gate`, `loop-worker-gate`) call the new CLI primitive instead of (or in addition to) any current direct-write code path.
- DC4 `internal/scoring/signal_hook_outcomes.go` + tests cover: no-sentinel iteration, all-allow, mixed advise, terminal remediate, prevention+remediation dedup, advisory-rule exclusion.
- DC5 `internal/scoring/rubric_test.go` validates the new weights sum to 1.0 and bumps `RubricVersion` per D5.
- DC6 `commands/score_test.go` covers the new readback section in text and `--json` modes.
- DC7 The T1b post-tool observation assessment doc lands with an explicit "approved | deferred | rejected" decision per D9 of `loop-discipline-stop-hooks`.
- DC8 An end-to-end test under `tests/test-r1-5-hook-enforcement-telemetry.sh` exercises: sentinel write → gate hard-remediation → outcome sidecar write → `da score iteration` showing the contribution.
- DC9 `docs/OUTCOME_SCORING_RUBRIC.md` documents the new signal, weight rebalance, RubricVersion ordering policy, and per-rule_id breakdown.
- DC10 Companion-plan fixtures (`orchestration-companion-stop-hooks` task) consume the same schema unchanged.

## Open questions (to be resolved in-plan)

- Q1 Should `hook_outcomes` weight be 0.10 (proposed) or 0.05 (less aggressive)? Calibrate after the first three real iterations have scored under 2.1.0.
- Q2 Should advisory rules ever feed the score at a lower weight (e.g. soft sub-score 0.8) instead of being excluded? Defer until T1b's evaluation establishes whether soft-rule signal-to-noise is acceptable.
- Q3 What is the archival pruning policy for old `iter-N.hook-outcomes.yaml` files? Recommendation: never auto-prune (matches R5's audit log indefinite retention); add a `da workflow hook-outcome prune --before <date>` admin-only command for operator request.
- Q4 If R5 ships first and bumps to 3.0.0, does the rebalance in D3 still apply, or does R5's `human_label` re-balance subsume it? T2c resolves at task time.
- Q5 Should `correlation_id` allow cross-iteration grouping (e.g. a PreCompact at iter-3 paired with a Stop at iter-4)? v1 says no: records are iteration-local. Revisit if real compaction patterns demand it.

## Boundary

No telemetry write may make a preventive or stop-hook remediation unreliable or slower than its enforcement timeout. Transcript text, raw tool output, and sensitive command payloads are not telemetry fields; store event name, bounded rule/result metadata, redacted failure classification where approved, and links to durable artifacts only. A post-tool observation must not be counted separately when it merely records the same prevention or terminal remediation outcome.
