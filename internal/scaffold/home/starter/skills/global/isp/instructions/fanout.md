# ISP Step 4: Fanout the Delegated Task(s)

Use `da workflow fanout` to create the bounded contract and bundle for each selected task.

## Evidence-aware context loading

Before fanning out, check `evidence_confidence` for each selected task:

| evidence_confidence | bundle context action |
|---|---|
| high / medium | Load sidecar `required_reads` + `decision_locks` into bundle context. Pass sidecar path as `--context-file .agents/workflow/evidence/<task_id>.scope.yaml`. |
| low | Note thin context in `--prompt`. Suggest worker reviews derive-scope output. |
| none | Thin context. Worker starts without scope evidence. |

Sidecar path: `.agents/workflow/evidence/<task_id>.scope.yaml`

## Staged-dispatch boundary

Do not inject `.agents/active/active.loop.md`, a loop-worker prompt file, or
the global loop-worker profile contents into typed `impl`, verifier, or
review stages. Those surfaces carry full-slice closeout behavior. The parent
loads each role-specific prompt and any explicit stage-safe product/project
overlay when it spawns that stage.

`--delegate-profile loop-worker` below remains compatibility metadata in the
current bundle schema until native named stage references are materialized;
it is not permission for a typed stage to load the legacy loop-worker
instructions.

## Isolated worktree per delegated slice

Each delegated slice runs in its own managed git worktree so concurrent
workers never cross-stage through a shared index and every slice records the
exact parent tip it forked from. Provision it with `da worktree` — never raw
`git worktree add` / `git branch`, and never re-derive the fork point with
`git merge-base` (the recorded base is authoritative):

```bash
da worktree create \
  --name <slice-name> \                    # [a-zA-Z0-9-]+ — derive from the task-id
  --path .agents/worktrees/<slice-name> \  # directory for the new linked worktree
  --base-branch <parent-branch> \          # its CURRENT tip is recorded as the base
  --purpose "<plan-id>/<task-id>" \        # free-form registry note
  --parent-pr <n>                          # optional: PR this slice feeds into
```

`create` forks `<slice-name>` off `<parent-branch>`, checks it out at `--path`
with its own isolated index, and records the parent tip as the slice's
immutable base ref. Pass that path to the worker (in the `--prompt` below and
the TASKS.yaml `notes`) so it implements inside the isolated worktree; the
parent's `da worktree merge-back` step (see `staged-runtime.md` § Parent gate)
later reads that recorded base, so no manual branch/base bookkeeping is needed.

## Fanout command

```bash
da workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --write-scope "<scope>" \
  --owner "<worker-name>" \
  --delegate-profile loop-worker \
  --feedback-goal "<concrete question evidence must answer>" \
  --prompt "Staged dispatch bundle: parent supplies role-specific stage instructions and any stage-safe overlay at stage spawn; typed children emit only their stage artifact." \
  --context-file .agents/active/loop-state.md \
  --context-file .agents/workflow/plans/<plan_id>/TASKS.yaml \
  [--context-file .agents/workflow/evidence/<task_id>.scope.yaml]  # when confidence >= medium
  --selection-reason "<why this task now>"
```

## Parallel fanout mode

When `max_batch > 1` AND parallel mode is active: fan out one bundle per task in `max_batch`. Keep write_scopes non-overlapping. If two tasks in `max_batch` have unexpected overlapping scopes, defer the conflicting task to the next pass.

## Write TASKS.yaml notes before handoff

Write constraints, risks, and KG findings into the `notes` field of the matching task in `.agents/workflow/plans/<plan-id>/TASKS.yaml`. The worker reads these at session start — do not rely on chat memory for load-bearing context.
