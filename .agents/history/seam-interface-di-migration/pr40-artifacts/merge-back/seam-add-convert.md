---
schema_version: 1
task_id: seam-add-convert
parent_plan_id: seam-interface-di-migration
title: Convert commands/add.go to per-file addDeps interface
summary: 'Converted commands/add.go to per-file addDeps interface-DI; six seams (osMkdirAll, osWriteFile, osRemove, osExecutable, copyFile, configLoad) flow through one interface. Commit 9cdf0d59 on origin/seam-interface-di; PR #40 CI in progress.'
files_changed: []
verification_result:
    status: pass
    summary: 'Touched commands/seams_test.go (one file outside the bundle write_scope) to migrate add-related tests whose signatures broke (TestWriteKGMCPConfigFile_MkdirAllError, _WriteFileError; TestWriteKGMCPConfigs_ExecutableError, _PerFileError; TestBackupExistingConfigsList_RemoveError, _LstatFails, _SymlinkBranch; TestRestoreLegacyResourceFile_NoMapping; TestRunAdd_ConfigLoadError) - same surgical migration pattern install.go used at 356d3f69. stdAddDeps intentionally routes through legacy seams.go package vars (osMkdirAll, copyFile, configLoad, etc.) so cross-file tests in refresh_test.go and seams_test.go that still pin fault-injection via withCopyFileStub/withRemoveStub keep working until atomic-delete. Did NOT touch review.go / review_test.go (parallel worker landed 9b6a96ab). Did NOT touch seams.go (legacy seams reaped by atomic-delete). Followup carried: ''atomic-delete commit removes the package vars and remaining with*Stub helpers now that both add.go and review.go are converted.'''
integration_notes: 'Touched commands/seams_test.go (one file outside the bundle write_scope) to migrate add-related tests whose signatures broke (TestWriteKGMCPConfigFile_MkdirAllError, _WriteFileError; TestWriteKGMCPConfigs_ExecutableError, _PerFileError; TestBackupExistingConfigsList_RemoveError, _LstatFails, _SymlinkBranch; TestRestoreLegacyResourceFile_NoMapping; TestRunAdd_ConfigLoadError) - same surgical migration pattern install.go used at 356d3f69. stdAddDeps intentionally routes through legacy seams.go package vars (osMkdirAll, copyFile, configLoad, etc.) so cross-file tests in refresh_test.go and seams_test.go that still pin fault-injection via withCopyFileStub/withRemoveStub keep working until atomic-delete. Did NOT touch review.go / review_test.go (parallel worker landed 9b6a96ab). Did NOT touch seams.go (legacy seams reaped by atomic-delete). Followup carried: ''atomic-delete commit removes the package vars and remaining with*Stub helpers now that both add.go and review.go are converted.'''
created_at: "2026-05-21T00:30:52Z"
---

## Summary

Converted commands/add.go to per-file addDeps interface-DI; six seams (osMkdirAll, osWriteFile, osRemove, osExecutable, copyFile, configLoad) flow through one interface. Commit 9cdf0d59 on origin/seam-interface-di; PR #40 CI in progress.

## Integration Notes

Touched commands/seams_test.go (one file outside the bundle write_scope) to migrate add-related tests whose signatures broke (TestWriteKGMCPConfigFile_MkdirAllError, _WriteFileError; TestWriteKGMCPConfigs_ExecutableError, _PerFileError; TestBackupExistingConfigsList_RemoveError, _LstatFails, _SymlinkBranch; TestRestoreLegacyResourceFile_NoMapping; TestRunAdd_ConfigLoadError) - same surgical migration pattern install.go used at 356d3f69. stdAddDeps intentionally routes through legacy seams.go package vars (osMkdirAll, copyFile, configLoad, etc.) so cross-file tests in refresh_test.go and seams_test.go that still pin fault-injection via withCopyFileStub/withRemoveStub keep working until atomic-delete. Did NOT touch review.go / review_test.go (parallel worker landed 9b6a96ab). Did NOT touch seams.go (legacy seams reaped by atomic-delete). Followup carried: 'atomic-delete commit removes the package vars and remaining with*Stub helpers now that both add.go and review.go are converted.'
