# Spec: graphstore public API contract + concurrent-invocation strategy

Status: SHIPPED + archived. Decision C (Hybrid) ratified 2026-05-17; canonical plan
gcc1–gcc5 implemented across PRs #30/#34/#57/#66 and archived at
`.agents/history/graphstore-concurrency-contract/` (PLAN.yaml status: archived;
impl-results.md confirms gcc1–gcc5 completion). All four Done criteria are met in
shipped code — see Done criteria section below for per-criterion evidence. Spec retained
as the design record. Originally drafted from PR #16 review threads (maxNodes Low-1,
deps-singleton Low-2).

## Problem

`da` is invoked as many short-lived OS processes, **concurrently, by
multiple agents** (this is the real usage pattern, not a single-process
CLI). Each invocation independently pays full resource setup:

- **SQLite**: opens its own connection; `SetMaxOpenConns(1)`+WAL means
  cross-process readers coexist but writers serialize → hard
  `SQLITE_BUSY` after 5s under concurrent-write contention.
- **CRG**: every query spawns a fresh Python subprocess
  (`crg.go` exec.Command ×3) — interpreter start + `import
  code_review_graph` cost *per call*. The dominant cost under load.
- **No cross-process reuse is possible** without a persistent process —
  separate OS processes cannot share an in-memory pool. So "connection
  pool / reuse within a time window so invocations don't each create
  their own conns" inherently requires a broker, not just a pool.
- Bounds (`maxNodes`/`maxDepth`) are **advisory, not enforced**
  (overshoot by a frontier); behavior differs by path (native BFS vs
  CRG bridge) → "works differently depending how it's used".

## Core decision (the principle)

**Contract-first.** Define a single, stable `graphstore` public API
contract such that the concurrency/optimization strategy is an
*implementation detail behind it*. Downstream callers and the injected
`Deps` handle bind to the contract, never to a backend or a process
model — so swapping ephemeral→pooled→daemon is transparent and "either
way it's used, it works as intended."

The contract MUST specify (backend- and strategy-agnostic):

1. **Enforced bounds.** `maxNodes`/`maxDepth`/result limits are *hard*
   (caller-visible guarantee), uniform across native + CRG paths, plus
   a request timeout. (Subsumes maxNodes Low-1: the fix becomes
   "enforce the contract", not a one-off patch.)
2. **Concurrency semantics, documented.** Read vs write guarantees;
   that a handle is single-goroutine within a process; that
   cross-process safety/serialization is the *provider's* job, not the
   caller's.
3. **Lifecycle.** Acquire/release is explicit and cheap; callers never
   manage backend connections.
4. **`Deps`/DI is the contract boundary.** The package-level `deps`
   singleton (di-refactor OD-1) is acceptable **iff** it holds a
   contract-typed handle whose provider owns
   pooling/serialization — the singleton stops being the concurrency
   story; the provider is. This resolves OD-1's (A) path with teeth.

## DECISION (maintainer, 2026-05-17): **C — Hybrid**

Build the stable `Store` contract + **Path A** (lazy/cheap ephemeral,
enforced bounds + timeout, document SQLite low-write-concurrency,
recommend Postgres+pooler for heavy deploys) **now**. Keep **Path B**
(persistent daemon owning pool + warm CRG worker) as a *transparent
provider swap behind the unchanged contract*, introduced later when
measured load justifies it. The contract is the prerequisite for both
and the thing that makes the later A→B swap invisible downstream.

Implication: the contract + bound-enforcement is the unit of work;
the daemon is explicitly deferred (not designed away — the contract
must not assume an ephemeral provider).

## Strategy options (for the record — C chosen)

- **A. Ephemeral + cheap + bounded.** Keep per-process invocations;
  lazy/late store open (only when a command needs the graph),
  short-lived read-mostly conns, enforced bounds + timeouts; document
  SQLite as low-write-concurrency and recommend Postgres + external
  pooler (pgbouncer) for heavy concurrent deployments; CRG stays
  per-call (optionally a warm CRG worker later). No daemon. Lowest
  complexity; does NOT truly pool across invocations (accepts that).
- **B. Persistent local daemon.** Promote the existing
  `MCPServer.Serve` into a long-lived local service owning the
  connection pool + a warm CRG worker; CLI invocations become thin
  clients (unix socket / reuse MCP framing) with autostart + idle
  shutdown ("reuse within a time window"). The only design that
  literally satisfies "invocations don't each create their own conns".
  Highest complexity (lifecycle, autostart, staleness, security of the
  socket); biggest payoff under heavy multi-agent load.
- **C. Hybrid.** Contract + Path A now (correctness, enforced bounds,
  cheap ephemeral); Path B later as a transparent provider swap behind
  the same contract once load justifies it. Sequences risk; contract
  work is the prerequisite for both.

## Done criteria — all met (shipped)

- **One published `Store` contract; all callers + `Deps` bind to it.** MET.
  `internal/graphstore/store.go:231-237` defines `Store` composing five roles
  (`CodeGraphReader`/`CodeGraphWriter`/`KGNoteStore`/`NoteSymbolLinkStore`/`Closer`,
  `:170-222`); compile-time `var _ Role` assertions at `:246-261`; DI boundary `Handle`
  at `:276-301`; callers bind via `commands/workflow/deps.go:35`.
- **Bounds enforced + uniform across native/CRG; request timeout honored;
  regression tests prove the hard cap and cross-path parity.** MET.
  Single chokepoint `internal/graphstore/bounds.go:30-53` (`hardMaxNodes=5000`,
  `hardMaxDepth=12`, `hardSearchLimit=2000`, `requestTimeout=30s`); CRG routes through
  the same helpers (`crg.go:694,143,147,154`). Hard cap: `bounds_enforcement_test.go:26-120`
  and `regression_contract_test.go:46-129`; cross-path parity: `:165-282`. Note: the
  pool is now `SetMaxOpenConns(512)` (`sqlite.go:94`), superseding the stale
  `SetMaxOpenConns(1)` text in the Problem section above.
- **Concurrency model documented; chosen strategy C-Hybrid implemented behind the
  unchanged contract.** MET. `internal/graphstore/CONTRACT.md:104-149` documents
  ownership. Path A shipped: lazy open `lazy.go:25-27,255-260`; SQLite WAL +
  `busy_timeout=5000` (`sqlite.go:99,130`); Postgres via `pgxpool`
  (`postgres.go:11,17,23-24`).
- **maxNodes Low-1 closed via the contract; di-refactor OD-1 closed
  (singleton justified by the provider-owns-concurrency rationale).** MET.
  Low-1: `regression_contract_test.go:39-68` (`TestRegression_MaxNodes_Low1_ClosedViaContract`).
  OD-1: `.agents/history/di-refactor-rollout/OPEN-DECISIONS.md:32-46` + rationale at
  `commands/workflow/deps.go:18-25`.

## When Path B (daemon) becomes required — trigger criteria

**Owner ruling, ratified 2026-07-04.** Path B (persistent daemon) remains deferred per
Decision C. The maintainer ruled: keep deferred until measured load justifies it, AND a
concrete trigger must be defined. Maintainer's words: "we need a metric / criteria for
knowing when load is too much and daemon is required."

The following are proposed observable signals that would flip the deferral. Exact numeric
thresholds are TBD pending instrumentation (instrumenting these metrics is a tracked
follow-on — not built in gcc1–gcc5):

- **SQLite write-contention rate:** sustained `SQLITE_BUSY` / retry rate above a threshold
  over a measurement window (e.g. more than 5% of write attempts busy-looping past
  `busy_timeout=5000ms`).
- **Write-transaction p99 latency:** p99 lock-acquire or write-transaction latency
  exceeding a bound that causes measurable agent-pipeline slowdowns (exact bound TBD).
- **Sustained concurrent-writer count:** concurrent-writer process count above N sustained
  across a measurement window (exact N TBD — likely 8–16 for typical multi-agent fanout).
- **WAL checkpoint stall / growth:** WAL file growing faster than checkpoint can drain,
  indicating sustained write pressure without adequate reader-writer balance.
- **CRG subprocess overhead dominates query latency:** per-call Python subprocess startup
  cost accounting for a majority of total query latency at sustained load (signals that a
  warm-CRG worker would recover meaningful time).

These signals are the observable conditions that justify a transparent Path-A-to-Path-B
provider swap behind the unchanged `Store` contract. The contract is deliberately
Path-B-ready (`CONTRACT.md:219-228`).

## Executor tier vs the `Store` seam (O7 reconciliation)

The graph **executor** ([graph-backend-adapter-contract §2.7](../graph-backend-adapter-contract/design.md#27-the-executor-is-an-architectural-tier-separate-from-the-contract))
sits *above* this `Store` contract: it owns traversal algorithm, DSL ref-join
lowering, and namespace-token resolution-at-boundary, and it reads/writes
*exclusively* through the role-segregated `Store` roles
(`CodeGraphReader`/`CodeGraphWriter`/`KGNoteStore`/`NoteSymbolLinkStore`/`Closer`)
— never bypassing them and never opening a backend directly. This `Store`
provider owns the orthogonal concern of connection/concurrency/bounds (the
ephemeral→pooled→daemon C-Hybrid swap below). An executor swap (B-tree → CSR)
and a provider swap (A → B daemon) are **independent**; neither is a contract
change to the other. The executor tier is single-sourced in adapter-contract
§2.7 — this note points *up* to it rather than re-specifying it. The shipped
role definitions live in [`internal/graphstore/CONTRACT.md`](../../../../internal/graphstore/CONTRACT.md)
([O7](../graph-backend-adapter-contract/open-questions-resolutions.md#o7--graphstore-store-seam-vs-adapter-executor-seam-reconciliation)).

## Relationship to existing artifacts

- Supersedes the standalone maxNodes follow-up (folds into contract
  enforcement).
- Closes/*informs* `di-refactor-rollout` OD-1 — di-refactor should not
  propagate the Deps pattern until this contract pins the boundary
  semantics.
- Independent of `testcontainers-separate-module` /
  `ci-venv-crg-interim` (different concerns).
- **[Shipped]** Decision C ratified 2026-05-17; Path A implemented and archived
  (gcc1–gcc5, PRs #30/#34/#57/#66). Path B deferred pending measured load; trigger
  criteria defined in the section above. This spec superseded the standalone maxNodes
  follow-up and informed di-refactor OD-1 closure.

## Tracked follow-ons (not built here)

- **Bulk-export bound exemption:** ratify the documented exemption for `ReadNodes`/`ReadEdges`
  (full-graph sync is legitimately unbounded; timeout still applies), or route a
  streaming/paged export to adapter-contract. Already flagged in `CONTRACT.md:200-217`;
  one-line ratification pending.
- **Daemon-trigger metric instrumentation:** implement the observable signals defined in the
  Path B trigger criteria section above so the deferral decision can be re-evaluated with
  real load data.
