# Loop State

Last updated: 2026-04-18
Iteration: 47 (orchestrator)

## Current Position

Orchestrator pass — 2026-04-18:
- **Bundle confirmed (this run):** `loop-agent-pipeline` / **`p4-review-agent`** → `.agents/active/delegation-bundles/del-p4-review-agent-1776528758.yaml` (**proceed** — **`p3f-streaming-verifier`** is **completed**; **`p4`** is **`in_progress`** with bounded review-agent + workflow + schema scope; this slice owns concurrent edits to **`docs/LOOP_ORCHESTRATION_SPEC.md`** and **`commands/workflow.go`** until merge-back).
- **`workflow next` vs in-flight `p4`:** CLI reports **`p8-orchestrator-awareness`** (first **pending** task; **`p4`** is **`in_progress`** so it is not the pending queue head). **Do not** fan out **`p8`** while **`p4`** is open — **shared `docs/LOOP_ORCHESTRATION_SPEC.md`**; **canonical TASKS.yaml notes + orchestrator serialization policy** win over **`workflow next`** for dispatch.
- **Parallelism:** `RALPH_MAX_PARALLEL_WORKERS=3` (this pass); **1** active delegation (**`p4`**); **no additional** `workflow fanout` emitted this pass.

## Loop Health

- **`workflow orient` vs checkpoint:** Checkpoint `next_action` can lag git + canonical focus — **canonical PLAN.yaml / TASKS.yaml** win for focus text.
- **`workflow next` vs `p4` / `p8`:** Historical note (pre-**`p4`** close): orchestrator could defer **`p8`** for **`docs/LOOP_ORCHESTRATION_SPEC.md`** overlap — **resolved** for **`p8` worker slice**; **`p8`** **merge-back** (iter-48) documents **D5** in **`ralph-orchestrate`**.
- **D5 in scripts:** `bin/tests/ralph-orchestrate` no longer passes **`active.loop.md`** as both `--project-overlay` and `--prompt-file` — default **inline `--prompt`**, optional **`.agents/prompts/loop-worker.project.md`** (or **RALPH_DELEGATION_PROMPT_FILE**) when present and not the overlay path.
- **`p6` / `p7` / `p5` vs `p4` pending:** Historical DAG text in TASKS may still be stale; **canonical** **`workflow tasks`** wins.

## Next Iteration Playbook

1. **Parent:** review **`.agents/active/merge-back/p8-orchestrator-awareness.md`**, then **`workflow advance loop-agent-pipeline p8-orchestrator-awareness completed`**, then **`workflow delegation closeout --plan loop-agent-pipeline --task p8-orchestrator-awareness --decision accept`**, refresh **`## Current Position`** in this file.
2. **Then:** pick next pending (e.g. **`p7-post-closeout`**) with **`workflow next`**, **`workflow tasks`**, and orchestrator pass as needed.
3. **Evidence:** `go run ./cmd/dot-agents workflow tasks loop-agent-pipeline`; `go run ./cmd/dot-agents workflow orient`.

## Scenario Coverage

| Family | Last exercised |
|--------|----------------|
| orchestrator-selection | 2026-04-18 — confirmed **`p4`** bundle after **`p3f`** completion; deferred **`p8`** fanout despite **`workflow next`** |
| delegation-lifecycle | 2026-04-18 — TASKS notes aligned to **`p4`** bundle path + **`p8`** deferral rationale |

## Command Coverage

| Command | Tested | Last Iteration |
|---------|--------|----------------|
| `workflow orient` | yes | 47 |
| `workflow next` | yes | 47 |
| `workflow tasks loop-agent-pipeline` | yes | 47 |

## Iteration Log

_(Workers append here; orchestrator does not replace Current Position from worker turns.)_
