# Resource Intent Centralization Plan

Status: Phase 4 complete (2026-04-11); Phase 5 in progress — `RunSharedTargetProjection` unifies refresh/install/add shared-target dry-run + apply; status/explain audit registry done
Depends on: `docs/rfcs/resource-intent-centralization-rfc.md`

## Context

This plan builds on:

- `docs/rfcs/resource-intent-centralization-rfc.md`
- `.agents/active/platform-dir-unification.plan.md`
- `.agents/active/resource-sync-architecture-analysis.plan.md`
- `.agents/active/planner-resource-write-safety.md`

It is intentionally scoped to the shared resource ownership problem, not the concurrent skill-fix slice. The fork handling skill fixes can continue independently.

## Constraints

- Do not auto-promote review-stage skill outputs back into canonical managed resources yet.
- Do not solve shared-target conflicts by making low-level link helpers broadly destructive.
- Keep canonicalization separate from projection.
- Treat shared repo-local targets such as `.agents/skills/<name>` as centrally owned outputs, and carry canonical `agents/` projections through the same intent/planner framework once the shared-skill slice is in place.

## Goal

Introduce a maintainable internal model where platforms declare what they need, a central executor owns shared projection targets, and command flows (`import`, `install`, `refresh`, `remove`, `status`, `explain`) stop depending on scattered path logic and ad hoc filesystem mutations.

## Resolved Direction

The design questions that previously blocked this plan are now resolved in `docs/rfcs/resource-intent-centralization-rfc.md`.

- `ResourceIntent` is a small declarative model with typed source references, explicit ownership, shape/transport, and pruning/replacement policy.
- Shared repo-local targets are planned centrally before writes; platform adapters stay thin for truly platform-owned outputs.
- Import naming conflicts preserve both variants using origin-prefixed fallback names and create advisory review notes under `~/.agents/review-notes/import-conflicts/`.
- Non-empty directory replacement remains executor-only and allowlisted; low-level link helpers stay conservative.
- The first rollout slice is shared skill convergence first, immediately followed by canonical `agents/` bucket onboarding into the same framework: repo `.agents/skills/<name>` and related shared compatibility mirrors first, then repo-local `agents/` projections (`.claude/agents/`, `.codex/agents/*.toml`, `.opencode/agent/*.md`, `.github/agents/*.agent.md`) rather than treating agents as a separate architecture later.
- Focused verification should cover intent dedupe/conflicts, import conflict notes, imported directory -> managed mirror convergence, and status/explain registry correctness.

## Phase 1: Extract Shared Command Spine ✓ COMPLETE (2026-04-11)

- [x] Move shared project-sync helpers into `internal/projectsync` package.
- [x] Extracted and centralized:
  - project bucket directory creation (`CreateProjectDirs`)
  - refresh marker writing (`WriteRefreshMarker`, `RefreshMarkerContent`)
  - file copy helper (`CopyFile`)
  - gitignore entry helper (`EnsureGitignoreEntry`)
- [x] Behavior unchanged — all callers in add.go, refresh.go, import.go, install.go, init.go updated.
- Deferred to Phase 3: `mapResourceRelToDest` (tightly coupled to import.go constants); `restoreFromResourcesCounted` chain (transitively depends on `importCandidate`/`canonicalImportOutputs`).

## Phase 2: Introduce Resource Intent Model

- [x] Define an internal `ResourceIntent` shape for projection outputs. (`internal/platform/resource_intent.go`, validated by `internal/platform/resource_intent_test.go`)
- [x] Minimum fields are present on `ResourceIntent` / `ResourceSourceRef`:
  - target path
  - source bucket/scope resolver
  - transport (`symlink`, `hardlink`, rendered file, rendered fanout)
  - ownership (`shared`, `platform-owned`, `user-home`)
  - pruning/replacement policy
  - provenance label for diagnostics
- [x] Add a planner/executor layer that aggregates intents before any filesystem writes.

Completed in this session:
- Added `internal/platform/resource_plan.go` with a minimal `ResourcePlan` builder/executor:
  - validates and groups intents by conflict key
  - deduplicates compatible shared-target intents
  - fails on incompatible intents for the same target
  - executes the first allowlisted shared-skill mirror slice (`direct_dir` + `symlink`)
- Routed repo-local shared skill mirrors through the planner/executor in:
  - `internal/platform/claude.go`
  - `internal/platform/codex.go`
  - `internal/platform/opencode.go`
  - `internal/platform/copilot.go`
- Added focused regression coverage for:
  - identical-intent dedupe
  - conflicting-intent rejection
  - imported repo skill directory -> managed symlink convergence
- Requirement update: after the shared-skill slice, the same framework should absorb canonical `agents/` projections too; `agents/` is now part of the rollout scope, not a deferred architectural extra.

## Phase 3: Centralize Shared Repo Targets First

- [x] `refresh` / `install --dry-run` emit merged shared-target symlink lines (with duplicate-intent merge counts) before per-platform dry-run rows — operators can see the centralized plan without writes (2026-04-11).
- [ ] Migrate the highest-conflict repo-local outputs onto the shared executor first:
  - `.agents/skills/<name>`
  - `.claude/skills/<name>` when emitted as a shared compatibility mirror
  - `.claude/settings.local.json` compatibility output if multiple platforms still project it after the skill-mirror slice lands
- [ ] Deduplicate identical intents and fail fast on incompatible intents for the same path.
- [ ] Add safe directory replacement only in the centralized executor for approved shared targets.
- [ ] Extend the same planner/executor framework to canonical `agents/` projections immediately after the shared-skill path is stable:
  - `.claude/agents/<name>`
  - `.codex/agents/*.toml`
  - `.opencode/agent/*.md`
  - `.github/agents/*.agent.md`
  - any shared compatibility mirrors or cleanup paths needed to keep `agents/` behavior consistent across platforms

## Phase 4: Thin Platform Adapters ✓ COMPLETE (2026-04-11)

- [x] Refactor platform `CreateLinks()` implementations so they primarily emit intents instead of mutating the filesystem directly for shared outputs.
  - claude.createSkillsLinks: now only handles user-home skills; shared targets delegated to command layer (iteration 11)
  - codex.createSkillsLinks, opencode.createSkillsLinks, copilot.createSkillsLinks: now return nil; all shared targets delegated to command layer (iteration 12)
- [x] Leave truly platform-owned outputs local to the platform adapter:
  - `.codex/hooks.json`
  - `.github/copilot-instructions.md`
  - `.cursor/rules/*`
  - native rendered hook/config outputs
- [x] Preserve current precedence and transform behavior while moving ownership to the executor.
- [ ] Apply the same adapter-thinning to `agents/` projections once the planner supports them so platform code stops owning agent projection logic ad hoc. (deferred — SharedTargetIntents for agents/ not yet implemented)

## Phase 5: Unify Command Consumers

- [x] Single shared projection plan build: `BuildSharedTargetPlan` aggregates intents once; `DryRunSharedTargetPlanLines` and `CollectAndExecuteSharedTargetPlan` both use it (2026-04-11).
- [x] Update `refresh` to canonicalize repo context (`SetWindowsMirrorContext`), use `InstalledEnabledPlatforms`, then one shared projection step: `RunSharedTargetProjection` (merged plan → dry-run lines or execute) (2026-04-11).
- [x] Update `install` to call `RunSharedTargetProjection` in `createInstallPlatformLinks` after manifest linking (same executor as refresh; platform list = installed only) (2026-04-11).
- [x] `add` uses `RunSharedTargetProjection` for the shared-target apply path (2026-04-11).
- [x] `remove` calls `RemoveSharedTargetPlan` (merged shared intents) before per-platform `RemoveLinks`; idempotent overlap with adapter cleanup (2026-04-11).
- [x] Update `status` and `explain` to read from the same resource registry so diagnostics describe actual managed behavior rather than hand-maintained expectations. (`status --audit` / doctor verbose: `DryRunSharedTargetPlanLines` + `InstalledEnabledPlatforms`; `explain links` documents the path.)

## Phase 6: Verification

- [x] Add focused tests for shared-target intent dedupe and conflict detection. (2026-04-11 — `BuildSharedTargetPlan` aggregation path via `stubPlatform`: dedupe across platforms, conflicting intents error, `SharedTargetIntents` error wrap + `DryRunSharedTargetPlanLines` propagation; complements direct `BuildResourcePlan` tests)
- [x] Add import conflict coverage for stable origin-prefixed fallback naming and advisory review-note creation. (2026-04-11 — `importOutput.Origin` from hook specs; `importConflictFirstFreeAlternateDestRel` + `importPreservedConflictCandidate`; `~/.agents/review-notes/import-conflicts/ic-*.yaml`; tests)
- [x] Add refresh/import regression tests for imported directory -> managed shared-target transition. (2026-04-11 — `commands/refresh_test.go` `TestRefreshReplacesImportedRepoSkillDirWithManagedSymlink`: import-from-refresh then `RunSharedTargetProjection` + Claude `CreateLinks` replaces repo `.agents/skills/<name>/` dir with symlink)
- [x] Add coverage proving non-empty directory replacement is executor-only and allowlisted. (2026-04-11 — `internal/platform/resource_plan_test.go`: `TestExecuteDirSymlinkIntentRejectsNonAllowlistedImportedDirectory`, `TestExecuteDirSymlinkIntentRejectsAllowlistedDirectoryWithoutImportedMarkers`, `TestExecuteDirSymlinkIntentReplacesAllowlistedDirectoryWhenImportedMarkerPresent` lock `removeImportedDirIfAllowlisted` / `prepareIntentTargetForReplacement` refusal strings and success path)
- [ ] Add status/explain coverage so the new registry remains the source of truth.
- [ ] Run focused packages first, then `go test ./...`.

## Explicit Out Of Scope

- Review-stage skill condensation/promotion work noted in `planner-resource-write-safety.md`
- Bash parity changes under `src/lib/**`
- New Stage 2 resource buckets
- Any fix that relies on recursively deleting arbitrary user directories via generic link helpers
