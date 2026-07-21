# Command packages are auto-derived by globalflagcov

`internal/globalflagcov` statically analyzes every Cobra command's `RunE` or
`Run` handler to prove which global persistent flags it reads. The analyzer now
builds the same root command tree as the CLI, walks its reachable commands, maps
each handler's runtime program counter back to its defining Go package, and
loads that deduplicated package set.

## Rule

Do **not** manually register new command packages in `globalflagcov`. Wire the
command into `commands.NewRootCommand`; its handler package is then included
automatically. Packages without a reachable handler are intentionally excluded,
so the analyzer retains the old guard against pulling in experimental
`./commands/...` subpackages.

## Historical context

The analyzer formerly used an explicit `packages.Load` list. Missing a newly
wired package caused `TestReportNoUnresolvedHandlers` to report an "unresolved
closure" only in the integrated suite, as happened for observability o8 and was
patched manually in #467. This PR removes that maintenance rule by deriving the
package set from the live command tree.
