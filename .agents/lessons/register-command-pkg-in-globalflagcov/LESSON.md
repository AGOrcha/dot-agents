# Register a new command package in the globalflagcov analyzer

`internal/globalflagcov` statically analyzes every cobra command's `RunE` closure
to prove which global persistent flags it reads. It loads an **explicit,
hand-maintained list** of command packages (`loadCommandPackages` in
`internal/globalflagcov/static.go` — a literal `packages.Load(cfg, "./commands",
"./commands/agents", …)` call), deliberately NOT a `./commands/...` glob (the
comment says the glob would pull in not-yet-wired experimental subpackages).

## The failure

When you add a new command package and wire it into the root command tree
(`commands/root.go`), but do NOT add it to that explicit list, its `RunE` closures
resolve as **"unresolved closure …"** and `TestReportNoUnresolvedHandlers` fails:

```
observability status: unresolved closure observability.NewCmd.newStatusCmd.func1
observability sync: unresolved closure observability.newSyncCmd.func1
```

(The analyzer can't find the closure's `FuncLit` because the package isn't in its
loaded set — compiler inlining of small constructors makes the symbol names look
odd, e.g. `NewCmd.newStatusCmd.func1`, but the root cause is the missing package.)

## Rule

Adding a command package that lands in the root CLI = add `"./commands/<pkg>",` to
the `packages.Load` list in `internal/globalflagcov/static.go`, same commit.

## Why it bites in fan-out

This is a **cross-cutting guard outside any one command package's write_scope** — a
subagent scoped to `commands/observability/` cannot see it, and its focused
`go test ./commands/observability/...` passes clean. It only fails under
`go test ./...`. The orchestrator (or whoever runs the full suite at integration)
catches it, never the slice worker. Budget a full-suite run at plan integration for
exactly this class of guard. Grounded in obs-dashboard-cf-deploy o8 (fixed in #467);
sibling of [[lint-check-count-assertion]] (a guard test that needs a manual update
when you add a surface).

## Durable fix (follow-up)

The analyzer could derive its package set from the actual root command tree
(walk `root.Commands()` transitively) instead of a hand-maintained literal list,
which would make this class of miss impossible. Tracked as a proposal
(`~/.agents/proposals/globalflagcov-derive-command-packages.md`).
