# Lesson: worktrees outside the repo root break `go build` VCS stamping

## What happened
Two recovered branches lived in worktrees at **sibling paths outside the repo root**
(`/Users/nikashp/Documents/dot-agents-wt-p3`, `…-wt-p5`). Pushing them failed the
pre-push gate at the `go build (POSIX + windows) + go vet` step with:

```
error obtaining VCS status: exit status 128
	Use -buildvcs=false to disable VCS stamping.
```

This happened even though:
- `git status`, `git rev-parse HEAD`, `git show -s` all exit 0 in that worktree
- the linked `.git` gitdir pointer is healthy
- `go build -buildvcs=false ./cmd/da` compiles cleanly (so the CODE is fine)

Branches in worktrees **under** `.agents/worktrees/` (inside the repo root) build and
push fine. Four branches pushed gate-clean from there in the same session.

## Root cause
Go's `buildvcs` stamping resolves the VCS root by walking up from the package dir and
runs git from there. For a linked worktree at a sibling path outside the main repo tree,
go's specific git invocation returns exit 128 (it cannot reconcile the worktree's
out-of-tree location with the commondir), while plain git commands still succeed. It is
an environment/location problem, not a code or git-data problem.

## Rule
Create agent worktrees **under the repo root** (`.agents/worktrees/<name>`), never at a
sibling path outside it. If a branch is already stuck in a sibling-path worktree, relocate
it before pushing — do NOT reach for `-buildvcs=false` (that disables a real gate step).

## How to apply
When the pre-push gate fails only at `go build`/`go vet` with `error obtaining VCS status:
exit status 128` and `git`+compile are otherwise healthy, check whether the worktree is
outside the repo root. If so, relocate and push from the healthy location:

```sh
git worktree remove --force /path/to/sibling-wt        # tree must be clean; artifacts
                                                       # are already in the canonical store
git worktree add .agents/worktrees/<name> <branch>
ln -s "$REPO_ROOT/.venv" .agents/worktrees/<name>/.venv  # pre-push CRG gate needs it
git -C .agents/worktrees/<name> push -u origin <branch>
```

Related: [`worktree-no-cd`](../worktree-no-cd/LESSON.md),
[`concurrent-workers-one-worktree`](../concurrent-workers-one-worktree/LESSON.md).
