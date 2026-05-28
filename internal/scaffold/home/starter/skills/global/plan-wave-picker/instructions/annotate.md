# Step 2: Annotate Tasks and Present Recommended Batch

For each task in `max_batch`, assign a readiness label based on `evidence_confidence`:

| evidence_confidence | readiness label | meaning |
|---|---|---|
| high | delegation-ready | Strong scope evidence. Fan out with confidence. |
| medium | delegation-ready | Reasonable scope evidence. Fan out, but worker should review sidecar. |
| low | delegation-possible | Thin scope evidence. Flag context gap; suggest worker reviews derive-scope output. |
| none | cautious | No scope evidence. Recommend running `workflow plan derive-scope` first if KG is ready. |

## Output format

Present a recommended batch summary:

```
Recommended batch (max_batch): N tasks

  [<plan_id>/<task_id>] <title>
    readiness: <label>
    evidence: <evidence_confidence>
    next step: <one-line guidance>
    write_scope: <scope or '[no write_scope declared]'>
```

One-line next-step guidance by label:

| label | next step |
|---|---|
| delegation-ready | Fan out via `workflow fanout` — sidecar context available. |
| delegation-possible | Fan out, but note thin context; worker should check scope before implementing. |
| cautious | Run `workflow plan derive-scope <plan_id> <task_id>` first, then re-check eligible. |

## Sidecar content for delegation-ready tasks

For tasks labeled `delegation-ready` (confidence `medium` or `high`), read the sidecar and surface key fields in the output:

```bash
cat .agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml
```

Add to the task's output block:
```
    sidecar:
      required_reads: <list or 'none'>
      decision_locks: <list or 'none'>
```

This gives the delegating orchestrator the context it needs to populate the bundle's `--context-file` and `--prompt` fields without re-reading the full sidecar themselves.

For `delegation-possible` (low confidence) or `cautious` (none): do not read or surface the sidecar — it either doesn't exist or doesn't contain reliable scope data.

## Cautions to surface

- **Empty write_scope** (`write_scope_declared: false`): append `[no write_scope declared]` and suggest the user adds a `--write-scope` flag or runs `derive-scope`.
- **All tasks cautious**: recommend a derive-scope pass before delegating anything.
- **max_batch smaller than total_eligible**: note that remaining tasks are deferred due to write_scope conflicts; they will be eligible after the current batch completes.

## Do not recompute

Do not attempt to:
- Re-rank tasks by priority (the binary already sorts in_progress before pending)
- Re-derive max_batch from the conflict graph (it is pre-computed)
- Suggest tasks outside `max_batch` as alternatives

If the user wants a different batch, they should re-run `workflow eligible` with a different `--limit` or adjust task `write_scope` values.
