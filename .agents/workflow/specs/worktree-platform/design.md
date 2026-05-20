# Spec: managed worktree platform for delegation/branch isolation

Status: draft / open-decision (maintainer-proposed 2026-05-17). Needs
the git-layer fork (below) decided before it graduates to a plan.

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

## Relationships

- **Unblocks `workflow-commit-command`** (index isolation) — that plan
  should depend on this; its atomicity/locking finding is largely
  subsumed.
- Sibling of `graphstore-concurrency-contract` (same "concurrent
  short-lived agents" theme, different resource).
- If the typed git layer becomes an injected dependency, it follows
  the `di-refactor-rollout` Deps/contract pattern (sequencing note).
- Independent of `coverage-gate-per-file`.
