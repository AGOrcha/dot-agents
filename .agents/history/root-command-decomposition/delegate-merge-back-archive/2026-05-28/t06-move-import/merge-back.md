---
schema_version: 1
task_id: t06-move-import
parent_plan_id: root-command-decomposition
title: Move import command + import_plugins into commands/lifecycle/
summary: 'Pre-edit stop. t06 import move blocked by cross-file helper coupling: import.go uses unexported helpers from add.go (mirrorBackup, isBackupArtifact, isManagedProjectOutput) and refresh.go (mapResourceRelToDest). Lifecycle subpackage cannot reach them without an import cycle, and lifecycle.Deps does not expose them; deps.go is outside the t06 write_scope. Worker stopped before any file edit to avoid scope expansion. Worktree (.agents/worktrees/t06, branch feature/t06-move-import) created off master at dc60a9bb and remains clean. Fold-back observation t06-import-helper-coupling recorded against task t06-move-import with three resolution options: (a) extend lifecycle.Deps with helper function fields, (b) duplicate the four helpers into commands/lifecycle/import.go as transitional copies (OD-2 style), (c) re-order to gate t06 on t04 (add) and t07 (refresh) completion and call lifecycle.mirrorBackup etc. directly. No PR opened. No commits.'
files_changed:
    - .agents/active/delegation/fsops-windows-tests.yaml
    - .agents/workflow/plans/root-command-decomposition/TASKS.yaml
verification_result:
    status: partial
    summary: Parent must (1) choose a resolution path for the helper-coupling fold-back before re-dispatching t06, (2) leave task t06-move-import in pending status and do NOT advance, (3) consider whether t04 and t07 should be reordered to merge before t06 or whether lifecycle.Deps should be extended in a small follow-up task. The worktree at .agents/worktrees/t06 can be removed (git worktree remove) or kept for the re-dispatch.
integration_notes: Parent must (1) choose a resolution path for the helper-coupling fold-back before re-dispatching t06, (2) leave task t06-move-import in pending status and do NOT advance, (3) consider whether t04 and t07 should be reordered to merge before t06 or whether lifecycle.Deps should be extended in a small follow-up task. The worktree at .agents/worktrees/t06 can be removed (git worktree remove) or kept for the re-dispatch.
created_at: "2026-05-25T14:00:39Z"
---

## Summary

Pre-edit stop. t06 import move blocked by cross-file helper coupling: import.go uses unexported helpers from add.go (mirrorBackup, isBackupArtifact, isManagedProjectOutput) and refresh.go (mapResourceRelToDest). Lifecycle subpackage cannot reach them without an import cycle, and lifecycle.Deps does not expose them; deps.go is outside the t06 write_scope. Worker stopped before any file edit to avoid scope expansion. Worktree (.agents/worktrees/t06, branch feature/t06-move-import) created off master at dc60a9bb and remains clean. Fold-back observation t06-import-helper-coupling recorded against task t06-move-import with three resolution options: (a) extend lifecycle.Deps with helper function fields, (b) duplicate the four helpers into commands/lifecycle/import.go as transitional copies (OD-2 style), (c) re-order to gate t06 on t04 (add) and t07 (refresh) completion and call lifecycle.mirrorBackup etc. directly. No PR opened. No commits.

## Integration Notes

Parent must (1) choose a resolution path for the helper-coupling fold-back before re-dispatching t06, (2) leave task t06-move-import in pending status and do NOT advance, (3) consider whether t04 and t07 should be reordered to merge before t06 or whether lifecycle.Deps should be extended in a small follow-up task. The worktree at .agents/worktrees/t06 can be removed (git worktree remove) or kept for the re-dispatch.
