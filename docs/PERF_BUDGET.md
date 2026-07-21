# Performance Budget — `da` command chain

> **See also** [`PERF_BUDGET_WORKFLOW.md`](PERF_BUDGET_WORKFLOW.md) — companion budget for the `da workflow` command chain (`workflow-perf-optimization` effort).

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
  scaffolding and the two shipped optimizations documented here (a third was
  investigated, implemented, cross-harness reviewed across three rounds, and
  ultimately DROPPED — see §3 below — because it gated a fail-closed security
  decision on an approximation of git's semantics that kept proving
  incomplete).

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

### 3. The H14 CAS-ignore install lock — optimization CONSIDERED, then DROPPED for security robustness

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

**Three attempted fixes, three cross-harness-found bypasses — DROPPED**: a
per-call fast path (skip the lock-guarded install when the on-disk
`.gitignore` already "looks" correct) was implemented and cross-harness
reviewed across three rounds. Each round's fast-path gate — trusting
`CASPathIgnored` alone, then adding a canonical-regular-file + exact-line
check, then requiring the managed block to be structurally TERMINAL — closed
the PREVIOUS round's reported divergence but the review process kept
surfacing a NEW way the fast path's approximation of real git's gitignore
semantics could diverge from the real `git` binary's actual behavior:

1. **Round 2** — a symlinked `.gitignore` (real git refuses to read a
   symlinked `.gitignore`, treating it as absent; the fast path's
   `os.ReadFile`-based check followed the symlink and trusted its target).
2. **Round 2** — a leading-whitespace pattern (`" cache/"`; real git treats
   leading whitespace as significant, unlike trailing whitespace, which it
   strips; the fast path's trim canonicalized the two).
3. **Round 3** — a canonical block followed by a re-inclusion (`!cache/`)
   shadowed by a trailing-tab variant (`cache/<TAB>`); real git strips only
   trailing UNESCAPED SPACES, never tabs, so the tab-suffixed line is a
   distinct, non-matching pattern and the negation is the true last-match
   winner — a divergence the fast path's block-containment check did not
   consider structurally.

**Decision (round 4)**: stop re-approximating a fail-closed SECURITY gate.
`EnsureAndVerifyCASIgnore` now ALWAYS canonicalizes
(`EnsureProvenanceGitignore` — which unconditionally rewrites `.gitignore`
via atomic temp+rename, replacing any symlink, stray negation, or other
divergent occupant with the exact managed form) and then unconditionally
re-verifies with `CASPathIgnored`, on every single artifact, with zero fast
path. This is the original, provably-correct H14 behavior with no
approximation of git's semantics anywhere in the decision path. The
`CASPathIgnored` trim-accuracy fix (strip only a trailing `\r`; delegate
trailing-space handling to `gitignore.ParsePattern`, matching real git;
never strip tabs) is KEPT — it is a correctness improvement to the
mandatory reverify path regardless of the fast path's fate, and the real-git
regression tests for it (`TestCASPathIgnored_LeadingWhitespaceNotSignificant`,
`TestCASPathIgnored_TrailingTabNotStripped`) are kept too, still asserted
against the real `git` binary with the local/global/system git config
neutralized (`GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM=/dev/null`, `-c
core.excludesFile=/dev/null`) for hermeticity. The fast-path-specific tests
(canonical-form checks, terminal-block happy-path) were removed along with
the fast path itself.

**Verdict**: NOT optimized. The ~62%-of-CPU cost this call carries in a
per-artifact hydrate is real and unchanged from the original finding — this
document records it as a deliberate trade of perf for a provably-airtight
security gate, not a missed optimization.

**Follow-up (structural, not semantic)**: the real future win here is
architectural — hoist the CAS-ignore install+verify to run ONCE per
`install`/`refresh` invocation (before the per-artifact materialize loop
starts) rather than once per artifact, since the "cache/" pattern is
family/digest-independent and the loop's own per-artifact CAS writes don't
need a fresh gitignore check between them within a single process
invocation. This changes WHERE the call happens (call-site restructuring in
`commands/internal/lifecycle/packages_pass2.go` / `MaterializeToStore`'s
caller), not WHAT it approximates — it introduces no new gitignore-semantics
guesswork, so it does not carry the same risk class as the dropped fast
path. Out of scope for this pass; flagged here for a future task.

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
  ./commands/internal/lifecycle/... ./commands/workflow/... ./internal/graphstore/...
  ./internal/dashboard/store/... ./internal/adapters/builtin/crg/... -bench . -benchmem`
  runs the full benchmark suite this document is based on.
- `scripts/perf/run-benchmarks.sh` wraps the same commands and writes a
  timestamped, diffable report to `scripts/perf/reports/` (gitignored).
- To compare two runs quantitatively: `go install
  golang.org/x/perf/cmd/benchstat@latest` then `benchstat <old-report>
  <new-report>`.
- `go test ./internal/config/... ./commands/... ./internal/platform/...` (the
  full functional suite, not just benchmarks) must stay green — every
  optimization in this document is validated against the existing test suite
  (including the H1/H2/H7/H8/H14/H16 security-gate tests) with zero relaxation.

## Workflow-perf-optimization Phase 0 baselines (2026-07, Apple M4 Pro, -benchtime=10x)

Measure-first baselines for the repo-wide perf plan (spec:
`.agents/workflow/specs/workflow-perf-optimization/design.md`). **Headline proofs
are measured on Windows** (git-subprocess cost is ~10x there); the Mac numbers
below are the fast inner-loop signal + the alloc/spawn regression guard (both
machine-stable). Each row names the hypothesis it will prove.

### commands/workflow (git-spawn counted)
| bench | ns/op | allocs/op | git-spawns/op | target |
|--|--|--|--|--|
| CollectWorkflowState | 24.0ms | 725 | 5 | H2 |
| RunWorkflowCheckpoint | 34.1ms | 2768 | 7 (dup isGitRepo probe) | H2 |
| WritePlanStateRefCAS tasks=1/10/50 | 40.9/86.0/292.3ms | 749/1539/5059 | 8.9/18.8/62.8 (O(tasks)) | H3 |
| ReadPlanTaskRecordsFromStateRef tasks=1/10/50 | 14.0/55.6/251.0ms | 402/2540/12066 | 3/12/52 (O(tasks)) | H3 |
| LoadIterLogDocument v1/v2 | 39.9/16.4us | 537/222 | 0 | H9 (single-pass v1 decode) |
| AppendHookOutcome | 214.7us | 799 | 0 | H9 (record-granular validate) |

### internal/graphstore (@5k nodes)
| bench | ns/op | allocs/op | target |
|--|--|--|--|
| GetImpactRadius | 72.8ms | 575,996 | H7 (BFS + per-row decodeExtra) |
| GetStats | 1.41ms | 149 (flat) | already efficient |
| SearchNodes | 2.22ms | 37,830 | H7 |
| GetEdgesAmong | 13.7ms | 145,399 | H7 (batch/decode) |

### internal/dashboard/store (@100 sessions x 20 iters)
ListRuns / GetRun / ListIterations / Health all ~170-180ms, ~1.6M allocs/op —
every query reparses the whole root via `sessions()`. Target: **H8**.

### internal/adapters/builtin/crg (@2k symbols)
Flows/Communities ~0.73/0.87ms, ~3.9k allocs; RiskIndex 106us, 13 allocs (flat).
Target: **H7** (postprocess adjacency churn).

### internal/platform + internal/config
BuildPipelineSpec 3.5us/16 allocs; OMP/CC emit 29.7/79.0us, 94 allocs;
ScopedResourceScan N200 946us/2428 allocs; LoadAgentsRC N200 513us/1717 allocs;
GenerateAgentsRC N200 1.71ms/4060 allocs. Targets: **H6** (agentsrc/resources
caching), pipeline-emit builder churn.

### Windows headline baselines (commands/workflow, pap-home, -benchtime=3x)

The ~10-14x git-spawn amplification vs Mac, measured on a real Windows box — the
environment the optimization must satisfy (Windows-in-the-loop proof rule).

| bench | Mac ns/op | Windows ns/op | Win/Mac | git-spawns |
|--|--|--|--|--|
| CollectWorkflowState | 24.0ms | 254.8ms | 10.6x | 5 |
| RunWorkflowCheckpoint | 34.1ms | 476.9ms | 14.0x | 7 |
| WritePlanStateRefCAS tasks=50 | 292ms | **4208ms** | 14.4x | 74 |
| ReadPlanTaskRecordsFromStateRef tasks=50 | 251ms | 2756ms | 11.0x | 52 |
| LoadIterLogDocument v1 | 39.9us | 271.8us | 6.8x | 0 |
| AppendHookOutcome | 214.7us | 5214us | 24x | 0 |

Headline: a single 50-task state-ref write is **4.2s on Windows** (74 git-spawns,
O(tasks)) — the H3 batch/in-process case in one number. git-spawn counts are
deterministic across OS (the machine-independent H1-H3 regression metric);
wall-clock is the Windows headline each optimization re-measures.
