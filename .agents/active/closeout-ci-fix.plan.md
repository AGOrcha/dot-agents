Status: active
Plan: closeout-ci-fix

## Goals

- Record the loop-agent post-closeout dirty-workspace follow-up in the canonical workspace plan.
- Fix `ralph-closeout` so archive moves and canonical plan updates are committed after archival.
- Fix the failing GitHub Actions shell path where `dot-agents add` aborts on a non-git project.

## Steps

1. Update `loop-agent-pipeline` notes for the post-closeout lane.
2. Patch `bin/tests/ralph-closeout` staging so post-archive state is committed.
3. Patch the shell config helper used by `src/bin/dot-agents add`.
4. Reproduce the CI failure locally and run focused verification.
