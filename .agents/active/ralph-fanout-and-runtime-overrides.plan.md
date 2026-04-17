# Ralph Fanout And Runtime Overrides

Status: In progress

## Goal

Fix Ralph orchestration so it does not fan out conflicting tasks, and add per-role runtime overrides for model and agent binary selection.

## Scope

- `bin/tests/ralph-orchestrate`
- `bin/tests/ralph-cursor-loop`
- `bin/tests/ralph-closeout`
- `bin/tests/ralph-pipeline`

## Outcomes

1. Orchestrate skips pending tasks whose write scope overlaps an active delegation.
2. Orchestrate also avoids selecting multiple new tasks with overlapping write scopes in the same pass.
3. Ralph supports separate model and `AGENT_BIN` overrides for:
   - orchestrator
   - closeout
   - generic worker
   - impl worker
   - verifier worker
   - review worker

## Verification

- `bash -n` on all Ralph scripts
- Readback of task-selection logic and env precedence
