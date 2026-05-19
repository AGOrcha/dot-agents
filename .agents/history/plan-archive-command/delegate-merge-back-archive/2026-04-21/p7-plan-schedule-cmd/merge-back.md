---
schema_version: 1
task_id: p7-plan-schedule-cmd
parent_plan_id: plan-archive-command
title: Add workflow plan schedule subcommand
summary: |-
  Added runWorkflowPlanSchedule() in plan_task.go implementing Kahn BFS topological sort.
  Wired planScheduleCmd into planCmd in cmd.go. Output: wave→task list, critical path length,
  max intra-plan parallelism. --json flag supported. Worker committed 6f613c5 but hit rate
  limit before writing merge-back artifact. Build clean, subcommand verified present.
files_changed:
    - commands/workflow/cmd.go
    - commands/workflow/plan_task.go
verification_result:
    status: partial
    summary: "go build ./... passes; planScheduleCmd wired and callable. Full go test not re-verified post-commit."
integration_notes: "Parent wrote merge-back after worker rate-limited during closeout. Code committed at 6f613c5."
created_at: "2026-04-21T06:10:00Z"
---

## Summary

Worker committed implementation at 6f613c5 then rate-limited before writing merge-back.
Parent confirmed build clean and subcommand present in cmd.go.

## Integration Notes

Accept — code is committed and build passes.
