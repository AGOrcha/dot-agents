# Closeout archives the merge-back an iteration-close sentinel still guards — clear the sentinel too

**Captured:** 2026-07-15
**Triggered by:** a full main-session tool deadlock — the `iteration-close-gate` Stop/PreToolUse hook blocked *every* tool for ~20 cycles because a worker's iteration-close sentinel referenced a merge-back artifact that `delegation closeout` had just archived.

## The mistake

An isolated worktree worker's `.agents/` copy is stale, so it runs its `/iteration-close`
(`verify record` → `checkpoint` → `merge-back`) **from the main checkout** instead. That writes a
hook-sentinel at `.agents/active/hook-sentinels/iteration-close-<run>.json` in **main**, listing
`expected_artifacts` = the iter-log entry + `.agents/active/merge-back/<task>.md`.

Then the orchestrator runs `da workflow delegation closeout … --decision accept`, which **archives**
the merge-back artifact to `.agents/history/…/delegate-merge-back-archive/…`. The sentinel is still
active and still points at the now-moved merge-back file. On the next Stop (and, in this repo's
config, every PreToolUse), `iteration-close-gate/gate.sh` reads the latest sentinel, finds an
expected artifact missing, and **hard-blocks** — remediation text only, no tool executes. The
session cannot fix its own filesystem because the fix requires a tool. Only a user-run `!` shell
command or a settings change breaks it.

A resumed worker (round-2 fix) makes it worse: it writes a **second** sentinel for the new run, but
its `merge-back` CLI call is **rejected** because the delegation already flipped to `completed` on
round-1 — so the second sentinel's merge-back is never produced, and clearing the first reveals the
second (they stack).

## Why it happens

`delegation closeout` and the `iteration-close-gate` sentinel have **independent lifecycles**.
Closeout consumes/archives the merge-back; it does **not** clear the hook-sentinel that references
it. The gate checks artifact **paths on disk**, so archiving = "missing" from the gate's view.

## The rule

When you close out a delegated task whose worker ran `/iteration-close` **from the main checkout**
(any task where `.agents/active/hook-sentinels/iteration-close-*.json` exists for that run):

1. **Closeout first**, then **clear the sentinel** in the same step:
   `da workflow hook-sentinel clear iteration-close --run-id <RUN_ID>`
   (find run ids via `da workflow hook-sentinel read iteration-close --latest`; sentinels **stack**,
   so re-read and clear until `read --latest` reports none).
2. A **resumed** worker (round-2) whose `merge-back` is rejected (delegation already completed)
   leaves a sentinel whose merge-back will never be written — its expected artifacts are already
   satisfied by round-1's, so **clear that sentinel** rather than trying to regenerate the artifact.
3. If already deadlocked: the escape is **not** a tool (all blocked). Either restore the exact
   `expected_artifacts` path via a user `!` shell command, **or** clear the sentinel via `!`. Read
   the gate at `~/.agents/hooks/global/iteration-close-gate/gate.sh` and the sentinel via
   `da workflow hook-sentinel read iteration-close --latest` to get the exact run id + paths.

## Prevention

- Prefer resolving a worktree worker's merge-back from **its own** `.agents` (bridge it to main
  before closeout) so the sentinel and the archive move together, or
- Treat `hook-sentinel clear` as a **mandatory step of closeout** for main-checkout iteration-close
  runs — see [[worktree-isolation-defeats-status-tracking]].
