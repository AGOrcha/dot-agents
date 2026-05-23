# Architecture note: root `commands` package decomposition

Status: finding / draft (not yet a plan contract)
Date: 2026-05-17
Origin: module-boundary review during the pr3a/b/c branch-split work.

## Finding

Post pr3a/b/c extraction the dependency graph is a clean acyclic DAG and
feature subpackages (`agents`, `hooks`, `skills`, `sync`, `workflow`,
`kg`) are properly isolated (none import each other). One boundary smell
remains:

**The root `commands` package has a dual role.** It is both the
composition root (wires all subcommands) *and* the implementation home
for the lifecycle command cluster — `add`, `doctor`, `import`, `init`,
`install`, `refresh`, `remove`, `status` plus `review`, `mcp`,
`settings`, `rules`, `ux`, `session_stats`. Because of this, `commands`
still imports `internal/links`, `internal/platform`,
`internal/projectsync`, `internal/scaffold/{home,hooks}` directly — it is
the only L1 package reaching that far down. 31 test files; mixed
responsibilities.

## Relationship to existing plans

- **`di-refactor-rollout` does NOT cover this.** That plan (6 tasks,
  `DI refactor: <package>`) replaces package-var seams with a `Deps`
  struct *within the current layout*. It explicitly preserves `commands`
  as one package. Orthogonal: it changes how root is tested, not what is
  in root.
- Therefore root decomposition is **unowned**.

## Recommended disposition

A separate future plan (`root-command-decomposition`): extract the
lifecycle/resource command cluster into a `commands/lifecycle` (or
similarly scoped) subpackage so root becomes a thin composition root and
the `internal/{links,platform,projectsync,scaffold}` imports move with
the cluster.

Sequencing: should land **after** `di-refactor-rollout` for the affected
packages, or the seam→DI migration churns the same files twice. Do not
fold into di-refactor-rollout — different scope and write-surface.

No code is proposed now. This note exists so the boundary is a tracked,
conscious deferral rather than invisible debt.
