---
schema_version: 1
task_id: cj-outputs-exact-prune
parent_plan_id: config-v2-coherence
title: 'Outputs (sync) half: exact/prune projection in refresh/install'
summary: 'Implemented config-v2-coherence §7A.5/D10 outputs (sync) half: EXACT/PRUNE projection. internal/platform/resource_plan.go adds ResourcePlan.PruneStaleSharedTargets (removes managed outputs no longer in the resolved set; user files + non-managed links untouched; per-target failures aggregated) and RunSharedTargetProjectionExact (exact-by-default apply+prune; dry-run previews prune lines; existing RunSharedTargetProjection signature unchanged). commands/refresh.go adds ensureLockFreshForRefresh (EnsureResolved lock half before projection, manifest-gated best-effort) and switches refresh to exact-by-default via refreshInexact. Branch feature/config-v2-coherence-cj-outputs-exact-prune pushed to org; PR https://github.com/AGOrcha/dot-agents/pull/38.'
files_changed:
    - .agents/workflow/plans/config-v2-coherence/TASKS.yaml
verification_result:
    status: pass
    summary: 'Deferred (fold-back, out of this task write scope): the --inexact cobra flag wiring in commands/internal/lifecycle/refresh.go + lifecycle.Deps signature (refreshInexact package var defaults to exact, satisfying the spec default). Defensive prune-after-execute error branch in RunSharedTargetProjectionExact is the one uncovered line (mutually-exclusive with a writable target dir). refresh.go stays on the coverage allowlist (legacy-tail); new ensureLockFreshForRefresh is 100%.'
integration_notes: 'Deferred (fold-back, out of this task write scope): the --inexact cobra flag wiring in commands/internal/lifecycle/refresh.go + lifecycle.Deps signature (refreshInexact package var defaults to exact, satisfying the spec default). Defensive prune-after-execute error branch in RunSharedTargetProjectionExact is the one uncovered line (mutually-exclusive with a writable target dir). refresh.go stays on the coverage allowlist (legacy-tail); new ensureLockFreshForRefresh is 100%.'
created_at: "2026-06-06T22:07:19Z"
---

## Summary

Implemented config-v2-coherence §7A.5/D10 outputs (sync) half: EXACT/PRUNE projection. internal/platform/resource_plan.go adds ResourcePlan.PruneStaleSharedTargets (removes managed outputs no longer in the resolved set; user files + non-managed links untouched; per-target failures aggregated) and RunSharedTargetProjectionExact (exact-by-default apply+prune; dry-run previews prune lines; existing RunSharedTargetProjection signature unchanged). commands/refresh.go adds ensureLockFreshForRefresh (EnsureResolved lock half before projection, manifest-gated best-effort) and switches refresh to exact-by-default via refreshInexact. Branch feature/config-v2-coherence-cj-outputs-exact-prune pushed to org; PR https://github.com/AGOrcha/dot-agents/pull/38.

## Integration Notes

Deferred (fold-back, out of this task write scope): the --inexact cobra flag wiring in commands/internal/lifecycle/refresh.go + lifecycle.Deps signature (refreshInexact package var defaults to exact, satisfying the spec default). Defensive prune-after-execute error branch in RunSharedTargetProjectionExact is the one uncovered line (mutually-exclusive with a writable target dir). refresh.go stays on the coverage allowlist (legacy-tail); new ensureLockFreshForRefresh is 100%.
