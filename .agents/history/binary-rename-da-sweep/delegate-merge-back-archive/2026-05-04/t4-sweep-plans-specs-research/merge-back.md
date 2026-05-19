---
schema_version: 1
task_id: t4-sweep-plans-specs-research
parent_plan_id: binary-rename-da-sweep
title: Sweep .agents/workflow/{plans,specs}/ + research/ for binary-name references
summary: Swept 'dot-agents <verb>' -> 'da <verb>' across .agents/workflow/plans/, .agents/workflow/specs/, and research/ per ADR-0006 hard cutover. 34 files modified in commit 88fd4a6. Hard-test grep clean (59 remaining matches all project-name prose or file paths).
files_changed: []
verification_result:
    status: pass
    summary: 'All remaining 59 grep matches are project-name references (project-as-subject, project-as-modifier, file paths like src/bin/dot-agents, ./bin/dot-agents, cmd/dot-agents). No binary invocations of dot-agents <verb> remain in plans/specs/research. Pre-existing dirty state on PLAN.yaml/TASKS.yaml of binary-rename-da-sweep (workflow runtime updates: schema-comment removal, updated_at timestamp, current_focus_task) was carried through the commit since both files are inside this task''s write_scope and required edits anyway. .agents/history/ untouched. .agentsrc.json + research/evaluations/agent-execution.md remained as out-of-scope dirty state at session start and were left untouched.'
integration_notes: 'All remaining 59 grep matches are project-name references (project-as-subject, project-as-modifier, file paths like src/bin/dot-agents, ./bin/dot-agents, cmd/dot-agents). No binary invocations of dot-agents <verb> remain in plans/specs/research. Pre-existing dirty state on PLAN.yaml/TASKS.yaml of binary-rename-da-sweep (workflow runtime updates: schema-comment removal, updated_at timestamp, current_focus_task) was carried through the commit since both files are inside this task''s write_scope and required edits anyway. .agents/history/ untouched. .agentsrc.json + research/evaluations/agent-execution.md remained as out-of-scope dirty state at session start and were left untouched.'
created_at: "2026-05-04T02:37:54Z"
---

## Summary

Swept 'dot-agents <verb>' -> 'da <verb>' across .agents/workflow/plans/, .agents/workflow/specs/, and research/ per ADR-0006 hard cutover. 34 files modified in commit 88fd4a6. Hard-test grep clean (59 remaining matches all project-name prose or file paths).

## Integration Notes

All remaining 59 grep matches are project-name references (project-as-subject, project-as-modifier, file paths like src/bin/dot-agents, ./bin/dot-agents, cmd/dot-agents). No binary invocations of dot-agents <verb> remain in plans/specs/research. Pre-existing dirty state on PLAN.yaml/TASKS.yaml of binary-rename-da-sweep (workflow runtime updates: schema-comment removal, updated_at timestamp, current_focus_task) was carried through the commit since both files are inside this task's write_scope and required edits anyway. .agents/history/ untouched. .agentsrc.json + research/evaluations/agent-execution.md remained as out-of-scope dirty state at session start and were left untouched.
