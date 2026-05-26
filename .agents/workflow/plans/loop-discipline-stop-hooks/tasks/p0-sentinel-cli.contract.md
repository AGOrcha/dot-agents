# P0 Sentinel CLI Contract

- task: `p0-sentinel-cli`
- requirements: R4, D5, D7; resolves Q1 and Q2
- owner boundary: `commands/workflow/` and the sentinel schema files in
  `schemas/` and `commands/workflow/static/`

## Goal

Provide the durable invocation record used by all three stop gates. This
task records context; it does not decide whether a stop is blocked.

## Required Reads

- `commands/workflow/cmd.go` (`newWorkflowCmd`) for command registration.
- `commands/workflow/verification_result_schema.go` and
  `commands/workflow/review_decision_schema.go` for canonical/embedded
  schema validation conventions.
- `../../specs/loop-discipline-stop-hooks/design.md` for R4 and D7.

## CLI Contract

Add `da workflow hook-sentinel` below the existing workflow command:

```text
da workflow hook-sentinel write <skill> --run-id <id> --plan <plan-id> --task <task-id> --agent-type <type> [--expect <path>...] [--write-scope <path>...] [--eligible-snapshot-loaded] [--max-batch <n>]
da workflow hook-sentinel read <skill> (--run-id <id> | --latest) [--json]
da workflow hook-sentinel clear <skill> --run-id <id>
```

`<skill>` is one of `iteration-close`, `isp`, or `loop-worker`.
`agent-type` is `main` or `loop-worker`. `write` captures the current Git
HEAD itself; callers must not supply an untrusted replacement.

## Persisted Document

The version 1 schema validates a JSON document containing:

```json
{
  "schema_version": 1,
  "skill": "loop-worker",
  "run_id": "run-id",
  "started_at": "RFC3339 timestamp",
  "plan_id": "plan-id",
  "task_id": "task-id",
  "agent_type": "loop-worker",
  "expected_artifacts": [],
  "context": {
    "git_head_at_start": "commit oid",
    "write_scope": [],
    "eligible_snapshot_loaded": false,
    "max_batch": 0
  }
}
```

Omit context keys not applicable to a skill rather than assigning invented
meaning. Store active records at
`.agents/active/hook-sentinels/<skill>-<run-id>.json`.

## Persistence Rules

- Validate skill names, run IDs, and paths before constructing a filename.
- Reject collisions; v1 has no overwrite flag.
- Write atomically so a stop hook cannot read partial JSON.
- `read --latest` selects the most recent `started_at`, with filename as
  the deterministic tie-breaker.
- `clear` moves a successful record to
  `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/`; pruning is
  deferred and no record is silently deleted in v1.

## Acceptance

- Register the command from `newWorkflowCmd`.
- Keep canonical and embedded schema copies byte-equivalent.
- Unit test write/read round-trip, latest selection, invalid skill/run ID,
  collision rejection, schema failure, and archive-on-clear.
- Return structured JSON under global `--json` without embedding human
  prose in machine fields.

## Out of Scope

- Stop-hook gate evaluation.
- Transcript parsing or inference of commands invoked by an agent.
- Archive retention or pruning policy beyond the required durable
  per-plan destination.
