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

## Fanout command

```bash
da workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --write-scope "<scope>" \
  --owner "<worker-name>" \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --feedback-goal "<concrete question evidence must answer>" \
  --prompt "<inline instruction>" \
  --prompt-file .agents/prompts/loop-worker.project.md \
  --context-file .agents/active/loop-state.md \
  --context-file .agents/workflow/plans/<plan_id>/TASKS.yaml \
  [--context-file .agents/workflow/evidence/<task_id>.scope.yaml]  # when confidence >= medium
  --selection-reason "<why this task now>"
```

## Parallel fanout mode

When `max_batch > 1` AND parallel mode is active: fan out one bundle per task in `max_batch`. Keep write_scopes non-overlapping. If two tasks in `max_batch` have unexpected overlapping scopes, defer the conflicting task to the next pass.

## Write TASKS.yaml notes before handoff

Write constraints, risks, and KG findings into the `notes` field of the matching task in `.agents/workflow/plans/<plan-id>/TASKS.yaml`. The worker reads these at session start — do not rely on chat memory for load-bearing context.
