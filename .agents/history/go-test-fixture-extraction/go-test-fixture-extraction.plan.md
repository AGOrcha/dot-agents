# Plan — go-test-fixture-extraction

Cluster D follow-up to PR#10's `sonarqube-pr10` plan. Extracts shared
`*_test.go` fixture helpers into `internal/testutil/` to clear the bulk
of `new_duplicated_lines_density` debt deferred by ADR-0008 (option B).

## Quick links

- **Spec / contract:** [`.agents/workflow/specs/go-test-fixture-extraction/design.md`](../../specs/go-test-fixture-extraction/design.md)
- **Source decision:** [`docs/adr/0008-pr10-duplication-scope.md`](../../../../docs/adr/0008-pr10-duplication-scope.md)
- **Source classification:** [`.agents/workflow/plans/sonarqube-pr10/findings.md`](../sonarqube-pr10/findings.md) §2 Cluster D
- **Companion plan:** [`production-code-helper-extraction`](../production-code-helper-extraction/) (Cluster E.other)

## Why this is its own plan

Cluster D spans 21 `*_test.go` files and ~1,500–1,700 duplicated lines.
Adding it to PR#10 (already 895 changed files) is a merge-risk play with
no upside. ADR-0008 chose option B: ship PR#10 with a duplication waiver
and track Cluster D as this plan. **Do not stack this work on PR#10.**

## Sequencing summary

1. **T1 audit** — re-snapshot live duplication, converge canonical helper
   signatures in `design.md`. No code change.
2. **T2 land helpers** — `internal/testutil/` exists with helpers + unit
   tests. No consumers yet.
3. **T3–T7 per-cluster extraction** — one task per package family, one
   commit per file. Touch tests only.
4. **T8 closeout** — open follow-up PR off `master`, verify SonarCloud
   gate, archive.

T3–T7 are parallelizable after T2 lands. Each per-file commit must run
its package's `go test` clean.

## Out of scope

- Production code dedup (Cluster E.other → `production-code-helper-extraction`).
- New test scenarios. If extraction reveals a missing case, file a
  follow-up — do not bundle.
- Stacking on PR#10. Merge readiness for PR#10 depends on ADR-0008's
  waiver, not on this plan's completion.

## Done

When the follow-up PR merges, with `new_duplicated_lines_density` below
3% (or Cluster D's contribution reduced ≥ 60%, whichever comes first
given Cluster E.other's concurrent or deferred state).
