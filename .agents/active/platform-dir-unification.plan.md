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
