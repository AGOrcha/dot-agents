# Worker Mode Comparison: Script vs Pattern E

## Goal

Compare `ralph-worker.sh` (script worker) against Pattern E (Claude Code Agent tool subagent)
on equivalent tasks. Metrics to compare:

| Metric | Script | Pattern E |
|--------|--------|-----------|
| worker_iterations | ? | ? |
| merge_back_status | ? | ? |
| persisted_via_workflow_commands | ? | ? |
| context_tokens_approx | n/a | ? |
| wall time (approx) | ? | ? |

## How to populate this table

1. **Script run (clean):**
   ```bash
   # For a specific bounded implementation task:
   RALPH_FANOUT_PLAN=<plan-id> \
   RALPH_FANOUT_TASK=<task-id> \
   RALPH_FANOUT_WRITE_SCOPE="<comma-separated-paths>" \
   RALPH_ITERATIONS=5 \
   ./bin/tests/ralph-pipeline
   # Then read metrics.json from .ralph-loop-streams/run-<timestamp>/metrics.json
   # token_detail per task is populated from .ralph-loop-streams/run-*/workers/*/worker-iter-*.ndjson
   ```
   Requirements: `RALPH_NO_LOG` must NOT be set (default 0); log dir is created automatically.

2. **Pattern E run:** orchestrator session → fanout → `Agent(...)` call → write Pattern E metrics manually
   (see `orchestrator-session-start/instructions/workflow.md` → Pattern E metrics capture)

## Choosing equivalent tasks for A/B comparison

For a fair comparison pick a task that is:
- **Bounded implementation** (not architectural/decision) — produces actual code + tests
- **≤5 files write_scope** or a single tightly-scoped directory
- **Same plan** if possible, same task type (TypeScript command additions work well)

Phase-3 (Pattern E) was ideal: 8 specific commands, 33 tests, single `ports/typescript/src/commands/` dir.
Phase-4 (script) was a poor match: architectural decision task, broad `docs/` + TS dirs.

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
2. `ralph-worker`: `import yaml,sys` outside the `try` block — `ModuleNotFoundError` fired before fallback; fixed with `import re,sys` at top and `try: import yaml` inside
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
- ~~Re-run script worker on a bounded implementation task (same write_scope size as phase-3) with `RALPH_NO_LOG=0` to capture token/timing metrics~~ **DONE** — A/B run on 2026-04-14, see Run 3 below
- ~~Compare context_tokens_approx between Pattern E (~77.7k) vs script worker on equivalent task~~ **DONE** — results stark: see A/B table below

## A/B Direct Comparison — 2026-04-14 (same plan, same iteration budget, non-overlapping tasks)

### Setup
Both workers ran **in parallel** on the `typescript-port` plan. Tasks were designed to be equivalent in scope and complexity:
- **Pattern E** → `ts-ab-workflow-commands` (write: `commands/workflow.ts`, `tests/workflow.test.ts`)
- **Script worker** → `ts-ab-kg-commands` (write: `commands/kg.ts`, `tests/kg.test.ts`)

Both tasks: implement read-only TS command stubs with positive + negative tests. Same write_scope size (2 files), same task type (bounded implementation).

### Pattern E run — 2026-04-14T19:27:37Z

- plan_id: typescript-port
- task_id: ts-ab-workflow-commands
- worker_iterations: 1
- merge_back_status: present (pass)
- persisted_via_workflow_commands: yes
- total_tokens: 59,993 (approx — reported by subagent)
- tool_uses: 44
- duration_ms: 376,772 (~6.3 min)
- task_result: `runWorkflowOrient`, `runWorkflowTasks`, `runWorkflowHealth` implemented; 18 new tests; suite 92/92 passing
- commit: b467cc0
- metrics_file: .ralph-loop-streams/pattern-e-ab-20260414-192737/metrics.json

### Script worker run — 2026-04-14T19:27:37Z (parallel to Pattern E)

- plan_id: typescript-port
- task_id: ts-ab-kg-commands
- worker_iterations: 1
- merge_back_status: present (pass)
- persisted_via_workflow_commands: yes
- total_tokens: 2,710,560 (207,026 input + 2,503,534 cache read)
- tool_uses: 63
- duration_ms: 263,614 (~4.4 min)
- task_result: `runKgHealth`, `runKgQuery` stub implemented; 8 new tests; suite 74/74 passing
- commit: 9f9532e + eecf77b
- metrics_file: .ralph-loop-streams/script-ab-20260414-*/metrics.json

### A/B Comparison Table

| Metric | Pattern E | Script worker |
|--------|-----------|---------------|
| task | ts-ab-workflow-commands | ts-ab-kg-commands |
| write_scope files | 2 | 2 |
| task type | bounded implementation | bounded implementation |
| iterations to merge-back | 1 | 1 |
| **total tokens** | **~60k** | **~2.71M** |
| input tokens | ~60k | ~207k |
| cache read tokens | (included above) | ~2.5M |
| tool uses | 44 | 63 |
| wall time | ~6.3 min | ~4.4 min |
| merge_back_status | present/pass | present/pass |
| persisted_via_workflow_commands | yes | yes |
| tests added | 18 | 8 |
| suite passing | 92/92 | 74/74 |

### Key Findings

1. **Token efficiency**: Pattern E used ~45x fewer tokens than script worker (60k vs 2.71M). The dominant cost in script worker is cache read — the `loop-state.md` file and full project overlay are loaded verbatim into every agent call, inflating context by ~2.5M tokens per iteration.

2. **Implementation density**: Pattern E produced 18 tests vs script worker's 8 — more thorough coverage per token spent.

3. **Wall time**: Script worker was slightly faster (~4.4 min vs ~6.3 min) despite 45x more tokens, likely because cache hits are cheaper per wall-clock ms than fresh computation.

4. **Role isolation**: Both modes correctly honored write_scope. Neither crossed into the other task's files.

5. **Merge-back quality**: Both submitted clean merge-backs via `workflow merge-back` CLI. No anti-pattern detected in either mode.

### Root cause of script worker token bloat

`ralph-worker` builds a prompt that inlines the full content of:
- `$WORKER_OVERLAY` (`active.loop.md`) — this is the project overlay passed verbatim
- `$LOOP_PROFILE_FILE` (`loop-worker.md`) — global worker profile

The `active.loop.md` file includes a copy of `loop-state.md` which contains the entire orchestration history. On this repo that file is large (~2500+ lines), which dominates the context window on every iteration. Pattern E workers receive a lean bundle reference and read only what they need via tool calls.

### Recommendation

For bounded implementation tasks (≤5 files, single directory):
- **Pattern E preferred** — ~45x token savings, higher test coverage, no context bloat
- Script worker suitable when Pattern E is unavailable (no interactive session) or for tasks where wall time matters more than cost

Mitigation for script worker token bloat: trim `active.loop.md` to a minimal overlay (remove full loop-state.md inclusion), or use `--context-file` selectively rather than full overlay injection. This could reduce script worker context to ~200k-500k, closing most of the gap.

## A/B Direct Comparison — REPLACE_DATE (same plan, same task, same base commit, different worktrees)

  ### Setup
  Both workers ran in parallel in separate sibling worktrees from the same base commit.

  - base_commit: `REPLACE_SHA`
  - plan_id: `loop-runtime-refactor`
  - task_id: `phase-5d-iter-log-schema`
  - write_scope: `schemas/workflow-iter-log.schema.json`, `commands/workflow.go`, `commands/workflow_test.go`, `.agents/
  active/iteration-log/`

  ### Pattern E run — REPLACE_TIMESTAMP

  - worker_mode: subagent
  - worker_iterations: REPLACE
  - merge_back_status: REPLACE
  - persisted_via_workflow_commands: REPLACE
  - total_tokens: REPLACE_OR_NULL
  - tool_uses: REPLACE_OR_NULL
  - duration_ms: REPLACE_OR_NULL
  - task_result: REPLACE
  - commit: REPLACE
  - metrics_file: `.ralph-loop-streams/pattern-e-ab-REPLACE/metrics.json`

  ### Script run — REPLACE_TIMESTAMP

  - worker_mode: script
  - worker_iterations: REPLACE
  - merge_back_status: REPLACE
  - persisted_via_workflow_commands: REPLACE
  - total_tokens: REPLACE_OR_NULL
  - tool_uses: REPLACE_OR_NULL
  - duration_ms: REPLACE_OR_NULL
  - task_result: REPLACE
  - commit: REPLACE
  - metrics_file: `.ralph-loop-streams/script-ab-REPLACE/metrics.json`

  ### Comparison Table

  | Metric | Pattern E | Script worker |
  |--------|-----------|---------------|
  | task | `phase-5d-iter-log-schema` | `phase-5d-iter-log-schema` |
  | iterations to merge-back | REPLACE | REPLACE |
  | total tokens | REPLACE | REPLACE |
  | tool uses | REPLACE | REPLACE |
  | wall time | REPLACE | REPLACE |
  | merge_back_status | REPLACE | REPLACE |
  | persisted_via_workflow_commands | REPLACE | REPLACE |
  | commit | REPLACE | REPLACE |

  ### Notes

  - Same base commit, same task, separate worktrees.
  - Do not compare results from the main checkout with worktree runs.
  - `workflow advance` currently has a false-success bug; verify `TASKS.yaml` on disk after closeout.
