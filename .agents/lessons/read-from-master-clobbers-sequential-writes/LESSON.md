# Lesson: `work_tracking.read_from = master` clobbers sequential same-checkout writes

**Date:** 2026-07-17
**Surfaced by:** dogfooding the git-ref workstore opt-in (`git-ref-work-backend`).

## Pattern

`work_tracking.read_from = master` (the read-from-master shim, PR #410) makes every
`da workflow` transition resolve the plan's canonical `TASKS.yaml`/`PLAN.yaml` from
`origin/<default-branch>` via `git show`, then rewrite the **whole** working-copy file
from that snapshot plus the single status change.

If two transitions run in the **same checkout** without a push to master in between,
the second transition re-reads the **stale** `origin/master` (which still lacks the
first transition's change) and rewrites the file — **silently reverting the first
change**. Observed live:

```
advance read-from-master-shim  -> completed   # working copy: shim=completed
advance git-ref-state-ref-write -> completed   # re-read stale master (both pending)
                                               # working copy: shim=PENDING again (clobbered)
```

With `read_from` at its `worktree` default the same two advances accumulate correctly
(read-your-writes holds).

## Rule

- Do NOT enable `read_from: master` as a repo-wide default. It is a **worktree-worker
  isolation** shim: correct only when each process does **one** transition in its own
  worktree and merges back — not for an orchestrator doing sequential mutations in the
  main checkout.
- The safe realization of "use the `refs/agents/state` ref backend" is
  `work_tracking.write_to = state-ref` alone (additive CAS mirror; `read_from` stays
  `worktree`).
- The real fix belongs in `git-ref-work-backend/workstore-git-ref-backend`: the read
  path must be read-your-writes safe (e.g. overlay uncommitted local state on the
  master read, or only apply the shim in isolated worktrees), before `read_from: master`
  can be a default.
