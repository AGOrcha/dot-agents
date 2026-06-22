# ISP Step 1: Load Orientation Context

`orchestrator-session-start` should have already run `workflow eligible --json --plan <scope>` and presented the orientation summary. Use that output.

If the output was not passed in (or is stale), re-run:

```bash
da workflow eligible --json --plan <scope>
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

## Write Stop-Gate Sentinel

Write the sentinel **once**, immediately after you have consumed the
eligible/orientation snapshot above and resolved the plan and the task you are
about to act on — and **before** selecting or dispatching any work. The
Stop/SubagentStop gate reads the latest `isp` sentinel and checks fanout
discipline (a bundle written this turn must have a parent-gate iter-log entry,
declared `max_batch` should materialize, etc.). No sentinel means the gate
exits 0 and nothing is enforced this turn.

Pick a filename-safe `--run-id`, set `--agent-type main` (the orchestrator runs
isp directly, not as a delegated subagent), and record the two isp gate signals:
`--eligible-snapshot-loaded` when the eligible JSON came from
orchestrator-session-start (omit the flag if you had to re-run `workflow
eligible` yourself), and `--max-batch <n>` set to the size of the fanout set you
intend to dispatch:

```bash
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"

da workflow hook-sentinel write isp \
  --run-id "$RUN_ID" \
  --plan <plan-id> \
  --task <first-task-in-batch> \
  --agent-type main \
  --eligible-snapshot-loaded \
  --max-batch <len(max_batch)>
```

If you re-ran orientation yourself rather than loading the orchestrator's
snapshot, drop `--eligible-snapshot-loaded` so the sentinel records the truth —
the gate uses that signal (only with a verified trace) to advise on re-orient
discipline, and a false flag would misreport intent.

## Gate contract (do not silently break it)

The `isp-gate` HOOK.yaml/gate.sh reads this sentinel. If you change which
governed actions isp performs — the fanout/parent-gate sequence, the
`max_batch` materialization contract, or the bundle-then-iter-log ordering — you
MUST update the matching gate contract and its tests in the same change. A skill
edit that drifts from the gate silently disables enforcement. No instruction
here permits hard remediation on transcript-only facts (re-run orient,
direct-vs-fanout prose) without verified trace input; those remain soft
advisories when no trace is supplied.
