# A worktree worker's base branch can predate the task — and untracked bundle/contract files never git-propagate into it

## Pattern (t2c-codex-import-seam, package-artifact-install, 2026-07-16)

A worker spawned into an isolated worktree off `feat/package-artifact-install` found that its
checkout was missing the very artifacts it needed to operate:

- `TASKS.yaml` in the worktree **had no `t2c` entry at all** — the base branch was cut before
  t2c was created in the parent's live checkout.
- Sibling `t2b` showed `status: pending` in the worktree despite its code being **verifiably
  integrated** (`codexManagedTomlMarker` / `writeCodexAgentTomlFile` present) — stale status
  frozen at branch-cut time.
- The delegation contract `.agents/active/delegation/t2c-codex-import-seam.yaml` was **absent**
  from the worktree because it was **untracked** in the parent checkout — git never carries an
  untracked file into a new worktree/branch.

The worker had to hand-sync the `t2c` + `t2b` task entries and copy the contract from the
parent's live checkout before the workflow CLI would run at all.

## Root cause

Two independent propagation gaps, both from the same source — **a worktree branch only ever
contains what was committed to its base ref at cut time**:

1. **Temporal:** the base branch predates newer plan/task edits, so the worktree's `TASKS.yaml`
   is a frozen older snapshot (missing entries, stale sibling statuses). This is the inbound
   dual of [[worktree-isolation-defeats-status-tracking]] (which is the *outbound* gap — a
   worker's status update never reaching the scout).
2. **Untracked:** delegation bundles/contracts authored in `.agents/active/` are often
   **untracked** in the parent. Git propagates only committed content into a new worktree, so an
   untracked contract is invisible to every worker spawned off a branch — no matter how fresh
   the base ref is.

## Rule

1. **Rebase the worktree base onto the live tip before spawning**, or spawn off the parent's
   current HEAD — never off a branch cut before the task existed. If the worker's first act is
   "reconcile my `TASKS.yaml` against the parent", the base ref was stale.

2. **Deliver the bundle/contract by explicit path, not via git.** The worker gets the bundle
   path in its prompt and reads it from the **parent checkout's absolute path** (or the bundle
   is committed before spawn). Do not assume an `.agents/active/delegation/*.yaml` written in the
   parent is visible inside the worktree — it is untracked and it is not.

3. **Treat worktree in-checkout task status as advisory only.** Its `TASKS.yaml` is a branch-cut
   snapshot; the parent's live checkout is the source of truth for status. A worker that must sync
   status to make the CLI operate should say so in the merge-back (t2c did) so the parent
   reconciles the real entries rather than the worktree's frozen copy.

## Cross-links

Inbound dual of [[worktree-isolation-defeats-status-tracking]]. Staleness sibling of
[[validate-bundle-against-head]] (bundle snapshot decays under a moving tree) and
[[stale-local-checkout-mass-drift]] (a checkout behind origin reads stale plans). Provenance
sibling of [[classify-generated-files-before-cleanup]] (untracked ≠ junk; here untracked ≠
propagated).
