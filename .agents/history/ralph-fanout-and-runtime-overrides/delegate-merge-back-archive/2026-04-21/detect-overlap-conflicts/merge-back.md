---
schema_version: 1
task_id: detect-overlap-conflicts
parent_plan_id: ralph-fanout-and-runtime-overrides
title: Detect overlapping fanout conflicts
summary: Refactored discover_unblocked_tasks() in bin/tests/ralph-orchestrate to use 'workflow eligible --json --plan <ids> --limit <n>' instead of multi-step Python+bash approach. The eligible command already handles active-delegation exclusion, dependency blocking, and write-scope conflict detection via max_batch. RALPH_RUN_PLAN completion mode passes --limit 1 to serialize selection. RALPH_FANOUT_PLAN/TASK manual override path is preserved unchanged. No changes to commands/workflow.go or commands/workflow_test.go were required.
files_changed:
    - .agents/workflow/plans/ralph-fanout-and-runtime-overrides/TASKS.yaml
    - .agentsrc.json
verification_result:
    status: pass
    summary: Clean apply; only bin/tests/ralph-orchestrate changed. RALPH_BUNDLE output line format unchanged. The active_delegation_scopes(), scope_lists_overlap(), and parse_plan_scope() helpers remain defined (for backward compat / manual path), though active_delegation_scopes() and parse_plan_scope() are no longer called by discover_unblocked_tasks().
integration_notes: Clean apply; only bin/tests/ralph-orchestrate changed. RALPH_BUNDLE output line format unchanged. The active_delegation_scopes(), scope_lists_overlap(), and parse_plan_scope() helpers remain defined (for backward compat / manual path), though active_delegation_scopes() and parse_plan_scope() are no longer called by discover_unblocked_tasks().
created_at: "2026-04-21T16:34:33Z"
---

## Summary

Refactored discover_unblocked_tasks() in bin/tests/ralph-orchestrate to use 'workflow eligible --json --plan <ids> --limit <n>' instead of multi-step Python+bash approach. The eligible command already handles active-delegation exclusion, dependency blocking, and write-scope conflict detection via max_batch. RALPH_RUN_PLAN completion mode passes --limit 1 to serialize selection. RALPH_FANOUT_PLAN/TASK manual override path is preserved unchanged. No changes to commands/workflow.go or commands/workflow_test.go were required.

## Integration Notes

Clean apply; only bin/tests/ralph-orchestrate changed. RALPH_BUNDLE output line format unchanged. The active_delegation_scopes(), scope_lists_overlap(), and parse_plan_scope() helpers remain defined (for backward compat / manual path), though active_delegation_scopes() and parse_plan_scope() are no longer called by discover_unblocked_tasks().
