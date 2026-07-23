# Fold-back: p1a-mapper-extensions bundle write_scope points at non-mapper files

## Observation

The delegation bundle `del-p1a-mapper-extensions-1779798327.yaml` lists
write_scope as:

- `internal/platform/codex.go`
- `internal/platform/copilot.go`
- `internal/platform/cursor.go`
- `internal/platform/coverage_gap_test.go`

But the actual event-mapping functions named in the user prompt and in the
contract's "Required Reads" section (`codexEventName`, `cursorEventName`,
`copilotEventName`, and all four `render*HookConfig`/`renderCopilotHookFile`
functions) live in `internal/platform/hooks.go`.

`codex.go`, `copilot.go`, and `cursor.go` contain platform metadata, installer
discovery, session-file resolution, and `createProjectHookFiles` glue, but no
`when`-to-event mapping. A `grep` for `EventName` or `subagent` across those
three files returns zero hits.

## Impact

A strict reading of the bundle would block the task: the requested changes
cannot be made within the listed write_scope. The contract is correct; the
bundle's write_scope is a fanout-generator error (likely a name-similarity
heuristic mapping `codex/copilot/cursor` event mapping to the same-named
platform files).

## Recommendation

When the fanout generator computes write_scope for hook mapper tasks, it
should derive scope from the contract's "Required Reads" or grep for the
specific function names mentioned, not from platform name → filename. For
this task and future P1b/P1c/P1d siblings, `internal/platform/hooks.go` is
the canonical edit target.

## Action taken in this iteration

Worker proceeded under the contract (which is the authoritative artifact per
the workflow-artifact-model). The edit to `hooks.go` is logically inside the
"platform mapper" scope the bundle intended to express; the test file
listed in write_scope (`coverage_gap_test.go`) houses the regression test as
specified. Fold-back recorded so the orchestrator can correct the fanout
template.
