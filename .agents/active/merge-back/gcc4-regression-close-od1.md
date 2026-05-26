---
schema_version: 1
task_id: gcc4-regression-close-od1
parent_plan_id: graphstore-concurrency-contract
title: Regression (hard-cap + cross-path parity + timeout); close maxNodes Low-1 & OD-1
summary: 'gcc4: route postgres reads/execs through requestContext (gcc2-deferred) + add the contract regression suite that closes OD-1 and maxNodes Low-1 via the chokepoint. PR #66 open on feature/gcc4-regression-close-od1.'
files_changed:
    - .agents/active/delegation/agents-pkg.yaml
    - .agents/active/delegation/skills-pkg.yaml
    - .agents/active/delegation/t02-introduce-lifecycle-skeleton.yaml
    - .agents/active/delegation/t10pre-extract-canonical-cmd-helpers.yaml
    - .agents/active/delegation/t3-extract-platform-tests.yaml
verification_result:
    status: pass
    summary: 'Parent: review PR #66 and merge before invoking gcc5. Write scope was internal/graphstore/ only (OD-1 rationale comment already in commands/workflow/deps.go from gcc3). Pre-existing TestCRGBridgeFreshBuildRealCRG env failure confirmed unrelated. Postgres parity tests skip cleanly when Docker is unavailable; in-package regressions run unconditionally and carry the load-bearing OD-1 + Low-1 proof.'
integration_notes: 'Parent: review PR #66 and merge before invoking gcc5. Write scope was internal/graphstore/ only (OD-1 rationale comment already in commands/workflow/deps.go from gcc3). Pre-existing TestCRGBridgeFreshBuildRealCRG env failure confirmed unrelated. Postgres parity tests skip cleanly when Docker is unavailable; in-package regressions run unconditionally and carry the load-bearing OD-1 + Low-1 proof.'
created_at: "2026-05-24T23:14:50Z"
---

## Summary

gcc4: route postgres reads/execs through requestContext (gcc2-deferred) + add the contract regression suite that closes OD-1 and maxNodes Low-1 via the chokepoint. PR #66 open on feature/gcc4-regression-close-od1.

## Integration Notes

Parent: review PR #66 and merge before invoking gcc5. Write scope was internal/graphstore/ only (OD-1 rationale comment already in commands/workflow/deps.go from gcc3). Pre-existing TestCRGBridgeFreshBuildRealCRG env failure confirmed unrelated. Postgres parity tests skip cleanly when Docker is unavailable; in-package regressions run unconditionally and carry the load-bearing OD-1 + Low-1 proof.
