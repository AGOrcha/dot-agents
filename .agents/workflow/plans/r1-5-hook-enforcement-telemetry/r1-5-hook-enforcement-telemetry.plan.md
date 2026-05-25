# R1.5 Hook Enforcement Telemetry Plan

- plan-id: `r1-5-hook-enforcement-telemetry`
- spec: [`../../specs/r1-5-hook-enforcement-telemetry/design.md`](../../specs/r1-5-hook-enforcement-telemetry/design.md)
- predecessor: `r1-outcome-scoring` (completed)
- upstream producer: `loop-discipline-stop-hooks`

## Purpose

The completed R1 plan scores signals that already existed. Loop-discipline
hooks create a new objective signal: a gate evaluated an invocation and
allowed it, advised it, or required remediation. This R1.5 plan tracks that
increment rather than reopening the completed baseline.

## Sequence

1. `t0-outcome-contract` defines a versioned outcome document linked to the
   history-archived sentinel record.
2. `t1-capture-outcomes` persists gate results after the upstream hook
   bundles and native output behavior have shipped.
3. `t2-scoring-signal` adds the outcome as an explainable scoring input with
   a deliberate rubric version change.
4. `t3-cli-readback` makes the contribution visible in score queries.

## Dependency Boundary

This plan must not implement against draft hook payload assumptions.
`t1-capture-outcomes` begins only after the loop-discipline P2/P5 contract
has passed verification, including Cursor's native `followup_message`
behavior and durable sentinel history archive location.
