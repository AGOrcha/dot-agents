---
schema_version: 1
task_id: p3-badge-and-countlinks
parent_plan_id: platform-driven-diagnostics
title: 'P3: Badge + CountLinks across 5 platforms'
summary: 'P3: Badge() + CountLinks() implemented across cursor, claude, codex, copilot, opencode (StatusBadger + LinkCounter sister interfaces). status.go''s 5 *TextBadge helpers + collectProjectPlatforms collapsed to a single platform.All() iterator — text + JSON now share one source of truth. HasMultipleHardLinks relocated to internal/platform/claude_linkcount_{unix,windows}.go (absorbed P1 fold-back per .agents/active/fold-back/p1-hasmultiplehardlinks-move-deferred-to-p3.md); lifecycle seam wraps platform export so backup-safety + status badge math cannot diverge. PR #135 READY: coverage gate, SonarCloud, macOS/Linux/Windows tests all green. CLI byte-identical pre/post for status, status --json, doctor --json.'
files_changed:
    - .agents/workflow/plans/coverage-gate-per-file/PLAN.yaml
    - .agents/workflow/plans/coverage-gate-per-file/TASKS.yaml
    - .agents/workflow/plans/cross-platform-test-skips-audit/PLAN.yaml
    - .agents/workflow/plans/cross-platform-test-skips-audit/TASKS.yaml
    - .agents/workflow/plans/loop-discipline-stop-hooks/PLAN.yaml
    - .agents/workflow/plans/loop-discipline-stop-hooks/TASKS.yaml
    - .agents/workflow/plans/production-code-helper-extraction/PLAN.yaml
    - .agents/workflow/plans/production-code-helper-extraction/TASKS.yaml
    - .agents/workflow/plans/r1-5-hook-enforcement-telemetry/PLAN.yaml
    - .agents/workflow/plans/r1-5-hook-enforcement-telemetry/TASKS.yaml
    - .agents/workflow/plans/r3-background-worker-service/PLAN.yaml
    - .agents/workflow/plans/r3-background-worker-service/TASKS.yaml
    - .agents/workflow/plans/refresh-skill-relink/PLAN.yaml
    - .agents/workflow/plans/refresh-skill-relink/TASKS.yaml
    - .agents/workflow/plans/shared-target-projection-wiring/TASKS.yaml
    - .agents/workflow/specs/coverage-gate-per-file/design.md
    - .agents/workflow/specs/production-code-helper-extraction/design.md
verification_result:
    status: pass
    summary: 'Worker self-monitored CI per [[worker-owns-pr-readiness-loop]]: initial run flagged coverage gate (claude_linkcount_*.go 0% — no platform-pkg test) and Sonar; second push added TestHasMultipleHardLinks_PlatformPkg + TestClaudeCountRules_ManagedFileBranch + TestAddManagedFileCounts_BrokenSymlink + TestAddManagedDirCounts_MixedEntries to drive coverage above 95%, and swapped allowlist entries (commands/lifecycle/linkcount_*.go → internal/platform/claude_linkcount_*.go) preserving the same ratchet count. Parent can advance — PR is ready to merge. P3 complete; P4 (orphan canonicals + userconfig) unblocked.'
integration_notes: 'Worker self-monitored CI per [[worker-owns-pr-readiness-loop]]: initial run flagged coverage gate (claude_linkcount_*.go 0% — no platform-pkg test) and Sonar; second push added TestHasMultipleHardLinks_PlatformPkg + TestClaudeCountRules_ManagedFileBranch + TestAddManagedFileCounts_BrokenSymlink + TestAddManagedDirCounts_MixedEntries to drive coverage above 95%, and swapped allowlist entries (commands/lifecycle/linkcount_*.go → internal/platform/claude_linkcount_*.go) preserving the same ratchet count. Parent can advance — PR is ready to merge. P3 complete; P4 (orphan canonicals + userconfig) unblocked.'
created_at: "2026-05-27T23:56:51Z"
---

## Summary

P3: Badge() + CountLinks() implemented across cursor, claude, codex, copilot, opencode (StatusBadger + LinkCounter sister interfaces). status.go's 5 *TextBadge helpers + collectProjectPlatforms collapsed to a single platform.All() iterator — text + JSON now share one source of truth. HasMultipleHardLinks relocated to internal/platform/claude_linkcount_{unix,windows}.go (absorbed P1 fold-back per .agents/active/fold-back/p1-hasmultiplehardlinks-move-deferred-to-p3.md); lifecycle seam wraps platform export so backup-safety + status badge math cannot diverge. PR #135 READY: coverage gate, SonarCloud, macOS/Linux/Windows tests all green. CLI byte-identical pre/post for status, status --json, doctor --json.

## Integration Notes

Worker self-monitored CI per [[worker-owns-pr-readiness-loop]]: initial run flagged coverage gate (claude_linkcount_*.go 0% — no platform-pkg test) and Sonar; second push added TestHasMultipleHardLinks_PlatformPkg + TestClaudeCountRules_ManagedFileBranch + TestAddManagedFileCounts_BrokenSymlink + TestAddManagedDirCounts_MixedEntries to drive coverage above 95%, and swapped allowlist entries (commands/lifecycle/linkcount_*.go → internal/platform/claude_linkcount_*.go) preserving the same ratchet count. Parent can advance — PR is ready to merge. P3 complete; P4 (orphan canonicals + userconfig) unblocked.
