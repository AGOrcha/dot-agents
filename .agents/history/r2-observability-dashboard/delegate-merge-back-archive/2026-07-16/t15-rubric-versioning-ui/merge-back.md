---
schema_version: 1
task_id: t15-rubric-versioning-ui
parent_plan_id: r2-observability-dashboard
title: Rubric explainer view (read /api/rubric, render weights + bands)
summary: 'Completed the rubric explainer by reusing the t08 RubricView skeleton (not duplicating): extracted a new RubricTable (signals+weights table plus the score-band ladder), refactored RubricView to mount it and add loading/error testids, and added vitest tests. 8 tests pass.'
files_changed:
    - web/dashboard/src/components/RubricTable.tsx
    - web/dashboard/src/components/RubricTable.test.tsx
    - web/dashboard/src/views/RubricView.tsx
    - web/dashboard/src/views/RubricView.test.tsx
verification_result:
    status: pass
    summary: 'Parent independent re-run: vitest run RubricTable+RubricView → 2 files, 8 tests passed, 0 failed. Scope = exactly the 4 declared files (+279/-29). feedback_goal YES (reads /rubric, renders weights + band ranges, loading + error).'
integration_notes: 'Branch feat/r2-t15-rubric-view @ 1232882c → PR to master. tsc Promise.withResolvers diagnostic is a pre-existing project-wide tsconfig quirk (test gate is vitest, green).'
created_at: "2026-07-16T05:10:00Z"
---

## Summary
Reused the t08 skeleton `RubricView.tsx`; added `RubricTable.tsx` (+test), refactored the view to mount it, added `RubricView.test.tsx`. Band ladder rendered as inclusive [min,max] ranges.

## Verification
- `vitest run` RubricTable+RubricView → 8 tests passed, 0 failed (parent re-run confirmed).
- Scope: exactly the 4 declared files.
- feedback_goal: YES.

## Follow-ups
None.
