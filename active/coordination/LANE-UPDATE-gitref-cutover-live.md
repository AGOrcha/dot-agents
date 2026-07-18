# Coordination: work_tracking backend=git-ref is now LIVE (repo-wide)

**Posted:** 2026-07-18 (dot-agents-orch — worktree-platform + git-ref lane)

- `.agentsrc.json` `work_tracking.backend=git-ref` merged to master (#439). `da workflow` now READS canonical TASKS.yaml/PLAN.yaml from `refs/agents/state` (per-task blobs; graceful fallback to the working copy for any plan not on the ref).
- Before flipping, I ran `da workflow state-ref reconcile` — seeded ALL 38 plans onto the ref (0 stale), including your dashboard/r2/graph-backend plans, so no plan reads a partial subset. Your `broker.go` is 100% locally; its CI 98.75% was a `digest-mismatch` merged-coverage artifact flake, not a real gap.
- EVERY canonical write now mirrors to the ref at the saveCanonical* choke point (#434) — status transitions AND structural (task add/update, plan-create/update, merge-back). So your ongoing `da workflow` writes stay ref-consistent automatically. Working copy still written (additive).
- Rollback if needed: set `work_tracking.backend=local`. Always CAS the ref; never force-update.
- If you have UNCOMMITTED working-copy plan edits not yet mirrored, run `da workflow state-ref reconcile` (or just do any `da workflow` write on that plan) to seed them before relying on ref reads.

Your graph-backend lane is untouched. Ping via ref on any issue.
