# KG Phase 2: Basic Ingest

Spec: `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — Phase 2
Status: Completed (2026-04-10)
Depends on: KG Phase 1 (graph core)

## Goal

Turn raw material into curated graph knowledge through a repeatable ingest pipeline. Support markdown and local-source inputs with source recording, note creation, index updates, and log entries.

## Implementation Steps

### Step 1: Raw source recording

- [ ] `RawSource` struct (frontmatter for raw source files):
  - schema_version, id, title, source_type (markdown/pdf/text/url/transcript/meeting_notes/repo_doc)
  - original_path (string — where the source came from)
  - captured_at (string)
  - status: pending/imported/skipped
  - summary (string — optional, populated after ingest)
- [ ] `recordRawSource(kgHome string, source RawSource, content []byte) error`:
  1. Write content to `raw/inbox/<id>.md` (or appropriate extension)
  2. Write source metadata as YAML frontmatter
- [ ] `moveToImported(kgHome string, sourceID string) error` — move from `raw/inbox/` to `raw/imported/`
- [ ] `listPendingRawSources(kgHome string) ([]RawSource, error)` — scan `raw/inbox/`
- [ ] `isValidSourceType()` validator
- [ ] Tests: record, list pending, move to imported

### Step 2: Extraction helpers

- [ ] `extractClaims(content string) []string` — extract key claims/assertions from markdown content (simple heuristic: headers, bold text, list items with assertions)
- [ ] `extractEntities(content string) []string` — extract named entities (capitalized multi-word phrases, code identifiers, proper nouns)
- [ ] `extractDecisions(content string) []string` — extract decision-like statements (lines containing "decided", "chose", "will use", "should", etc.)
- [ ] These are intentionally simple heuristics — agents do the real synthesis
- [ ] Tests: extraction from sample markdown

### Step 3: Note creation and update rules

- [ ] `createGraphNote(kgHome string, note *GraphNote, body string) error`:
  1. Determine subdirectory from note.Type (e.g., "concept" -> "notes/concepts/")
  2. Write note file as `notes/<type>s/<id>.md` with YAML frontmatter + body
  3. Update `notes/index.md`
  4. Append to `notes/log.md`
  5. Return error if note with same ID already exists (use updateGraphNote instead)
- [ ] `updateGraphNote(kgHome string, note *GraphNote, body string) error`:
  1. Read existing note
  2. Update frontmatter fields (preserve created_at, update updated_at)
  3. Replace body
  4. Update index
  5. Append update event to log
- [ ] `noteExists(kgHome, noteID string) (bool, string)` — check existence, return path
- [ ] Tests: create note, update note, duplicate creation rejected, index updated

### Step 4: Ingest pipeline

- [ ] `IngestResult` struct: source_id, notes_created ([]string), notes_updated ([]string), warnings[], errors[]
- [ ] `ingestSource(kgHome string, sourceID string) (*IngestResult, error)`:
  1. Read raw source from `raw/inbox/<sourceID>.md`
  2. Parse source metadata
  3. Create source summary note under `notes/sources/`
  4. Extract entities, claims, decisions
  5. For each extracted entity: create entity note if not exists
  6. For each decision: create decision note
  7. Cross-link: source summary links to created notes, created notes link back
  8. Move source to `raw/imported/`
  9. Update health
  10. Return ingest result
- [ ] Tests: full ingest pipeline with sample markdown source

### Step 5: `kg ingest` subcommand

- [ ] `kgIngestCmd` (Use: "ingest") with `runKGIngest()`:
  - Arg: path to source file OR `--all` to process entire inbox
  - Optional flags: `--type` (source type override), `--title`
  1. If path given: copy to `raw/inbox/`, create source metadata, run ingest
  2. If `--all`: iterate `raw/inbox/`, ingest each
  3. Display: notes created, notes updated, warnings
  4. `--json` flag for machine output
  5. `--dry-run` flag to show what would be created without writing
- [ ] Tests: ingest from file, ingest all, dry-run produces plan

### Step 6: Ingest queue management

- [ ] `kgQueueCmd` (Use: "queue") with `runKGQueue()`:
  1. List items in `raw/inbox/` with status
  2. Show count and source types
  3. `--json` flag
- [ ] Tests: queue listing with empty and populated inbox

### Step 7: Update health after ingest

- [ ] After each ingest: recompute and write `graph-health.json`
- [ ] Update `queue_depth` to reflect remaining inbox items
- [ ] Tests: health reflects post-ingest state

## Files Modified

- `commands/kg.go`
- `commands/kg_test.go`

## Acceptance Criteria

- Raw sources can be recorded and tracked
- Ingest pipeline creates source summaries, entity notes, and decision notes
- Index and log are updated on every ingest
- Health reflects current queue and note state

## Verification

```bash
go test ./commands -run 'KGIngest|RawSource|IngestPipeline|KGQueue'
go test ./commands
go test ./...
```
