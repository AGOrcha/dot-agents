---
schema_version: 1
task_id: establish-toolchain-and-built-binary-baseline
parent_plan_id: ci-smoke-suite-hardening
title: Add toolchain hygiene and built-binary baseline verification to CI
summary: Added gofmt -l check step, go vet step, binary authenticity check (file cmd validates real Go binary not shim), and 4 extended smoke steps (workflow status, workflow health, workflow plan, skills list) to .github/workflows/test.yml. Also formatted 5 Go files (internal/graphstore/crg.go, sqlite.go, sqlite_test.go, store.go, internal/platform/opencode.go) that the new gofmt check caught.
files_changed:
    - .agents/workflow/plans/ci-smoke-suite-hardening/TASKS.yaml
verification_result:
    status: pass
    summary: No conflicts. All changes are additive steps in the test job. gofmt fix-up commits are on the same branch.
integration_notes: No conflicts. All changes are additive steps in the test job. gofmt fix-up commits are on the same branch.
created_at: "2026-04-19T20:07:37Z"
---

## Summary

Added gofmt -l check step, go vet step, binary authenticity check (file cmd validates real Go binary not shim), and 4 extended smoke steps (workflow status, workflow health, workflow plan, skills list) to .github/workflows/test.yml. Also formatted 5 Go files (internal/graphstore/crg.go, sqlite.go, sqlite_test.go, store.go, internal/platform/opencode.go) that the new gofmt check caught.

## Integration Notes

No conflicts. All changes are additive steps in the test job. gofmt fix-up commits are on the same branch.
