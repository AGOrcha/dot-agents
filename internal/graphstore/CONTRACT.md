# graphstore Store contract

This document is the human-readable companion to the `Store` interface
godoc in `store.go`. It states the provider guarantees and the
concurrency-ownership / Deps-boundary rule that downstream callers and the
dependency-injection singleton bind to.

Spec: `.agents/workflow/specs/graphstore-concurrency-contract/design.md`
(decision **C-Hybrid**, maintainer 2026-05-17): publish the stable `Store`
contract + Path A now; keep Path B (persistent daemon owning the pool +
warm CRG worker) as a *transparent provider swap behind this unchanged
contract*, introduced later when measured load justifies it.

## What `Store` is

`Store` is the single, stable, backend-agnostic surface for every graph
operation: code-graph read/write, KG-note read/write, note↔symbol links,
bounded queries (`SearchNodes`, `GetImpactRadius`), and lifecycle
(`Close`). It is derived from the operations callers already use against
the concrete stores — it is not over-specified with speculative methods.

Callers and the injected `Deps` handle bind to `Store`, **never** to a
concrete backend (`*SQLiteStore`, `*PostgresStore`) and **never** to a
process model. That binding is what makes the
ephemeral → pooled → daemon evolution a transparent provider swap with no
caller-visible change.

## Provider guarantees

1. **Bounds.** Operations that take `maxNodes` / `maxDepth` / `limit`
   express the caller's requested ceiling. The contract's intent is a
   *hard, uniform* cap across the native BFS path and the CRG bridge path.
   Enforcing that uniformly is the **provider's** responsibility, delivered
   by Path A in **gcc2** — it is not yet enforced by every concrete store
   at the moment this contract is published. (This subsumes the standalone
   maxNodes Low-1 follow-up: the fix becomes "enforce the contract", not a
   one-off patch.)
2. **Request timeout.** Long traversals are bounded by a provider-owned
   request timeout. Callers do not wrap `Store` calls in their own
   deadline.
3. **Concurrency ownership.** A `Store` handle is **single-goroutine
   within a process**. Callers must not share one handle across goroutines
   without their own synchronization. Cross-process safety and
   write-serialization — SQLite's single-writer + WAL behavior, a
   connection pool, or a future broker/daemon — are the **provider's**
   job. They are not the caller's job and not the Deps singleton's job.
4. **Lifecycle.** Acquire/release is explicit and cheap. Callers obtain a
   `Store`, use it, and `Close` it. They never manage backend connections,
   pools, or CRG subprocess workers directly.

## Deps boundary (di-refactor OD-1)

The package-level `deps` singleton in `commands/*` is acceptable **iff**
it holds a *contract-typed* handle whose provider owns pooling and
serialization. The singleton is only a holder of the contract; it is
**not** the concurrency story — the provider behind the contract is.

`graphstore.Handle` pins that boundary:

- `Handle` carries a single `Store` and exposes it only via `Store()`.
- The singleton reads the graph exclusively through `Handle.Store()` and
  can never reach a concrete backend.
- `Handle.Store()` returns `nil` when unset, so callers keep their
  existing direct-open path until gcc3 wires this end-to-end.

This closes di-refactor OD-1's path (A) "with teeth": the singleton is
justified by the provider-owns-concurrency rationale, not waved through.

## Scope of gcc1 (this artifact) — and what is deferred

**In scope (this PR, additive, no behavior change):**

- Publish + document the `Store` contract (godoc + this file).
- Compile-time assertions: `var _ Store = (*SQLiteStore)(nil)` and
  `(*PostgresStore)(nil)`.
- Define the contract-typed `Handle` boundary the Deps singleton will
  hold.

**Deferred (gated on review of this contract):**

- **gcc2 — Path A internals.** Implement hard, uniform bound enforcement
  + request timeout across native and CRG paths; document SQLite as
  low-write-concurrency; recommend Postgres + external pooler for heavy
  concurrent deployments.
- **gcc3 — bind all callers.** Refactor every call site and the
  `commands/*` Deps singletons to hold/use `graphstore.Handle` instead of
  opening concrete stores directly.

Path B (persistent local daemon) remains explicitly deferred and is a
later transparent provider swap behind this unchanged contract — it is
deferred, **not** designed away: the contract must not assume an ephemeral
provider.
