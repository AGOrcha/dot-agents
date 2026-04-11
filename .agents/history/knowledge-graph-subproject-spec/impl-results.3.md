# Implementation Results 3

Date: 2026-04-10
Task: KG Phase 3 — Deterministic Query Surface

## What was done

### Types (`commands/kg.go`)
- `GraphQuery` — intent, query, scope, limit
- `GraphQueryResult` — id, type, title, summary, path, source_refs
- `GraphQueryResponse` — normalized envelope (schema_version, intent, query, results, warnings, provider, timestamp)
- `isValidQueryIntent()` — validates against 9 supported intents

### Search engine
- `scoreMatch()` — 5-tier relevance: exact title > title prefix > title substring > summary substring > body substring
- `searchNotes(kgHome, noteType, query, limit)` — walks type-specific or all subdirs, scores and ranks
- `searchByLinks(kgHome, noteID)` — follows `links` field from a note, loads and returns linked notes

### Intent dispatch
- `executeQuery()` — switches on all 9 intents; stubs `contradictions` with warning; wraps health snapshot for `graph_health`; logs every query to `notes/log.md`
- `executeBatchQuery()` — runs a slice of queries, collects all responses
- `sortedKeys()` helper for deterministic intent listing

### Command
- `kg query [query] --intent <intent> [--limit N] [--scope s]` — table display or `--json` normalized response

## Verification
```
go test ./... — all green (14 new Phase 3 tests)
go run ./cmd/dot-agents kg query --intent decision_lookup cobra
  → finds dec-test-source-2 (from Phase 2 ingest)
go run ./cmd/dot-agents kg query --intent entity_context "Claude Code"
  → finds ent-claude-code
go run ./cmd/dot-agents kg query --intent graph_health --json
  → {status=healthy, notes=5, queue=0}
```

## Next
KG Phase 4 (Lint & Maintenance) — broken link detection, stale note surfacing, contradiction detection stub upgrade
