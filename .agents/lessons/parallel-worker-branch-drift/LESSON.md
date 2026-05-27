# Parallel-worker branch drift via prek stash/restore

## Pattern

When multiple loop-workers run in parallel against adjacent `.agents/worktrees/<task>/` directories, a worker's commit can land on the *wrong* branch — specifically a sibling worker's branch that happened to be checked out in some shared shell state. Surfaced by p1b-worker during Wave 7 (2026-05-26): its first commit landed on `feature/t-archival-policy` (a sibling worker's branch) instead of `p1b-canonical-when-values`. The worker recovered via cherry-pick + reset; no remote impact.

## Root cause

The prek pre-commit hook runs a `git stash push` / `git stash pop` cycle around the hook execution to give the hook a clean tree. With parallel workers operating on the same filesystem, the stash/restore can interact with whichever branch the underlying shell currently has HEAD pointed at — and that HEAD can drift between bash invocations in a long-running session if any prior tool call ran `git checkout` (even in a stash context).

## Rule

- After every worker push, verify the remote branch HEAD matches what the worker reports as its commit. `gh api repos/<owner>/<repo>/git/refs/heads/<branch>` is the cheapest cross-check.
- For workers operating in isolated worktrees, *every* git command they emit must use `git -C <worktree-path>` — never `cd` (already covered by `[[worktree-no-cd]]`). Workers that forget this can have their commit land on the main worktree's branch.
- If a worker reports a "branch-switch incident" in its closeout, treat it as a soft fail signal: spot-check that its PR actually contains its declared commits via `gh pr view <n> --json commits` before relying on the worker's claimed coverage / scope.
- When opening many parallel worker PRs back-to-back, watch for cross-branch commit landings in the GitHub branch list. The recovery is cherry-pick + reset (no force push needed if caught before merge), but it's an opportunity to lose work if undetected.

## Cross-references

- `[[worktree-no-cd]]` — primary defense; the drift surfaces when that rule is violated by a hook or wrapper script
- `[[worker-owns-pr-readiness-loop]]` — workers should self-check branch HEAD as part of post-push verification before declaring READY
