---
schema_version: 1
task_id: t0-outcome-contract
parent_plan_id: r1-5-hook-enforcement-telemetry
title: Define durable hook-outcome schema and archived-sentinel linkage
summary: 'workflow-hook-outcome schema + bundled twin land per design D2. File shape: top-level schema_version=1 plus ordered records[] of HookOutcome objects. Closed via additionalProperties=false; propertyNames deny list rejects transcript_excerpt, tool_input, tool_output, stdout, stderr, command_args, failure_message (R1.2 / Boundary). lifecycle_point + intervention_class required together (R1.3). schemas.go registration deferred to t-schema-validator-wiring per split. Verification: JSON parse OK on both files; go build clean. PR #83 https://github.com/NikashPrakash/dot-agents/pull/83 commit bd60d479.'
files_changed: []
verification_result:
    status: pass
    summary: 'Parent: review PR #83 and merge before kicking t-schema-validator-wiring (which registers this schema in commands/workflow/static/schemas.go) and t1-capture-outcomes (which adds the CLI primitive).'
integration_notes: 'Parent: review PR #83 and merge before kicking t-schema-validator-wiring (which registers this schema in commands/workflow/static/schemas.go) and t1-capture-outcomes (which adds the CLI primitive).'
created_at: "2026-05-26T11:39:16Z"
---

## Summary

workflow-hook-outcome schema + bundled twin land per design D2. File shape: top-level schema_version=1 plus ordered records[] of HookOutcome objects. Closed via additionalProperties=false; propertyNames deny list rejects transcript_excerpt, tool_input, tool_output, stdout, stderr, command_args, failure_message (R1.2 / Boundary). lifecycle_point + intervention_class required together (R1.3). schemas.go registration deferred to t-schema-validator-wiring per split. Verification: JSON parse OK on both files; go build clean. PR #83 https://github.com/NikashPrakash/dot-agents/pull/83 commit bd60d479.

## Integration Notes

Parent: review PR #83 and merge before kicking t-schema-validator-wiring (which registers this schema in commands/workflow/static/schemas.go) and t1-capture-outcomes (which adds the CLI primitive).
