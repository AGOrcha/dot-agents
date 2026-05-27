# go-test-fixture-extraction — impl-results

Plan archive timestamp: 2026-05-26
Closeout task: t8-archive
Source decision: docs/adr/0008-pr10-duplication-scope.md (option B, Cluster D)

## Hard outcome

SonarCloud quality gate on master (live API snapshot `api/qualitygates/project_status?projectKey=NikashPrakash_dot-agents&branch=master`, 2026-05-26):

| Metric | Before (PR#10 baseline, 2026-05-04) | After (master, 2026-05-26) | Threshold |
|---|---|---|---|
| `new_duplicated_lines_density` | 4.7% (ERROR) | 0.1% (OK) | < 3% |
| `duplicated_lines` (overall) | ~2,500 Cluster D portion | 131 total / 7 blocks | n/a |
| `duplicated_lines_density` (overall) | n/a recorded | 0.2% | n/a |

- Plan-level success criterion `new_duplicated_lines_density below 3% OR Cluster D reduced ≥ 60%`: both clauses satisfied. Cluster D reduction is **~94.8%** (2,501 dup lines → 131 surviving across the whole project, of which most surviving blocks are Cluster E.production-code, not Cluster D test scaffolding).
- The wider gate is still ERROR on `new_security_rating` and `new_security_hotspots_reviewed` (separate hotspot remediation tracks; orthogonal to this plan per ADR-0008 scope split).

## What landed (commit lineage on master)

All 7 extraction tasks completed before t8-archive ran:

- **t1** (audit) → updated `.agents/workflow/specs/go-test-fixture-extraction/design.md` with the canonical helper signature table.
- **t2** (create) → `internal/testutil/` package with `NewTempProject`, `WriteAgentsRC`, `WriteScopeFile`, etc.; covered by `internal/testutil/testutil_test.go`.
- **t2_5** (extend) → added `NewTempAgentsHome` and `WriteScopeFilePath` to absorb env-var preamble and nested-path seeds surfaced by t5/t7 mid-flight.
- **t3** (platform tests) → `internal/platform/{resource_plan,mcp_settings,rules}_test.go` migrated to `internal/testutil`. Per-file commits.
- **t3b** (relocate) → coverage_gap{,2..5}_test.go (5 sentinel files, ~4,476 lines, 210 tests) relocated into topic-named files (`plugins_test.go`, `cursor_test.go`, `claude_test.go`, `codex_test.go`, `copilot_test.go`, `opencode_test.go`, `lifecycle_test.go`, plus existing topic files). All 5 source files deleted. Coverage gate held at >95% per per-file gate (no allowlist per `[[no-lazy-allowlist-tech-debt]]`).
- **t4** (workflow tests) → 8 files in `commands/workflow/` migrated. Local helper `testutil_test.go` collapsed onto `internal/testutil`. Merged via PR #44.
- **t5** (commands root) → `commands/{refresh,import,mcp_settings,rules}_test.go` migrated. Merged via PR #49 (orphan-commit recovery branch).
- **t6** (graphstore tests, file-level) → `internal/graphstore/{sqlite,postgres,mcp_server}_test.go`; merged via PR #84. Fold-back surfaced that bulk-mirrored backend tests needed a graphstore-specific sub-helper, leading to t6b + t6c.
- **t6b** (extend storetest) → added `RunMetadataRoundTrip`, `RunEdgeUpsertCreate/Update`, `RunNoteSymbolLinkRoundTrip/Idempotent` runners under `internal/graphstore/internal/storetest`. Prefix-isolated for shared postgres testcontainer.
- **t6c** (graphstore migrate) → migrated `sqlite_test.go` + `postgres_test.go` onto the t6b runners; one commit per backend.
- **t7** (skills/agents/kg tests) → `commands/{agents/agents,skills/promote,kg/kg}_test.go` migrated. Initial spawn was no-op (already migrated); re-routed work in archived `t7-no-op/`.

## What the per-task merge-backs do not capture

Per-task merge-backs in `.agents/history/archived-delegations/2026-05-{23,24,26-w*}/` cover each task's local diff and verification trace. This document records the cross-cutting facts that no single task owned:

1. **Plan-level Sonar verification.** The gate snapshot above is the closeout deliverable; individual extraction tasks could only verify their package's `go test` not the project-wide duplication-density signal.
2. **The fold-back loop that produced t2_5, t6b, t6c, and t3b.** None of these were in the original PLAN.yaml; each was surfaced mid-flight by a downstream task's worker and back-pressured into a new dependency. The plan started with 8 tasks; it ended with 12 because the extraction pattern was richer than the audit captured.
3. **Anti-scope adherence.** No production-code-helper-extraction (Cluster E) commits were bundled into any of these PRs. ADR-0008's scope split held end-to-end. The remaining 131 duplicated lines on master are almost entirely Cluster E (cross-module `list/render/promote`), which is a separate plan.

## Verification trace at closeout

- `go test ./... -count=1` from `.agents/worktrees/t8-archive` (master HEAD `47776c68`): all 28 packages PASS except `TestCRGBridgeFreshBuildRealCRG` in `internal/graphstore`, which fails due to the documented environment-dependent missing Python module `code_review_graph` (pre-existing, called out in t6b notes; unrelated to this plan).
- SonarCloud API (`/api/qualitygates/project_status?projectKey=NikashPrakash_dot-agents&branch=master`, 2026-05-26): `new_duplicated_lines_density = 0.1%` (status OK against the 3% threshold).
- SonarCloud API (`/api/measures/component`): `duplicated_lines = 131`, `duplicated_blocks = 7`, `duplicated_lines_density = 0.2%` project-wide.

## Cross-references

- Source decision: `docs/adr/0008-pr10-duplication-scope.md` (option B).
- Source classification: `.agents/history/sonarqube-pr10/findings.md` (Cluster D row).
- Sibling deferred plan: `production-code-helper-extraction` (Cluster E.other) — independent per ADR-0008.
