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
