# Spec: shared-target-projection-wiring

**Status:** active (decision locked — forward-port, not redesign)
**Created:** 2026-05-18
**Origin:** `.agents/proposals/codex-hooks-agents-linking-gap.md` (audit, 2026-05-18)
**Related:** `pr10-branch-split` (this is a missing-change the split did not carry),
`config-distribution-model` / resource-model work (the producer this consumes),
`platform-session-integration`.

---

## 1. Problem

The shared-target projection — the code that materializes repo-level
`.codex/agents/*.toml` **and** Claude's shared `.claude/skills/` /
`.agents/skills/` projection — has **no production caller on master**.

Ground truth (audit, file:line in the proposal):

- The producer landed on master via `bdaf37ea feat(platform): resource
  model, shared target plan, projectsync`:
  `codex.SharedTargetIntents` → `BuildSharedCodexAgentTomlIntents` →
  `executeRenderSingleWrite`, run by `CollectAndExecuteSharedTargetPlan`
  / `RunSharedTargetProjection` / `BuildSharedTargetPlan`
  (`internal/platform/resource_plan.go:467-492`).
- `codex.createAgentsLinks` only **prunes** stale tomls
  (`codex.go:195-199`); it does not materialize them.
- The command-layer invocation (`829a3490 commands: wire
  CollectAndExecuteSharedTargetPlan before platform loops`,
  `c78b470b platform: add RunSharedTargetProjection for
  refresh/install/add`) shipped **only** on
  `feature/PA-cursor-projectsync-phase1-extract-293f` and was never
  merged to master (`git merge-base --is-ancestor 829a3490
  origin/master` → not an ancestor).

Effect on master: `da refresh` / `da install` / `da add` never produce
repo-level `.codex/agents/*.toml`, and Claude's shared skills projection
is dormant. User-scoped `~/.codex/agents/*.toml` still works (written
directly via `ensureUserAgents`), which masked the gap.

This is the same artifact-loss class as the stranded prompts/specs:
designed-and-implemented work that never reached master because the
integration commit was branch-only. `pr10-branch-split`'s
`retire-old-branches` task is the intended safety net (its step 1 —
"git diff merged master vs original branch tip shows no missing
changes" — should have caught this); this spec is the explicit
remediation rather than relying on that catch.

## 2. Goal

`da refresh`, `da install`, and `da add` invoke the shared-target
projection once per project before the per-platform `CreateLinks` loop,
so repo-level shared targets (Codex agent TOMLs, Claude shared skills)
materialize on master — with verified dry-run and idempotence parity.

## 3. Decision (locked)

**Forward-port, do not redesign.** The producer side is complete and
tested on master; the design intent is captured by the resource-model
work and the reference commits `829a3490` / `c78b470b`. This is a
designed-but-unmerged *integration*, not open design. Reproduce the
reference wiring on master, verifying it against current master
(call-site signatures may have drifted since the feature branch).

Rejected: re-architecting the projection or introducing a new
invocation model — out of scope, no evidence the design is wrong (it
works on the feature branch and in `internal/platform` tests).

## 4. Requirements (behavioral)

1. After this lands, on master, `da refresh` in a project with enabled
   platforms produces repo-level `.codex/agents/*.toml` and the Claude
   shared `.claude/skills/` / `.agents/skills/` projection — matching
   the feature-branch behavior.
2. `da install` and `da add` exhibit the same projection (same entry
   ordering: projection before per-platform `CreateLinks`).
3. Dry-run (`RunSharedTargetProjection(..., dryRun)`) reports intended
   writes without mutating the tree, and a second consecutive real run
   is a no-op (idempotent) — both proven by regression tests.
4. No regression to existing per-platform `CreateLinks`, user-scoped
   agent/hook writing, or `projectsync` behavior.
5. Codex hooks behavior is unchanged (already correct on master — not
   in scope; see audit Finding 1).

## 5. Done criteria

- A production caller of `CollectAndExecuteSharedTargetPlan` (or
  `RunSharedTargetProjection`) exists in the `refresh`/`install`/`add`
  command paths, ordered before the per-platform loop, scoped to the
  enabled platform set.
- Regression tests assert: (a) repo `.codex/agents/*.toml` produced,
  (b) Claude shared-skills projection produced, (c) dry-run no-mutation,
  (d) idempotent second run — all on master.
- `go test ./... ` green; per-file coverage gate satisfied for the
  touched command files (interface-seam pattern per
  [[prefer-test-seam-over-untestable]] / di-refactor direction — no new
  `[defensive-unreachable]` allowlist entries).
- `docs/PLATFORM_DIRS_DOCS.md` Codex/Cursor rows reconciled to the
  now-correct behavior (the #29 amendment's "NOT produced on master"
  caveat is removed once true).

## 6. Scope boundaries

In scope: the command-layer invocation wiring + its tests + the doc
reconciliation. Out of scope: the producer (`internal/platform`
shared-target machinery — already complete), Codex hooks (already
correct), the Cursor `.claude/agents/` doc claim (separate
audit/follow-up), and any DI-pattern change (consume the existing
`Deps`/contract shape; do not pre-empt `di-refactor-rollout`).

## 7. Open questions

- **Q1 — entry-point set.** Reference wired refresh+install+add. Confirm
  no fourth production entry (e.g. a `sync` path) also needs it; the
  plan's task-1 investigation resolves this against current master.
- **Q2 — pr10 reconciliation.** Should this be tracked as a
  `pr10-branch-split` gap-closure (a missing slice the split owes) or a
  standalone plan? Proposed: standalone plan, cross-referenced from
  `pr10-branch-split` / `retire-old-branches` so closeout's diff-verify
  step explicitly accounts for it rather than re-flagging it.
