# Gotchas: Loop Worker

Worker-specific failure modes. Read before implementing.

## Scope Creep

- **Touching files outside write_scope** — the bundle defines your boundary. If a dependency you need is outside write_scope, stop. Write a fold-back observation, mark the task paused, and hand back to the parent. Do not expand scope unilaterally.
- **Picking a second task** — you own ONE task (from the bundle). When merge-back is written, you are done. Do not call `workflow next` to find more work.

## Wrong Closeout Command

- **Running `workflow advance` as the worker** — advance is for the parent/orchestrator after reviewing your merge-back. Workers run `workflow merge-back`. If you advance directly, the delegation contract is violated and the parent has no signal to review.
- **Skipping verify + checkpoint before merge-back** — the minimal sequence is `verify record` → `checkpoint` → `merge-back`. Skipping verify leaves no audit trail. The parent cannot accept/reject without it.

## Orchestrator State (not your scope)

- **Updating `## Current Position` in loop-state.md** — Current Position is orchestrator scope. Workers write to `## Iteration Log` and `## Next Iteration Playbook` ONLY.
- **Running `workflow orient` or `workflow status` at startup** — these are orchestrator startup tools. Your context is the bundle, not the repo-wide state.

## CLI Broken Fallback

- **If `dot-agents` won't build or the binary is missing** — mark `persisted_via_workflow_commands: paused — <reason>` in your iteration log. Create a fold-back: `go run ./cmd/dot-agents workflow fold-back create --plan <id> --observation '[tool-bug]: <detail>' --propose`. Continue with implementation; run deferred persist commands at the start of the next iteration.

## Merge-Back Ownership

- **Parent runs `workflow advance` and `workflow delegation closeout` after reviewing your merge-back** — you do not see those commands succeed. Your job ends when `.agents/active/merge-back/<task_id>.md` is written. Do not poll or wait for the parent to respond before stopping.
