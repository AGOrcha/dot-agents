# Step 1: Run Eligible and Extract max_batch

Call the dot-agents CLI to get the full annotated task set:

```bash
da --json workflow eligible --plan <scope>
```

Omit `--plan` to see all active plans. The `<scope>` should be the comma-separated plan IDs the user wants to focus on.

## Fields to extract

From the JSON output:

| Field | Use |
|---|---|
| `max_batch` | Pre-computed non-conflicting task IDs — this is the recommended batch. Do not recompute. |
| `total_eligible` | Total unblocked task count across all plans in scope |
| `max_parallel` | Size of max_batch |
| per-task `evidence_confidence` | `none\|low\|medium\|high` — drives readiness label in step 2 |
| per-task `has_evidence` | Whether a scope sidecar exists for this task |
| per-task `write_scope_declared` | False = empty write_scope (caution flag) |
| per-task `conflicts_with` | Which tasks this task conflicts with (for context only) |

## If total_eligible == 0

Surface the lock/pause: run `da workflow orient` and report the blocking state. Do not attempt to derive a batch manually.

## If max_batch is empty but total_eligible > 0

All unblocked tasks conflict with each other. Report this to the user: the orchestrator must pick one task at a time and proceed serialized.
