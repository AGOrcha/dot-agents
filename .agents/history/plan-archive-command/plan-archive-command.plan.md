# Plan Archive Command — Canonical Plan

**Plan ID:** plan-archive-command  
**Status:** active  
**Spec:** [design.md](../../specs/plan-archive-command/design.md)

---

## Why this exists

`.agents/workflow/plans/` accumulates completed plans indefinitely. The `archived` plan status
exists in the schema but has zero behavioral effect — setting it via `workflow plan update` only
writes the field; no files move. This plan wires the `archived` status to an actual file move,
extends drift/health/sweep to surface and automate cleanup, and eliminates the recurring manual
git-commit pattern that has substituted for a proper archive command.

---

## Key decisions and invariants (do not reopen without a fold-back)

1. **Dedicated command, not a side-effect.** `workflow plan archive` is standalone. `plan update
   --status archived` never moves files. The precedent is `delegation closeout` — dedicated
   command for state transition + file move.

2. **`completed` is an intentional holding state.** Plans in `workflow/plans/` with
   `status: completed` are correct and expected. Archive is an explicit act, not automatic.

3. **Status stamp before move.** `PLAN.yaml` gets `status: archived` + `updated_at` written
   in-place BEFORE any file movement. The PLAN.yaml that lands in history reflects terminal state
   regardless of which merge path runs.

4. **Content-aware merge for pre-populated history dirs.** Three file classes:
   - DMA artifacts (`delegate-merge-back-archive/**`) → always skip
   - Canonical plan files (`PLAN.yaml`, `TASKS.yaml`, `<id>.plan.md`) → always overwrite
   - Evidence sidecars (`evidence/*.scope.yaml`) and other files → sha256+mtime check
   This is idempotent: re-running after a partial failure is safe.

5. **`fs.go` is the shared FS primitives layer.** `copyWorkflowArtifact` and `copyWorkflowDir`
   move from `delegation.go` into `fs.go` in p0. All plan lifecycle operations import from there.
   Never add archive-specific FS logic back into `delegation.go`.

6. **Bulk is sequential with per-plan failure isolation.** `--plan a,b,c` archives each in order.
   Failure on one plan is logged and skipped; the command continues and summarizes all outcomes.

---

## Task sequence

```
p0-extract-fs-helpers ─────────────────────────────────────────────────────────┐
p1-historybasedir-helper ─────────────────────────────────────────────────────┐ │
                                                                               ↓ ↓
                                                                    p2-archive-handler
                                                                         │
                                               ┌─────────────────────────┤
                                               ↓                         ↓
                                         p3-wire-cmd              p4-drift-extension
                                               │                         │
                                               ↓                         ↓
                                    p7-plan-schedule-cmd       p4b-health-extension
                                                                         │
                                                                         ↓
                                                                p5-sweep-extension
                                                                         │
                                                                         ↓
                                              p6-tests ←────────────────┘
                                              (also depends on p3-wire-cmd)
```

p0 and p1 have no dependencies — both can start immediately and run in parallel.

---

## Out of scope

- Automatic archival on last-task completion (requires defining "ready" — deferred)
- Auto-archive trigger from `workflow health`
- Unarchive / restore from history back to plans
- `isp-prompt-orchestrator.plan.md` cleanup (tracked separately — special case §5.2 in spec)

---

## Ralph Pipeline Notes

**Direct impact: low. Additive, no breaking changes.**

### Commands ralph can call after this plan lands

- `workflow plan archive --plan <id>` — ralph could incorporate this as a post-completion step
  after `workflow advance` marks the final task done. Currently no ralph cleanup step exists
  for completed plans; this fills that gap.
- `workflow sweep --apply --yes` — ralph could call sweep as a bulk cleanup pass at session end.
  The new `SweepActionArchiveCompletedPlans` action adds archive candidates to the sweep proposal
  list; `--yes` would auto-archive all without interactive prompts.

### JSON output changes ralph scripts may need to handle

- `workflow drift --json` gains `completed_plan_ids` and `inconsistent_archived_plan_ids` fields.
  Both are additive — existing ralph scripts parsing drift JSON will not break. Scripts that
  check drift for go/no-go decisions may want to act on `inconsistent_archived_plan_ids` (non-empty
  means a prior archive was interrupted — an error condition worth failing on in CI).
- `workflow health --json` gains `completed_plans_pending_archive` count. Additive. Ralph health
  checks that gate on `status == healthy` are unaffected.

### Insight: ralph post-plan cleanup hook

Ralph has no plan lifecycle cleanup today. A natural extension post-this-plan:
add a ralph cleanup step that calls `workflow plan archive --plan <completed-id>` after a plan's
final task is closed out. This would keep `workflow/plans/` clean automatically for plans that
go through the ralph pipeline. The archive command's `--force` flag is available for plans that
lack `completed` status but are functionally done.
