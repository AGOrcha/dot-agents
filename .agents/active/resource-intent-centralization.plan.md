# Resource Intent Centralization Plan

## Context

This plan builds on:

- `.agents/active/platform-dir-unification.plan.md`
- `.agents/active/resource-sync-architecture-analysis.plan.md`
- `.agents/active/planner-resource-write-safety.md`

It is intentionally scoped to the shared resource ownership problem, not the concurrent skill-fix slice. The fork handling skill fixes can continue independently.

## Constraints

- Do not auto-promote review-stage skill outputs back into canonical managed resources yet.
- Do not solve shared-target conflicts by making low-level link helpers broadly destructive.
- Keep canonicalization separate from projection.
- Treat shared repo-local targets such as `.agents/skills/<name>` as centrally owned outputs.

## Goal

Introduce a maintainable internal model where platforms declare what they need, a central executor owns shared projection targets, and command flows (`import`, `install`, `refresh`, `remove`, `status`, `explain`) stop depending on scattered path logic and ad hoc filesystem mutations.

## Phase 1: Extract Shared Command Spine

- [ ] Move shared canonicalization/project-sync helpers out of `commands/add.go` and `commands/refresh.go` into a neutral package, likely `internal/projectsync` or `internal/resources`.
- [ ] Extract and centralize:
  - canonical path mapping (`mapResourceRelToDest`)
  - project bucket directory creation
  - resource restore from `~/.agents/resources`
  - refresh marker writing
- [ ] Keep command behavior unchanged during extraction.

## Phase 2: Introduce Resource Intent Model

- [ ] Define an internal `ResourceIntent` shape for projection outputs.
- [ ] Minimum fields:
  - target path
  - source bucket/scope resolver
  - transport (`symlink`, `hardlink`, rendered file, rendered fanout)
  - ownership (`shared`, `platform-owned`, `user-home`)
  - pruning/replacement policy
  - provenance label for diagnostics
- [ ] Add a planner/executor layer that aggregates intents before any filesystem writes.

## Phase 3: Centralize Shared Repo Targets First

- [ ] Migrate the highest-conflict repo-local outputs onto the shared executor first:
  - `.agents/skills/<name>`
  - `.claude/agents/<name>` if it remains shared
  - `.claude/settings.local.json` compatibility output if multiple platforms still project it
- [ ] Deduplicate identical intents and fail fast on incompatible intents for the same path.
- [ ] Add safe directory replacement only in the centralized executor for approved shared targets.

## Phase 4: Thin Platform Adapters

- [ ] Refactor platform `CreateLinks()` implementations so they primarily emit intents instead of mutating the filesystem directly for shared outputs.
- [ ] Leave truly platform-owned outputs local to the platform adapter:
  - `.codex/hooks.json`
  - `.github/copilot-instructions.md`
  - `.cursor/rules/*`
  - native rendered hook/config outputs
- [ ] Preserve current precedence and transform behavior while moving ownership to the executor.

## Phase 5: Unify Command Consumers

- [ ] Update `refresh` to:
  1. canonicalize inputs
  2. build projection intents
  3. execute one projection plan
- [ ] Update `install` to use the same projection executor after canonical source linking.
- [ ] Update `remove` to remove managed outputs via the same registry/intents instead of platform-specific path lists where possible.
- [ ] Update `status` and `explain` to read from the same resource registry so diagnostics describe actual managed behavior rather than hand-maintained expectations.

## Phase 6: Verification

- [ ] Add focused tests for shared-target intent dedupe and conflict detection.
- [ ] Add refresh/import regression tests for imported directory -> managed shared-target transition.
- [ ] Add status/explain coverage so the new registry remains the source of truth.
- [ ] Run focused packages first, then `go test ./...`.

## Explicit Out Of Scope

- Review-stage skill condensation/promotion work noted in `planner-resource-write-safety.md`
- Bash parity changes under `src/lib/**`
- New Stage 2 resource buckets
- Any fix that relies on recursively deleting arbitrary user directories via generic link helpers
