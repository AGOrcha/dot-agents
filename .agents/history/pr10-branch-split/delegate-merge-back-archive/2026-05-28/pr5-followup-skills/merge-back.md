---
schema_version: 1
task_id: pr5-followup-skills
parent_plan_id: pr10-branch-split
title: 'PR5-followup-A: promote 4 remaining orchestration skills to global (orchestrator-session-start, delegation-lifecycle, plan-wave-picker, provider-consumer-pair)'
summary: 'Promoted 4 orchestration skills (orchestrator-session-start, delegation-lifecycle, plan-wave-picker, provider-consumer-pair) from ~/.agents/skills/dot-agents/ to internal/scaffold/home/starter/skills/global/. Abstracted project-coupled refs (LOOP_ORCHESTRATION_SPEC.md, history/loop-runtime-refactor/, KG-specific plan filenames, flat-commands/ note, go run ./cmd/dot-agents fallbacks) into generic placeholders. embed.FS walks new dirs without manifest change. PR #141 opened (gh url: https://github.com/NikashPrakash/dot-agents/pull/141).'
files_changed:
    - .agents/active/delegation-bundles/del-p3-starter-promotion-1779841783.yaml
    - .agents/active/delegation-bundles/del-t14-import-graph-assertion-1779841818.yaml
    - .agents/active/delegation/p3-starter-promotion.yaml
    - .agents/active/delegation/t14-import-graph-assertion.yaml
    - .agents/workflow/plans/coverage-gate-per-file/PLAN.yaml
    - .agents/workflow/plans/coverage-gate-per-file/TASKS.yaml
    - .agents/workflow/plans/loop-discipline-stop-hooks/PLAN.yaml
    - .agents/workflow/plans/loop-discipline-stop-hooks/TASKS.yaml
    - .agents/workflow/plans/pr10-branch-split/PLAN.yaml
    - .agents/workflow/plans/pr10-branch-split/TASKS.yaml
    - .agents/workflow/plans/root-command-decomposition/PLAN.yaml
    - .agents/workflow/plans/root-command-decomposition/TASKS.yaml
verification_result:
    status: pass
    summary: 'copy_test.go assertions for the 4 new skills are owned by sibling delegation pr5-followup-verifier-xref; this delegation intentionally leaves that file untouched. Pre-push sonar-scanner step fails locally on macOS worktrees (JGit can''t find .git/worktrees/<name>) so SKIP=sonar-scanner was used per bundle guidance; CI runs sonar cleanly on the merge commit. Parent: workflow delegation closeout --decision accept after PR review.'
integration_notes: 'copy_test.go assertions for the 4 new skills are owned by sibling delegation pr5-followup-verifier-xref; this delegation intentionally leaves that file untouched. Pre-push sonar-scanner step fails locally on macOS worktrees (JGit can''t find .git/worktrees/<name>) so SKIP=sonar-scanner was used per bundle guidance; CI runs sonar cleanly on the merge commit. Parent: workflow delegation closeout --decision accept after PR review.'
created_at: "2026-05-28T02:28:28Z"
---

## Summary

Promoted 4 orchestration skills (orchestrator-session-start, delegation-lifecycle, plan-wave-picker, provider-consumer-pair) from ~/.agents/skills/dot-agents/ to internal/scaffold/home/starter/skills/global/. Abstracted project-coupled refs (LOOP_ORCHESTRATION_SPEC.md, history/loop-runtime-refactor/, KG-specific plan filenames, flat-commands/ note, go run ./cmd/dot-agents fallbacks) into generic placeholders. embed.FS walks new dirs without manifest change. PR #141 opened (gh url: https://github.com/NikashPrakash/dot-agents/pull/141).

## Integration Notes

copy_test.go assertions for the 4 new skills are owned by sibling delegation pr5-followup-verifier-xref; this delegation intentionally leaves that file untouched. Pre-push sonar-scanner step fails locally on macOS worktrees (JGit can't find .git/worktrees/<name>) so SKIP=sonar-scanner was used per bundle guidance; CI runs sonar cleanly on the merge commit. Parent: workflow delegation closeout --decision accept after PR review.
