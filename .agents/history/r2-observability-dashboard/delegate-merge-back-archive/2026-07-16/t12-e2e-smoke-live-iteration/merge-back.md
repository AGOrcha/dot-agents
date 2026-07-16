---
schema_version: 1
task_id: t12-e2e-smoke-live-iteration
parent_plan_id: r2-observability-dashboard
title: End-to-end smoke — live iteration triggers UI update within 2s
summary: 'Already complete on master (86ed255d, stale task status; reconcile-task-status-on-pr-merge). Go test TestLiveIterationPropagatesToSSEClientWithinBudget ok against the real SSE API with an explicit 2s budget; Playwright spec well-formed + env-gated with browser leg CI-gated. No new code authored (no-duplicate directive).'
files_changed:
    - tests/e2e/dashboard_live_iteration_test.go
    - web/dashboard/playwright.config.ts
    - web/dashboard/e2e/live-iteration.spec.ts
verification_result:
    status: pass
    summary: 'Delegate DashboardE2ESmoke-2 + parent: go vet clean, go test -run TestLiveIterationPropagatesToSSEClientWithinBudget ok 0.26s; playwright --list discovers both tests; env-gated E2E_LIVE_ITERATION, skips cleanly. Explicit <2s in both legs.'
integration_notes: 'Branch feat/r2-t12-e2e-smoke == master 8c15b47d (0 unique commits); NO PR needed. Reconcile stale-pending status to completed. Browser leg documented for CI.'
created_at: "2026-07-16T05:10:00Z"
---

## Summary
Already complete on master — introduced by `86ed255d` ("test(e2e): live-iteration dashboard e2e smoke (Go + Playwright), stacked on t6a"). Task status was stale-pending. No new code.

## Verification
- `go vet ./tests/e2e/` clean; `go test -run TestLiveIterationPropagatesToSSEClientWithinBudget ./tests/e2e/` → ok 0.26s.
- Real API: `server.New{Addr,IterLogDirs,RepoDir}` → SSE `/api/v1/observability/events`; iter-record write → watch.publish → broker → SSE frame.
- Explicit <2s: Go `propagationBudget = 2*time.Second`; Playwright `PROPAGATION_BUDGET_MS = 2000`.
- Browser leg → CI (built SPA + da-dashboard + chromium), env-gated, recipe in playwright.config.ts header.

## Follow-ups
None.
