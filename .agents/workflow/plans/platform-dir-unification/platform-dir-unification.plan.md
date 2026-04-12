# Canonical `~/.agents` Rollout Plan

Status: Blocked — Phases 1+2+3 complete (Go layer). Phases 4+5 (bash parity, new bucket expansion) are blocked on resource-intent-centralization implementation rollout.
Depends on: `docs/rfcs/resource-intent-centralization-rfc.md` plus the resulting resource-intent-centralization implementation



## Summary

Implement this in two stages with `Go-first, bash-later` scope.

Stage 1 refactors only the currently supported resource set to the documented canonical-storage model: `rules`, `settings`, `mcp`, `skills`, `agents`, `hooks`, and current Cursor ignore support. Stage 2 adds the newly documented buckets: `commands`, `output-styles`, `ignore`, `modes`, `plugins`, `themes`, and `prompts`.

The parallelization strategy is: one coordinator owns the shared schema and command/mapping layer, then platform workers modify disjoint platform files only. Bash parity is a separate later phase so the first rollout is not blocked by shell-path collisions.

## Implementation Phases

### Phase 1: Shared Go spine and contract
Status: Completed

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
- Updated `commands/status.go` to surface the canonical store at the top of `dot-agents status` and to account for newer Stage 1 outputs such as Codex hooks and OpenCode agents.
- Updated `commands/init.go` so the generated `~/.agents/README.md` and completion guidance describe the Stage 1 canonical buckets more clearly.

Still open in this phase:
- No additional Phase 1 command-layer work is required before moving on to the remaining platform and validation items.

### Phase 2: Go platform emitter wave
Status: Completed

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
  - `internal/platform/codex.go` now uses shared scoped resolution for settings/skills, emits `.codex/hooks.json` from canonical hook files, and renders native `.codex/agents/*.toml` from canonical `AGENT.md` files.
- Worker C scope landed for the current shared-resource subset:
  - `internal/platform/copilot.go` now uses shared scoped resolution for skills, MCP, and Claude-compatible hook/settings wiring.

Still open in this phase:
- No additional Phase 2 emitter work is required for the Stage 1 resource set.

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
- Added a concrete hook design doc in `docs/CANONICAL_HOOKS_DESIGN.md` describing canonical `HOOK.yaml` bundles, platform allowlists, and shared hook emission semantics.
- Added a first shared hook contract in `internal/platform/hooks.go` with `HookSpec`, explicit emission modes, and shared direct/fanout emit helpers.
- Migrated current Go hook wiring in `internal/platform/cursor.go`, `internal/platform/claude.go`, `internal/platform/codex.go`, and `internal/platform/copilot.go` onto the shared hook helpers while preserving current behavior.
- Implemented canonical `HOOK.yaml` bundle loading in `internal/platform/hooks.go`, including bundle metadata, platform allowlists, and relative-command resolution against the hook bundle directory.
- Implemented native write-based hook rendering for canonical bundles into:
  - `.claude/settings*.json`
  - `.cursor/hooks.json`
  - `.codex/hooks.json`
  - `.github/hooks/*.json`
- Switched the Go hook emitters to a canonical-bundle-first policy with legacy flat-file hook configs as fallback when no applicable bundles exist.
- Added integration-style tests in `internal/platform/platform_test.go` covering:
  - OpenCode agent emission from canonical `agents/{scope}/{name}/AGENT.md`
  - Codex hook emission to both project and user scope
- Added integration-style tests in `internal/platform/stage1_integration_test.go` covering:
  - Claude dual skill outputs into `.claude/skills/` and `.agents/skills/`
  - Cursor hardlink behavior and MCP target selection
  - Copilot MCP target selection and hook fanout/priority
- Added dedicated Codex-native coverage in `internal/platform/codex_test.go` for:
  - TOML rendering from canonical `AGENT.md`
  - native `.codex/agents/*.toml` creation and cleanup behavior
- Added focused helper coverage in `internal/platform/hooks_test.go` for hook-spec precedence and shared hook fanout emission.
- Expanded `internal/platform/stage1_integration_test.go` with higher-risk precedence coverage for:
  - Claude hook-vs-settings compat selection at both project and user scope
  - Cursor hook scope precedence and MCP scope-first fallback
  - Copilot Claude-compat scope precedence and Copilot instruction precedence
  - Codex `codex-hooks.json` project fallback precedence over global `codex.json`
- Added end-to-end hook translation coverage in `internal/platform/stage1_integration_test.go` for:
  - project hook sources translating into Cursor, Codex, Claude-compatible, and Copilot-native outputs
  - settings-bucket fallback translating into Claude and Copilot compatibility outputs when hook files are absent
- Added canonical hook bundle coverage in `internal/platform/hooks_test.go` and `internal/platform/stage1_integration_test.go` for:
  - `HOOK.yaml` bundle discovery
  - absolute command resolution from bundle-local scripts
  - native JSON rendering for Claude, Cursor, Codex, and Copilot hook outputs
- Added safe cleanup helpers in `internal/platform/hooks.go` for write-rendered hook outputs by removing only files whose bytes still match the canonical renderer output.
- Updated `RemoveLinks` in `internal/platform/claude.go`, `internal/platform/cursor.go`, `internal/platform/codex.go`, and `internal/platform/copilot.go` so canonical hook bundles clean up their rendered repo outputs instead of relying on symlink-only removal.
- Added integration coverage in `internal/platform/stage1_integration_test.go` for rendered-hook cleanup during `RemoveLinks` across Claude, Cursor, Codex, and Copilot.
- Started command-layer canonical bundle round-tripping for native Copilot hook fanout:
  - `.github/hooks/<name>.json` now normalizes to `hooks/<project>/<name>/HOOK.yaml`
  - `commands/import.go` can translate representable Copilot hook files into canonical `HOOK.yaml` bundles
  - `commands/add.go` restore-from-resources now rehydrates those backed-up Copilot hook files into canonical bundles
- Expanded command-layer canonical hook import beyond Copilot fanout:
  - `commands/import.go` now parses representable aggregate hook outputs from `.cursor/hooks.json`, `.codex/hooks.json`, and Claude-compatible settings files into multiple canonical `HOOK.yaml` bundles
  - unsupported aggregate hook files still fall back to the legacy raw-import path instead of losing data
  - `commands/add.go` restore-from-resources now rehydrates representable aggregate hook backups into canonical bundles too
  - Copilot native hook fanout import now uses the source filename as a stronger identity hint and can split multi-action files into multiple canonical hook bundles before resorting to legacy fallback
- Added transition cleanup during create/refresh for hook outputs:
  - `internal/platform/copilot.go` now prunes stale rendered `.github/hooks/*.json` files that are no longer part of the current canonical hook set
  - `internal/platform/claude.go`, `internal/platform/cursor.go`, and `internal/platform/codex.go` now remove stale rendered single-file hook outputs when canonical bundles disappear and no legacy fallback remains
- Improved reverse-import heuristics for aggregate hook files:
  - generated canonical hook bundle names now use event plus smarter command/matcher hints instead of only event plus command stem
  - normalized matcher strings can now populate both canonical `match.tools` and canonical `match.expression` when whitespace or richer syntax would otherwise be lost
- Expanded the canonical hook schema itself to capture richer matcher semantics:
  - `HOOK.yaml` now supports `match.expression` alongside `match.tools`
  - shared hook loading/rendering in `internal/platform/hooks.go` now treats matcher precedence as `platform_overrides.<platform>.matcher` → `match.expression` → `match.tools`
  - Copilot representability checks now treat canonical `match.expression` as a real matcher constraint instead of only looking at `match.tools`
- Closed the remaining Claude user-settings cleanup gap:
  - `internal/platform/claude.go` now removes stale rendered `~/.claude/settings.json` files when global canonical hooks disappear and no legacy compat file exists
- Added regression coverage in `commands/import_test.go` and `commands/refresh_test.go` for canonical Copilot hook mapping and resource restore into `HOOK.yaml` bundles.
- Added regression coverage in `commands/import_test.go` for aggregate native hook import from Cursor, Codex, and Claude-compatible settings files.
- Added regression coverage in `commands/import_test.go` for smarter imported hook naming and canonical `match.expression` preservation.
- Added regression coverage in `commands/import_test.go` for the hardest aggregate native imports:
  - generic shared commands distinguished by matcher hints
  - duplicate identity collisions getting stable suffixes
  - multi-action aggregate inputs splitting into distinct canonical hook bundles
  - Copilot multi-action fanout canonicalization versus fallback only on truly unsupported events
- Expanded `internal/platform/stage1_integration_test.go` with transition coverage for stale rendered hook cleanup during repeated `CreateLinks` runs.
- Expanded `internal/platform/stage1_integration_test.go` with coverage for stale global Claude user settings cleanup when canonical hook bundles disappear.
- Added hook-schema coverage in `internal/platform/hooks_test.go` and `internal/platform/stage1_integration_test.go` for:
  - loading `match.expression` from `HOOK.yaml`
  - rendering canonical matcher expressions through Claude and Cursor outputs
  - treating canonical matcher expressions as non-representable for Copilot fanout
- Re-ran `env GOCACHE=/tmp/go-build go test ./internal/platform`, `env GOCACHE=/tmp/go-build go test ./commands`, and `env GOCACHE=/tmp/go-build go test ./...`.

Still open in this phase:
- Additional coverage is now optional rather than required. The main remaining opportunities are deeper transition cleanup edge cases for more complex multi-hook renames and deletions, better naming heuristics for highly ambiguous aggregate imports that still lack stable logical names, and any future bash-parity validation once Phase 4 starts.

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
