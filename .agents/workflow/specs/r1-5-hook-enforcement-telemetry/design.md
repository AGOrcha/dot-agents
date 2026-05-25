# R1.5 Hook Enforcement Telemetry - Design

- spec-id: `r1-5-hook-enforcement-telemetry`
- status: draft
- date: 2026-05-25
- predecessor: `r1-outcome-scoring` (completed)
- producer dependency: `loop-discipline-stop-hooks`

## Problem

`r1-outcome-scoring` shipped scoring of existing telemetry and workflow
artifacts. The loop-discipline stop-hook plan introduces a new objective
signal source: whether a skill invocation completed cleanly, advised, or
required hard remediation. Leaving that signal untracked would discard the
strongest direct evidence the new gates produce.

## Decision

Track hook enforcement telemetry as R1.5 rather than reopening the completed
R1 plan. The stop-hook plan owns enforcement behavior; this plan consumes its
stable structured outcome contract.

## Requirements

- Define a versioned hook-outcome document linked to archived hook sentinels.
- Persist one outcome for each evaluated sentinel invocation: `allow`,
  `advise`, or `remediate`, including rule identifiers and platform, without
  storing transcript contents.
- Add hook-outcome signals to explainable scoring only after the capture
  contract is stable and tested.
- Provide CLI readback so a reviewer can see which gate outcomes affected an
  iteration or session score.

## Boundary

No telemetry write may make a stop-hook remediation unreliable or slower than
its enforcement timeout. Transcript text and sensitive command payloads are
not telemetry fields; store rule/result metadata and links to durable
artifacts only.
