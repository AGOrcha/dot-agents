---
schema_version: 1
task_id: t3-extract-platform-tests
parent_plan_id: go-test-fixture-extraction
title: Refactor internal/platform/*_test.go to use internal/testutil
summary: 'Refactored 3 internal/platform/*_test.go to consume internal/testutil. 3 commits on feature/t3-platform-tests-testutil, PR #62. Per-file deltas: rules_test.go (5 call sites migrated), mcp_settings_test.go (4 call sites migrated), resource_plan_test.go (-43 LOC: 4 fixture-helper bodies rewritten to delegate to testutil.WriteScopeFilePath; 2 inline blocks collapsed). Verification on tip 2bc2acaf: go build clean; go test ./internal/platform -race -count=1 PASS (2.615s); go vet clean; gofmt -l clean. Tests-only; zero production-code edits.'
files_changed:
    - .agents/active/delegation/agents-pkg.yaml
    - .agents/active/delegation/t02-introduce-lifecycle-skeleton.yaml
verification_result:
    status: pass
    summary: 'Three findings for orchestrator triage: (1) [LOW] helpers_test.go::writeScopeFile is now orphan after this PR (last 2 callers migrated). File is outside this task''s write_scope; add to t8 sweep so the 18-line file can be deleted. (2) [MEDIUM] testutil-extension wanted: 5 sites in resource_plan_test.go share a 5-line tmp/repo/agentsHome/MkdirAll(skills/proj)/Setenv preamble that no existing helper covers (NewTempAgentsHome skips the repo path + empty-bucket-dir mkdir). Recommend a NewTempProjectRoots(t, bucketScopes...) extension; same t5.5 candidate the t5 merge-back flagged. (3) [LOW] resource_plan_test.go retains a few inline fixture blocks that mix repo+canonical writes with intent-specific fields — defer to a future fluent-builder helper if duplication remains visible after t8.'
integration_notes: 'Three findings for orchestrator triage: (1) [LOW] helpers_test.go::writeScopeFile is now orphan after this PR (last 2 callers migrated). File is outside this task''s write_scope; add to t8 sweep so the 18-line file can be deleted. (2) [MEDIUM] testutil-extension wanted: 5 sites in resource_plan_test.go share a 5-line tmp/repo/agentsHome/MkdirAll(skills/proj)/Setenv preamble that no existing helper covers (NewTempAgentsHome skips the repo path + empty-bucket-dir mkdir). Recommend a NewTempProjectRoots(t, bucketScopes...) extension; same t5.5 candidate the t5 merge-back flagged. (3) [LOW] resource_plan_test.go retains a few inline fixture blocks that mix repo+canonical writes with intent-specific fields — defer to a future fluent-builder helper if duplication remains visible after t8.'
created_at: "2026-05-24T23:09:04Z"
---

## Summary

Refactored 3 internal/platform/*_test.go to consume internal/testutil. 3 commits on feature/t3-platform-tests-testutil, PR #62. Per-file deltas: rules_test.go (5 call sites migrated), mcp_settings_test.go (4 call sites migrated), resource_plan_test.go (-43 LOC: 4 fixture-helper bodies rewritten to delegate to testutil.WriteScopeFilePath; 2 inline blocks collapsed). Verification on tip 2bc2acaf: go build clean; go test ./internal/platform -race -count=1 PASS (2.615s); go vet clean; gofmt -l clean. Tests-only; zero production-code edits.

## Integration Notes

Three findings for orchestrator triage: (1) [LOW] helpers_test.go::writeScopeFile is now orphan after this PR (last 2 callers migrated). File is outside this task's write_scope; add to t8 sweep so the 18-line file can be deleted. (2) [MEDIUM] testutil-extension wanted: 5 sites in resource_plan_test.go share a 5-line tmp/repo/agentsHome/MkdirAll(skills/proj)/Setenv preamble that no existing helper covers (NewTempAgentsHome skips the repo path + empty-bucket-dir mkdir). Recommend a NewTempProjectRoots(t, bucketScopes...) extension; same t5.5 candidate the t5 merge-back flagged. (3) [LOW] resource_plan_test.go retains a few inline fixture blocks that mix repo+canonical writes with intent-specific fields — defer to a future fluent-builder helper if duplication remains visible after t8.
