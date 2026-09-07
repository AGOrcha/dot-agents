# Concurrency-safe writes for the local workflow store

**Spec ID**: workflow-store-concurrency
**KG Briefing**: Generated 2026-07-22 — 0 prior KG decisions (graph holds code nodes only; grounded in code + spec + lessons per kg-brief degraded mode), 1 governing spec (`work-tracking-storage-abstraction` D2/D3/D5), 3 applicable lessons, 1 originating proposal
**Status**: draft

## Problem Statement

`da workflow` performs unsynchronized read-modify-write (RMW) on the shared canonical
coordination files of the **default `local` backend** — the per-plan task file, the plan
file, delegation contracts, and delegation bundles. Only ONE writer path is serialized
today: the task-update RMW acquires a cross-process advisory lock on the plan's task file.
Every other RMW writer — fold-back task-note edits, delegation→advance status writes,
closeout, merge-back status edits, contract materialization, bundle writes — takes NO lock.

A lock on one of N writers does not serialize the file. So two concurrent `da` processes
(parallel workers, an orchestrator plus a worker, two status transitions) can each load the
same file, mutate a different slice, and write back, and the later writer silently clobbers
the earlier writer's update. A lost-update was confirmed live in board reconcile. This is
the exact reason a running session must serialize `da workflow` plan creation by hand rather
than trust the store.

A second, distinct integrity gap: the canonical save primitives write in place (marshal →
overwrite), with no temp-file-plus-rename. A crash or interrupt mid-write can leave a
truncated or partially written file even for a single writer — a torn write, independent of
the lost-update race.

This matters now because the surface of concurrent `da` invocation is growing (loop workers,
orchestrator reconciliation, background daemon, hooks that shell out to `da`), and the
working-copy files remain authored even under the `git-ref` backend (the ref mirror is
additive on top of the local save), so a torn or clobbered working copy is reachable in every
backend, not just `local`.

## Goals

- Every canonical RMW writer of the local workflow store observes a single, consistent
  serialization contract, so no concurrent `da` process can lose another's committed update.
- The serialization is **impossible to forget** by construction: a new RMW writer added
  without the guard is the exception a reviewer/lint can flag, not the silent default.
- Each canonical write is crash-atomic: an interrupted write never leaves a truncated or
  partially serialized file on disk.
- The contract is cross-platform correct, including the constraint that the lock is released
  before any rename/remove step on platforms where a held handle blocks those operations.
- The existing single locked writer is unified onto the shared contract — one lock
  implementation, not two.
- No behavioral change for the single-writer common case beyond durability: reads, file
  formats, and CLI outputs are unchanged.

## Decisions

- **D1 — Serialize at the load-modify-write helper, not per call site.** The guard is pushed
  DOWN into a small set of RMW helpers that own load → mutate → save as one critical section,
  so every command routes its mutation through a helper that already holds the lock. Rejected
  alternative: sprinkling the existing lock call at each RMW site — that is precisely how the
  first pass serialized 1 of 8 writers and left 7 racing.
- **D2 — Reuse the existing shared cross-process advisory lock primitive.** The repo already
  ships a cross-process, cross-platform advisory file lock used for the config lock and the
  single locked workflow writer today; the contract builds on that primitive rather than
  introducing a second locking mechanism. One lock implementation across the store.
- **D3 — Lock scope is keyed to the unit of contention, not one global mutex.** Distinct
  plans, and distinct coordination artifacts, must remain independently writable so unrelated
  concurrent work does not serialize needlessly (preserving the disjoint-task concurrency the
  status-transition mirror already relies on). The scope key granularity is an open question
  (see below), but the contract is scope-keyed, not process-global.
- **D4 — Atomic write underneath the lock.** The save step writes to a temporary file and
  renames into place, so a crash mid-write cannot truncate the canonical file even within the
  held lock. This composes with D1 (the lock guards the read-your-write ordering; the atomic
  rename guards the durability of the byte write itself).
- **D5 — Bounded acquisition with a clear failure, never a silent unlocked proceed.** A writer
  that cannot claim the lock within a bounded wait surfaces a wrapped, actionable error rather
  than proceeding without serialization. Retry is bounded and observable.
- **D6 — Interim contract, convergent with the storage abstraction.** This is the hardening of
  the `local` backend's write path, explicitly the interim contract the storage-abstraction
  spec's `WorkStore` facade (its D2/D3/D5) will later subsume. The helpers introduced here are
  the seam the eventual `localfs` `WorkStore` wraps — this narrows, not widens, the future
  migration. The `git-ref` backend's own ref-plane concurrency (compare-and-swap on the state
  ref) is out of scope and already handled; this spec governs the working-copy file plane that
  every backend still writes.

## Requirements

- All canonical RMW writers of the local workflow store — plan task file, plan file,
  delegation contract, delegation bundle — complete their load-modify-write while holding the
  scope-keyed advisory lock for the artifact they mutate.
- A writer added to the store without going through a guarded RMW helper is detectable in
  review (bare write-only primitives are reserved for first-creation, not RMW).
- Concurrent transitions to disjoint units (different plans, or disjoint artifacts) proceed
  without serializing against each other.
- A concurrent write to the SAME unit by N processes preserves all N committed updates — none
  is lost.
- An interrupted write leaves either the prior complete file or the new complete file, never a
  truncated one.
- Lock acquisition that cannot succeed within the bounded window fails with a clear error and
  does not fall through to an unlocked write.
- The lock is released before any rename/remove that a platform would block on a held handle.
- The previously single locked writer uses the same contract; there is exactly one lock
  implementation for the store.

## Open Questions

- **Scope-key granularity.** Is the lock keyed per canonical file (plan-task file, plan file,
  each contract file, the bundles directory), or per plan (one key covering that plan's task
  file + plan file + its contracts)? Per-file maximizes concurrency but a fold-back that edits
  both the task file and the plan file would then need two keys in a fixed order to stay
  deadlock-free; per-plan is simpler and deadlock-free but serializes a plan's task-file and
  plan-file writers against each other. The plan phase must choose and justify.
- **Whether to generalize the existing named lock helper or introduce a store-scoped guard.**
  The existing helper is plan-task-file specific by name; the contract needs a scope-keyed
  form. Rename/generalize in place, or add a store-lock entry point on the shared lock package
  that the task-file helper then delegates to? (Consolidation lesson: a "generalize this
  primitive" ask usually means consolidate the duplication, not just widen the signature.)
- **Retry/timeout policy surface.** Is the bounded acquisition window the shared primitive's
  existing default, or does the store contract need its own (longer, because a wave of workers
  can queue on one hot plan)? Does a timeout warrant a retry loop or an immediate actionable
  error?
- **Delegation bundle key.** Bundles are written into a shared directory rather than a
  per-plan file; do concurrent bundle writers need a directory-scoped key, or is per-bundle-id
  file-atomic write (D4) sufficient without a lock because writers never target the same file?

## Done Criteria

- A concurrency test launches N concurrent processes/goroutines that each mutate a DIFFERENT
  slice of the SAME plan's canonical state (e.g. distinct task notes, or a task-note edit plus
  a plan-status edit) and asserts all N updates survive after all complete — the lost-update
  is closed. The test is deterministic (real contention with a barrier, no sleeps).
- A test proves a writer that cannot acquire the lock within the bounded window returns a
  clear wrapped error and does NOT write unlocked.
- A test proves an interrupted/failed write leaves the prior complete file intact (atomic
  rename), never a truncated file.
- Every canonical RMW writer enumerated in the plan routes through a guarded helper — verified
  by a search/review gate that no RMW site calls a bare write-only save primitive.
- Cross-platform test suite passes, including the release-before-rename ordering constraint.
- Exactly one advisory-lock implementation remains for the workflow store.

## Deferred

- The `WorkStore` interface and any `localfs`/`kg`/`git-ref` backend abstraction — owned by
  `work-tracking-storage-abstraction`; this spec is the interim `local`-plane hardening that
  its `localfs` facade later wraps.
- The `git-ref` backend's ref-plane compare-and-swap concurrency — already delivered by the
  archived `git-ref-work-backend` plan; unchanged here.
- Orchestration-level worker serialization (wave barriers, leases) — owned by
  `full-loop-orchestration-runtime`; that reduces contention but does not make the store
  itself safe, which is this spec's job.
- Any change to the config lock (`.agentsrc.lock`) write path — already hardened
  (config-v2 interprocess lost-update protection).

## Related

- Originating proposal: `.agents/active/ideation-inbox/workflow-store-concurrency-safe-writes.md`
  (follow-up to the task-update lock that guarded 1 of 8 writers).
- Spec: `work-tracking-storage-abstraction` — D2 (backend is SOT for status; local is the
  working copy), D3 (`WorkStore` facade), D5 (atomic transitions). This spec hardens the
  `local` plane those decisions treat as the working copy / migration bridge.
- Lessons: `git-ref-backend-structural-writes-must-mirror` (fix at the canonical-write choke
  point; the same choke point is the lock home); `consolidate-vestigial-siblings-on-rename`
  (a "generalize the primitive" ask means consolidate the duplication); `leverage-cross-platform-fs-helpers`
  and `live-smoke-must-run-on-every-target-os` (the release-before-rename / atomic-write
  behavior must be exercised on every target OS, not `runtime.GOOS`-branched).
- Precedent: the config-lock interprocess lost-update protection (compare-and-merge-retry OR
  advisory lock held across the read-modify-write, preserving atomic temp+rename) is the shape
  the store contract mirrors.
