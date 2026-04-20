# Proposal: `workflow plan archive` Command

## Problem

`.agents/workflow/plans/` accumulates completed plans indefinitely. Right now 8 of 11 plans in
the repo are `completed` or `paused`, leaving only 2 active. Every orient, plan-list, and next
invocation scans all 11. Agents have been cleaning this up manually in git commits (see: 98c719e).

The `archived` status already exists in the PLAN.yaml schema (`draft|active|paused|completed|archived`)
but has no behavioral effect—it is just a field value. No command moves the directory.

The `delegation closeout` command is the closest precedent: it archives delegation artifacts into
`.agents/history/<plan-id>/delegate-merge-back-archive/…`. That pattern should be extended to
plan-level archival.

---

## Root Cause

`listCanonicalPlanIDs` (state.go:773) lists every subdirectory of `.agents/workflow/plans/` that
contains a `PLAN.yaml`—regardless of status. So `completed` and `archived` plans appear in:
- `workflow plan` (list)
- `workflow orient`
- `workflow health`
- `workflow complete --plan`

`selectNextCanonicalTask` (plan_task.go:874) already skips non-active plans, so task selection is
correct. But the directory noise is real, and agents re-read completed PLAN.yaml files on every
orient cycle.

---

## Proposed Solution

### 1. New command: `workflow plan archive --plan <id>`

A subcommand added alongside `plan create` and `plan update` in the `planCmd` subtree.

```
dot-agents workflow plan archive --plan graph-bridge-command-readiness
dot-agents workflow plan archive --plan platform-dir-unification --force
dot-agents --dry-run workflow plan archive --plan kg-command-surface-readiness
```

**Behavior:**

1. Load `PLAN.yaml` from `.agents/workflow/plans/<id>/`.
2. Guard: refuse unless `status` is `completed` (or `--force` to bypass).
3. Stamp `status: archived` and `updated_at: <now>` in PLAN.yaml before moving.
4. Merge `.agents/workflow/plans/<id>/` → `.agents/history/<id>/`:
   - If `history/<id>/` does not exist: `os.Rename()` (fast, atomic on same filesystem).
   - If `history/<id>/` already exists (delegation closeout may have pre-created it): walk
     workflow plan dir and copy each file/subdir that does not already exist in history.
     Files that already exist in history are **skipped** (delegation artifacts are authoritative).
     PLAN.yaml and TASKS.yaml are always overwritten (they carry more complete final state).
5. After successful merge, `os.RemoveAll` the source workflow plan directory.
6. Print summary: files archived, files skipped, destination path.

**Dry-run** (`-n`): prints the merge plan without touching the filesystem.

**Implementation location:**
- `commands/workflow/cmd.go` — add `planArchiveCmd` to `planCmd.AddCommand()`
- `commands/workflow/plan_task.go` — add `runWorkflowPlanArchive(planID string, force bool) error`
- Reuse `copyWorkflowDir` and `copyWorkflowArtifact` already in `delegation.go`
- Add `historyBaseDir(projectPath string) string` helper to `state.go` alongside `plansBaseDir`

### 2. Extend drift detection

Add to `RepoDriftReport` (drift.go):
```go
CompletedPlanIDs []string `json:"completed_plan_ids,omitempty"`
```

`detectRepoDrift` populates this by loading each PLAN.yaml and collecting IDs where
`status == "completed"`. A plan with `status == "archived"` still in workflow/plans/ is also
flagged (inconsistent state—archive was partial).

### 3. Extend sweep

Add sweep action type `SweepActionArchiveCompletedPlans`. When `--apply` is set, call
`runWorkflowPlanArchive` for each detected completed plan (with confirmation per plan unless `--yes`).
Sweep already has the confirm/skip/log pattern to copy.

---

## Semantic Clarification (status field meaning post-change)

| Status    | Location               | Meaning                                              |
|-----------|------------------------|------------------------------------------------------|
| completed | workflow/plans/<id>/   | All tasks done; awaiting explicit archive            |
| archived  | history/<id>/          | Moved to history; PLAN.yaml records final state      |
| archived  | workflow/plans/<id>/   | Inconsistent — drift should flag, sweep should fix   |

This keeps `completed` as a deliberate holding state (agent can still reference the plan for
impl-results, verification docs, etc.) and `archived` as the terminal state after physical move.

---

## What NOT to Do

**Do not wire archive into `plan update --status archived`.**
`plan update` is a pure metadata command with no filesystem side effects. Adding a conditional
directory move there would: (a) change its contract, (b) make dry-run behavior ambiguous,
(c) cause confusing failures if history merge has conflicts. Keep it a dedicated command like
`delegation closeout`.

**Do not filter `archived` plans in `listCanonicalPlanIDs`.**
Once a plan is archived its directory is gone from workflow/plans/—no filter needed. The filter
would only matter for the inconsistent state (archived status but still in workflow/plans/),
which sweep handles.

---

## Implementation Scope

| File                              | Change                                                        |
|-----------------------------------|---------------------------------------------------------------|
| `commands/workflow/cmd.go`        | Add `planArchiveCmd` under `planCmd`                          |
| `commands/workflow/plan_task.go`  | Add `runWorkflowPlanArchive()`                                |
| `commands/workflow/state.go`      | Add `historyBaseDir()` helper                                 |
| `commands/workflow/drift.go`      | Add `CompletedPlanIDs []string` to `RepoDriftReport`          |
| `commands/workflow/sweep.go`      | Add `SweepActionArchiveCompletedPlans` type and apply handler |
| `commands/workflow/*_test.go`     | Table-driven tests for archive (no-history, merge, dry-run)   |

No schema changes needed—`archived` is already a valid PLAN.yaml status value.

---

## Open Questions

1. **Bulk archive flag?** `--plan` could accept comma-separated IDs (matching `workflow complete`
   convention) to archive multiple plans in one invocation. Simple extension.
2. **What about `paused` plans?** Currently excluded from archive guard (must be `completed`).
   `--force` covers intentional cases. Feels right—paused is not done.
3. **`impl-results.md` lives in history already.** The merge strategy (skip existing, overwrite
   PLAN+TASKS) handles this without special-casing.
