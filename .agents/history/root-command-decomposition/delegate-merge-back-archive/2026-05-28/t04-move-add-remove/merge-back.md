---
schema_version: 1
task_id: t04-move-add-remove
parent_plan_id: root-command-decomposition
title: Move add and remove commands into commands/lifecycle/
summary: 't04 move blocked by cross-file helper coupling not declared in TASKS.yaml depends_on. Fold-back observation written at .agents/active/fold-back/t04-add-remove-helper-coupling.yaml mirroring the t06 fold-back pattern. add.go has INBOUND coupling (stdAddDeps/addDeps/mirrorBackup/isBackupArtifact/isManagedProjectOutput/ensureGlobalKGMCPConfigs/restoreFromResourcesCountedWithDeps consumed by refresh.go/import.go/init.go/review.go/sync.go) and OUTBOUND coupling (relCursorRulesDir/agentsHooksPrefix/isManagedSymlink/mapResourceRelToDest/importCandidate/canonicalImportOutputs/hasMultipleHardLinks defined in import.go/refresh.go/linkcount). remove.go is smaller but still has dirCleaner consumed by init.go and runRemove by 3 cross-cutting tests. Three resolutions proposed: (a) extend lifecycle.Deps with helper callbacks (still leaves inbound breakage), (b) duplicate ~250 LOC of helpers into lifecycle/, (c) re-order with a t02b precursor that lifts shared helpers (matches t10pre precedent). Worker stopped before any commands/ or commands/lifecycle/ edits. No PR opened. Worktree .agents/worktrees/t04 / branch feature/t04-move-add-remove at master HEAD dc60a9bb.'
files_changed:
    - .agents/active/delegation/fsops-windows-tests.yaml
    - .agents/active/delegation/t06-move-import.yaml
    - .agents/workflow/plans/root-command-decomposition/TASKS.yaml
verification_result:
    status: partial
    summary: 'Parent should NOT run workflow advance for t04 -- task remains pending. Reconcile fold-back routed_to (task_note:root-command-decomposition/t04-move-add-remove) before respawning a t04 worker. Recommended next step: spawn a t02b precursor worker per resolution (c), or extend lifecycle.Deps (resolution a) as a one-shot infra task, then re-fanout t04+t05+t06+t07+t08+t09 against the new helper home. Worktree can be removed (git worktree remove .agents/worktrees/t04) or kept for the respawned worker.'
integration_notes: 'Parent should NOT run workflow advance for t04 -- task remains pending. Reconcile fold-back routed_to (task_note:root-command-decomposition/t04-move-add-remove) before respawning a t04 worker. Recommended next step: spawn a t02b precursor worker per resolution (c), or extend lifecycle.Deps (resolution a) as a one-shot infra task, then re-fanout t04+t05+t06+t07+t08+t09 against the new helper home. Worktree can be removed (git worktree remove .agents/worktrees/t04) or kept for the respawned worker.'
created_at: "2026-05-25T14:03:34Z"
---

## Summary

t04 move blocked by cross-file helper coupling not declared in TASKS.yaml depends_on. Fold-back observation written at .agents/active/fold-back/t04-add-remove-helper-coupling.yaml mirroring the t06 fold-back pattern. add.go has INBOUND coupling (stdAddDeps/addDeps/mirrorBackup/isBackupArtifact/isManagedProjectOutput/ensureGlobalKGMCPConfigs/restoreFromResourcesCountedWithDeps consumed by refresh.go/import.go/init.go/review.go/sync.go) and OUTBOUND coupling (relCursorRulesDir/agentsHooksPrefix/isManagedSymlink/mapResourceRelToDest/importCandidate/canonicalImportOutputs/hasMultipleHardLinks defined in import.go/refresh.go/linkcount). remove.go is smaller but still has dirCleaner consumed by init.go and runRemove by 3 cross-cutting tests. Three resolutions proposed: (a) extend lifecycle.Deps with helper callbacks (still leaves inbound breakage), (b) duplicate ~250 LOC of helpers into lifecycle/, (c) re-order with a t02b precursor that lifts shared helpers (matches t10pre precedent). Worker stopped before any commands/ or commands/lifecycle/ edits. No PR opened. Worktree .agents/worktrees/t04 / branch feature/t04-move-add-remove at master HEAD dc60a9bb.

## Integration Notes

Parent should NOT run workflow advance for t04 -- task remains pending. Reconcile fold-back routed_to (task_note:root-command-decomposition/t04-move-add-remove) before respawning a t04 worker. Recommended next step: spawn a t02b precursor worker per resolution (c), or extend lifecycle.Deps (resolution a) as a one-shot infra task, then re-fanout t04+t05+t06+t07+t08+t09 against the new helper home. Worktree can be removed (git worktree remove .agents/worktrees/t04) or kept for the respawned worker.
