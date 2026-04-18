---
schema_version: 1
task_id: p10-workflow-command-decomposition
parent_plan_id: loop-agent-pipeline
title: Split workflow command into subpackage files to reduce worker hotspot contention
summary: Extracted package workflow under commands/workflow with Deps injection; thin commands/workflow.go; schemas/embeds under workflow/static; globalflagcov loads ./commands/workflow; tests adjusted.
files_changed:
    - .agents/active/delegation-bundles/del-c3-sync-command-decomposition-1776539849.yaml
    - .agents/active/delegation-bundles/del-c4-skills-command-decomposition-1776539849.yaml
    - .agents/active/delegation-bundles/del-c6-status-import-helper-extraction-1776539976.yaml
    - .agents/active/delegation/c3-sync-command-decomposition.yaml
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-04-18T19:39:37Z"
---

## Summary

Extracted package workflow under commands/workflow with Deps injection; thin commands/workflow.go; schemas/embeds under workflow/static; globalflagcov loads ./commands/workflow; tests adjusted.

## Integration Notes


