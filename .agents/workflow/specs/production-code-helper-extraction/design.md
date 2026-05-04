# Spec — production-code-helper-extraction

## Problem

PR#10's SonarCloud quality gate fails on `new_duplicated_lines_density`
at 4.7% (target ≤3%). Per ADR-0008 (option B), the test-fixture cluster
(D) is the load-bearing one and gets its own plan
(`go-test-fixture-extraction`). The remaining production-code cluster —
**Cluster E.other**, ~400–500 duplicated lines across roughly a dozen
files in `internal/graphstore/`, `internal/platform/`, `commands/`, and
`ports/typescript/src/commands/` — is what this plan addresses.

The in-PR cherries (Cluster F: `commands/{agents,skills}/list.go`; and
E.promote: `commands/{agents,skills}/promote.go`) were taken as part of
PR#10 via `internal/projectsync.{ListBucket, PromoteResource, CopyTree,
ReadFrontmatterDescription}`. Cluster E.other is what remains.

## Goals

1. Drive the production-code share of duplicated lines below the level
   where, combined with `go-test-fixture-extraction`, the PR-level
   density gate stays under 3% on future PRs.
2. Where extraction is viable, prefer a **`projectsync`-shaped helper**
   (BucketSpec-style parameterization) over copy-pasted bodies, so the
   pattern stays consistent with the agents/skills work in PR#10.
3. Where extraction is not viable (one-off coincidence rather than
   structural shape duplication), accept the duplication and tag it
   in the design memo with reasoning so a future audit doesn't
   re-litigate.

## Non-goals

- Behavior changes. Production-code refactor only; same wire format,
  same outputs, same side effects.
- Touching tests beyond what's needed to keep them green. The test
  cluster has its own plan.
- Adding new abstractions to support future not-yet-needed callers.
  Extract only what is duplicated today; YAGNI applies.

## Decisions

- **One pair-group per task.** Each "group" is a set of files Sonar
  reports as cross-duplicated (e.g. sqlite.go ↔ postgres.go is one
  group; settings.go ↔ mcp.go ↔ rules.go is another). Tasks do not
  span groups — that keeps blast radius small and makes per-task
  reverts safe.
- **Helpers live in the smallest enclosing package.** A graphstore
  helper goes in `internal/graphstore/` (or `internal/graphstore/internal/`
  if it shouldn't escape); a CLI helper used only by `commands/*.go`
  goes in `commands/internal/cmdutil/` rather than `internal/`.
- **`projectsync` is for resource-bucket abstractions only.** Don't
  bloat it with generic helpers (e.g. SQL builders, TS string utilities)
  just because it already exists. Each helper finds its right home.
- **Each task self-verifies via `go test ./<package>/...`.** No
  cross-task SonarCloud poll until plan closeout.

## Open questions (resolve in T1 audit)

- `internal/graphstore/sqlite.go` (line 475 ~80 lines) ↔
  `internal/graphstore/postgres.go` (line 589 ~80 lines): is this
  query-loop or row-scan duplication? If row-scan, a generic
  `scanGraphRows` helper collapses both. If it includes
  driver-specific syntax, the helper has to carry a SQL-dialect
  parameter and may not be a net win.
- `internal/platform/resource_plan.go` self-duplication (three blocks
  at lines 284, 337, 461 — 18-26 lines each): is it the
  `listScopedResourceDirs` + intent-construction pattern? If yes, a
  generic `intentsFromBucket(bucket, manifestName)` helper kills all
  three. Likely the cleanest single win in this plan.
- `commands/{settings,mcp,rules}.go` three-way duplication (19-24
  line blocks): the cobra subcommand registration + write boilerplate.
  Worth extracting only if all three pass the same kind of
  `dot-agents <X> <op>` shape; if subtly different, accept the dup.
- TS port (`ports/typescript/src/commands/{agents,skills}.ts`,
  20-line block): is this the same listResource pattern as the Go
  port? If yes, mirror the Go extraction in
  `ports/typescript/src/lib/projectsync.ts`. Coordinate with the
  `typescript-port` plan owner so the extraction doesn't get reverted
  by parallel port churn.

## Scope (live counts from PR#10 analysis, 2026-05-04)

### Group GS — graphstore SQL backends

- `internal/graphstore/sqlite.go` (116 dup lines, 4 blocks, 14.3%)
- `internal/graphstore/postgres.go` (94 dup lines, 2 blocks, 10.4%)
- `internal/graphstore/mcp_server.go` (87 dup lines, 4 blocks, 12.6%)

Cross-pair duplication (sqlite ↔ postgres) is the dominant source;
mcp_server.go has its own (likely tool-dispatch / handler shape).

### Group RP — resource_plan self-duplication

- `internal/platform/resource_plan.go` (70 dup lines, 5 blocks, 10.9%)

Three near-identical bucket-listing blocks. Tightest single win.

### Group CMDS — commands/* CLI families

- `commands/import_plugins.go` (50 dup lines, 3 blocks, 7.6%)
- `commands/settings.go` (55 dup lines, 3 blocks, 29.3%)
- `commands/mcp.go` (55 dup lines, 3 blocks, 29.3%)
- `commands/rules.go` (43 dup lines, 2 blocks, 17.9%)
- `commands/explain.go` (23 dup lines, 2 blocks, 9.1%)
- `commands/status.go` (50 dup lines, 4 blocks, 4.2%)
- `commands/ux.go` (25 dup lines, 2 blocks, 8.7%)
- `commands/skills.go` (residual after PR#10 cleanup; re-snap at T1)

settings/mcp/rules form a clear three-way cross-duplication. The
others are likely independent or self-duplicated; T1 audit
classifies.

### Group MCP — internal/platform mcp_settings

- `internal/platform/mcp_settings.go` (26 dup lines, 2 blocks, 14.0%)
- `internal/platform/plugins.go` (22 dup lines, 1 block, 10.9%)

May share shape with the commands/mcp.go group. T1 audit checks.

### Group TS — TypeScript port mirrors

- `ports/typescript/src/commands/agents.ts` (20 dup lines, 1 block, 17.1%)
- `ports/typescript/src/commands/skills.ts` (20 dup lines, 1 block, 16.9%)

Mirrors the Go agents/skills duplication that PR#10 fixed. Direct
analog: extract a `lib/projectsync.ts`.

## Done criteria

- All groups in scope have either (a) a shared helper landed and the
  duplicated bodies removed, or (b) an explicit "do not extract"
  classification in `design.md` with reasoning.
- `go test ./...` passes on the branch tip. Behavioral parity
  confirmed by every existing test still passing without modification
  beyond import-path changes.
- The eventual follow-up PR's SonarCloud snapshot reports
  Cluster E.other contribution reduced by ≥ 60% OR
  `new_duplicated_lines_density` below 3% (whichever first). The
  latter only achievable if `go-test-fixture-extraction` lands first
  or concurrently — see "Constraints".

## Constraints

- **Must NOT stack on PR#10.** Same reasoning as `go-test-fixture-extraction`:
  PR#10 is already 895 changed files.
- **Plan-level gate verification depends on cluster-D state.**
  Production-code dedup alone removes ~400-500 lines (≈ 0.7-0.9 pp);
  not enough to flip the gate. The math only works once Cluster D
  also lands. Order the merges accordingly: this plan's PR can ship
  before, after, or in parallel with `go-test-fixture-extraction`,
  but the gate-flip moment is whichever PR is second.
- **One group per task; one commit per file within a task** so any
  single extraction is bisectable and revertable independently.
