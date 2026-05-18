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
operation. It is **segregated into cohesive role interfaces** (grouped by
how callers actually use the store) and then composed: `Store` embeds all
roles. It is derived from the operations callers already use against the
concrete stores — not over-specified with speculative methods.

Callers and the injected `Deps` handle bind to `Store` (or, preferably, a
narrower role), **never** to a concrete backend (`*SQLiteStore`,
`*PostgresStore`) and **never** to a process model. That binding is what
makes the ephemeral → pooled → daemon evolution a transparent provider
swap with no caller-visible change.

## Role segregation (ISP) — depend on the narrowest role

The 28-method surface is split into five roles. **A caller, and the Deps
handle, should depend on the narrowest role it actually uses.** This is
the Interface-Segregation point: a test fake stubs only that role's
handful of methods, not all 28 — exactly what the cg6b 95%-coverage tail
exploits.

| Role | Methods | Typical caller |
|---|---|---|
| `CodeGraphReader` | `GetNode`, `GetNodesByFile`, `GetEdgesBySource/Target/Among`, `GetAllFiles`, `SearchNodes`, `GetMetadata`, `GetStats`, `GetImpactRadius` | read-mostly: status, review, impact, orient |
| `CodeGraphWriter` | `UpsertNode`, `UpsertEdge`, `RemoveFileData`, `StoreFileNodesEdges`, `SetMetadata`, `Commit` | build/update pipeline only |
| `KGNoteStore` | `UpsertKGNote`, `GetKGNote`, `SearchKGNotes`, `ListArchivedKGNotes` | KG curation/sync |
| `NoteSymbolLinkStore` | `UpsertNoteSymbolLink`, `GetLinksForNote/ForSymbol`, `DeleteNoteSymbolLink` | warm-link sync |
| `Closer` | `Close` | the handle owner only — borrowed handles must not depend on it |

`Store = CodeGraphReader + CodeGraphWriter + KGNoteStore +
NoteSymbolLinkStore + Closer` (interface embedding). Whole-store
callers and `var _ Store = (*SQLiteStore)(nil)` /
`(*PostgresStore)(nil)` still hold unchanged; each role also has its own
`var _ Role = (*SQLiteStore)(nil)` / `(*PostgresStore)(nil)` assertion so
narrowing to any role is compiler-guaranteed safe.

### How a caller picks a role

1. Identify the single concern the caller exercises (it almost always
   uses exactly one of read / write / KG-note / link).
2. Depend on that role interface in the function/struct signature — not
   `Store`. If it genuinely spans concerns, depend on `Store`.
3. Obtain the role by declaring the dependency as the narrow role type
   and assigning it from `Handle.Store()` — a `Store` already *is* every
   role (it embeds them), so no per-role accessor is needed:

   ```go
   var r graphstore.CodeGraphReader = h.Store() // a Store IS a CodeGraphReader
   ```

   This is the idiomatic "accept interfaces" Go: one nil-safe accessor,
   no duplicated narrowing bodies, and the call site documents exactly
   the role it needs. Nil-safety is inherited — an unset handle's
   `Store()` is nil, so the narrowed interface value is nil too.
4. Tests inject a fake implementing only that role.

Do not depend on `Closer` unless the caller owns the handle's lifetime
(it must not `Close` a borrowed handle).

## Provider guarantees

1. **Bounds (enforced — Path A, gcc2).** Operations that take `maxNodes`
   / `maxDepth` / `limit` express the caller's *requested ceiling*, not a
   guarantee the provider will return that many. The provider enforces a
   **hard, uniform** cap, identical across the native BFS path and the CRG
   bridge path:
   - `0` or a negative value means "use the provider default".
   - A value above the provider hard cap is silently clamped down — a
     caller can ask for less than the cap but never more.
   - The impact-radius BFS stops *exactly* at the node cap; it never
     overshoots by a trailing frontier (the pre-Path-A behaviour the spec
     flagged as "advisory, overshoot by a frontier").
   - The same constants bound the CRG path: `GetImpactRadius` clamps
     `MaxDepth`/`MaxResults` and the direct `ReadNodes`/`ReadEdges` reads
     are capped (they can no longer return an entire table unbounded).

   The hard caps live in one place (`bounds.go`) and every provider routes
   `maxNodes`/`maxDepth`/`limit` through the single `clampBound`
   chokepoint, so "uniform across native + CRG" is structural, not a
   convention. (This subsumes the standalone maxNodes Low-1 follow-up: the
   fix is "enforce the contract", not a one-off patch.)
2. **Request timeout (enforced — Path A, gcc2).** Every graph traversal /
   query carries a **provider-owned** deadline. Callers do **not** wrap
   `Store` calls in their own deadline. The native backends apply it to
   the full edge scan + BFS via a context timeout; the CRG bridge applies
   it to the Python subprocess via `exec.CommandContext`, so a runaway
   traversal is killed rather than hanging the invocation. A parent
   context's cancellation is still honoured (the provider deadline does
   not sever caller cancellation).
3. **Concurrency ownership.** A `Store` handle is **single-goroutine
   within a process**. Callers must not share one handle across goroutines
   without their own synchronization. Cross-process safety and
   write-serialization — SQLite's single-writer + WAL behavior, a
   connection pool, or a future broker/daemon — are the **provider's**
   job. They are not the caller's job and not the Deps singleton's job.
4. **Lifecycle (lazy/cheap — Path A, gcc2).** Acquire/release is explicit
   and cheap. Callers obtain a `Store`, use it, and `Close` it; they never
   manage backend connections, pools, or CRG subprocess workers directly.
   `da` runs as many short-lived processes, so a process that never
   touches the graph must not pay the store-open cost. `NewLazyStore`
   wraps a provider-open thunk in a `Store` whose backend is opened on the
   *first* contract call and at most once; constructing it is zero-I/O,
   and `Close` on a never-used handle is a no-op (it does not trigger a
   late open just to close). A `LazyStore` IS a `Store`, so this is a
   transparent provider variant — the future A→B daemon swap stays
   invisible to callers and to the `Deps` handle.

## Backend concurrency guidance (Path A)

Path A is *ephemeral + cheap + bounded*: it does not pool connections
across the separate `da` OS processes (separate processes cannot share an
in-memory pool — that is Path B's job, deferred). Choose the backend by
write-concurrency need:

- **SQLite (default) — low write concurrency.** SQLite opens one
  connection with WAL + `busy_timeout=5000`. Concurrent *readers* across
  processes coexist fine; concurrent *writers* serialise and, after ~5s of
  contention, surface a hard `SQLITE_BUSY` rather than queueing
  indefinitely. That is a user-visible flake under heavy concurrent-write
  load (many agents writing the same graph at once), not data loss.
  Acceptable for the common single-orchestrator usage; **not** suitable as
  the shared store for a heavily concurrent multi-agent deployment.
- **Postgres + external pooler — heavy concurrent deployments.** For
  deployments with many agents writing concurrently, use the Postgres
  backend behind an external connection pooler (e.g. **pgbouncer** in
  transaction-pooling mode). Postgres provides real MVCC write
  concurrency, and the external pooler absorbs the connection churn that
  per-invocation ephemeral opens would otherwise impose on the server.
  This is the recommended Path A configuration for heavy load until Path B
  (a persistent local daemon owning the pool + a warm CRG worker) lands as
  a transparent provider swap behind this unchanged contract.

The bounds + request-timeout guarantees above hold identically on both
backends and on the CRG path; only the write-concurrency characteristics
differ, and they are a deployment choice, not a contract difference.

## Deps boundary (di-refactor OD-1)

The package-level `deps` singleton in `commands/*` is acceptable **iff**
it holds a *contract-typed* handle whose provider owns pooling and
serialization. The singleton is only a holder of the contract; it is
**not** the concurrency story — the provider behind the contract is.

`graphstore.Handle` pins that boundary:

- `Handle` carries a single `Store` and exposes it only through the
  contract-typed `Store()` accessor — never a concrete backend.
- The singleton (and gcc3-bound callers) narrow to the role they need by
  declaring that role type and assigning from `Store()`; a `Store` is
  already every role. One accessor, no duplicated narrowing code.
- `Store()` is nil-safe: an unset handle yields a nil `Store` (hence a
  nil narrowed role), so callers keep their existing direct-open path
  until gcc3 wires this end-to-end.

This closes di-refactor OD-1's path (A) "with teeth": the singleton is
justified by the provider-owns-concurrency rationale, not waved through.

## Scope of gcc1 (this artifact) — and what is deferred

**In scope (this PR, additive, no behavior change):**

- Publish + document the `Store` contract, segregated into five role
  interfaces composed into `Store` (godoc + this file).
- Compile-time assertions for the composed `Store` AND each role against
  `(*SQLiteStore)(nil)` and `(*PostgresStore)(nil)`.
- Define the contract-typed `Handle` boundary with a single nil-safe
  `Store()` accessor the Deps singleton will hold; callers narrow to a
  role by declaring the role type and assigning from `Store()`.

**Delivered (Path A — gcc2, this PR):**

- Hard, uniform `maxNodes`/`maxDepth`/`limit` enforcement across the
  native BFS and CRG paths via the single `bounds.go` chokepoint; the
  impact BFS no longer overshoots by a frontier.
- Provider-owned request timeout on native traversals (context) and the
  CRG Python subprocess (`exec.CommandContext`).
- Lazy/cheap ephemeral open (`NewLazyStore`) — late, at-most-once,
  zero-I/O construction, no-op close when unused.
- SQLite documented as low-write-concurrency; Postgres + external pooler
  (pgbouncer) recommended for heavy concurrent deployments.

The `Store` interface is **unchanged** by gcc2 — all of the above is
provider-internal behaviour behind the contract published by gcc1, so the
future A→B swap stays transparent.

### Contract-pressure flagged by Path A (for the spec / gcc3)

The "hard, uniform `limit` cap, `0` = provider default" bound model fits
user-facing *bounded* queries (`SearchNodes`, `GetImpactRadius`,
`SearchKGNotes`) cleanly. It does **not** fit the CRG bulk-export reads
`ReadNodes`/`ReadEdges`, which the warm-link sync calls with `0` meaning
"mirror the ENTIRE graph". Applying the cap there would silently truncate
a legitimate full-graph sync on any non-trivial repo. Path A therefore
left those two methods at their `0 = all` semantics (timeout still
applies) and did **not** bend the contract to match. Resolution is
deferred to the spec/gcc3: either explicitly exempt bulk export from the
bound guarantee, or give export a streaming/paged contract. This is
recorded here rather than silently resolved.

Also note: enforcing the hard depth cap means a caller asking for
`maxDepth` greater than the cap (e.g. an old call passing `20`) is now
clamped to the documented hard cap rather than honoured — intended Path A
behaviour, called out so it is not mistaken for a regression.

**Deferred:**

- **gcc3 — bind all callers.** Refactor every call site and the
  `commands/*` Deps singletons to hold/use `graphstore.Handle` /
  `NewLazyStore` instead of opening concrete stores directly.

Path B (persistent local daemon) remains explicitly deferred and is a
later transparent provider swap behind this unchanged contract — it is
deferred, **not** designed away: the contract must not assume an ephemeral
provider.
