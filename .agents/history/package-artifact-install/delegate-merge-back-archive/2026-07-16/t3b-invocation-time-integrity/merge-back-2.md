---
schema_version: 1
task_id: t3b-invocation-time-integrity
parent_plan_id: package-artifact-install
title: 'Invocation-time integrity + CAS orphan GC'
summary: 'Verify-at-use + CAS orphan GC; round-2 closed a data-loss BLOCKER.'
files_changed:
    - internal/config/materialize.go
    - internal/platform/resource_plan.go
verification_result:
    status: pass
    summary: 'tests green under -race.'
integration_notes: 'Restored to clear iteration-close guard; commits d926a59f + 0ba4569c.'
created_at: "2026-07-15T02:05:00Z"
---
Restored to clear iteration-close guard.