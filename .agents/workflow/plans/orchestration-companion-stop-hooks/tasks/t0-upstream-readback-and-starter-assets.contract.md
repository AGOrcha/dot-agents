# T0 Starter Assets Contract

- task: `t0-upstream-readback-and-starter-assets`
- external prerequisite: completed `loop-discipline-stop-hooks` P3b
- requirements: R1, D3

## Goal

Ship the two companion skill trees not covered by the upstream starter work,
while making `plan-wave-picker` speak the canonical workflow model it is
supposed to guide. Do not duplicate or replace the `delegation-lifecycle`
starter tree created upstream.

## Grounded Source Map

The repository-local and installed trees were compared during planning and
contained equivalent content. Use the repository-local trees as the source
for reviewable starter promotion:

| Source | Destination |
| --- | --- |
| `.agents/skills/orchestrator-session-start/` complete tree | `internal/scaffold/home/starter/skills/global/orchestrator-session-start/` |
| `.agents/skills/plan-wave-picker/` complete tree | `internal/scaffold/home/starter/skills/global/plan-wave-picker/` |

Read, but do not alter in this task:

- `internal/scaffold/home/starter/skills/global/delegation-lifecycle/`, which
  must already exist through upstream P3b.
- `internal/scaffold/home/embed.go` and
  `internal/scaffold/home/copy_test.go`, because the upstream plan established
  recursive starter-copy behavior and the local test pattern.

## Required Wave-Picker Correction

The copied starter form must make the following authority order explicit:

1. `da --json workflow eligible [--plan <scope>]` supplies the eligible set,
   conflict annotations, evidence confidence, and `max_batch`.
2. Canonical `.agents/workflow/plans/<id>/{PLAN.yaml,TASKS.yaml}` supplies
   plan and task state.
3. Legacy `.agents/active/*.plan.md` may be consulted as compatibility prose
   but must not override canonical state.

This task changes only the starter copy. Updating source assets under
`.agents/skills/` requires separate ownership and is not implicit here.

## Acceptance

- Add starter-copy assertions for a representative instruction file from
  each new skill tree.
- Preserve the upstream assertion that `delegation-lifecycle` materializes.
- Materializing starter assets into an empty home emits the two new complete
  trees without loader changes.
- No hook bundle is created for `plan-wave-picker`.

## Out of Scope

- Sentinel wiring, which belongs to T2 and T3.
- Edits to the upstream parent plan or source assets outside this plan's
  write scope.
