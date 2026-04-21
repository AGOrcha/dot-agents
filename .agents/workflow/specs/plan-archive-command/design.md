# Plan Archive Command — Design Spec

**Status:** active  
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
4. Extend `workflow health` to show pending-archive count.
5. Extend sweep to propose and apply bulk archival.
6. Keep the operation safe: guard against accidental archival of active work, support dry-run,
   handle the existing history-dir-already-populated case correctly with content-aware merging.

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

**Decision:** when `history/<id>/` already exists, use a content-aware merge per file:

**File classification determines merge behavior:**

| File class | Examples | Rule |
|---|---|---|
| DMA artifacts | `delegate-merge-back-archive/**` | Always skip — delegation artifacts are authoritative, never overwrite |
| Canonical plan files | `PLAN.yaml`, `TASKS.yaml`, `<id>.plan.md` | Always overwrite — these carry final canonical state |
| Other files | loose docs, research notes | Time + hash check: overwrite if source is newer OR sha256 differs; skip if identical |

**Time + hash check for other files:**
1. Compute `sha256.Sum256` of the source file.
2. If no file exists in history → copy.
3. If file exists in history and sha256 matches → skip (identical content).
4. If file exists in history and sha256 differs → compare `mtime`. If source is newer → overwrite.
   If history copy is newer → skip and warn ("history copy of `<file>` is newer than source — skipped").

This makes the merge idempotent: re-running archive after a partial failure produces the same
result without re-copying unchanged files.

**Fast path when `history/<id>/` does not exist:**
`os.Rename` the entire directory (atomic on same filesystem). Then open and stamp `PLAN.yaml`
`status: archived` + `updated_at: <now>` in the new location.

**Failure recovery (§3.3a):**
If `os.RemoveAll` of the source directory fails after a successful merge, retry once
automatically in code before surfacing the error. Because the merge is idempotent (hash check
prevents redundant copies), a re-run after a failed removal is safe. Error output must make
this explicit: `"archive merge complete but source removal failed — re-run to clean up"`.

### 3.4 Status stamping

**Decision:** stamp `status: archived` and `updated_at: <now>` in `PLAN.yaml` before executing
the file move. This ensures the PLAN.yaml that lands in history reflects the terminal state,
not the pre-archive state, regardless of which merge path is taken.

### 3.5 Guard: require completed status

**Decision:** `workflow plan archive` refuses to archive a plan unless its status is `completed`.
`--force` bypasses the guard for exceptional cases (e.g., archiving a `paused` plan or a
no-status plan like `typescript-port`).

**Rationale:** the guard prevents accidentally archiving work that is still in progress. Agents
operating autonomously during orient/next cycles should not be able to inadvertently trigger
archival of an active plan via sweep unless the plan is provably done.

### 3.6 Dry-run support

**Decision:** the global `-n` / `--dry-run` flag must work. Dry-run prints the per-file merge
plan (copy / overwrite / skip / skip-dma / skip-newer) for each file without touching the
filesystem.

### 3.7 Bulk archive via `--plan <id1>,<id2>`

**Decision:** `--plan` accepts a comma-separated list of plan IDs, matching the convention of
`workflow complete --plan`. Each plan is archived in sequence; a failure on one plan is logged
and the command continues with the next (no early exit). Final output summarizes per-plan
results.

### 3.8 `workflow health` completed-plan count

**Decision:** `workflow health` adds a `"completed plans pending archive: N"` line when N > 0.
This is a hygiene signal, not a warning that blocks other work.

**Future — auto-archive readiness signal (deferred):** what "ready to archive" means beyond
`status == completed` (e.g., impl-results present, verification passed, no open fold-backs)
needs explicit definition before health can drive an auto-archive trigger. Deferred to a
follow-on spec.

### 3.9 Scope of `workflow drift` extension

**Decision:** add `CompletedPlanIDs []string` to `RepoDriftReport`. Drift populates this with
plan IDs from `workflow/plans/` where `status == "completed"`. A plan where `status == "archived"`
still exists in `workflow/plans/` is flagged separately as `InconsistentArchivedPlanIDs []string`
(error-level — archive was interrupted).

Drift does NOT auto-archive. It only surfaces the signal.

### 3.10 Scope of `workflow sweep` extension

**Decision:** add `SweepActionArchiveCompletedPlans` sweep action type. `planSweep()` emits one
action per completed plan ID from the drift report. `applySweepAction()` calls the archive
handler (reusing §3.3 logic). `RequiresConfirmation: true` — the sweep confirm loop prompts per
plan unless `--yes`. A failed archive for one plan is logged; sweep continues with the next.

### 3.11 No automatic archival on task completion

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
dot-agents workflow plan archive --plan <id1>,<id2>,<id3>
dot-agents workflow plan archive --plan <id> --force
dot-agents --dry-run workflow plan archive --plan <id1>,<id2>
```

Flags:
- `--plan` (required): one or more plan IDs, comma-separated
- `--force`: bypass completed-status guard (applied per plan in bulk mode)

### 4.2 Archive handler behavior (per plan)

1. Load `PLAN.yaml` from `workflow/plans/<id>/`. Error if not found.
2. Check `status == "completed"`. If not: log error with hint, skip this plan (in bulk mode);
   return error (in single mode). `--force` bypasses this check.
3. Stamp `status = "archived"`, `updated_at = now()`. Save in-place (still in workflow/plans/).
4. Determine path:
   - `history/<id>/` absent → `os.Rename` then done.
   - `history/<id>/` exists → content-aware merge (§3.3).
5. Attempt `os.RemoveAll` on source. On failure → retry once → if still failing, log and return
   partial-completion error.
6. Print per-plan summary: `Archived <id> → .agents/history/<id>/  (N copied, M overwritten, P skipped)`.

### 4.3 Drift detection requirements

- `RepoDriftReport` gains `CompletedPlanIDs []string` and `InconsistentArchivedPlanIDs []string`.
- Completed plans are a hygiene drift signal (informational).
- Archived-status plans still in `workflow/plans/` are an error-level drift signal.
- Human-readable drift output mentions both when non-zero.
- `--json` output includes both new fields.

### 4.4 `workflow health` requirements

- Add `completed_plans_pending_archive int` to health JSON output.
- Add `"completed plans pending archive: N"` to human-readable output when N > 0.
- This does not change the overall health status (healthy/partial/degraded) — it is informational.

### 4.5 Sweep requirements

- New sweep action type for archival.
- Sweep dry-run (default) lists plans that would be archived.
- Sweep `--apply` prompts per plan (unless `--yes`), then archives.
- Sweep log records each archive action outcome (applied / skipped / failed).
- If archive fails for one plan, sweep continues with the next (no early exit).

### 4.6 Reusable helpers

- `historyBaseDir(projectPath string) string` added to `state.go` alongside `plansBaseDir`.
- `copyWorkflowArtifact` from `delegation.go` reused for individual file copy.
- New `mergeWorkflowPlanDir` function (in `plan_task.go` or new `archive.go`) owns the
  content-aware merge walk — does NOT reuse `copyWorkflowDir` directly since it needs
  per-file classification and hash logic.

---

## 5. Special Cases

### 5.1 `typescript-port` (no status, empty TASKS.yaml)

Not a zombie — intentionally kept alive as an ongoing tracking plan for the Go port of the
TypeScript source. Treat as: drift flags `status-missing`; `--force` is required to archive it.
It should retain its `workflow/plans/` directory until the port work is formally scoped.

### 5.2 `isp-prompt-orchestrator.plan.md` in `.agents/active/`

A stale loose plan file created by an agent following an outdated instruction (no matching
`workflow/plans/<id>/` directory). Should be cleaned up by giving it a proper history entry:
create `history/isp-prompt-orchestrator/` with a minimal PLAN.yaml, TASKS.yaml, and impl-results
capturing what was done. Work criteria was not large — this is a simple cleanup task, not in
scope for this spec. Track separately.

---

## 6. Out of Scope

- Automatic archival on last-task completion (requires defining "ready" — deferred).
- Auto-archive trigger from `workflow health` (deferred — needs readiness definition).
- Restoring an archived plan from history back into `workflow/plans/` (unarchive).
- Archiving plans across multiple repos in one invocation (sweep handles cross-repo).
- Any schema changes — `archived` is already a valid PLAN.yaml status.
- `isp-prompt-orchestrator.plan.md` cleanup (tracked separately, §5.2).

---

## 7. Done Criteria

| Criterion | Verifiable by |
|-----------|---------------|
| `workflow plan archive --plan graph-bridge-command-readiness` stamps archived, merges with existing history entry (PLAN+TASKS overwritten, DMA untouched), removes source dir | smoke |
| `workflow plan archive --plan ci-smoke-suite-hardening,error-message-compliance` archives both in sequence, reports per-plan summary | smoke |
| `workflow plan archive --plan planner-evidence-backed-write-scope` (status: active) returns error with hint; `--force` succeeds | unit test |
| `--dry-run` prints per-file merge plan (copy/overwrite/skip/skip-dma) without touching filesystem | unit test |
| Identical files (same sha256) are skipped; differing files are overwritten if source is newer | unit test |
| Partial merge + failed RemoveAll → retry once → produces correct error message | unit test |
| `workflow drift` JSON includes `completed_plan_ids` and `inconsistent_archived_plan_ids` | unit test |
| `workflow health` human output shows `"completed plans pending archive: 7"` on current fixture | unit test |
| `workflow sweep` dry-run lists the 7 completed plans as archive candidates | smoke |
| `workflow sweep --apply --yes` archives all 7, leaving only 3 active/paused plans in `workflow/plans/` | smoke |
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
