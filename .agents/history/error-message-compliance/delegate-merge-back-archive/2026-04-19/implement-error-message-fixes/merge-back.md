---
schema_version: 1
task_id: implement-error-message-fixes
parent_plan_id: error-message-compliance
title: Normalize mismatched command errors onto the shared CLIError contract
summary: 'Review accepted: scoped error-message normalization matches the contract, with clearer recoverable failures and finite-domain validation across the touched KG and agent commands.'
files_changed:
    - .agents/workflow/plans/error-message-compliance/PLAN.yaml
    - .agents/workflow/plans/error-message-compliance/TASKS.yaml
    - commands/agents/import.go
    - commands/agents/promote.go
    - commands/agents/remove.go
    - commands/kg/bridge.go
    - commands/kg/query_lint_maintain.go
    - commands/kg/sync_code_warm_link.go
    - docs/ERROR_MESSAGE_CONTRACT.md
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-04-19T21:43:58Z"
---

## Summary

Review accepted: scoped error-message normalization matches the contract, with clearer recoverable failures and finite-domain validation across the touched KG and agent commands.

## Integration Notes


