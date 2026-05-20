# Spec — seam-interface-di-migration

## Problem

Test fault-injection in this codebase is overwhelmingly done with
**package-level function-variable seams**: `var osX = os.X` (and
`json.*`, `yaml.*`, `config.*`, `runRefresh`, …) declared in a
per-package `seams.go`, swapped to a stub in `*_test.go` via a
`withXStub(t, ...)` helper that save/restores with `t.Cleanup`.

This pattern is pervasive — ~36 seams across 6 `seams.go` files
(`commands/`, `commands/agents/`, `commands/workflow/`, `commands/kg/`,
`commands/skills/`, `internal/platform/`) — and it keeps growing
(PR#35 extends `commands/workflow/seams.go`; PR#36 nearly added an
`osReadDir` func-var before being converted).

The maintainer has ruled (2026-05-19) that the func-var seam pattern is
an **early implementation detail that was not fully caught**, not the
target design. The **preferred seam shape is interface-based dependency
injection (interface-DI)**: a narrow collaborator interface, a real
implementation in production, and a fake injected via constructor or
parameter in tests. Prevalence of the func-var pattern is not
endorsement (see lesson `prefer-interface-di-over-funcvar-seams`).

Without a tracked migration, every new error-branch test re-applies the
legacy pattern (the `prefer-test-seam-over-untestable` policy says "add
a seam, don't allowlist as untestable" — correct, but the seam shape it
implies must now be interface-DI).

## Goals

1. Convert existing func-var seams to interface-DI, package by package,
   behavior-preserving, tests staying green throughout.
2. Stop net-new func-var seams: new fault-injection points use
   interface-DI from the start.
3. Establish and document the canonical interface-DI seam shape so
   future tests and reviews have an unambiguous target.
4. Fold PR#35 into this migration as its first concrete target (it is
   **held for rework** — not merged on the legacy pattern).

## Non-goals

- Behavior changes. Pure refactor: same wire format, outputs, side
  effects. No new abstractions for not-yet-needed callers (YAGNI).
- Big-bang single-commit conversion. Risk and review surface make an
  incremental, per-package sequence mandatory.
- Removing the `prefer-test-seam-over-untestable` policy. It still
  holds; only the seam *shape* changes.
- Touching `internal/graphstore` seam style as part of this (it already
  has 0 func-var seams / uses collaborator injection — it is the
  reference, not a target).

## Decisions

1. **Canonical shape.** A narrow interface named for the collaborator
   (not "Seam"), a production struct implementation backed by stdlib,
   injected via constructor or explicit parameter. Reference
   implementations: `internal/graphstore` `NewHandle(store Store)`
   (collaborator) and the `commands.dirCleaner` / `osDirCleaner`
   pair introduced in PR#36's `remove.go` (`ReadDir`/`RemoveAll`
   injected into `emptyProjectDirs`).
2. **Injection site.** Prefer constructor/struct-field injection. Where
   a command is a free function invoked by Cobra (`runRemove` etc.),
   restructuring it for injectability is part of the per-package task —
   do NOT substitute a swappable package-level interface var (that is
   just a func-var seam wearing an interface).
3. **Incremental, per package.** One task per `seams.go` package; each
   converts that package's seams + the `withXStub` test helpers + call
   sites in one reviewable unit, tests green per task.
4. **PR#35 is held for rework** and becomes this plan's first target —
   its `commands/workflow` seams convert to interface-DI before merge.
   Its non-seam value (schema-compile dedupe, retiring
   `[defensive-unreachable]` coverage exceptions) is preserved.
5. **remove.go is partially converted already.** PR#36 converted
   `emptyProjectDirs` to interface-DI but left the pre-existing
   `removeProjectDirs` on the `osRemoveAll` func-var (accepted in-file
   inconsistency). Finishing `commands/seams.go` includes completing
   `remove.go`.

## Requirements

- Each converted package: zero `var xFn = realFn` seam declarations
  remain; `seams.go` either deleted or reduced to interface + prod impl.
- `withXStub` helpers replaced by injected fakes; no global
  save/restore-with-Cleanup mutation of package state.
- Coverage gate (cg6b per-file) holds or improves; error branches stay
  covered via injected fakes, not allowlist regressions.
- `go test ./...` green after each task; no behavior diff.
- A short convention doc (where the interface-DI shape is specified) is
  referenced from the lesson and from `prefer-test-seam-over-untestable`.

## Open questions (plan must resolve)

1. Conversion order across the 6 packages — likely
   `commands/workflow` first (unblocks #35), then `commands/` (largest,
   12 seams, includes remove.go finish), then the smaller leaves.
2. For Cobra free-function commands, the concrete injectable shape
   (struct with method + injected collaborator vs threading a param):
   pick one and apply consistently; resolve the `runRemove` non-clean
   failure coverage gap noted in PR#36 here.
3. Coexistence period: is a half-migrated package acceptable between
   tasks, or must each `seams.go` flip atomically? (Leaning atomic
   per package to avoid two patterns in one file.)

## Done criteria

- All 6 `seams.go` packages use interface-DI; no func-var seams remain.
- PR#35 reworked to interface-DI and merge-ready on the new pattern.
- Convention documented and cross-linked from the lesson + memory.
- `go test ./...` green; coverage gate not regressed.

## Deferred

- `internal/graphstore` (already conformant).
- Any production restructuring beyond what injectability requires.

## Relationships

- Lesson: `.agents/lessons/prefer-interface-di-over-funcvar-seams/`.
- Supersedes the func-var implication of memory
  `prefer-test-seam-over-untestable` (policy kept, shape changed).
- PR#35 (`cg6b-b2-workflow-schema`) — held for rework, first target.
- PR#36 (`hooks-cli-showremove`) — `remove.go` partial conversion is
  the reference example; finishing it is in scope.
- Independent of `pr10-branch-split` (branch-split closeout is separate;
  this is a cross-cutting refactor with its own lifecycle).
