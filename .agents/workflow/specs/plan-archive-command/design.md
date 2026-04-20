# Plan Archive Command — Design Spec

**Status:** draft  
**Written:** 2026-04-20  
**Plan:** plan-archive-command  
**Resource model baseline:** `.agents/proposals/resource-model-current-state.md`

---

## 1. Problem Statement

`.agents/workflow/plans/` accumulates completed plans indefinitely. The `archived` plan status
exists in the schema but has no behavioral effect — setting it via `workflow plan update` only
writes the field, it never moves any files. There is no command that transitions a plan from the
active workflow registry into the immutable history store.

As a result:
- Every `workflow orient`, `workflow plan`, and `workflow health` invocation scans all plans
  including fully-done ones. As of 2026-04-20: 7 of 11 plans are `completed`, 1 has no status.
- Agents re-read completed `PLAN.yaml` files on every orient cycle, burning tokens on resolved
  context.
- The "what are we actually working on?" signal is buried in noise.
- Archival has been done manually via git commits (98c719e, b0828cd, 87bce37) — a recurring
  human cost with no command surface to guide it.

---

## 2. Goals

1. Provide an explicit command that physically moves a completed plan from `workflow/plans/<id>/`
   into `history/<id>/`.
2. Make the `archived` status mean something: a plan marked `archived` must no longer exist in
   `workflow/plans/`.
3. Extend drift detection to surface completed-but-not-archived plans as a hygiene signal.
4. Extend sweep to propose and apply bulk archival.
5. Keep the operation safe: guard against accidental archival of active work, support dry-run,
   handle the existing history-dir-already-populated case correctly.

---

## 3. Decisions

### 3.1 Dedicated command, not a side-effect of `plan update`

**Decision:** `workflow plan archive --plan <id>` is a standalone command. Setting
`--status archived` via `plan update` does NOT trigger file movement.

**Rationale:** `plan update` is a pure metadata editor with no filesystem side effects. Adding
a conditional directory move would: (a) change its contract, (b) make dry-run semantics
ambiguous, (c) cause confusing partial-failure states if the merge has conflicts. The closest
precedent is `delegation closeout` — a dedicated command for a state transition + file move.

### 3.2 Status semantics post-implementation

| Status    | Location expected         | Meaning                                                |
|-----------|---------------------------|--------------------------------------------------------|
| completed | workflow/plans/<id>/      | All tasks done; awaiting explicit archive              |
| archived  | history/<id>/PLAN.yaml    | Moved to history; final state recorded                 |
| archived  | workflow/plans/<id>/      | Inconsistent — drift must flag, sweep must fix         |

`completed` is an intentional holding state. An agent or human may still reference the plan's
TASKS.yaml, impl-results, or spec links while deciding whether closure is truly final. Archive
is an explicit act, not an automatic consequence of task completion.

### 3.3 Merge strategy when history dir already exists

Delegation closeout (`workflow delegation closeout`) writes artifacts into
`history/<id>/delegate-merge-back-archive/` before the plan itself is ever archived. So for
most plans the history directory pre-exists when archive runs.

**Decision:** when `history/<id>/` already exists:
- Walk and copy each file from `workflow/plans/<id>/` to `history/<id>/`.
- **Skip** any file that already exists in history — delegation artifacts are authoritative and
  must not be overwritten.
- **Exception:** always overwrite `PLAN.yaml` and `TASKS.yaml`. These carry the final canonical
  state and the history copies may be stale snapshots from an earlier delegation closeout.
- After all files are transferred, `os.RemoveAll` the source workflow plan directory.

When `history/<id>/` does not exist: `os.Rename` the entire directory (fast, atomic on same
filesystem). Then update `PLAN.yaml` status to `archived` in the new location.

### 3.4 Status stamping

**Decision:** stamp `status: archived` and `updated_at: <now>` in `PLAN.yaml` before moving
files. This ensures the PLAN.yaml that lands in history reflects the terminal state, not the
pre-archive state.

### 3.5 Guard: require completed status

**Decision:** `workflow plan archive` refuses to archive a plan unless its status is `completed`.
`--force` bypasses the guard for exceptional cases (e.g., archiving a `paused` or no-status plan
like `typescript-port`).

**Rationale:** the guard prevents accidentally archiving work that is still in progress. Agents
operating autonomously during orient/next cycles should not be able to inadvertently trigger
archival of an active plan via sweep unless the plan is provably done.

### 3.6 Dry-run support

**Decision:** the global `-n` / `--dry-run` flag must work. Dry-run prints the merge plan
(which files would be moved, which would be skipped, source and destination paths) without
touching the filesystem.

### 3.7 Scope of `workflow drift` extension

**Decision:** add `CompletedPlanIDs []string` to `RepoDriftReport`. Drift populates this with
plan IDs from `workflow/plans/` where `status == "completed"`. A plan where `status == "archived"`
still exists in `workflow/plans/` is also flagged as inconsistent (separate field or same list
with a label).

Drift does NOT auto-archive. It only surfaces the signal.

### 3.8 Scope of `workflow sweep` extension

**Decision:** add `SweepActionArchiveCompletedPlans` sweep action type. `planSweep()` emits one
action per completed plan ID from the drift report. `applySweepAction()` calls the archive
handler. `RequiresConfirmation: true` — the sweep confirm loop prompts per plan unless `--yes`.

### 3.9 No automatic archival on task completion

**Decision:** completing all tasks in a plan (via `workflow advance`) does NOT automatically
trigger archival. The plan stays in `workflow/plans/` with `status: completed` until an explicit
`workflow plan archive` or `workflow sweep --apply` runs.

**Rationale:** an agent finishing the last task may not be the right moment to archive — there
may be open impl-results to write, verification to run, or follow-on decisions to make. Explicit
intent is required.

---

## 4. Requirements

### 4.1 Command surface

```
dot-agents workflow plan archive --plan <id>
dot-agents workflow plan archive --plan <id> --force
dot-agents --dry-run workflow plan archive --plan <id>
```

Flags:
- `--plan` (required): canonical plan ID to archive
- `--force`: bypass completed-status guard

Future (not in scope): `--plan <id1>,<id2>` comma-separated bulk archive, matching `workflow
complete` convention.

### 4.2 Archive handler behavior

1. Load `PLAN.yaml` from `workflow/plans/<id>/`. Error if not found.
2. Check `status == "completed"`. If not: return error with hint to use `--force` or
   `workflow plan update <id> --status completed` first.
3. Stamp `status = "archived"`, `updated_at = now()`. Save in-place (still in workflow/plans/).
4. Determine merge path vs rename path (does `history/<id>/` exist?).
5. Execute move. See §3.3 for merge strategy.
6. Print summary: `Archived <id> → .agents/history/<id>/  (N files moved, M skipped)`.

### 4.3 Drift detection requirements

- `RepoDriftReport` gains `CompletedPlanIDs []string`.
- A completed plan lingering in `workflow/plans/` is a drift signal, not an error.
- A plan with `status: archived` still in `workflow/plans/` is an error-level drift signal
  (inconsistent state — archive was interrupted or partially applied).
- Drift human-readable output must mention completed plan count when non-zero.
- `--json` output must include the new field.

### 4.4 Sweep requirements

- New sweep action type for archival.
- Sweep dry-run (default) lists plans that would be archived.
- Sweep `--apply` prompts per plan (unless `--yes`), then archives.
- Sweep log records each archive action outcome (applied / skipped / failed).
- If archive fails for one plan, sweep continues with the next (no early exit).

### 4.5 Reusable helpers

- `historyBaseDir(projectPath string) string` added to `state.go` alongside `plansBaseDir`.
- `copyWorkflowDir` and `copyWorkflowArtifact` from `delegation.go` are reused as-is.

---

## 5. Out of Scope

- Automatic archival on last-task completion.
- Bulk `--plan <id1>,<id2>` flag (defer; add when there's a concrete use case beyond the 7
  currently stuck plans, which sweep handles).
- Restoring an archived plan from history back into `workflow/plans/` (unarchive).
- Archiving plans across multiple repos in one invocation (that's sweep's job).
- Any schema changes — `archived` is already a valid PLAN.yaml status.

---

## 6. Open Questions

**Q1. Should `workflow health` summary include completed-plan count?**
Today health shows active task counts and checkpoint staleness. Adding a "N completed plans
pending archive" line would surface the signal without requiring a separate drift invocation.
_Lean yes — low cost, directly actionable._

**Q2. How should typescript-port (no status field) be treated?**
It has no status in PLAN.yaml, 0/0 tasks, and a history entry with DMA content. It is
effectively a zombie. Should drift flag it as "unknown status" separately from "completed"?
Should archive accept it with `--force`?
_Proposed: drift flags it as `status-missing` and suggests `--force` archival or manual update._

**Q3. What about isp-prompt-orchestrator.plan.md in `.agents/active/`?**
This is a stale loose plan file in `active/` with no matching `workflow/plans/<id>/` directory.
The lesson `archive-completed-active-plans` covers this pattern, but there is no command that
handles it. Is this in scope for the archive command, or a separate cleanup operation?
_Proposed: out of scope for this spec. `workflow sweep` could add a loose-plan-file sweep action
separately._

**Q4. Should the merge strategy overwrite `.plan.md` narrative files?**
PLAN.yaml and TASKS.yaml are explicitly overwritten (final canonical state). What about
`<id>.plan.md`? History may already have a copy from an earlier manual archive. The workflow/plans
copy may be more recent.
_Proposed: treat `.plan.md` the same as PLAN.yaml — always overwrite. It is a companion to
PLAN.yaml, not a delegation artifact._

**Q5. What happens if `os.RemoveAll` fails after a partial merge?**
The source directory would be partially empty, with some files moved and some remaining.
Subsequent re-runs would see the remaining files as "would be skipped" (already in history).
Is this acceptable, or do we need a rollback / transaction mechanism?
_Proposed: acceptable for now. The merge strategy is idempotent — a re-run will move remaining
files. Document this in error output: "archive partially applied — re-run to complete."_

**Q6. Should bulk `--plan a,b,c` be in the first implementation or deferred?**
Sweep already handles bulk archival. The one-by-one command is sufficient for explicit use.
_Proposed: defer. Implement after sweep is wired._

---

## 7. Done Criteria

| Criterion | Verifiable by |
|-----------|---------------|
| `workflow plan archive --plan graph-bridge-command-readiness` moves all 3 files to `history/`, merges with existing history entry, removes source dir | manual smoke |
| `workflow plan archive --plan planner-evidence-backed-write-scope` (status: active) returns error with helpful hint | unit test |
| `workflow plan archive --plan <id> --force` archives a non-completed plan | unit test |
| `--dry-run` prints merge plan without touching filesystem | unit test |
| `workflow drift` JSON includes `completed_plan_ids` with 7 entries on current fixture | unit test |
| `workflow sweep` (dry-run) lists the 7 completed plans as archive candidates | smoke |
| `workflow sweep --apply` with `--yes` archives all 7, leaving only 3 plans in `workflow/plans/` | smoke |
| `go test ./...` passes | CI |

---

## 8. Relationship to Other Work

- **plan-archive-command plan**: implementation task breakdown, dependency ordering, write scopes.
  This spec is the "what and why"; the plan is the "how and in what order".
- **resource-model-current-state.md**: the system snapshot this spec was written against.
- **archive-completed-active-plans lesson**: documents the manual pattern this command automates.
- **delegation closeout**: the closest behavioral precedent — dedicated command for state
  transition + archive move. Same pattern, plan-level scope.
- **completed-plan-audit-analysis spec**: audits whether completed plans *should* be archived.
  That spec answers "is it really done?" before this command answers "move it to history".
