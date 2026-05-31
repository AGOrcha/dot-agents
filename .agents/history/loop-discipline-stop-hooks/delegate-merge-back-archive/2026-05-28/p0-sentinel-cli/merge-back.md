---
schema_version: 1
task_id: p0-sentinel-cli
parent_plan_id: loop-discipline-stop-hooks
title: 'Sentinel CLI: write/read/clear under ''da workflow hook-sentinel'''
summary: Add da workflow hook-sentinel (write/read/clear) with v1 schema, atomic temp+rename writes, archive-on-clear to .agents/history/<plan>/hook-sentinels/<date>/, 36 new tests pass; required scope expansion to cmd.go for command registration (contract acceptance bullet); commands/workflow/static/ has no Go registry today so the bundle-listed static/schemas.go was not created — embedded twin pattern matches existing iter-log dual-write
files_changed: []
verification_result:
    status: pass
    summary: 'Cross-plan coordination: schemas.go registry does not exist in this codebase; each schema has its own _schema.go file with go:embed. Dual-write guard test added in hook_sentinel_test.go (TestWorkflowHookSentinelEmbeddedSchemaMatchesCanonical). If t0-outcome-contract worker assumed a central registry too, they will likely fold back same way.'
integration_notes: 'Cross-plan coordination: schemas.go registry does not exist in this codebase; each schema has its own _schema.go file with go:embed. Dual-write guard test added in hook_sentinel_test.go (TestWorkflowHookSentinelEmbeddedSchemaMatchesCanonical). If t0-outcome-contract worker assumed a central registry too, they will likely fold back same way.'
created_at: "2026-05-26T11:47:13Z"
---

## Summary

Add da workflow hook-sentinel (write/read/clear) with v1 schema, atomic temp+rename writes, archive-on-clear to .agents/history/<plan>/hook-sentinels/<date>/, 36 new tests pass; required scope expansion to cmd.go for command registration (contract acceptance bullet); commands/workflow/static/ has no Go registry today so the bundle-listed static/schemas.go was not created — embedded twin pattern matches existing iter-log dual-write

## Integration Notes

Cross-plan coordination: schemas.go registry does not exist in this codebase; each schema has its own _schema.go file with go:embed. Dual-write guard test added in hook_sentinel_test.go (TestWorkflowHookSentinelEmbeddedSchemaMatchesCanonical). If t0-outcome-contract worker assumed a central registry too, they will likely fold back same way.
