# Loop State

Last updated: 2026-04-17
Iteration: 40 (orchestrator pass)

## Current Position

Orchestrator pass — 2026-04-17:
- **Plans / tasks bundled (this run):** `agent-resource-lifecycle` / `agents-import` → `del-agents-import-1776434328.yaml`; `loop-agent-pipeline` / `p1-pipeline-control` → `del-p1-pipeline-control-1776434329.yaml`
- **Active delegations:** 2 (under `RALPH_MAX_PARALLEL_WORKERS=3`; third slot intentionally unused — see Loop Health)
- **Decision:** Confirmed both bundles proceed — scopes are **disjoint** (`commands/agents.go` + tests vs `commands/workflow.go` + tests + `bin/tests/ralph-pipeline` + `ralph-orchestrate`). No additional fanout: `agents-remove` and any task that edits `commands/agents.go` must wait for `agents-import`; downstream pipeline tasks that touch `commands/workflow.go` must wait for `p1-pipeline-control`.

## Loop Health

- **`workflow next` vs canonical TASKS.yaml:** `workflow next` now **locks to plan ids that have active delegations** (pending/active contract), so it will not jump to another plan while work is in-flight elsewhere. Optional **`workflow next --plan <id>`** scopes to one plan. Canonical TASKS.yaml remains authoritative for task status.
- **`workflow orient` vs checkpoint:** If orient still warns checkpoint stale vs branch tip, treat `next_action` from canonical plan + TASKS.yaml as authoritative.
- **Parallelism:** `agents-import` merge-back exists for parent review; **`p1-pipeline-control` merge-back** is at `.agents/active/merge-back/p1-pipeline-control.md` — still **in_progress** in YAML until orchestrator runs delegation closeout + advance. `agents-remove` remains **serialized** after import closes.
- **Fanout / pipeline:** `workflow fanout` creates `.agents/active/verification/<task_id>/` before dispatch; TDD gate blocks Go-only write_scope without adjacent `*_test.go` unless `--skip-tdd-gate`; **`--verifier-retry-max`** maps to bundle `primary_chain_max`; **`RALPH_VERIFIER_RETRY_MAX`** in `ralph-orchestrate` forwards the flag.

## Next Iteration Playbook

1. **Orchestrator:** Review `merge-back/agents-import.md` and **`merge-back/p1-pipeline-control.md`** → `workflow advance` + `workflow delegation closeout` per bundle when satisfied.
2. **After `agents-import` is completed in YAML:** `workflow fanout` for `agents-remove` (same write_scope family), unless branch already satisfies removal work.
3. **After `p1-pipeline-control` closes:** Schedule `p2-impl-agent-surface` or next unblocked pipeline task — avoid parallel `commands/workflow.go` edits with successors until the parent advances p1.
4. **Evidence:** `go run ./cmd/dot-agents workflow tasks loop-agent-pipeline`; `go test ./commands -run 'Fanout_|SelectNext'` for this slice.

## Scenario Coverage

| Family | Last exercised |
|--------|----------------|
| orchestrator-selection | 2026-04-17 — dual-plan fanout: `agents-import` + `p1-pipeline-control` (disjoint scopes) |
| delegation-lifecycle | Active: 2 bundles (`agents-import`, `p1-pipeline-control`); `agents-remove` queued post-import |

## Command Coverage

| Command | Tested | Last iteration |
|---------|--------|------------------|
| `workflow orient` | yes | 40 |
| `workflow next` | yes | 40 |
| `workflow tasks agent-resource-lifecycle` | yes | 40 |
| `workflow tasks loop-agent-pipeline` | yes | 36 |
| `workflow merge-back` (p1-pipeline-control) | yes | 36 |

## Iteration Log

_(Workers append here; orchestrator does not replace Current Position from worker turns.)_
