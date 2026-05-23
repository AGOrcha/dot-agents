---
schema_version: 1
task_id: seam-install-convert
parent_plan_id: seam-interface-di-migration
title: Convert commands/install.go to per-file installDeps interface
summary: 'Converted commands/install.go from package-var func-seams (osMkdirAll/osSymlink/osGetwd/configLoad) to per-file installDeps interface; landed as 356d3f69; install_test.go + the install branches of seams_test.go migrated to fakeInstallDeps; commands package tests green (69.5s, race+count=1); seams.go install vars deferred to atomic-delete commit per bundle gotcha #4'
files_changed: []
verification_result:
    status: pass
    summary: 'Push to origin/seam-interface-di is live. PR #40 lint passed at commit; test matrix (macos/ubuntu/windows) was still pending at merge-back time — recheck with gh pr checks 40 before delegating add.go bundle. Full merge-back narrative at .agents/active/delegation/seam-install-convert.merge-back.md'
integration_notes: 'Push to origin/seam-interface-di is live. PR #40 lint passed at commit; test matrix (macos/ubuntu/windows) was still pending at merge-back time — recheck with gh pr checks 40 before delegating add.go bundle. Full merge-back narrative at .agents/active/delegation/seam-install-convert.merge-back.md'
created_at: "2026-05-20T13:58:33Z"
---

## Summary

Converted commands/install.go from package-var func-seams (osMkdirAll/osSymlink/osGetwd/configLoad) to per-file installDeps interface; landed as 356d3f69; install_test.go + the install branches of seams_test.go migrated to fakeInstallDeps; commands package tests green (69.5s, race+count=1); seams.go install vars deferred to atomic-delete commit per bundle gotcha #4

## Integration Notes

Push to origin/seam-interface-di is live. PR #40 lint passed at commit; test matrix (macos/ubuntu/windows) was still pending at merge-back time — recheck with gh pr checks 40 before delegating add.go bundle. Full merge-back narrative at .agents/active/delegation/seam-install-convert.merge-back.md
