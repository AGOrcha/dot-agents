---
schema_version: 1
task_id: se6-graphstore-atomic
parent_plan_id: swallowed-errors-loud-atomic
title: 'Slice 6: graphstore atomicity + corrupt-extra visibility (top risk #3)'
summary: 'Implemented se6 (Slice 6: graphstore atomicity + corrupt-extra visibility). 6a: StoreFileNodesEdges (sqlite.go + postgres.go, 4 sites) now checks the encodeExtra error on both the node and edge insert loops and aborts (rolling back the tx via the existing deferred Rollback) instead of silently substituting "{}" for that row while the rest of the batch committed. 6b: decodeExtra (shared by sqlite.go''s scanNode/collectNodes/collectEdges and reused by postgres.go''s pg* equivalents) now logs a slog.Warn and tags the returned map with a sentinel key on JSON decode failure instead of silently dropping to nil -- no signature change. 6c: crg.go ReadNodes/ReadEdges now check os.IsNotExist before treating a Stat failure as "no CRG db" (a real Stat error is surfaced instead of silently mirroring zero rows), and count per-row Scan failures, surfacing the count via the returned error instead of a silent continue (well-formed rows are still returned). mcp_server.go untouched (se1''s scope). Tests added: NaN-float-in-Extra rollback (sqlite + live-postgres testcontainer, node and edge subtests), corrupt-extra-JSON warn+tag on read, CRG permission-denied-Stat error, CRG per-row scan-failure counting. go build ./... and go test ./internal/graphstore/... + ./commands/kg/... all green.'
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
    summary: 'PR #361 open: https://github.com/AGOrcha/dot-agents/pull/361 -- base master, head swallowed/6-graphstore-atomic, independent (not stacked on se0/se1/etc). Write scope was exactly internal/graphstore/{sqlite,postgres,crg}.go + their _test.go; diff stat confirms no other files touched. STOPPED before merge per delegation contract -- parent to review and merge.'
integration_notes: 'PR #361 open: https://github.com/AGOrcha/dot-agents/pull/361 -- base master, head swallowed/6-graphstore-atomic, independent (not stacked on se0/se1/etc). Write scope was exactly internal/graphstore/{sqlite,postgres,crg}.go + their _test.go; diff stat confirms no other files touched. STOPPED before merge per delegation contract -- parent to review and merge.'
created_at: "2026-07-09T03:17:08Z"
---

## Summary

Implemented se6 (Slice 6: graphstore atomicity + corrupt-extra visibility). 6a: StoreFileNodesEdges (sqlite.go + postgres.go, 4 sites) now checks the encodeExtra error on both the node and edge insert loops and aborts (rolling back the tx via the existing deferred Rollback) instead of silently substituting "{}" for that row while the rest of the batch committed. 6b: decodeExtra (shared by sqlite.go's scanNode/collectNodes/collectEdges and reused by postgres.go's pg* equivalents) now logs a slog.Warn and tags the returned map with a sentinel key on JSON decode failure instead of silently dropping to nil -- no signature change. 6c: crg.go ReadNodes/ReadEdges now check os.IsNotExist before treating a Stat failure as "no CRG db" (a real Stat error is surfaced instead of silently mirroring zero rows), and count per-row Scan failures, surfacing the count via the returned error instead of a silent continue (well-formed rows are still returned). mcp_server.go untouched (se1's scope). Tests added: NaN-float-in-Extra rollback (sqlite + live-postgres testcontainer, node and edge subtests), corrupt-extra-JSON warn+tag on read, CRG permission-denied-Stat error, CRG per-row scan-failure counting. go build ./... and go test ./internal/graphstore/... + ./commands/kg/... all green.

## Integration Notes

PR #361 open: https://github.com/AGOrcha/dot-agents/pull/361 -- base master, head swallowed/6-graphstore-atomic, independent (not stacked on se0/se1/etc). Write scope was exactly internal/graphstore/{sqlite,postgres,crg}.go + their _test.go; diff stat confirms no other files touched. STOPPED before merge per delegation contract -- parent to review and merge.
