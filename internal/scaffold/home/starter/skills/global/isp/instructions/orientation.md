# ISP Step 1: Load Orientation Context

`orchestrator-session-start` should have already run `workflow eligible --json --plan <scope>` and presented the orientation summary. Use that output.

If the output was not passed in (or is stale), re-run:

```bash
go run ./cmd/dot-agents workflow eligible --json --plan <scope>
```

Key fields to extract from the JSON:
- `eligible_tasks` — full annotated task list
- `max_batch` — pre-computed non-conflicting task IDs to fan out in this pass
- `total_eligible` — how many tasks are unblocked
- per-task: `has_evidence`, `evidence_confidence` (`none|low|medium|high`), `write_scope`, `write_scope_declared`, `conflicts_with`

## Active delegation check

```bash
ls .agents/active/delegation-bundles/
```

If any bundle exists for a task in `max_batch`, do **not** re-fanout that task — reuse the existing bundle and go directly to `instructions/fanout.md` for the staged runtime.

## Scoped completion vs parallel fanout

- **Scoped completion mode** (one plan in scope, only one pass per task): serialized. Take the first task in `max_batch`.
- **Parallel fanout mode** (`max_batch > 1` AND no active delegations AND `max_parallel_workers > 1`): fan out all tasks in `max_batch` in this pass.

If unclear, default to serialized.
