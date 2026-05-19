# Ralph Runtime Permissions And Error Handling

Date: 2026-04-19

## Summary

Reviewed the latest Ralph stream (`.ralph-loop-streams/run-20260419-001811`) and fixed the two runtime issues it exposed:

- Codex worker failures caused by usage caps or hidden-path sandbox restrictions now stop the worker with explicit terminal classifications instead of silently exhausting iterations.
- Codex-based Ralph roles now launch with additional writable roots for repo `.agents`, repo `.git`, and `~/.agents`, so repo-local workflow artifacts and checkpoint state are pre-authorized.

Also reduced noisy orchestration context by filtering canonical plan discovery to directories that still contain `PLAN.yaml`.

## Files

- `bin/tests/ralph-worker`
- `bin/tests/ralph-pipeline`
- `bin/tests/ralph-orchestrate`
- `bin/tests/ralph-closeout`
- `commands/workflow/state.go`
- `commands/workflow/state_plan_test.go`

## Verification

- `bash -n bin/tests/ralph-worker bin/tests/ralph-orchestrate bin/tests/ralph-pipeline bin/tests/ralph-closeout`
- `go test ./commands/workflow`
- `go test ./...`

## Notes

- The repo already had uncommitted `p11` scoped-completion work in `commands/workflow/cmd.go`, `commands/workflow/plan_task.go`, `docs/LOOP_ORCHESTRATION_SPEC.md`, `bin/tests/ralph-orchestrate`, `bin/tests/ralph-pipeline`, and `bin/tests/ralph-closeout`; this change worked with that state and did not revert it.
