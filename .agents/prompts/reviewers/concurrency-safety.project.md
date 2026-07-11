# Concurrency-safety lens — dot-agents repo overlay

Repo-local committed layer. In this repo the base (`reviewers/reviewer.base.md`) and lens
(`reviewers/concurrency-safety.md`) layers resolve unresolved, so this overlay is **self-sufficient**:
it carries the full lens contract for judging the daemon's concurrency design.

## What this lens judges

Read the diff (`da workflow tasks` for the bundle + acceptance) and judge concurrency **by
construction** — shared-state access, lock discipline, channel/goroutine lifecycle,
cancellation/shutdown, and freedom from races and deadlocks. One lens only; read-only; stay in scope.

## dot-agents grounding (`internal/graphstore/CONTRACT.md`)

The daemon is **Path B**: a persistent local daemon owning the connection pool + a warm CRG worker, a
transparent provider swap behind the unchanged `Store` contract. The contract fixes the boundary:

- A `Store` handle is **single-goroutine within a process** — a borrowed handle shared across
  goroutines without the caller's own synchronization is a defect.
- **Cross-process safety + write-serialization is the provider's job**, not the caller's and not the
  `Deps` singleton's (SQLite single-writer + WAL + `busy_timeout`; Postgres MVCC + external pooler;
  or the daemon's pool). The daemon now IS that provider — it must actually own serialization.
- Every traversal/query carries a **provider-owned deadline** (`exec.CommandContext` for the CRG
  subprocess) AND still honors parent-context cancellation.
- `LazyStore` opens the backend at most once (`sync.Once`); a failed open is **sticky** — the handle
  never silently degrades to half-open.

## APPROVE when

- Shared state is either confined to one goroutine or guarded by a clearly-scoped lock/channel; lock
  order is consistent (documented where non-obvious); no lock held across a blocking call or I/O.
- Every spawned goroutine has a defined owner and exit path; shutdown drains/cancels workers via
  `context` + `errgroup`/`WaitGroup`; no goroutine outlives the daemon's lifecycle.
- Cancellation propagates end-to-end; the CRG subprocess is killed on deadline; `Close` is idempotent
  and race-free.
- The design is race-clean under `go test -race` and deadlock-free by construction (acquire/release
  pairing, no nested-lock cycles).

## REJECT when (name the specific site)

- A borrowed `Store`/handle shared across goroutines without synchronization; a data race the `-race`
  verifier would (or does) catch.
- Unbounded goroutine spawn, a leaked goroutine (no exit on shutdown), or a channel with no closer /
  no reader (send on closed channel, or a permanent block).
- Lock held across I/O or a subprocess call; inconsistent lock ordering → deadlock potential;
  double-`Close`/double-unlock; `busy_timeout` contention treated as corruption instead of a surfaced
  `SQLITE_BUSY`.
- Cancellation/deadline dropped (a runaway traversal that can hang the daemon), or a `sync.Once` open
  that degrades to half-open on failure.

Verdict line `(lens: concurrency-safety)`. `fail` on any BLOCKER/HIGH.
