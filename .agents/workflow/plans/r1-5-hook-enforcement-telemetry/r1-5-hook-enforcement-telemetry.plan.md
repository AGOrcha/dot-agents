# R1.5 Hook Enforcement Telemetry Plan

- plan-id: `r1-5-hook-enforcement-telemetry`
- spec: [`../../specs/r1-5-hook-enforcement-telemetry/design.md`](../../specs/r1-5-hook-enforcement-telemetry/design.md)
- predecessor: `r1-outcome-scoring` (completed)
- upstream producer: `loop-discipline-stop-hooks`

## Purpose

The completed R1 plan scores signals that already existed. Loop-discipline
hooks create new objective signals: a gate evaluated an invocation and
allowed it, prevented a forbidden action, advised it, or required terminal
remediation. Approved lifecycle work also identifies `PostToolUse` and
`PostToolUseFailure` as potential observation inputs for workflow-error
feedback. This R1.5 plan tracks that increment rather than reopening the
completed baseline.

## Sequence

1. `t0-outcome-contract` defines a versioned outcome document linked to the
   history-archived sentinel record.
2. `t1-capture-outcomes` persists gate results after the upstream hook
   bundles and native output behavior have shipped.
3. `t1b-post-tool-observation-evaluation` determines whether post-tool
   success/failure observations are sufficiently stable, redacted, and
   deduplicated to retain or score.
4. `t2-scoring-signal` adds approved outcomes as explainable scoring input with
   a deliberate rubric version change.
5. `t3-cli-readback` makes the contribution visible in score queries.

## Dependency Boundary

This plan must not implement against draft hook payload assumptions.
`t1-capture-outcomes` begins only after the loop-discipline P2/P5 contract
has passed verification, including approved pre-action/start/compaction
behavior, Cursor's native `followup_message` behavior, and durable sentinel
history archive location. Post-tool observation must remain non-blocking and
must not enter scoring until T1b resolves payload, privacy, and
double-counting boundaries.
