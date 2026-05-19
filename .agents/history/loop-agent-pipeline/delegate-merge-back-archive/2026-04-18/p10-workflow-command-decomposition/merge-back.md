---
schema_version: 1
task_id: p10-workflow-command-decomposition
parent_plan_id: loop-agent-pipeline
title: Split workflow command into subpackage files to reduce worker hotspot contention
summary: Extracted package workflow under commands/workflow with Deps injection; thin commands/workflow.go; schemas/embeds under workflow/static; globalflagcov loads ./commands/workflow; tests split across commands/workflow/*_test.go and testutil_test.go; cmd.go entry for cobra tree.
files_changed:
    - commands/workflow.go
    - commands/workflow/cmd.go
    - commands/workflow/
    - .agents/active/merge-back/p10-workflow-command-decomposition.md (removed after archive)
    - .agents/active/delegation/p10-workflow-command-decomposition.yaml (removed after archive)
    - .agents/active/delegation-bundles/del-p10-workflow-command-decomposition-1776539976.yaml (removed after archive)
    - .agents/active/verification/p10-workflow-command-decomposition/merge-back.result.yaml (removed after archive)
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-04-18T19:39:37Z"
archived_at: "2026-04-18T23:59:00Z"
---

## Summary

Extracted package workflow under commands/workflow with Deps injection; thin commands/workflow.go; schemas/embeds under workflow/static; globalflagcov loads ./commands/workflow; tests adjusted and split into multiple *_test.go files plus testutil_test.go; cmd.go holds the cobra command tree.

## Integration Notes

Archived from `.agents/active/` after canonical task **p10** completion (delegation contract, bundle, merge-back snapshot).
