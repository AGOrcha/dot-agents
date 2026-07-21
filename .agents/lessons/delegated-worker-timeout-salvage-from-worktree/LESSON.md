# Lesson: a delegated worker that times out mid-report has usually FINISHED — salvage from its worktree, don't restart

**Date:** 2026-07-21
**Surfaced by:** two workers this session (`RefBackManagedArtifacts`,
`IterLogRegistry`) were `cancelled` at ~40m — but each transcript ended with the
work DONE and verified ("All green. Let me confirm the final changeset"),
truncated only as it was about to report. The complete, uncommitted changeset
sat in the worker's worktree. Restarting would have thrown away ~40m of correct
work.

## Pattern
A long delegated worker hits a wall-clock/runtime limit (~40m here) and is
`cancelled` (status: failed/cancelled) **after** finishing + verifying its
changes but **before** it could return a report. Because the worker was told
"do NOT commit" (the parent commits), the work is intact-but-uncommitted in its
worktree.

## Cause
The runtime caps a single delegated task's duration; a big task (mirror across N
writers, a new subsystem + tests) that also runs its own `go test -race`
verification can exceed it. "Cancelled" reads like failure, but the diff already
landed on disk.

## Rule
On a `cancelled`/failed long worker, **establish ground truth in its worktree
before restarting**:
1. `cd <worktree>; git status --short` — are the expected files present?
2. `go build ./...` + the relevant `go test` — does the work verify?
3. Read the tail of `history://<worker>` — did it reach "all green / final
   changeset"?
If it's complete + verifies, **commit + PR it yourself** (the parent was going
to commit anyway). Only re-delegate the *remainder* if genuinely partial.

## Regression check / prevention
- Scope a single worker so its implement+verify fits well under the runtime cap;
  split "build + full-race-verify + big test suite" across workers, or have the
  worker report BEFORE the final full-suite race run.
- Never treat a `cancelled` long worker as "start over" without the 3-step
  worktree ground-truth check above.
