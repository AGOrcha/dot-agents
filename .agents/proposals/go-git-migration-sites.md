# go-git Migration Sites

**Status:** project-local proposal — enumeration only, no migration in this doc.
**Context:** PR #127 introduced `internal/gitremote` as the canonical home for
parsing git remote URLs, delegating to go-git/v6's `transport.ParseURL`. The
first consumer is `internal/config/agentsrc.go` (repo_id derivation).

This document enumerates the **other** sites in the codebase that currently
hand-roll some form of git remote URL or path parsing and should migrate
incrementally. Each cluster below is one follow-up task; do not bundle them.

## Audit method

Grepped (against worktree `p0b-repo-id` at `f6f93b89`) for:

- `regexp.*github\.com`
- `strings.Split.*"@".*"/"` (SCP detection)
- `strings.TrimPrefix.*"git@"` / `"https://"` / `"http://"` / `"ssh://"`
- `strings.HasSuffix.*".git"` then trim
- `url.Parse` followed by manual path splitting
- `execabs.Command("git", ..., "remote", "get-url", ...)` callers
- Hardcoded host strings (`"github.com"`, `"gitlab."`, `"bitbucket."`) used
  in URL classification

After the migration in #127, only **one** cluster of hand-rolled parsing
remains. Two other sites shell out for the raw remote URL but do not parse
it; they are listed as low-priority follow-ups.

## Enumeration

| Cluster | Files | Sites (file:line) | Current parsing pattern | Suggested helper |
|---|---|---|---|---|
| **status-source-desc** | `commands/internal/lifecycle/status.go` | `commands/internal/lifecycle/status.go:430-433` | Loops over `[]string{"https://", "http://", "git@"}` calling `strings.TrimPrefix` then `strings.TrimSuffix(u, ".git")` to build a display string for a `git:`-type source. Misses `ssh://`, `git://`, embedded userinfo, and ports — uppercase hosts pass through. | `gitremote.ParseRemoteURL(u)` → format as `ref.Host + ":" + ref.Path` (or `ref.Host + "/" + ref.Path` if the display should match repo_id). Fall back to raw `u` on `ErrNotRemote` so non-URL sources still render. |
| **status-remote-display** | `commands/internal/lifecycle/status.go`, `commands/sync/helpers.go` | `commands/internal/lifecycle/status.go:253`, `commands/sync/helpers.go:68` | `execabs.Command("git", "-C", agentsHome, "remote", "get-url", "origin")` — raw URL is displayed verbatim with no parsing. Not strictly hand-rolled parsing, but a candidate for normalization-on-display so the status output is consistent across SSH/HTTPS forms. **Low priority** — purely cosmetic, no correctness bug. | Optional: `gitremote.ParseRemoteURL` then render `ref.CanonicalForm` instead of raw. Skip if maintainer prefers the verbatim form (it reveals the actual transport in use). |

## What is NOT a migration site

Recorded here so the next audit does not re-flag them:

- `internal/config/agentsrc.go:24-30` — already migrated in #127.
- `commands/workflow/commit_cmd.go` — uses `go-git/v6` directly for
  commit/worktree ops, not URL parsing. Not in scope.
- `internal/graphstore/crg.go`, `internal/scoring/signal_git.go`,
  `internal/testutil/testutil.go`, `commands/workflow/{state,delegation,
  iter_log,plan_task}.go`, `commands/sync/{commit,log}.go`,
  `commands/kg/sync_code_warm_link.go` — shell out to `git` for diff/
  rev-parse/log/show/commit/etc. They do not parse remote URLs.
- `internal/config/agentsrc.go:654` — `"git:" + s.URL + "\x00" + s.Ref`
  composes a cache-key string from an already-canonical source URL. Not
  parsing.

## Per-cluster follow-up tasks (to be filed by orchestrator)

After #127 merges, the orchestrator should file (separately):

1. **`gitremote/migrate-status-source-desc`** — migrate
   `status.go:430-433`. Single-file change, ~5 LOC delta. Add a test
   asserting `git:` source descriptions render canonical host/path for
   SSH, https://, https-with-port, .git-suffixed, and uppercase-host
   inputs.

2. **`gitremote/normalize-status-remote-display`** *(optional, low
   priority)* — decide with maintainer whether the raw `remote get-url`
   output in `da status` should be normalized via
   `gitremote.CanonicalRepoID` for consistency, or left verbatim to
   reveal the transport. If yes, migrate both
   `commands/internal/lifecycle/status.go:253` and
   `commands/sync/helpers.go:68` in one task.

Each task should:
- Touch only the file(s) for its cluster.
- Include a focused test that covers the URL shapes the migrated site is
  expected to handle (at minimum: SSH SCP, https://, https-with-port,
  .git-suffixed, uppercase-host).
- Reference this doc and PR #127 in its commit message.
