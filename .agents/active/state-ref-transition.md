# Workflow-state ref backend — cutover ROLLED BACK (dot-agents)

**Status (2026-07-18):** additive opt-in `write_to=state-ref` ACTIVE; the `backend=git-ref` READ cutover was attempted, validated for status reads, then **rolled back** after dogfooding exposed a structural-write clobber bug. Re-cutover is blocked on the choke-point mirror fix below.

## Active configuration

`.agentsrc.json`:

```json
"work_tracking": { "write_to": "state-ref" }
```

- Reads resolve from the **working copy** (read-your-writes safe for all ops).
- Writes: each status transition additively mirrors to `refs/agents/state` (CAS); working copy always written.
- `read_from: master` is NOT used (clobbers sequential same-checkout writes — `.agents/lessons/read-from-master-clobbers-sequential-writes/`).

## Why the git-ref read cutover was rolled back

`backend=git-ref` READS canonical state from the ref, but the mirror only fires on **status transitions** — structural writes (`task add`/`update`, plan-create, merge-back) go through `saveCanonicalTasks`/`saveCanonicalPlan` and write only the working copy, NOT the ref. So the ref goes stale for non-transition writes and sequential structural writes clobber each other via the stale-ref read (same read-your-writes class as `read_from=master`). See `.agents/lessons/git-ref-backend-structural-writes-must-mirror/`.

## Re-cutover gate (in addition to the original migration gate)

1. `git-ref-work-backend/ref-back-managed-artifacts` (or a dedicated fix) makes the **canonical-write choke point** (`saveCanonicalTasks`/`saveCanonicalPlan`) trigger the ref mirror under git-ref, so EVERY canonical write is immediately ref-visible.
2. Cutover validation re-run MUST exercise STRUCTURAL writes (add/update) under git-ref, not just status-transition reads.
3. Then flip `work_tracking.backend=git-ref` again; rollback stays a config revert to `write_to=state-ref`.

## Coordination
`refs/agents/state` is SHARED with a co-located session (`active/coordination/`). Always CAS; never force-update.
