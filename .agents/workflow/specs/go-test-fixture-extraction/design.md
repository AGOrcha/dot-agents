# Spec — go-test-fixture-extraction

## Problem

PR#10's SonarCloud quality gate fails on `new_duplicated_lines_density` at
4.7% (target ≤3%). Per the `sonarqube-pr10` plan's findings.md and
ADR-0008 (option B), the dominant cluster of duplication is **Cluster D**:
~1,500–1,700 duplicated lines across 18+ `*_test.go` files. The bulk is
repeated test scaffolding — temp-project setup, `.agentsrc.json` fixture
writers, manifest fixture writers, table-test boilerplate — copied
package-by-package because Go test helpers are not exported across
packages by default and per-package `setupTempProject` is the path of
least resistance for any new test.

The deferred work is exactly this extraction: produce one canonical
`internal/testutil` package, refactor every `*_test.go` to consume it, and
flip the duplication density gate.

## Goals

1. Drive `new_duplicated_lines_density` below the 3% threshold on a
   subsequent PR (this plan does NOT stack on PR#10 — see "Constraints").
2. Establish `internal/testutil` (or `internal/testfixtures`) as the
   canonical source for cross-package test scaffolding so future tests
   reuse instead of re-copy.
3. Net-remove ~1,500 lines from `*_test.go` files; net-add ~250 lines
   in the new helper package.

## Non-goals

- Reducing duplication in production code (handled by
  `production-code-helper-extraction`).
- Behavioral test changes. Tests must still cover the same scenarios with
  the same assertions; only the scaffolding changes.
- Adding new test cases. If a per-package extraction reveals a missing
  case, file a follow-up — do not bundle it.

## Decisions

- **Package name:** `internal/testutil`. Matches Go-stdlib precedent and
  is short. Alternatives considered: `internal/testfixtures` (clearer
  intent, longer); `pkg/testing` (rejected — not a public API).
- **Helper signatures take `*testing.T` as the first arg** and call
  `t.Helper()` + `t.Fatal` on errors, matching the existing style in
  `commands/agents/agents_test.go::setupAgentsEnv`.
- **No `t.TempDir()` wrapper.** Each helper accepts a path. Callers
  control the `t.TempDir()` call. Wrapping `TempDir()` couples helper
  semantics to test lifecycle and complicates nested fixtures.
- **Per-bucket helpers wrap, not replace, the generic ones.** Pattern:
  - `testutil.NewTempProject(t, projectName)` returns
    `(agentsHome, projectPath string)` — generic.
  - `testutil.WriteAgentManifest(t, projectPath, name)` and
    `testutil.WriteSkillManifest(t, projectPath, name)` — bucket-specific
    wrappers around a `WriteManifest(t, dir, manifest, name)` core. Keeps
    call sites readable; doesn't push every test to learn the bucket spec.
- **Scope is whatever Sonar reports as duplicated.** Do not preemptively
  refactor non-duplicated test scaffolding "while we're here." Cluster D
  is large enough.

## Open questions

- Do `internal/graphstore/{sqlite,postgres}_test.go` share enough fixture
  shape with the broader test corpus to use the same helpers, or do they
  need their own `internal/graphstore/internal/storetestutil` (since they
  build a real DB)? Resolve in T1 audit task.
- Do `internal/platform/resource_plan_test.go` (239 dup lines, the
  largest single offender) and `commands/refresh_test.go` (114 dup lines,
  32.1% density) duplicate each other or just internally? If
  cross-package, they consume the helper directly; if internal,
  table-driven tests may be a better fix than helper extraction. Resolve
  in T1 audit task.

## Done criteria

- `go test ./...` passes on the branch tip with zero behavioral changes.
- `mcp__sonarqube__get_project_quality_gate_status` for the next PR
  reports `new_duplicated_lines_density` below 3%, OR the metric is
  reduced by at least 60% (gate may still fail if production-code
  cluster moves the needle the wrong way concurrently — that is a
  cross-plan concern, not a local one).
- `internal/testutil/` exists with package-level docs, helper docs, and
  per-helper unit tests where helper logic is non-trivial.
- Every file in the Cluster D inventory has been refactored to use the
  helper, with the diff confined to test files (no production-code
  changes in this plan).

## Constraints

- **Must NOT stack on PR#10.** PR#10 is already 895 changed files; adding
  18+ test-file refactors to the same review is a merge-risk play with
  no upside (option B in ADR-0008 explicitly defers this work to a
  separate PR).
- **One test-file refactor per commit** (or per task), so reviewer can
  bisect and revert any single extraction without reverting the whole
  effort.
- **Per-task verification is `go test ./<package>` only.** No SonarCloud
  poll inside individual tasks; the gate is verified once at plan
  closeout.

## Cluster D inventory (live counts from PR#10 analysis, 2026-05-04)

| File | Dup lines | Blocks | Density |
|---|---|---|---|
| internal/platform/resource_plan_test.go | 239 | 15 | 26.8% |
| commands/workflow/state_plan_test.go | 184 | 11 | 18.3% |
| commands/agents/agents_test.go | 119 | 7 | 15.7% |
| commands/refresh_test.go | 114 | 4 | 32.1% |
| commands/workflow/testutil_test.go | 110 | 6 | 14.1% |
| commands/skills/promote_test.go | 108 | 6 | 28.3% |
| commands/kg/kg_test.go | 91 | 11 | 3.8% |
| commands/workflow/workflow_integration_test.go | 84 | 5 | 11.6% |
| commands/workflow/delegation_fanout_test.go | 74 | 4 | 8.1% |
| commands/workflow/iter_log_test.go | 61 | 4 | 9.9% |
| internal/graphstore/postgres_test.go | 57 | 3 | 10.2% |
| internal/graphstore/sqlite_test.go | 57 | 3 | 7.8% |
| commands/workflow/foldback_test.go | 49 | 3 | 12.9% |
| commands/import_test.go | 48 | 2 | 5.0% |
| commands/mcp_settings_test.go | 46 | 4 | 44.2% |
| internal/graphstore/mcp_server_test.go | 42 | 2 | 11.6% |
| commands/workflow/plan_task_test.go | 26 | 2 | 2.2% |
| commands/rules_test.go | 23 | 2 | 22.1% |
| commands/workflow/drift_sweep_test.go | 22 | 2 | 6.2% |
| internal/platform/rules_test.go | 13 | 1 | 14.0% |
| internal/platform/mcp_settings_test.go | 13 | 1 | 15.7% |

Counts drift on every push to PR head; re-snapshot the live duplication
inventory at plan kickoff before sequencing tasks.
