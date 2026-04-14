# Worker Mode Comparison: Script vs Pattern E

## Goal

Compare `ralph-cursor-loop.sh` (script worker) against Pattern E (Claude Code Agent tool subagent)
on equivalent tasks. Metrics to compare:

| Metric | Script | Pattern E |
|--------|--------|-----------|
| worker_iterations | ? | ? |
| merge_back_status | ? | ? |
| persisted_via_workflow_commands | ? | ? |
| context_tokens_approx | n/a | ? |
| wall time (approx) | ? | ? |

## How to populate this table

1. **Script run:** `./bin/tests/ralph-pipeline` → read `metrics.json` from `.ralph-loop-streams/run-*/`
2. **Pattern E run:** orchestrator session → fanout → `Agent(...)` call → write Pattern E metrics manually
   (see `orchestrator-session-start/instructions/workflow.md` → Pattern E metrics capture)

Run both modes on the **same task** (same plan_id + task_id, same write_scope) for a meaningful comparison.

## Runs

*(populate after first script and Pattern E runs on the same task)*

### Script run — <timestamp>

- plan_id:
- task_id:
- worker_iterations:
- merge_back_status:
- persisted_via_workflow_commands:
- metrics_file:

### Pattern E run — <timestamp>

- plan_id:
- task_id:
- worker_iterations:
- merge_back_status:
- persisted_via_workflow_commands:
- context_tokens_approx:
- metrics_file:

## Analysis

*(write after both runs)*
