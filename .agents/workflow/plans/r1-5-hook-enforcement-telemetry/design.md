# R1.5 Plan Design — In-Plan Decision Records

- plan-id: `r1-5-hook-enforcement-telemetry`
- spec: [`../../specs/r1-5-hook-enforcement-telemetry/design.md`](../../specs/r1-5-hook-enforcement-telemetry/design.md)

## Purpose

The spec at `specs/r1-5-hook-enforcement-telemetry/design.md` is the contract:
it owns the durable decisions (D1–D9), requirements (R1–R8), done-criteria
(DC1–DC10), and the open questions list (Q1–Q5). This plan-side `design.md`
is where open questions are resolved as work proceeds, per the workflow
artifact model rule that "plans answer questions specs leave open."

Each entry below resolves one spec open question, names the resolving task,
and points at the file/line where the decision is operationalized. Spec
decisions remain in the spec — only the resolution of in-plan questions
lives here.

## Q3 — Hook-outcome sidecar retention and archival policy

**Spec reference.** `specs/r1-5-hook-enforcement-telemetry/design.md` Q3
("What is the archival pruning policy for old `iter-N.hook-outcomes.yaml`
files?").

**Resolving task.** `t-archival-policy` (this delegation).

**Resolution: never auto-prune; admin-only manual pruning on operator request.**

The retention policy for `.agents/active/iteration-log/iter-N.hook-outcomes.yaml`
sidecars mirrors R5's audit log policy (`r5-review-labeling-access` design
D5.4): **indefinite retention by default, no automatic pruning, no time-based
expiration**. An admin-only `da workflow hook-outcome prune --before <date>`
command spec is reserved for the operator-driven case; implementation is
deferred until an operator request emerges.

### Rationale

1. **Score reproducibility under future RubricVersions.** The sidecars feed
   `internal/scoring/signal_hook_outcomes.go` (spec R4.1). Per spec R8.2,
   `da score run --recompute` must remain safe to re-run against any
   historical iteration. Under a future RubricVersion (e.g. 2.1.0 from
   this plan, 3.x from R5 or beyond per spec D5/Q4), a recompute reads the
   original sidecar to re-derive `hook_outcomes` against the new weights.
   Auto-pruning at any age would silently transform recomputed scores from
   "rescored under new rubric" to "absent signal does not vote" — invisible
   in the output, fatal to longitudinal comparison.

2. **Consistency with R5's governance-relevant audit trail.** R5 D5.4
   reasons: "labels feed R1 scoring forever (a re-score under a new
   RubricVersion must be reproducible), so the audit log must outlive any
   single rubric version." The identical reasoning applies here. Hook
   outcomes are the objective evidence behind a `remediate`/`advise`/`allow`
   contribution; an explainable score that cites a rule the sidecar can no
   longer produce is not explainable.

3. **Tiny on-disk footprint.** Per spec D1, each sidecar is an ordered list
   of `HookOutcome` records — one per evaluated sentinel-anchored hook
   invocation in the iteration window. Empirically (R1 historical data
   note) iterations produce ≤1–10 records; YAML at this density is
   kilobytes per iteration. Over the salvaged 65 iterations the projected
   total is well under a megabyte; over a year of active use, single-digit
   megabytes. Disk pressure is not the constraint that earns automatic
   pruning.

4. **No PII or transcript content lives in the sidecar.** Spec D2 forbids
   raw transcript bodies, redacted user text, or model outputs in the
   schema (`workflow-hook-outcome.schema.json` rejects any disallowed
   transcript-content field, asserted by the `t-schema-validator-wiring`
   validator). The privacy-driven argument for time-based expiration (the
   usual reason auto-prune is the safe default) does not apply — there is
   nothing here that decays into a privacy liability with age.

5. **Symmetry with the archived sentinel.** Per spec D1 each terminal
   `HookOutcome` is paired by `sentinel_id` to a record in the durable
   sentinel history archive (the upstream `loop-discipline-stop-hooks`
   contract). Auto-pruning the outcome sidecar while the sentinel remains
   archived would create dangling references on recompute. Keeping the
   sidecar's retention floor at "as long as the sentinel exists" is the
   minimum coherent invariant; "indefinite" honors it without a foreign
   key check.

### Alternatives rejected

- *Time-based auto-prune (e.g. 90 days, 1 year).* Breaks score
  reproducibility under future RubricVersions. The silent-recompute failure
  mode is the worst combination: no error, wrong-but-plausible score.
- *Size-cap rotation (e.g. keep last N MB).* Footprint analysis shows no
  size pressure for the foreseeable workload, and per-iteration sidecar
  ordering is not amenable to rotation in the way an append-only log is.
- *Mirror R5's yearly rotation file pattern.* R5 rotates because its
  audit log is one append-only JSON-lines file. R1.5 sidecars are
  one file per iteration (per spec D1) — rotation has nothing to rotate
  across.
- *Auto-prune iterations not landed on master (per spec signal `landed`
  semantics).* Conflates score-input retention with merge-state. A
  not-landed iteration is still scoring-relevant context (the score itself
  encodes the negative `landed` outcome). Removing its evidence weakens
  every later cross-iteration analysis that asks "what does a low-`landed`
  iteration look like in `hook_outcomes`?"

### Manual pruning command spec (deferred implementation)

A `da workflow hook-outcome prune --before <date>` admin-only command is
the documented escape hatch if an operator ever needs to compact older
sidecars. **Implementation is deferred** until an operator request
materializes — this section is the design contract the implementation
would meet.

**Surface.**

```
da workflow hook-outcome prune --before <YYYY-MM-DD> [--dry-run] [--yes]
```

- `--before <YYYY-MM-DD>` (required). Prunes iteration sidecars whose
  iteration timestamp (`iter-N.yaml` top-level `timestamp` field) is
  strictly before this date. Date-only granularity matches R5's
  `da review audit prune --before` UX.
- `--dry-run` (default if neither `--dry-run` nor `--yes` is given). Lists
  the sidecar paths that would be removed and the total reclaimed bytes;
  performs no writes.
- `--yes` confirms destructive execution; required for non-interactive use.

**Behavior.**

1. Walk `.agents/active/iteration-log/iter-*.hook-outcomes.yaml`.
2. For each file, read the matching `iter-N.yaml` to determine the
   iteration timestamp. Sidecars whose canonical iteration record is
   missing (orphans) are listed but not removed without an extra
   `--include-orphans` flag — a missing iteration record is itself a bug
   to investigate, not a routine cleanup target.
3. Filter to sidecars with `iteration.timestamp < --before`.
4. In `--dry-run` mode (or when `--yes` is absent): print the file list
   and reclaim estimate; exit 0.
5. With `--yes`: delete the selected sidecars and emit a single
   `pruned <N> sidecars, reclaimed <bytes>` line.

**Admin-only enforcement.** The command must reject invocations without
admin authority. Reuse R5's role-gated authentication contract
(`r5-review-labeling-access` D5.3): the same `~/.agents/review/users.yaml`
+ `Authorization: Bearer <token>` mechanism that gates
`da review audit prune` gates `da workflow hook-outcome prune`. A
non-admin token returns exit code 2 and a clear error message; no
sidecars are touched.

**Audit trail.** Each successful prune writes one record to R5's
`.agents/active/review/audit.log.jsonl` chain (action
`workflow.hook-outcome.prune`, target = pruned-file list as content-
addressed hashes). This makes pruning a tamper-evident, reversible-via-
backup event — not a silent disk operation. If R5 has not yet shipped at
the time the command implements, the implementation MUST block on R5's
audit-log primitive rather than fall back to an unaudited delete.

**What this command MUST NOT do.**

- Auto-run on any timer, hook, or CI path. There is no scheduled cron
  invocation, no `da workflow checkpoint` integration, no hook-on-archive
  trigger. The decision to compact is always an explicit operator action.
- Touch iteration records (`iter-N.yaml`), score sidecars
  (`iter-N.score.yaml`), or sentinel-history archives. Scope is strictly
  hook-outcome sidecars.
- Modify or summarize the contents of sidecars before deletion. Compact-
  in-place is not in scope; the command is delete-only.

### Cross-references

- **R5 audit log retention.** `specs/r5-review-labeling-access/design.md`
  D5.4 — the precedent this resolution mirrors.
- **Spec D1 (persistence shape).** Defines the sidecar location and
  per-iteration shape that retention applies to.
- **Spec R4.1 (extractor) and R8.2 (recompute safety).** The
  reproducibility requirements that forbid auto-pruning.
- **Spec D2 (no transcript content).** The privacy invariant that makes
  indefinite retention safe.
- **Done-criterion DC9.** Requires `docs/OUTCOME_SCORING_RUBRIC.md` to
  document the archival policy alongside the other R1.5-driven doc
  changes. This task lands the archival paragraph; the `t-docs`
  consolidation task will integrate it with the rest of the R1.5 doc
  delta.

## Q4 / D5 — RubricVersion ordering with R5

**Resolving task.** `t2c-rubric-version-coordination` (this delegation).

**Resolution: lookup-at-execution, first-to-merge-wins, second-rebases —
no plan-authored version target.** R1.5 and `r5-review-labeling-access`
both mutate the `RubricVersion` constant in `internal/scoring/rubric.go`,
so the two plans must not both claim the same number. The coordination
rule is:

1. The rubric-bump task in each plan **reads the current `RubricVersion`
   constant at the moment it runs** — never at plan-authoring time.
2. It **picks the next free version on top of the current value**, sized
   per the versioning policy: R1.5 ships `hook_outcomes` as a **minor**
   bump (weights rebalanced, signal set widened by one, combination
   method unchanged); R5 ships `human_label` as a **major** bump because
   it introduces the first signal that depends on **external human
   input** rather than agent-run telemetry — a qualitatively new
   dependency surface.
3. **First to merge wins the bump it planned for; the second rebases.**
   R1.5 observed `2.0.2` at execution time and took the next free minor,
   `2.1.0` (the shipped value). If R5 merges first to `3.0.0`, R1.5's
   later mutator would instead target `3.1.0`; if R1.5 merges first (as
   it did), R5 sees `2.1.0` and targets `3.0.0`.

**Rationale.** A plan-authored target ("we will be 2.1.0") goes stale the
moment another in-flight plan rebases to a new base, producing a
merge-conflict-by-version where two open plans both claim the same
number. Reading the constant on the day the task ships keeps the version
ladder monotonic without requiring either plan to predict the other.
This decision is operationalized in the rubric's
[RubricVersion ordering for concurrent plans](../../../../docs/OUTCOME_SCORING_RUBRIC.md)
section, which records the same rule and the cross-plan version table.

### Cross-references

- **R5 RubricVersion mutation.** `specs/r5-review-labeling-access/design.md`
  D5.3 — R5's `human_label` signal is the concurrent rubric mutator this
  ordering rule coordinates with.
- **Spec D5/Q4 (this question).** The spec defers concurrent-version
  ordering to execution time; this section is the operative resolution.
- **`docs/OUTCOME_SCORING_RUBRIC.md` → Versioning policy.** The
  authoritative, user-facing statement of the lookup-at-execution rule
  and the R1.5↔R5 coordination note.

## Other open questions

The remaining spec open questions are resolved by their own tasks and are
recorded here only as pointers — their resolutions land in the task notes
and code/doc changes those tasks produce, not in this file.

- Q1 (weight calibration) — resolved by `t2-scoring-signal` after the
  first three real iterations score under 2.1.0.
- Q2 (advisory-rule sub-scoring) — resolved by
  `t1b-post-tool-observation-evaluation`.
- Q4 (RubricVersion ordering with R5) — resolved above in
  [Q4 / D5 — RubricVersion ordering with R5](#q4--d5--rubricversion-ordering-with-r5).
- Q5 (cross-iteration `correlation_id` grouping) — out of scope for v1
  per the spec; revisit only if real compaction patterns demand it.
