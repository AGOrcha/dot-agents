# Plan — production-code-helper-extraction

Cluster E.other follow-up to PR#10's `sonarqube-pr10` plan. Extracts
remaining production-code duplication into shared helpers (extending
the `internal/projectsync` pattern PR#10 introduced for `ListBucket` /
`PromoteResource`) or explicitly accepts dups that would be net
negative to extract.

## Quick links

- **Spec / contract:** [`.agents/workflow/specs/production-code-helper-extraction/design.md`](../../specs/production-code-helper-extraction/design.md)
- **Source decision:** [`docs/adr/0008-pr10-duplication-scope.md`](../../../../docs/adr/0008-pr10-duplication-scope.md)
- **Source classification:** [`.agents/workflow/plans/sonarqube-pr10/findings.md`](../sonarqube-pr10/findings.md) §2 Cluster E
- **Companion plan:** [`go-test-fixture-extraction`](../go-test-fixture-extraction/) (Cluster D — load-bearing for the gate)
- **In-PR precedent:** PR#10 already shipped `internal/projectsync.ListBucket`,
  `PromoteResource`, `CopyTree`, `ReadFrontmatterDescription` — Cluster F
  and E.promote — as a template for the work in this plan.

## Why this is its own plan

Cluster E.other is ~400-500 dup lines spread across roughly a dozen
files in four areas (graphstore, platform, commands/*, ts port). The
extractions are **design-heavy**: each pair has its own structural
shape, and some pairs are accidental duplication that should NOT be
extracted. Mass-fixing in PR#10 (already 895 changed files) is
ill-advised; ADR-0008 chose option B and tracks this as a follow-up.

## Sequencing summary

1. **T1 triage** — re-snapshot live duplication; classify each group
   EXTRACT vs ACCEPT with reasoning. No code change.
2. **T2 graphstore** — sqlite/postgres/mcp_server shared helpers.
   Largest cluster (~150-200 lines). Wire-format risk: SQL parity.
3. **T3 resource_plan** — single-file three-block extraction. Tightest
   single win (~70 lines).
4. **T4 commands CLI family** — settings/mcp/rules + others. Up to 8
   files; T1 may narrow.
5. **T5 platform mcp_settings** — small two-file extract; may merge
   into T4 if T1 finds shape overlap.
6. **T6 ts port** — mirror PR#10's Go projectsync extraction in
   TypeScript. Coordinate with `typescript-port` plan owner.
7. **T7 closeout** — open follow-up PR off `master`, snapshot gate,
   archive.

T2-T6 are parallelizable after T1 lands. Each per-task hard test is
`go test ./<affected-package>/...`.

## Out of scope

- Test-fixture dedup (Cluster D → `go-test-fixture-extraction`).
- New abstractions for not-yet-needed callers. YAGNI applies.
- Stacking on PR#10. The math (Cluster E.other alone ≈ 0.7-0.9 pp)
  doesn't flip the gate; this plan's value comes from compounding
  with `go-test-fixture-extraction`. Order the merges accordingly:
  the gate-flip happens whenever the second of the two PRs lands.

## Done

When the follow-up PR merges, with Cluster E.other contribution
reduced ≥ 60% (gate may still fail if Cluster D hasn't landed — that
ordering concern is owned by the cross-plan merge sequencing, not by
this plan).
