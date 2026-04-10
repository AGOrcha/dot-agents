# KG Phase 3: Deterministic Query Surface

Spec: `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — Phase 3
Status: Third phase
Depends on: KG Phase 2 (basic ingest — graph has content to query)

## Goal

Provide deterministic query intents with normalized response shapes so agents query the graph through stable operations instead of document spelunking.

## Query Intents

Required initial intents:

| Intent | Description |
|--------|-------------|
| `source_lookup` | Find source summaries by topic or ID |
| `entity_context` | Retrieve graph context for an entity |
| `concept_context` | Retrieve context for a concept |
| `decision_lookup` | Find decisions and rationale by topic |
| `repo_context` | Retrieve repo/subsystem context |
| `synthesis_lookup` | Find synthesis notes |
| `related_notes` | Find notes linked to a given note |
| `contradictions` | Surface conflicting active claims |
| `graph_health` | Return current health snapshot |

## Normalized Response Contract

Every query returns:

```json
{
  "schema_version": 1,
  "intent": "<intent>",
  "query": "<query string>",
  "results": [
    { "id": "...", "type": "...", "title": "...", "summary": "...", "path": "...", "source_refs": [...] }
  ],
  "warnings": [],
  "provider": "local-index",
  "timestamp": "..."
}
```

## Implementation Steps

### Step 1: Query contract types

- [ ] `GraphQuery` struct: intent (string), query (string), scope (string, optional), limit (int, default 10)
- [ ] `GraphQueryResponse` struct: schema_version, intent, query, results ([]GraphQueryResult), warnings, provider, timestamp
- [ ] `GraphQueryResult` struct: id, type, title, summary, path, source_refs
- [ ] `isValidQueryIntent()` validator for the 9 intents
- [ ] Tests: type validation

### Step 2: Index-based search engine

- [ ] `searchNotes(kgHome string, noteType string, query string, limit int) ([]GraphQueryResult, error)`:
  1. Walk `notes/<type>s/` directory (or all `notes/` subdirs if noteType is empty)
  2. Parse each note's frontmatter
  3. Score relevance: exact match on title > substring in title > substring in summary > substring in body
  4. Sort by relevance score, return top N
- [ ] `searchByLinks(kgHome string, noteID string) ([]GraphQueryResult, error)`:
  1. Load the target note
  2. For each ID in its `links` field, load that note
  3. Return as results
- [ ] Tests: search finds relevant notes, ranking works, empty results handled

### Step 3: Intent dispatch

- [ ] `executeQuery(kgHome string, query GraphQuery) (GraphQueryResponse, error)`:
  - Switch on intent:
    - `source_lookup`: searchNotes(kgHome, "source", query.Query, query.Limit)
    - `entity_context`: searchNotes(kgHome, "entity", query.Query, query.Limit)
    - `concept_context`: searchNotes(kgHome, "concept", query.Query, query.Limit)
    - `decision_lookup`: searchNotes(kgHome, "decision", query.Query, query.Limit)
    - `repo_context`: searchNotes(kgHome, "repo", query.Query, query.Limit)
    - `synthesis_lookup`: searchNotes(kgHome, "synthesis", query.Query, query.Limit)
    - `related_notes`: searchByLinks(kgHome, query.Query) (query.Query is a note ID)
    - `contradictions`: findContradictions(kgHome) (Phase 4 stub — return empty for now)
    - `graph_health`: wrap readGraphHealth into response
  - Build normalized response with provider="local-index", timestamp=now
- [ ] Tests: each intent returns correct shape, unknown intent rejected

### Step 4: `kg query` subcommand

- [ ] `kgQueryCmd` (Use: "query") with `runKGQuery()`:
  - Required flag: `--intent`
  - Arg: query string (or `--id` for related_notes)
  - Optional flags: `--scope`, `--limit` (default 10)
  1. Validate intent
  2. Execute query
  3. Display results: table with id/type/title/summary
  4. `--json` flag for normalized response
- [ ] Tests: query with valid intent, missing intent, JSON output matches contract

### Step 5: Batch query support

- [ ] `executeBatchQuery(kgHome string, queries []GraphQuery) ([]GraphQueryResponse, error)` — run multiple queries, return all responses
- [ ] Useful for agents that need multiple intents in one call
- [ ] Tests: batch with 2-3 queries

### Step 6: Log queries

- [ ] Append query events to `notes/log.md`: `## [date] query | <intent>: <query>`
- [ ] Tests: log entry created after query

## Files Modified

- `commands/kg.go`
- `commands/kg_test.go`

## Acceptance Criteria

- Every supported intent returns a normalized response
- Agents can query by intent without knowing file layout
- Results include id, type, title, summary, path, source_refs
- Unknown intents are rejected with clear error

## Verification

```bash
go test ./commands -run 'KGQuery|QueryIntent|SearchNotes|GraphQuery'
go test ./commands
go test ./...
```
