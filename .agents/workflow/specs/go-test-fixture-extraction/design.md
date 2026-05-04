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
- **Self-duplicated files get table-driven refactor, not helper
  extraction.** The T1 audit revealed that ~70% of the Cluster D dup
  lines are *intra-file* repetition — the same test body shape repeated
  across 2–11 sibling cases in one file (e.g. resource_plan_test's
  `validShared{Skill,Agent,Plugin}Intent` round-trips, kg_test's
  search/index permutations). Those collapse cleanly to table-driven
  tests inside the file; an `internal/testutil` helper would not reduce
  Sonar duplication for them. The bucket-aware helpers below cover only
  the genuinely cross-package shape (project setup + manifest writers +
  scope-tree fixtures + git repo init).
- **Graphstore tests get a sub-helper, not the canonical helper.**
  `internal/graphstore/{sqlite,postgres}_test.go` cross-duplicate at
  test-body shape (UpsertNode/UpsertEdge round-trips against a real
  store handle). They cannot use `internal/testutil` because the
  duplication is in body, not setup. Resolution: introduce
  `internal/graphstore/internal/storetest` with a `RunStoreSuite(t,
  func(t) graphstore.Store)` style runner that both files invoke with
  their own opener. mcp_server_test.go's duplication is self-duplicated
  rpc-call shape — table-driven inside the file.

## Open questions

(none — resolved by T1 audit)

## Canonical helper signatures

The following helpers (and only these) constitute `internal/testutil`'s
exported API for this plan. Each lives in `internal/testutil/<file>.go`
with go-doc on every export. All signatures take `*testing.T` first,
call `t.Helper()`, and `t.Fatal` on prerequisite errors (no returned
errors — callers always want immediate test failure on fixture setup).

| Helper | Signature | Purpose | Replaces |
|---|---|---|---|
| `NewTempProject` | `func NewTempProject(t *testing.T, projectName string) (agentsHome, projectPath string)` | Create `tmp/agents/` + `tmp/repo/` under `t.TempDir()`, set `AGENTS_HOME`, write `.agentsrc.json` with `Version=1`, `Project=projectName`, `Sources=[{Type:"local"}]`. Returns the two paths. | `agents/agents_test.go::setupAgentsEnv`; `skills/promote_test.go::setupSkillsEnv` |
| `WriteAgentManifest` | `func WriteAgentManifest(t *testing.T, projectPath, agentName string)` | Mkdir `<projectPath>/.agents/agents/<agentName>/` and write `AGENT.md` with frontmatter (`name`, `description: test agent`) + body. Bucket-aware wrapper. | `agents/agents_test.go::writeAgentMD` |
| `WriteSkillManifest` | `func WriteSkillManifest(t *testing.T, projectPath, skillName string)` | Same as `WriteAgentManifest` but writes `<projectPath>/.agents/skills/<skillName>/SKILL.md`. | `skills/promote_test.go::writeSkillMD` |
| `WriteCanonicalAgent` | `func WriteCanonicalAgent(t *testing.T, agentsHome, projectName, agentName string) string` | Mkdir `<agentsHome>/agents/<projectName>/<agentName>/` and write canonical `AGENT.md`. Returns the directory path. | `agents/agents_test.go::writeCanonicalAgent` |
| `WriteCanonicalSkill` | `func WriteCanonicalSkill(t *testing.T, agentsHome, projectName, skillName string) string` | Symmetric: canonical SKILL.md under `<agentsHome>/skills/<projectName>/<skillName>/`. Returns the directory path. | (new — needed for skills symmetry, not currently a per-file helper) |
| `WriteScopeFile` | `func WriteScopeFile(t *testing.T, agentsHome, bucket, scope, baseName string, content []byte)` | Mkdir `<agentsHome>/<bucket>/<scope>/` and write `<baseName>` with `content`. Used by mcp/settings/rules tests where the fixture is just "drop a file under the scope tree." | `commands/mcp_settings_test.go` inline blocks; `commands/rules_test.go` inline blocks; `internal/platform/{mcp_settings,rules}_test.go` inline blocks |
| `InitGitRepo` | `func InitGitRepo(t *testing.T, repoPath string, files map[string]string)` | Run `git init` + set test author/committer env, write the supplied path→content map (creating parent dirs), `git add .`, `git commit -m "init"`. No return. | `commands/workflow/testutil_test.go::initWorkflowTestRepo`; `commands/scaffold_hooks_test.go::initShellHookTestRepo`; the inline git block in `workflow_integration_test.go::TestWorkflow_EmptyStateGraceful` |
| `WriteAgentsRC` | `func WriteAgentsRC(t *testing.T, projectPath string, rc *config.AgentsRC)` | Convenience wrapper around `rc.Save(projectPath)` that `t.Fatal`s on error. Defaults to `Version=1, Sources=[{Type:"local"}]` when `rc` is nil. | The repeated 6-line `rc := &config.AgentsRC{...}; rc.Save(projectPath)` pattern in agents/skills setups and the inline `.agentsrc.json` literal in `workflow_integration_test.go` line 638-640 and `scaffold_hooks_test.go` line 58 |

Notes on what is **deliberately not** in this table:

- `setupTempProject` / `setupTestProject` / `setupFanout*Project` / `setupVerifierDispatchProject` (all in `commands/workflow/testutil_test.go`) — these stay as `workflow`-package helpers. Their cross-file overlap is intra-package (within `commands/workflow/`), so Go's normal package-private helper sharing handles it without an exported testutil dependency. T4's job is to keep them as one canonical local helper, not export them.
- `validShared{Skill,Agent,Plugin}Intent` (in `resource_plan_test.go`) — these are package-private type constructors for `platform.ResourceIntent` literals; lifting them to `internal/testutil` would create a cycle (`testutil` → `platform`). T3 handles `resource_plan_test.go`'s 239 dup lines via table-driven test rewriting, NOT by extracting these constructors.
- `openTestStore` / `openPGTestStore` — handled by the graphstore-internal `storetest` sub-helper (decisions section), not by `internal/testutil`.
- `captureStdout`, `captureStderr` — present in `kg_test.go` and `delegation_fanout_test.go` but the duplications report does not flag them; leave alone.

### Graphstore sub-helper (separate from internal/testutil)

`internal/graphstore/internal/storetest/storetest.go`:

| Helper | Signature | Purpose |
|---|---|---|
| `RunNodeRoundTripSuite` | `func RunNodeRoundTripSuite(t *testing.T, openStore func(*testing.T) graphstore.Store)` | Runs the shared UpsertNode/GetNode/GetNodesByFile assertion sequence (the 21-line block at sqlite_test.go:135 / postgres_test.go:134) against a backend opener. |
| `RunEdgeRoundTripSuite` | `func RunEdgeRoundTripSuite(t *testing.T, openStore func(*testing.T) graphstore.Store)` | Runs the shared UpsertEdge/GetEdges/GetEdgesByFile assertion sequence (the 24-line block at sqlite_test.go:505 / postgres_test.go:369). |
| `RunStatsSuite` | `func RunStatsSuite(t *testing.T, openStore func(*testing.T) graphstore.Store)` | Runs the GetStats round-trip (the 12-line block at sqlite_test.go:559 / postgres_test.go:423). |

The `graphstore.Store` interface already exists in the package, so the
sub-helper imports `graphstore` cleanly. The sub-helper lives under
`internal/graphstore/internal/` to ensure it is not consumed outside
graphstore.

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

## Cluster D inventory (live counts from PR#10 analysis, 2026-05-04, re-snapped during T1 audit)

Re-snapped via `mcp__sonarqube__search_duplicated_files` and per-file
`mcp__sonarqube__get_duplications` against project
`NikashPrakash_dot-agents`, pullRequest `10`. Counts are **identical to
the 2026-05-04 baseline** — no test file has dropped out, no new test
file has joined, no per-file count has changed. Total 21 files, 1,420
test dup lines (out of 2,541 cluster total — the rest is production
code in cluster E).

The "Shape" column is the T1 audit classification that determines which
extraction strategy a given file gets in T3–T7:

- **Cross-pkg helper** — duplication crosses package boundaries; consumes `internal/testutil`.
- **Self / table-drive** — duplication is intra-file repetition; refactor in place to table-driven tests, no helper needed.
- **Mixed** — file has both kinds of blocks; both refactors apply.
- **Graphstore sub-helper** — handled by `internal/graphstore/internal/storetest`, not the canonical helper.

| File | Dup lines | Blocks | Density | Shape | Handler task |
|---|---|---|---|---|---|
| internal/platform/resource_plan_test.go | 239 | 15 | 26.8% | Self / table-drive | T3 |
| commands/workflow/state_plan_test.go | 184 | 11 | 18.3% | Self / table-drive | T4 |
| commands/agents/agents_test.go | 119 | 7 | 15.7% | Cross-pkg helper (↔ skills/promote_test.go) | T7 |
| commands/refresh_test.go | 114 | 4 | 32.1% | Self / table-drive | T5 |
| commands/workflow/testutil_test.go | 110 | 6 | 14.1% | Mixed (cross-pkg `InitGitRepo` + intra-pkg `setupFanout*`) | T4 |
| commands/skills/promote_test.go | 108 | 6 | 28.3% | Cross-pkg helper (↔ agents/agents_test.go) | T7 |
| commands/kg/kg_test.go | 91 | 11 | 3.8% | Self / table-drive | T7 |
| commands/workflow/workflow_integration_test.go | 84 | 5 | 11.6% | Mixed (cross-pkg `InitGitRepo` block + self-dup blocks) | T4 |
| commands/workflow/delegation_fanout_test.go | 74 | 4 | 8.1% | Self / table-drive | T4 |
| commands/workflow/iter_log_test.go | 61 | 4 | 9.9% | Self / table-drive | T4 |
| internal/graphstore/postgres_test.go | 57 | 3 | 10.2% | Graphstore sub-helper (↔ sqlite_test.go) | T6 |
| internal/graphstore/sqlite_test.go | 57 | 3 | 7.8% | Graphstore sub-helper (↔ postgres_test.go) | T6 |
| commands/workflow/foldback_test.go | 49 | 3 | 12.9% | Self / table-drive | T4 |
| commands/import_test.go | 48 | 2 | 5.0% | Self / table-drive | T5 |
| commands/mcp_settings_test.go | 46 | 4 | 44.2% | Cross-pkg helper (↔ rules_test.go via `WriteScopeFile`) | T5 |
| internal/graphstore/mcp_server_test.go | 42 | 2 | 11.6% | Self / table-drive | T6 |
| commands/workflow/plan_task_test.go | 26 | 2 | 2.2% | Self / table-drive | T4 |
| commands/rules_test.go | 23 | 2 | 22.1% | Cross-pkg helper (↔ commands/mcp_settings_test.go via `WriteScopeFile`) | T5 |
| commands/workflow/drift_sweep_test.go | 22 | 2 | 6.2% | Self / table-drive | T4 |
| internal/platform/rules_test.go | 13 | 1 | 14.0% | Cross-pkg helper (↔ internal/platform/mcp_settings_test.go via `WriteScopeFile`) | T3 |
| internal/platform/mcp_settings_test.go | 13 | 1 | 15.7% | Cross-pkg helper (↔ internal/platform/rules_test.go via `WriteScopeFile`) | T3 |

### Cross-package overlap discovered outside the original inventory

`commands/scaffold_hooks_test.go::initShellHookTestRepo` (16 lines)
duplicates `commands/workflow/testutil_test.go::initWorkflowTestRepo`
(reported by Sonar as a cross-block in the testutil_test.go duplication
list — the source file `scaffold_hooks_test.go` doesn't appear in the
top-level dup list because its own density is below the report
threshold, but the block participates in the cluster). T5 must update
`scaffold_hooks_test.go` alongside the `commands/`-root files so the
canonical `InitGitRepo` helper drains the duplicate from both sides.

### Shape distribution

- **Cross-package helper consumers (8 files):** agents/agents_test.go,
  skills/promote_test.go, refresh_test.go partial, commands/mcp_settings_test.go,
  commands/rules_test.go, internal/platform/mcp_settings_test.go,
  internal/platform/rules_test.go, scaffold_hooks_test.go (carried in by T5).
  Plus mixed contributions from commands/workflow/testutil_test.go and
  commands/workflow/workflow_integration_test.go.
- **Self-duplicated / table-driven (12 files):** resource_plan_test.go,
  state_plan_test.go, refresh_test.go (both blocks are self-dup),
  kg_test.go, workflow_integration_test.go (most blocks),
  delegation_fanout_test.go, iter_log_test.go, foldback_test.go,
  import_test.go, mcp_server_test.go, plan_task_test.go,
  drift_sweep_test.go.
- **Graphstore sub-helper (2 files):** sqlite_test.go, postgres_test.go.

### Implication for T2 sizing

`internal/testutil` ships with **8 exported helpers** (NewTempProject,
WriteAgentManifest, WriteSkillManifest, WriteCanonicalAgent,
WriteCanonicalSkill, WriteScopeFile, InitGitRepo, WriteAgentsRC) plus
the graphstore sub-helper's 3 exported runners — total 11 cross-package
test scaffolding entry points. The original goal of "net-add ~250 lines
in the new helper package" still holds; the rest of the line reduction
comes from in-place table-driven refactors, which net-remove without
adding to a helper package.

Counts drift on every push to PR head; T1 has re-snapshot at audit
kickoff (2026-05-04 numbers held). Subsequent extraction tasks should
not re-poll Sonar mid-iteration — the gate is verified at plan
closeout (T8) only.
