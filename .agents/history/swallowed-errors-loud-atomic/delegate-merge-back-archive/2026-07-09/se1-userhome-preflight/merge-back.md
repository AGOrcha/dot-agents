---
schema_version: 1
task_id: se1-userhome-preflight
parent_plan_id: swallowed-errors-loud-atomic
title: 'Slice 1: UserHomeDir preflight, hard-fail on unresolvable home (top risk #2)'
summary: 'Implemented se1: config.PreflightUserHome() hard-fails with actionable error (''set $HOME or $AGENTS_HOME'') unless AGENTS_HOME is set, wired into root PersistentPreRunE (commands/root.go). Applied the same guard to the 2 duplicate UserHomeDir-swallow sites, each keyed off their own KG_HOME override: commands/kg/kg.go kgHome() and internal/graphstore/mcp_server.go defaultKGHome() -- both kept their func() string signature (many out-of-write_scope callers) and hard-fail via a package-level exit hook (kgHomeExit/defaultKGHomeExit) instead of threading an error return, so tests observe the failure without killing the test binary. Not a signature refactor of AgentsHome''s callers. Scope: exactly commands/root.go, commands/root_test.go, internal/config/paths.go, internal/config/paths_test.go, commands/kg/kg.go, commands/kg/kg_test.go, internal/graphstore/mcp_server.go, internal/graphstore/mcp_server_test.go (8 files). Tests: override+HOME-unset -> normal operation; both unset -> hard actionable error, no relative fallback (all 4 sites). go build ./... clean; go test ./internal/config/... ./commands/kg/... ./internal/graphstore/... ./commands/... all green; gofmt/go vet clean. PR #357 open base master head swallowed/1-userhome-preflight, STOPPED before merge, no AI trailer.'
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
    summary: 'Independent slice off master, not stacked. Review + merge PR #357 when ready; no advance run by this worker per delegated-path contract.'
integration_notes: 'Independent slice off master, not stacked. Review + merge PR #357 when ready; no advance run by this worker per delegated-path contract.'
created_at: "2026-07-09T03:05:30Z"
---

## Summary

Implemented se1: config.PreflightUserHome() hard-fails with actionable error ('set $HOME or $AGENTS_HOME') unless AGENTS_HOME is set, wired into root PersistentPreRunE (commands/root.go). Applied the same guard to the 2 duplicate UserHomeDir-swallow sites, each keyed off their own KG_HOME override: commands/kg/kg.go kgHome() and internal/graphstore/mcp_server.go defaultKGHome() -- both kept their func() string signature (many out-of-write_scope callers) and hard-fail via a package-level exit hook (kgHomeExit/defaultKGHomeExit) instead of threading an error return, so tests observe the failure without killing the test binary. Not a signature refactor of AgentsHome's callers. Scope: exactly commands/root.go, commands/root_test.go, internal/config/paths.go, internal/config/paths_test.go, commands/kg/kg.go, commands/kg/kg_test.go, internal/graphstore/mcp_server.go, internal/graphstore/mcp_server_test.go (8 files). Tests: override+HOME-unset -> normal operation; both unset -> hard actionable error, no relative fallback (all 4 sites). go build ./... clean; go test ./internal/config/... ./commands/kg/... ./internal/graphstore/... ./commands/... all green; gofmt/go vet clean. PR #357 open base master head swallowed/1-userhome-preflight, STOPPED before merge, no AI trailer.

## Integration Notes

Independent slice off master, not stacked. Review + merge PR #357 when ready; no advance run by this worker per delegated-path contract.
