# Spec: per-file, platform-aware coverage gate (phased → hard 95%)

Status: accepted-direction (maintainer 2026-05-17, from #18 review:
`sync_code_warm_link.go` 61% slipped in while the per-package gate passed).

## Problem

`scripts/coverage-gate.sh` enforces ~95% Go-statement coverage **per
package**, per-OS, on that OS's own `coverage.out`. Two defects:

1. **Aggregation masks weak files.** A 61%-covered file rides the
   package average above 95% behind well-tested siblings — it ships
   silently (observed: `commands/kg/sync_code_warm_link.go` in #18).
2. **Per-file naïvely is unsound under build tags.** Single-OS coverage
   cannot cover other-OS build-tagged files (`*_windows.go` on Linux =
   0%). Per-package hid this (mixed packages averaged out); per-file
   would false-fail ~12 platform/ignore-tagged/non-Go files that are
   uncoverable on a given runner.

Measured blast radius (Sonar line cov, project): **62 / 186 files
< 95%** — ~12 measurement-artifact (unfixable-by-tests), ~40 genuine
legacy tail (crg.go 51%, init.go 83%, links.go 76%, …), rest near-miss.

## Decisions

1. **Granularity → per file** (not per package).
2. **Platform-aware via merged multi-OS profile.** CI already uploads
   `coverage-{windows,macos,ubuntu}-latest` artifacts. Add a
   **post-matrix job** that downloads all three, **merges** them
   (e.g. `gocovmerge`), and runs the gate **once** on the union. A
   `*_windows.go` file is credited by the Windows run, `*_unix.go` by
   mac/ubuntu. Eliminates platform false-fails at the root. The
   in-matrix per-OS gate is demoted (smoke/removed). *Rejected
   alternative:* per-OS build-tag parsing in the gate — more fragile
   than merging profiles that already exist.
3. **Exclusion + exception model.**
   - Pattern excludes (carry + extend current `EXCLUDE_RE`):
     `//go:build ignore` scripts (`scripts/cov*.go`), non-Go
     (`*.rb`), `cmd/*` entrypoints, test scaffolding (`storetest`,
     `testutil`, `linktest`), scaffold embeds.
   - **Per-file exception allowlist** (`scripts/coverage-exceptions.txt`
     or similar): any legitimately-sub-95 file NOT pattern-excludable
     must be listed **with a one-line rationale**; unlisted + sub-95 =
     fail. The allowlist is the auditable escape hatch, reviewed like
     code.
4. **Phased rollout (end state = hard per-file 95%).**
   - **P1:** per-file **AND per-package = warn** (prints offenders,
     exit 0). CI stays green; baseline captured. *(Correction, cg2
     finding: the original "package stays enforce in P1" assumed
     merged ≥ per-OS. FALSE — the merged multi-OS profile is STRICTER
     than the old Linux/macOS-only gate: it surfaces `*_windows.go`
     statements that Windows's POSIX-skipping suite covers weakly,
     dragging some packages <95%. So package enforce must also wait
     for remediation. The old per-OS gate was lenient by excluding
     Windows — that leniency is the thing being removed.)*
   - **P2:** remediate the ~40-file legacy tail (own tracked,
     sized backlog) or allowlist-with-rationale where genuinely
     untestable.
   - **P3:** flip per-file to **enforce** (hard 95%); retire the
     now-redundant package gate.
5. **#18 compliance (gate before #18).** P1 gate lands before #18
   merges. #18 must bring `commands/kg/sync_code_warm_link.go` to ≥95%
   (real tests on pr3c) — default; allowlisting it is **not** accepted
   here (it is the originating offender). v0.3.2 is delayed accordingly.

## Done criteria

- Post-matrix merge+gate job green; **zero platform false-positives**
  (every `*_windows.go`/`*_unix.go` credited from its OS run).
- Gate emits an exact per-file offender list; mode flag
  (`warn`|`enforce`) works; allowlist honored + rationale-required.
- P1 lands with CI green (package enforce, per-file warn).
- `sync_code_warm_link.go` ≥95% before #18 merges.
- End state: per-file enforce at 95% (±0.05 tol) with the legacy tail
  remediated/allowlisted; package gate removed.

## Scope / deferred

- Threshold value unchanged (95%, 0.05pp tol).
- The ~40-file legacy remediation is a **separate sized backlog**
  (P2), not one PR.
- Touches `scripts/coverage-gate.sh` + `.github/workflows/test.yml`
  (master) → via PR (no direct commits).
- Independent of `graphstore-concurrency-contract`,
  `workflow-commit-command`, `di-refactor-rollout`.
