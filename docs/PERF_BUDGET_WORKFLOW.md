# Performance Budget — `da workflow` command chain

Source: the `workflow-perf-optimization` plan (spec:
`.agents/workflow/specs/workflow-perf-optimization/design.md`). This is the
companion to `docs/PERF_BUDGET.md` (which covers the separate
`package-artifact-install t9` config-chain effort). It records each
workflow-perf win, the number it achieved, and — the important part — the
**deterministic in-CI test that guards it against regression**, so a future
slowdown is diagnosable, not just detectable.

## Why deterministic counts, not `ns/op`

`commands/workflow` and the git-heavy `commands/kg` / `commands/sync` suites are
dominated by **git-subprocess spawns**, and git-spawn cost on `windows-latest`
is ~10× Mac — so the spawn *count*, not wall-clock, is both the real lever and
the CI headline. Every win here is therefore proven by a **machine-independent,
deterministic metric** baked into an ordinary `go test` assertion — a git-spawn
count (via a PATH git-shim against the prebuilt test binary), an `allocs/op`
bound, or a tree-OID equality — NOT by variance-prone `ns/op` benchstat.

**The regression guard is the normal `go test ./...` run in CI**: a regression
*fails a test*, it does not merely drift a benchmark. Absolute wall-clock
numbers below (Apple M4 Pro) are secondary; the counts/OIDs are the contract.

## Wins (merged)

| # | Win | PR | Metric (before → after) | Guard test |
|---|---|---|---|---|
| H1 | `commands/workflow` shared committed-repo fixtures | #492 | `closeTaskTestRepo` **150 → 6** fixture git-spawns (−96%); Mac −22% | `fixture_template_test.go` — 1-bootstrap / N-copies proof + migrated `closeTaskTestRepo`/archive tests |
| H2 | checkpoint git-summary dedup | #485 | **7 → 5** git-spawns per checkpoint | checkpoint git-summary spawn-count assertion (reused `git status --short` snapshot) |
| H3-read | state-ref read `O(tasks)` → `O(1)` (`git cat-file --batch`) | #486 | **52 → 3** git-spawns @50 tasks (−94%) | `parseCatFileBatch` byte-parity + per-`show` ordering parity tests |
| H3-write | state-ref write `O(files)` → `O(1)` (go-git in-process) | #490 | **74 → 2.99** git-spawns @50 tasks (−96%) | `TestBuildStateRefCommit_TreeOIDMatchesPlumbing` (tree OID byte-identical to git plumbing) + linked-worktree common-store test |
| H6 | render-manifest command-scoped read cache | #491 | `allocs/op` **−57 to −66%** (warm) | in-place-writer chokepoint + stat-guard reload tests (`TestCache_PathChangeReloads`) |
| H8 | dashboard store session-projection memo | #489 | warm-path `allocs/op` **−42 to −71%** | fingerprint-invalidation + memo-not-corrupted tests (`FilterDoesNotCorruptMemo`, `EvictDropsSessionsMemo`) |
| H9 | crg regex hoist / iter-log single-pass / hook-outcome `O(1)` | #487 | crg regex **−82%**, paths **−86%**, iter-log v1 decode **−48%** | `BenchmarkParseCRGMutationSummary` + iter-log v1/v2 decode tests |
| P4-kg | `commands/kg` shared empty-repo template | #494 | **203 → 92** git-spawns (−54.7%) | `TestKGRepoTemplateReusesSingleBootstrap` |
| P4-sync | `commands/sync` shared seeded-repo template | #494 | **258 → 134** git-spawns (−48.1%) | `TestSyncRepoTemplateReusesSingleBootstrap` |

H1 and P4 are **test-only**; H2/H3/H6/H8/H9 are behavior-preserving production
changes. All landed under `-race` with the full existing suite green and no
coverage relaxation.

## Falsifications (measured, no PR)

The plan's discipline is *falsify before optimizing*. These candidates were
measured and **rejected** rather than force-fit:

- **H7 / H7b — graphstore read batching.** The read path is **output-bound**:
  ~44% of time is modernc per-row string materialization for the
  `ImpactResult` / `SearchNodes` records the contracts must return. A ≥30%
  `allocs/op` cut is architecturally unavailable without a *forbidden* contract
  change; batched node resolution reaches only ~5–10%. Reverted, no PR.
- **P4 cross-package kills** (#494 report): `internal/scoring` (only ~14%
  templatable — reaching 20% would require rewriting ~19 diverse-history tests,
  a red flag), `internal/config` (2 spawns, a CAS-path-ignored correctness
  gate), `internal/graphstore` write/seed (already single-`Tx` + WAL +
  `synchronous=NORMAL` from prior work — corroborates H7b), `internal/service`
  (negligible), `commands/internal/lifecycle` and `internal/eval/harness`
  (0 spawns).

The single reverted sub-change is H9's **hook-outcome record-granular
validation**: switching from whole-sidecar to per-append validation was a
genuine, untested behavior delta (fail-closed → fail-open on a corrupt prior
record) in a security-sensitive path, so it was reverted; the regex/iter-log
wins in #487 were kept.

## Timeout scaffold — reverted

The `-timeout=1200s` bump in `GO_TEST_FLAGS` (`test.yml` + `auto-release.yml`)
was a scaffold for the *pre-optimization* `commands/workflow -race` wall-clock
on Windows (~438s non-race baseline). With H1/H2/H3 landed it was reverted to
Go's **600s** per-package default (#493) and **empirically confirmed** — the
`windows-latest -race` job is green at 600s. `-timeout` stays per-package, so a
genuine hang still fails the run.

## Re-measuring

- **Spawn-count wins** (H1, H2, H3, P4): asserted directly by the guard tests —
  `go test ./commands/workflow/... ./commands/kg/... ./commands/sync/... -race`.
- **Alloc-based wins** (H6, H8, H9): benches live beside the code
  (`*_bench_test.go`); e.g.
  `go test ./internal/graphstore/... -run xxx -bench . -benchmem`, compared
  across runs with `benchstat`.
