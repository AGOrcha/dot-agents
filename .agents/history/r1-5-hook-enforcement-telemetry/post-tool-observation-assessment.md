# Post-Tool Observation Evaluation (R1.5 T1b)

- task-id: `t1b-post-tool-observation-evaluation`
- plan-id: `r1-5-hook-enforcement-telemetry`
- date: 2026-05-26
- status: **decided — DEFERRED to R1.5.1** (observation NOT admitted to v1
  scoring; see [Decision](#decision))
- resolves: spec `r1-5-hook-enforcement-telemetry/design.md` R3.2 and
  `loop-discipline-stop-hooks/design.md` D9 / DC13
- companion artifact: `docs/OUTCOME_SCORING_RUBRIC.md` §"Post-tool
  observation evaluation (R1.5 T1b)"
- research only — no code, no schema change, no sidecar field added

## Purpose

The R1.5 spec (D3, R3, D4) admits `hook_outcomes` as a new scoring signal
fed by terminal-gate, pre-action-prevention, and continuity-advice
records, but holds the post-tool surface (`PostToolUse` /
`PostToolUseFailure`) out of v1 pending an explicit evaluation. The
upstream `loop-discipline-stop-hooks` plan ships these events as mapped
observation candidates (D9, HOOKS.md §"PostToolUse and PostToolUseFailure
are observation candidates") with no blocking semantics.

This document is the gate that R1.5 R3.2 / DC13 require: before any
post-tool observation contributes to scoring, four boundaries must be
named and answered — payload stability, workflow-command filtering,
redaction/privacy, and deduplication against terminal/pre-action records
for the same intent. Each section below states the criterion, what was
found, what remains unknown, and what the assessment concludes.

The output is one of three verdicts: **approved** (admit as an objective
signal in T2-scoring-signal), **deferred** (the criteria do not yet hold
but could in a future R1.5.1), or **rejected** (the post-tool surface is
permanently disqualified as a scoring input).

## Scope of evaluation

In scope:

- `post_tool_use` and `post_tool_use_failure` canonical `HookSpec.When`
  values as already mapped in `internal/platform/hooks.go` (claude,
  codex, cursor, copilot tables).
- Workflow-command observation only: `da workflow *`, `da score *`,
  `da review *`, `da workflow hook-sentinel *`, and the gate.sh entrypoints
  shipped under `internal/scaffold/hooks/global/`.
- Use of post-tool records as a **scoring** input — feeding the
  `hook_outcomes` sub-score per R1.5 D3 — and the prior question of
  whether the records are safe to persist at all in
  `iter-N.hook-outcomes.yaml` under the D2 schema.

Out of scope:

- Blocking behavior at post-tool. The upstream D9 already states post-tool
  events are observation candidates, not gates; this assessment does not
  reopen that.
- Non-workflow tool calls (file edits, MCP calls, arbitrary Bash). These
  generate the bulk of post-tool payload volume but have no contract with
  the rubric; admitting them is a separate, larger evaluation that does
  not block R1.5 v1.
- Vendor-specific failure-message normalization beyond a bounded enum.
  The D2 schema explicitly disallows raw `stderr` / `failure_message`
  free-text; any normalization that would change that is a schema
  amendment outside R1.5 v1.

## Criteria

The four criteria are written as decision gates. Each must answer **yes**
for an "approved" verdict.

### C1. Vendor payload stability

> Is the post-tool event payload — specifically the fields needed to
> attribute a record to a workflow command, a `rule_id`, and a result —
> stable enough across Claude, Codex, Copilot, and Cursor that a single
> v1 extractor can populate `HookOutcome` without per-platform branching
> deeper than the existing event-table layer in `internal/platform/hooks.go`?

**Findings.**

- The canonical mapper today (`internal/platform/hooks.go` lines 605–705)
  has `post_tool_use` mapped on all four platforms and
  `post_tool_use_failure` mapped on three. Codex's row in `docs/HOOKS.md`
  (line 85) has no `post_tool_use_failure` entry — Codex's vendor surface
  documents only `PostToolUse` and conflates success/failure in that one
  event. `docs/PLATFORM_DIRS_DOCS.md` line 53 corroborates: Codex's
  documented event list is `SessionStart, SubagentStart, PreToolUse,
  PermissionRequest, PostToolUse, PreCompact, PostCompact,
  UserPromptSubmit, SubagentStop, Stop` — no `PostToolUseFailure`.
  Implication: on Codex, a failed workflow command surfaces as a
  `PostToolUse` record whose body must be inspected to discover the
  failure, which the D2 schema's disallowed-fields list (`stderr`,
  `tool_output`, `failure_message`) forbids storing.
- Cursor and Copilot expose `postToolUse` / `postToolUseFailure` as
  separate events (`HOOKS.md` line 84–85), but their payload shape is
  vendor-private and not currently round-tripped through a golden fixture
  in `internal/platform/hooks_test.go`. Existing tests (line 401–411)
  cover the *event name* mapping, not payload field names or types.
- Claude's `PostToolUse` payload is documented in `docs/PLATFORM_DIRS_DOCS.md`
  line 42 (broad event surface enumeration) but specific field shapes
  for tool name, args, exit code, and durations are not pinned in our
  parity tests.
- No platform's post-tool payload includes a stable identifier that
  joins back to a `sentinel_id` written by `da workflow hook-sentinel`.
  The terminal-gate path (D2, R2.1) achieves this join because the gate
  itself runs inside the sentinel-anchored skill invocation; a post-tool
  event fires outside that invocation surface and would have to recover
  the active sentinel via a side-channel read of
  `.agents/active/loop-state.md`. That read is the same pattern R2.2
  already uses for the terminal write, so it is feasible — but it
  introduces a new "active-sentinel-at-post-tool-time" semantic that
  does not exist for the terminal events.

**Unknown / requires vendor-doc walk.**

- Whether Claude's `PostToolUse` payload reliably carries the originating
  tool name and its argv as structured fields (vs. only via the tool
  result body). Without a structured tool-name field, the workflow-command
  filter in C2 is best-effort string-matching on payload text.
- Whether Codex's `PostToolUse` payload exposes a `success: bool` or
  exit-code field separately from the tool output body, which would let
  us classify pass/fail without storing forbidden free-text.
- Whether Copilot's `postToolUse` and `postToolUseFailure` payloads use
  the same field names (consistent enough for a single extractor) or
  diverge as the existing event-name divergence (`agentStop` vs `stop`)
  suggests is possible.

**Verdict on C1.** **Partial.** The event-name mapping is stable. The
payload-field stability needed to populate a `HookOutcome` without
storing disallowed free-text is **not** established for any of the four
platforms in v1 — no golden fixtures, no documented field contracts in
our parity layer, and Codex's missing dedicated failure event forces
payload introspection that the D2 schema forbids.

### C2. Workflow-command filtering

> Can a post-tool record be reliably restricted to **only** the workflow
> commands the rubric cares about (`da workflow advance`,
> `da workflow merge-back`, `da workflow checkpoint`,
> `da workflow fanout`, `da workflow plan archive`,
> `da workflow hook-sentinel *`, `da workflow verify *`, plus
> `iteration-close-gate`, `isp-gate`, `loop-worker-gate` script invocations),
> without false positives (other `da` subcommands, unrelated Bash) and
> without false negatives (workflow commands invoked indirectly via a
> wrapper or alias)?

**Findings.**

- The `iteration-close` skill and the three gate scripts run shell
  commands whose argv begins with `da workflow` or whose script path lives
  under `internal/scaffold/hooks/global/*-gate/gate.sh`. A regex like
  `^(da\s+workflow\s+(advance|merge-back|checkpoint|fanout|verify\s+\w+|hook-sentinel\s+\w+|plan\s+archive)|\.agents/.*/gate\.sh)$`
  would identify them deterministically *if* the post-tool payload
  exposes the argv as a stable field on every platform — see C1's
  unknowns.
- The fallback approach — string-matching on the payload's tool-output
  body — is unsafe under D2 (forbids storing the body) and unreliable
  (output formatting varies by command). The hook-sentinel write path
  already solved this problem for the terminal/pre-action side by
  hosting the gate inside the sentinel-anchored skill and writing the
  outcome from there with the command intent already known. Post-tool
  observation cannot inherit that property; it must rediscover the
  command intent from the payload.
- A "named approved commands" allow-list is operationally tractable.
  T-archival-policy already enumerates `da workflow hook-outcome prune`
  as a future admin-only command; an equivalent allow-list constant in
  `internal/scoring/signal_hook_outcomes.go` (or a new
  `internal/scoring/posttool_filter.go` if T1b were approving) is
  feasible. The allow-list does not solve the *attribution* problem
  (which sentinel, which iteration, which rule) — only the *filter*
  problem.

**Verdict on C2.** **Partial.** Filtering by command name is feasible if
C1 stabilizes the payload's tool-name field. Until then, filtering is
guesswork on payload text, which the D2 schema's no-free-text boundary
makes both legally and practically unworkable.

### C3. Redaction & privacy

> Can the persisted post-tool record honor the D2 disallowed-fields
> contract (no `transcript_excerpt`, `tool_input`, `tool_output`,
> `stdout`, `stderr`, `command_args`, `failure_message` beyond a bounded
> enum) **and** carry enough information to be scorable?

**Findings.**

- The terminal-gate path satisfies this by classifying the outcome
  inside the gate (the gate decides `allow` / `advise` / `remediate`
  with a known `rule_id`) and persisting only the classification. The
  post-tool surface has no analogous classifier in v1 — the only
  information the hook has access to is the raw tool result, which is
  exactly what D2 forbids storing.
- A "failure-category enum" (`exit_nonzero`, `timeout_exceeded`,
  `permission_denied`, `vendor_error`, `unknown`) is the only path that
  satisfies D2 without inventing a free-text field. It requires a
  classifier — either the post-tool hook itself runs a small shell
  decision tree, or a follow-up extractor reads the (forbidden!) payload
  body once at write time and stores only the bucketed result. Both
  options expand the gate.sh contract beyond what the upstream plan
  scoped.
- Workflow-command argv may itself be sensitive. `da workflow advance
  --plan <id> --task <id>` is benign; `da workflow plan archive --plan
  <id>` is benign; `da workflow verify record --note "<free text from
  agent>"` is **not** benign — the `--note` flag accepts arbitrary
  agent-authored prose that may include transcript content. The D2
  boundary cleanly excludes this today because terminal-gate records
  never carry argv. Admitting post-tool records would require either
  (a) reintroducing argv with a redaction layer, or (b) capturing only
  the command name and dropping flags entirely. Option (b) loses the
  attribution detail that would make the record valuable.
- The R5 audit-log contract (`design.md` §"Audit trail integration",
  referenced by t-archival-policy) sets a precedent: scored telemetry
  files never carry agent-authored free-text. Reintroducing it for
  post-tool would silently weaken that contract for every downstream
  reader.

**Verdict on C3.** **Negative.** The redaction story is not solvable in
v1 without either (a) a new classifier inside the post-tool hook itself
(scope expansion), or (b) a relaxation of the D2 disallowed-fields list
(contract regression). Neither is acceptable for R1.5 v1.

### C4. Deduplication against terminal & pre-action records

> If a workflow command was already accounted for at terminal time
> (`stop` / `subagent_stop` `remediate_at_stop`) or at pre-action time
> (`pre_tool_use` `prevent_before_action`), does a post-tool record for
> the same command produce **zero** additional sub-score contribution,
> per the spec boundary "a post-tool observation must not be counted
> separately when it merely records the same prevention or terminal
> remediation outcome"?

**Findings.**

- D4 already establishes the dedup primitive: `correlation_id` groups
  pre+terminal records. Extending it to post-tool would require a
  `correlation_id` value derivable from the post-tool payload. The
  natural candidate is the active `sentinel_id` at the moment the
  post-tool event fires — same recovery path as C1's unknown.
- For a *prevented* command (`pre_tool_use` `prevent_before_action`
  rule_id like `iteration-close.R1.8`), no tool call actually runs, so
  no `post_tool_use` event fires for that intent. There is nothing to
  dedup against in that direction — the post-tool record exists only
  when the command did execute.
- For a *remediated* terminal outcome (e.g. `loop-worker.R3.1` write-scope
  escape, fired at `subagent_stop`), the tool calls that produced the
  escape already executed and emitted their own post-tool events
  individually. The remediation does not know which post-tool record
  caused the escape, and the post-tool record does not know it will
  later be remediated. Dedup would require correlation across iteration
  time, which v1 explicitly rejected in Q5 ("v1 says no: records are
  iteration-local").
- For a *successful* workflow command (the common case), a `post_tool_use`
  record would represent a new fact ("`da workflow advance` ran cleanly"),
  not a duplicate of any terminal/pre-action record. This is the only
  intervention class where post-tool observation would carry distinct
  signal — but it is also exactly the class for which the existing
  rubric already treats absence of a remediate/advise outcome as
  success implicitly (`hook_outcomes` sub-score = 1.0 when all rules
  evaluate to `allow`). Adding post-tool "ran cleanly" records would
  add no marginal information and would force the dedup engine to
  match success records against absence of failure records — a
  much weaker contract than D4's symmetric prevent/remediate match.

**Verdict on C4.** **Negative for value, positive for safety.** The
dedup is safe (no double-counting because post-tool success records
would not overlap with terminal/pre-action records). But the
*marginal information* a post-tool success record adds over the
existing "no remediate ⇒ allow" path is zero. Failed-command records
(`post_tool_use_failure`) might add marginal information, but their
attribution depends on resolving C1 and their persistence depends on
resolving C3.

## Decision

**Deferred to R1.5.1.** Post-tool observation is **NOT** admitted to
v1 scoring. T2-scoring-signal MUST implement the `hook_outcomes` signal
per R1.5 D3 / R4 using **only** terminal-gate (`remediate_at_stop`)
and pre-action (`prevent_before_action`) records. Continuity-advice
(`pre_compact`) records remain observational per D4. The post-tool
surface remains a mapped observation candidate per D9 and HOOKS.md but
emits no `iter-N.hook-outcomes.yaml` records and contributes no
sub-score.

This decision satisfies:

- spec R3.1 ("Until T1b approves a stable contract, `post_tool_use` and
  `post_tool_use_failure` events MUST NOT produce
  `iter-N.hook-outcomes.yaml` records") — the contract is not approved.
- spec R3.2 — all five required questions (payload stability,
  workflow-command filter, redaction strategy, dedup, noise budget)
  have explicit answers below.
- loop-discipline DC13 — an explicit decision is recorded, and the
  answer for failed-workflow-command persistence is "not in v1".
- the spec Boundary clause — no post-tool record will be counted that
  merely re-records a prevention or terminal remediation.

### Required answers per R3.2

| Question | Resolution |
|---|---|
| (a) per-platform payload stability | Unstable in v1. Event names are mapped on all four platforms but payload field shapes are not pinned by golden fixtures, and Codex lacks a dedicated `PostToolUseFailure` event (PostToolUse conflates success/failure). Re-evaluate after a payload-fixture parity task lands. |
| (b) workflow-command filter regex | Reserved but not enabled. Future regex (R1.5.1): `^(da\s+workflow\s+(advance\|merge-back\|checkpoint\|fanout\|verify\s+\w+\|hook-sentinel\s+\w+\|plan\s+archive)\|\.agents/.*-gate/gate\.sh)$`. Application requires (a) first. |
| (c) redaction strategy for failure messages | Required form: bounded enum `{exit_nonzero, timeout_exceeded, permission_denied, vendor_error, unknown}`. Implementation requires a new classifier in `gate.sh` or extractor, out of scope for R1.5 v1. Free-text `failure_message` remains disallowed by D2. |
| (d) deduplication against terminal remediation | Mechanism designed: extend D4's `correlation_id` join to also match post-tool records by `(sentinel_id, rule_id_resolved_from_argv)`. Not wired in v1. Success records would not dedup against absence of remediate records (no overlap); failure records would dedup 1:1 against any later `remediate_at_stop` for the same `correlation_id`. |
| (e) noise budget cap | Recommended for R1.5.1: **max 20 post-tool records per `iter-N.hook-outcomes.yaml`** with back-pressure that drops the 21st+ silently and emits a single stderr advisory `"posttool-observation: noise budget exceeded for iter <N>, dropped <K> records"`. Mirrors the spec's 8000ms hook-budget posture (R2.4). Not wired in v1. |

## Rationale

The four criteria do not all clear v1. C1 is partial (event-name parity
is stable, payload-field parity is not pinned), C2 is partial
(filtering needs C1), C3 is negative (redaction breaks the D2 contract
without a new classifier), C4 is negative-for-value (the marginal
information over "absent ⇒ allow" is near zero for successful commands;
failure records depend on C1+C3). The strict reading of the spec
boundary clause — "a post-tool observation must not be counted
separately when it merely records the same prevention or terminal
remediation outcome" — does not by itself preclude admitting *new*
information from post-tool, but it does forbid admitting information
the existing surface already covers, which is most of what post-tool
would produce in v1.

A deferred verdict is preferable to a rejected verdict because none of
the failed criteria are intrinsic to post-tool — each becomes solvable
with bounded follow-up work:

- C1 → a payload-fixture parity task (Claude, Codex, Copilot, Cursor;
  one golden per `(platform, event)` pair) under the existing
  `internal/platform/hooks_test.go` pattern.
- C2 → naturally follows C1 (the regex is trivial once the argv field
  is pinned).
- C3 → either a small classifier in `gate.sh` writes the bounded enum
  directly, or a new "post-tool classifier" CLI primitive
  (`da workflow hook-outcome classify-posttool`) does so on a payload
  handed over stdin; either approach preserves D2.
- C4 → the dedup extension is a small change to the future extractor
  (`internal/scoring/signal_hook_outcomes.go`) once C1/C2/C3 are in.

The cheaper alternative — admit post-tool today but only as an
observational track per the rejected D3 alternative ("Add as
`IterationObjectives` only") — was considered and is not adopted here.
Adding an observation track for events whose attribution and redaction
are not yet solved would seed `iter-N.hook-outcomes.yaml` with records
that later have to be either reclassified or purged when the
contract stabilizes. R1.5 v1 keeps the file empty of post-tool records,
so there is no migration cost when R1.5.1 admits them under a settled
contract.

## Rejected alternatives

1. **Admit post-tool success records only.** Cheapest path — they need
   no redaction beyond command-name capture. Rejected because their
   marginal information over the existing `hook_outcomes = 1.0 when no
   remediate fires` path is zero, and admitting them establishes a
   payload-shape contract we have not yet validated in C1.
2. **Admit post-tool failure records as a separate observational track
   (no scoring).** Looks safe but seeds the file with records under a
   schema that R1.5.1 will need to amend. Either we keep them under v1
   schema (post-tool record without `intervention_class` enum extension
   = invalid per D2 R1.3) or we extend the schema now for a v1.0 the
   plan explicitly defers. Both choices burn a schema version on an
   unstable contract.
3. **Score post-tool failure records with a hard 0.5 weight.** This
   would treat any failed workflow command as a remediate-class outcome.
   Rejected because the failure might be an expected fail-fast (e.g.
   `da workflow verify record` returning non-zero when the verification
   itself failed, which is *correct* gate behavior) — the rubric must
   not penalize the gate for being honest about a failed verification
   the rubric is *also* about to penalize via the `verifier` signal.
   Double-counting via two signals is exactly the boundary clause D4
   was written to prevent.
4. **Reject permanently.** Rejected because the failure modes are all
   contract gaps, not intrinsic limits — see Rationale above.

## What changes when R1.5.1 reopens this

When (if) R1.5.1 is opened, the work is bounded:

1. Land the payload-fixture parity task (one golden per
   `(platform, post_tool_event)`).
2. Extend the D2 schema with `intervention_class: observe_tool_result`
   (the enum already reserves the name) and a `failure_category` bounded
   enum field (additive, schema_version bump to 2).
3. Add `da workflow hook-outcome write` accept-path for post-tool
   records, gated on the workflow-command allow-list and noise budget.
4. Extend `internal/scoring/signal_hook_outcomes.go` dedup to match
   post-tool failure records by `correlation_id` against later
   terminal `remediate_at_stop` for the same `(sentinel_id, rule_id)`.
5. Bump `RubricVersion` per the policy in
   `docs/OUTCOME_SCORING_RUBRIC.md` §"Versioning policy" — a sub-score
   mapping change for `hook_outcomes` is a minor bump; the existing
   weights need not move.

Until then, the post-tool surface is **mapped, observable to operators
via their own hook scripts, and not consumed by R1.5 scoring**.

## References

- spec: `.agents/workflow/specs/r1-5-hook-enforcement-telemetry/design.md`
  (D2 schema, D3 signal, D4 dedup, R3 post-tool defer, DC7 this assessment)
- spec: `.agents/workflow/specs/loop-discipline-stop-hooks/design.md`
  (D9 observation candidates, R6.7, DC13 explicit decision)
- code: `internal/platform/hooks.go` lines 605–705 (per-platform event
  mapping tables)
- code: `internal/platform/hooks_test.go` lines 401–411 (event-name
  parity tests; no payload-shape tests)
- docs: `docs/HOOKS.md` lines 84–85 (per-platform mapping table) and
  158–167 (observation-candidate notice)
- docs: `docs/PLATFORM_DIRS_DOCS.md` lines 42, 53, 252–253 (vendor
  event surfaces and per-platform parity table)
- companion artifact: `docs/OUTCOME_SCORING_RUBRIC.md` §"Post-tool
  observation evaluation (R1.5 T1b)"
- predecessor decision: `.agents/active/merge-back/t-archival-policy.md`
  (hook-outcome sidecar retention; same rubric file)
