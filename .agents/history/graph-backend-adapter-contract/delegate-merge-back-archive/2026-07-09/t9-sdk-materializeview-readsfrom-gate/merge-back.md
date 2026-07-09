---
schema_version: 1
task_id: t9-sdk-materializeview-readsfrom-gate
parent_plan_id: graph-backend-adapter-contract
title: Wire reads_from enforcement into SDK.MaterializeView
summary: 'Wired a ReadsFromGate parameter into SDK.MaterializeView (satisfied structurally by *registry.Registry.ValidateReadsFrom — the same §11.2 migration_only rule EnforceReadsFrom applies at adapter-load time, t4) so every materialize_view call now consults the registry before the runner executes or anything writes; fail-closed when no gate is supplied. Added internal/adapters/sdk/readsfrom_gate_test.go with an integration test against a real registry.Registry: a view reading a migration_only adapter is rejected (runner never runs, nothing persists), a legitimate view still succeeds end-to-end, and a nil gate is rejected. Threaded an allowAllGate stub through the 4 pre-existing MaterializeView mechanics tests in sdk_test.go so they keep testing token/store plumbing unchanged. Mutation-verified: temporarily stripped the gate.ValidateReadsFrom call, reran the gate tests -> 2 failed as expected, restored -> green. go build ./... clean repo-wide (MaterializeView had zero production callers before this change, so the signature change is zero-blast-radius). PR #360 open: https://github.com/AGOrcha/dot-agents/pull/360 (base master, head feat/t9-sdk-readsfrom-gate) — stopped before merge per the delegation contract.'
files_changed:
    - .agents/active/delegation-bundles/del-cg-project-local-overlay-1780781079.yaml
    - .agents/active/delegation-bundles/del-gcc5-verify-close-unblock-1779848143.yaml
    - .agents/active/delegation-bundles/del-l1-followups-1782677806.yaml
    - .agents/active/delegation-bundles/del-p1c-verifier-profile-source-aware-1780784015.yaml
    - .agents/active/delegation-bundles/del-p1e-docs-hooks-consistency-1779841818.yaml
    - .agents/active/delegation-bundles/del-p2-hook-scripts-1779841782.yaml
    - .agents/active/delegation-bundles/del-p3-starter-promotion-1779841783.yaml
    - .agents/active/delegation-bundles/del-t14-import-graph-assertion-1779841818.yaml
    - .agents/active/delegation-bundles/del-t2-config-relevance-resolver-1780554684.yaml
    - .agents/active/delegation-bundles/del-t3-cli-readback-1779841781.yaml
    - .agents/active/delegation-bundles/del-t4-relevance-recompute-1780556883.yaml
    - .agents/active/delegation/gcc5-verify-close-unblock.yaml
    - .agents/active/delegation/l1-followups.yaml
    - .agents/active/delegation/p1c-verifier-profile-source-aware.yaml
    - .agents/active/delegation/p1e-docs-hooks-consistency.yaml
    - .agents/active/delegation/p2-hook-scripts.yaml
    - .agents/active/delegation/p3-starter-promotion.yaml
    - .agents/active/delegation/t14-import-graph-assertion.yaml
    - .agents/active/delegation/t3-cli-readback.yaml
    - .agents/active/merge-back/gcc5-verify-close-unblock.md
    - .agents/active/merge-back/t3-cli-readback.md
    - .agents/active/verification/gcc5-verify-close-unblock/custom.result.yaml
    - .agents/active/verification/gcc5-verify-close-unblock/merge-back.result.yaml
    - .agents/active/verification/t3-cli-readback/merge-back.result.yaml
    - .agents/active/verification/t3-cli-readback/unit.result.yaml
    - .agents/lessons/index.md
    - .agents/workflow/plans/swallowed-errors-loud-atomic/PLAN.yaml
    - .agents/workflow/plans/swallowed-errors-loud-atomic/TASKS.yaml
    - .agentsrc.json
    - commands/import.go
    - commands/import_test.go
    - commands/internal/lifecycle/deps.go
    - commands/internal/lifecycle/project.go
    - commands/internal/lifecycle/project_test.go
    - commands/root.go
    - internal/config/fetcher.go
    - internal/config/fetcher_test.go
    - internal/dashboard/handlers/handlers_test.go
verification_result:
    status: pass
    summary: 'Scope stayed exactly within write_scope internal/adapters/sdk/ (3 files: sdk.go, sdk_test.go, readsfrom_gate_test.go). No other packages touched or needed touching — MaterializeView had no production callers, so nothing outside the SDK package required a signature-change update. PR #360 is open against master and needs review + merge; I did not merge it. No AI attribution trailer on the commit or PR per repo convention.'
integration_notes: 'Scope stayed exactly within write_scope internal/adapters/sdk/ (3 files: sdk.go, sdk_test.go, readsfrom_gate_test.go). No other packages touched or needed touching — MaterializeView had no production callers, so nothing outside the SDK package required a signature-change update. PR #360 is open against master and needs review + merge; I did not merge it. No AI attribution trailer on the commit or PR per repo convention.'
created_at: "2026-07-09T03:18:09Z"
---

## Summary

Wired a ReadsFromGate parameter into SDK.MaterializeView (satisfied structurally by *registry.Registry.ValidateReadsFrom — the same §11.2 migration_only rule EnforceReadsFrom applies at adapter-load time, t4) so every materialize_view call now consults the registry before the runner executes or anything writes; fail-closed when no gate is supplied. Added internal/adapters/sdk/readsfrom_gate_test.go with an integration test against a real registry.Registry: a view reading a migration_only adapter is rejected (runner never runs, nothing persists), a legitimate view still succeeds end-to-end, and a nil gate is rejected. Threaded an allowAllGate stub through the 4 pre-existing MaterializeView mechanics tests in sdk_test.go so they keep testing token/store plumbing unchanged. Mutation-verified: temporarily stripped the gate.ValidateReadsFrom call, reran the gate tests -> 2 failed as expected, restored -> green. go build ./... clean repo-wide (MaterializeView had zero production callers before this change, so the signature change is zero-blast-radius). PR #360 open: https://github.com/AGOrcha/dot-agents/pull/360 (base master, head feat/t9-sdk-readsfrom-gate) — stopped before merge per the delegation contract.

## Integration Notes

Scope stayed exactly within write_scope internal/adapters/sdk/ (3 files: sdk.go, sdk_test.go, readsfrom_gate_test.go). No other packages touched or needed touching — MaterializeView had no production callers, so nothing outside the SDK package required a signature-change update. PR #360 is open against master and needs review + merge; I did not merge it. No AI attribution trailer on the commit or PR per repo convention.
