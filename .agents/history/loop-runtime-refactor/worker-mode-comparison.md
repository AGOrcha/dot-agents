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

### Script run — 2026-04-14T19:02:33Z

- plan_id: typescript-port
- task_id: phase-4-advanced-surface-decision
- worker_iterations: 1 (budget was 1)
- merge_back_status: present (pass)
- persisted_via_workflow_commands: yes
- context_tokens_approx: unknown (output stream lost — SIGPIPE from test harness)
- tool_uses: unknown
- duration_ms: unknown
- task_result: boundary decision documented in TASKS.yaml (option 2: read-only workflow future); no docs/TS implementation artifacts created in 1 iteration
- commit: n/a (uncommitted at time of merge-back)
- metrics_file: n/a (RALPH_NO_LOG=1 during test run)

**Pipeline bugs found and fixed during this run:**
1. `ralph-orchestrate`: `--project-overlay` absolute path double-prefixed — `filepath.Join(repoRoot, abs)` in Go concatenates; fixed by stripping `$REPO_ROOT/` prefix
2. `ralph-cursor-loop`: `import yaml,sys` outside the `try` block — `ModuleNotFoundError` fired before fallback; fixed with `import re,sys` at top and `try: import yaml` inside
3. `ralph-pipeline`: no fallback when BUNDLES empty after parsing (re-run case where contract already exists); added delegation-bundles/ scan fallback

## Analysis

**Comparison (same plan, equivalent task type, 1 iteration each):**

| Metric | Pattern E | Script worker |
|--------|-----------|---------------|
| task | phase-3-stage1-command-mvp | phase-4-advanced-surface-decision |
| iterations to merge-back | 1 | 1 |
| total tokens | ~77.7k | unknown (stream lost) |
| tool uses | 64 | unknown |
| wall time | ~6.4 min | unknown |
| merge_back_status | present/pass | present/pass |
| persisted_via_workflow_commands | yes | yes |
| implementation artifacts | 8 commands + 33 new tests | YAML/loop-state notes only (no docs/TS files) |
| tests passing | 63/63 | 66/66 (no new tests added) |

**Observations from Pattern E run:**
- Cold-start capable — worker oriented correctly from bundle + TASKS.yaml notes alone
- Stayed within write_scope (bundle had empty write_scope field; worker correctly used TASKS.yaml constraints)
- Single iteration to complete all 8 commands — no retry needed
- `persisted_via_workflow_commands: yes` — the anti-pattern we were targeting did not occur
- Empty `write_scope` in bundle is a gap to address (fanout doesn't auto-pull from task definition)

**Observations from script worker run:**
- `persisted_via_workflow_commands: yes` — merge-back submitted correctly via CLI
- 1 iteration budget was insufficient for phase-4 (architectural decision + docs + CLI help + tests): only boundary annotation in YAML was produced
- Pipeline had 3 bugs that blocked the full E2E run; all fixed before the direct worker invocation
- Token/timing data lost due to test harness pipe truncation (RALPH_NO_LOG=1 + `| head -5` SIGPIPE); script worker needs `RALPH_NO_LOG=0` + log dir to capture metrics
- Task nature matters: phase-3 (bounded implementation) vs phase-4 (architecture + docs) — not an apples-to-apples comparison for throughput; choose same task type for future A/B

**To-do for complete comparison:**
- Re-run script worker on a bounded implementation task (same write_scope size as phase-3) with `RALPH_NO_LOG=0` to capture token/timing metrics
- Compare context_tokens_approx between Pattern E (~77.7k) vs script worker on equivalent task
