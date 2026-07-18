# Workflow-state ref backend — CUTOVER COMPLETE (dot-agents)

**Status (2026-07-18):** `git-ref` is the ACTIVE work-tracking backend for this repo. The cutover is done, re-validated with structural writes, and user-authorized. The earlier rollback (structural-write clobber) is fixed.

## Active configuration

`.agentsrc.json`: `"work_tracking": { "backend": "git-ref" }`

- **Reads** project canonical `TASKS.yaml`/`PLAN.yaml` from `refs/agents/state` (per-task blobs); graceful fallback to the working copy for any plan not on the ref. Reading the LOCAL ref is read-your-writes safe.
- **Writes**: every canonical write — status transitions AND structural writes (`task add`/`update`, plan-create/update, merge-back) — mirrors to the ref at the `saveCanonicalTasks`/`saveCanonicalPlan` choke point (#434). The working copy is still written (additive) for projection fidelity + rollback.
- **Commits**: `da workflow commit` does not stage `.agents/workflow/**` plan-state into code-branch commits (it lives on the ref); `.agents/history/**` + explicit `--include` still stage.
- **Surfacing**: `da workflow status` → `backend: git-ref` + `coordination SOT: refs/agents/state (live)`.

## How the cutover was made safe
1. **#434 choke-point mirror** — structural writes now mirror (fixed the clobber that forced the first rollback).
2. **`da workflow state-ref reconcile`** (#435) — seeded ALL 38 plans onto the ref (0 stale) so no plan reads a partial subset.
3. **Re-validated**: two sequential structural `task update`s under git-ref both survive (no clobber); projection == working copy; rollback (set `backend=local`) returns to working-copy reads with no data loss.

## Rollback
Set `work_tracking.backend` back to `local` (or unset). Reads return to the always-current working copy; the ref remains a durable audit trail. No data migration.

## Coordination
`refs/agents/state` is SHARED with a co-located session (`active/coordination/`). Always CAS; never force-update. The reconcile + #434 keep every session's writes ref-consistent.

## Follow-on
`ref-back-managed-artifacts` (mirror `.agents/active` delegation/merge-back/verification/review artifacts to the ref) + `proposal-scope-flag` (Proposal scope field routing into diff-scopes/sources) extend coverage beyond plan coordination state.
