# git-ref coordination-state backend + read-from-master shim

Produced by the **kg-ideate** pipeline (2026-07-11) from proposal
`.agents/proposals/read-task-state-from-master-source.md`. Implements
`work-tracking-storage-abstraction/design.md §9` (D9/D10).

## Phase 1 — briefing (kg-ideate)

- **KG traversal:** graph is READY (14.5k nodes) but holds **code** nodes only —
  no SDD decision/contradiction nodes for this topic (KG-as-SOT ingest not
  populated). Degraded per skill: ground in the spec + lessons, don't fabricate.
  Impact radius of the hub file `commands/workflow/plan_task.go` = 125 direct /
  175 impacted nodes / 9 files (seeds write-scopes below).
- **Prior decisions (spec):** `work-tracking-storage-abstraction` D1 (two planes),
  D1′ (KG-as-SOT tiers), D2 (backend is SOT for status; reads resolve against the
  backend, not the per-worktree YAML), D5 (atomic transition = re-dispatch fix),
  D8 (scope declares SOT; default `local`), §3B (the FS projection IS the agent's
  interface — zero new semantics).
- **Lessons:** `worktree-isolation-defeats-status-tracking` (the exact pain: 5×p1c
  re-dispatch storm; "the real fix is a shared coordination backend"),
  `stale-local-master-ref` / `stale-local-checkout-mass-drift` (read origin, not the
  stale local ref), `single-source-of-truth-across-specs-and-plans` (⇒ the spec was
  **amended**, not duplicated), `workflow-task-update-replaces-notes` (⇒ payout notes
  left intact, not clobbered).
- **Gap the design fills:** §2 backend ladder jumps `local` → `kg` (daemon+graph).
  `git-ref` is the missing rung: git-native shared SOT, no daemon.

## The decision (spec §9)

- **D9 `git-ref` backend** — state on `refs/agents/state` (configurable), orthogonal
  to the code branch; read via git / a shared linked worktree; write via `update-ref`
  CAS; per-task state files to avoid line-conflicts. Same `WorkStore` (D3) + scope
  ladder (D8). The sane default upgrade from `local`; graduate `local → git-ref → kg`
  with zero agent-facing change.
- **D10 (answers "do we sync the ref to the default branch?") — NO.** The ref is a
  parallel lineage (like `refs/notes/*`), never merged into `main`. It syncs **ref ↔
  remote** (push/fetch `refs/agents/state`); worktrees on one host share it natively;
  an optional **one-way** audit snapshot may project ref → `main`, never a merge back.

## Task graph (dep-ordered)

1. `spec-git-ref-backend` — ✅ completed (spec §9 amendment, this run).
2. `read-from-master-shim` — **focus**; `work_tracking.read_from=master` so
   `loadCanonicalTasks` + scout eligibility read the canonical ref, not the worktree
   copy. **Ship first** — cheapest fix for the re-dispatch storm.
3. `git-ref-state-ref-write` — write transitions to the ref via `update-ref` CAS.
4. `per-task-state-files` — split status into per-task files under the ref.
5. `decouple-coordination-commits` — stop staging `.agents/workflow/**` into code
   commits (folds/re-scopes the commit-scope thread).
6. `workstore-git-ref-backend` — expose behind `WorkStore` + `work_tracking.backend`.

## Cross-plan relations

- **Supersedes/re-scopes** payout `worker-bundle-authoring:commit-1-task-pathset` +
  `commit-2-cli-scoped-mode` (cross-repo — coordinate before implementing those;
  with a state ref, task-state isn't committed into code branches at all).
- **Complements** dot-agents `worktree-platform` (wt4 index-isolation): worktree
  isolation for the *code* plane, git-ref for the *coordination* plane.
- **Converges with** `work-tracking-storage-abstraction`'s `kg` end-state on the same
  `WorkStore` seam; does not compete with it.
- **Unblocks in spirit** `worker-bundle-lessons` (sane cross-worktree bundle status),
  though not a hard dependency.

## Phase 4 — execution handoff

`kg-ideate` produces no code. Direct-vs-fanout: `read-from-master-shim` is a small,
self-contained read-path change — **direct** work for the da loop/orchestrator.
Tasks 3–6 are sequential and touch the workflow-store hub (`plan_task.go`) — run
them serially (not fanned out) to avoid store-write collisions, per
`concurrent-workers-one-worktree`. The plan is **active** so the loop picks up
`read-from-master-shim` as the next eligible task here.
