---
schema_version: 1
task_id: seam-atomic-convergence
parent_plan_id: seam-interface-di-migration
title: Atomic convergence — flip stdAddDeps, thread importDeps+addDeps through runRefresh, delete legacy seams
summary: 'Atomic convergence commit (09f1f12f) closing PR #40. Flipped stdAddDeps to direct os.*/projectsync/config calls; threaded importDeps through runImportFromRefresh and addDeps through runRefresh→restoreFromResources (signature change: runRefresh now takes 4 args). Deleted commands/seams.go entirely (12 package vars + 4 imports gone). Removed 12 with*Stub helpers from seams_test.go; migrated TestRunRefresh_ConfigLoadError + TestRunRefresh_ProjectFilterNotFound to fakeImportDeps; migrated TestRunRefresh_RestoreFailureDoesNotStampMetadata to fakeAddDeps{copyFile: ...} preserving the metadata-skip assertion intact. 28 runRefresh call sites updated to the 4-arg form across refresh/refresh_idempotency/seams/review tests; 3 mechanical signature catch-ups in tests outside the bundle''s primary write_scope (doctor_repair_e2e_test.go, import_test.go, review_test.go) — pure 1-line argument additions, no behavioral change, anticipated by bundle gotcha #6.'
files_changed: []
verification_result:
    status: pass
    summary: 'Pre-existing TestCRGBridgeFreshBuildRealCRG (missing python module) still failing — unrelated and explicitly acceptable. PR #40 push 9cdf0d59..09f1f12f; CI Lint Workflows pass, 3 Test jobs pending at push time. After parent accepts, ensure forbidden_scope is reconciled with bundle gotcha #6 in the seam-interface-di-migration spec (the gotcha mentions 23 callers but the actual call-site fanout is 28 + 3 outside-scope = 31; only the 3 outside-scope touches need spec attention).'
integration_notes: 'Pre-existing TestCRGBridgeFreshBuildRealCRG (missing python module) still failing — unrelated and explicitly acceptable. PR #40 push 9cdf0d59..09f1f12f; CI Lint Workflows pass, 3 Test jobs pending at push time. After parent accepts, ensure forbidden_scope is reconciled with bundle gotcha #6 in the seam-interface-di-migration spec (the gotcha mentions 23 callers but the actual call-site fanout is 28 + 3 outside-scope = 31; only the 3 outside-scope touches need spec attention).'
created_at: "2026-05-21T01:02:40Z"
---

## Summary

Atomic convergence commit (09f1f12f) closing PR #40. Flipped stdAddDeps to direct os.*/projectsync/config calls; threaded importDeps through runImportFromRefresh and addDeps through runRefresh→restoreFromResources (signature change: runRefresh now takes 4 args). Deleted commands/seams.go entirely (12 package vars + 4 imports gone). Removed 12 with*Stub helpers from seams_test.go; migrated TestRunRefresh_ConfigLoadError + TestRunRefresh_ProjectFilterNotFound to fakeImportDeps; migrated TestRunRefresh_RestoreFailureDoesNotStampMetadata to fakeAddDeps{copyFile: ...} preserving the metadata-skip assertion intact. 28 runRefresh call sites updated to the 4-arg form across refresh/refresh_idempotency/seams/review tests; 3 mechanical signature catch-ups in tests outside the bundle's primary write_scope (doctor_repair_e2e_test.go, import_test.go, review_test.go) — pure 1-line argument additions, no behavioral change, anticipated by bundle gotcha #6.

## Integration Notes

Pre-existing TestCRGBridgeFreshBuildRealCRG (missing python module) still failing — unrelated and explicitly acceptable. PR #40 push 9cdf0d59..09f1f12f; CI Lint Workflows pass, 3 Test jobs pending at push time. After parent accepts, ensure forbidden_scope is reconciled with bundle gotcha #6 in the seam-interface-di-migration spec (the gotcha mentions 23 callers but the actual call-site fanout is 28 + 3 outside-scope = 31; only the 3 outside-scope touches need spec attention).
