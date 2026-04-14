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

### Pattern E run — 2026-04-14T08:43:01Z

- plan_id: typescript-port
- task_id: phase-3-stage1-command-mvp
- worker_iterations: 1
- merge_back_status: present (pass)
- persisted_via_workflow_commands: yes
- context_tokens_approx: 77,705
- tool_uses: 64
- duration_ms: 385,596 (~6.4 min)
- task_result: 63/63 TypeScript tests pass; 8 MVP commands + cli.ts + 33 new tests
- commit: b6937fb
- metrics_file: .ralph-loop-streams/pattern-e-20260414-084301/metrics.json

### Script run — (pending)

*(Run ./bin/tests/ralph-pipeline on an equivalent task to capture script-mode baseline)*

## Analysis

**Pattern E first result:**

| Metric | Pattern E | Script (baseline) |
|--------|-----------|-------------------|
| iterations to merge-back | 1 | TBD |
| total tokens | ~77.7k | TBD |
| tool uses | 64 | TBD |
| wall time | ~6.4 min | TBD |
| merge_back_status | present/pass | TBD |
| persisted_via_workflow_commands | yes | TBD |

**Observations from first Pattern E run:**
- Cold-start capable — worker oriented correctly from bundle + TASKS.yaml notes alone
- Stayed within write_scope (bundle had empty write_scope field; worker correctly used TASKS.yaml constraints)
- Single iteration to complete all 8 commands — no retry needed
- `persisted_via_workflow_commands: yes` — the anti-pattern we were targeting did not occur
- Empty `write_scope` in bundle is a gap to address (fanout doesn't auto-pull from task definition)
