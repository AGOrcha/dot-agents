---
schema_version: 1
task_id: seam-review-convert
parent_plan_id: seam-interface-di-migration
title: Convert commands/review.go to per-file reviewDeps interface
summary: 'Converted commands/review.go to per-file reviewDeps interface-DI; six os/higher-order seams (osMkdirAll, osWriteFile, osRemove, applyProposalFn, archiveProposalFn, runRefreshFn) flow through one interface. Commit 9b6a96ab on origin/seam-interface-di; PR #40 CI in progress.'
files_changed:
    - commands/add.go
verification_result:
    status: pass
    summary: 'Touched commands/seams_test.go (one file outside the bundle write_scope) to delete six captureProposalRollback and three approve/reject test functions that called the now-dead package vars; equivalent DI coverage was added to commands/review_test.go via fakeReviewDeps. withApplyProposalStub / withArchiveProposalStub / withRunRefreshStub helpers are intentionally left in place for the planned atomic-delete commit. Did NOT touch add.go / add_test.go (parallel worker owns those) or seams.go (legacy seams must remain for add.go''s outstanding conversion and the atomic-delete commit). Followup carried: ''add.go conversion is in parallel — confirm both land before atomic-delete''.'
integration_notes: 'Touched commands/seams_test.go (one file outside the bundle write_scope) to delete six captureProposalRollback and three approve/reject test functions that called the now-dead package vars; equivalent DI coverage was added to commands/review_test.go via fakeReviewDeps. withApplyProposalStub / withArchiveProposalStub / withRunRefreshStub helpers are intentionally left in place for the planned atomic-delete commit. Did NOT touch add.go / add_test.go (parallel worker owns those) or seams.go (legacy seams must remain for add.go''s outstanding conversion and the atomic-delete commit). Followup carried: ''add.go conversion is in parallel — confirm both land before atomic-delete''.'
created_at: "2026-05-21T00:17:28Z"
---

## Summary

Converted commands/review.go to per-file reviewDeps interface-DI; six os/higher-order seams (osMkdirAll, osWriteFile, osRemove, applyProposalFn, archiveProposalFn, runRefreshFn) flow through one interface. Commit 9b6a96ab on origin/seam-interface-di; PR #40 CI in progress.

## Integration Notes

Touched commands/seams_test.go (one file outside the bundle write_scope) to delete six captureProposalRollback and three approve/reject test functions that called the now-dead package vars; equivalent DI coverage was added to commands/review_test.go via fakeReviewDeps. withApplyProposalStub / withArchiveProposalStub / withRunRefreshStub helpers are intentionally left in place for the planned atomic-delete commit. Did NOT touch add.go / add_test.go (parallel worker owns those) or seams.go (legacy seams must remain for add.go's outstanding conversion and the atomic-delete commit). Followup carried: 'add.go conversion is in parallel — confirm both land before atomic-delete'.
