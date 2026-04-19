---
schema_version: 1
task_id: expand-command-surface-smokes
parent_plan_id: ci-smoke-suite-hardening
title: Expand CLI smoke coverage for workflow, kg, and resource lifecycle surfaces
summary: Added 6 new smoke steps to .github/workflows/test.yml (workflow orient, workflow next, kg --help, kg bridge health, agents list, hooks list). All use identical HOME/AGENTS_HOME isolation pattern. All verified to exit 0 on fresh isolated HOME. kg health excluded (exits 1 without pre-built graph). YAML syntax validated. Commit 4f068fc.
files_changed:
    - .agents/workflow/plans/ci-smoke-suite-hardening/PLAN.yaml
    - .agents/workflow/plans/ci-smoke-suite-hardening/TASKS.yaml
    - bin/dot-agents
verification_result:
    status: pass
    summary: Clean add — 42 new lines, no existing steps modified, all steps inserted before Cleanup.
integration_notes: Clean add — 42 new lines, no existing steps modified, all steps inserted before Cleanup.
created_at: "2026-04-19T20:23:03Z"
---

## Summary

Added 6 new smoke steps to .github/workflows/test.yml (workflow orient, workflow next, kg --help, kg bridge health, agents list, hooks list). All use identical HOME/AGENTS_HOME isolation pattern. All verified to exit 0 on fresh isolated HOME. kg health excluded (exits 1 without pre-built graph). YAML syntax validated. Commit 4f068fc.

## Integration Notes

Clean add — 42 new lines, no existing steps modified, all steps inserted before Cleanup.
