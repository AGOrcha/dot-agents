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
stage:          <absent>        typed stages are not executed by loop-worker
```

Do NOT derive plan_id or task_id from workflow orient or workflow next — the bundle is authoritative.

If the bundle assigns a typed `stage` or `role` (`impl`, verifier, or
reviewer), stop. The parent must dispatch a named staged agent with
stage-safe instructions; the legacy `loop-worker` path would inject
full-slice merge-back behavior into a child that should instead emit a typed
artifact and stop.

## Step 2 — Confirm task status

```bash
da workflow tasks <plan_id from bundle>
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

## Step 4 — Write the stop-gate sentinel

After the 3-step startup (you now know `plan_id`, `task_id`, and the delegated
`write_scope` from the bundle), write the sentinel **once**, before making any
scoped edit or running any workflow closeout command:

```bash
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"

da workflow hook-sentinel write loop-worker \
  --run-id "$RUN_ID" \
  --plan <plan_id from bundle> \
  --task <task_id from bundle> \
  --agent-type loop-worker \
  --write-scope <path-or-glob from bundle> \
  --write-scope <next write_scope entry>   # repeat for every entry
```

Pass one `--write-scope` flag per entry in the bundle's `write_scope` list,
verbatim. The SubagentStop gate diffs the files your subagent changed against
this recorded scope and hard-remediates any edit outside it; the sentinel is the
single source of truth for "what was this worker allowed to touch?" (D6 — the
gate self-filters on the sentinel's `agent_type == "loop-worker"`). If you never
write the sentinel, the gate exits 0 and no scope enforcement runs for your
turn.

## What NOT to do at startup

- Do NOT run `workflow orient` — it's an orchestrator tool
- Do NOT run `workflow next` — your bundle assigns the task, not the selector
- Do NOT run `workflow status` — stale checkpoint, adds no value for a worker
- Do NOT read `.agents/active/loop-state.md ## Current Position` to decide what to work on — read your bundle
