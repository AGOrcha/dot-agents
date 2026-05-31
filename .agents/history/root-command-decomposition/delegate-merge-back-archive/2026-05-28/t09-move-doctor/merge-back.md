---
schema_version: 1
task_id: t09-move-doctor
parent_plan_id: root-command-decomposition
title: Move doctor command into commands/lifecycle/
summary: 'Moved commands/doctor.go + doctor_test.go + doctor_repair_e2e_test.go into commands/lifecycle/; commands/doctor.go shrinks to a transitional shim (Deps-injection + syncLifecycleGlobals RunE wrapper + runDoctor forwarder for seams_test.go). Duplicates collapsed: resolveLinkDest and managedLinkBroken (already in lifecycle/status.go from t08) dropped; managedLinkHealthy kept in lifecycle/doctor.go. PR #86 open against master.'
files_changed:
    - .agents/active/delegation/t0-outcome-contract.yaml
    - .agents/active/delegation/t6-extract-graphstore-tests.yaml
verification_result:
    status: pass
    summary: 'HasMultipleHardLinks un-export decision: DEFERRED to t11 (no seams_test.go reference exists, so un-export is safe, but the rename touches commands/lifecycle/{backup,status,linkcount_unix,linkcount_windows,backup_test,status_exports_test}.go which are all outside t09 write_scope). Fold-back: commands/status.go printAudit forwarder + lifecycle.PrintAudit trampoline + its export test become deletable in t13 once doctor''s verbose branches call the lowercase printAudit intra-package; TestPrintAuditShim_ForwardsToLifecycle keeps commands/status.go above the coverage gate in the t09→t13 window. Lifecycle cluster complete (t03-t09). Pre-existing graphstore test failure on this runner is unrelated (missing python code_review_graph module).'
integration_notes: 'HasMultipleHardLinks un-export decision: DEFERRED to t11 (no seams_test.go reference exists, so un-export is safe, but the rename touches commands/lifecycle/{backup,status,linkcount_unix,linkcount_windows,backup_test,status_exports_test}.go which are all outside t09 write_scope). Fold-back: commands/status.go printAudit forwarder + lifecycle.PrintAudit trampoline + its export test become deletable in t13 once doctor''s verbose branches call the lowercase printAudit intra-package; TestPrintAuditShim_ForwardsToLifecycle keeps commands/status.go above the coverage gate in the t09→t13 window. Lifecycle cluster complete (t03-t09). Pre-existing graphstore test failure on this runner is unrelated (missing python code_review_graph module).'
created_at: "2026-05-26T12:03:45Z"
---

## Summary

Moved commands/doctor.go + doctor_test.go + doctor_repair_e2e_test.go into commands/lifecycle/; commands/doctor.go shrinks to a transitional shim (Deps-injection + syncLifecycleGlobals RunE wrapper + runDoctor forwarder for seams_test.go). Duplicates collapsed: resolveLinkDest and managedLinkBroken (already in lifecycle/status.go from t08) dropped; managedLinkHealthy kept in lifecycle/doctor.go. PR #86 open against master.

## Integration Notes

HasMultipleHardLinks un-export decision: DEFERRED to t11 (no seams_test.go reference exists, so un-export is safe, but the rename touches commands/lifecycle/{backup,status,linkcount_unix,linkcount_windows,backup_test,status_exports_test}.go which are all outside t09 write_scope). Fold-back: commands/status.go printAudit forwarder + lifecycle.PrintAudit trampoline + its export test become deletable in t13 once doctor's verbose branches call the lowercase printAudit intra-package; TestPrintAuditShim_ForwardsToLifecycle keeps commands/status.go above the coverage gate in the t09→t13 window. Lifecycle cluster complete (t03-t09). Pre-existing graphstore test failure on this runner is unrelated (missing python code_review_graph module).
