# Gotchas: Delegation Lifecycle

Common failure points:

## Write Scope Discipline

- Declare write scope at fanout time and treat it as immutable. Changing scope informally later defeats the conflict model.
- Prefer directory paths such as `commands/` rather than ad-hoc file globs for common cases. The overlap checks are built around prefix containment.
- Pre-validating scope (file-exists-on-HEAD, caller walk, coverage-delta) is the canonical pre-fanout gate — see **§ 0** of `instructions/workflow.md` in this skill. A bundle authored from stale task notes spawns on shipped work or forces a fold-back; § 0 is the hard gate that prevents it.

## Bundle Drift

- TASKS.yaml entries (`status`, `notes`, `write_scope`) are write-time snapshots; they decay as the tree moves. The cross-check-vs-shipped-PRs step is **§ 0a** of the canonical pre-fanout gate (`instructions/workflow.md`).
- If every task that `depends_on: <X>` is marked completed, then X is almost certainly status-stale; verify by PR search, then closeout.

## Worker Profile Selection

- `loop-worker` requires a delegation bundle and will REFUSE to self-select work or run an orchestrator-shaped multi-tier PR flow. Use it ONLY when a bundle exists.
- For cross-cutting hygiene work without a canonical task (e.g. fix N Sonar issues across M files), use `general-purpose` instead. The bounded-stage contract is what makes parallel `loop-worker` fanout safe — do not try to brief a `loop-worker` into acting like an orchestrator.

## Closeout And Status Advancement

- `workflow merge-back` does NOT advance task status. It writes the merge-back artifact and marks the delegation `merge-back-complete`. Status stays `in_progress` until the parent runs closeout.
- For delegated work: `workflow delegation closeout --decision accept` advances status AND archives delegation artifacts in one step. Do NOT also call `workflow advance`.
- For direct (non-delegated) work: `workflow advance` is correct, AND consider using `workflow contract create --direct` upfront so the work still has a closeout audit trail.
- Orphaned delegations remain active until explicitly resolved. If a sub-agent stops without merge-back, clean up the delegation (`workflow delegation closeout --decision reject` or hand-edit, depending on the project's process) rather than leaving stale active state behind.

## Verifier Boundary

- If the project registers a verifier_profile (e.g. `pr-ci`) in `.agentsrc.json`, the impl worker should exit at merge-back and let the verifier own the PR-CI-watch loop. The worker polling CI in parallel wastes context and races the verifier's auto-fixes.
- If NO verifier_profile is registered, the impl worker owns the loop end-to-end (fallback mode). Don't half-do it — own the loop until the PR is genuinely review-ready, or escalate via `pending_intent`.

## Coordination Drift

- The system records scope and status, but it cannot enforce write discipline at edit time. Sub-agents still need to honor the scope contract.
- If a sub-agent needs parent attention, set `pending_intent` on the contract. Do not rely on the parent to infer the need from prose alone.

## Canonical Paths

- Plans live under `.agents/workflow/plans/<plan-id>/` (`PLAN.yaml` + `TASKS.yaml` + `<plan-id>.plan.md`). Never `.agents/active/` — that is reserved for transient runtime artifacts (delegation bundles, merge-back, fold-back, verification, iteration logs).
- Bundle path: `.agents/active/delegation-bundles/<delegation_id>.yaml`. Contract path: `.agents/active/delegation/<task-id>.yaml`. Merge-back path: `.agents/active/merge-back/<task-id>.md`. Verifier output: `.agents/active/verification/<task-id>/<profile>.result.yaml`.

## Worktree Discipline

- Provision delegated slices with `da worktree create` (managed sub-branch + recorded base), never raw `git worktree add` / `git branch`; integrate with `da worktree merge-back --onto <parent>`, never a hand-run `git merge` / `git merge-base`. The recorded base is authoritative — `merge-back` catches a parent that advanced/force-pushed (`ErrStaleBase`) instead of silently rebasing onto the wrong commit.
- Each `da worktree create` slice has its OWN isolated index, so a worker's commits land only on its slice branch. If a single session nonetheless touches multiple worktrees, use `git -C /abs/path <cmd>` — never `cd` to a worktree (`cd` persists `pwd` across subsequent Bash calls and silently lands commits/branches/pushes in the wrong one). For build/test that needs cwd inside a worktree, use a subshell: `(cd "$WT" && go test ./...)`.
