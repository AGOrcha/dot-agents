# Implementation Results 3

Date: 2026-04-10
Wave: Wave 2 — Canonical Plan And Task Artifacts
Spec: `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md`

## Slice Completed

Implemented canonical YAML-based plan and task artifacts so agents can determine active plan, active task, blockers, and next unblocked task without parsing prose-only plan files.

## Changes Made

### `commands/workflow.go`

New types:
- `CanonicalPlan` struct — PLAN.yaml schema with dual json/yaml tags
- `CanonicalTaskFile` + `CanonicalTask` structs — TASKS.yaml schema
- `workflowCanonicalPlanSummary` — compact view for orient/status

New I/O functions:
- `plansBaseDir(projectPath)` — path helper for `.agents/workflow/plans/`
- `listCanonicalPlanIDs(projectPath)` — graceful empty when dir absent
- `loadCanonicalPlan` / `saveCanonicalPlan`
- `loadCanonicalTasks` / `saveCanonicalTasks`
- `collectCanonicalPlans(projectPath)` — returns summaries + warnings

Validation helpers:
- `isValidPlanStatus()` — draft/active/paused/completed/archived
- `isValidTaskStatus()` — pending/in_progress/blocked/completed/cancelled

New subcommands:
- `workflow plan` — list canonical plans (JSON supported)
- `workflow plan show <plan-id>` — plan metadata + task summary (JSON supported)
- `workflow tasks <plan-id>` — task listing with dependencies (JSON supported)
- `workflow advance <plan-id> --task <id> --status <status>` — mutate task status, auto-updates PLAN.yaml current_focus_task and updated_at

Integration:
- `workflowOrientState` gains `CanonicalPlans` field
- `collectWorkflowState()` calls `collectCanonicalPlans()` and merges warnings
- `deriveWorkflowNextAction()` prefers canonical plan focus task over legacy plan pending items
- `renderWorkflowOrientMarkdown()` gains "# Canonical Plans" section
- `runWorkflowStatus()` shows canonical plan count

### `commands/workflow_test.go`

New helper:
- `addCanonicalPlanFixture(t, repo)` — creates `.agents/workflow/plans/wave-2/PLAN.yaml` and `TASKS.yaml` in test repo

New tests (11):
- `TestIsValidPlanStatus`
- `TestIsValidTaskStatus`
- `TestListCanonicalPlanIDsEmptyWhenDirAbsent`
- `TestListCanonicalPlanIDs`
- `TestLoadCanonicalPlanRoundTrip`
- `TestLoadCanonicalTasksRoundTrip`
- `TestCollectCanonicalPlans`
- `TestRunWorkflowAdvanceUpdatesTaskAndPlan`
- `TestRunWorkflowAdvanceInvalidStatus`
- `TestRunWorkflowAdvanceMissingTask`
- `TestCollectWorkflowStateIncludesCanonicalPlans`

Updated tests:
- `TestRenderWorkflowOrientMarkdownIncludesRequiredSections` — checks `# Canonical Plans` heading

## Verification

```
go test ./commands -run 'Canonical|PlanStatus|TaskStatus|Advance|ListCanonical|CollectWorkflowStateIncludes' → 11 PASS
go test ./... → all packages pass
go run ./cmd/dot-agents refresh dot-agents → links refreshed cleanly
```

## Backward Compatibility

- Existing `.agents/active/*.plan.md` files remain valid and collected unchanged
- Canonical plans are additive via a separate `CanonicalPlans` field, not merged into `ActivePlans`
- Missing `.agents/workflow/plans/` directory returns empty slice, no error
