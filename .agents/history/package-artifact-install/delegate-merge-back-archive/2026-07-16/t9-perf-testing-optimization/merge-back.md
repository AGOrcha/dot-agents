---
schema_version: 1
task_id: t9-perf-testing-optimization
parent_plan_id: package-artifact-install
title: 'Performance testing + optimization across the da command chain'
summary: 'End-to-end perf baselines (testing.B benchmarks across resolver/ensure_resolved/resource_plan/install + scripts/perf/run-benchmarks.sh over realistic fixtures: 5/25/100 layers, 10/50/200 packages, cold/warm CAS, 20-project sweep). Two bottlenecks optimized and cross-harness-confirmed clean — writeCachedLayer read-and-compare before write (100-layer resolve -33%) and writeCodexAgentTomlFile byte-compare skip inside the verified-managed branch (200-package warm re-projection -56%). A third candidate (EnsureAndVerifyCASIgnore per-call fast-path) was CONSIDERED then DROPPED for security robustness after cross-harness review found an H14 fail-closed bypass in three consecutive rounds (symlink, leading-whitespace, trailing-tab+negation .gitignore variants) — the gate now always canonicalizes+verifies (provably safe); the trim-accuracy fix to CASPathIgnored is kept. Regression guard: committed benchmark suite + docs/PERF_BUDGET.md.'
files_changed:
    - internal/config/fetcher.go
    - internal/platform/codex.go
    - internal/config/local_source.go
    - internal/config/resolver_bench_test.go
    - internal/config/ensure_resolved_bench_test.go
    - internal/platform/resource_plan_bench_test.go
    - commands/internal/lifecycle/install_bench_test.go
    - internal/config/materialize_test.go
    - scripts/perf/run-benchmarks.sh
    - docs/PERF_BUDGET.md
    - .gitignore
verification_result:
    status: pass
    summary: 'go test ./internal/config/... ./internal/platform/... ./commands/... -race green; benchmark guard runs; gofmt/vet clean. Cross-harness Codex found + drove out an H14 fast-path BLOCKER across 3 rounds; resolved by dropping the fast-path (always-canonicalize). writeCachedLayer + codex optimizations confirmed gate-preserving.'
integration_notes: 'Commits 85956f7f + 15ce3cdb + 1b2833ea + 76346eb2 cherry-picked onto feat. The dropped fast-path perf win is recoverable safely via a run-level hoist — tracked as fold-back cas-ignore-install-run-level-hoist. Last task; plan complete.'
created_at: "2026-07-16T05:30:00Z"
---

## Summary

End-to-end perf pass. Two optimizations landed (−33% / −56%), a third dropped for security robustness (H14 gate stays always-canonicalizing), regression guard committed. See summary frontmatter.
