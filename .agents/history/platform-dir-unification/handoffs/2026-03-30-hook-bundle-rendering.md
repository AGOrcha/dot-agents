# Handoff: Hook Bundle Rendering

**Created:** 2026-03-30
**Author:** Claude Code session
**For:** AI Agent
**Status:** Ready to execute

---

## Summary

This session moved hooks from a design-only concept into a working Go implementation for canonical `HOOK.yaml` bundles under `~/.agents/hooks/<scope>/<name>/`. The current code now loads canonical bundles, resolves bundle-local commands, renders native hook JSON for Claude, Cursor, Codex, and Copilot, and falls back to legacy flat hook files when no applicable bundles exist.

The next agent should treat the active plan as the source of truth for rollout status and focus on the remaining Stage 1 gaps around write-rendered hook cleanup/removal and import/refresh support for canonical hook bundles. Bash parity is still intentionally deferred.

## Project Context

This repo is `dot-agents`, a Go rewrite of a tool that keeps canonical resources under `~/.agents` and wires them into project and user-level platform paths for Cursor, Claude Code, Codex, OpenCode, and GitHub Copilot.

Current design direction:

- one canonical source per resource type in `~/.agents`
- compatibility outputs first where useful
- native outputs when formats diverge
- Go-first rollout, bash parity later

This handoff is specifically about the hook portion of that rollout.

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

Still open in this phase:
- Additional coverage is now optional rather than required. The main remaining opportunities are cleanup/removal edge cases, command/import support for canonical hook bundles, and any future bash-parity validation once Phase 4 starts.

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
```

## Key Files

| File | Why It Matters |
|------|----------------|
| `.agents/active/platform-dir-unification.plan.md` | Current source of truth for rollout phase status and remaining work |
| `docs/CANONICAL_HOOKS_DESIGN.md` | Canonical hook storage and renderer design, including `HOOK.yaml` schema and migration stages |
| `internal/platform/hooks.go` | Shared hook contract, bundle loader, native renderers, and canonical-first fallback logic |
| `internal/platform/claude.go` | Claude-compatible hook rendering into `.claude/settings*.json` |
| `internal/platform/cursor.go` | Cursor canonical-first hook rendering into `.cursor/hooks.json` |
| `internal/platform/codex.go` | Codex canonical-first hook rendering into `.codex/hooks.json` |
| `internal/platform/copilot.go` | Copilot canonical-first `.github/hooks/*.json` fanout and Claude-compatible settings rendering |
| `internal/platform/hooks_test.go` | Unit coverage for bundle loading and shared hook helper behavior |
| `internal/platform/stage1_integration_test.go` | Integration coverage for translated native hook outputs from canonical bundles |
| `commands/import.go` | Still legacy-hook oriented; not yet able to normalize native outputs back into canonical `HOOK.yaml` bundles |
| `commands/refresh.go` | Current repo-relative mapping still points hook outputs back to legacy flat JSON destinations |

## Current State

**Done:**

---

## Update — 2026-03-30

Hook work moved well beyond the original handoff. The Go-side Stage 1 hook path now supports canonical `HOOK.yaml` bundles as the primary source, native write-rendered outputs for Claude, Cursor, Codex, and Copilot, cleanup of rendered hook artifacts during both `RemoveLinks` and repeated `CreateLinks` runs, and reverse-import of representable native hook outputs back into canonical bundles.

### What changed since the last handoff

- Implemented safe cleanup helpers for write-rendered hook outputs in `internal/platform/hooks.go`, and wired them into:
  - `internal/platform/claude.go`
  - `internal/platform/cursor.go`
  - `internal/platform/codex.go`
  - `internal/platform/copilot.go`
- Added transition cleanup so stale rendered hook files are pruned during repeated create/refresh flows, not only on remove:
  - Copilot now prunes stale `.github/hooks/*.json` files that were previously rendered from canonical bundles
  - Claude, Cursor, and Codex now remove stale rendered single-file hook outputs when canonical bundles disappear and no legacy fallback remains
- Expanded command-layer reverse import so representable native aggregate files now canonicalize into `hooks/<scope>/<name>/HOOK.yaml`:
  - `.cursor/hooks.json`
  - `.codex/hooks.json`
  - `.claude/settings.local.json`
  - `.claude/settings.json`
  - `.github/hooks/*.json`
- Expanded the canonical hook schema itself:
  - `HOOK.yaml` now supports `match.expression` alongside `match.tools`
  - shared matcher precedence is now:
    - `platform_overrides.<platform>.matcher`
    - canonical `match.expression`
    - canonical `match.tools`
- Tightened aggregate import heuristics:
  - smarter event/command/matcher-based bundle naming
  - stable numeric suffixes for collisions
  - stronger use of Copilot filename hints
  - Copilot multi-action fanout files can now split into multiple canonical hook bundles instead of always falling back to raw JSON

### Files most relevant now

- `internal/platform/hooks.go`
- `internal/platform/hooks_test.go`
- `internal/platform/claude.go`
- `internal/platform/cursor.go`
- `internal/platform/codex.go`
- `internal/platform/copilot.go`
- `internal/platform/stage1_integration_test.go`
- `commands/import.go`
- `commands/import_test.go`
- `commands/refresh.go`
- `commands/refresh_test.go`
- `commands/add.go`
- `docs/CANONICAL_HOOKS_DESIGN.md`
- `.agents/active/platform-dir-unification.plan.md`

### Validation completed

- `env GOCACHE=/tmp/go-build go test ./commands`
- `env GOCACHE=/tmp/go-build go test ./internal/platform`
- `env GOCACHE=/tmp/go-build go test ./...`

At the time of this update, the worktree was clean before appending this handoff update.

### Remaining hook work

The hook path is now in a much stronger place. The remaining opportunities are mostly polish rather than missing architecture:

- deeper transition cleanup edge cases for more complex multi-hook rename/delete sequences
- even better naming heuristics for highly ambiguous aggregate imports that still do not expose a stable logical identity
- eventual bash parity once Phase 4 starts

### New context: PR check investigation started

Work on a failing PR check has started in parallel:

- failing check reported by user: `SonarCloud Code Analysis`
- delegated to subagent: `Jason` (`019d3d33-37ab-7ca1-845f-adb82de6d145`)
- status at handoff update time: investigation started, no findings merged back yet

This matters because the next agent may need to resume either:

- hook-plan follow-through if the SonarCloud issue points back at these recent changes, or
- a separate PR-check remediation path once the subagent reports concrete failure details

### Status change

The handoff status should now be treated as:

- hook implementation checkpoint: largely current
- broader session status: active, because SonarCloud investigation is in progress in a delegated subagent
- Added `HOOK.yaml` bundle loading under `~/.agents/hooks/<scope>/<name>/HOOK.yaml`.
- Added bundle-local command resolution so `./script.sh` resolves relative to the hook bundle directory.
- Added native write-based JSON renderers for:
  - Claude settings-backed hooks
  - Cursor hooks
  - Codex hooks
  - Copilot per-file hook fanout
- Switched Go hook emitters to prefer canonical bundles and fall back to legacy flat files when no applicable bundles exist.
- Added integration tests for canonical bundle rendering and content-level assertions across platform outputs.
- Added `go.yaml.in/yaml/v3` to module metadata.

**In Progress:**
- Phase 3 remains in progress overall because hook cleanup/removal and command-layer import/refresh support for canonical hook bundles are not finished.
- The active plan is accurate and already updated through this hook bundle milestone.

**Not Started:**
- Bash parity for hook bundles and write-rendered native hook outputs.
- Canonical hook bundle import support in `commands/import.go` / `commands/refresh.go`.
- Cleanup/removal rules for write-rendered hook files beyond current link-oriented removal paths.

## Decisions Made

- **Canonical hook bundles are the target source of truth** — `HOOK.yaml` bundles now represent the normalized future model; legacy flat files remain only as migration fallback.
- **Canonical-first, legacy fallback** — platform emitters now try canonical bundle rendering first and only use linked flat JSON files when no applicable canonical bundles exist.
- **Use explicit platform allowlists** — `enabled_on` and `required_on` are honored at load/render time so unsupported hooks can be skipped or failed intentionally.
- **Keep the initial bundle schema small and renderable** — current implementation supports event, tool matcher, command, timeout, and platform overrides rather than trying to model every possible hook feature up front.
- **Write native files instead of linking rendered output** — once a hook comes from `HOOK.yaml`, its destination is a managed rendered file, not a symlink or hardlink.
- **Do not block on bash parity** — Go behavior is the priority; bash remains a later phase.
- **Use `/tmp` GOCACHE in this environment when needed** — `go test` and module metadata updates worked with `env GOCACHE=/tmp/go-build ...` because the default user cache path hit sandbox restrictions.

## Important Context

- `git diff --stat` currently prints nothing even though `git status --short` shows multiple tracked modifications and new files. Treat `git status` as authoritative here.
- Current worktree modifications at handoff:
  - `.agents/active/platform-dir-unification.plan.md`
  - `docs/CANONICAL_HOOKS_DESIGN.md`
  - `docs/PLATFORM_DIRS_DOCS.md`
  - `go.mod`
  - `go.sum`
  - `internal/platform/claude.go`
  - `internal/platform/codex.go`
  - `internal/platform/copilot.go`
  - `internal/platform/cursor.go`
  - `internal/platform/hooks.go`
  - `internal/platform/hooks_test.go`
  - `internal/platform/stage1_integration_test.go`
- There is an older handoff at `.agents/active/handoffs/2026-03-29-platform-dir-unification.md`, but it predates the canonical bundle loader/renderers and is now stale for hook work.
- The current implementation does not yet update `commands/import.go` or `commands/refresh.go` to reconstruct canonical `HOOK.yaml` bundles from rendered native hook files. Those commands still normalize hook outputs back into legacy flat JSON destinations.
- Removal logic is still mostly link-oriented. Write-rendered files are created by `writeManagedFile(...)` in `internal/platform/hooks.go`, but there is no corresponding unified removal path for those rendered outputs yet.
- The rendered platform shapes were based on a conservative, locally supported subset:
  - Claude: settings-style `"hooks"` object with matcher and command arrays
  - Codex: same broad structure for supported event subset
  - Cursor: `version: 1` and lower-camel event keys with simple command entries
  - Copilot: one JSON file per logical hook with `version: 1` and event-keyed command arrays
- OpenCode still has no dedicated hook surface and was intentionally not included in this milestone.

## Next Steps

1. **Implement cleanup/removal for rendered hook files** — update platform `RemoveLinks` paths or add shared helpers so write-rendered `.claude/settings*.json`, `.cursor/hooks.json`, `.codex/hooks.json`, and `.github/hooks/*.json` are cleaned up safely when they were generated from canonical bundles.
2. **Teach `import` / `refresh` about canonical hook bundles** — define how native rendered hook outputs should normalize back into `~/.agents/hooks/<scope>/<name>/HOOK.yaml` instead of legacy flat JSON files. Acceptance: command-layer mapping and tests no longer assume only `hooks/<scope>/*.json`.
3. **Decide whether to support mixed canonical-and-legacy merge behavior** — current canonical-first behavior chooses rendered canonical outputs over legacy ones. If mixed merging is desired, implement it deliberately with tests rather than accreting it implicitly.
4. **Decide whether `commands/hooks` should start authoring canonical bundles** — the shell hook management command is still Claude-settings-centric. If the repo wants a true single-SOT workflow, that command or its Go replacement will eventually need canonical bundle authoring support.
5. **Only after Go hook behavior is stable, start bash parity** — mirror the canonical-first hook logic into the bash platform scripts without changing the established Go behavior.

## Constraints

- Do not revert unrelated user changes.
- Keep the active plan file in `.agents/active/` accurate if you move the work forward.
- Preserve current externally visible behavior for legacy flat hook files unless you are intentionally migrating a platform and have tests for the change.
- Prefer canonical bundle loading plus native rendering over inventing new platform-specific flat source files.
- When running Go commands in this environment, prefer `env GOCACHE=/tmp/go-build ...` if the default cache path hits sandbox permission errors.
- Bash parity is not part of this handoff’s immediate next step unless the user explicitly redirects there.
