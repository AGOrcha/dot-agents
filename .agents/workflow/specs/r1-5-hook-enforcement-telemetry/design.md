# R1.5 Hook Enforcement Telemetry - Design

- spec-id: `r1-5-hook-enforcement-telemetry`
- status: active
- date: 2026-05-25
- predecessor: `r1-outcome-scoring` (completed)
- producer dependency: `loop-discipline-stop-hooks`

## Problem

`r1-outcome-scoring` shipped scoring of existing telemetry and workflow
artifacts. The loop-discipline stop-hook plan introduces a new objective
signal source: whether a skill invocation completed cleanly, was prevented
before a forbidden action, was advised, or required hard remediation. The
approved lifecycle expansion also makes post-tool success/failure events a
possible source of non-blocking workflow feedback. Leaving those signals
unexamined would discard useful evidence the new hooks produce.

## Decision

Track hook enforcement telemetry as R1.5 rather than reopening the completed
R1 plan. The stop-hook plan owns enforcement behavior; this plan consumes its
stable structured outcome contract.

## Requirements

- Define a versioned hook-outcome document linked to archived hook sentinels.
- Persist one outcome for each evaluated sentinel invocation: `allow`,
  `advise`, or `remediate`, including rule identifiers and platform, without
  storing transcript contents.
- Represent the lifecycle point and intervention class separately so scoring
  can distinguish `prevent_before_action`, `remediate_at_stop`,
  `continuity_advice`, and `observe_tool_result` without inflating one
  incident into multiple enforcement outcomes.
- Evaluate `PostToolUse` and `PostToolUseFailure` for bounded observation of
  workflow commands such as fanout, verify, checkpoint, merge-back, and
  closeout. Observation remains non-blocking and unscored until the
  evaluation establishes stable vendor payloads, deduplication, and
  privacy/noise controls.
- Add hook-outcome signals to explainable scoring only after the capture
  contract is stable and tested.
- Provide CLI readback so a reviewer can see which gate outcomes affected an
  iteration or session score.

## Boundary

No telemetry write may make a preventive or stop-hook remediation unreliable
or slower than its enforcement timeout. Transcript text, raw tool output, and
sensitive command payloads are not telemetry fields; store event name,
bounded rule/result metadata, redacted failure classification where approved,
and links to durable artifacts only. A post-tool observation must not be
counted separately when it merely records the same prevention or terminal
remediation outcome.
