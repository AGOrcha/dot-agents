# Wave 7: Cross-Repo Sweep And Drift

Spec: `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — Wave 7
Status: Directional backlog (requires RFC before coding)
Depends on: Wave 6 (delegation), stable multi-project inventory

## Goal

Surface workflow drift across managed repos and allow safe, reviewable sweep operations. Avoid turning `dot-agents` into a continuously running control plane.

## Pre-Implementation Requirements

Requires RFC to resolve:
- Threshold definitions for "stale" proposals, "missing" checkpoints
- How sweep mutations interact with the proposal/review queue
- Cross-repo operation permissions model
- Whether sweep results aggregate into a central report or per-repo artifacts

## Implementation Steps

### Step 1: Multi-project inventory access

- [ ] `loadManagedProjects() ([]ManagedProject, error)` — read `~/.agents/config.json` for all managed projects with their paths and names
- [ ] `ManagedProject` struct: name, path, last_seen (optional)
- [ ] Validate each project path exists and is accessible
- [ ] Tests: load from fixture config, handle missing paths gracefully

### Step 2: Per-repo drift detection

- [ ] `RepoDriftReport` struct:
  - project (name, path)
  - stale_checkpoint (bool — checkpoint older than threshold, e.g. 7 days)
  - missing_checkpoint (bool)
  - stale_proposals (int — proposals older than threshold)
  - missing_hooks ([]string — expected hooks not scaffolded)
  - inconsistent_preferences ([]string — preferences diverging from a baseline)
  - missing_plan_structure (bool — no `.agents/workflow/` directory)
  - warnings[]
- [ ] `detectRepoDrift(project ManagedProject) (RepoDriftReport, error)`:
  1. Check for checkpoint existence and age
  2. Count stale proposals
  3. Check for scaffolded hooks
  4. Check for workflow preferences file
  5. Check for canonical plan structure
  6. Return report with warnings
- [ ] All checks are read-only — no mutations
- [ ] Tests: drift detection with various conditions, missing repo handled gracefully

### Step 3: Aggregate drift report

- [ ] `AggregateDriftReport` struct:
  - timestamp, total_projects, projects_checked
  - reports []RepoDriftReport
  - summary: healthy_count, warning_count, error_count
  - top_warnings[]
- [ ] `aggregateDrift(reports []RepoDriftReport) AggregateDriftReport`
- [ ] Tests: aggregation with mixed health states

### Step 4: `workflow drift` subcommand

- [ ] `driftCmd` (Use: "drift") with `runWorkflowDrift()`:
  1. Load managed projects
  2. Run drift detection on each (in sequence — no concurrency for simplicity)
  3. Aggregate results
  4. Display: per-repo status table, top warnings, summary counts
  5. `--json` flag for machine output
  6. `--project <name>` flag to check a single project
- [ ] Default is read-only reporting
- [ ] Tests: drift report with multiple repos, single-project mode

### Step 5: Sweep operation types

- [ ] `SweepAction` enum: `scaffold_hooks`, `clear_stale_proposals`, `create_checkpoint_reminder`, `create_plan_structure`
- [ ] `SweepPlan` struct:
  - actions []SweepActionItem — each with project, action, description, requires_confirmation (bool)
  - created_at
- [ ] `planSweep(reports []RepoDriftReport) SweepPlan` — generate sweep plan from drift reports
- [ ] Mutating actions always require confirmation
- [ ] Tests: sweep plan generated from drift, all mutating actions flagged

### Step 6: `workflow sweep` subcommand

- [ ] `sweepCmd` (Use: "sweep") with `runWorkflowSweep()`:
  1. Run drift detection across all managed projects
  2. Generate sweep plan
  3. Display planned actions with confirmation prompts
  4. `--dry-run` flag (default behavior) — show what would happen
  5. `--apply` flag — execute the sweep actions with per-action confirmation
  6. Actions: scaffold missing hooks, remove stale proposals (via proposal review), create plan directories
  7. Log all actions to `~/.agents/context/sweep-log.jsonl`
- [ ] Tests: dry-run produces plan, apply without --apply flag rejected

### Step 7: Integration with status

- [ ] Add `ManagedProjectCount` to `workflowOrientState` (optional, only if multi-project)
- [ ] `workflow status` shows drift warning if current project has drift issues
- [ ] Tests: status reflects local drift

## Files Modified

- `commands/workflow.go`
- `commands/workflow_test.go`
- `internal/config/paths.go` (minor — add sweep log path helper if needed)

## Key Constraints

- Default behavior is always read-only reporting
- Mutating sweep actions require explicit `--apply` and per-action confirmation
- Cross-repo operations must NOT become default-mutable
- No continuously running daemon — sweep is on-demand

## Acceptance Criteria

A human or agent can identify workflow drift across managed repos without manual repo-by-repo inspection.

## Verification

```bash
go test ./commands -run 'Drift|Sweep|ManagedProject'
go test ./commands
go test ./...
```
