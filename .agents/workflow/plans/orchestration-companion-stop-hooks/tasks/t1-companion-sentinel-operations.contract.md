# T1 Companion Sentinel Operations Contract

- task: `t1-companion-sentinel-operations`
- external prerequisite: completed `loop-discipline-stop-hooks` P0
- requirements: R2, D4

## Goal

Extend the single upstream sentinel protocol just enough for the two
companion lifecycle gates. Do not add a parallel record format, parser, or
archive location.

## Allowed Extension

Extend the accepted skill set with:

- `orchestrator-session-start`
- `delegation-lifecycle`

Extend versioned context with an operation discriminator restricted to:

| Skill | Allowed operation | Evidence known at write time |
| --- | --- | --- |
| `orchestrator-session-start` | `fanout_handoff` | plan/task, expected delegation and bundle paths, expected write scope, optional required sidecar path and evidence confidence |
| `orchestrator-session-start` | `existing_bundle_handoff` | plan/task, existing delegation and bundle paths, expected write scope |
| `delegation-lifecycle` | `parent_closeout` | plan/task, selected `accept` or `reject` decision, expected archive-relative artifacts, active artifact paths expected to be cleared |

Do not permit an `operation` combination that has no corresponding gate. In
particular, there is no `plan-wave-picker` sentinel and no delegated worker
operation in this plan.

## Archive and Validation Contract

- Reuse `.agents/active/hook-sentinels/` while active and
  `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/` after successful
  evaluation.
- Validate all declared paths as repository-relative before persisting them.
- Retain the upstream atomic write, collision rejection, deterministic latest
  read, and no-silent-delete behavior.
- Do not store transcript text or tool-command bodies.

## Acceptance

- Unit tests accept each documented skill/operation pair and reject
  unsupported combinations.
- Unit tests cover expected artifact and cleanup path validation.
- Existing upstream sentinel fixtures continue to validate without migration.
- Successful companion sentinel archival uses the same durable history
  destination as upstream records.

## Out of Scope

- Gate decision logic.
- Selection-receipt persistence for `plan-wave-picker`.
- R1.5 outcome persistence beyond preserving fields it later consumes.
