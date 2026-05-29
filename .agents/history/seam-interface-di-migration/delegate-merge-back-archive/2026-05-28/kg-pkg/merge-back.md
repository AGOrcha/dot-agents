---
schema_version: 1
task_id: kg-pkg
parent_plan_id: seam-interface-di-migration
title: Convert commands/kg/seams.go to interface-DI
summary: 'Converted commands/kg/seams.go (7 func-var seams) to kgIO interface-DI per docs/TEST_SEAMS.md and the platform-pkg / commands/add.go references. Added commands/kg/io.go (kgIO + stdKGIO) and io_test.go (fakeKGIO with nil-delegates-to-real semantics + with*Error builders); deleted seams.go entirely. Threaded io kgIO through every kg free function that touched a seam plus their callers up to the Cobra handlers (which read deps.IO via kgIOFrom). Deps gained an IO kgIO field that NewKGCmd wires to stdKGIO{}. runKGSync split into a thin wrapper plus runKGSyncIO so its post-pull lint-error branch stays testable with a fake. PR #67 https://github.com/NikashPrakash/dot-agents/pull/67. Two commits: 88d88e17 production refactor, d72b1c7c test conversion. Verification: go test ./commands/kg -race -count=1 ok 15.9s; go vet, gofmt -l, go build ./... all clean. Pre-existing TestCRGBridgeFreshBuildRealCRG (missing python module) still failing — unrelated and explicitly acceptable per the seam-atomic-convergence merge-back precedent.'
files_changed:
    - .agents/active/delegation/agents-pkg.yaml
    - .agents/active/delegation/gcc4-regression-close-od1.yaml
    - .agents/active/delegation/skills-pkg.yaml
    - .agents/active/delegation/t02-introduce-lifecycle-skeleton.yaml
    - .agents/active/delegation/t10pre-extract-canonical-cmd-helpers.yaml
    - .agents/active/delegation/t3-extract-platform-tests.yaml
verification_result:
    status: pass
    summary: 'PR is open, do NOT merge per delegation bundle. Concurrent fanout workers in sibling worktrees (skills-pkg, agents-pkg, wc-path-derivation, t3, gcc4, t02, t10pre) — scope commands/kg/ does not overlap any of them. Pattern set here mirrors PR #59 (platform-pkg) exactly — a sibling skills-pkg / agents-pkg worker can model their conversion on this PR. Note for next conversions in the package: kgIO is one role-named interface for the whole package (not per-file) consistent with the convention rule ''convert the whole package as one unit''.'
integration_notes: 'PR is open, do NOT merge per delegation bundle. Concurrent fanout workers in sibling worktrees (skills-pkg, agents-pkg, wc-path-derivation, t3, gcc4, t02, t10pre) — scope commands/kg/ does not overlap any of them. Pattern set here mirrors PR #59 (platform-pkg) exactly — a sibling skills-pkg / agents-pkg worker can model their conversion on this PR. Note for next conversions in the package: kgIO is one role-named interface for the whole package (not per-file) consistent with the convention rule ''convert the whole package as one unit''.'
created_at: "2026-05-24T23:31:51Z"
---

## Summary

Converted commands/kg/seams.go (7 func-var seams) to kgIO interface-DI per docs/TEST_SEAMS.md and the platform-pkg / commands/add.go references. Added commands/kg/io.go (kgIO + stdKGIO) and io_test.go (fakeKGIO with nil-delegates-to-real semantics + with*Error builders); deleted seams.go entirely. Threaded io kgIO through every kg free function that touched a seam plus their callers up to the Cobra handlers (which read deps.IO via kgIOFrom). Deps gained an IO kgIO field that NewKGCmd wires to stdKGIO{}. runKGSync split into a thin wrapper plus runKGSyncIO so its post-pull lint-error branch stays testable with a fake. PR #67 https://github.com/NikashPrakash/dot-agents/pull/67. Two commits: 88d88e17 production refactor, d72b1c7c test conversion. Verification: go test ./commands/kg -race -count=1 ok 15.9s; go vet, gofmt -l, go build ./... all clean. Pre-existing TestCRGBridgeFreshBuildRealCRG (missing python module) still failing — unrelated and explicitly acceptable per the seam-atomic-convergence merge-back precedent.

## Integration Notes

PR is open, do NOT merge per delegation bundle. Concurrent fanout workers in sibling worktrees (skills-pkg, agents-pkg, wc-path-derivation, t3, gcc4, t02, t10pre) — scope commands/kg/ does not overlap any of them. Pattern set here mirrors PR #59 (platform-pkg) exactly — a sibling skills-pkg / agents-pkg worker can model their conversion on this PR. Note for next conversions in the package: kgIO is one role-named interface for the whole package (not per-file) consistent with the convention rule 'convert the whole package as one unit'.
