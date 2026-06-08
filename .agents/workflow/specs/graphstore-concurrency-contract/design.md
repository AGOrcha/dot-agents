# Spec: graphstore public API contract + concurrent-invocation strategy

Status: active (decision C locked) (maintainer-directed analysis 2026-05-17,
from PR #16 review threads: maxNodes Low-1, deps-singleton Low-2)

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

## Done criteria (when planned)

- One published `Store` contract; all callers + `Deps` bind to it.
- Bounds enforced + uniform across native/CRG; request timeout honored;
  regression tests prove the hard cap and cross-path parity.
- Concurrency model documented; chosen strategy (A/B/C) implemented
  behind the unchanged contract.
- maxNodes Low-1 closed *via the contract*; di-refactor OD-1 closed
  (singleton justified by the provider-owns-concurrency rationale).

## Relationship to existing artifacts

- Supersedes the standalone maxNodes follow-up (folds into contract
  enforcement).
- Closes/*informs* `di-refactor-rollout` OD-1 — di-refactor should not
  propagate the Deps pattern until this contract pins the boundary
  semantics.
- Independent of `testcontainers-separate-module` /
  `ci-venv-crg-interim` (different concerns).
- No code changed. Pivotal fork (A/B/C) is a maintainer decision before
  this graduates to a plan.
