# P4 Skill Sentinel Wiring Contract

- task: `p4-sentinel-wiring`
- requirements: R8, D5, D7
- dependencies: `p0-sentinel-cli`, `p3-starter-promotion`

## Goal

Make each starter-shipped skill open its own enforcement record as soon as it
has enough invocation context, before performing the actions that its stop
gate evaluates.

## Wiring Points

| Skill | Sentinel-write placement | Required context |
| --- | --- | --- |
| `iteration-close` | after resolving direct/delegated mode and task identity, before verify/checkpoint/advance or merge-back actions | plan, task, run ID, expected closeout artifacts, agent type |
| `isp` | after consuming the pre-gathered eligible/orientation input, before selecting or dispatching work | plan, task/run ID, `eligible_snapshot_loaded`, `max_batch` |
| `loop-worker` | after reading the delegation bundle, before scoped implementation or workflow closeout commands | plan, task, run ID, `agent_type=loop-worker`, delegated `write_scope` |

The wording “start of governed work” is intentional: a skill cannot write a
correct sentinel before it knows task identity or delegated scope.

## Documentation Edits

- Update each copied `SKILL.md` or its loaded instruction file so the
  sentinel invocation is a required step, not optional advice.
- Add a concise gotcha/proposal-criteria statement that changes to governed
  workflow actions must update the corresponding gate contract and tests.
- If `isp` lacks a loaded gotchas document, place the rule in its existing
  instruction structure rather than creating an unused file.

## Acceptance

- Each skill issues `da workflow hook-sentinel write` exactly once for one
  invocation.
- Invocation arguments satisfy the P0 schema and include only context the
  skill actually knows.
- No instruction claims transcript-only behavior can require hard remediation without
  verified trace input.

## Out of Scope

- Changes to the source files in `~/.agents`; the starter is the plan-owned
  deliverable.
- Gate script implementation.
