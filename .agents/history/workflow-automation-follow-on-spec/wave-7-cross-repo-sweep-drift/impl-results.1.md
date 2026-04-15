# Implementation Results 1

Date: 2026-04-11
Task: Wave 7 — Cross-Repo Sweep and Drift (all 7 steps)

## RFC

`docs/rfcs/wave-7-cross-repo-sweep-rfc.md` — resolved 5 design decisions:
1. Managed inventory only (`~/.agents/config.json`)
2. Thresholds: 7 days checkpoint, 30 days proposals
3. Sweep mutations: confirmation-gated (not proposal-gated)
4. Both per-repo and aggregate reports; saved to `~/.agents/context/drift-report.json`
5. Default read-only; mutations require `--apply`

## Step 1: ManagedProject + loadManagedProjects

- `ManagedProject` struct: name, path
- `loadManagedProjects()` — reads `config.Load().Projects`, sorts by name

## Step 2: RepoDriftReport + detectRepoDrift

- `RepoDriftReport` — reachable, missing_checkpoint, stale_checkpoint, checkpoint_age_days, stale_proposal_count, missing_workflow_dir, missing_plan_structure, warnings, status
- `detectRepoDrift(project, checkpointStaleDays, proposalStaleDays)` — all checks read-only:
  1. Path existence (→ unreachable if not found)
  2. Checkpoint existence and age
  3. Stale proposals (via `config.ListPendingProposals()`)
  4. `.agents/workflow/` directory presence
  5. `.agents/workflow/plans/` directory presence

## Step 3: AggregateDriftReport + aggregateDrift

- `AggregateDriftReport` — timestamp, counts, reports, top_warnings
- `aggregateDrift(reports)` — sums by status, deduplicates top warnings with `[project]` prefix

## Step 4: `workflow drift` subcommand

- `runWorkflowDrift` — loads managed projects, runs drift, aggregates, saves report, renders table
- Flags: `--stale-days`, `--proposal-days`, `--project` (single-project mode), `--json`
- Saves drift report to `~/.agents/context/drift-report.json`

## Step 5: SweepAction types + planSweep

- `SweepActionType` enum: `scaffold_workflow_dir`, `create_plan_structure`, `create_checkpoint_reminder`, `flag_stale_proposals`
- `SweepActionItem` — project, action, description, requires_confirmation
- `SweepPlan` — slice of items
- `planSweep(reports)` — generates actions for each drift condition; all FS-mutating actions require confirmation

## Step 6: `workflow sweep` subcommand + sweep log

- `SweepLogEntry` + `appendSweepLog()` → `~/.agents/context/sweep-log.jsonl`
- `applySweepAction()` — creates dirs for scaffold/plan-structure; info-only for reminder/flag
- `runWorkflowSweep` — drift → plan → dry-run display or `--apply` with per-action confirmation
- `Flags.Yes` (`-y`) bypasses confirmation prompts

## Step 7: Status/orient integration

- `LocalDrift *RepoDriftReport` on `workflowOrientState` — populated for current project
- Only set when `Status != "healthy"` — nil for healthy projects (no noise)
- `renderWorkflowOrientMarkdown` — `# Local Drift` section with warnings
- `collectWorkflowState` — runs `detectRepoDrift` on current project using default thresholds

## Tests (workflow_test.go)

- `TestDetectRepoDrift_Unreachable`
- `TestDetectRepoDrift_FreshProject`
- `TestDetectRepoDrift_HealthyProject`
- `TestDetectRepoDrift_StaleCheckpoint`
- `TestAggregateDrift_Summary`
- `TestPlanSweep_GeneratesActions`
- `TestPlanSweep_UnreachableSkipped`
- `TestPlanSweep_AllMutatingActionsRequireConfirmation`

## Verification

```
go test ./... — all green
go run ./cmd/dot-agents workflow drift --help → registered
go run ./cmd/dot-agents workflow sweep --help → registered
go run ./cmd/dot-agents workflow drift → live output against 3 managed projects
  ResumeAgent [warn], dot-agents [warn], payout [warn]
  report saved: ~/.agents/context/drift-report.json
```

Wave 7 complete — all 7 steps, RFC written and accepted.
