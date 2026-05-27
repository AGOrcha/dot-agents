# Multi-worktree sessions: always `git -C`, never `cd`

## Pattern

When working in a session that touches multiple git worktrees (main repo + `.claude/worktrees/<name>` + `.agents/worktrees/<name>`), **never `cd` to a worktree to run git commands.** Always use `git -C /absolute/path/to/worktree <subcommand>`.

## Root cause

The Bash tool's shell preserves `pwd` between calls. A single `cd <worktree> && <one-shot-command>` silently moves the session into that worktree for *every subsequent Bash call* that doesn't override it. The Edit/Write tools take absolute paths and target the real path regardless, but `git checkout`, `git checkout -b`, `git commit`, and even `gh pr create` (when `--head` is omitted) read the git context of the current pwd. Result: branches get created in the wrong worktree, commits land on the wrong branch, and pushes go to the wrong ref.

Two incidents in session 2026-05-20:
1. `cd "$SEAM" && go build` after a recovery step persisted pwd into the seam-di worktree. The next `git checkout master && git checkout -b crg-dsl-e-f` happened *inside* seam-di, switching its HEAD away from `seam-interface-di` and creating the new branch on its tree. Edits to the spec via absolute Edit paths landed in the *main* worktree (correct filesystem) while the new branch existed in *seam-di* (wrong git context). Untangling required copying the file across worktrees and restoring seam-di's branch.
2. `git checkout crg-dsl-e-f` (without `-C`) ran in seam-di because of the lingering pwd. Then a later `git -C "$SEAM" commit` for an unrelated spec landed on the wrong branch (`crg-dsl-e-f` instead of `seam-interface-di`). Required `git reset --hard origin/<branch>` + recheckout + recopy + recommit.

## Rule

- For any session touching ≥2 worktrees: set `SEAM=/abs/path` (or similar) at the top, then use `git -C "$SEAM" <cmd>` everywhere.
- Build/test commands that genuinely require cwd inside a worktree (`go build`, `go test`) should use a subshell: `(cd "$SEAM" && go test ...)` — the parentheses prevent pwd leak.
- After any `cd`, run `pwd` in the very next call to confirm where the session is, before any git operation.
- `gh pr create` accepts `--head <branch>` explicitly — always pass it instead of relying on the current branch.

## Cross-references

- `[[parallel-worker-branch-drift]]` — sibling lesson where prek hooks trigger the same drift mechanism even when the worker's commands all use `git -C`
