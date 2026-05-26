---
schema_version: 1
task_id: agents-pkg
parent_plan_id: seam-interface-di-migration
title: Convert commands/agents/seams.go to interface-DI
summary: 'Converted commands/agents/seams.go osReadlink func-var to readlinker interface-DI. ensureImportRepoAgentsSlot + cleanupManagedAgentRepoPath take readlinker as explicit param; ImportAgentIn/RemoveAgentIn pass stdReadlinker{}. Tests use fakeReadlinker with nil-delegates-to-real convention. Added TestStdReadlinker_DelegatesToOSReadlink for prod-impl coverage. Smallest of the 4 seam leaves (1 seam). PR #61: https://github.com/NikashPrakash/dot-agents/pull/61. Commit: 91e33813.'
files_changed:
    - .agents/active/delegation/t02-introduce-lifecycle-skeleton.yaml
verification_result:
    status: pass
    summary: Behavior-preserving. All commands/agents tests pass under -race. Branch feature/agents-pkg-interface-di pushed. Do NOT merge — parent reviews; concurrent worktrees kg-pkg/skills-pkg/platform-pkg/etc have no overlap with commands/agents/. Pre-existing unrelated test failure in internal/graphstore TestCRGBridgeFreshBuildRealCRG (missing code_review_graph python module) is environment, not introduced by this change.
integration_notes: Behavior-preserving. All commands/agents tests pass under -race. Branch feature/agents-pkg-interface-di pushed. Do NOT merge — parent reviews; concurrent worktrees kg-pkg/skills-pkg/platform-pkg/etc have no overlap with commands/agents/. Pre-existing unrelated test failure in internal/graphstore TestCRGBridgeFreshBuildRealCRG (missing code_review_graph python module) is environment, not introduced by this change.
created_at: "2026-05-24T23:08:26Z"
---

## Summary

Converted commands/agents/seams.go osReadlink func-var to readlinker interface-DI. ensureImportRepoAgentsSlot + cleanupManagedAgentRepoPath take readlinker as explicit param; ImportAgentIn/RemoveAgentIn pass stdReadlinker{}. Tests use fakeReadlinker with nil-delegates-to-real convention. Added TestStdReadlinker_DelegatesToOSReadlink for prod-impl coverage. Smallest of the 4 seam leaves (1 seam). PR #61: https://github.com/NikashPrakash/dot-agents/pull/61. Commit: 91e33813.

## Integration Notes

Behavior-preserving. All commands/agents tests pass under -race. Branch feature/agents-pkg-interface-di pushed. Do NOT merge — parent reviews; concurrent worktrees kg-pkg/skills-pkg/platform-pkg/etc have no overlap with commands/agents/. Pre-existing unrelated test failure in internal/graphstore TestCRGBridgeFreshBuildRealCRG (missing code_review_graph python module) is environment, not introduced by this change.
