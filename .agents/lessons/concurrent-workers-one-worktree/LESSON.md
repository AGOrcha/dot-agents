# Lesson: don't spawn a 2nd worker into a worktree a prior worker can resume into

## Pattern (gcc2 #34, 2026-05-19)

A pass-2 fix worker (`gcc2-fix`, a77ed8d9) finished its turn but had
**armed its own background CI-waiter `Bash` tasks** ("I'll stop polling
and wait for the async CI notification"). Those waiters later re-invoked
that agent, which **resumed and resumed editing** `.claude/worktrees/gcc2`
— at the same time a freshly spawned `gcc2-fix3` (a28fb734) was editing
the same branch `gcc2-path-a`. Two agents mutated one worktree
concurrently.

It resolved cleanly *by luck* (each landed a separate commit → clean
linear stack, working tree clean), but concurrent mutation of one
worktree/branch is a classic half-applied-tangle / lost-edit hazard.

## Root cause

A worker that arms a background waiter (CI poll, monitor) is **not
terminated** — it will resume into its worktree when the waiter fires.
Spawning a new worker into that same worktree before the prior one is
*fully* done creates concurrent writers. The orchestrator treated
"worker sent its final report" as "worker done"; it was not (a waiter
was armed).

## Rule / how to apply

- One active writer per worktree/branch. Before spawning a worker into
  an existing worktree, confirm the prior occupant is fully terminated —
  **no armed background waiters that can resume it**.
- Forbid delegated workers from arming their own background CI/monitor
  waiters: the bundle's closeout is "push + report"; **CI verification
  is the orchestrator's job** (main thread polls and relays). A worker
  that self-arms a waiter stays alive and can collide.
- If a fix needs another pass, either reuse the SAME still-alive worker
  via SendMessage, or spawn the next pass into a FRESH worktree off the
  updated branch — never a second concurrent worker into the live one.

Relates to [[subagent-out-of-workspace-access]] and
[[stale-local-master-ref]] (all delegation-correctness rules).
