DONE

# Impl stage — wire-managed-gitignore-autofill (D14/R8)

Stage: impl (`swarm-da-inner-loop-d14-impl-0`). Upstream: readiness=READY.
Worktree: `.agents/worktrees/d14`, branch `swarm/managed-gitignore-autofill`.

## Commit
- `2bba96819c020234792cf6c7e9ce7728252c6876`
  `feat(refresh): wire managed .gitignore auto-fill (D14/R8) + commit lock contract`
  (no AI trailer)

## Files changed
- `internal/platform/platform.go` — new `ManagedOutputReporter` interface,
  `staticManagedOutputs` table (cursor/claude/codex/opencode/antigravity), and
  `CollectManagedOutputs(platforms)` aggregator.
- `internal/platform/copilot.go` — `ManagedOutputs()` (dynamic copilot surface,
  incl. `.github/hooks/*.json`, `.github/copilot-instructions.md`,
  `.github/agents/`, `.vscode/mcp.json`, `.claude/settings.local.json`).
- `commands/refresh.go` — `+internal/links` import; new
  `ensureManagedGitignoreForRefresh(path, enabledPlatforms)` called from
  `refreshOneProject`; one `links.EnsureManagedGitignore` call per project,
  keyed off the config-enabled set, dry-run-safe.
- `commands/refresh_test.go` — `TestRunRefresh_WritesManagedGitignoreBlock`
  (drives runRefresh end-to-end) + `TestCollectManagedOutputs_CopilotDynamicAndStaticPlatforms`.
- `.gitignore` — removed BOTH #381 ad-hoc rules (`.github/hooks/*.json` and
  `.agentsrc.lock`) plus their comment block.
- `.agentsrc.lock` — NEW, committed (was untracked); generated via built
  `da config explain`, digests byte-identical to the on-disk lock.
- `.agents/lessons/classify-generated-files-before-cleanup/LESSON.md` + `index.md`
  — corrected the wrong "lock files -> gitignore" guidance.

## The 5 items
1. **Per-platform output surface** — `ManagedOutputReporter` seam +
   `CollectManagedOutputs`; copilot enumerates its own dynamic outputs, the
   static-surface platforms via a central table in `platform.go`. NO paths
   hardcoded in `refresh.go` (it only calls `platform.CollectManagedOutputs`).
2. **Wired into `da refresh`** — one `EnsureManagedGitignore` call per project
   in `refreshOneProject`; idempotent/byte-stable (verified across two refreshes),
   preserves user ignores outside the markers (verified).
3. **#381 rules retired** — both `.github/hooks/*.json` and `.agentsrc.lock`
   removed from root `.gitignore` (worktree file; `git check-ignore` confirms
   neither is ignored now).
4. **`.agentsrc.lock` committed** — now tracked; `neverIgnored` keeps it out of
   the managed block. Dogfood confirms it is NOT ignored after refresh.
5. **Lesson fixed** — committed-contract lockfiles are TRACKED; per-machine
   wiring is ignored via the da-managed block (never ad-hoc root rules); body +
   index row updated, config-vs-telemetry-vs-stale classification kept.

## Self-test results (in-worktree)
- `go build ./...` — clean.
- `go vet ./commands/... ./internal/...` — clean.
- `go test ./internal/links/... ./internal/platform/... ./commands/...` — ALL PASS
  (links ok, platform ok, commands ok + all commands/* subpackages ok).
- New tests run: `TestRunRefresh_WritesManagedGitignoreBlock` PASS,
  `TestCollectManagedOutputs_CopilotDynamicAndStaticPlatforms` PASS.
- Real-binary dogfood (built `bin/da`, temp git project, all platforms
  default-enabled): managed block written with `.github/hooks/*.json` inside,
  `.agentsrc.lock`/`.agentsrc.json` NOT ignored, user line `user-secret/`
  preserved above the markers, block sorted+deduped, second refresh byte-stable.

## Note for downstream stages
- Correcting my own tool slip: an initial `.gitignore` edit landed on the MAIN
  repo `.gitignore` (identical content → snapshot-tag collision resolved to cwd
  root). Reverted via `git checkout -- .gitignore` in the main repo (confirmed
  clean) and re-applied to the worktree file only. MAIN repo tree is untouched
  by this slice.
- Scope note: this repo's committed `.gitignore` keeps its hand-maintained
  "dot-agents specific" section; the managed block is written by `da refresh`
  at runtime. Item 3 only retired the two #381 rules, per the task.
