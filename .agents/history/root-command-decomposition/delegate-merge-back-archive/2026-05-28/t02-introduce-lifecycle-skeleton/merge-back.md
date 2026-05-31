---
schema_version: 1
task_id: t02-introduce-lifecycle-skeleton
parent_plan_id: root-command-decomposition
title: Create commands/lifecycle/ skeleton
summary: Created commands/lifecycle/ skeleton (doc.go + deps.go) mirroring agents/skills convention; per OD-7, no NewDeps() factory. Wave-3 move tasks (t03-t09) can now branch and begin moving lifecycle commands into this package.
files_changed: []
verification_result:
    status: pass
    summary: 'PR #60 https://github.com/NikashPrakash/dot-agents/pull/60 on feature/t02-lifecycle-skeleton (commit 29ef51a5). Branch worktree at .agents/worktrees/t02-introduce-lifecycle-skeleton. Verification: go build green, go test green for commands/lifecycle ([no test files]) and parent commands package. gofmt clean. Pre-existing internal/graphstore CRG python-env test failure unrelated. Merge PR #60 before unblocking t03-t09.'
integration_notes: 'PR #60 https://github.com/NikashPrakash/dot-agents/pull/60 on feature/t02-lifecycle-skeleton (commit 29ef51a5). Branch worktree at .agents/worktrees/t02-introduce-lifecycle-skeleton. Verification: go build green, go test green for commands/lifecycle ([no test files]) and parent commands package. gofmt clean. Pre-existing internal/graphstore CRG python-env test failure unrelated. Merge PR #60 before unblocking t03-t09.'
created_at: "2026-05-24T23:06:20Z"
---

## Summary

Created commands/lifecycle/ skeleton (doc.go + deps.go) mirroring agents/skills convention; per OD-7, no NewDeps() factory. Wave-3 move tasks (t03-t09) can now branch and begin moving lifecycle commands into this package.

## Integration Notes

PR #60 https://github.com/NikashPrakash/dot-agents/pull/60 on feature/t02-lifecycle-skeleton (commit 29ef51a5). Branch worktree at .agents/worktrees/t02-introduce-lifecycle-skeleton. Verification: go build green, go test green for commands/lifecycle ([no test files]) and parent commands package. gofmt clean. Pre-existing internal/graphstore CRG python-env test failure unrelated. Merge PR #60 before unblocking t03-t09.
