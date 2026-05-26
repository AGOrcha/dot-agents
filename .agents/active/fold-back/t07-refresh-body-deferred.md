# Fold-back: t07 refresh body cannot move atomically in parallel with t03/t04/t06

**Source task**: `root-command-decomposition / t07-move-refresh`
**Observed**: 2026-05-25
**Severity**: medium (planning constraint not anticipated in SHAPE.md)

## Observation

The t07 move (refresh.go → commands/lifecycle/refresh.go) cannot land the
`runRefresh` body inside `commands/lifecycle/` while t03 (install), t04
(add/remove), and t06 (import) are still in-flight in parallel worktrees.

`runRefresh` and its private helpers (`restoreFromResources`, `mapResourceRelToDest`)
reach into symbols that live in sibling root-cluster files which have not yet
moved:

- `addDeps`, `stdAddDeps`, `restoreFromResourcesCountedWithDeps`,
  `restoreLegacyResourceFile` (defined in `commands/add.go` — owned by t04)
- `importDeps`, `stdImportDeps`, `runImportFromRefresh`,
  `importScopeAll`/`importScopeProject`, `agentsHooksPrefix`, and the
  full `rel*Dir` / `rel*JSON` constant set (defined in `commands/import.go`
  — owned by t06)

Lifecycle is a sibling subpackage under `commands/`; it cannot
`import "commands"` because `commands/root.go` already imports `lifecycle`.
The full body move requires either:

1. **Cluster co-move** — t03/t04/t06/t07 land together so the addDeps /
   importDeps types and constants move into lifecycle simultaneously. The
   plan explicitly chose per-task PRs ("one PR per task = one PR per command
   move" — SHAPE.md §7) which forbids this.
2. **Massive cross-cluster glue in `lifecycle.Deps`** — every helper refresh
   reaches into becomes a Deps function pointer (`RestoreFromResources`,
   `RunImportFromRefresh`, every `rel*Dir` constant, every `import*` scope
   string, the addDeps interface…). This is a large per-PR diff and inverts
   ownership — t07 has to know all of t04's and t06's internals.
3. **Duplicate the helpers** in lifecycle for the t07 window then delete on
   t11/t13 — duplication with drift risk.

## Resolution chosen (this PR)

Apply the `commands/sync.go` shim pattern already in the repo:

- `commands/lifecycle/refresh.go` houses `NewRefreshCmd(deps Deps)` — cobra
  metadata + `--import` flag wiring + RunE that delegates to
  `deps.RunRefresh(filter, importAlso)`.
- `lifecycle.Deps` gains one new field: `RunRefresh func(projectFilter string, importAlso bool) error`.
- `commands/refresh.go` retains the legacy `runRefresh` body, its package-var
  seams (`stdRefreshConfigLoader` etc.), the `refreshImport` flag mirror,
  and `mapResourceRelToDest` + `restoreFromResources` helpers. Its
  `NewRefreshCmd()` shim constructs `lifecycle.Deps` with a `RunRefresh`
  closure and forwards to `lifecycle.NewRefreshCmd`.
- Existing `commands/refresh_test.go` and `commands/refresh_idempotency_test.go`
  stay in root because they exercise `runRefresh` directly and depend on
  `stdAddDeps` / `stdImportDeps` / `fakeAddDeps` / `seedAllPlatformInstallSignals`.
- A small `commands/lifecycle/refresh_test.go` covers the lifecycle
  constructor (cobra metadata, flag presence, RunE → deps dispatch).
  This satisfies the bundle write_scope.

This matches the SHAPE.md §6 invariant ("root file remains in place wiring
the constructor; t13 deletes the shim and switches root.go to lifecycle
directly") and preserves the package-var seams per §5/§6.

## Follow-up

The full body move (runRefresh + helpers → lifecycle/refresh.go private)
becomes viable after t04 and t06 merge. Re-home the body and the existing
root tests as part of t11 or a new follow-up task (`t07b-finish-refresh-body-move`).
SHAPE.md §4a line 243 currently expects refresh_test.go +
refresh_idempotency_test.go to land in lifecycle in t07. Update the table
to mark them as t07b (or t11) instead.

## Action

- File this observation. Do not block the t07 PR on it.
- When t04 and t06 merge, open the follow-up task; this fold-back becomes
  its starting context.
