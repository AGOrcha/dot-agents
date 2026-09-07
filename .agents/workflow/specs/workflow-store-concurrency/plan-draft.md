# Plan draft — workflow-store-concurrency

> DRAFT ONLY. Not created via `da workflow plan create` (the shared workflow store is not
> concurrency-safe for concurrent creation — the very defect this plan fixes; the parent
> serializes plan creation). Promote with `da workflow plan create workflow-store-concurrency`
> when the store is quiescent, then sync PLAN.yaml / TASKS.yaml / `<id>.plan.md`.

Spec: `.agents/workflow/specs/workflow-store-concurrency/design.md`

## PLAN.yaml (proposed)

```yaml
spec_ref: .agents/workflow/specs/workflow-store-concurrency/design.md
concurrency_mode: concurrent-folded
concurrency_rationale: |-
  Decisions are clear from the code + originating proposal + the config-lock precedent; the
  open questions (scope-key granularity, generalize-vs-add lock entry point, bundle key) shape
  HOW p0/p1 are built but do not reorder tasks or change write-scopes. The atomic-write task
  (p2) is orthogonal to the lock task (p0) and can be authored alongside it. A thin spec is
  co-authored with this plan rather than fully stabilized first.
```

## TASKS.yaml (proposed)

```yaml
- id: p0-generalize-store-lock
  title: Generalize the plan-task advisory lock into a scope-keyed workflow-store lock
  status: pending
  write_scope:
    - internal/agentslock/
    - commands/workflow/plan_task.go
  depends_on: []
  blocks: []
  verification_required: true
  notes: |-
    Promote today's `withTasksLock` (commands/workflow/plan_task.go) — a cross-process advisory
    lock via agentslock.AcquireFileLock keyed on a plan's TASKS.yaml path — into a scope-keyed
    store guard (e.g. WithStoreLock(projectPath, scopeKey, fn)) so plan-file, delegation-contract,
    and bundle writers can share ONE lock implementation. Resolve open question "generalize the
    named helper vs add a store-scoped entry point on agentslock": prefer consolidating (per
    lesson consolidate-vestigial-siblings-on-rename) — one primitive, `withTasksLock` folds onto
    it. Keep the shared agentslock public API stable (also used by config/KG/units locks) and keep
    the Windows release-before-rename ordering (lesson live-smoke-must-run-on-every-target-os).
    Verified: existing TASKS.yaml lock tests still pass; new unit test that two scope keys are
    independent and the same key serializes; GOOS=windows go vet clean.

- id: p2-atomic-canonical-write
  title: Make canonical store saves crash-atomic (temp file + rename)
  status: pending
  write_scope:
    - commands/workflow/plan_task.go
    - commands/workflow/delegation.go
  depends_on: []
  blocks: []
  verification_required: true
  notes: |-
    saveCanonicalTasks (plan_task.go ~1530), saveCanonicalPlan (~1464), and saveDelegationContract
    (delegation.go ~144) currently marshal then overwrite in place via osWriteFile — NOT atomic, so
    an interrupted write can truncate the canonical file. Route the byte write through the repo's
    atomic writer (fsops.WriteFileAtomic, the same helper agentslock.Flush uses) so a crash leaves
    the prior or the new complete file, never a partial one. Preserve file mode + the existing
    state-ref mirror ordering (save local, then mirror). Orthogonal to p0 — foldable alongside it.
    Verified: a test that fails the rename step (injected fault) leaves the prior file intact;
    existing round-trip save/load tests pass.

- id: p1-rmw-helpers-at-chokepoint
  title: Add lock-owning load-modify-write helpers at the canonical-write choke point
  status: pending
  write_scope:
    - commands/workflow/plan_task.go
    - commands/workflow/delegation.go
    - commands/workflow/contract_core.go
  depends_on:
    - p0-generalize-store-lock
  blocks: []
  verification_required: true
  notes: |-
    Add mutateCanonicalTasks / mutateCanonicalPlan / mutateDelegationContract helpers that do
    load -> mutate(fn) -> save under WithStoreLock as ONE critical section, layered onto the
    EXISTING choke points saveCanonicalTasksMirrored (plan_task.go ~1545) and
    saveCanonicalPlanMirrored (~1591) so the lock and the state-ref mirror share one funnel. Bare
    saveCanonical*/saveDelegationContract become write-only primitives for first-creation only.
    Resolve open question "scope-key granularity" here (per-file vs per-plan; if per-file and a
    single op touches two files, acquire keys in a fixed total order to stay deadlock-free) and
    record the choice in plan notes. Verified: unit test that the helper serializes a same-key
    RMW and that disjoint keys do not block; helpers hold the lock across load..save.

- id: p3-migrate-unguarded-writers
  title: Route every unguarded canonical RMW writer through the guarded helpers
  status: pending
  write_scope:
    - commands/workflow/delegation.go
    - commands/workflow/contract_core.go
    - commands/workflow/eligible_accounting.go
  depends_on:
    - p1-rmw-helpers-at-chokepoint
  blocks: []
  verification_required: true
  notes: |-
    Migrate the confirmed unguarded RMW sites to the p1 helpers: delegation.go fold-back task-note
    + fold-back plan edits, delegation->advance status write, closeout, merge-back status edit, and
    reconcile-before-closeout contract save; contract_core.go saveDelegationContract at
    materialize; eligible_accounting.go canonical writes; and saveDelegationBundle /
    saveDelegationBundleWithBase (resolve the "bundle key" open question — directory-scoped lock vs
    rely on per-bundle-id atomic write from p2). The 4 existing plan_task.go withTasksLock sites
    already conform via p0. Verified: a review/search gate asserts no RMW call site invokes a bare
    saveCanonical*/saveDelegationContract outside first-creation; touched-package tests pass.

- id: p4-concurrency-and-crossplatform-tests
  title: Prove no-lost-update, bounded-timeout error, atomicity, and cross-platform
  status: pending
  write_scope:
    - commands/workflow/plan_task_test.go
    - commands/workflow/delegation_test.go
  depends_on:
    - p1-rmw-helpers-at-chokepoint
    - p2-atomic-canonical-write
    - p3-migrate-unguarded-writers
  blocks: []
  verification_required: true
  notes: |-
    Extend the existing TestTasksYamlLock_* pattern: N concurrent writers mutating DIFFERENT slices
    of the SAME plan (distinct task notes + a plan-status edit through a DIFFERENT writer path, e.g.
    a delegation status write racing a task update) all survive — the lost-update is closed across
    writer paths, not just within task-update. Add: timeout-wraps-clear-error (lock held by another
    caller), atomic-write-leaves-prior-file-on-fault. Deterministic (barrier, no sleeps). Run under
    CI's exact flags (-race -count=1, lesson match-ci-test-flags-locally) and GOOS=windows go vet.
    Traces to spec done-criteria 1-3, 5.
```

## Dependency order

```
p0-generalize-store-lock ─┐
                          ├─→ p1-rmw-helpers-at-chokepoint ─→ p3-migrate-unguarded-writers ─┐
p2-atomic-canonical-write ┘                                                                  ├─→ p4-tests
                          └────────────────────────────────────────────────────────────────┘
```

- p0 and p2 are independent (lock vs durability) — author concurrently.
- p1 needs p0's scope-keyed guard.
- p3 needs p1's helpers.
- p4 needs p1 + p2 + p3 (asserts the whole contract end-to-end).
- Serial execution within one worktree is recommended anyway (all touch the
  `commands/workflow/` store hub — the same store-write-collision caution the git-ref plan
  applied), so run the chain in one worktree, not fanned out.

## Write-scope grounding

Impact radius (from the archived `git-ref-work-backend` briefing): the hub file
`commands/workflow/plan_task.go` = 125 direct / 175 impacted nodes across 9 files; the store
writers cluster in `plan_task.go`, `delegation.go`, `contract_core.go`, `eligible_accounting.go`.
The shared lock primitive lives in `internal/agentslock/`. Tests extend the existing
`commands/workflow/plan_task_test.go` lock suite plus `delegation_test.go`.
