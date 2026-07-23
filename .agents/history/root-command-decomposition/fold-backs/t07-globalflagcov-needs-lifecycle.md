# Fold-back: t07 requires `internal/globalflagcov` to load `commands/lifecycle`

**Source task**: `root-command-decomposition / t07-move-refresh`
**Observed**: 2026-05-25
**Severity**: low (one-line fix; scope expansion)

## Observation

`internal/globalflagcov/static.go::loadCommandPackages` ships an explicit
allowlist of command packages to type-check. After t07 moves the `refresh`
cobra constructor into `commands/lifecycle/`, the RunE closure
`lifecycle.NewRefreshCmd.func1` is captured by the runtime PC walker but
cannot be resolved because the lifecycle package was never indexed.

This trips `TestReportNoUnresolvedHandlers` with:

    refresh: unresolved closure lifecycle.NewRefreshCmd.func1

## Resolution

Add `"./commands/lifecycle"` to the `packages.Load(...)` call in
`internal/globalflagcov/static.go`. One line, same shape as the existing
agents / sync / hooks / skills / kg / workflow entries.

## Scope note

`internal/globalflagcov/static.go` is outside the t07 bundle write_scope
(`commands/lifecycle/refresh*.go`, `commands/refresh*.go`). The change is
included in the t07 PR anyway because:

1. The build failure is caused by t07's move and would block the PR.
2. The fix is mechanical and matches the existing per-package allowlist
   pattern — no design decision.
3. The alternative (leave the build red) is worse than a one-line scope
   expansion documented here.

Future lifecycle moves (t03 install, t04 add/remove, t05 init, t06 import,
t08 status, t09 doctor) will land RunE closures in the lifecycle package
too. They benefit from the same fix — t07 lands it once for the cluster.

## Follow-up

When the t14 importguard task is implemented, it should also assert that
`internal/globalflagcov/static.go`'s explicit package list stays in sync
with the actual subpackages under `commands/`. Otherwise a new subpackage
landing in a future plan will silently lose closure coverage.
