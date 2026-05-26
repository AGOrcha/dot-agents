---
schema_version: 1
task_id: t10pre-extract-canonical-cmd-helpers
parent_plan_id: root-command-decomposition
title: 'Precursor: lift canonicalCmdFlags + canonicalCmdExampleBlock into commands/internal/cmdutil'
summary: 'Extracted canonicalCmdFlags + canonicalCmdExampleBlock from commands/rules.go into commands/internal/cmdutil as exported CanonicalCmdFlags and CanonicalCmdExampleBlock. Updated commands/{rules,mcp,settings}.go to consume cmdutil-qualified names, plus the four package-local test files that referenced the originals (rules_test.go, mcp_test.go, settings_test.go, resource_parity_test.go). Added focused unit tests in commands/internal/cmdutil/canonical_cmd_test.go. PR #63 open against master (branch feature/t10pre-cmdutil-extract, commit 8e18f7e8). Worktree at .agents/worktrees/t10pre-extract-canonical-cmd-helpers.'
files_changed:
    - .agents/active/delegation/agents-pkg.yaml
    - .agents/active/delegation/t02-introduce-lifecycle-skeleton.yaml
    - .agents/active/delegation/t3-extract-platform-tests.yaml
verification_result:
    status: pass
    summary: 'Bundle write_scope was narrower than the actual touch surface: the four *_test.go files (commands/{mcp,settings,rules,resource_parity}_test.go) reference the originals and had to be updated to keep tests compiling per the bundle prompt''s ''tests must stay green'' clause. Touching them is in spirit of the task (rename propagation in same package), not a scope escalation; flagging here so future fanout bundles for symbol-extraction tasks include their _test.go consumers in write_scope. Verification: go build ./... clean; go test ./commands -race -count=1 PASS (91s); go test ./commands/internal/cmdutil -race -count=1 PASS (1s); go vet ./... clean; gofmt -l . empty. PR #63 must remain unmerged until t10a/b/c worktrees rebase on this branch (see TASKS.yaml for the staged-decomposition order).'
integration_notes: 'Bundle write_scope was narrower than the actual touch surface: the four *_test.go files (commands/{mcp,settings,rules,resource_parity}_test.go) reference the originals and had to be updated to keep tests compiling per the bundle prompt''s ''tests must stay green'' clause. Touching them is in spirit of the task (rename propagation in same package), not a scope escalation; flagging here so future fanout bundles for symbol-extraction tasks include their _test.go consumers in write_scope. Verification: go build ./... clean; go test ./commands -race -count=1 PASS (91s); go test ./commands/internal/cmdutil -race -count=1 PASS (1s); go vet ./... clean; gofmt -l . empty. PR #63 must remain unmerged until t10a/b/c worktrees rebase on this branch (see TASKS.yaml for the staged-decomposition order).'
created_at: "2026-05-24T23:09:35Z"
---

## Summary

Extracted canonicalCmdFlags + canonicalCmdExampleBlock from commands/rules.go into commands/internal/cmdutil as exported CanonicalCmdFlags and CanonicalCmdExampleBlock. Updated commands/{rules,mcp,settings}.go to consume cmdutil-qualified names, plus the four package-local test files that referenced the originals (rules_test.go, mcp_test.go, settings_test.go, resource_parity_test.go). Added focused unit tests in commands/internal/cmdutil/canonical_cmd_test.go. PR #63 open against master (branch feature/t10pre-cmdutil-extract, commit 8e18f7e8). Worktree at .agents/worktrees/t10pre-extract-canonical-cmd-helpers.

## Integration Notes

Bundle write_scope was narrower than the actual touch surface: the four *_test.go files (commands/{mcp,settings,rules,resource_parity}_test.go) reference the originals and had to be updated to keep tests compiling per the bundle prompt's 'tests must stay green' clause. Touching them is in spirit of the task (rename propagation in same package), not a scope escalation; flagging here so future fanout bundles for symbol-extraction tasks include their _test.go consumers in write_scope. Verification: go build ./... clean; go test ./commands -race -count=1 PASS (91s); go test ./commands/internal/cmdutil -race -count=1 PASS (1s); go vet ./... clean; gofmt -l . empty. PR #63 must remain unmerged until t10a/b/c worktrees rebase on this branch (see TASKS.yaml for the staged-decomposition order).
