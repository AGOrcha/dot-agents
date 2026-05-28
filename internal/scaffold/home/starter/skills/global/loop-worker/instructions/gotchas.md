# Gotchas: Loop Worker

Worker-specific failure modes. Read before implementing.

## Typed Stage Misrouting

- **Using `loop-worker` for an ISP typed stage** — This skill is legacy/full-slice compatibility only. If the bundle identifies an `impl`, verifier, or reviewer stage, return it to the parent for named stage dispatch; do not inject `/iteration-close` or merge-back into that child.

## Scope Creep

- **Touching files outside write_scope** — the bundle defines your boundary. If a dependency you need is outside write_scope, stop. Write a fold-back observation, mark the task paused, and hand back to the parent. Do not expand scope unilaterally.
- **Picking a second task** — you own ONE task (from the bundle). When merge-back is written, you are done. Do not call `workflow next` to find more work.

## Wrong Closeout Command

- **Running `workflow advance` as the worker** — advance is for direct,
  non-delegated completion, not this worker or its delegation parent. Workers
  run `workflow merge-back`; the parent reviews it and runs delegation
  closeout. If you advance directly, the delegation contract is violated and
  the parent has no signal to review.
- **Skipping verify + checkpoint before merge-back** — the minimal sequence is `verify record` → `checkpoint` → `merge-back`. Skipping verify leaves no audit trail. The parent cannot accept/reject without it.

## Orchestrator State (not your scope)

- **Updating `## Current Position` in loop-state.md** — Current Position is orchestrator scope. Workers write to `## Iteration Log` and `## Next Iteration Playbook` ONLY.
- **Running `workflow orient` or `workflow status` at startup** — these are orchestrator startup tools. Your context is the bundle, not the repo-wide state.

## CLI Broken Fallback

- **If `da` won't build or the binary is missing** — mark `persisted_via_workflow_commands: paused — <reason>` in your iteration log. Create a fold-back: `go run ./cmd/da workflow fold-back create --plan <id> --observation '[tool-bug]: <detail>' --propose`. Continue with implementation; run deferred persist commands at the start of the next iteration.

## Merge-Back Ownership

- **Parent runs `workflow delegation closeout` after reviewing your merge-back** — accepted closeout completes the delegated task. The parent does not also run `workflow advance`; that command is for direct non-delegated work. Your job ends when `.agents/active/merge-back/<task_id>.md` is written.
