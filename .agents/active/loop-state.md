# Loop State

Last updated: 2026-04-17
Iteration: 41 (orchestrator pass)

## Current Position

Orchestrator pass — 2026-04-17:
- **Bundles confirmed (this run):** `agent-resource-lifecycle` / `agents-remove` → `del-agents-remove-1776438876.yaml`; `loop-agent-pipeline` / `p2-impl-agent-surface` → `del-p2-impl-agent-surface-1776438876.yaml`; `loop-agent-pipeline` / `p3a-result-schema` → `del-p3a-result-schema-1776438877.yaml`. **RALPH_MAX_PARALLEL_WORKERS=3** — slots full; no additional fanout.
- **`workflow next` (canonical):** `agent-resource-lifecycle` / `agents-import` — **in_progress** (same `commands/agents.go` scope as `agents-remove`; **serialize**).
- **Decision:** **Proceed now:** only **`p2-impl-agent-surface`** (disjoint from `commands/workflow.go` and `commands/agents.go`). **Park / do not dispatch workers** for **`agents-remove`** until `agents-import` closes; **`p3a-result-schema`** until **`p1-pipeline-control`** closes (both edit `commands/workflow.go`). TASKS.yaml notes updated with bundle ids and gating.

## Loop Health

- **`workflow next` vs active bundles:** Canonical selector still surfaces **`agents-import`** (in_progress). **`agents-remove`:** worker merge-back written (`.agents/active/merge-back/agents-remove.md`); implementation complete — **parent** advances + `workflow delegation closeout` after review (still serialize with **`agents-import`** on the same Go files until import task advances). **`p3a-result-schema`:** worker merge-back written (`.agents/active/merge-back/p3a-result-schema.md`, verification result under `.agents/active/verification/p3a-result-schema/merge-back.result.yaml`) — **parent** `workflow advance` + `workflow delegation closeout` after review.
- **`workflow orient` vs checkpoint:** Checkpoint `next_action` can lag; canonical plan focus (`agents remove…` / pipeline focus) reflects newer orchestrate commits — use TASKS.yaml + bundle gating.
- **Parallelism:** Run **`p2`** worker when ready. **`p3a-result-schema`** implementation is in merge-back review (no longer blocked on **`p1`** for this slice — parent reconciles TASKS/deps). Pending merge-backs: **`agents-remove`**, **`p3a-result-schema`**, plus prior queue per `orient`.
- **Fanout / pipeline:** `workflow fanout` creates `.agents/active/verification/<task_id>/` before dispatch; TDD gate blocks Go-only write_scope without adjacent `*_test.go` unless `--skip-tdd-gate`; **`--verifier-retry-max`** maps to bundle `primary_chain_max`; **`RALPH_VERIFIER_RETRY_MAX`** in `ralph-orchestrate` forwards the flag.

## Next Iteration Playbook

1. **Dispatch / run:** **`p2-impl-agent-surface`** worker only (`del-p2-impl-agent-surface-1776438876`) — unblock path that does not touch `workflow.go` or `agents.go`.
2. **Parent closeout (agents):** Review **`.agents/active/merge-back/agents-remove.md`**, then **`workflow advance agent-resource-lifecycle agents-remove completed`** and **`workflow delegation closeout`** when accepting the delegation. Complete **`agents-import`** merge-back → advance on that task (same file scope — order per your review). Complete **`p1-pipeline-control`** merge-back → advance when ready.
3. **Parent closeout (pipeline p3a):** Review **`.agents/active/merge-back/p3a-result-schema.md`** and verification YAML, then **`workflow advance loop-agent-pipeline p3a-result-schema completed`** and **`workflow delegation closeout --plan loop-agent-pipeline --task p3a-result-schema --decision accept`** when accepting.
4. **Orchestrator after workers:** `workflow verify record`, `workflow merge-back`, `workflow advance`, `workflow delegation closeout` per completed bundle; re-run `workflow next --plan …` if needed.
5. **Evidence:** `go run ./cmd/dot-agents workflow tasks agent-resource-lifecycle`; `go run ./cmd/dot-agents workflow tasks loop-agent-pipeline`; `go test ./commands -run 'RemoveAgentIn|Agents'` or `go test ./commands -run Workflow` as appropriate.

## Scenario Coverage

| Family | Last exercised |
|--------|----------------|
| orchestrator-selection | 2026-04-17 — triple fanout at cap: `agents-remove`, `p2-impl-agent-surface`, `p3a-result-schema`; gating: only p2 runnable vs p1/agents-import |
| delegation-lifecycle | Active: 3 bundles; 2 parked on `agents-import` / `p1` blockers; merge-back queue per orient |

## Command Coverage

| Command | Tested | Last iteration |
|---------|--------|------------------|
| `workflow orient` | yes | 41 |
| `workflow next` | yes | 41 |
| `workflow tasks agent-resource-lifecycle` | yes | 41 |
| `workflow tasks loop-agent-pipeline` | yes | 41 |
| `workflow merge-back` (agents-remove) | yes | 38 |
| `workflow merge-back` (p1-pipeline-control) | no | — |
| `workflow merge-back` (p3a-result-schema) | yes | 39 |

## Iteration Log

_(Workers append here; orchestrator does not replace Current Position from worker turns.)_
