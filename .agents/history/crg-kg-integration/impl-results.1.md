# Implementation Results 1

Date: 2026-04-11
Task: CRG+KG Integration — Phase A: GraphStore interface + SQLite backend

## Summary

Ported the core Python code-review-graph storage layer to Go as `internal/graphstore`, extended with KG-specific tables for the unified warm-layer design.

## Files Created

### `internal/graphstore/store.go`
- `GraphStore` interface with 26 methods (code graph read/write, KG notes, note→symbol links, lifecycle)
- Types: `NodeInfo`, `EdgeInfo`, `GraphNode`, `GraphEdge`, `GraphStats`, `ImpactResult`, `KGNote`, `NoteSymbolLink`
- Constants: `NodeKind*` (File, Class, Function, Type, Test), `EdgeKind*` (CALLS, IMPORTS_FROM, INHERITS, IMPLEMENTS, CONTAINS, TESTED_BY, DEPENDS_ON)

### `internal/graphstore/migrations.go`
- `schemaSQL` constant — DDL for all tables and indexes
- Tables: `nodes`, `edges`, `metadata` (from Python CRG), plus new `kg_notes`, `note_symbol_links`
- Indexes on all hot query paths (file_path, kind, qualified_name, source/target, note_type, archived_at)
- WAL mode + busy_timeout pragmas set on open

### `internal/graphstore/sqlite.go`
- `SQLiteStore` — pure-Go SQLite via `modernc.org/sqlite` (no CGO)
- `OpenSQLite(dbPath)` — creates parent dirs, initializes schema idempotently
- Code graph: `UpsertNode` (ON CONFLICT upsert), `UpsertEdge` (find-or-insert), `RemoveFileData`, `StoreFileNodesEdges` (atomic tx)
- Read: `GetNode`, `GetNodesByFile`, `GetEdgesBySource/Target`, `GetEdgesAmong` (batched 450-at-a-time for SQLite variable limit), `GetAllFiles`, `SearchNodes` (LIKE scan), `GetStats`
- `GetImpactRadius` — pure-Go BFS on in-memory adjacency map, traverses both forward and reverse edges, capped at `maxNodes`
- KG notes: `UpsertKGNote`, `GetKGNote`, `SearchKGNotes`, `ListArchivedKGNotes`
- Note→symbol links: `UpsertNoteSymbolLink` (idempotent ON CONFLICT DO NOTHING), `GetLinksForNote`, `GetLinksForSymbol`, `DeleteNoteSymbolLink`

### `internal/graphstore/sqlite_test.go`
37 tests covering:
- Schema init + idempotent open
- Metadata round-trip, missing key, overwrite
- Node: create, update (upsert same qualified name), get round-trip, not-found, by-file
- Edge: create, update, by-source, by-target
- RemoveFileData, StoreFileNodesEdges (atomic replace)
- SearchNodes, limit enforcement, GetAllFiles
- GetStats (empty and populated)
- GetEdgesAmong (batched), empty input
- GetImpactRadius (basic BFS, empty files, maxNodes cap)
- KGNote: upsert, update in-place, not-found, search, list-archived
- NoteSymbolLink: round-trip, idempotent, get-for-symbol, delete
- Persistence across close/reopen

## Dependency Added

`modernc.org/sqlite v1.48.2` — pure-Go SQLite driver, no CGO requirement

## Test Results

```
go test ./internal/graphstore/... -v
--- 37 tests all PASS
go test ./... — all packages green
```

## Key Design Decisions

1. **Pure-Go SQLite** (`modernc.org/sqlite`) — avoids CGO complexity and aligns with existing Go-only dependency policy
2. **Schema mirrors Python CRG exactly** — same column names, same ON CONFLICT upsert semantics for drop-in compatibility with future Phase B parser port
3. **BFS without NetworkX** — in-memory adjacency map from a single edge scan; adequate for maxNodes=500 repo-size targets
4. **KG tables extend, not replace** — `kg_notes` and `note_symbol_links` sit alongside code tables in the same DB, enabling cross-ref joins in Phase D
5. **Batched `GetEdgesAmong`** — 450-item SQLite IN clause batches prevent variable-number overflow on dense graphs
