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

## Update (2026-07-21): "isolated" in a brief ≠ isolated; and reset-in-shared-tree

Spawned 3 parallel Sonar fixers whose brief SAID "you run in an isolated
worktree" — but the `task` call did NOT pass `isolated: true`, so all three ran
in the ONE shared main checkout. Worse, each was told (via IRC, to fix a
stale-base issue) to `git fetch && git reset --hard origin/master` — in a shared
tree that is mutually destructive: `reset --hard` wipes every sibling's
in-progress working-tree edits, and it moved the shared branch ref 3× (visible
in `git reflog`). Net: collided, partial, untrustworthy patches.

Then relaunching WITH `isolated: true` failed outright — `Isolated task
execution requires a git repository. fatal: not a git repository: (null)` — the
worktree-add machinery choked in a repo already carrying ~35 worktrees.

Rules:
- **Verify the `isolated: true` flag is actually passed** at launch; a brief
  that merely SAYS "isolated" isolates nothing. Confirm from the spawn params,
  not the prose.
- **NEVER instruct a shared-tree (non-isolated) agent to `git reset --hard`**
  (or any branch-ref move) — it destroys concurrent siblings' work and moves the
  shared branch. Fix a wrong base by re-spawning from a correct base, not by
  resetting under live siblings.
- **`isolated: true` can itself fail** (`not a git repository`) in a repo dense
  with worktrees. Have a DIY fallback: make the edits yourself in ONE controlled
  worktree off `origin/master`, or run the workers strictly sequentially (one
  writer, no reset) — don't keep retrying a flaky isolation machinery.
- Root prevention: keep the orchestrator's main checkout ON `origin/master`
  (not a stale plan branch) so any worktree/agent forks from the right base.

Relates to [[subagent-out-of-workspace-access]] and
[[stale-local-master-ref]] (all delegation-correctness rules).
