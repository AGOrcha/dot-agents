# Handoff: Platform Dir Unification

**Created:** 2026-03-29
**Author:** Claude Code session
**For:** AI Agent
**Status:** Ready to execute

---

## Summary

This task is the Stage 1 Go-first rollout of the canonical `~/.agents` storage model documented in `docs/PLATFORM_DIRS_DOCS.md`. The current session landed the shared Go resource-resolution layer, rewired most current Go emitters onto it, updated normalization for OpenCode agents and Codex hooks, and marked the active plan file with honest progress status.

The next agent should treat the active plan as the source of truth for phase status, then finish the remaining Stage 1 Go work before starting any bash parity work. The most important incomplete items are `commands/status.go`, `commands/init.go`, and deciding how far to take native Codex agent emission versus keeping the current compatibility path for now.

## Project Context

This repo is `dot-agents`, a Go rewrite of a tool that keeps a canonical `~/.agents` directory and wires resources into project-specific platform paths for Cursor, Claude Code, Codex, OpenCode, and GitHub Copilot. The active design direction is:

- one canonical source per resource type in `~/.agents`
- greatest-common compatibility outputs first where useful
- native/platform-specific outputs where compatibility paths do not exist or formats diverge

The current rollout scope is Stage 1 only:

- `rules`
- `settings`
- `mcp`
- `skills`
- `agents`
- `hooks`
- current Cursor ignore support

Stage 2 resource buckets such as `commands`, `output-styles`, `modes`, `plugins`, `themes`, and `prompts` are intentionally deferred.

## The Plan

Source of truth: `.agents/active/platform-dir-unification.plan.md`

```markdown
# Canonical `~/.agents` Rollout Plan

## Summary

Implement this in two stages with `Go-first, bash-later` scope.

Stage 1 refactors only the currently supported resource set to the documented canonical-storage model: `rules`, `settings`, `mcp`, `skills`, `agents`, `hooks`, and current Cursor ignore support. Stage 2 adds the newly documented buckets: `commands`, `output-styles`, `ignore`, `modes`, `plugins`, `themes`, and `prompts`.

The parallelization strategy is: one coordinator owns the shared schema and command/mapping layer, then platform workers modify disjoint platform files only. Bash parity is a separate later phase so the first rollout is not blocked by shell-path collisions.

## Implementation Phases

### Phase 1: Shared Go spine and contract
Status: In progress

Owner: coordinator only

Files:
- `commands/init.go`
- `commands/import.go`
- `commands/refresh.go`
- `commands/status.go`
- `commands/explain.go`
- `internal/platform/platform.go`
- new shared helper file under `internal/platform/` for canonical resource resolution and emit rules
- shared tests in `commands/*_test.go`

Changes:
- Introduce a single internal resource-emission contract that all platforms use.
- Define canonical source resolution for the current resource set only.
- Encode emission modes explicitly: `symlink`, `hardlink`, and `transform/render`.
- Move path precedence decisions out of ad hoc platform logic into shared helpers where possible.
- Keep the public CLI surface unchanged.
- Update `init` so the documented stage-1 canonical directories are always present and explained consistently.
- Update `import` and `refresh` mapping so project files normalize back into the same canonical stage-1 buckets.
- Update `status` and `explain` text to reflect the new model and stop describing outdated direct-path assumptions.

Phase gate:
- No platform file changes until the shared contract, precedence rules, and import/refresh normalization are merged.

Completed in this session:
- Added a shared Go helper layer in `internal/platform/resources.go` for scoped source resolution and common resource directory syncing.
- Updated `commands/import.go` and `commands/refresh.go` normalization to understand `.codex/hooks.json` and `.opencode/agent/*.md`.
- Updated `commands/add.go` scanning so existing Codex hook files, OpenCode agents, and GitHub hook files are detected before takeover.
- Updated `commands/explain.go` and `src/share/templates/standard/README.md` so the documented structure better matches current Stage 1 behavior.

Still open in this phase:
- `commands/init.go` has not been updated to create or explain any new Stage 1 canonical structure beyond the existing layout.
- `commands/status.go` has not been updated yet to surface the new canonical/resource-emitter state.

### Phase 2: Go platform emitter wave
Status: In progress

Run these workers in parallel after Phase 1 lands.

Worker A: Cursor + Claude
Owned files:
- `internal/platform/cursor.go`
- `internal/platform/claude.go`

Scope:
- Rewire both platforms to consume the new shared contract.
- Preserve Cursor hardlink behavior where required.
- Keep the documented dual-output skill policy working from one canonical source.
- Keep Claude hooks/settings precedence aligned with the shared contract.

Worker B: Codex + OpenCode
Owned files:
- `internal/platform/codex.go`
- `internal/platform/opencode.go`

Scope:
- Replace current compat-only shortcuts with proper canonical-source emission.
- Keep `.agents/skills/` output for Codex/OpenCode compatibility where the contract says it is required.
- Add native transform support where a resource cannot be emitted as a raw directory symlink.

Worker C: GitHub Copilot
Owned files:
- `internal/platform/copilot.go`

Scope:
- Rewire Copilot outputs to the same shared contract.
- Keep Copilot-specific transforms isolated here: agent file naming, MCP target selection, hook-file fanout.

No-collision rule:
- Platform workers do not edit `commands/*.go`, shared helper files, or each other’s platform files.

Completed in this session:
- Worker A scope landed in one pass:
  - `internal/platform/cursor.go` now uses shared scoped resolution for settings, MCP, ignore files, and hooks.
  - `internal/platform/claude.go` now uses shared scoped resolution for MCP/settings precedence and shared skill directory syncing.
- Worker B scope partially landed:
  - `internal/platform/opencode.go` now emits `.opencode/agent/*.md` from canonical `agents/{scope}/{name}/AGENT.md` instead of the older `rules/opencode-*.md` path.
  - `internal/platform/codex.go` now uses shared scoped resolution for settings/skills and emits `.codex/hooks.json` from canonical hook files.
- Worker C scope landed for the current shared-resource subset:
  - `internal/platform/copilot.go` now uses shared scoped resolution for skills, MCP, and Claude-compatible hook/settings wiring.

Still open in this phase:
- Codex agents still use the existing compatibility path (`.claude/agents/`) rather than a native `.codex/agents/*.toml` transform.
- No platform-specific native transform/render layer has been added yet beyond direct file or directory linkage.

### Phase 3: Go integration and validation pass
Status: In progress

Owner: coordinator only

Files:
- shared helper file(s) from Phase 1
- `commands/import.go`
- `commands/refresh.go`
- `commands/status.go`
- test files

Changes:
- Reconcile any gaps found after platform branches merge.
- Add or expand table-driven tests for normalization and precedence.
- Add integration-style tempdir tests for the highest-risk outputs:
  - dual skill outputs
  - agent transforms
  - MCP target selection
  - Cursor hardlink behavior
  - hook fanout
- Run full `go test ./...`.

Acceptance for Stage 1:
- Current resource types emit from one canonical source in `~/.agents`.
- Refresh/import round-trips preserve the canonical buckets.
- No platform still depends on bespoke path logic that contradicts the shared contract.

Completed in this session:
- Added regression coverage in `commands/import_test.go` and `commands/refresh_test.go` for Codex hooks and OpenCode agent normalization.
- Ran `go test ./commands ./internal/platform ./internal/config ./internal/links`.
- Ran `go test ./...`.

Still open in this phase:
- No tempdir integration tests exist yet for dual-skill outputs, agent transforms, MCP target selection, hardlink behavior, or hook fanout.
- `commands/status.go` still needs a final reconciliation pass if the new outputs should be surfaced explicitly.

### Phase 4: Bash parity wave
Status: Not started

Start only after Stage 1 Go behavior is stable.

Coordinator-owned bash files:
- `src/lib/commands/init.sh`
- `src/lib/commands/import.sh`
- `src/lib/commands/refresh.sh`
- `src/lib/commands/status.sh`
- `src/lib/commands/explain.sh`
- `src/lib/utils/resource-restore-map.sh`

Parallel workers:
- Worker A: `src/lib/platforms/cursor.sh`, `src/lib/platforms/claude-code.sh`
- Worker B: `src/lib/platforms/codex.sh`, `src/lib/platforms/opencode.sh`
- Worker C: `src/lib/platforms/github-copilot.sh`

No-collision rule:
- Same ownership split as the Go wave.
- Bash workers do not touch `src/lib/commands/*` or shared utils.

### Phase 5: New bucket expansion
Status: Not started

After current resources are stable in both Go and bash.

Coordinator first:
- extend the shared contract for `commands`, `output-styles`, `ignore`, `modes`, `plugins`, `themes`, `prompts`
- extend `init`, `import`, `refresh`, `status`, and `explain`

Parallel worker split:
- Worker A: Cursor and Claude resource additions
- Worker B: OpenCode resource additions
- Worker C: Copilot resource additions
- Codex likely has no new standalone bucket unless docs or product behavior change

## Interfaces and Ownership Rules

Internal interface changes:
- Add a shared internal resource descriptor layer that defines:
  - canonical source bucket
  - project/global scope resolution
  - output target path(s)
  - emission mode
  - precedence order
- `Platform.CreateLinks` remains the public internal entrypoint, but platform implementations become thin emitters over the shared descriptor logic.

Ownership rules for parallel work:
- Only the coordinator edits shared schema, normalization, command UX, and shared tests.
- Platform workers own only their assigned platform files.
- Do not split one platform across multiple workers.
- Do not mix Go and bash edits in the same worker until the bash parity phase.
- Merge order is fixed: Phase 1 base, then Phase 2 workers in any order, then Phase 3 integration.

## Test Plan

- Update mapping tests around `mapResourceRelToDest` for every stage-1 canonical resource.
- Add tests for canonical-source precedence across `global` vs project scope.
- Add tempdir platform tests covering:
  - skills emitted to both required compat targets from one canonical source
  - agent transform outputs for Copilot and any Codex/OpenCode native formats
  - MCP target selection for Cursor, Claude, Codex, OpenCode, and Copilot
  - Cursor hardlink creation for rules and ignore files
  - hook emission and reserved-name handling
- Run `go test ./...` at the end of Phases 1, 3, and 5.
- Run bash-path verification only in Phase 4 and Phase 5 after shell parity work lands.

## Assumptions

- Chosen defaults:
  - `Go-first, bash later`
  - `Two-stage rollout`
- No new CLI commands or flags are required for Stage 1.
- Stage 1 covers only resources already implemented in some form today.
- `docs/PLATFORM_DIRS_DOCS.md` is the target architecture source of truth when resolving path-precedence disputes.
- If Codex/OpenCode native agent formats require lossy transforms, Stage 1 may keep compat outputs first and defer full native transform completeness to the Stage 3 integration pass, but the shared emitter hook points must exist in Phase 1.
```

## Key Files

| File | Why It Matters |
|------|----------------|
| `.agents/active/platform-dir-unification.plan.md` | Current source of truth for phase status and next steps |
| `internal/platform/resources.go` | New shared Go helper layer for scoped resource resolution and common syncing behavior |
| `internal/platform/opencode.go` | Reworked to emit OpenCode agents from canonical `agents/.../AGENT.md` |
| `internal/platform/codex.go` | Reworked to use shared resolution and now emits `.codex/hooks.json` |
| `commands/import.go` | Canonicalization import mapping now recognizes Codex hooks and OpenCode agent outputs |
| `commands/refresh.go` | Refresh normalization mirrors the import mapping changes |
| `commands/import_test.go` | Regression coverage for global and project hook normalization |
| `commands/refresh_test.go` | Regression coverage for OpenCode agent and Codex hook normalization |

## Current State

**Done:**
- Added `internal/platform/resources.go` with shared helpers for:
  - scoped source lookup
  - cross-bucket source lookup
  - canonical resource dir enumeration
  - common dir/file sync helpers
- Rewired Go platform emitters to use the shared resolver for current Stage 1 resources:
  - Cursor
  - Claude
  - Codex
  - OpenCode
  - GitHub Copilot
- OpenCode agents now emit from canonical `~/.agents/agents/{scope}/{name}/AGENT.md` to `.opencode/agent/{name}.md`.
- Codex now emits hooks from canonical `~/.agents/hooks/{scope}/codex.json` to `.codex/hooks.json`.
- Updated `commands/add.go` scanning for `.codex/hooks.json`, `.opencode/agent`, and `.github/hooks`.
- Updated `commands/import.go` and `commands/refresh.go` normalization for:
  - `.codex/hooks.json -> hooks/{scope}/codex.json`
  - `.opencode/agent/{name}.md -> agents/{scope}/{name}/AGENT.md`
- Added regression tests and ran:
  - `go test ./commands ./internal/platform ./internal/config ./internal/links`
  - `go test ./...`
- Updated `.agents/active/platform-dir-unification.plan.md` with explicit progress markings.

**In Progress:**
- Phase 1 is only partially complete because `commands/init.go` and `commands/status.go` still need reconciliation with the new shared-emitter model.
- Phase 3 is only partially complete because coverage is still mostly mapping-focused; tempdir integration tests do not exist yet.
- Codex agent output is still compatibility-first and has not been transformed to native `.codex/agents/*.toml`.

**Not Started:**
- Bash parity wave
- Stage 2 resource-bucket expansion
- Native transform/render work for platform-specific agent formats beyond the simple current cases

## Decisions Made

- **Go-first, bash-later remains the rollout boundary** — This keeps the shared contract and platform emitter refactor from colliding with the legacy shell implementation too early.
- **Two-stage rollout remains the scope boundary** — Current resources first, newly documented buckets later.
- **OpenCode agent emission should come from canonical `agents/` rather than `rules/opencode-*.md`** — The docs now treat agents as a first-class canonical resource, so the older rule-derived path was the wrong long-term shape.
- **Codex hooks were worth landing in Stage 1 now** — Hooks are already part of the Stage 1 resource set and there was a clean canonical file mapping available.
- **Codex native agent transform is still deferred** — The repo’s canonical schema is still `AGENT.md`-centric, and a native `.toml` emitter should be added deliberately rather than guessed.

## Important Context

- There was no existing active handoff file in `.agents/active/handoffs/`; this handoff is newly created.
- The active plan file was previously unmarked; this session updated it so the next agent should trust the plan file rather than older chat summaries.
- The worktree is uncommitted. No commit was made in this session.
- Current modified/untracked files from this task are:
  - `.agents/active/platform-dir-unification.plan.md`
  - `commands/add.go`
  - `commands/explain.go`
  - `commands/import.go`
  - `commands/import_test.go`
  - `commands/refresh.go`
  - `commands/refresh_test.go`
  - `internal/platform/claude.go`
  - `internal/platform/codex.go`
  - `internal/platform/copilot.go`
  - `internal/platform/cursor.go`
  - `internal/platform/opencode.go`
  - `internal/platform/resources.go`
- `commands/hooks.go` also shows as modified in `git status`, but it was not changed in this session. Treat that as pre-existing branch state and do not overwrite casually.
- The build/test baseline for this handoff is green on the Go side for the packages above.

## Next Steps

1. **Finish Phase 1 command reconciliation** — Update `commands/status.go` and `commands/init.go` so they reflect the Stage 1 canonical/resource-emitter model and do not describe stale behavior.
2. **Strengthen Phase 3 validation** — Add tempdir/integration-style tests for:
   - Cursor hardlink behavior
   - dual-skill outputs
   - OpenCode agent output
   - Codex hook output
   - MCP target selection and hook fanout
3. **Decide the Codex agent boundary for Stage 1** — Either:
   - keep `.claude/agents/` compatibility output as the explicit Stage 1 finish line, or
   - add a deliberate native `.codex/agents/*.toml` transform with tests
4. **Only after the Go path is stable, start Phase 4 bash parity** — follow the file-ownership split already encoded in the plan.

## Constraints

- Do not revert unrelated branch changes.
- Keep the rollout `Go-first, bash-later`.
- Keep Stage 2 resource buckets out of scope for now.
- Use `.agents/active/platform-dir-unification.plan.md` as the current truth for phase status.
- Preserve the canonical single-source policy documented in `docs/PLATFORM_DIRS_DOCS.md`.
- Be careful with `commands/hooks.go`; it is already modified in the worktree and was not part of this session’s implementation.
