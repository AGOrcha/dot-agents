# config-transitive-layering loop — operating runbook

**Read this before touching the loop.** It exists because the iteration-log
machinery silently mislabels iterations under shared active-state (see the bug
below). A fresh session that starts in the main tree will re-discover the bug or,
worse, checkpoint poisoned records into master's `.agents/active/`.

## Where the loop runs
- **Worktree:** `/tmp/ctl-loop` — branch `loop/config-transitive-layering`, forked
  off `origin/master`. Do the work HERE, not in `~/proj-docs/dot-agents` (that tree
  is on a stale docs branch with divergent iter-logs and other sessions' untracked
  state).
- **Dev binary:** `/tmp/ctl-loop/da-dev` (`go build -o da-dev ./cmd/da`). Untracked;
  rebuild if stale. Invoke as `./da-dev …` from the worktree.
- **Held-aside foreign state:** `/tmp/ctl-held/` holds another session's active
  delegation (`t2-materialize-store.yaml` + its bundle) that was moved OUT of
  `.agents/active/delegation/` in this worktree. **It MUST stay out of the scan
  dir.** The removal is an *unstaged* deletion in this worktree (never commit it);
  master and the other session's worktree still have their copies.

## The bug this loop works around
`scanActiveDelegationContract` → `firstReadableDelegationContract`
(`commands/workflow/iter_log.go:220-248`) is **plan-unscoped**: `checkpoint
--log-to-iter N` derives `wave`/`task_id`/`feedback_goal` from the *first* active
delegation contract in the shared, committed `.agents/active/delegation/`,
regardless of plan. With a foreign contract present it stamps THIS plan's iter-log
with the OTHER plan's focus. Fold-back: `iterlog-scoops-foreign-plan-delegation`
(routed to `full-loop-orchestration-runtime`). Fix = scope the scan to the loop's
active plan.

With the foreign contract held aside and this being **direct** (non-delegated)
work, the scan now returns empty, so the checkpoint writes empty `wave`/`task_id`
— which is why step 5 below patches the iter-log.

## Per-iteration procedure (direct work)
1. Pick the next eligible task: `./da-dev workflow eligible --plan config-transitive-layering`.
2. Implement strictly within the task's `write_scope`. Run focused + package tests
   (`go test ./internal/config/...`), `gofmt -l`, `go vet`. Commit the CODE first.
3. Stop-gate sentinel: `./da-dev workflow hook-sentinel write iteration-close
   --run-id ctl-iterN --plan config-transitive-layering --task <id> --agent-type main --write-scope <each file>`.
4. `./da-dev workflow verify record --kind test --status pass --command "…" --summary "…"`
   — **no `--task`** (direct work has no delegation contract; `--task`/`--kind review`
   require one and will error).
5. `./da-dev workflow checkpoint --log-to-iter N --role impl --verification-status pass --verification-summary "…" --message "…"`,
   then **PATCH `.agents/active/iteration-log/iter-N.yaml`**: set `wave: config-transitive-layering`,
   `task_id: <id>`, `impl.item/summary/scope_note/feedback_goal`, `files_changed`/`lines_*`
   to the code commit's stat, and a `verifiers:` entry `{type: unit, status: pass, summary: …}`.
   (Empty stubs — like the shipped iter-70 — are also acceptable; the point is
   correct `wave`/`task_id`/`commit`, never a foreign plan's.)
6. `./da-dev workflow advance config-transitive-layering --task <id> --status completed`.
7. **Obs sync:** `./da-dev score run` (writes iter-N.score.yaml + publishes
   `iteration.scored`) then `./da-dev kg warm --type session`. Checkpoint already
   published `iteration.checkpointed`.
8. Commit the workflow state (iter-N.yaml + TASKS.yaml). `git push`.

## Task spine + progress
1. ✅ `org-config-spec-transitive-scope` (iter-71, `6532f487`) — spec.
2. ✅ `config-source-scope-schema` (iter-72, `e7a0b6a4`) — Source.Scope/Owner.
3. ✅ `config-transitive-extends-resolver` (iter-73, `14dad8a0`) — CORE done.
   `resolveExtendsGraph` + `walk`: recursive children-first transitive extends
   (org→team→repo-local); ref+digest dedupe (keep-first/baseline; fail-loud on a
   divergent digest); cycle detect (`ReasonCycle`); full transitive locking. 5
   tests (post-order, diamond dedupe, cycle, digest-conflict, offline-lock). The
   flat concurrent path (`extendsResult`/`resolveConcurrency`) was removed; flat
   behavior is preserved.
4. ✅ `layered-consumers-relevance-verify` (iter-74, `b3df0626`) — DONE. The
   consumer migration off `loadFlatSnapshot` already shipped in config-v2
   (`runRelevance`→`resolveLayered`, `ResolvePreconditionPolicy`→`ResolveLocked`);
   task 3 supplied transitivity. The task-4 fix: the OFFLINE replay
   (`ResolveLocked`→`readLockedExtends`) only walked the repo's DIRECT extends,
   dropping transitively-locked layers — so relevance/verify still showed
   `matched:false` for org-through-team app_types. Made `readLockedExtends`
   transitive (`lockedExtendsState`, mirrors the online walk). Proving test +
   ResolveLocked-transitive + online/offline diamond parity all green. Live smoke:
   `config explain` shows the org skill inherited via team→org; install/refresh
   idempotent + collision-free (the flagged link-collision did NOT surface).
   Adjacent finding: `extends-inherited-skill-not-projected` — a config-inherited
   skill resolved but was not platform-projected. Per owner this was the plan's
   ORIGINAL intent (org resources must reach the repo), so it became task 5 and the
   fold-back was consumed.
5. ✅ `extends-inherited-resource-projection` (iter-75, `bf2d1442`) — DONE. Install
   linked `rc.Skills`/`rc.Agents` from the FLAT manifest, so an extends-supplied
   (org→team) skill/agent resolved but never materialized. Fix: `runInstall` links
   from `ensureRes.Snapshot.Effective` (resolved/union-merged/transitive) with a
   `--dry-run` nil-guard; `linkInstallResources` always searches the canonical home
   (`~/.agents`) so a home-canonical resource resolves even with declared sources.
   `refresh` needs no change (re-projects from the canonical bucket install fills).
   Tests: RunInstall projects an extends-inherited skill + dry-run guard; live smoke
   projects org-skill and survives refresh. Consumed fold-back moved to
   `.agents/history/config-transitive-layering/fold-backs/`.

## Status: 5/5 tasks done — plan STAYS OPEN (owner direction: do NOT archive)
All five tasks completed (iters 71–75) on `loop/config-transitive-layering`. The owner
kept the plan open — more scope may remain: agents/rules/commands projection parity
beyond skills+agents; org-SOURCE-fetched resource bytes (not just home-canonical);
projection of other resource types. Do NOT run `da workflow plan archive`.
Downstream (after merge, in their own repos): unpause `org-layer-buildout`,
`team-config-revamp`, and payout `platform-config-layering` adopt tasks via
`da workflow plan update <id> --status active`. Restore the held-aside foreign
delegation (`/tmp/ctl-held` → `.agents/active/delegation/`) if this worktree is
reused (moved in-worktree only; master untouched).
