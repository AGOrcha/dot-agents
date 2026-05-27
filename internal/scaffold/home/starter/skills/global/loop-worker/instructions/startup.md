# Loop Worker — Startup

Cold-start with a delegation bundle as your only context. Three steps, no more.

## Step 1 — Read the bundle

Read the bundle YAML at the path given in your invocation prompt:

```
plan_id:        <string>        the canonical plan this task belongs to
task_id:        <string>        the specific task you are implementing
write_scope:    [list]          files/directories you are allowed to modify
feedback_goal:  <string>        the concrete question your CLI evidence must answer
context_files:  [list]          additional files to read before starting
```

Do NOT derive plan_id or task_id from workflow orient or workflow next — the bundle is authoritative.

## Step 2 — Confirm task status

```bash
go run ./cmd/dot-agents workflow tasks <plan_id from bundle>
```

Confirm:
- Your task_id is present and in status `in_progress` or `pending`
- Its dependencies are met (status `completed` or no blocking deps)

If the task is already `completed`, stop — do not implement a completed task.

## Step 3 — Check dirty state

```bash
git status --short
```

If uncommitted changes from a prior iteration exist:
- If they belong to your write_scope: review, stage, and commit them before starting
- If they belong outside your write_scope: do not touch them; note in your iteration log

## What NOT to do at startup

- Do NOT run `workflow orient` — it's an orchestrator tool
- Do NOT run `workflow next` — your bundle assigns the task, not the selector
- Do NOT run `workflow status` — stale checkpoint, adds no value for a worker
- Do NOT read `.agents/active/loop-state.md ## Current Position` to decide what to work on — read your bundle
