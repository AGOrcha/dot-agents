# PR10 SQ3 CI Fixes

Status: in_progress

## Goal

Clear the 25 open SonarCloud issues currently reported for PR `#10`.

## Findings Snapshot

- 23 `go:S3776` cognitive-complexity issues, concentrated in `commands/workflow/*` tests plus a smaller set of production helpers.
- 1 `godre:S8193` readability issue in `commands/kg/kg.go`.
- 1 `typescript:S6606` nullish-coalescing issue in `ports/typescript/src/commands/workflow.ts`.

## Execution Plan

- Fix the two single-line style/readability issues first.
- Refactor test-heavy complexity cases by extracting assertion/setup helpers and table-driven subtests.
- Refactor production complexity cases with small helper extraction only where needed to drop below Sonar thresholds.
- Run focused Go and TypeScript verification for touched areas, then re-check the local issue count inputs where possible.
