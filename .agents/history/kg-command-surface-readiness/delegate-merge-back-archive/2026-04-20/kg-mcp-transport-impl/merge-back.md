---
schema_version: 1
task_id: kg-mcp-transport-impl
parent_plan_id: kg-command-surface-readiness
title: Implement MCP parity decisions from audit
summary: Added Status() freshness guards to handleGetReviewContext and handleGetImpactRadius; both return structured JSON errors (error/state/hint) when graph is unbuilt or busy_or_locked. Added Files []string to DetectChangesOptions and pass req.Files through; CRG v1.x limitation documented in inline comment. Four new unit tests cover unbuilt, busy, and ready-graph paths.
files_changed:
    - .agents/active/delegation-bundles/del-kg-freshness-impl-1776636903.yaml
    - .agents/active/delegation/kg-freshness-impl.yaml
    - .agents/active/verification/kg-freshness-impl/impl-handoff.yaml
    - .agents/workflow/plans/kg-command-surface-readiness/PLAN.yaml
    - .agents/workflow/plans/kg-command-surface-readiness/TASKS.yaml
    - commands/kg/cmd.go
    - commands/kg/kg_test.go
    - commands/kg/sync_code_warm_link.go
verification_result:
    status: pass
    summary: 'No conflicts. Only write-scope files touched: internal/graphstore/mcp_server.go, internal/graphstore/crg.go, internal/graphstore/mcp_server_test.go. cmd.go, kg.go, sync_code_warm_link.go untouched.'
integration_notes: 'No conflicts. Only write-scope files touched: internal/graphstore/mcp_server.go, internal/graphstore/crg.go, internal/graphstore/mcp_server_test.go. cmd.go, kg.go, sync_code_warm_link.go untouched.'
created_at: "2026-04-20T01:21:52Z"
---

## Summary

Added Status() freshness guards to handleGetReviewContext and handleGetImpactRadius; both return structured JSON errors (error/state/hint) when graph is unbuilt or busy_or_locked. Added Files []string to DetectChangesOptions and pass req.Files through; CRG v1.x limitation documented in inline comment. Four new unit tests cover unbuilt, busy, and ready-graph paths.

## Integration Notes

No conflicts. Only write-scope files touched: internal/graphstore/mcp_server.go, internal/graphstore/crg.go, internal/graphstore/mcp_server_test.go. cmd.go, kg.go, sync_code_warm_link.go untouched.
