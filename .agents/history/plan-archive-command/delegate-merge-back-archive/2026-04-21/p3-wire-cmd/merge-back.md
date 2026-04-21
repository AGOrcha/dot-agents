---
schema_version: 1
task_id: p3-wire-cmd
parent_plan_id: plan-archive-command
title: Wire planArchiveCmd into cmd.go under planCmd
summary: 'Added planArchiveCmd to cmd.go alongside planCreateCmd and planUpdateCmd. Flags: --plan (required, comma-separated IDs) and --force (bool). Calls runWorkflowPlanArchive(). Dry-run via global -n flag by adding DryRun func() bool to workflow.GlobalFlags (deps.go) and wiring it in commands/workflow.go. go build passes; workflow package tests pass; dot-agents workflow plan archive --help shows the subcommand with correct flags.'
files_changed:
    - .agents/active/delegation-bundles/del-p1-historybasedir-helper-1776745216.yaml
    - .agents/active/delegation-bundles/del-p2-archive-handler-1776746536.yaml
    - .agents/active/delegation-bundles/del-p3-wire-cmd-1776746947.yaml
    - .agents/active/delegation-bundles/del-p4-drift-extension-1776746947.yaml
    - .agents/active/delegation-bundles/del-p5-sweep-extension-1776746947.yaml
    - .agents/active/delegation/p1-historybasedir-helper.yaml
    - .agents/active/delegation/p2-archive-handler.yaml
    - .agents/active/delegation/p3-wire-cmd.yaml
    - .agents/active/delegation/p4-drift-extension.yaml
    - .agents/active/delegation/p5-sweep-extension.yaml
    - .agents/workflow/plans/plan-archive-command/PLAN.yaml
    - .agents/workflow/plans/plan-archive-command/TASKS.yaml
    - .agents/workflow/plans/planner-evidence-backed-write-scope/PLAN.yaml
    - .agents/workflow/plans/planner-evidence-backed-write-scope/TASKS.yaml
verification_result:
    status: pass
    summary: 'Three files changed: commands/workflow/cmd.go (planArchiveCmd added), commands/workflow/deps.go (GlobalFlags.DryRun field), commands/workflow.go (DryRun wired). All committed in 7ac9452 and 2641fc9.'
integration_notes: 'Three files changed: commands/workflow/cmd.go (planArchiveCmd added), commands/workflow/deps.go (GlobalFlags.DryRun field), commands/workflow.go (DryRun wired). All committed in 7ac9452 and 2641fc9.'
created_at: "2026-04-21T12:09:56Z"
---

## Summary

Added planArchiveCmd to cmd.go alongside planCreateCmd and planUpdateCmd. Flags: --plan (required, comma-separated IDs) and --force (bool). Calls runWorkflowPlanArchive(). Dry-run via global -n flag by adding DryRun func() bool to workflow.GlobalFlags (deps.go) and wiring it in commands/workflow.go. go build passes; workflow package tests pass; dot-agents workflow plan archive --help shows the subcommand with correct flags.

## Integration Notes

Three files changed: commands/workflow/cmd.go (planArchiveCmd added), commands/workflow/deps.go (GlobalFlags.DryRun field), commands/workflow.go (DryRun wired). All committed in 7ac9452 and 2641fc9.
