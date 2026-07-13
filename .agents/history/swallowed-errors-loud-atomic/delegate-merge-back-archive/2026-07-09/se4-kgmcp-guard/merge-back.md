---
schema_version: 1
task_id: se4-kgmcp-guard
parent_plan_id: swallowed-errors-loud-atomic
title: 'Slice 4: kgmcp WriteKGMCPConfigFile corrupt-overwrite guard'
summary: 'Fixed WriteKGMCPConfigFile''s swallowed json.Unmarshal error: converted the discarded ''_ = json.Unmarshal'' into a checked early-return that aborts the write (returns an error, leaves the on-disk file byte-for-byte untouched) when an EXISTING claude.json/cursor.json/mcp.json is corrupt JSON. Also distinguished legitimately-absent (os.IsNotExist -> start from empty config, unchanged) from a real ReadFile error (permission denied etc -> abort, same as the corrupt-JSON case) since both are the same should-be-ATOMIC risk class. Added TestWriteKGMCPConfigFile_CorruptExistingFileAborts (core acceptance test: seeded corrupt claude.json, asserts error + untouched bytes) and TestWriteKGMCPConfigFile_UnreadableExistingFileAborts (chmod 0000, POSIX-only). Full existing kgmcp_test.go suite (merge/mkdir-error/write-error/executable-error propagation) still green. go build ./... and go test ./commands/internal/lifecycle/... both green. Committed (no AI trailer), pushed swallowed/4-kgmcp-guard, opened PR #355 base master -- https://github.com/AGOrcha/dot-agents/pull/355. STOPPED before merge per instructions.'
files_changed:
    - .agents/active/delegation-bundles/del-cg-project-local-overlay-1780781079.yaml
    - .agents/active/delegation-bundles/del-gcc5-verify-close-unblock-1779848143.yaml
    - .agents/active/delegation-bundles/del-l1-followups-1782677806.yaml
    - .agents/active/delegation-bundles/del-p1c-verifier-profile-source-aware-1780784015.yaml
    - .agents/active/delegation-bundles/del-p1e-docs-hooks-consistency-1779841818.yaml
    - .agents/active/delegation-bundles/del-p2-hook-scripts-1779841782.yaml
    - .agents/active/delegation-bundles/del-p3-starter-promotion-1779841783.yaml
    - .agents/active/delegation-bundles/del-t14-import-graph-assertion-1779841818.yaml
    - .agents/active/delegation-bundles/del-t2-config-relevance-resolver-1780554684.yaml
    - .agents/active/delegation-bundles/del-t3-cli-readback-1779841781.yaml
    - .agents/active/delegation-bundles/del-t4-relevance-recompute-1780556883.yaml
    - .agents/active/delegation/gcc5-verify-close-unblock.yaml
    - .agents/active/delegation/l1-followups.yaml
    - .agents/active/delegation/p1c-verifier-profile-source-aware.yaml
    - .agents/active/delegation/p1e-docs-hooks-consistency.yaml
    - .agents/active/delegation/p2-hook-scripts.yaml
    - .agents/active/delegation/p3-starter-promotion.yaml
    - .agents/active/delegation/t14-import-graph-assertion.yaml
    - .agents/active/delegation/t3-cli-readback.yaml
    - .agents/active/merge-back/gcc5-verify-close-unblock.md
    - .agents/active/merge-back/t3-cli-readback.md
    - .agents/active/verification/gcc5-verify-close-unblock/custom.result.yaml
    - .agents/active/verification/gcc5-verify-close-unblock/merge-back.result.yaml
    - .agents/active/verification/t3-cli-readback/merge-back.result.yaml
    - .agents/active/verification/t3-cli-readback/unit.result.yaml
    - .agents/lessons/index.md
    - .agents/workflow/plans/swallowed-errors-loud-atomic/PLAN.yaml
    - .agents/workflow/plans/swallowed-errors-loud-atomic/TASKS.yaml
    - .agentsrc.json
    - commands/import.go
    - commands/import_test.go
    - commands/internal/lifecycle/deps.go
    - commands/internal/lifecycle/project.go
    - commands/internal/lifecycle/project_test.go
    - commands/root.go
    - internal/config/fetcher.go
    - internal/config/fetcher_test.go
    - internal/dashboard/handlers/handlers_test.go
verification_result:
    status: pass
    summary: 'Independent slice off master, not stacked on any other swallowed-errors branch. Write scope strictly commands/internal/lifecycle/kgmcp.go + kgmcp_test.go (2 files, +70/-4). No cross-slice file overlap. Parent should review PR #355, then run workflow advance + workflow delegation closeout.'
integration_notes: 'Independent slice off master, not stacked on any other swallowed-errors branch. Write scope strictly commands/internal/lifecycle/kgmcp.go + kgmcp_test.go (2 files, +70/-4). No cross-slice file overlap. Parent should review PR #355, then run workflow advance + workflow delegation closeout.'
created_at: "2026-07-09T03:00:46Z"
---

## Summary

Fixed WriteKGMCPConfigFile's swallowed json.Unmarshal error: converted the discarded '_ = json.Unmarshal' into a checked early-return that aborts the write (returns an error, leaves the on-disk file byte-for-byte untouched) when an EXISTING claude.json/cursor.json/mcp.json is corrupt JSON. Also distinguished legitimately-absent (os.IsNotExist -> start from empty config, unchanged) from a real ReadFile error (permission denied etc -> abort, same as the corrupt-JSON case) since both are the same should-be-ATOMIC risk class. Added TestWriteKGMCPConfigFile_CorruptExistingFileAborts (core acceptance test: seeded corrupt claude.json, asserts error + untouched bytes) and TestWriteKGMCPConfigFile_UnreadableExistingFileAborts (chmod 0000, POSIX-only). Full existing kgmcp_test.go suite (merge/mkdir-error/write-error/executable-error propagation) still green. go build ./... and go test ./commands/internal/lifecycle/... both green. Committed (no AI trailer), pushed swallowed/4-kgmcp-guard, opened PR #355 base master -- https://github.com/AGOrcha/dot-agents/pull/355. STOPPED before merge per instructions.

## Integration Notes

Independent slice off master, not stacked on any other swallowed-errors branch. Write scope strictly commands/internal/lifecycle/kgmcp.go + kgmcp_test.go (2 files, +70/-4). No cross-slice file overlap. Parent should review PR #355, then run workflow advance + workflow delegation closeout.
