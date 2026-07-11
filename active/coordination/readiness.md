READY

# Readiness gate — wire-managed-gitignore-autofill (D14/R8)

Stage: readiness (`swarm-da-inner-loop-d14-readiness-0`). Base: `origin/master` @ `917908d4`
(local HEAD == origin/master, up-to-date after `git fetch origin master`).

## Reconciliation verdict: D14 TASK VALID AS WRITTEN — no rescoping

All four premises the task rests on are confirmed true against `origin/master` (917908d4):

1. **`EnsureManagedGitignore` is truly unwired.** Grep of `internal/` + `commands/` finds only
   the definition (`internal/links/gitignore.go`) and its unit tests
   (`internal/links/gitignore_test.go`). ZERO production callers. `commands/refresh.go` does not
   reference it.
2. **`config-v2-coherence` did NOT ship a competing ".gitignore auto-fill (§6.1)".** No `gitignore`/
   `auto-fill`/`6.1` matches in `.agents/workflow/specs/config-v2-coherence/design.md`, nor in any
   §6.1 heading there. `commands/config/` has no gitignore code. The task is NOT a duplicate.
3. **The only OTHER gitignore auto-fill is `EnsureProvenanceGitignore`** on `internal/config`
   `LocalSource` (D7 / §7A.5, marker `# >>> dot-agents managed (local source provenance) >>>`).
   This is the DISTINCT `~/.agents` local-source block — a separate, non-overlapping marker — and
   gitignore.go's own header comment already acknowledges the split. No conflict.
4. **`.agentsrc.lock` contract premise holds.** `.agentsrc.lock` is on disk but UNTRACKED
   (`git ls-files --error-unmatch` fails) — task item 4 (commit it) is valid. Note:
   `3c23cd3c fix(refresh): write refresh metadata to .agentsrc.lock, not .agentsrc.json` means
   `da refresh` already materializes the lock, reinforcing that it should be committed + never
   ignored.
5. **#381 ad-hoc root rules present.** Root `.gitignore` carries BOTH `.github/hooks/*.json`
   (line 138) and `.agentsrc.lock` (line 140) — the exact rules task item 3 retires.

`git log -- internal/links/gitignore.go` shows a single commit `d7286a56 feat(links): managed
.gitignore auto-fill for consuming projects` (definition-only landing, no wiring). Consistent with
"has ZERO production callers."

## KG blast-radius (da 0.4.2 `kg impact --json`, graph_state=ready)

- `internal/links/gitignore.go`: 8 nodes directly changed, 58 impacted (2 hops), 9 additional
  files. Direct nodes = the file + its funcs (`EnsureManagedGitignore`, `normalizeIgnoreEntries`,
  `joinManagedGitignore`, `readManagedGitignore`, …).
- `commands/refresh.go`: 18 nodes directly changed, 68 impacted (2 hops), 9 additional files
  (`NewRefreshCmd`, `announceRefreshProject`, `checkRefreshProjectPath`, `ensureLockFreshForRefresh`,
  …).
- Additional-files fan-out for both is the generic package-level closure (`cmd/da/main.go`,
  `commands/add.go`, `commands/add_test.go`, `cmd/da/main_test.go`, `cmd/globalflag-coverage/main.go`,
  plus non-code `.agents/history/test-file-structure/*` and `.agents/sandbox/*` scratch) — no
  surprising cross-subsystem coupling. Impl/verify should focus on `internal/links`,
  `internal/platform`, and `commands/refresh` per the write_scope.

## Worktree

- Path: `.agents/worktrees/d14`  (created OFF `origin/master` @ `917908d4`, `git -C`, no cd).
- Branch: `swarm/managed-gitignore-autofill` (tracks `origin/master`).
- `.venv` symlinked: `.agents/worktrees/d14/.venv -> /Users/nikashp/proj-docs/dot-agents/.venv`.

## Pointers

- Prior UNREVIEWED partial attempt (optional reference, 250 lines):
  `.agents/worktrees/_state/swarm-run/research/prior-d14-attempt.patch`.
- Canonical task: `.agents/workflow/plans/managed-gitignore-autofill/TASKS.yaml`
  → `wire-managed-gitignore-autofill` (notes = authoritative 5-item checklist).
- Design source: `.agents/workflow/specs/config-distribution-model/design.md` §15 (D14/R8).
