# Wave 2: Canonical Plan And Task Artifacts

Spec: `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — Wave 2
Status: Next wave
Depends on: MVP workflow automation (complete)

## Goal

Introduce YAML-based canonical plan and task artifacts so agents can determine active plan, active task, blockers, and next unblocked task without parsing prose-only plan files.

## Artifacts Introduced

| Path | Purpose |
|------|---------|
| `.agents/workflow/plans/<plan-id>/PLAN.yaml` | Plan metadata and phase summary |
| `.agents/workflow/plans/<plan-id>/TASKS.yaml` | Task graph, status, dependencies |
| `.agents/workflow/plans/<plan-id>/plan.md` | Optional human-readable narrative |

## Implementation Steps

### Step 1: Structs, validation, and I/O functions

Add to `commands/workflow.go`:

- [ ] `CanonicalPlan` struct — schema_version, id, title, status (draft/active/paused/completed/archived), summary, created_at, updated_at, owner, success_criteria, verification_strategy, current_focus_task. Dual json/yaml tags.
- [ ] `CanonicalTaskFile` struct — schema_version, plan_id, tasks[]
- [ ] `CanonicalTask` struct — id, title, status (pending/in_progress/blocked/completed/cancelled), depends_on, blocks, owner, write_scope, verification_required, notes
- [ ] `workflowCanonicalPlanSummary` struct — id, title, status, current_focus_task, blocked/pending/completed counts (for orient integration)
- [ ] `isValidPlanStatus()` and `isValidTaskStatus()` validation helpers
- [ ] `plansBaseDir(projectPath) string` — returns `filepath.Join(projectPath, ".agents", "workflow", "plans")`
- [ ] `loadCanonicalPlan(projectPath, planID) (*CanonicalPlan, error)`
- [ ] `saveCanonicalPlan(projectPath string, plan *CanonicalPlan) error` — MkdirAll + Marshal + WriteFile
- [ ] `loadCanonicalTasks(projectPath, planID) (*CanonicalTaskFile, error)`
- [ ] `saveCanonicalTasks(projectPath string, tasks *CanonicalTaskFile) error`
- [ ] `listCanonicalPlanIDs(projectPath) ([]string, error)` — ReadDir, empty slice if dir absent
- [ ] Tests: `TestLoadCanonicalPlanRoundTrip`, `TestLoadCanonicalTasksRoundTrip`, `TestListCanonicalPlanIDsEmpty`, `TestListCanonicalPlanIDs`, `TestIsValidPlanStatus`, `TestIsValidTaskStatus`

### Step 2: `plan` and `plan show` subcommands

- [ ] `planCmd` (Use: "plan") with `runWorkflowPlanList()` — list all canonical plans with id/title/status/focus task. JSON via `Flags.JSON`
- [ ] `planShowCmd` (Use: "show", Args: ExactArgs(1)) with `runWorkflowPlanShow(planID)` — plan metadata + task summary (counts by status, focus task, blockers). JSON supported
- [ ] Register: `planCmd.AddCommand(planShowCmd)`, add `planCmd` to main workflow cmd
- [ ] Tests for list (empty, populated) and show (valid, missing plan)

### Step 3: `tasks` subcommand

- [ ] `tasksCmd` (Use: "tasks", Args: ExactArgs(1)) with `runWorkflowTasks(planID)` — all tasks with id/title/status/depends_on. JSON supported
- [ ] Tests for task listing and missing plan

### Step 4: `advance` subcommand

- [ ] `advanceCmd` (Use: "advance", Args: ExactArgs(1)) with required `--task` and `--status` flags
- [ ] `runWorkflowAdvance(planID, taskID, status)`:
  1. Validate status with `isValidTaskStatus()`
  2. Load TASKS.yaml, find task by ID, update status
  3. Save TASKS.yaml
  4. Load PLAN.yaml, update `updated_at`; if task -> `in_progress`, set `current_focus_task`
  5. Save PLAN.yaml
  6. `ui.Success()` feedback
- [ ] Tests: advance pending->in_progress, in_progress->completed, invalid status rejected, missing task rejected

### Step 5: Orient/status integration

- [ ] Add `CanonicalPlans []workflowCanonicalPlanSummary` to `workflowOrientState`
- [ ] `collectCanonicalPlans(projectPath) ([]workflowCanonicalPlanSummary, []string)` — graceful warnings for malformed files
- [ ] Update `collectWorkflowState()` to call `collectCanonicalPlans()` and merge warnings
- [ ] Update `deriveWorkflowNextAction()` — check canonical plan focus tasks after checkpoint, before legacy plans
- [ ] Update `renderWorkflowOrientMarkdown()` — add "# Canonical Plans" section
- [ ] Update `runWorkflowStatus()` — show canonical plan count
- [ ] Update existing `TestCollectWorkflowState...` with canonical plan fixtures

## Files Modified

- `commands/workflow.go`
- `commands/workflow_test.go`

## Acceptance Criteria

An agent can determine the active plan, active task, blockers, and next unblocked task without parsing prose-only plan files.

## Verification

```bash
go test ./commands -run 'Canonical|PlanStatus|TaskStatus|Advance'
go test ./commands
go test ./...
```
