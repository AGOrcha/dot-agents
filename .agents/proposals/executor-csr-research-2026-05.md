# Executor CSR research note (project-local proposal, 2026-05-19)

## Status
**Decision-of-record: parked (2026-05-19; re-confirmed 2026-05-25).** Do not start implementation. Re-open only on the two named triggers (see "Triggers to re-open" section below). Project-local design note for the option of a Go-native CSR executor (pgGraph-style) over the gcc-shipped scoped-KG primitives. The graph-backend-adapter-contract spec's new §2.7 names this as a permitted v2 swap; this note records the analysis backing the decision NOT to build it yet.

## Source
Architecture analysis of `https://github.com/Evokoa/pgGraph`
(Apache-2.0, Rust + pgrx, alpha 0.1.1) shallow-cloned + read
2026-05-19. Cited docs at `docs/contributor_guide/{architecture,
persistence-format, build-pipeline, engine-internals, traversal-
search-paths, sync-internals, memory-model, safety-security,
benchmarking}.mdx` and `docs/user_guide/{limitations-and-fit,
sync-and-maintenance}.mdx`.

## Verdict

**Do not build a Go-native CSR executor v1 yet. Measure first.**

- pgGraph's wins materialize at **>5 hops on >100K edges** with
  multiple Postgres backends sharing mmapped pages. dot-agents does
  not run those workloads today.
- The v1 B-tree / recursive-traversal executor over scoped-KG
  primitives (gcc1's `Store` family + gcc2's Path A) is almost
  certainly adequate for current dot-agents query depth + breadth.
- The hard parts of porting pgGraph (atomic build pipeline, mmap
  page-share across backends, tenant bitmap composition under Rust
  autovec) are exactly the parts that don't help at dot-agents'
  scale. The easy parts (CSR arrays, BFS loop, circuit breakers) are
  the parts that already work fine in the v1 executor.
- pgGraph itself frames its benchmark numbers as alpha and NOT a
  cross-engine claim; the cache-friendly CSR loop is its real win,
  but only when (a) the graph is large enough that recursive-CTE
  planning beats B-tree random IO and (b) you have many concurrent
  backends. dot-agents is one process, hundreds-to-low-millions of
  nodes per scope at most.

## What would change this verdict

Trigger the deferred plan only on one of:

1. **Measured workload exceeds v1 envelope.** Add lightweight
   executor-tier metrics today (depth distribution, neighbor count,
   per-query wall time). If p99 traversal > 50ms on any non-pathological
   workload, or traversals start dominating dot-agents request
   budget, re-open this plan.
2. **An adapter demands deep traversal.** A v1 adapter author files
   a §12.4 v1-blocker for a query that requires >5-hop traversal at
   real scale (e.g. TTRPG `allegiance_path` proves the real query
   pattern is depth 4–6 on a real campaign dataset).
3. **A second backend joins** (e.g. a Postgres CRG mirror under
   real concurrent CI load) where pgGraph's mmap-shared-pages win
   actually applies.

## Deferred plan skeleton (p1–p6)

Captured for future use; do NOT create canonical workflow tasks now.

1. **p1 — csr-on-disk-format-spike.** Define a dot-agents-flavored
   `.dax`/similar on-disk format inspired by pgGraph's 11-section
   layout, scoped to header + active bits + source-id u32 + edge
   offsets + targets + type_ids + (optional weights) + resolution
   index + PK offsets/bytes. Drop bincode; use Go-native length-
   prefix framing. Write a small encoder/decoder + golden-file
   fixture. Round-trip + CRC tamper + malformed-section rejection
   tests.
2. **p2 — mmap-load + validated-pointers.** Implement
   `LoadGraphFile(path)` with header→CRC→section-bounds→CSR-
   monotonicity→target-index-bound validation. Forward CSR via
   `unsafe.Slice` over mmap; derive reverse CSR into heap. Fuzz the
   loader (mirrors pgGraph's `fuzz_targets/load_graph_file.rs`).
3. **p3 — build-pipeline-go (in-memory).** `BuildGraph(input
   GraphInput)`, in-memory v1 (no spill), OOM-policy preflight
   ported from pgGraph. Round-trip vs reload.
4. **p4 — traversal-loop-microbench (GO/NO-GO GATE).** Port
   `bfs::execute` semantics. Benchmark at 2/3/5/8 hops on
   100K-node/1M-edge synthetic vs the v1 recursive executor. **If
   the CSR loop isn't ≥3× the recursive executor at 3+ hops, halt
   the plan and reconsider scope.**
5. **p5 — adapter-contract surface decision.** Design doc deciding
   which pgGraph responsibilities move where (source-row hydration,
   freshness/sync polling, ACL preflight, edge-type registration,
   tenant membership, filter predicates). Pick source-of-truth model
   (Postgres direct vs CRG snapshot vs existing KG store).
6. **p6 — dual-executor plumbing + CRG dual-read.** Wire CSR as
   parallel implementation; dual-read + zero-divergence over 24h
   integration; flip default only when divergence is zero AND CSR p99
   ≤ recursive p50 at 3+ hops.

## Open questions (only when triggered)

1. Adapter-tier mmap library: ship our own `unsafe.Slice`-over-mmap or
   take `golang.org/x/exp/mmap` dep?
2. Source-of-truth model: Postgres mirror, CRG snapshot, or existing
   knowledge-graph filesystem store?
3. u8 edge-label cap (254) or u16 from day 1?
4. Filter index — port or skip in v1?
5. Sync overlays — needed or just "rebuild on change"?
6. Tenant bitmaps — needed at all?
7. Concurrency: per-process or `sync.RWMutex` shared?
8. External-sort spill in build, or in-memory ceiling + fail closed?
9. CRC32 vs xxh3 for format integrity?

## What to do RIGHT NOW (cheap)

Add executor-tier observability to the v1 path:

- depth distribution histogram per query class
- neighbor-count distribution
- per-query wall time (p50/p95/p99)

Wire a structured log or metrics surface so a future "should we
build CSR?" decision is data-driven, not vibes-driven. This work is
in scope of the gcc3/gcc4 follow-ups regardless.

## Not the right approach
- **Rust cgo binding** to pgGraph. Maintainer rejected this 2026-05-19
  — too clunky, insufficient control over query semantics.
- **Pre-emptive port.** No measured signal; opportunity cost on
  gcc3/gcc4, di-refactor-rollout, seam-interface-di-migration tail.
