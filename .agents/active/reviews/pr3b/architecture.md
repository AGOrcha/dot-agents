# Architecture Review — pr3b/workflow (PR#16)

_Lens 1 of 3 (architecture + standards), general-purpose reviewer, read-only._

## Verdict
**approve with nits**

## Critical findings
None. `go build ./...` passes; no import cycle; dependency direction is one-way
(`commands` → `commands/workflow` → `internal/graphstore`; graphstore imports
nothing from `commands`).

## Major findings

1. **`migrations.go` is not a migration system — the name misleads.** It is a
   single idempotent `schemaSQL` DDL blob (all `CREATE ... IF NOT EXISTS`)
   applied at `sqlite.go:56`. No `PRAGMA user_version`, no version table, no
   ordered steps. Additive-only by construction (zero out-of-order/irreversible
   risk today) — but there is **no mechanism to evolve an existing column** (a
   future `ALTER TABLE` silently no-ops on old DBs because the `CREATE` is
   skipped). Acceptable for a fresh port; recommend renaming to `schema.go` or
   adding a `user_version` seam before the first destructive schema change.
   Flag now so the gap is a conscious decision.

2. **Draft-plan surfacing (`aaa21dc`) is a user-visible contract change.**
   `eligible`/`next`/`orient` now emit a draft-plans warning and a top-level
   `draft_plans` JSON field (state.go:339-371). Net improvement over the prior
   silent skip, backward-compatible (additive field), so **not breaking** — but
   it changes a JSON contract agents/scripts parse and should be noted in the
   PR description / changelog. No code change required.

## Minor findings / nits

1. **`buildEdgeAdjacency`/`edgeRowIterator` (commit 73ff88f) is sound.** The
   3-method interface (`Next/Scan/Err`) is the correct common subset of
   `*sql.Rows` and `pgx.Rows`; both `GetImpactRadius` paths are now symmetric.
   The sqlite path correctly **gained** the previously-missing `rows.Err()`
   check with no behavior regression. Ignoring `rows.Close()`'s return is
   acceptable and now consistent (sql Close error meaningless after a fully
   drained read-only SELECT; pgx Close is void). Explicit `rows.Close()` before
   the err check (not `defer`) is the right ordering.

2. **`scripts/internal/covprofile` placement is correct.** A real `internal`
   package imported by the `//go:build ignore` dev scripts is the sound dedup
   approach — ignored files can't be imported but can import; `internal/`
   scopes it to `scripts/`.

3. **graphstore `Store` is a clean abstraction.** sqlite + postgres implement
   the full interface; callers (`graph.go:278`) depend on the interface, no
   concrete-type leaks into `commands/workflow`. `CRGBridge` is correctly **not**
   a `Store` (distinct method set, reads CRG's own read-only SQLite file) —
   properly isolated. `internal/graphstore/internal/storetest` uses
   double-`internal` to restrict the harness to the graphstore subtree —
   correct.

4. **`NewWorkflowCmd()`/`Deps` matches the pr3a pattern** (closure-injected
   flag getters, `InitTestDeps`, minimal `root.go` wiring).

## Notes
- Cohesion: only ~21 non-test source files (the "60" is mostly tests). Seams
  are clear (cmd/state/plan_task/delegation/*_schema/seams). Not a dumping
  ground. `plan_task.go` (82KB) and `delegation.go` (54KB) are large but
  topic-coherent; splitting optional.
- ~12 `coverage_pushN_test.go` files named only by iteration number convey no
  intent — maintenance smell (out of scope here).
- Postgres backend is implemented + testcontainer-tested but not yet wired into
  the CLI (only `OpenSQLite` called). Forward-compatible infra; acceptable.
