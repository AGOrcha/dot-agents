# KG Phase 1: Graph Core

Spec: `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — Phase 1
Status: Completed (2026-04-10)
Depends on: Nothing (standalone subproject)

## Goal

Establish the canonical file-based graph layout under `KG_HOME`, with note schema, index, operation log, and health snapshot. This is the foundation all other phases build on.

## Decision: Subproject Structure

The KG is a separate subproject from `dot-agents`. It should live as its own Go module/CLI (`kg`) or as a subcommand tree under `dot-agents`. Given the spec says "the graph should remain usable without dot-agents", a separate CLI is cleaner. However, for pragmatic reasons in the first phase, implementing as a `dot-agents kg` subcommand tree avoids module/release overhead while keeping the code separable.

Recommended: `dot-agents kg` subcommand tree in `commands/kg.go`, separable later.

## Artifacts Established

| Path | Purpose |
|------|---------|
| `KG_HOME/self/config.yaml` | Graph root configuration and enabled adapters |
| `KG_HOME/self/schema/` | Schema definitions (for future use) |
| `KG_HOME/self/prompts/` | Agent prompts for graph operations |
| `KG_HOME/self/policies/` | Operating policies |
| `KG_HOME/raw/inbox/` | Incoming raw sources |
| `KG_HOME/raw/imported/` | Processed raw sources |
| `KG_HOME/raw/assets/` | Binary assets |
| `KG_HOME/notes/sources/` | Source summary notes |
| `KG_HOME/notes/entities/` | Entity notes |
| `KG_HOME/notes/concepts/` | Concept notes |
| `KG_HOME/notes/synthesis/` | Synthesis notes |
| `KG_HOME/notes/decisions/` | Decision records |
| `KG_HOME/notes/repos/` | Repo/subsystem context |
| `KG_HOME/notes/index.md` | Content-oriented catalog |
| `KG_HOME/notes/log.md` | Append-only operation log |
| `KG_HOME/ops/queue/` | Ingestion queue |
| `KG_HOME/ops/sessions/` | Session records |
| `KG_HOME/ops/lint/` | Lint results |
| `KG_HOME/ops/adapters/` | Adapter state |
| `KG_HOME/ops/health/graph-health.json` | Health snapshot |

## Implementation Steps

### Step 1: KG config types and path helpers

- [ ] `KGConfig` struct:
  - schema_version, name (graph name), description
  - adapters_enabled ([]string — initially empty)
  - created_at, updated_at
- [ ] `kgHome() string` — returns `$KG_HOME` or `~/knowledge-graph/` default
- [ ] `kgConfigPath() string` — `filepath.Join(kgHome(), "self", "config.yaml")`
- [ ] `loadKGConfig() (*KGConfig, error)`
- [ ] `saveKGConfig(config *KGConfig) error`
- [ ] Tests: path resolution, config round-trip, KG_HOME env override

### Step 2: Graph note schema types

- [ ] `GraphNote` struct (represents frontmatter of any graph page):
  - schema_version (int), id, type (source/entity/concept/synthesis/decision/repo/session)
  - title, summary, status (draft/active/stale/superseded/archived)
  - source_refs ([]string), links ([]string)
  - created_at, updated_at
  - confidence (low/medium/high)
- [ ] `isValidNoteType()`, `isValidNoteStatus()`, `isValidConfidence()` validators
- [ ] `parseGraphNote(content []byte) (*GraphNote, string, error)` — parse YAML frontmatter, return frontmatter + body markdown
- [ ] `renderGraphNote(note *GraphNote, body string) ([]byte, error)` — render frontmatter + body
- [ ] Tests: parse round-trip, validation, malformed frontmatter handling

### Step 3: Index and log contracts

- [ ] `appendLogEntry(kgHome string, entry string) error` — append to `notes/log.md` with heading format: `## [YYYY-MM-DD] verb | subject`
- [ ] `readLogEntries(kgHome string, limit int) ([]string, error)` — read last N entries
- [ ] `updateIndex(kgHome string, note *GraphNote) error` — add/update note entry in `notes/index.md` organized by type
- [ ] `readIndex(kgHome string) ([]IndexEntry, error)` — parse index
- [ ] `IndexEntry` struct: id, type, title, one_line_summary, path
- [ ] Tests: log append, index update, idempotent index updates

### Step 4: Health snapshot

- [ ] `GraphHealth` struct:
  - schema_version, timestamp
  - note_count, source_count, orphan_count, broken_link_count
  - stale_count, contradiction_count, queue_depth
  - status: healthy/warn/error
  - warnings[]
- [ ] `computeGraphHealth(kgHome string) (GraphHealth, error)`:
  1. Count notes by type (walk `notes/` subdirectories)
  2. Count raw sources
  3. Count queue items
  4. Set status based on counts (e.g. orphan_count > 0 -> warn)
  5. Initially: broken_link_count, contradiction_count = 0 (computed in Phase 4)
- [ ] `writeGraphHealth(kgHome string, health GraphHealth) error` — write to `ops/health/graph-health.json`
- [ ] `readGraphHealth(kgHome string) (*GraphHealth, error)`
- [ ] Tests: compute from fixture graph, health write/read

### Step 5: `kg setup` subcommand

- [ ] `kgCmd` (Use: "kg") parent command
- [ ] `kgSetupCmd` (Use: "setup") with `runKGSetup()`:
  1. Determine `kgHome()` path
  2. If already initialized (config.yaml exists), show status and exit
  3. Create full directory tree (all dirs from the layout)
  4. Write initial `self/config.yaml`
  5. Write initial empty `notes/index.md` with header
  6. Write initial empty `notes/log.md` with header
  7. Compute and write initial health snapshot
  8. Append setup event to log
  9. `ui.Success()` with graph home path
- [ ] Tests: setup creates all dirs and files, idempotent (second run doesn't error)

### Step 6: `kg health` subcommand

- [ ] `kgHealthCmd` (Use: "health") with `runKGHealth()`:
  1. Verify KG_HOME exists and is initialized
  2. Compute health snapshot
  3. Write updated health
  4. Display: note counts by type, queue depth, status, warnings
  5. `--json` flag for machine output
- [ ] Tests: health output with empty graph, graph with notes

### Step 7: Register commands

- [ ] Add `kgCmd` to root command in `cmd/dot-agents/main.go`
- [ ] `kgCmd.AddCommand(kgSetupCmd, kgHealthCmd)`

## Files Created/Modified

- `commands/kg.go` (new — all KG command code)
- `commands/kg_test.go` (new — all KG tests)
- `cmd/dot-agents/main.go` (register kgCmd)

## Acceptance Criteria

- `dot-agents kg setup` creates the full canonical layout
- `dot-agents kg health` reports graph status
- Graph notes can be parsed and rendered with valid frontmatter
- Index and log contracts are established and appendable

## Verification

```bash
go test ./commands -run 'KG|GraphNote|GraphHealth'
go test ./commands
go test ./...
```
