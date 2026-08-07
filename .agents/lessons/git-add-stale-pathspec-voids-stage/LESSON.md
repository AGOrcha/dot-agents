---
name: git-add-stale-pathspec-voids-stage
description: One non-matching pathspec makes `git add` exit non-zero and stage NOTHING, so a chained commit silently omits real work
type: feedback
---

`git add A B C` where any one path does not exist (e.g. a file you just `git mv`'d,
so the OLD path is gone) fails with `fatal: pathspec '...' did not match any files`
and stages **none** of A/B/C. A following `git commit` then commits only whatever
was *already* staged — the new files you meant to add are silently left out.

**Observed 2×** in the config-transitive-layering loop: iter-71 and iter-75 both
committed only a pre-staged rename while `iter-N.yaml` + `TASKS.yaml` were dropped,
because the `git add` list included a stale post-`git mv` path. The commit "succeeds"
and pushes, so nothing looks wrong until `git status --short` shows the intended
files still untracked/modified.

**Root cause:** `git add` is all-or-nothing on its argument list — a bad pathspec
aborts the whole invocation, and a chained `&& git commit` (or a commit in the same
script) still runs against the partial/empty index.

**Rule / how to apply:**
- Never pass a path you just moved/deleted to `git add`. After `git mv old new`, the
  rename is already staged — do not re-`git add old`.
- Prefer `git add -A <dir>` (or separate `git add` calls per path) so one dead
  pathspec cannot void the whole stage.
- ALWAYS run `git status --short` after staging and confirm the intended paths show
  staged (`A`/`M` in col 1) BEFORE committing; treat a `fatal: pathspec` line as a
  hard stop, not a warning.
- After the commit, verify `git show --stat` contains every file you intended — a
  one-file commit where you expected several is the tell.
