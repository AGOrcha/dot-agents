# Bundle → execution (loop-worker + project loop)

Use this when a **delegation bundle already exists** (for example after `workflow fanout` or a harness script). This is the **implementation worker** path—not the orchestrator "pick the next task" path.

## The three layers (read in order)

Do not improvise handoff from chat. The bundle points at the same stack `workflow fanout` records:

| Layer | Typical path | Role |
|-------|----------------|------|
| **1. Bundle (task truth)** | `.agents/active/delegation-bundles/<delegation_id>.yaml` | `scope.write_scope`, `prompt`, `context.required_files`, `verification`, `closeout.worker_must` |
| **2. Global worker profile** | `~/.agents/profiles/loop-worker.md` | Cross-repo habits: respect `write_scope`, tests-first, merge-back discipline |
| **3. Project loop overlay** | `.agents/active/active.loop.md` (and any paths under `worker.project_overlay_files` / `prompt.prompt_files` in the bundle) | Repo dogfood rhythm, loop-state, CLI inventory |

Also read **`.agents/active/delegation/<task-id>.yaml`** for contract title, success criteria, and `may_mutate_workflow_state`.

## Execution checklist (worker)

1. Open the **bundle YAML** and note `plan_id`, `task_id`, `delegation_id`, and every path under `prompt`, `context`, and `scope`.
2. Load **`loop-worker.md`** and the **project overlay** (`active.loop.md` or bundle-listed files) before editing code.
3. Implement **only** under `scope.write_scope`. If something requires changes outside it, stop and escalate (parent must re-scope or fan out again).
4. Run **focused tests** for touched packages, then broader checks as the overlay prescribes.
5. **Close out as the worker** (not as the parent orchestrator):
   - `da workflow verify record` (outcome + summary)
   - `da workflow checkpoint` (iteration message + verification status)
   - `da workflow merge-back` with `--task <task-id>` and evidence summary
   Do **not** run `workflow advance` yourself unless you are the parent owning the plan.

## Parent (after merge-back)

- Review merge-back, then `workflow delegation closeout` / `workflow advance` as your process requires.
  See `instructions/workflow.md` for full command examples.

## Relationship to other skills

- **`iteration-close`** — same verify → checkpoint → merge-back ordering; use when persisting loop state after implementation.
- **`orchestrator-session-start`** — chooses work and may **create** fanout; it is not the implementation pass after the bundle exists.
