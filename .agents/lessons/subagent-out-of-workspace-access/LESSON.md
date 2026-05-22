# Lesson: spawned subagents are sandboxed to the workspace root

## Pattern observed (recurring, 3+ times this session)

Spawned subagents (Agent tool / codex:rescue / Explore) cannot read or
write paths outside the workspace root
(`/Users/nikashp/Documents/dot-agents`):

- review workers couldn't write reports to `.agents/active/reviews/...`
  when the target was outside their worktree;
- the Explore agent mining `/Users/nikashp/Documents/payout/swarm-cd`
  was hard-blocked on Bash for that out-of-repo path;
- codex:rescue could not reach the companion runtime path.

Each time the workaround was the **main agent** doing the read-only
work itself (the main session is not sandboxed the same way), which
defeats the point of delegating to keep context clean.

## Root cause

Sub-agent sandboxes are scoped to the workspace root. External
reference repos (`payout/swarm-cd`, sibling projects), `~/.agents`,
`/tmp` worktrees, and the codex companion path are outside it.

## Rule / how to apply

When a delegated task legitimately needs an out-of-workspace path
(reference-codebase mining, cross-repo comparison, an external
worktree), **equip the spawned agent with that directory up front**:

- Add the path to the harness additional-directories allowlist
  (Claude Code `--add-dir` / `additionalDirectories` /
  `permissions.additionalDirectories` in settings) BEFORE spawning, so
  the subagent's sandbox includes it; or
- keep the worktree inside the workspace root (e.g.
  `.claude/worktrees/<x>`) — proven writable by subagents this session
  (pr3b/pr3c/covgate worktrees worked; `/tmp/wt-*` did not).

**Refinement (cg6b B1, 2026-05-18):** an *in-workspace* worktree
(`.claude/worktrees/<x>`) is ALSO blocked when the subagent is spawned
without a permissive permission mode — the agent's cwd-override
isolation denies `cd`/`env -C` into the worktree, and `go test ./...`
needs cwd inside that module (no out-of-module flag). cg4 succeeded
because it ran permissively; the test-runner spawn did not. Fix is the
Agent `mode` parameter, not directory allowlisting: spawn
worktree-executing workers with `mode: bypassPermissions` (safe here —
isolated worktree, no merge, user gates the PR). Two distinct levers:
`--add-dir`/allowlist for OUT-of-workspace paths; `mode` for
IN-workspace worktree bash execution.

Default: if a delegation's inputs are out-of-workspace, either
pre-authorize the dir or do that bounded read inline; if it executes
tooling in an in-workspace worktree, spawn with a permissive `mode` —
do not spawn default, hit the block, and re-spawn (wasted cycles,
observed repeatedly).

See [[update-config]] for the settings mechanism;
relates to [[test-file-naming]] only insofar as both are
delegation-hygiene rules.
