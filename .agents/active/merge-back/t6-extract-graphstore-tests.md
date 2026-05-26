---
schema_version: 1
task_id: t6-extract-graphstore-tests
parent_plan_id: go-test-fixture-extraction
title: Refactor internal/graphstore/*_test.go to use internal/testutil
summary: 'Partial closure of t6. mcp_server_test.go refactored with 2 local helpers (runMCPCallExpectErrorCode, decodeResultMap) collapsing 10 dup call-sites. sqlite_test.go + postgres_test.go remain unmigrated: cross-file dup requires extending internal/graphstore/internal/storetest with new runners (out of bundle write_scope). Fold-back filed recommending t6b-extend-storetest task.'
files_changed:
    - .agents/active/delegation/t0-outcome-contract.yaml
verification_result:
    status: partial
    summary: 'PR https://github.com/NikashPrakash/dot-agents/pull/84 (commit 17fbad8a). Worktree: .agents/worktrees/t6-extract-graphstore-tests. Branch: feature/t6-graphstore-tests-testutil. Do NOT merge until orchestrator picks fold-back decision (see .agents/active/fold-back/t6-sqlite-postgres-need-storetest-extension.md). Verification: go build, go vet, gofmt clean; go test ./internal/graphstore -race -count=1 -skip TestCRGBridge PASS (16.7s). TestCRGBridge* is pre-existing env-dependent (code-review-graph Python module not on PATH), unrelated to this refactor.'
integration_notes: 'PR https://github.com/NikashPrakash/dot-agents/pull/84 (commit 17fbad8a). Worktree: .agents/worktrees/t6-extract-graphstore-tests. Branch: feature/t6-graphstore-tests-testutil. Do NOT merge until orchestrator picks fold-back decision (see .agents/active/fold-back/t6-sqlite-postgres-need-storetest-extension.md). Verification: go build, go vet, gofmt clean; go test ./internal/graphstore -race -count=1 -skip TestCRGBridge PASS (16.7s). TestCRGBridge* is pre-existing env-dependent (code-review-graph Python module not on PATH), unrelated to this refactor.'
created_at: "2026-05-26T11:46:25Z"
---

## Summary

Partial closure of t6. mcp_server_test.go refactored with 2 local helpers (runMCPCallExpectErrorCode, decodeResultMap) collapsing 10 dup call-sites. sqlite_test.go + postgres_test.go remain unmigrated: cross-file dup requires extending internal/graphstore/internal/storetest with new runners (out of bundle write_scope). Fold-back filed recommending t6b-extend-storetest task.

## Integration Notes

PR https://github.com/NikashPrakash/dot-agents/pull/84 (commit 17fbad8a). Worktree: .agents/worktrees/t6-extract-graphstore-tests. Branch: feature/t6-graphstore-tests-testutil. Do NOT merge until orchestrator picks fold-back decision (see .agents/active/fold-back/t6-sqlite-postgres-need-storetest-extension.md). Verification: go build, go vet, gofmt clean; go test ./internal/graphstore -race -count=1 -skip TestCRGBridge PASS (16.7s). TestCRGBridge* is pre-existing env-dependent (code-review-graph Python module not on PATH), unrelated to this refactor.
