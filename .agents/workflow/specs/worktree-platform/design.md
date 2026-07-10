# Spec: managed worktree platform for delegation/branch isolation

Status: draft / **wt0 + wt1 VERIFIED COMPLETE** (spike merged via PR #171,
2026-05-28; typed interface merged via PR #181, 2026-05-29 — both
re-confirmed against the actual repo, not just TASKS.yaml, on 2026-07-10).
`internal/gitwt` ships a tested `Manager`/`Worktree` implementation richer
than the "seam for wt1" this doc originally sketched. go-git version
re-checked current (no bump) 2026-07-10. wt2-wt6 + release-minor refreshed
and groomed to implementation-ready — see "Refresh: wt0/wt1 reality check"
below and TASKS.yaml.

## Problem

Delegation, agent spawning, multi-branch work, and PR sub-branch →
merge-back flows currently create git worktrees + sub-branches **ad
hoc, manually, with no managed lifecycle or boundary tracking**.
Concrete evidence (this session): the bad-boundary `git rebase --onto`
trap recurred ~4× — every time, because the *prior base* of a stacked
branch had to be re-derived by hand and `git merge-base` silently
returned the wrong ancestor after a force-push. A managed platform that
records each worktree's branch + true base + parent PR makes that class
of error structurally impossible.

It also resolves the **`workflow-commit-command` blocker**: that plan
is parked on the adversarial finding that `git commit` commits the
whole shared index and concurrent agents race the index/ref. **A
worktree-per-delegation model gives each agent its own working tree +
index** — `da workflow commit` in a worktree can only ever stage that
worktree's tree. This spec is the structural answer to that finding.

Secondary: the few Go git call sites (`commands/sync.go`,
`status.go`, `explain.go`) shell out to `git` — untyped, error-prone.

## Honest scoping note

**Corrected 2026-07-10 — this note predates wt0/wt1 shipping.** `go-git`
**is** now a dependency (pinned in `go.mod` since wt0/wt1), and the binary
has a real typed git surface: `internal/gitwt/{gitwt.go,gogit.go}` (merged
PR #181) plus one live consumer, `internal/eval/sandbox/worktree.go` (the
unrelated R4 eval-harness's sandbox — its `doc.go` names *this plan's*
managed-worktree provider as the intended future swap-in). The shell-git
call sites (`commands/sync.go`, `status.go`, `explain.go`) are unchanged
and still untyped.

What is **still** true: `internal/gitwt` is a low-level, single-repo
lifecycle seam only — no registry (branch/purpose/parent-PR/created-at —
wt2), no sub-branch/merge-back workflow (wt3), no config provisioning
(wt2/wt3), and — verified by grep — **zero callers in `commands/workflow/`
or the delegation/fanout skills**. wt2-wt5 remain a genuine net-new
subsystem, not a refactor.

## Decisions

1. **First-class managed worktree lifecycle.** create (per delegation /
   agent / sub-branch), a tracked registry (branch, **true base ref**,
   parent PR, purpose, created-at), reuse, and deterministic cleanup
   (auto-prune-if-unchanged, mirroring the harness `isolation:worktree`
   behavior).
2. **Sub-branch → merge-back as a first-class operation**, with the
   base ref recorded so rebases/merge-backs never re-derive it by hand
   (kills the recurring trap).
3. **Per-worktree index isolation is the contract** the
   `workflow-commit-command` plan binds to (its single-writer / no
   shared-index requirement is satisfied by construction here).
4. **Typed git layer** — the platform exposes typed Go operations, not
   stringly-typed shell calls, regardless of the mechanism chosen below.
5. **Worktree config is auto-provisioned on create and torn down on
   remove.** Only the project's **main** worktree path is registered with
   `da` today; a freshly created linked worktree is an unknown path with
   no agent config/links, so an agent spawned there has no skills, agents,
   hooks, or MCP wiring. The platform therefore provisions the worktree's
   config as part of `create` (reusing the canonical `add` + `install`
   setup path, not a reimplementation) and tears it down as part of
   `remove`. See "Worktree config provisioning lifecycle" below.

## DECISION (maintainer 2026-05-17): A — pure go-git **v6**

The historical go-git linked-worktree gap is addressed in **v6** by
`github.com/go-git/go-git/v6/x/plumbing/worktree`
(https://pkg.go.dev/github.com/go-git/go-git/v6/x/plumbing/worktree).
Direction: **pure go-git v6**, typed/in-process, no shell git.

The wt0 task is therefore reframed from "choose A/B/C" to **verify**:
confirm `v6/x/plumbing/worktree` actually supports linked-worktree
create / list / remove / prune + the `.git/worktrees/<id>` admin
files and per-worktree index, on our git versions. (`x/` = extended/
experimental in v6 — stability + API shape must be checked, not
assumed.) Fallback to Hybrid (go-git core + shell `git worktree`) ONLY
if the spike disproves v6 linked-worktree support.

Reference input: the **`payout/swarm-cd`** codebase may already use
go-git worktree support — mine it for a concrete, working API pattern
to base the spike + interface on (subagent investigation).

### Reference implementation — `payout/swarm-cd/swarmcd/worktree.go` (CONFIRMED)

Investigated 2026-05-17. swarm-cd uses **go-git v6**
(`go-git/v6 v6.0.0-20260305…`, `go-billy/v6`) with
`gitworktree "github.com/go-git/go-git/v6/x/plumbing/worktree"` and
**zero shell-git** — proving pure-go-git-v6 linked worktrees are
feasible. The pattern to adopt:

- `mgr, _ := gitworktree.New(repo.Storer)` → `*gitworktree.Worktree`
  manager.
- `mgr.Add(name, path, gitworktree.WithDetachedHead(),
  gitworktree.WithCommit(plumbing.NewHash(rev)))` → create linked
  worktree; typed errors `gitworktree.ErrWorktreeAlreadyExists` /
  `ErrWorktreeNotFound`.
- `mgr.Remove(name)` → remove.
- `repo2, _ := mgr.Open(osfs.New(path))`; `repo2.Worktree()` → operate
  the linked worktree.
- Stale cleanup = last-used-marker + TTL scan + `mgr.Remove`
  (`WorktreeStaleTTL`, `markWorktreeUsed`/`getWorktreeLastUsed`),
  plus path-component sanitization and repo↔worktree path resolution
  helpers — directly reusable for our registry/prune.

wt0 residual verification (NOT re-deciding — adopting): swarm-cd uses
**detached-HEAD + a commit**; we need **branch-mode** worktrees for
sub-branch → merge-back — confirm `gitworktree` supports
create-on/with a branch (and branch creation). Also confirm
**per-worktree index isolation** holds via `mgr.Open(...).Worktree()`
(the guarantee `workflow-commit-command` binds to), and whether the
pkg exposes a `List` or we enumerate via our own registry.

## Worktree config provisioning lifecycle

**Problem.** `da` knows a project only by its **main worktree path** (the
`cfg.AddProject(name, path)` entry written at `da add`). A linked worktree
created by this platform is a *different path on disk* that `da` has never
seen, so none of the project's managed config (skill/agent/hook links, MCP
wiring, generated platform files) exists there. An agent spawned in that
worktree runs with empty config.

**Decision.** `create` provisions the worktree's config; `remove` tears it
down. Both **reuse the canonical setup path** rather than re-linking by hand:

- `RegisterInstallProject(projectName, projectPath, deps)` +
  `RunInstall(strict, deps)` (`commands/internal/lifecycle/install.go`) —
  already exported, callable library functions — are the "two command
  trees" that materialize a project's config at a path. (`da add`'s
  `runAdd` scaffolds + registers; `da install` applies the `.agentsrc.json`
  manifest. Provisioning composes the install-side entry points so it does
  not re-implement linking.)
- Provisioning reflects the **current effective config** (post
  config-v2: the resolved/merged config, not a stale snapshot), so a new
  worktree always gets up-to-date links.

**Ephemeral identity (open question for the plan).** A naïve
`RegisterInstallProject` of the worktree path under the *same project name*
would **clobber the parent project's registration** (same name, different
path). The provisioned worktree must therefore carry an **ephemeral identity
keyed to the worktree** (e.g. `<project>@wt-<hash>`, matching the
`worktreeNameRE` charset constraints from Residual 1) **or** the install
path must be scoped under the parent project without overwriting its
`projects[]` entry. The plan must pick one; the registry (wt2) records which
ephemeral identity maps to which worktree so teardown is unambiguous.

**Layering.** `da add` / `da install` live in `commands/` and
`commands/internal/lifecycle/`; the typed git `Manager` lives in
`internal/gitwt` and **must not import `commands`**. So config provisioning
is **orchestrated one layer up** — the `da worktree create` / `remove`
command (or the orchestration/skill layer) composes
`gitwt.AddBranch(...)` **then** the install lifecycle, and on teardown
`uninstall/deregister` **then** `gitwt.Remove(...)`. `internal/gitwt` stays
git-only; provisioning is never wired inside the `Manager`.

**Cleanup contract.** `remove` must:
- tear down exactly what `create` provisioned (the worktree's links + the
  ephemeral registration) and **never touch the parent/main project's**
  config entry or AGENTS_HOME dirs;
- be idempotent (safe on a partially-provisioned or already-clean worktree);
- integrate with the existing auto-prune-if-unchanged path so an unchanged
  worktree's config is also reclaimed.

## Refresh: wt0/wt1 reality check + go-git currency (2026-07-10)

**wt0 + wt1 are genuinely COMPLETE, not stale-vs-reality (unlike the
da-recipe precedent this task was modeled on).** Verified directly
against the repo, not TASKS.yaml's say-so:

- `internal/gitwt/gitwt.go` + `gogit.go` exist, `go build
  ./internal/gitwt/... ./internal/eval/sandbox/...` is clean, and 14
  tests in `gitwt_test.go` cover exactly the wt0 residuals:
  `TestAddBranch`/`TestAddBranchErrors` (Residual 1, branch-mode
  create), `TestAddDetached`, `TestList`, `TestRemove`, `TestPrune`,
  `TestBaseRefRecordRead`, `TestOpenError`,
  `TestAdminDir_RealStatErrorSurfaces`, and — critically —
  `TestIndexIsolation` (Residual 2: commits a file only in worktree A,
  asserts it never appears in worktree B's status or the main repo's
  status, and that only A's branch advances). PR #171 (wt0, merged
  2026-05-28) and PR #181 (wt1, merged 2026-05-29) are both real,
  resolvable merge commits.
- **The shipped interfaces are richer than the "seam for wt1" this doc
  sketched** — see the corrected code block below. `RecordBaseRef`/
  `BaseRef` and `Prune` landed as **wt1** primitives, not deferred to
  wt2 as originally implied. This shrinks wt2's real remaining scope.
- `internal/eval/sandbox` (R4 eval-harness sandbox, separate plan,
  already merged) is a **live consumer** of `gitwt`: `NewWorktreeSandbox`
  opens a `gitwt.Manager`, calls `AddDetached` + `Prune`, and layers its
  **own** bespoke registry on top — a YAML marker sidecar
  (`RunID`/`WorktreeName`/`BaseCommit`/`ProvisionedAt`) plus a
  retention-window `PruneStale` — because wt2 does not exist yet. It
  does not call `gitwt.RecordBaseRef`/`BaseRef` at all, duplicating that
  bookkeeping itself. `internal/eval/sandbox/doc.go` says explicitly:
  *"the worktree-platform plan's managed-worktree provider... can
  replace the v1 implementation without any caller change."* wt2 should
  treat this marker/TTL pattern as a second reference implementation
  (alongside swarm-cd) to generalize from, and plan a follow-up to move
  `eval/sandbox` onto the wt2 registry rather than maintaining a
  parallel bookkeeping scheme indefinitely.
- **`commands/workflow/` and the delegation skills do not consume
  `gitwt` at all today.** `workflow fanout` does not create git
  worktrees (grep confirms zero `gitwt`/worktree-creation references in
  `commands/workflow/*.go`; the only "worktree" hits are directory-name
  skip-lists for symbol scanning). `delegation-lifecycle`/`isp`/
  `loop-worker`'s instructions contain no "isolated"/"isolation"
  language either. The worktree isolation actually in play today for
  delegated subagents is the **outer coding harness's** own
  isolated-worktree execution mode (`task(..., isolated: true)`) —
  external to dot-agents, not something this repo implements or can
  refresh. Decision #1's "mirroring the harness `isolation:worktree`
  behavior" phrase is therefore an **external precedent to emulate**,
  not existing dot-agents behavior to refactor — wt5-skills-integration
  is still fully net-new.
- Worktree config provisioning (`RegisterInstallProject`/`RunInstall` in
  `commands/internal/lifecycle/install.go`) — re-verified present with
  unchanged signatures; still fully unimplemented for worktrees, exactly
  as scoped to wt2/wt3/wt5 below (not wt1, correctly).
- The lesson `dead-worker-wip-reconcile-on-pickup`, cited in this plan's
  own `refresh-ideate-worktree-platform` task notes, **does not exist**
  under `.agents/lessons/` (checked the index plus a repo-wide grep — no
  hits). Treat that citation as aspirational/mistaken. The real siblings
  already in the lessons index that wt2/wt3's cleanup/registry design
  should reconcile against instead: `concurrent-workers-one-worktree`
  (one active writer per worktree; orchestrator polls, workers never
  self-arm), `worktree-isolation-defeats-status-tracking` (an isolated
  worker's status updates are invisible to the main-repo scout — wt2's
  registry must be readable from OUTSIDE the worktree that wrote it,
  e.g. under the main repo's own admin dir, not something only the
  linked worktree can see), `parallel-worker-branch-drift` (pre-commit
  hooks can land a worker's commit on a sibling worktree's branch via
  stash/restore — wt3's merge-back must verify branch HEAD matches the
  expected commit post-op), and `stale-local-checkout-mass-drift`
  (backup-branch + hard-sync recovery — relevant to wt2/wt3
  failure-recovery design).

**go-git version: no bump.** Re-checked
`https://github.com/go-git/go-git/tags` directly (not a cached search)
on 2026-07-10: `v6.0.0-alpha.4` (2026-05-18) is still the newest go-git
v6 tag — no alpha.5/beta/rc/stable has shipped upstream since the wt0
pin (v5 line separately cut `v5.19.1` the same day; irrelevant, we're on
v6). `go.mod`/`go.sum` are unchanged (`go-git/go-git/v6 v6.0.0-alpha.4`,
`go-git/go-billy/v6 v6.0.0-alpha.1`). `go build ./internal/gitwt/...
./internal/eval/sandbox/...` is clean against the pinned version, and
the `x/plumbing/worktree` exported API used by both packages
(`New`/`Add`/`Remove`/`List`/`Prune`/`Open`,
`WithCommit`/`WithDetachedHead`, the typed `ErrWorktree*` errors) is
unchanged from what wt0/wt1 verified. **Decision: keep the pin.**
Re-check before wt6-verify-close in case upstream ships a stable
v6.0.0 in the interim.

**swarm-cd reference: still current.** Re-read
`swarm-cd/swarmcd/worktree.go` on 2026-07-10 (path is now
`~/proj-docs/payout/swarm-cd/...` — the operator's home dir reorganized
from `~/Documents/payout` to `~/proj-docs/payout`; the file itself is
byte-for-byte the same pattern: same imports, same
`gitworktree.New(repo.Storer)` → `Add`/`Remove`/`Open`).
`swarm-cd/go.mod` still pins the exact pseudo-version noted in the wt0
findings (`go-git/v6 v6.0.0-20260305211659-2083cf940afa`, `go-billy/v6
v6.0.0-20260226131633-45bd0956d66f`) — no drift to reconcile.

## Skills integration

`delegation-lifecycle`, `isp`, `loop-worker` consume the platform so
orchestration naturally isolates each delegated slice in its own
worktree/sub-branch with a recorded base.

## Done criteria (when planned)

- A delegated task gets an isolated worktree+sub-branch with its true
  base recorded; merge-back/rebase uses the recorded base (no
  hand-derivation) — a deliberate stale-base scenario is caught.
- Per-worktree index proven isolated (concurrent agents cannot
  cross-stage) — closes `workflow-commit-command` finding #3.
- Cleanup prunes unchanged worktrees deterministically.
- Chosen git mechanism (A/B/C) implemented behind one typed interface;
  callers/skills bind to the interface only.
- **Config provisioning round-trip:** a created worktree has working agent
  config (skills/agents/hooks/MCP present and resolving) materialized via
  the `add`/`install` path under an ephemeral identity; `remove` tears it
  down completely and the **parent project's registration + config is
  provably untouched** (a before/after diff of the parent project's
  `projects[]` entry and AGENTS_HOME dir is identical).

## wt0 spike findings (VERIFY, 2026-05-28)

Decision A (pure go-git v6, zero shell-git) is **CONFIRMED**. The 4
residuals left open after the swarm-cd reference mining are all
resolved below. None disproved Decision A — no hybrid shell-git
fallback is required for any operation. swarm-cd was found on disk at
`/Users/nikashp/Documents/payout/swarm-cd/swarmcd/worktree.go` and
re-read: it is exactly the pattern the spec described
(`gitworktree.New(repo.Storer)` → `Add`/`Remove`/`Open`, typed errors,
TTL last-used marker stale-cleanup), using detached-HEAD + `WithCommit`.

Each residual below was proved against the version dot-agents already
pins (`go-git/v6 v6.0.0-alpha.4`) by a throwaway `go run` program (built
and run clean, not committed). The exported worktree API verified:

```go
package github.com/go-git/go-git/v6/x/plumbing/worktree
func New(storer storage.Storer) (*Worktree, error)
func (w *Worktree) Add(wt billy.Filesystem, name string, opts ...Option) error
func (w *Worktree) Remove(name string) error
func (w *Worktree) List() ([]string, error)
func (w *Worktree) Open(wt billy.Filesystem) (*git.Repository, error)
func (w *Worktree) Init(wt billy.Filesystem, name string) error
// options:
func WithCommit(commit plumbing.Hash) Option
func WithDetachedHead() Option
// typed errors:
var ErrWorktreeNotFound, ErrWorktreeAlreadyExists
```

### Residual 1 — branch-mode worktree create: **PASS**

go-git v6's `Add` already does branch-mode; swarm-cd just opted out of
it. The mechanism (verified in `worktree.go`'s `Add`):

```go
opt := &git.CheckoutOptions{Hash: o.commit}
if !o.detachedHead {            // <- omit WithDetachedHead()
    opt.Branch = plumbing.NewBranchReferenceName(name)  // branch named == worktree name
    opt.Create = true          // creates the branch
}
return work.Checkout(opt)
```

So `mgr.Add(osfs.New(path), "feature-x", gitworktree.WithCommit(base))`
(no `WithDetachedHead`) creates a linked worktree checked out onto a
**new branch `refs/heads/feature-x`**. Empirically:

- worktree `HEAD = refs/heads/feature-x` (`Head().Name().IsBranch() == true`)
- the branch ref is created in the **shared** object store, visible to
  the main repo (`repo.Reference("refs/heads/feature-x")` resolves).
- shell `git worktree list` agrees: the linked dir is `[feature-x]`,
  not detached.

**Constraints a wt1 implementer must honor (load-bearing):**

1. **Branch name == worktree name**, and the name must match
   `^[a-zA-Z0-9\-]+$` (enforced by `worktreeNameRE` in `Add`). Slashes,
   dots, underscores are rejected. Sanitize/encode caller branch names
   into this charset (swarm-cd uses an md5-digest `wt-%x` scheme for the
   worktree name and a separate path — adopt that: derive a safe
   worktree *name* from a hash and keep the human branch/path mapping in
   our wt2 registry).
2. `Add` always does `Create=true` for branch mode — it creates a
   **new** branch and **errors if the branch already exists**
   (`a branch named "refs/heads/<name>" already exists`, verified).
   For "worktree on an *existing* branch," wt1 must either (a) create
   the worktree detached on the branch's tip commit then
   `Checkout(existing-branch)` via the opened worktree, or (b) ensure
   the branch does not pre-exist. The common delegation flow (new
   sub-branch per delegated slice) hits the supported happy path
   directly. This is an API shape to wrap, **not** a fallback to shell.

### Residual 2 — per-worktree index isolation: **PASS**

`mgr.Open(osfs.New(path)).Worktree()` yields a worktree whose index and
HEAD are independent of the main repo's. Empirically, with the main
repo on `refs/heads/master` and the linked worktree on
`refs/heads/feature-x`:

- committing `wt-only.txt` in the linked worktree advanced
  `feature-x` to `49473e3` while main's `master` stayed at `acc64a2`
  — the branches diverged independently, the commit did **not** touch
  main's branch.
- main repo `Status()` is **clean** (zero entries); `wt-only.txt` is
  not in main's working tree or index.
- conversely, staging `main-only.txt` in the main repo does **not**
  appear in the linked worktree's `Status()` staging area.

This is the exact guarantee `workflow-commit-command` finding #3 binds
to: a `da workflow commit` run inside a managed worktree can only stage
that worktree's tree. (Note: the object **store** is shared — that is
correct and desired; isolation is at the index/HEAD/working-tree level,
which is what matters for concurrent committers. Each `Open` builds a
fresh `filesystem.NewStorage` over the worktree's dual-FS, so the
per-worktree index lives under `.git/worktrees/<name>/`.)

### Residual 3 — List support: **PASS (native)**

go-git v6 exposes `func (w *Worktree) List() ([]string, error)` which
enumerates `.git/worktrees/<name>` dirs. Verified: returns
`["feature-x"]` after one add. So enumeration is native — wt2 does
**not** need a registry merely to *list* worktrees. The wt2 registry is
still required for the **semantic** metadata go-git does not store
(true base ref, parent PR, purpose, created-at, last-used) — `List`
gives names, the registry gives meaning. Reconcile the two by keying
the registry on the worktree name returned by `List`.

### Residual 4 — pinned go-git v6 version: `v6.0.0-alpha.4`

**Pin `github.com/go-git/go-git/v6 v6.0.0-alpha.4`** (with
`github.com/go-git/go-billy/v6 v6.0.0-alpha.1`). dot-agents **already
pins exactly this** in `go.mod`/`go.sum` (resolved time
`2026-05-18T13:59:25Z`), and the `x/plumbing/worktree` package is
present and imports clean under it — so wt1 adds **no new
supply-chain surface** for the worktree mechanism beyond what is already
vendored.

Notes for the implementer:

- `alpha.4` is a **tagged** release (cleaner to pin than a
  pseudo-version). It is *newer* than the pseudo-version swarm-cd uses
  (`v6.0.0-20260305211659-2083cf940afa`); the only delta in the
  worktree pkg is that `alpha.4` adds extra `Storer`/`Close` safety in
  `Add`/`Open` (it avoids closing the shared storer and cleans up the
  storage on `Open` error). The branch-mode `Add` logic and the options
  are byte-identical between the two. So adopting swarm-cd's pattern on
  `alpha.4` is strictly safe.
- `osfs.New(path)` (`go-git/go-billy/v6/osfs`) is the `billy.Filesystem`
  passed to `Add`/`Open`, matching swarm-cd.

### Typed interface, AS SHIPPED (wt1, PR #181, merged 2026-05-29)

`internal/gitwt/gitwt.go` shipped a richer interface than the seam
sketched above — `RecordBaseRef`/`BaseRef` and `Prune` landed as **wt1**
primitives instead of being deferred to wt2, and `Remove` takes the
working-tree `path` directly (matching the "wraps mgr.Remove +
os.RemoveAll(path)" comment literally — no adapter needed):

```go
// internal/gitwt — implementation wraps go-git v6 x/plumbing/worktree.
type Manager interface {
    AddBranch(name, path string, base plumbing.Hash) error
    AddDetached(name, path string, commit plumbing.Hash) error
    Remove(name, path string) error
    List() ([]string, error)
    Prune() ([]string, error)          // admin-metadata cleanup for dirs gone from disk
    Open(path string) (Worktree, error)
    RecordBaseRef(name string, base plumbing.Hash) error
    BaseRef(name string) (plumbing.Hash, error)
}

// Worktree — per-worktree index/status/commit/branch ops. Shipped in wt1;
// TestIndexIsolation proves Stage/Commit here never touches another
// worktree's index or the main repo's.
type Worktree interface {
    Stage(path string) error
    Status() (git.Status, error)
    Commit(message string, opts *CommitOptions) (plumbing.Hash, error)
    Head() (*plumbing.Reference, error)
    Branch() (string, error)
}
```

Note `Manager.Prune()` is narrower than wt2's "auto-prune-if-unchanged"
goal: it only reclaims admin metadata for worktrees whose **directory is
already gone from disk** (manual/out-of-band deletion), mirroring
`internal/eval/sandbox`'s `PruneStale` pattern. It does **not** decide
"unchanged" (no commits past the recorded base) — that staleness policy,
plus the rich metadata (purpose, parent PR, created-at, last-used), is
still wt2's job, now layered on top of `RecordBaseRef`/`BaseRef` rather
than inventing base-ref storage itself.

wt1 binds callers to `Manager`; the go-git mechanism stays behind it so
a future swap (or the hybrid fallback, which this spike showed is **not
needed**) is invisible.

## Relationships (reconciled 2026-07-10)

- **`workflow-commit-command` already shipped (`status: completed`,
  through `wc-verify-close`) — WITHOUT waiting on this plan.** Its own
  spec (`specs/workflow-commit-command/design.md`) still declares
  "Blocked-by / unblocked-by `worktree-platform` (`wt4-index-isolation`)"
  and "sequence after worktree-platform wt4", but wt4 is still `pending`
  here — that declared dependency was never actually honored. In
  practice `commit_pathset.go`/`commit_cmd.go` solved "concurrent agents
  race the shared index" with **deterministic scoped path-set
  derivation** inside the single main index/worktree (never `-A`,
  managed-root allowlist), not physical per-worktree isolation. So the
  original "unblocks" framing below was aspirational for that specific
  plan — corrected here rather than repeated.
- **wt4-index-isolation's core guarantee is already proven by wt1**
  (`TestIndexIsolation` — see "Refresh" above). wt4's genuine residual
  is a real **concurrent** (goroutine + `-race`) version of that test
  (today's is sequential) plus formalizing it as a done-criterion — not
  a from-scratch build. Its actual future consumer is
  wt5-skills-integration (and transitively any worker-bundle-authoring
  commit-2 caller running inside a linked worktree), not
  `workflow-commit-command`.
- **New: `worker-bundle-authoring` plan (payout workspace) tasks
  `commit-1-task-pathset` + `commit-2-cli-scoped-mode`** (both
  `pending`) extend the *same files* wt3's write_scope also touches
  (`commands/workflow/commit_pathset.go`, `commit_cmd.go`) with
  **task-scoped** staging (`--scope task`: only the active task's
  write_scope, not every managed-root path) — still inside the single
  main index, no worktree/gitwt dependency. No write_scope collision
  today (wt2-wt4 stay in `internal/gitwt/`), but a real sequencing
  opportunity: once wt5 moves delegated workers into linked worktrees,
  each worker's `da workflow commit` step should call commit-2's
  `--scope task` mode rather than wt5 re-inventing path scoping. **wt5
  should depend on / consume `commit-2-cli-scoped-mode`'s CLI surface
  once it ships**, not duplicate it; if wt5 becomes ready first, land a
  thin scoped-commit call the delegation skill can swap onto commit-2
  later.
- Sibling of `graphstore-concurrency-contract` (same "concurrent
  short-lived agents" theme, different resource).
- If the typed git layer becomes an injected dependency, it follows
  the `di-refactor-rollout` Deps/contract pattern (sequencing note).
- Independent of `coverage-gate-per-file`.
