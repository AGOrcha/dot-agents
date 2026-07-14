# Managed-gitignore auto-fill (D14/R8 wiring)

## Problem
`internal/links/gitignore.go` implements the D14/R8 contract — a single idempotent
`# >>> dot-agents managed (project outputs) >>>` block in a consuming project's
`.gitignore` that carries every **materialized/generated** output (projected platform
links, generated platform configs, materialized asset units) plus the always-ignored
`.agentsrc.local.json` overlay, while **never** ignoring the committed
`.agentsrc.json`/`.agentsrc.lock` contract files.

But `EnsureManagedGitignore` has **zero production callers** — it is defined and unit-tested
only. Nothing collects per-platform outputs and calls it. Consequently generated per-machine
outputs (e.g. Copilot's `.github/hooks/*.json`) show up as untracked noise, which in #381 got
"fixed" with two **wrong ad-hoc root `.gitignore` rules**:
- `.github/hooks/*.json` — right intent, wrong place (belongs in the managed block).
- `.agentsrc.lock` — **contract violation**: it is `neverIgnored` (a committed lockfile,
  the `uv.lock` analog) and must stay tracked.

## Approach
1. **Per-platform output surface** — enumerate each enabled platform's generated/projected
   repo-relative outputs. Copilot: `.github/hooks/*.json`, `.github/copilot-instructions.md`,
   `.github/agents/*.agent.md`; Claude/Cursor/Codex/OpenCode analogous. Add a seam
   (`Platform` method) or reuse the existing link-intent/`resource_plan` collection rather than
   hardcoding paths in `refresh.go`.
2. **Wire into `da refresh`** — collect all enabled-platform outputs → one
   `EnsureManagedGitignore(repoRoot, outputs)` call. Idempotent, byte-stable, preserves
   user-authored ignores outside the markers (existing tests cover regeneration/dedup/sort/
   never-ignore-contract).
3. **Retire #381's ad-hoc rules** — remove both `.github/hooks/*.json` and `.agentsrc.lock`
   from the root `.gitignore`.
4. **Fix the `.agentsrc.lock` contract** — ensure it is not ignored and **commit** this repo's
   `.agentsrc.lock` (currently untracked).
5. **Correct the lesson** `classify-generated-files-before-cleanup` — its "lock files →
   gitignore" line is wrong; committed-contract lockfiles are TRACKED, only machine-specific
   generated *wiring* is ignored (via the managed block, not root rules).

## Design source
`config-distribution-model/design.md` §15 (D14/R8, R8 auto-fill). Contract + markers +
`alwaysIgnored`/`neverIgnored` sets: `internal/links/gitignore.go`. Copilot output generation:
`internal/platform/copilot.go` + `hooks.go` (`renderCopilotHookFile`, `createProjectHookFiles`).

## Verification
`go test ./internal/links/... ./internal/platform/... ./commands/...`; dogfood `da refresh` in a
temp project + this repo — managed block contains copilot `.github/hooks/*.json`, `git status`
clean with hooks ignored via the block, `.agentsrc.lock` tracked, refresh byte-stable on re-run.

## Provenance
Follow-up correction to #381's workspace-state PR (ad-hoc root `.gitignore` rules). Scheduled as
the first inner-loop task per the session decision to fix `.agentsrc.lock` inside the D14/R8 work
rather than a standalone revert.
