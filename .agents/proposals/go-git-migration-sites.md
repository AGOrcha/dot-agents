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
| **status-remote-display** ✅ resolved in #127 | `commands/internal/lifecycle/status.go`, `commands/sync/helpers.go` | `commands/internal/lifecycle/status.go:253`, `commands/sync/helpers.go:68` | Was: `execabs.Command("git", "-C", agentsHome, "remote", "get-url", "origin")`. Now: `gitremote.ReadOriginURL(agentsHome)` (in-process via go-git, no subprocess) + display normalized to `gitremote.CanonicalRepoID(raw)` form, falling back to the raw URL when canonicalization yields "". Maintainer ruling on PR #127 ("yes normalize it") — folded inline rather than deferred to a follow-up because the call-site delta was small (~6 LOC per site). | — |

## What is NOT a migration site

Recorded here so the next audit does not re-flag them:

- `internal/config/agentsrc.go:24-30` — already migrated in #127. The
  `gitRemoteOriginURL` seam now defaults to `gitremote.ReadOriginURL`
  (go-git/v6 in-process), and the two other `remote get-url origin`
  callers (`status.go:253`, `helpers.go:68`) were folded into the same PR.
- `commands/workflow/commit_cmd.go` — uses `go-git/v6` directly for
  commit/worktree ops, not URL parsing. Not in scope.
- `internal/graphstore/crg.go`, `internal/scoring/signal_git.go`,
  `internal/testutil/testutil.go`, `commands/workflow/{state,delegation,
  iter_log,plan_task}.go`, `commands/sync/{commit,log}.go`,
  `commands/kg/sync_code_warm_link.go` — shell out to `git` for diff/
  rev-parse/log/show/commit/etc. They do not parse remote URLs, so they
  are **not** migration targets for the `internal/gitremote` helper.

  Maintainer-flagged on PR #127 ("not a migration site for this helper
  but should be accounted for"). Tracking status as of #127:

  - **`internal/graphstore/crg.go`** — the largest of the cluster
    (Python subprocess per query, dominant cost under load). Tracked in
    `.agents/workflow/specs/graph-backend-adapter-contract/design.md`
    §11.4 (decommissioning gate for the CRG bridge + subprocess
    machinery) and `.agents/workflow/specs/graphstore-concurrency-contract/design.md`
    (per-call subprocess startup cost is called out explicitly under
    "CRG: every query spawns a fresh Python subprocess"). Decommission
    is gated on tool-by-tool parity per spec §11.1, not on a standalone
    shell-out migration.
  - **`internal/scoring/signal_git.go`** — owned by its scoring slice
    in `.agents/workflow/plans/r1-outcome-scoring/SLICES.yaml` (slice
    `signal-git`, write_scope locked to that file). Not flagged for
    go-git migration; the shell-outs are bounded per-call git
    invocations (`rev-parse`, `merge-base`, `log`, `show`) that don't
    parse remote URLs and don't sit on a hot path.
  - **Everything else in this bullet** (testutil, workflow/{state,
    delegation,iter_log,plan_task}, sync/{commit,log}, kg/sync_code_warm_link) —
    **not individually tracked** for go-git migration anywhere. They
    are per-call shell-outs to git for diff/rev-parse/log operations,
    not parsing remote URLs. Filed as a backlog candidate in the
    per-cluster section below (`audit-internal-git-shellouts`) so a
    future scoping pass can decide whether to bundle them with the
    CRG decommission or sweep them separately.
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

2. ~~**`gitremote/normalize-status-remote-display`**~~ — **landed in
   #127** per maintainer ruling ("yes normalize it"). Both
   `commands/internal/lifecycle/status.go:253` and
   `commands/sync/helpers.go:68` now route through
   `gitremote.ReadOriginURL` + `gitremote.CanonicalRepoID`.

3. **`audit-internal-git-shellouts`** *(scoping task, not implementation)* —
   the cluster flagged under "What is NOT a migration site" above
   (testutil + workflow/{state,delegation,iter_log,plan_task} +
   sync/{commit,log} + kg/sync_code_warm_link) is **not** tracked under
   any existing plan/spec for go-git migration. Decide whether to:
   (a) bundle into the `graph-backend-adapter-contract` decommission
   sweep, (b) handle as a standalone CLI-modernization pass, or
   (c) leave intentionally as shell-outs (they're bounded per-call,
   low-frequency, and the syscall cost is dwarfed by the git
   operations themselves). `internal/graphstore/crg.go` and
   `internal/scoring/signal_git.go` are *already* tracked elsewhere
   (graph-backend-adapter-contract §11.4 + r1-outcome-scoring slice)
   and are out of scope for this audit.

Each task should:
- Touch only the file(s) for its cluster.
- Include a focused test that covers the URL shapes the migrated site is
  expected to handle (at minimum: SSH SCP, https://, https-with-port,
  .git-suffixed, uppercase-host).
- Reference this doc and PR #127 in its commit message.
