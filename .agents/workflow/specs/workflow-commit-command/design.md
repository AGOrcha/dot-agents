# Spec: `da workflow commit` — workflow-state/git commit coupling (Part 2)

Status: accepted (graduated from global proposal
`workflow-state-commit-coupling`, Part 2; maintainer-directed
2026-05-17). Self-contained here so the repo does not depend on the
archived `~/.agents/proposals/` artifact.

## Problem

`da workflow` mutates on-disk canonical state
(`.agents/workflow/plans/**`, `.agents/history/**`, checkpoints,
verify/fanout/merge-back artifacts) but never stages or commits it — git
tracking is a separate manual step. State silently drifts from history
between sessions; a `git reset`/branch switch destroys uncommitted
canonical-store progress (observed live, the originating incident).
Part 1 (the global operator-discipline rule) is already applied and
active; it is the stopgap. This spec is the durable tooling fix.

## Decisions

1. **First-class `da workflow commit` subcommand.** Stages the
   workflow-state paths changed since the last workflow-state commit and
   commits them with a generated message; idempotent; no-op when clean.
2. **iteration-close integration.** The verify→checkpoint→
   advance/merge-back close path ends by invoking the same staged commit
   so the iteration-log entry and the state commit are atomic.
3. **Deterministic, scoped path set — never `git add -A`.** The staged
   set is *derived*, not a blanket add: managed `.agents/workflow/**`,
   `.agents/history/**`, and session-touched state files only.
   Submodule-pointer and pre-existing-untracked entries are excluded
   (mandatory, not optional). Never stage a path not derived from the
   workflow mutation set.
4. **Headless-safe.** Works with raw `da` (no Claude Code hook
   dependency); a settings hook is NOT an acceptable sole mechanism.
5. **Per-project opt-out** via workflow prefs for repos that
   intentionally manage state commits elsewhere.
6. **`--dry-run`** prints the exact `git add` set and the commit message,
   makes no changes.

## Requirements (behavioral)

- After any `workflow plan|task|advance|checkpoint|archive|
  fanout|merge-back|fold-back` mutation, running the command (or closing
  the iteration) leaves the managed state paths clean.
- Generated message notes state was updated via `da workflow` and
  committed to keep the canonical store and git history in sync; the
  state commit is distinct from code commits.
- Opt-out pref short-circuits to a no-op (with a clear status line).
- Deterministic: same mutation set ⇒ same staged path set; never stages
  outside the derived set even if the worktree is otherwise dirty.

## Done criteria (verifiable)

- `da workflow commit --dry-run` lists only workflow-state paths and
  excludes submodule / pre-existing-untracked noise (test with a
  deliberately dirty worktree + a submodule pointer).
- After a representative `plan/task/advance/checkpoint/archive`
  sequence, managed state paths are clean post-command; a second run is
  a clean no-op (idempotent).
- `iteration-close` leaves zero uncommitted canonical-store drift.
- Opt-out pref → documented no-op.
- Headless: passes driven by raw `da` with no Claude Code env.
- Coverage gate stays green for touched packages.

## Scope / deferred

- **In:** `commands/workflow/` (new `commit` subcommand + path
  derivation + dry-run + prefs gate), iteration-close path,
  workflow-prefs key, tests.
- **Deferred / out:** auto-commit of *code* deliverables (this is
  workflow-state only); any daemon/watcher; cross-repo orchestration.
- Independent of `graphstore-concurrency-contract` and
  `di-refactor-rollout` (different subsystem).

## Relationship

- Graduates Part 2 of `workflow-state-commit-coupling` (Part 1 = the
  active global rule, unchanged).
- **Depends on `pr3b-workflow`** (the workflow subpackage extraction in
  `pr10-branch-split` / PR #16) — implement as a follow-up **after
  0.3.1 ships**; do not fold into the approved #16.
- **Blocked-by / unblocked-by `worktree-platform` (`wt4-index-isolation`).**
  The adversarial finding #3 (`git commit` stages the whole shared
  index; concurrent agents race it) is structurally resolved by
  per-worktree index isolation. This plan's single-writer / scoped-
  staging requirement binds to that contract — sequence after
  worktree-platform wt4.
