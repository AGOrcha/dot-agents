# Handoff: ISP Orchestrator — Scoped Run ci-smoke-suite-hardening / error-message-compliance / kg-command-surface-readiness

**Created:** 2026-04-19
**Author:** Claude Code session
**For:** AI Agent
**Status:** Ready to execute — 3 of 5 ci-smoke-suite-hardening tasks done; 2 remain in scope

---

## Summary

The ISP orchestrator was running a scoped multi-plan pass over `ci-smoke-suite-hardening`, `error-message-compliance`, and `kg-command-surface-readiness`. Three tasks in `ci-smoke-suite-hardening` were completed and closed out this session. The session ended mid-run while inspecting context for the 4th task (`release-packaging-and-parity`). The next agent should pick up at the parent gate for that task — no bundle has been fanned out yet.

## Project Context

`dot-agents` is a Go CLI tool (Cobra, `cmd/dot-agents/main.go`) that manages AI agent configurations across projects. It has a canonical workflow engine (`commands/workflow.go`, `internal/`) with plan/task/delegation lifecycle, a KG bridge, and a CI setup in `.github/workflows/`. The repo uses a three-layer orchestrator model: plans in `.agents/workflow/plans/<id>/`, delegation bundles in `.agents/active/delegation-bundles/`, merge-backs in `.agents/active/merge-back/`.

## Completed This Session (ci-smoke-suite-hardening)

### Task 1: `establish-toolchain-and-built-binary-baseline` — **completed, closed out**
- Added `gofmt -l` check, `go vet`, binary-type verification, and extended smoke steps (`workflow status/health/plan`, `skills list`) to `.github/workflows/test.yml`
- Fixed formatting in 5 files: `internal/graphstore/crg.go`, `internal/graphstore/sqlite.go`, `internal/graphstore/sqlite_test.go`, `internal/graphstore/store.go`, `internal/platform/opencode.go`
- Commits: `b99268f`

### Task 2: `isolate-home-based-smoke-harness` — **completed, closed out**
- Added job-level `env: SMOKE_HOME: ${{ runner.temp }}/smoke-home` and `SMOKE_AGENTS_HOME: ${{ runner.temp }}/smoke-home/.agents` to the `test` job
- All 15 smoke steps carry `HOME`/`AGENTS_HOME` overrides; gofmt/vet/test/build steps are untouched
- Fixed actionlint warnings: replaced `mktemp`+`GITHUB_ENV` pattern with static `runner.temp` reference
- Commits: `4081ed3`, `bfc78cf`

### Task 3: `expand-command-surface-smokes` — **completed, closed out**
- Added 9 new smoke steps: `workflow orient`, `workflow next`, `kg --help`, `kg bridge health`, `agents list`, `hooks list`, `rules list`, `mcp list`, `settings list`
- Orchestrator fixed a worker omission (rules/mcp/settings list wrongly excluded) before accepting
- Commits: `4f068fc`, `4ae1a06`

## Current State

**Done (3/5 ci-smoke-suite-hardening):**
- `establish-toolchain-and-built-binary-baseline`
- `isolate-home-based-smoke-harness`
- `expand-command-surface-smokes`

**Not started — next up:**
- `release-packaging-and-parity` — no bundle created yet; orchestrator was reviewing context when session ended
- `define-heavy-integration-lane` — blocked on `expand-command-surface-smokes` (now unblocked)

**Other scoped plans (0 tasks completed this session):**
- `error-message-compliance` — 0 tasks listed (status: proposed, 0 pending); likely needs plan authoring before tasks are actionable
- `kg-command-surface-readiness` — 7 tasks all pending; not yet reached this session

## Key Files

| File | Why It Matters |
|------|----------------|
| `.github/workflows/test.yml` | Primary CI file; all 3 completed tasks modified this file |
| `.github/workflows/auto-release.yml` | Release CI; `release-packaging-and-parity` must align this with `test.yml` |
| `.goreleaser.yaml` | Release config; `release-packaging-and-parity` write_scope includes this |
| `.agents/workflow/plans/ci-smoke-suite-hardening/TASKS.yaml` | Canonical task state for the active plan |
| `.agents/workflow/plans/ci-smoke-suite-hardening/ci-smoke-suite-hardening.plan.md` | Plan spec — read this before fanning out the next task |

## Decisions Made

- **`runner.temp` over `mktemp`** — actionlint cannot statically verify GITHUB_ENV-set variables; `runner.temp` is a known static context. All future CI isolation should use `runner.temp`.
- **`rules list`/`mcp list`/`settings list` are valid smokes** — worker incorrectly excluded them; orchestrator verified they exit 0 on fresh HOME and added them directly.
- **No new external lint tools** — plan constraint; only `gofmt` and `go vet` (already in Go toolchain) added, not golangci-lint or similar.

## Important Context

- The `auto-release.yml` uses `src/bin/dot-agents` (the old shell launcher shim) for version verification and smoke, not the built Go binary. `release-packaging-and-parity` should fix this inconsistency — see `auto-release.yml` lines 51–58 and 77–83.
- The `auto-release.yml` runs `go test ./...` without `-race` or `-count=1` flags, unlike `test.yml`. Aligning these is within scope for `release-packaging-and-parity`.
- `go-version: '1.26.x'` in both workflows is a future Go version (1.26 doesn't exist yet); this is pre-existing drift, not something this plan introduced — note it but don't fix it as part of this plan.
- `error-message-compliance` shows 0 pending tasks — check `workflow orient` to confirm if it needs plan authoring before tasks can be selected.
- `kg-command-surface-readiness` has 7 pending tasks with `kg-freshness-audit` as the entry task; it depends on no external plans in the scoped set.

## Next Steps

1. **Start ISP orchestrator pass** with `--plan ci-smoke-suite-hardening,error-message-compliance,kg-command-surface-readiness`
   - Use `go run ./cmd/dot-agents workflow complete --json --plan ci-smoke-suite-hardening,error-message-compliance,kg-command-surface-readiness` to confirm state is `actionable` and next task is `release-packaging-and-parity`

2. **Fanout `release-packaging-and-parity`** (write_scope: `.github/workflows/`, `.goreleaser.yaml`, `scripts/`, `docs/`)
   - Goal: add `goreleaser check` (dry-run packaging validation) to `test.yml`; replace `src/bin/dot-agents` references in `auto-release.yml` with the built Go binary; align `go test` flags between the two workflows
   - Must not break the actual release flow — changes to `auto-release.yml` should be conservative

3. **After `release-packaging-and-parity`:** fanout `define-heavy-integration-lane` (last ci-smoke-suite-hardening task; `verification_required: false`; write_scope: `.github/workflows/`, `tests/`, `docs/`)

4. **Then pivot to `kg-command-surface-readiness`** — `go run ./cmd/dot-agents workflow next --plan kg-command-surface-readiness` to confirm entry task

5. **Check `error-message-compliance`** — `go run ./cmd/dot-agents workflow orient` to understand why it shows 0 pending tasks; may need `/plan-wave-picker` or plan authoring

## Constraints

- Use ISP pattern: orchestrator selects → fanout → impl worker (loop-worker subagent) → verifier → review → parent closeout. Do not collapse orchestrator + worker into one session.
- All new CI steps must follow the `HOME`/`AGENTS_HOME` isolation pattern already in `test.yml` (job-level env `SMOKE_HOME`/`SMOKE_AGENTS_HOME`, per-step env override)
- Do not touch `go-version: '1.26.x'` — it's pre-existing drift outside this plan's scope
- `go run ./cmd/dot-agents workflow delegation closeout` + `workflow advance` are required after each accepted review — do not skip
- Scoped plan set is fixed: `ci-smoke-suite-hardening,error-message-compliance,kg-command-surface-readiness` — do not pull in `planner-evidence-backed-write-scope` or `resource-command-parity` even if they appear in `workflow next`
