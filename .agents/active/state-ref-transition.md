# Temporary workflow-state ref transition — dot-agents

> Remove this file after `git-ref-work-backend/document-and-default-git-ref` is complete, controlled multi-plan validation is green, active state has been migrated, and git-ref is the documented default backend.

## Additive opt-in ENABLED (user-directed, 2026-07-17)

Per user direction to "use the `refs/agents/state` ref backend for the workstore," the **shipped additive opt-in** is now enabled in `.agentsrc.json`:

```json
"work_tracking": { "write_to": "state-ref" }
```

This is the ADDITIVE stopgap, **not** the canonical migration below (its gate is still CLOSED):

- `write_to: state-ref` — each status transition is ALSO mirrored to `refs/agents/state` via atomic CAS (16-retry). The per-worktree working copy is STILL written every time and REMAINS canonical; the ref mirror is additive and never merged to a code branch (D10). Verified live: `da workflow advance` moved the ref 7bc27b87→d1403937, mirroring plan `TASKS.yaml`/`PLAN.yaml` while preserving the co-located session's `active/coordination/` files.
- `read_from` is LEFT AT `worktree` (default). `read_from: master` was tried and reverted: it re-reads canonical state from stale `origin/master` on every transition, so sequential same-checkout advances **clobber each other's** working-copy edits (read-your-writes violation — see `.agents/lessons/read-from-master-clobbers-sequential-writes/`). It is a worktree-worker isolation shim, unsafe as a repo-wide default until that footgun is fixed (part of `workstore-git-ref-backend`).
- Scope of the mirror today: a plan's `TASKS.yaml` + `PLAN.yaml` only (`collectPlanStateFiles`). It does NOT cover the rest of `.agents/active|history|workflow` — the general WorkStore backend for those is unbuilt (draft `work-tracking-storage-abstraction` spec), and is the gated work below (`workstore-git-ref-backend`).
- `refs/agents/state` is SHARED with a co-located session (lane claims under `active/coordination/`). CAS preserves distinct paths (`TestWriteStateRefCAS_ConcurrentRMWPreservesAllWriters`) — never force-update; always CAS.


## Current authority

Do not infer that `refs/agents/state` is the canonical workflow backend merely because the ref exists. During transition:

- canonical `PLAN.yaml` / `TASKS.yaml` remain on the currently configured backend;
- `refs/agents/state` carries coordination/design artifacts AND (with the opt-in above) an additive, non-canonical mirror of plan `TASKS.yaml`/`PLAN.yaml`; it is still NOT the canonical backend;
- workers never edit a worktree copy of `TASKS.yaml` directly;
- Main serializes canonical mutations through repository-HEAD `da workflow` commands.

At orchestration start, build/use repository-HEAD da and run:

```sh
da workflow orient
da --json workflow slots
da --json workflow eligible
```

Inspect `git-ref-work-backend`. If `document-and-default-git-ref` is not complete, or HEAD exposes no documented backend-selection/migration command, the migration gate is closed.

## Transition behavior

1. Detect the documented active backend through da, never by file presence.
2. Read and write workflow state only through da surfaces.
3. Workers emit coordination, merge-back, and refinement artifacts; Main performs state transitions one at a time.
4. After each write, re-read the affected task, slots, and eligible state from a fresh process.
5. Coordination-ref commits use compare-and-swap. A CAS failure requires semantic reconciliation before retry.
6. Never partially migrate a subset of active plans while other plans continue writing the old backend.

`da` protects each TASKS mutation with the plan's cross-process advisory lock, so
concurrent commands cannot tear or corrupt the file. The outer driver's
`.driver-lock` excludes only another full-loop driver; it does not exclude an
ad-hoc `da` command in another terminal. A read→decide→write sequence still has a
semantic TOCTOU window across separate da invocations. Fresh pre-mutation reads,
transition validation, and post-write reads are therefore mandatory.

## Migration gate

Migration may start only when all are true:

- `git-ref-work-backend/document-and-default-git-ref` is complete;
- read-from-ref, CAS writes, per-task state, commit decoupling, and WorkStore adoption tests are green;
- `full-loop-orchestration-runtime/controlled-multiplan-validation` is complete;
- no active wave is between fanout and reconciliation;
- pending writes and delegation closeouts are drained;
- the shipped da command documents migration and rollback.

## Atomic cutover and rollback

Immediately before cutover, record the source backend identity/digest, old `refs/agents/state` object ID, normalized active-plan/task projection, active delegations/fold-backs, and iteration pointers. Run the shipped migration command once. Compare normalized source and ref-backed projections for every active task before enabling new writes. Any mismatch aborts and restores the recorded backend/ref through the documented rollback or CAS command. Never repair a partial migration by hand-editing YAML.

## Removal condition

Delete this file and its overlay callout when git-ref is the default, all active state reads back through da, rollback has been exercised, and no legacy worktree-state writer remains.
