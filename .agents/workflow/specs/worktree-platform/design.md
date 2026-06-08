# Spec: managed worktree platform for delegation/branch isolation

Status: active / git-layer decision VERIFIED (wt0 spike, 2026-05-28).
Decision A (pure go-git v6) confirmed feasible incl. all 4 residuals —
see "wt0 spike findings" below. Ready to graduate to wt1 implementation.

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

The dot-agents **Go binary barely does git today** (3 files). The
worktree/sub-branch usage that hurts is in the **orchestration + skill
layer and manual operator steps**, not the binary. So this is mostly a
**net-new subsystem**, not a refactor of existing shell-git. Whether it
lives as a `da worktree`/git package in the binary (consumed by skills)
vs purely in the skill/runtime layer is itself a scoping decision the
plan must make. `go-git` is **not currently a dependency** — adopting
it is a sizable new supply-chain surface (cf. the maintainer's own
testcontainers concern).

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

### Typed interface seam for wt1 (derived from the verified API)

```go
// internal/gitwt — implementation wraps go-git v6 x/plumbing/worktree.
type Manager interface {
    // Create a linked worktree at path on a NEW branch (branch name derived
    // from name; must match ^[a-zA-Z0-9-]+$). Records base in the wt2 registry.
    AddBranch(name, path string, base plumbing.Hash) error
    // Create detached (swarm-cd style), for read-only/ephemeral checkouts.
    AddDetached(name, path string, commit plumbing.Hash) error
    Remove(name string) error          // wraps mgr.Remove + os.RemoveAll(path)
    List() ([]string, error)           // wraps mgr.List()
    Open(path string) (Worktree, error)// wraps mgr.Open -> repo.Worktree (own index)
}
```

wt1 binds callers to `Manager`; the go-git mechanism stays behind it so
a future swap (or the hybrid fallback, which this spike showed is **not
needed**) is invisible.

## Relationships

- **Unblocks `workflow-commit-command`** (index isolation) — that plan
  should depend on this; its atomicity/locking finding is largely
  subsumed.
- Sibling of `graphstore-concurrency-contract` (same "concurrent
  short-lived agents" theme, different resource).
- If the typed git layer becomes an injected dependency, it follows
  the `di-refactor-rollout` Deps/contract pattern (sequencing note).
- Independent of `coverage-gate-per-file`.
