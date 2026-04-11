# Implementation Results 2

Date: 2026-04-10
Task: KG Phase 1 (Graph Core) + KG Phase 2 (Basic Ingest) implementation

## What was done

### KG Phase 1 — Graph Core (commands/kg.go, commands/kg_test.go)
- All types: `KGConfig`, `GraphNote`, `IndexEntry`, `GraphHealth`
- Path helpers: `kgHome()`, `kgConfigPath()`
- Config CRUD: `loadKGConfig`, `saveKGConfig`
- Note parse/render: `parseGraphNote`, `renderGraphNote` (YAML frontmatter + markdown body)
- Index/log: `appendLogEntry`, `readLogEntries`, `updateIndex`, `readIndex`
- Health: `computeGraphHealth`, `writeGraphHealth`, `readGraphHealth`
- Commands: `kg setup`, `kg health` — registered under `NewKGCmd()` → `main.go`
- 100% tests passing

### KG Phase 2 — Basic Ingest (commands/kg.go, commands/kg_test.go)
- `RawSource` struct + `recordRawSource`, `moveToImported`, `listPendingRawSources`
- Extraction heuristics: `extractClaims`, `extractEntities`, `extractDecisions`
- Note creation/update: `createGraphNote`, `updateGraphNote`, `noteExists`
- Ingest pipeline: `ingestSource` — source summary note + entity/decision note extraction, cross-linking, health update
- Helpers: `slugify`, `summarize`, `truncate`
- Commands: `kg ingest [file] [--all] [--dry-run]`, `kg queue`
- All tests passing; smoke-tested via `go run ./cmd/dot-agents`

### Bug fixed
- `TestUpdateIndex_AddAndReplace`: test used `strings.Count(content, "dec-001")` which counts 2 for a single valid entry (ID appears in link anchor + file path). Fixed to count `"- [dec-001]"` prefix occurrences.

## Verification
```
go test ./... — all green
go run ./cmd/dot-agents kg setup   — initializes ~/knowledge-graph layout
go run ./cmd/dot-agents kg ingest test-source.md — 5 notes created (src, entities, decisions)
go run ./cmd/dot-agents kg health  — shows 5 notes, healthy status
```

## Trace notes
- Entity extraction caps at 5 per source and decision extraction at 3 to avoid noise
- `walkNoteFiles` and `extractClaims` are implemented but not yet called from commands (available for Phase 3+)
- `updateGraphNote` ready for Phase 3 query/lint workflows
