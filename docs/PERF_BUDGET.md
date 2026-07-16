# Performance Budget — `da` command chain

Source: package-artifact-install t9 (performance testing + optimization across
the whole `da` command chain). This document is the regression guard's
narrative companion: the committed benchmark suite (`go test -bench`) and the
scripted harness (`scripts/perf/run-benchmarks.sh`) enforce these numbers
mechanically; this file records WHY the numbers are what they are, so a future
regression is diagnosable instead of just detectable.

Numbers below were measured on an Apple M4 Pro (`GOMAXPROCS=12`); absolute
`ns/op` will vary by machine, but the RELATIVE before/after deltas and the
`allocs/op` counts are stable and are what the regression guard should be read
against on any machine.

## Scope

Every `da` command that reads `.agentsrc.json` routes through the same shared
machinery, so optimizing it once benefits the whole chain:

- `da install` (`commands/internal/lifecycle/install.go`)
- `da refresh` (`commands/refresh.go`)
- `da config sync|explain|verify|lint` (`commands/config/*.go`)
- `da status`

All five funnel through:

1. **Pass-1 resolution** — `internal/config/resolver.go`,
   `LayeredResolver.Resolve` / `resolveExtends` (layers/extends).
2. **The §7A.5 auto-sync dispatch** — `internal/config/ensure_resolved.go`,
   `EnsureResolved` (decides Frozen / Locked / Offline / fresh-no-op /
   stale-reresolve).
3. **Pass-2 packages hydrate** —
   `commands/internal/lifecycle/packages_pass2.go`, `HydratePackagesUnits`
   (fetch + materialize + verify every declared package, per command run).
4. **Shared-target projection** — `internal/platform/resource_plan.go`,
   `RunSharedTargetProjectionExact` / `ProjectResolvedUnits` + the per-platform
   builders (Claude/Cursor dir-mirror, Codex/OpenCode/Copilot file-shaped).
5. **The `.agentsrc.lock` read-modify-write** —
   `internal/agentslock.Update` / `LayeredResolver.writeUnitsLock`.

## Methodology

- **Baselines**: Go `testing.B` benchmarks over realistic fixtures — many
  extends layers (5/25/100), many packages/artifacts (10/50/200), cold vs warm
  CAS content-address cache, and N projects sharing one `AGENTS_HOME` (20
  projects). Fixtures use the `local` source type (no network), matching the
  existing test convention in `resolver_test.go` / `packages_pass2_test.go`.
- **Profiling**: `go test -bench <name> -cpuprofile=... -memprofile=...` +
  `go tool pprof -top -cum`, cross-checked against a manual call-counter
  instrumentation pass (temporary, not committed) whenever a profile's
  cumulative-time attribution needed disambiguating from one-time fixture-setup
  cost vs steady-state per-iteration cost (`go test -bench` calibrates by
  calling the benchmark function more than once; `-cpuprofile` samples the
  WHOLE process, so pre-`b.ResetTimer()` setup and the timed loop both show up
  in the same profile — the counter cross-check is what confirmed each fix's
  steady-state hit rate, not just its presence in the profile).
- **Regression guard**: the benchmark suite below, run via `go test -bench` or
  `scripts/perf/run-benchmarks.sh`. No new feature code was added beyond test
  scaffolding and the three optimizations documented here.

## Benchmark suite (where to find it)

| File | Flow covered |
|---|---|
| `internal/config/resolver_bench_test.go` | Pass-1 `LayeredResolver.Resolve` / `ResolveLocked`, 5/25/100 layers, 20-project sweep |
| `internal/config/ensure_resolved_bench_test.go` | `EnsureResolved` fresh-path dispatch + `--frozen` |
| `internal/platform/resource_plan_bench_test.go` | `ProjectResolvedUnits` cold vs warm, 10/50/200 packages across all 6 platform builders |
| `commands/internal/lifecycle/install_bench_test.go` | `HydratePackagesUnits` cold (`resolvePackagesUnits`) vs warm (`hydratePackagesFromLock`), 10/50 packages |

Run the whole suite: `scripts/perf/run-benchmarks.sh` (wraps `go test -bench .
-benchmem` across all four packages and writes a timestamped report to
`scripts/perf/reports/`, gitignored). Run one package directly:
`go test ./internal/config/... -bench BenchmarkLayeredResolver -benchmem`.

## Top-K bottlenecks: before / after

### 1. Layer-cache write was unconditional on every resolve (FIXED)

**File**: `internal/config/fetcher.go`, `writeCachedLayer`.

**Finding**: every `LayeredResolver.Resolve` re-fetches every `extends` layer
(by design — a `local`/`http` source has no committed SHA to trust without
re-checking). But the content-addressed cache write
(`<cacheDir>/<sha>/layer.json`) was unconditional: even when the freshly-read
bytes were byte-identical to what was already cached at that exact
content-addressed path, the resolver still ran `MkdirAll` + `WriteFile` every
single time. Profiling a 100-layer local-extends chain (`go test
-cpuprofile`) showed this write pair as **~79% of total resolve CPU time**,
almost entirely redundant I/O on the steady-state (unchanged manifest)
re-resolve every `da config sync` / `da status` / `da install` performs.

**Fix**: `writeCachedLayer` now reads back the existing file at the
content-addressed target path and does a full `bytes.Equal` compare against
the freshly-fetched data BEFORE writing. A verified match skips the write
entirely. A miss (first-ever fetch, OR a stale/corrupt entry — e.g. left by a
prior interrupted process, since the previous write was a bare
`os.WriteFile`, not atomic) falls through to a real write — now via
`fsops.WriteFileAtomic` (temp+rename), which is strictly SAFER than the
previous non-atomic write (removes a latent torn-write corruption vector) as
a side effect of the fix, not a tradeoff against it. This is a full content
verification, not a cheap existence/size shortcut — the exact discipline this
codebase's H8 cache-hit-reverify pattern already establishes elsewhere
(`materialize.go`), applied here for the same reason: never trust a
content-addressed path without confirming the bytes.

**Before / after** (`BenchmarkLayeredResolverResolve_100Layers`, Apple M4 Pro):

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| Before | 12,022,067 | 5,545,542 | 44,970 |
| After | 8,105,729 | 5,596,816 | 44,884 |
| Δ | **−33%** | ~unchanged | ~unchanged |

### 2. Codex agent `.toml` render was unconditionally rewritten on every projection (FIXED)

**File**: `internal/platform/codex.go`, `writeCodexAgentTomlFile`.

**Finding**: `ProjectResolvedUnits` (the shared-target projection every `da
install`/`da refresh` runs) re-renders every sourced agent's Codex `.toml` on
every call, even when nothing changed. `isManagedCodexToml` already reads the
existing file to check the provenance marker, but the function then
unconditionally removed it and rewrote a fresh temp file + atomic rename —
paying `Remove` + `WriteFile` + `Rename` for every one of N agent units on
every re-projection. Profiling a 200-package warm re-projection
(`BenchmarkProjectResolvedUnits_Warm_200Packages`) showed
`writeCodexAgentTomlFile` as **~31% of total CPU time** before the fix.

**Fix**: in the "verified managed render" branch, read the existing file
content and `bytes.Equal`-compare it against the render this call is about to
write; a match returns early (no remove/write/rename). A mismatch (or read
error other than not-exist) falls through to the original replace path
unchanged. Correctness is unaffected: a genuinely stale render (source
content changed) still regenerates; the collision-resolution path for an
UNMANAGED occupant (a real content-aware ownership decision, not a
render-freshness check) is untouched.

**Before / after** (`BenchmarkProjectResolvedUnits_Warm_200Packages`):

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| Before | 51,869,371 | 30,374,523 | 63,610 |
| After | 22,826,497 | 30,309,664 | 62,620 |
| Δ | **−56%** | ~unchanged | ~unchanged |

`Warm_50Packages` and `Warm_10Packages` show the same relative improvement
(~53% and ~47% respectively) — the fix scales with the number of file-shaped
(Codex) agent units in the resolved set.

### 3. The H14 CAS-ignore install lock was re-acquired on every artifact materialize (FIXED)

**File**: `internal/config/local_source.go`, `EnsureAndVerifyCASIgnore`.

**Finding**: `MaterializeToStore` calls `EnsureAndVerifyCASIgnore` before
writing any CAS byte (H14: the content-addressed store must be verified
gitignored before content lands). This install+verify pair runs ONCE PER
ARTIFACT — every package a resolve materializes. `EnsureProvenanceGitignore`
(the install half) already short-circuits its own WRITE when the on-disk
gitignore is already correct (`if next == existing { return nil }`), but it
still acquires the inter-process advisory file lock
(`agentslock.AcquireFileLock`) and does a read-parse-rebuild pass every single
call, regardless of the no-op outcome. Profiling a 50-package warm
`HydratePackagesUnits` (the pass-2 hydrate every `da install`/`da refresh`
runs) showed this lock-acquire + read-merge pair as **~62% of total CPU
time**, almost entirely redundant: the permanent H14 pattern
(`alwaysIgnoredCAS = []string{"cache/"}`) is family/digest-independent, so
after the very first artifact in a batch (or the very first materialize ever
against a given `AGENTS_HOME`), every subsequent call re-derives an identical
answer.

**Fix**: `EnsureAndVerifyCASIgnore` now probes `CASPathIgnored` (a read-only
check — no lock, no write) FIRST. When it already reports "ignored", the call
returns immediately, skipping the lock-guarded install entirely. A miss
(first-ever materialize against this `AGENTS_HOME`, or an external revert
mid-batch — e.g. a concurrent hand-edit) still falls through to the full
install + a MANDATORY re-verify, unchanged from before: this is not a cached
"already ensured this session" shortcut (which could miss an external
revert), it is a real per-call verification, just ordered so the cheap check
runs before the expensive one instead of after. H14's fail-closed guarantee —
this function never returns success without a freshly verified ignored CAS
path — is unchanged.

**Before / after** (`BenchmarkHydratePackagesUnits`, 50 packages):

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| Cold, before | 32,031,000 | 1,630,217 | 12,614 |
| Cold, after | 9,432,964 | 1,202,661 | 9,008 |
| Cold Δ | **−71%** | −26% | −29% |
| Warm, before | 30,837,458 | 1,476,179 | 12,489 |
| Warm, after | 8,315,750 | 1,054,859 | 8,884 |
| Warm Δ | **−73%** | −29% | −29% |

This fix is the largest single win in the suite because it fires on BOTH the
cold (`resolvePackagesUnits`) and warm (`hydratePackagesFromLock`) pass-2
paths — every declared package, every `da install`/`da refresh` invocation,
regardless of whether the packages set itself changed.

## Already-fast / deliberately not optimized

### `agentslock.Update` / `AcquireFileLock` (the `.agentsrc.lock` read-modify-write itself)

`LayeredResolver.writeUnitsLock` and `commitArtifactLock` both go through
`agentslock.Update`, which acquires an inter-process advisory lock, reads the
current lock document, applies the caller's mutation, and writes it back.
This lock exists specifically to serialize concurrent `da` processes writing
the SAME `.agentsrc.lock` (proven by
`TestCommitArtifactLock_InterleavedWithPass1PreservesBothKeys` — a real
cross-pass lost-update bug this lock closes). Removing or weakening it would
reopen that lost-update window. `BenchmarkLayeredResolverResolveLocked_*`
(the read-only path) shows this cost is NOT paid on the fresh/no-op path —
only `Resolve` (which genuinely needs to persist a rewrite) pays it, at
roughly 700ns–800µs depending on layer count, which is proportionate to the
work being serialized. Verdict: **already justified** — this is a correctness
gate (the lost-update fix from the t3 review #3), not incidental overhead.

### `decodeEffective` (merged-config JSON marshal/unmarshal round-trip)

`resolveSnapshot` marshals the merged generic `map[string]any` back to JSON
and unmarshals it into a typed `AgentsRC` so `ExtraFields` round-trips
correctly. This runs once per `Resolve` call (not once per layer), so its
cost does not scale with layer count — `BenchmarkLayeredResolverResolve_*`
shows the marginal cost per additional layer is dominated by the fetch/cache
path (fixed #1), not this round-trip. Verdict: **already fast** for realistic
manifest sizes (config fragments are small policy JSON per
`maxLayerBytes = 4 << 20`, and real manifests are far smaller than that cap).

### CAS content materialize + verify (`MaterializeToStore`'s own extract + digest walk)

The content-addressed store write itself (stage → verify → atomic rename) is
a genuine security-relevant operation (H1/H2/H16: never trust existence
alone, always re-walk and re-verify a hit). It is NOT redundant the way the
gitignore lock (fix #3) was — every artifact's content genuinely needs its own
extract-and-verify pass. Its cost is visible in the `Cold_*`
`BenchmarkHydratePackagesUnits` numbers as the remaining floor after fix #3;
it was not touched, per the "never weaken a security gate for speed" rule.

## Regression guard

- `go test ./internal/config/... ./internal/platform/...
  ./commands/internal/lifecycle/... -bench . -benchmem` runs the full
  benchmark suite this document is based on.
- `scripts/perf/run-benchmarks.sh` wraps the same commands and writes a
  timestamped, diffable report to `scripts/perf/reports/` (gitignored).
- To compare two runs quantitatively: `go install
  golang.org/x/perf/cmd/benchstat@latest` then `benchstat <old-report>
  <new-report>`.
- `go test ./internal/config/... ./commands/... ./internal/platform/...` (the
  full functional suite, not just benchmarks) must stay green — every
  optimization in this document is validated against the existing test suite
  (including the H1/H2/H7/H8/H14/H16 security-gate tests) with zero relaxation.
