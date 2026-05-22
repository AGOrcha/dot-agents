# Lesson: audits must verify against origin/<branch>, not the local ref

## Pattern observed (2026-05-18, high-cost cascade)

The codex-hooks-agents-linking-gap audit concluded "the shared-target
projection has NO production caller on master" and cited
`git merge-base --is-ancestor ... origin/master`. That conclusion was
**false**. The local checkout's `master` ref was `04826b21` (PR #13);
real `origin/master` was `efb19756` (PR #26) — ~13 PRs ahead. The
projection wiring (refresh.go:157, add.go:531, install.go:222) had
landed via `8e86a35e`, an ancestor of the *real* master. The audit
read a stale tree.

Cascade from the one stale-ref read:
1. Audit Finding 2 wrong (claimed total gap; real gap is narrow:
   import-relink + doctor-repair paths only).
2. A #29 doc amendment shipped a **false** "not invoked by any command"
   claim (had to be re-corrected).
3. `shared-target-projection-wiring` plan stp1/stp2 were scoped against
   already-merged work (no-ops); the stp1 worker correctly refused to
   fabricate an empty PR and re-verified the premise.

## Root cause

A worktree/checkout's local `master` (or any branch) ref is only as
fresh as the last fetch into *that* ref. Worktrees created off
`origin/master` are fine; reasoning about "what's on master" using the
local `master` ref (or a stale worktree) silently uses old state. The
audit trusted the local ref's tree even while quoting an `origin/master`
command.

## Rule / how to apply

Any audit, analysis, or claim of the form "X is/isn't on master":
- `git fetch origin` FIRST, then reason only about `origin/<branch>`
  (`git show origin/master:path`, `git log origin/master`,
  `git merge-base --is-ancestor <sha> origin/master`).
- Cross-check by SHA: print `git rev-parse origin/master` and the local
  ref; if they differ, the local ref is not "master".
- For "is this commit on master" use `git merge-base --is-ancestor
  <sha> origin/master` (exit 0 = yes) — not a path/grep of the working
  tree.
- A delegation bundle that asserts a premise ("no caller exists")
  must instruct the worker to re-verify it against origin before
  acting, and to STOP (not fabricate) if the premise is already
  satisfied — as the stp1 worker correctly did.

Relates to [[subagent-out-of-workspace-access]] (both are
delegation-correctness rules); the bad-boundary rebase trap is the
same family (trusting a moved/stale ref).
